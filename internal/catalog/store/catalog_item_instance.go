package store

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/dcm-project/control-plane/internal/catalog/store/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	// ErrCatalogItemInstanceNotFound is returned when a catalog item instance is not found
	ErrCatalogItemInstanceNotFound = errors.New("catalog item instance not found")
	// ErrCatalogItemInstanceIDTaken is returned when a catalog item instance ID is already taken
	ErrCatalogItemInstanceIDTaken = errors.New("catalog item instance ID already exists")
	// ErrCatalogItemNotFoundRef is returned when the referenced catalog item does not exist
	ErrCatalogItemNotFoundRef = errors.New("referenced catalog item does not exist")
	// ErrCatalogItemInstanceConflict is returned when a concurrent modification is detected
	ErrCatalogItemInstanceConflict = errors.New("catalog item instance was modified concurrently")
)

// CatalogItemInstanceListOptions contains options for listing catalog item instances
type CatalogItemInstanceListOptions struct {
	PageToken     *string
	PageSize      int
	CatalogItemId *string
}

// CatalogItemInstanceListResult contains the result of a List operation
type CatalogItemInstanceListResult struct {
	CatalogItemInstances model.CatalogItemInstanceList
	NextPageToken        *string
}

// CatalogItemInstanceStore defines operations for CatalogItemInstance resources
type CatalogItemInstanceStore interface {
	List(ctx context.Context, opts *CatalogItemInstanceListOptions) (*CatalogItemInstanceListResult, error)
	Create(ctx context.Context, catalogItemInstance model.CatalogItemInstance) (*model.CatalogItemInstance, error)
	Get(ctx context.Context, id string) (*model.CatalogItemInstance, error)
	Update(ctx context.Context, catalogItemInstance *model.CatalogItemInstance) (*model.CatalogItemInstance, error)
	UpdateRunID(ctx context.Context, id string, expectedRunID string, newRunID string) (*model.CatalogItemInstance, error)
	Delete(ctx context.Context, id string) error
}

type catalogItemInstanceStore struct {
	db     *gorm.DB
	logger *slog.Logger
}

// NewCatalogItemInstanceStore creates a new CatalogItemInstance store
func NewCatalogItemInstanceStore(db *gorm.DB, logger *slog.Logger) CatalogItemInstanceStore {
	return &catalogItemInstanceStore{db: db, logger: logger}
}

// List returns a paginated list of catalog item instances
func (s *catalogItemInstanceStore) List(ctx context.Context, opts *CatalogItemInstanceListOptions) (*CatalogItemInstanceListResult, error) {
	var catalogItemInstances model.CatalogItemInstanceList
	query := s.db.WithContext(ctx)

	// Default max page size
	pageSize := 100
	if opts != nil && opts.PageSize > 0 {
		pageSize = opts.PageSize
	}

	offset := 0
	if opts != nil && opts.PageToken != nil && *opts.PageToken != "" {
		var err error
		offset, err = decodePageToken(*opts.PageToken)
		if err != nil {
			return nil, err
		}
	}

	query = query.Order("id ASC").Limit(pageSize + 1).Offset(offset)
	if opts != nil && opts.CatalogItemId != nil && *opts.CatalogItemId != "" {
		query = query.Where("spec_catalog_item_id = ?", *opts.CatalogItemId)
	}

	if err := query.Find(&catalogItemInstances).Error; err != nil {
		return nil, err
	}

	result := &CatalogItemInstanceListResult{
		CatalogItemInstances: catalogItemInstances,
	}
	if len(catalogItemInstances) > pageSize {
		result.CatalogItemInstances = catalogItemInstances[:pageSize]
		nextOffset := offset + pageSize
		nextPageToken, err := encodePageToken(nextOffset)
		if err != nil {
			return nil, err
		}
		result.NextPageToken = &nextPageToken
	}
	return result, nil
}

// Create creates a new catalog item
func (s *catalogItemInstanceStore) Create(ctx context.Context, catalogItemInstance model.CatalogItemInstance) (*model.CatalogItemInstance, error) {
	catalogItemInstance.SpecCatalogItemId = catalogItemInstance.Spec.CatalogItemId
	if err := s.db.WithContext(ctx).Clauses(clause.Returning{}).Create(&catalogItemInstance).Error; err != nil {
		return nil, s.mapConstraintError(ctx, err, catalogItemInstance)
	}
	return &catalogItemInstance, nil
}

