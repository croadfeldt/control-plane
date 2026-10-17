package v1alpha1

import (
	"errors"
	"fmt"

	v1alpha1 "github.com/dcm-project/control-plane/api/catalog/v1alpha1"
	"github.com/dcm-project/control-plane/internal/catalog/api/server"
	"github.com/dcm-project/control-plane/internal/catalog/service"
)

var (
	// ErrInvalidAPIVersion indicates the api_version is invalid (must be in the format v1alpha1)
	ErrInvalidAPIVersion = fmt.Errorf("invalid api_version: must be set to %s", supportedAPIVersion)

	// ErrInvalidDisplayName indicates the display_name is invalid (empty or exceeds 63 characters)
	ErrInvalidDisplayName = errors.New("invalid display_name: must not be non-empty")

	// ErrEmptySpec indicates the spec is empty (must have at least one field)
	ErrEmptySpec = errors.New("spec cannot be empty: must have at least one field")

	// ErrEmptyResources indicates the spec.resources array is empty (must have at least 1 resource)
	ErrEmptyResources = errors.New("spec.resources cannot be empty: must have at least one resource")
)

// mapCreateCatalogItemErrorToHTTP converts service domain errors to CreateCatalogItem HTTP responses
func mapCreateCatalogItemErrorToHTTP(err error) server.CreateCatalogItemResponseObject {
	switch {
	case errors.Is(err, service.ErrCatalogItemIDTaken):
		// Conflict errors -> 409 Conflict
		return server.CreateCatalogItem409JSONResponse{
			AlreadyExistsJSONResponse: server.AlreadyExistsJSONResponse{
				Type:   v1alpha1.ALREADYEXISTS,
				Status: 409,
				Title:  "Conflict",
				Detail: stringPtr(err.Error()),
			},
		}
	case errors.Is(err, service.ErrServiceTypeNotFound),
		errors.Is(err, service.ErrCatalogItemSpecConflict),
		errors.Is(err, service.ErrDependsOnCycleDetected),
		errors.Is(err, service.ErrDependsOnPathNotFound),
		errors.Is(err, service.ErrCatalogItemResourceNameTaken),
		errors.Is(err, service.ErrCatalogItemFieldPathTaken),
		errors.Is(err, service.ErrCatalogItemRequiresResourceNotFound),
		errors.Is(err, service.ErrCatalogItemRequiresCycle):
		// Validation errors -> 400 Bad Request
		return server.CreateCatalogItem400JSONResponse(v1alpha1.Error{
			Type:   v1alpha1.INVALIDARGUMENT,
			Status: 400,
			Title:  "Bad Request",
			Detail: stringPtr(err.Error()),
		})
	default:
		// Unknown errors -> 500 Internal Server Error
		return server.CreateCatalogItem500JSONResponse{
			InternalServerErrorJSONResponse: server.InternalServerErrorJSONResponse{
				Type:   v1alpha1.INTERNAL,
				Status: 500,
				Title:  "Internal Server Error",
				Detail: stringPtr(err.Error()),
			},
		}
	}
}

// mapGetCatalogItemErrorToHTTP converts service domain errors to GetCatalogItem HTTP responses
func mapGetCatalogItemErrorToHTTP(err error) server.GetCatalogItemResponseObject {
	switch {
	case errors.Is(err, service.ErrCatalogItemNotFound):
		// Not found -> 404 Not Found
		return server.GetCatalogItem404JSONResponse{
			NotFoundJSONResponse: server.NotFoundJSONResponse{
				Type:   v1alpha1.NOTFOUND,
				Status: 404,
				Title:  "Not Found",
				Detail: stringPtr(err.Error()),
			},
		}
	default:
		// Unknown errors -> 500 Internal Server Error
		return server.GetCatalogItem500JSONResponse{
			InternalServerErrorJSONResponse: server.InternalServerErrorJSONResponse{
				Type:   v1alpha1.INTERNAL,
				Status: 500,
				Title:  "Internal Server Error",
				Detail: stringPtr(err.Error()),
			},
		}
	}
}

// mapUpdateCatalogItemErrorToHTTP converts service domain errors to UpdateCatalogItem HTTP responses
func mapUpdateCatalogItemErrorToHTTP(err error) server.UpdateCatalogItemResponseObject {
	switch {
	case errors.Is(err, service.ErrImmutableSpecStructureUpdate),
		errors.Is(err, service.ErrCatalogItemSpecConflict),
		errors.Is(err, service.ErrDependsOnCycleDetected),
		errors.Is(err, service.ErrDependsOnPathNotFound),
		errors.Is(err, service.ErrCatalogItemResourceNameTaken),
		errors.Is(err, service.ErrCatalogItemFieldPathTaken),
		errors.Is(err, service.ErrCatalogItemRequiresResourceNotFound),
		errors.Is(err, service.ErrCatalogItemRequiresCycle):
		// Validation errors -> 400 Bad Request
		return server.UpdateCatalogItem400JSONResponse(v1alpha1.Error{
			Type:   v1alpha1.INVALIDARGUMENT,
			Status: 400,
			Title:  "Bad Request",
			Detail: stringPtr(err.Error()),
		})
	case errors.Is(err, service.ErrCatalogItemNotFound):
		// Not found -> 404 Not Found
		return server.UpdateCatalogItem404JSONResponse{
			NotFoundJSONResponse: server.NotFoundJSONResponse{
				Type:   v1alpha1.NOTFOUND,
				Status: 404,
				Title:  "Not Found",
				Detail: stringPtr(err.Error()),
			},
		}
	default:
		// Unknown errors -> 500 Internal Server Error
		return server.UpdateCatalogItem500JSONResponse{
			InternalServerErrorJSONResponse: server.InternalServerErrorJSONResponse{
				Type:   v1alpha1.INTERNAL,
				Status: 500,
				Title:  "Internal Server Error",
				Detail: stringPtr(err.Error()),
			},
		}
	}
}

// mapDeleteCatalogItemErrorToHTTP converts service domain errors to DeleteCatalogItem HTTP responses
func mapDeleteCatalogItemErrorToHTTP(err error) server.DeleteCatalogItemResponseObject {
	switch {
	case errors.Is(err, service.ErrCatalogItemNotFound):
		// Not found -> 404 Not Found
		return server.DeleteCatalogItem404JSONResponse{
			NotFoundJSONResponse: server.NotFoundJSONResponse{
				Type:   v1alpha1.NOTFOUND,
				Status: 404,
				Title:  "Not Found",
				Detail: stringPtr(err.Error()),
			},
		}
	case errors.Is(err, service.ErrCatalogItemHasInstances):
		// Has instances -> 409 Conflict
		return server.DeleteCatalogItem409JSONResponse{
			HasInstancesJSONResponse: server.HasInstancesJSONResponse{
				Type:   v1alpha1.FAILEDPRECONDITION,
				Status: 409,
				Title:  "Failed Precondition",
				Detail: stringPtr(err.Error()),
			},
		}
	default:
		// Unknown errors -> 500 Internal Server Error
		return server.DeleteCatalogItem500JSONResponse{
			InternalServerErrorJSONResponse: server.InternalServerErrorJSONResponse{
				Type:   v1alpha1.INTERNAL,
				Status: 500,
				Title:  "Internal Server Error",
				Detail: stringPtr(err.Error()),
			},
		}
	}
}
