package sprm

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	sprmv1alpha1 "github.com/dcm-project/control-plane/api/sp/v1alpha1/resource_manager"
	spservice "github.com/dcm-project/control-plane/internal/sp/service"
	rmsvc "github.com/dcm-project/control-plane/internal/sp/service/resource_manager"
)

type serviceClient struct {
	instances *rmsvc.InstanceService
}

// NewServiceClient adapts InstanceService for in-process use by PlacementService.
func NewServiceClient(instances *rmsvc.InstanceService) Client {
	return &serviceClient{instances: instances}
}

func (c *serviceClient) CreateResource(ctx context.Context, req CreateResourceRequest) (*CreateResourceResponse, error) {
	body := sprmv1alpha1.ServiceTypeInstance{
		Spec: req.Spec,
	}
	queryID := req.ID

	instance, err := c.instances.CreateInstance(ctx, &body, &queryID, req.AgentName)
	if err != nil {
		return nil, mapInstanceError(err)
	}

	resp := &CreateResourceResponse{}
	if instance.Id != nil {
		resp.ID = *instance.Id
	}
	if instance.Status != nil {
		resp.Status = *instance.Status
	}
	return resp, nil
}

func (c *serviceClient) GetOutputSpec(ctx context.Context, resourceID string) (*GetOutputSpecResponse, error) {
	outputSpec, err := c.instances.GetOutputSpec(ctx, resourceID)
	if err != nil {
		return nil, mapInstanceError(err)
	}
	return &GetOutputSpecResponse{OutputSpec: outputSpec}, nil
}

func (c *serviceClient) DeleteResource(ctx context.Context, resourceID string) error {
	return c.deleteResource(ctx, resourceID, false)
}

func (c *serviceClient) DeleteResourceDeferred(ctx context.Context, resourceID string) error {
	return c.deleteResource(ctx, resourceID, true)
}

func (c *serviceClient) deleteResource(ctx context.Context, resourceID string, deferred bool) error {
	if err := c.instances.DeleteInstance(ctx, resourceID, deferred); err != nil {
		return mapInstanceError(err)
	}
	return nil
}

func (c *serviceClient) ReassignResource(ctx context.Context, resourceID string, agentName string, expectedCurrentAgent string) error {
	if err := c.instances.ReassignAgent(ctx, resourceID, agentName, expectedCurrentAgent); err != nil {
		return mapInstanceError(err)
	}
	return nil
}

func mapInstanceError(err error) error {
	var svcErr *spservice.ServiceError
	if !errors.As(err, &svcErr) {
		return fmt.Errorf("sprm service error: %w", err)
	}

	status := http.StatusInternalServerError
	switch svcErr.Code {
	case spservice.ErrCodeValidation:
		status = http.StatusBadRequest
	case spservice.ErrCodeNotFound:
		status = http.StatusNotFound
	case spservice.ErrCodeConflict:
		status = http.StatusConflict
	case spservice.ErrCodeProvisioningError:
		status = http.StatusUnprocessableEntity
	case spservice.ErrCodeUnavailable:
		status = http.StatusServiceUnavailable
	}

	// 4xx bodies are client-facing validation detail and safe to return
	// verbatim. 5xx bodies may contain internal error strings (DB, NATS) -
	// log them server-side and return a generic message to the caller.
	body := svcErr.Message
	if status >= http.StatusInternalServerError {
		slog.Error("sprm adapter error", "status", status, "detail", svcErr.Message)
		if status == http.StatusServiceUnavailable {
			body = "service temporarily unavailable"
		} else {
			body = "internal server error"
		}
	}

	return &HTTPError{StatusCode: status, Body: body}
}
