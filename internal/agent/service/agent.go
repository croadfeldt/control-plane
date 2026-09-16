// Package service implements agent business logic.
package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	api "github.com/dcm-project/control-plane/api/agent/v1alpha1"
	agentstore "github.com/dcm-project/control-plane/internal/agent/store/agent"
	"github.com/dcm-project/control-plane/internal/agent/store/model"
	"github.com/google/uuid"
)

type AgentService struct {
	store                agentstore.Agent
	consumerLagThreshold int64
}

func NewAgentService(store agentstore.Agent, consumerLagThreshold int64) *AgentService {
	return &AgentService{
		store:                store,
		consumerLagThreshold: consumerLagThreshold,
	}
}

func (s *AgentService) RegisterOrUpdate(ctx context.Context, req api.AgentRegistrationRequest) (*api.Agent, bool, error) {
	if !strings.HasPrefix(req.TopicName, "dcm.agent.") {
		return nil, false, NewValidationError("topic_name must start with 'dcm.agent.'")
	}

	existing, err := s.store.GetByName(ctx, req.Name)
	if err != nil && !errors.Is(err, agentstore.ErrAgentNotFound) {
		return nil, false, err
	}

	cost := model.AgentCost(req.Cost)

	if existing != nil {
		existing.Environment = req.Environment
		existing.ServiceTypes = req.ServiceTypes
		existing.Cost = &cost
		existing.TopicName = req.TopicName
		existing.HealthStatus = model.AgentHealthStatusReady
		now := time.Now()
		existing.LastHeartbeat = &now

		updated, err := s.store.Update(ctx, *existing)
		if err != nil {
			if errors.Is(err, agentstore.ErrAgentConflict) {
				return nil, false, NewConflictError(fmt.Sprintf("topic name %q is already registered to another agent", req.TopicName))
			}
			return nil, false, err
		}
		result := modelToAPI(updated)
		return &result, false, nil
	}

	agent := model.Agent{
		ID:           uuid.New().String(),
		Name:         req.Name,
		Environment:  req.Environment,
		ServiceTypes: req.ServiceTypes,
		Cost:         &cost,
		TopicName:    req.TopicName,
		HealthStatus: model.AgentHealthStatusReady,
	}

	created, err := s.store.Create(ctx, agent)
	if err != nil {
		if errors.Is(err, agentstore.ErrAgentConflict) {
			return nil, false, NewConflictError(fmt.Sprintf("agent name %q or topic name %q is already registered", req.Name, req.TopicName))
		}
		return nil, false, err
	}
	result := modelToAPI(created)
	return &result, true, nil
}

func (s *AgentService) Get(ctx context.Context, id string) (*api.Agent, error) {
	agent, err := s.store.Get(ctx, id)
	if err != nil {
		if errors.Is(err, agentstore.ErrAgentNotFound) {
			return nil, NewNotFoundError("agent not found")
		}
		return nil, err
	}
	result := modelToAPI(agent)
	return &result, nil
}

func (s *AgentService) List(ctx context.Context, healthStatus string, pageSize int, pageToken string) (*AgentListResult, error) {
	var filter *agentstore.AgentFilter
	if healthStatus != "" {
		hs := model.AgentHealthStatus(healthStatus)
		filter = &agentstore.AgentFilter{HealthStatus: &hs}
	}

	pagination := &agentstore.Pagination{Limit: pageSize, PageToken: pageToken}

	storeResult, err := s.store.List(ctx, filter, pagination)
	if err != nil {
		if errors.Is(err, agentstore.ErrInvalidPageToken) {
			return nil, NewValidationError("invalid page_token")
		}
		return nil, err
	}

	result := &AgentListResult{NextPageToken: storeResult.NextPageToken}
	for _, a := range storeResult.Agents {
		result.Agents = append(result.Agents, modelToAPI(&a))
	}
	return result, nil
}

func (s *AgentService) Heartbeat(ctx context.Context, agentID string, req api.HeartbeatRequest) (*api.Agent, error) {
	if _, err := s.store.Get(ctx, agentID); err != nil {
		if errors.Is(err, agentstore.ErrAgentNotFound) {
			return nil, NewNotFoundError("agent not found")
		}
		return nil, err
	}

	healthStatus := model.AgentHealthStatusReady
	if req.ConsumerLag >= s.consumerLagThreshold {
		healthStatus = model.AgentHealthStatusCongested
	}

	// Atomic conditional UPDATE, not read-compare-then-write: two concurrent
	// out-of-order heartbeats could otherwise both pass a Go-level staleness
	// check and let the later Update() win regardless of timestamp order.
	if _, err := s.store.UpdateHeartbeatIfNewer(ctx, agentID, req.Timestamp, healthStatus); err != nil {
		return nil, err
	}
	// Re-fetch rather than reuse the pre-update snapshot: a concurrent
	// heartbeat or health sweep could have changed the row in between.
	updated, err := s.store.Get(ctx, agentID)
	if err != nil {
		return nil, err
	}
	result := modelToAPI(updated)
	return &result, nil
}

type AgentListResult struct {
	Agents        []api.Agent
	NextPageToken string
}

func modelToAPI(m *model.Agent) api.Agent {
	hs := api.AgentHealthStatus(m.HealthStatus)
	st := make([]string, len(m.ServiceTypes))
	copy(st, m.ServiceTypes)
	a := api.Agent{
		AgentId:      &m.ID,
		Name:         &m.Name,
		Environment:  &m.Environment,
		ServiceTypes: &st,
		TopicName:    &m.TopicName,
		HealthStatus: &hs,
		CreateTime:   &m.CreateTime,
		UpdateTime:   &m.UpdateTime,
	}
	if m.LastHeartbeat != nil {
		a.LastHeartbeat = m.LastHeartbeat
	}
	if m.Cost != nil {
		cost := api.AgentCost(*m.Cost)
		a.Cost = &cost
	}
	return a
}
