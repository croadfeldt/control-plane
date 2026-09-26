package records

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
)

// StateRecord is one immutable per-state record as stored. The columns beside
// Body exist for lookup only; Body is the record and is never edited.
type StateRecord struct {
	RecordUUID   string         `gorm:"primaryKey;type:varchar(36)"`
	EntityUUID   string         `gorm:"column:entity_uuid;type:varchar(36);index;not null"`
	TenantUUID   string         `gorm:"column:tenant_uuid;type:varchar(36);index;not null"`
	RecordType   string         `gorm:"column:record_type;type:varchar(32);not null"`
	State        string         `gorm:"column:state;type:varchar(16);not null"`
	ResourceType string         `gorm:"column:resource_type;type:varchar(128);not null"`
	Generation   int            `gorm:"column:generation;not null;default:1"`
	Head         string         `gorm:"column:head;type:varchar(80);not null"`
	Body         map[string]any `gorm:"column:body;type:jsonb;serializer:json;not null"`
	CreatedAt    time.Time      `gorm:"column:created_at;autoCreateTime"`
}

// TableName keeps the table apart from every upstream table.
func (StateRecord) TableName() string { return "udlm_records" }

// ErrNotFound is returned when no record matches.
var ErrNotFound = errors.New("udlm record not found")

// Store persists state records. Records are only ever inserted.
type Store interface {
	Put(ctx context.Context, rec *StateRecord) error
	// Latest returns the newest record of one state for an entity.
	Latest(ctx context.Context, entityUUID, state string) (*StateRecord, error)
	// Tail returns the newest record of any state for an entity (the chain head).
	Tail(ctx context.Context, entityUUID string) (*StateRecord, error)
	// ListByEntity returns every record for an entity, oldest first.
	ListByEntity(ctx context.Context, entityUUID string) ([]StateRecord, error)
	// ListEntities pages through the entities that have records.
	ListEntities(ctx context.Context, pageSize int, pageToken string) ([]EntitySummary, string, error)
}

type gormStore struct{ db *gorm.DB }

// NewStore returns a Store on the given database. Migrate StateRecord first.
func NewStore(db *gorm.DB) Store { return &gormStore{db: db} }

func (s *gormStore) Put(ctx context.Context, rec *StateRecord) error {
	return s.db.WithContext(ctx).Create(rec).Error
}

func (s *gormStore) Latest(ctx context.Context, entityUUID, state string) (*StateRecord, error) {
	var rec StateRecord
	err := s.db.WithContext(ctx).
		Where("entity_uuid = ? AND state = ?", entityUUID, state).
		Order("created_at DESC, record_uuid DESC").
		First(&rec).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	return &rec, err
}

func (s *gormStore) Tail(ctx context.Context, entityUUID string) (*StateRecord, error) {
	var rec StateRecord
	err := s.db.WithContext(ctx).
		Where("entity_uuid = ?", entityUUID).
		Order("created_at DESC, record_uuid DESC").
		First(&rec).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	return &rec, err
}

func (s *gormStore) ListByEntity(ctx context.Context, entityUUID string) ([]StateRecord, error) {
	var recs []StateRecord
	err := s.db.WithContext(ctx).
		Where("entity_uuid = ?", entityUUID).
		Order("created_at ASC, record_uuid ASC").
		Find(&recs).Error
	return recs, err
}

// EntitySummary is one row of ListEntities.
type EntitySummary struct {
	EntityUUID   string
	TenantUUID   string
	ResourceType string
	TypeVersion  string
	RecordCount  int
	Latest       *StateRecord // the newest record of any state
	Lifecycle    string       // from the states present
}

// ListEntities pages through entities that have records, ordered by entity
// uuid; pageToken is the last entity uuid of the previous page.
func (s *gormStore) ListEntities(ctx context.Context, pageSize int, pageToken string) ([]EntitySummary, string, error) {
	if pageSize <= 0 {
		pageSize = 100
	}
	var ids []string
	q := s.db.WithContext(ctx).Model(&StateRecord{}).Distinct("entity_uuid").Order("entity_uuid ASC").Limit(pageSize + 1)
	if pageToken != "" {
		q = q.Where("entity_uuid > ?", pageToken)
	}
	if err := q.Pluck("entity_uuid", &ids).Error; err != nil {
		return nil, "", err
	}
	next := ""
	if len(ids) > pageSize {
		ids = ids[:pageSize]
		next = ids[len(ids)-1]
	}
	out := make([]EntitySummary, 0, len(ids))
	for _, id := range ids {
		recs, err := s.ListByEntity(ctx, id)
		if err != nil {
			return nil, "", err
		}
		if len(recs) == 0 {
			continue
		}
		bodies := make([]map[string]any, len(recs))
		for i := range recs {
			bodies[i] = recs[i].Body
		}
		latest := recs[len(recs)-1]
		out = append(out, EntitySummary{
			EntityUUID:   id,
			TenantUUID:   latest.TenantUUID,
			ResourceType: latest.ResourceType,
			TypeVersion:  stringOf(latest.Body["type_version"]),
			RecordCount:  len(recs),
			Latest:       &latest,
			Lifecycle:    LifecycleState(bodies),
		})
	}
	return out, next, nil
}
