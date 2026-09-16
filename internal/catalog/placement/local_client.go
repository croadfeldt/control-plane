package placement

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/dcm-project/control-plane/internal/placement/service"
	"github.com/dcm-project/control-plane/internal/placement/types"
)

type localClient struct {
	svc    *service.PlacementService
	logger *slog.Logger
}

// NewLocalClient creates an in-process placement client backed by PlacementService.
func NewLocalClient(svc *service.PlacementService, logger *slog.Logger) Client {
	return &localClient{svc: svc, logger: logger.With("component", "placement-local")}
}

func (c *localClient) CreateRun(ctx context.Context, req CreateRunRequest) (*Run, error) {
	c.logger.InfoContext(ctx, "Creating run in placement (in-process)",
		"catalog_item_instance_id", req.CatalogItemInstanceId,
		"resource_count", len(req.Resources),
	)

	result, err := c.svc.CreateRun(ctx, &req)
	if err != nil {
		return nil, mapPlacementServiceError(err)
	}
	return mapAPIRun(result), nil
}

func (c *localClient) DeleteRun(ctx context.Context, runID string) error {
	c.logger.InfoContext(ctx, "Deleting run in placement (in-process)", "run_id", runID)
	if err := c.svc.DeleteRun(ctx, runID); err != nil {
		return mapPlacementServiceError(err)
	}
	return nil
}

func (c *localClient) RehydrateResource(ctx context.Context, runID, newRunID string) (*Resource, error) {
	c.logger.InfoContext(ctx, "Rehydrating resource in placement (in-process)",
		"run_id", runID,
		"new_run_id", newRunID,
	)
	result, err := c.svc.RehydrateResource(ctx, runID, newRunID)
	if err != nil {
		return nil, mapPlacementServiceError(err)
	}
	return mapAPIResource(result), nil
}

func mapAPIRun(r *types.Run) *Run {
	if r == nil {
		return nil
	}
	out := &Run{
		RunID:                 r.RunId,
		CatalogItemInstanceID: r.CatalogItemInstanceId,
		Resources:             make([]Resource, 0, len(r.Resources)),
	}
	for i := range r.Resources {
		out.Resources = append(out.Resources, *mapAPIResource(&r.Resources[i]))
	}
	return out
}

func mapAPIResource(r *types.Resource) *Resource {
	res := &Resource{Spec: r.Spec, Name: r.Name}
	if r.Id != nil {
		res.ID = *r.Id
	}
	if r.Path != nil {
		res.Path = *r.Path
	}
	return res
}

func mapPlacementServiceError(err error) error {
	var svcErr *service.ServiceError
	if !errors.As(err, &svcErr) {
		return fmt.Errorf("placement service error: %w", err)
	}

	status := http.StatusInternalServerError
	switch svcErr.Code {
	case service.ErrCodeValidation:
		status = http.StatusBadRequest
	case service.ErrCodePolicyRejected:
		status = http.StatusNotAcceptable
	case service.ErrCodeConflict, service.ErrCodePolicyConflict:
		status = http.StatusConflict
	case service.ErrCodeProvisioningError:
		status = http.StatusUnprocessableEntity
	case service.ErrCodeNotFound:
		status = http.StatusNotFound
	case service.ErrCodePolicyError, service.ErrCodePolicyInternalError:
		status = http.StatusFailedDependency
	case service.ErrCodeUnavailable:
		status = http.StatusServiceUnavailable
	}

	// 5xx bodies may carry internal error detail from downstream services -
	// log server-side and return a generic message to the API caller.
	body := svcErr.Message
	if status >= http.StatusInternalServerError {
		slog.Error("placement local client error", "status", status, "detail", svcErr.Message)
		if status == http.StatusServiceUnavailable {
			body = "service temporarily unavailable"
		} else {
			body = "internal server error"
		}
	}

	return &PlacementError{StatusCode: status, Body: body}
}
