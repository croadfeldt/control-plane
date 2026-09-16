package service

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/dcm-project/control-plane/api/catalog/v1alpha1"
	"github.com/dcm-project/control-plane/internal/catalog/store"
)

// allowedServiceTypes defines the restricted set of valid service type values
var allowedServiceTypes = map[string]bool{
	"vm":        true,
	"container": true,
	"cluster":   true,
	"database":  true,
	"storage":   true,
	"network":   true,
}

// CreateServiceTypeRequest contains the parameters for creating a service type
type CreateServiceTypeRequest struct {
	ID          *string   // Optional user-specified ID
	ApiVersion  string    // e.g., "v1alpha1"
	ServiceType string    // Must be: vm, container, cluster, database, storage, or network
	Metadata    *struct { // Optional labels
		Labels *map[string]string `json:"labels,omitempty"`
	}
	Spec map[string]any // Required, cannot be empty
}

// ServiceTypeListOptions contains options for listing service types
type ServiceTypeListOptions struct {
	PageToken   *string
	MaxPageSize *int32
}

// ServiceTypeListResult contains the result of a List operation
type ServiceTypeListResult struct {
	ServiceTypes  []v1alpha1.ServiceType
	NextPageToken *string
}

// ServiceTypeService defines the business logic for ServiceType operations
type ServiceTypeService interface {
	List(ctx context.Context, opts *ServiceTypeListOptions) (*ServiceTypeListResult, error)
	Create(ctx context.Context, req *CreateServiceTypeRequest) (*v1alpha1.ServiceType, error)
	Get(ctx context.Context, id string) (*v1alpha1.ServiceType, error)
}

type serviceTypeService struct {
	store  store.Store
	logger *slog.Logger
}

// newServiceTypeService creates a new ServiceTypeService instance
func newServiceTypeService(store store.Store, logger *slog.Logger) ServiceTypeService {
	return &serviceTypeService{store: store, logger: logger}
}

// List returns a paginated list of service types
func (s *serviceTypeService) List(ctx context.Context, opts *ServiceTypeListOptions) (*ServiceTypeListResult, error) {
	// Convert service options to store options
	var pageToken *string
	maxPageSize := 100
	if opts != nil {
		pageToken = opts.PageToken
		if opts.MaxPageSize != nil {
			maxPageSize = int(*opts.MaxPageSize)
		}
	}

	storeOpts := &store.ServiceTypeListOptions{
		PageToken: pageToken,
		PageSize:  maxPageSize,
	}

	// Call store layer
	storeResult, err := s.store.ServiceType().List(ctx, storeOpts)
	if err != nil {
		return nil, err
	}

	// Convert store models to API types
	apiTypes := make([]v1alpha1.ServiceType, len(storeResult.ServiceTypes))
	for i, storeModel := range storeResult.ServiceTypes {
		apiTypes[i] = toAPIType(&storeModel)
	}

	return &ServiceTypeListResult{
		ServiceTypes:  apiTypes,
		NextPageToken: storeResult.NextPageToken,
	}, nil
}

// Create creates a new service type with business validation
func (s *serviceTypeService) Create(ctx context.Context, req *CreateServiceTypeRequest) (*v1alpha1.ServiceType, error) {
	// Validate service type (must be one of the allowed values)
	if !allowedServiceTypes[req.ServiceType] {
		s.logger.WarnContext(ctx, "Invalid service type value", "service_type", req.ServiceType)
		return nil, ErrInvalidServiceType
	}

	// Generate ID
	id := getOrGenerateID(req.ID)

	// Generate path
	path := fmt.Sprintf("service-types/%s", id)

	// Convert to store model
	storeModel := toStoreModel(id, path, req)

	// Call store layer
	createdModel, err := s.store.ServiceType().Create(ctx, storeModel)
	if err != nil {
		s.logger.ErrorContext(ctx, "Failed to create service type in store", "id", id, "error", err)
		return nil, mapStoreError(err)
	}

	s.logger.InfoContext(ctx, "Service type created", "id", id, "service_type", req.ServiceType)
	// Convert result back to API type
	apiType := toAPIType(createdModel)
	return &apiType, nil
}

// Get retrieves a service type by ID
func (s *serviceTypeService) Get(ctx context.Context, id string) (*v1alpha1.ServiceType, error) {
	// Call store layer
	storeModel, err := s.store.ServiceType().Get(ctx, id)
	if err != nil {
		return nil, mapStoreError(err)
	}

	// Convert to API type
	apiType := toAPIType(storeModel)
	return &apiType, nil
}
