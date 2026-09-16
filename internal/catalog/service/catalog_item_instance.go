package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/dcm-project/control-plane/api/catalog/v1alpha1"
	"github.com/dcm-project/control-plane/internal/catalog/placement"
	"github.com/dcm-project/control-plane/internal/catalog/store"
	"github.com/google/uuid"
)

const catalogItemInstanceRollbackTimeout = 10 * time.Second

// CreateCatalogItemInstanceRequest contains the parameters for creating a catalog item instance
type CreateCatalogItemInstanceRequest struct {
	ID          *string                          // Optional user-specified ID
	ApiVersion  string                           // e.g., "v1alpha1"
	DisplayName string                           // Required, max 63 chars
	Spec        v1alpha1.CatalogItemInstanceSpec // Required, contains catalog_item_id and user_values
}

// CatalogItemInstanceListOptions contains options for listing catalog item instances
type CatalogItemInstanceListOptions struct {
	PageToken     *string
	MaxPageSize   *int32
	CatalogItemId *string // Filter by catalog_item_id
}

// CatalogItemInstanceListResult contains the result of a List operation
type CatalogItemInstanceListResult struct {
	CatalogItemInstances []v1alpha1.CatalogItemInstance
	NextPageToken        *string
}

// CatalogItemInstanceService defines the business logic for CatalogItemInstance operations
type CatalogItemInstanceService interface {
	List(ctx context.Context, opts CatalogItemInstanceListOptions) (*CatalogItemInstanceListResult, error)
	Create(ctx context.Context, req *CreateCatalogItemInstanceRequest) (*v1alpha1.CatalogItemInstance, error)
	Get(ctx context.Context, id string) (*v1alpha1.CatalogItemInstance, error)
	Delete(ctx context.Context, id string) error
	Rehydrate(ctx context.Context, id string) (*v1alpha1.CatalogItemInstance, error)
}

type catalogItemInstanceService struct {
	store       store.Store
	specBuilder *specBuilder
	pmClient    placement.Client
	logger      *slog.Logger
}

// newCatalogItemInstanceService creates a new CatalogItemInstanceService instance.
// pmClient must not be nil.
func newCatalogItemInstanceService(store store.Store, pmClient placement.Client, logger *slog.Logger) (CatalogItemInstanceService, error) {
	if pmClient == nil {
		return nil, fmt.Errorf("pmClient must not be nil")
	}
	return &catalogItemInstanceService{
		store:       store,
		specBuilder: newSpecBuilder(store),
		pmClient:    pmClient,
		logger:      logger,
	}, nil
}

// List returns a paginated list of catalog item instances
func (s *catalogItemInstanceService) List(ctx context.Context, opts CatalogItemInstanceListOptions) (*CatalogItemInstanceListResult, error) {
	// Convert service options to store options
	storeOpts := &store.CatalogItemInstanceListOptions{
		PageToken:     opts.PageToken,
		CatalogItemId: opts.CatalogItemId,
	}
	if opts.MaxPageSize != nil {
		storeOpts.PageSize = int(*opts.MaxPageSize)
	}

	// Call store layer
	storeResult, err := s.store.CatalogItemInstance().List(ctx, storeOpts)
	if err != nil {
		return nil, err
	}

	// Convert store models to API types
	apiTypes := make([]v1alpha1.CatalogItemInstance, len(storeResult.CatalogItemInstances))
	for i, storeModel := range storeResult.CatalogItemInstances {
		apiTypes[i] = catalogItemInstanceToAPIType(&storeModel)
	}

	return &CatalogItemInstanceListResult{
		CatalogItemInstances: apiTypes,
		NextPageToken:        storeResult.NextPageToken,
	}, nil
}

// Create creates a new catalog item instance
func (s *catalogItemInstanceService) Create(ctx context.Context, req *CreateCatalogItemInstanceRequest) (*v1alpha1.CatalogItemInstance, error) {
	// Generate IDs
	id := getOrGenerateID(req.ID)
	path := fmt.Sprintf("catalog-item-instances/%s", id)

	return s.createInstance(ctx, id, path, req)
}

