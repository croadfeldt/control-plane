// Package v1alpha1 implements the v1alpha1 API request handlers.
package v1alpha1

import (
	"context"
	"errors"
	"log/slog"

	"github.com/dcm-project/control-plane/internal/catalog/api/server"
	"github.com/dcm-project/control-plane/internal/catalog/service"
)

type Handler struct {
	service service.Service
	logger  *slog.Logger
}

func NewHandler(svc service.Service, logger *slog.Logger) *Handler {
	return &Handler{
		service: svc,
		logger:  logger.With("component", "handler"),
	}
}

// Compile-time verification
var _ server.StrictServerInterface = (*Handler)(nil)

// stringPtr returns a pointer to the given string
func stringPtr(s string) *string {
	return &s
}

// clientErrors are known domain errors that map to 4xx HTTP responses.
// Errors not in this list are treated as internal (5xx) failures.
// A slice + errors.Is loop is required because service-layer errors may be
// wrapped (via fmt.Errorf %w), so a direct map lookup would not match.
var clientErrors = []error{
	service.ErrInvalidServiceType,
	service.ErrServiceTypeIDTaken,
	service.ErrServiceTypeNameTaken,
	service.ErrServiceTypeNotFound,
	service.ErrCatalogItemNotFound,
	service.ErrCatalogItemIDTaken,
	service.ErrCatalogItemHasInstances,
	service.ErrImmutableSpecStructureUpdate,
	service.ErrCatalogItemInstanceNotFound,
	service.ErrCatalogItemInstanceIDTaken,
	service.ErrCatalogItemInstanceRunIDEmpty,
	service.ErrCatalogItemNotFoundForInstance,
	service.ErrUserValuePathNotFound,
	service.ErrUserValueNotEditable,
	service.ErrUserValueValidationFailed,
	service.ErrDependsOnCycleDetected,
	service.ErrDependsOnPathNotFound,
	service.ErrCatalogItemSpecConflict,
	service.ErrCatalogItemResourceNameTaken,
	service.ErrCatalogItemRequiresResourceNotFound,
	service.ErrCatalogItemRequiresCycle,
	service.ErrUserValueResourceRequired,
	service.ErrUserValueResourceNotFound,
	service.ErrUserValueDependsOnViolation,
	service.ErrInvalidCELExpression,
	service.ErrCELResourceNotFound,
	service.ErrCELSelfReference,
	service.ErrCELServiceTypeOutputNotFound,
	service.ErrCELRequiresResourceMissing,
	service.ErrPlacementManagerPolicyRejected,
	service.ErrPlacementManagerProviderError,
	service.ErrPlacementManagerPolicyDependency,
}

// logServiceError logs at Warn for expected client errors (4xx) and Error for
// internal failures (5xx), aligning log severity with HTTP response semantics.
func (h *Handler) logServiceError(ctx context.Context, msg string, err error, attrs ...any) {
	args := append([]any{"error", err}, attrs...)
	for _, ce := range clientErrors {
		if errors.Is(err, ce) {
			h.logger.WarnContext(ctx, msg, args...)
			return
		}
	}
	h.logger.ErrorContext(ctx, msg, args...)
}
