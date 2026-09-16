package v1alpha1

import (
	"errors"
	"fmt"

	v1alpha1 "github.com/dcm-project/control-plane/api/catalog/v1alpha1"
	"github.com/dcm-project/control-plane/internal/catalog/api/server"
	"github.com/dcm-project/control-plane/internal/catalog/service"
)

// ErrInvalidCatalogItemInstanceAPIVersion indicates the api_version is invalid
var ErrInvalidCatalogItemInstanceAPIVersion = fmt.Errorf("invalid api_version: must be set to %s", supportedAPIVersion)

// mapCreateCatalogItemInstanceErrorToHTTP converts service domain errors to CreateCatalogItemInstance HTTP responses
func mapCreateCatalogItemInstanceErrorToHTTP(err error) server.CreateCatalogItemInstanceResponseObject {
	switch {
	case errors.Is(err, service.ErrCatalogItemInstanceIDTaken):
		return server.CreateCatalogItemInstance409JSONResponse{
			AlreadyExistsJSONResponse: server.AlreadyExistsJSONResponse{
				Type:   v1alpha1.ALREADYEXISTS,
				Status: 409,
				Title:  "Conflict",
				Detail: stringPtr(err.Error()),
			},
		}
	case errors.Is(err, service.ErrCatalogItemNotFoundForInstance),
		errors.Is(err, service.ErrCatalogItemSpecConflict),
		errors.Is(err, service.ErrUserValuePathNotFound),
		errors.Is(err, service.ErrUserValueNotEditable),
		errors.Is(err, service.ErrUserValueValidationFailed),
		errors.Is(err, service.ErrUserValueDependsOnViolation),
		errors.Is(err, service.ErrUserValueResourceRequired),
		errors.Is(err, service.ErrUserValueResourceNotFound),
		errors.Is(err, service.ErrInvalidCELExpression),
		errors.Is(err, service.ErrCELResourceNotFound),
		errors.Is(err, service.ErrCELSelfReference),
		errors.Is(err, service.ErrCELServiceTypeOutputNotFound),
		errors.Is(err, service.ErrCELRequiresResourceMissing):
		return server.CreateCatalogItemInstance400JSONResponse(v1alpha1.Error{
			Type:   v1alpha1.INVALIDARGUMENT,
			Status: 400,
			Title:  "Bad Request",
			Detail: stringPtr(err.Error()),
		})
	case errors.Is(err, service.ErrPlacementManagerPolicyRejected):
		return server.CreateCatalogItemInstance406JSONResponse{
			PolicyRejectedJSONResponse: server.PolicyRejectedJSONResponse{
				Type:   v1alpha1.FAILEDPRECONDITION,
				Status: 406,
				Title:  "Policy Rejected",
				Detail: stringPtr(err.Error()),
			},
		}
	case errors.Is(err, service.ErrPlacementManagerProviderError):
		return server.CreateCatalogItemInstance422JSONResponse{
			ProviderErrorJSONResponse: server.ProviderErrorJSONResponse{
				Type:   v1alpha1.FAILEDPRECONDITION,
				Status: 422,
				Title:  "Provider Error",
				Detail: stringPtr(err.Error()),
			},
		}
	case errors.Is(err, service.ErrPlacementManagerPolicyDependency):
		return server.CreateCatalogItemInstance424JSONResponse{
			PolicyDependencyJSONResponse: server.PolicyDependencyJSONResponse{
				Type:   v1alpha1.FAILEDPRECONDITION,
				Status: 424,
				Title:  "Policy Dependency",
				Detail: stringPtr(err.Error()),
			},
		}
	case errors.Is(err, service.ErrPlacementManagerCreateFailed):
		return server.CreateCatalogItemInstance500JSONResponse{
			InternalServerErrorJSONResponse: server.InternalServerErrorJSONResponse{
				Type:   v1alpha1.INTERNAL,
				Status: 500,
				Title:  "Placement Manager Error",
				Detail: stringPtr(err.Error()),
			},
		}
	default:
		return server.CreateCatalogItemInstance500JSONResponse{
			InternalServerErrorJSONResponse: server.InternalServerErrorJSONResponse{
				Type:   v1alpha1.INTERNAL,
				Status: 500,
				Title:  "Internal Server Error",
				Detail: stringPtr(err.Error()),
			},
		}
	}
}