func (s *catalogItemInstanceService) createInstance(ctx context.Context, id, path string, req *CreateCatalogItemInstanceRequest) (*v1alpha1.CatalogItemInstance, error) {
	resolved, err := s.specBuilder.BuildResourceGraph(ctx, req.Spec.CatalogItemId, req.Spec.UserValues)
	if err != nil {
		s.logger.WarnContext(ctx, "Failed to build resource graph",
			"id", id,
			"catalog_item_id", req.Spec.CatalogItemId,
			"error", err,
		)
		return nil, err
	}
	if len(resolved) == 0 {
		return nil, fmt.Errorf("%w: catalog item has no resources", ErrCatalogItemSpecConflict)
	}

	pmResources := make([]placement.ResourceInput, 0, len(resolved))
	for _, res := range resolved {
		pmResources = append(pmResources, placement.ResourceInput{
			Name:              res.Name,
			Spec:              res.Spec,
			RequiresResources: append([]string(nil), res.RequiresResources...),
		})
	}

	// generate run id
	runID := uuid.New().String()

	// Persist instance with run_id
	storeModel := catalogItemInstanceToStoreModel(id, path, req, runID)
	createdModel, err := s.store.CatalogItemInstance().Create(ctx, storeModel)
	if err != nil {
		s.logger.ErrorContext(ctx, "Failed to create catalog item instance in store", "id", id, "error", err)
		return nil, mapCatalogItemInstanceStoreError(err)
	}

	s.logger.DebugContext(ctx, "Calling placement to create a run",
		"id", id,
		"run_id", runID,
		"resource_count", len(pmResources),
	)
	_, err = s.pmClient.CreateRun(ctx, placement.CreateRunRequest{
		CatalogItemInstanceId: id,
		RunId:                 runID,
		Resources:             pmResources,
	})
	if err != nil {
		mapped := mapPlacementError(err, ErrPlacementManagerCreateFailed)
		if rbErr := s.rollbackCatalogItemInstanceCreate(id); rbErr != nil {
			s.logger.ErrorContext(ctx, "Placement manager create failed and rollback delete failed",
				"id", id,
				"placement_error", err,
				"rollback_error", rbErr,
			)
			return nil, fmt.Errorf("%w; additionally, rollback failed: %v", mapped, rbErr)
		}
		s.logger.ErrorContext(ctx, "Placement manager create failed, rolled back DB record",
			"id", id,
			"error", err,
		)
		return nil, mapped
	}

	s.logger.InfoContext(ctx, "Catalog item instance created",
		"id", id,
		"catalog_item_id", req.Spec.CatalogItemId,
		"run_id", runID,
		"resource_count", len(resolved),
	)
	apiType := catalogItemInstanceToAPIType(createdModel)
	return &apiType, nil
}

// Get retrieves a catalog item instance by ID
func (s *catalogItemInstanceService) Get(ctx context.Context, id string) (*v1alpha1.CatalogItemInstance, error) {
	// Call store layer
	storeModel, err := s.store.CatalogItemInstance().Get(ctx, id)
	if err != nil {
		return nil, mapCatalogItemInstanceStoreError(err)
	}

	// Convert to API type
	apiType := catalogItemInstanceToAPIType(storeModel)
	return &apiType, nil
}

// Rehydrate rehydrates a catalog item instance by generating a new run ID
// and delegating to the Placement Manager.
// Uses DB-first CAS (compare-and-swap) to prevent concurrent rehydrates: the run id is updated
// in the DB before calling PM, so only one concurrent caller can proceed.
func (s *catalogItemInstanceService) Rehydrate(ctx context.Context, id string) (*v1alpha1.CatalogItemInstance, error) {
	// Look up existing instance
	instance, err := s.store.CatalogItemInstance().Get(ctx, id)
	if err != nil {
		return nil, mapCatalogItemInstanceStoreError(err)
	}

	_, err = s.store.CatalogItem().Get(ctx, instance.Spec.CatalogItemId)
	if err != nil {
		return nil, mapCatalogItemStoreError(err)
	}

	if len(instance.RunID) == 0 {
		return nil, ErrCatalogItemInstanceRunIDEmpty
	}
	oldRunID := instance.RunID
	newRunID := uuid.New().String()

	// DB first — CAS rejects concurrent callers here
	updatedModel, err := s.store.CatalogItemInstance().UpdateRunID(ctx, id, oldRunID, newRunID)
	if err != nil {
		s.logger.ErrorContext(ctx, "Failed to update run ID in store",
			"id", id,
			"error", err,
		)
		return nil, mapCatalogItemInstanceStoreError(err)
	}

	// Call Placement Manager rehydrate
	s.logger.DebugContext(ctx, "Calling placement manager to rehydrate run",
		"id", id,
		"old_run_id", oldRunID,
		"new_run_id", newRunID,
	)
	_, err = s.pmClient.RehydrateResource(ctx, oldRunID, newRunID)
	if err != nil {
		mapped := mapPlacementError(err, ErrPlacementManagerRehydrateFailed)
		if rbErr := s.rollbackRehydrateRunID(id, newRunID, oldRunID); rbErr != nil {
			s.logger.ErrorContext(ctx, "Placement manager rehydrate failed and rollback update failed",
				"id", id,
				"placement_error", err,
				"rollback_error", rbErr,
			)
			return nil, fmt.Errorf("%w; additionally, rollback failed: %v", mapped, rbErr)
		}
		s.logger.ErrorContext(ctx, "Placement manager rehydrate failed, rolled back run ID",
			"id", id,
			"error", err,
		)
		return nil, mapped
	}

	s.logger.InfoContext(ctx, "Catalog item instance rehydrated",
		"id", id,
		"new_run_id", newRunID,
	)

	apiType := catalogItemInstanceToAPIType(updatedModel)
	return &apiType, nil
}

