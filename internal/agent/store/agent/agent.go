// Package agent provides the GORM-based store for Agent entities.
package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/dcm-project/control-plane/internal/agent/store/model"
	"gorm.io/gorm"
)

var (
	ErrAgentNotFound = errors.New("agent not found")
	// ErrAgentConflict is returned when a Create/Update violates the unique
	// constraint on name or topic_name (e.g. two concurrent registrations of
	// a new agent with the same name).
	ErrAgentConflict = errors.New("agent name or topic name already in use")
)

// isUniqueViolation detects a unique-constraint violation across the DB
// drivers this store runs against (Postgres in production, SQLite in
// tests), matching the pattern already used in internal/auth/service and
// internal/placement/store for the same purpose.
func isUniqueViolation(err error) bool {
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "unique constraint") || strings.Contains(msg, "duplicate key")
}

type AgentFilter struct {
	HealthStatus *model.AgentHealthStatus
}

// Pagination carries the requested page size and an opaque page_token (as
// previously returned in AgentListResult.NextPageToken) for List. A zero
// Limit falls back to defaultListPageSize.
type Pagination struct {
	Limit     int
	PageToken string
}

// AgentListResult is the result of a List call: the page of agents plus an
// opaque NextPageToken (empty when there are no further pages), mirroring
// internal/policy/store/policy.go's PolicyListResult.
type AgentListResult struct {
	Agents        model.AgentList
	NextPageToken string
}

type Agent interface {
	Create(ctx context.Context, agent model.Agent) (*model.Agent, error)
	Get(ctx context.Context, id string) (*model.Agent, error)
	GetByName(ctx context.Context, name string) (*model.Agent, error)
	// List returns agents matching filter, paginated per pagination. Pass
	// pagination.PageToken from a prior AgentListResult.NextPageToken to
	// fetch the next page; ErrInvalidPageToken is returned if it can't be
	// decoded.
	List(ctx context.Context, filter *AgentFilter, pagination *Pagination) (*AgentListResult, error)
	Update(ctx context.Context, agent model.Agent) (*model.Agent, error)
	Delete(ctx context.Context, id string) error
	ListReady(ctx context.Context) (model.AgentList, error)
	// UpdateHeartbeatIfNewer atomically updates last_heartbeat/health_status
	// only if ts is strictly newer than the stored last_heartbeat (or none is
	// stored yet). Returns applied=false without error if ts is stale
	// (monotonicity), so two concurrent/out-of-order heartbeats can't race
	// past each other's in-memory read and have the older one win.
	UpdateHeartbeatIfNewer(ctx context.Context, id string, ts time.Time, healthStatus model.AgentHealthStatus) (bool, error)
	// MarkStaleUnavailable flips every agent whose heartbeat (or, absent
	// one, creation time) is older than cutoff to Unavailable in a single
	// conditional UPDATE, so the health check has no read-then-write window
	// in which a concurrent heartbeat could be clobbered back to Unavailable.
	MarkStaleUnavailable(ctx context.Context, cutoff time.Time) error
}

type AgentStore struct {
	db *gorm.DB
}

var _ Agent = (*AgentStore)(nil)

func NewAgent(db *gorm.DB) Agent {
	return &AgentStore{db: db}
}

func (s *AgentStore) Create(ctx context.Context, agent model.Agent) (*model.Agent, error) {
	if err := s.db.WithContext(ctx).Create(&agent).Error; err != nil {
		if isUniqueViolation(err) {
			return nil, ErrAgentConflict
		}
		return nil, err
	}
	return &agent, nil
}

func (s *AgentStore) Get(ctx context.Context, id string) (*model.Agent, error) {
	var agent model.Agent
	if err := s.db.WithContext(ctx).First(&agent, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrAgentNotFound
		}
		return nil, err
	}
	return &agent, nil
}

func (s *AgentStore) GetByName(ctx context.Context, name string) (*model.Agent, error) {
	var agent model.Agent
	if err := s.db.WithContext(ctx).First(&agent, "name = ?", name).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrAgentNotFound
		}
		return nil, err
	}
	return &agent, nil
}

// defaultListPageSize is used when pagination is nil or its Limit is <= 0,
// matching the max_page_size default documented in
// api/agent/v1alpha1/openapi.yaml.
const defaultListPageSize = 100