// mapRehydrateCatalogItemInstanceErrorToHTTP converts service domain errors to RehydrateCatalogItemInstance HTTP responses
func mapRehydrateCatalogItemInstanceErrorToHTTP(err error) server.RehydrateCatalogItemInstanceResponseObject {
	switch {
	case errors.Is(err, service.ErrCatalogItemInstanceNotFound):
		return server.RehydrateCatalogItemInstance404JSONResponse{
			NotFoundJSONResponse: server.NotFoundJSONResponse{
				Type:   v1alpha1.NOTFOUND,
				Status: 404,
				Title:  "Not Found",
				Detail: stringPtr(err.Error()),
			},
		}
	case errors.Is(err, service.ErrCatalogItemInstanceConflict):
		return server.RehydrateCatalogItemInstance409JSONResponse{
			AlreadyExistsJSONResponse: server.AlreadyExistsJSONResponse{
				Type:   v1alpha1.ALREADYEXISTS,
				Status: 409,
				Title:  "Conflict",
				Detail: stringPtr("this instance was modified by another request; please retry"),
			},
		}
	case errors.Is(err, service.ErrCatalogItemInstanceRunIDEmpty):
		return server.RehydrateCatalogItemInstance422JSONResponse{
			ProviderErrorJSONResponse: server.ProviderErrorJSONResponse{
				Type:   v1alpha1.FAILEDPRECONDITION,
				Status: 422,
				Title:  "Failed Precondition",
				Detail: stringPtr(err.Error()),
			},
		}
	case errors.Is(err, service.ErrPlacementManagerPolicyRejected):
		return server.RehydrateCatalogItemInstance406JSONResponse{
			PolicyRejectedJSONResponse: server.PolicyRejectedJSONResponse{
				Type:   v1alpha1.FAILEDPRECONDITION,
				Status: 406,
				Title:  "Policy Rejected",
				Detail: stringPtr(err.Error()),
			},
		}
	case errors.Is(err, service.ErrPlacementManagerProviderError):
		return server.RehydrateCatalogItemInstance422JSONResponse{
			ProviderErrorJSONResponse: server.ProviderErrorJSONResponse{
				Type:   v1alpha1.FAILEDPRECONDITION,
				Status: 422,
				Title:  "Provider Error",
				Detail: stringPtr(err.Error()),
			},
		}
	case errors.Is(err, service.ErrPlacementManagerPolicyDependency):
		return server.RehydrateCatalogItemInstance424JSONResponse{
			PolicyDependencyJSONResponse: server.PolicyDependencyJSONResponse{
				Type:   v1alpha1.FAILEDPRECONDITION,
				Status: 424,
				Title:  "Policy Dependency",
				Detail: stringPtr(err.Error()),
			},
		}
	case errors.Is(err, service.ErrPlacementManagerRehydrateFailed):
		return server.RehydrateCatalogItemInstance500JSONResponse{
			InternalServerErrorJSONResponse: server.InternalServerErrorJSONResponse{
				Type:   v1alpha1.INTERNAL,
				Status: 500,
				Title:  "Placement Manager Error",
				Detail: stringPtr(err.Error()),
			},
		}
	default:
		return server.RehydrateCatalogItemInstance500JSONResponse{
			InternalServerErrorJSONResponse: server.InternalServerErrorJSONResponse{
				Type:   v1alpha1.INTERNAL,
				Status: 500,
				Title:  "Internal Server Error",
				Detail: stringPtr(err.Error()),
			},
		}
	}
}

// mapGetCatalogItemInstanceErrorToHTTP converts service domain errors to GetCatalogItemInstance HTTP responses
func mapGetCatalogItemInstanceErrorToHTTP(err error) server.GetCatalogItemInstanceResponseObject {
	switch {
	case errors.Is(err, service.ErrCatalogItemInstanceNotFound):
		return server.GetCatalogItemInstance404JSONResponse{
			NotFoundJSONResponse: server.NotFoundJSONResponse{
				Type:   v1alpha1.NOTFOUND,
				Status: 404,
				Title:  "Not Found",
				Detail: stringPtr(err.Error()),
			},
		}
	default:
		return server.GetCatalogItemInstance500JSONResponse{
			InternalServerErrorJSONResponse: server.InternalServerErrorJSONResponse{
				Type:   v1alpha1.INTERNAL,
				Status: 500,
				Title:  "Internal Server Error",
				Detail: stringPtr(err.Error()),
			},
		}
	}
}

// mapDeleteCatalogItemInstanceErrorToHTTP converts service domain errors to DeleteCatalogItemInstance HTTP responses
func mapDeleteCatalogItemInstanceErrorToHTTP(err error) server.DeleteCatalogItemInstanceResponseObject {
	switch {
	case errors.Is(err, service.ErrCatalogItemInstanceNotFound):
		return server.DeleteCatalogItemInstance404JSONResponse{
			NotFoundJSONResponse: server.NotFoundJSONResponse{
				Type:   v1alpha1.NOTFOUND,
				Status: 404,
				Title:  "Not Found",
				Detail: stringPtr(err.Error()),
			},
		}
	case errors.Is(err, service.ErrPlacementManagerDeleteFailed):
		return server.DeleteCatalogItemInstance500JSONResponse{
			InternalServerErrorJSONResponse: server.InternalServerErrorJSONResponse{
				Type:   v1alpha1.INTERNAL,
				Status: 500,
				Title:  "Placement Manager Error",
				Detail: stringPtr(err.Error()),
			},
		}
	default:
		return server.DeleteCatalogItemInstance500JSONResponse{
			InternalServerErrorJSONResponse: server.InternalServerErrorJSONResponse{
				Type:   v1alpha1.INTERNAL,
				Status: 500,
				Title:  "Internal Server Error",
				Detail: stringPtr(err.Error()),
			},
		}
	}
}