// mapConstraintError maps a DB constraint violation to a store sentinel error
func (s *catalogItemInstanceStore) mapConstraintError(ctx context.Context, err error, attempted model.CatalogItemInstance) error {
	if err == nil {
		return nil
	}

	errStr := strings.ToLower(err.Error())

	// Check for foreign key violation first (before checking for generic constraint failed)
	if strings.Contains(errStr, "foreign key") {
		// Verify which constraint failed by checking if catalog item exists
		var ci model.CatalogItem
		if err := s.db.WithContext(ctx).Where("id = ?", attempted.SpecCatalogItemId).First(&ci).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrCatalogItemNotFoundRef
			}
		}
		return err
	}

	// Handle unique constraint violations
	if errors.Is(err, gorm.ErrDuplicatedKey) ||
		strings.Contains(errStr, "unique") ||
		strings.Contains(errStr, "duplicate key") {
		var row model.CatalogItemInstance
		dberr := s.db.WithContext(ctx).Where("id = ?", attempted.ID).Limit(1).First(&row).Error
		if dberr == nil {
			return ErrCatalogItemInstanceIDTaken
		}
		if !errors.Is(dberr, gorm.ErrRecordNotFound) {
			return err
		}
	}

	return err
}

// Get retrieves a catalog item by ID
func (s *catalogItemInstanceStore) Get(ctx context.Context, id string) (*model.CatalogItemInstance, error) {
	var catalogItemInstance model.CatalogItemInstance
	if err := s.db.WithContext(ctx).Where("id = ?", id).First(&catalogItemInstance).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrCatalogItemInstanceNotFound
		}
		return nil, fmt.Errorf("failed to get catalog item instance: %w", err)
	}
	return &catalogItemInstance, nil
}

// Update updates a catalog item (only mutable fields)
func (s *catalogItemInstanceStore) Update(ctx context.Context, catalogItemInstance *model.CatalogItemInstance) (*model.CatalogItemInstance, error) {
	// Extract catalog item ID from spec for denormalized field
	catalogItemInstance.SpecCatalogItemId = catalogItemInstance.Spec.CatalogItemId

	result := s.db.WithContext(ctx).Model(&model.CatalogItemInstance{}).
		Where("id = ?", catalogItemInstance.ID).
		Select("display_name", "spec", "spec_catalog_item_id").
		Updates(catalogItemInstance)

	if result.Error != nil {
		return nil, s.mapConstraintError(ctx, result.Error, *catalogItemInstance)
	}
	if result.RowsAffected == 0 {
		return nil, ErrCatalogItemInstanceNotFound
	}
	return catalogItemInstance, nil
}

// UpdateRunID atomically updates run_id only when it still matches expectedRunID.
// Returns ErrCatalogItemInstanceConflict if the row exists but run_id has changed.
func (s *catalogItemInstanceStore) UpdateRunID(ctx context.Context, id string, expectedRunID string, newRunID string) (*model.CatalogItemInstance, error) {
	result := s.db.WithContext(ctx).Model(&model.CatalogItemInstance{}).
		Where("id = ? AND run_id = ?", id, expectedRunID).
		Update("run_id", newRunID)

	if result.Error != nil {
		return nil, fmt.Errorf("failed to update run ID: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		_, err := s.Get(ctx, id)
		if errors.Is(err, ErrCatalogItemInstanceNotFound) {
			return nil, ErrCatalogItemInstanceNotFound
		}
		if err != nil {
			return nil, fmt.Errorf("failed to check instance existence: %w", err)
		}
		return nil, ErrCatalogItemInstanceConflict
	}

	return s.Get(ctx, id)
}

// Delete deletes a catalog item by ID
func (s *catalogItemInstanceStore) Delete(ctx context.Context, id string) error {
	result := s.db.WithContext(ctx).Where("id = ?", id).Delete(&model.CatalogItemInstance{})
	if result.Error != nil {
		return fmt.Errorf("failed to delete catalog item instance: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return ErrCatalogItemInstanceNotFound
	}
	return nil
}