// Delete deletes a catalog item instance by ID
func (s *catalogItemInstanceService) Delete(ctx context.Context, id string) error {
	instance, err := s.store.CatalogItemInstance().Get(ctx, id)
	if err != nil {
		return mapCatalogItemInstanceStoreError(err)
	}

	_, err = s.store.CatalogItem().Get(ctx, instance.Spec.CatalogItemId)
	if err != nil {
		return mapCatalogItemStoreError(err)
	}
	if instance.RunID == "" {
		return fmt.Errorf("%w: catalog item instance has no run id", ErrPlacementManagerDeleteFailed)
	}
	s.logger.DebugContext(ctx, "Calling placement manager to delete run", "id", id, "run_id", instance.RunID)
	if err := s.pmClient.DeleteRun(ctx, instance.RunID); err != nil {
		s.logger.ErrorContext(ctx, "Placement manager delete failed", "id", id, "error", err)
		// mapPlacementError, not a direct wrap: distinguishes
		// policy-rejected/provider-error/policy-dependency (406/422/424)
		// from a generic placement failure, matching create/rehydrate.
		return mapPlacementError(err, ErrPlacementManagerDeleteFailed)
	}

	err = s.store.CatalogItemInstance().Delete(ctx, id)
	if err != nil {
		s.logger.ErrorContext(ctx, "Failed to delete catalog item instance from store", "id", id, "error", err)
		return mapCatalogItemInstanceStoreError(err)
	}

	s.logger.InfoContext(ctx, "Catalog item instance deleted", "id", id)
	return nil
}

// rollbackCatalogItemInstanceCreate deletes a catalog item instance after a failed
// placement create. Used with the DB-first create path so PM failures do not leave orphans.
func (s *catalogItemInstanceService) rollbackCatalogItemInstanceCreate(id string) error {
	rbCtx, cancel := context.WithTimeout(context.Background(), catalogItemInstanceRollbackTimeout)
	defer cancel()
	return s.store.CatalogItemInstance().Delete(rbCtx, id)
}

func (s *catalogItemInstanceService) rollbackRehydrateRunID(id, newRunID, oldRunID string) error {
	rbCtx, cancel := context.WithTimeout(context.Background(), catalogItemInstanceRollbackTimeout)
	defer cancel()
	_, err := s.store.CatalogItemInstance().UpdateRunID(rbCtx, id, newRunID, oldRunID)
	return err
}

// mapPlacementError inspects the error from the placement client and maps
// known HTTP status codes (406, 422, 424) to specific sentinel errors. For
// unrecognised codes or non-PlacementError errors, the genericSentinel is used.
func mapPlacementError(err error, genericSentinel error) error {
	var pmErr *placement.PlacementError
	if errors.As(err, &pmErr) {
		switch pmErr.StatusCode {
		case http.StatusNotAcceptable:
			return fmt.Errorf("%w: %s", ErrPlacementManagerPolicyRejected, pmErr.Error())
		case http.StatusUnprocessableEntity:
			return fmt.Errorf("%w: %s", ErrPlacementManagerProviderError, pmErr.Error())
		case http.StatusFailedDependency:
			return fmt.Errorf("%w: %s", ErrPlacementManagerPolicyDependency, pmErr.Error())
		}
	}
	return fmt.Errorf("%w: %s", genericSentinel, err.Error())
}