func (s *AgentStore) List(ctx context.Context, filter *AgentFilter, pagination *Pagination) (*AgentListResult, error) {
	var agents model.AgentList
	query := s.db.WithContext(ctx)

	if filter != nil && filter.HealthStatus != nil {
		query = query.Where("health_status = ?", *filter.HealthStatus)
	}

	pageSize := defaultListPageSize
	offset := 0
	if pagination != nil {
		if pagination.Limit > 0 {
			pageSize = pagination.Limit
		}
		if pagination.PageToken != "" {
			var err error
			offset, err = decodePageToken(pagination.PageToken)
			if err != nil {
				return nil, err
			}
		}
	}

	// Query with limit+1 to detect whether there are more results beyond
	// this page, mirroring internal/policy/store/policy.go's List.
	query = query.Order("name ASC").Limit(pageSize + 1).Offset(offset)
	if err := query.Find(&agents).Error; err != nil {
		return nil, err
	}

	result := &AgentListResult{Agents: agents}
	if len(agents) > pageSize {
		result.Agents = agents[:pageSize]
		nextOffset := offset + pageSize
		nextToken, err := encodePageToken(nextOffset)
		if err != nil {
			return nil, err
		}
		result.NextPageToken = nextToken
	}
	return result, nil
}

func (s *AgentStore) Update(ctx context.Context, agent model.Agent) (*model.Agent, error) {
	// service_types has a `serializer:json` tag for struct-based Create/Save,
	// but a map-based Updates call bypasses that serializer, so it must be
	// marshalled by hand here or the driver receives a raw []string and
	// mis-binds it as a composite value.
	serviceTypesJSON, err := json.Marshal(agent.ServiceTypes)
	if err != nil {
		return nil, err
	}

	result := s.db.WithContext(ctx).Model(&model.Agent{}).Where("id = ?", agent.ID).Updates(map[string]any{
		"environment":   agent.Environment,
		"service_types": string(serviceTypesJSON),
		"cost":          agent.Cost,
		"topic_name":    agent.TopicName,
	})
	if result.Error != nil {
		if isUniqueViolation(result.Error) {
			return nil, ErrAgentConflict
		}
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		return nil, ErrAgentNotFound
	}

	// CAS-guarded by the same monotonicity rule as UpdateHeartbeatIfNewer, so
	// a concurrent, newer heartbeat can't be clobbered by this re-registration.
	if agent.LastHeartbeat != nil {
		if err := s.db.WithContext(ctx).Model(&model.Agent{}).
			Where("id = ? AND (last_heartbeat IS NULL OR last_heartbeat < ?)", agent.ID, *agent.LastHeartbeat).
			Updates(map[string]any{
				"last_heartbeat": agent.LastHeartbeat,
				"health_status":  agent.HealthStatus,
			}).Error; err != nil {
			return nil, err
		}
	}

	return s.Get(ctx, agent.ID)
}

func (s *AgentStore) Delete(ctx context.Context, id string) error {
	result := s.db.WithContext(ctx).Where("id = ?", id).Delete(&model.Agent{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrAgentNotFound
	}
	return nil
}

func (s *AgentStore) ListReady(ctx context.Context) (model.AgentList, error) {
	var agents model.AgentList
	if err := s.db.WithContext(ctx).
		Where("health_status = ?", model.AgentHealthStatusReady).
		Find(&agents).Error; err != nil {
		return nil, err
	}
	return agents, nil
}

func (s *AgentStore) UpdateHeartbeatIfNewer(ctx context.Context, id string, ts time.Time, healthStatus model.AgentHealthStatus) (bool, error) {
	result := s.db.WithContext(ctx).Model(&model.Agent{}).
		Where("id = ? AND (last_heartbeat IS NULL OR last_heartbeat < ?)", id, ts).
		Updates(map[string]any{
			"last_heartbeat": ts,
			"health_status":  healthStatus,
		})
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}

func (s *AgentStore) MarkStaleUnavailable(ctx context.Context, cutoff time.Time) error {
	return s.db.WithContext(ctx).Model(&model.Agent{}).
		Where("health_status != ?", model.AgentHealthStatusUnavailable).
		Where("(last_heartbeat IS NOT NULL AND last_heartbeat < ?) OR (last_heartbeat IS NULL AND create_time < ?)", cutoff, cutoff).
		Update("health_status", model.AgentHealthStatusUnavailable).Error
}
