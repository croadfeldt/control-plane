package service

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/dcm-project/control-plane/api/catalog/v1alpha1"
	"github.com/dcm-project/control-plane/internal/catalog/store"
	"github.com/dcm-project/control-plane/internal/catalog/store/model"
)

// CreateCatalogItemRequest contains the parameters for creating a catalog item
type CreateCatalogItemRequest struct {
	ID          *string                  // Optional user-specified ID
	ApiVersion  string                   // e.g., "v1alpha1"
	DisplayName string                   // Required, max 63 chars
	Spec        v1alpha1.CatalogItemSpec // Required, contains catalog resources
}

// UpdateCatalogItemRequest contains the parameters for updating a catalog item
type UpdateCatalogItemRequest struct {
	DisplayName *string                   // Optional, max 63 chars
	Spec        *v1alpha1.CatalogItemSpec // Optional, but if provided, validates fields
}

// CatalogItemListOptions contains options for listing catalog items
type CatalogItemListOptions struct {
	PageToken   *string
	MaxPageSize *int32
	ServiceType *string // Filter by service_type
	FiledUnder  *string // Filter by usage group (ADR-082)
}

// CatalogItemListResult contains the result of a List operation
type CatalogItemListResult struct {
	CatalogItems  []v1alpha1.CatalogItem
	NextPageToken *string
}

// CatalogItemService defines the business logic for CatalogItem operations
type CatalogItemService interface {
	List(ctx context.Context, opts CatalogItemListOptions) (*CatalogItemListResult, error)
	Create(ctx context.Context, req *CreateCatalogItemRequest) (*v1alpha1.CatalogItem, error)
	Get(ctx context.Context, id string) (*v1alpha1.CatalogItem, error)
	Update(ctx context.Context, id string, req *UpdateCatalogItemRequest) (*v1alpha1.CatalogItem, error)
	Delete(ctx context.Context, id string) error
}

type catalogItemService struct {
	store  store.Store
	logger *slog.Logger
}

// newCatalogItemService creates a new CatalogItemService instance
func newCatalogItemService(store store.Store, logger *slog.Logger) CatalogItemService {
	return &catalogItemService{store: store, logger: logger}
}

// List returns a paginated list of catalog items
func (s *catalogItemService) List(ctx context.Context, opts CatalogItemListOptions) (*CatalogItemListResult, error) {
	// Convert service options to store options
	storeOpts := &store.CatalogItemListOptions{
		PageToken:   opts.PageToken,
		ServiceType: opts.ServiceType,
	}
	if opts.FiledUnder != nil && *opts.FiledUnder != "" {
		group := *opts.FiledUnder
		storeOpts.Match = func(m model.CatalogItem) bool { return filedUnder(catalogItemGroups(m.Spec), group) }
	}
	if opts.MaxPageSize != nil {
		storeOpts.PageSize = int(*opts.MaxPageSize)
	}

	// Call store layer
	storeResult, err := s.store.CatalogItem().List(ctx, storeOpts)
	if err != nil {
		return nil, err
	}

	// Convert store models to API types
	apiTypes := make([]v1alpha1.CatalogItem, len(storeResult.CatalogItems))
	for i, storeModel := range storeResult.CatalogItems {
		apiTypes[i] = catalogItemToAPIType(&storeModel)
	}

	return &CatalogItemListResult{
		CatalogItems:  apiTypes,
		NextPageToken: storeResult.NextPageToken,
	}, nil
}

// Create creates a new catalog item (request validation is performed by OpenAPI middleware)
func (s *catalogItemService) Create(ctx context.Context, req *CreateCatalogItemRequest) (*v1alpha1.CatalogItem, error) {
	// Generate ID
	id := getOrGenerateID(req.ID)
	// Generate path
	path := fmt.Sprintf("catalog-items/%s", id)

	// Convert to store model
	storeModel := catalogItemToStoreModel(id, path, req)

	if err := validateCatalogItemSpec(ctx, s.store, storeModel.Spec); err != nil {
		s.logger.WarnContext(ctx, "Catalog item spec validation failed", "id", id, "error", err)
		return nil, err
	}

	// Call store layer
	createdModel, err := s.store.CatalogItem().Create(ctx, storeModel)
	if err != nil {
		s.logger.ErrorContext(ctx, "Failed to create catalog item in store", "id", id, "error", err)
		return nil, mapCatalogItemStoreError(err)
	}

	s.logger.InfoContext(ctx, "Catalog item created", "id", id)
	// Convert result back to API type
	apiType := catalogItemToAPIType(createdModel)
	return &apiType, nil
}

// Get retrieves a catalog item by ID
func (s *catalogItemService) Get(ctx context.Context, id string) (*v1alpha1.CatalogItem, error) {
	// Call store layer
	storeModel, err := s.store.CatalogItem().Get(ctx, id)
	if err != nil {
		return nil, mapCatalogItemStoreError(err)
	}

	// Convert to API type
	apiType := catalogItemToAPIType(storeModel)
	return &apiType, nil
}

// Update updates an existing catalog item with validation
func (s *catalogItemService) Update(ctx context.Context, id string, req *UpdateCatalogItemRequest) (*v1alpha1.CatalogItem, error) {
	// Fetch existing item first to validate immutability
	existing, err := s.store.CatalogItem().Get(ctx, id)
	if err != nil {
		return nil, mapCatalogItemStoreError(err)
	}

	// Build updated model starting from existing
	updated, err := mergeCatalogItem(existing, req)
	if err != nil {
		s.logger.WarnContext(ctx, "Catalog item merge validation failed", "id", id, "error", err)
		return nil, err
	}

	// Validate spec after merge
	if err := validateCatalogItemSpec(ctx, s.store, updated.Spec); err != nil {
		s.logger.WarnContext(ctx, "Catalog item spec validation failed on update", "id", id, "error", err)
		return nil, err
	}

	// Call store layer (it only updates display_name and spec)
	err = s.store.CatalogItem().Update(ctx, updated)
	if err != nil {
		s.logger.ErrorContext(ctx, "Failed to update catalog item in store", "id", id, "error", err)
		return nil, mapCatalogItemStoreError(err)
	}

	// Fetch the updated item to get the new update_time
	updatedModel, err := s.store.CatalogItem().Get(ctx, id)
	if err != nil {
		return nil, mapCatalogItemStoreError(err)
	}

	s.logger.InfoContext(ctx, "Catalog item updated", "id", id)
	// Convert result back to API type
	apiType := catalogItemToAPIType(updatedModel)
	return &apiType, nil
}

func mergeCatalogItem(existing *model.CatalogItem, req *UpdateCatalogItemRequest) (*model.CatalogItem, error) {
	merged := *existing
	// Apply display_name if provided (validation is performed by OpenAPI middleware)
	if req.DisplayName != nil {
		merged.DisplayName = *req.DisplayName
	}

	// Validate and apply spec if provided
	if req.Spec != nil {
		newSpec := catalogItemSpecAPIToModel(*req.Spec)
		if err := validateCatalogImmutable(existing.Spec, newSpec); err != nil {
			return nil, err
		}
		merged.Spec = newSpec
	}
	return &merged, nil
}

// Delete deletes a catalog item by ID
func (s *catalogItemService) Delete(ctx context.Context, id string) error {
	err := s.store.CatalogItem().Delete(ctx, id)
	if err != nil {
		s.logger.ErrorContext(ctx, "Failed to delete catalog item", "id", id, "error", err)
		return mapCatalogItemStoreError(err)
	}
	s.logger.InfoContext(ctx, "Catalog item deleted", "id", id)
	return nil
}
