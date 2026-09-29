package v1alpha1

import (
	"context"

	v1alpha1 "github.com/dcm-project/control-plane/api/catalog/v1alpha1"
	"github.com/dcm-project/control-plane/internal/catalog/api/server"
	"github.com/dcm-project/control-plane/internal/catalog/service"
)

func (h *Handler) ListServiceTypes(ctx context.Context, request server.ListServiceTypesRequestObject) (server.ListServiceTypesResponseObject, error) {
	h.logger.DebugContext(ctx, "Listing service types")

	// Build service request from HTTP params
	opts := &service.ServiceTypeListOptions{
		PageToken:   request.Params.PageToken,
		MaxPageSize: request.Params.MaxPageSize,
		FiledUnder:  request.Params.FiledUnder,
	}

	// Call service layer
	result, err := h.service.ServiceType().List(ctx, opts)
	if err != nil {
		if isInvalidPageToken(err) {
			h.logger.WarnContext(ctx, "Invalid page_token", "error", err)
			return server.ListServiceTypes400JSONResponse{
				BadRequestJSONResponse: invalidPageTokenBadRequest(err),
			}, nil
		}
		h.logger.ErrorContext(ctx, "Failed to list service types", "error", err)
		return server.ListServiceTypes500JSONResponse{
			InternalServerErrorJSONResponse: server.InternalServerErrorJSONResponse{
				Type:   v1alpha1.INTERNAL,
				Status: 500,
				Title:  "Internal Server Error",
				Detail: stringPtr(err.Error()),
			},
		}, nil
	}

	h.logger.DebugContext(ctx, "Listed service types", "count", len(result.ServiceTypes))

	// Return HTTP response
	response := server.ListServiceTypes200JSONResponse(v1alpha1.ServiceTypeList{
		Results: result.ServiceTypes,
	})
	if result.NextPageToken != nil {
		response.NextPageToken = *result.NextPageToken
	}

	return response, nil
}

func (h *Handler) CreateServiceType(ctx context.Context, request server.CreateServiceTypeRequestObject) (server.CreateServiceTypeResponseObject, error) {
	h.logger.InfoContext(ctx, "Creating service type",
		"id", request.Params.Id,
		"service_type", request.Body.ServiceType,
	)

	// Build service request from HTTP params
	req := &service.CreateServiceTypeRequest{
		ID:          request.Params.Id,
		ApiVersion:  request.Body.ApiVersion,
		ServiceType: request.Body.ServiceType,
		Metadata:    request.Body.Metadata,
		Spec:        request.Body.Spec,
	}

	// Call service layer
	result, err := h.service.ServiceType().Create(ctx, req)
	if err != nil {
		h.logServiceError(ctx, "Failed to create service type", err, "service_type", request.Body.ServiceType)
		return mapCreateServiceErrorToHTTP(err), nil
	}

	h.logger.InfoContext(ctx, "Created service type", "service_type", result.ServiceType)

	// Return HTTP response
	return server.CreateServiceType201JSONResponse(*result), nil
}

func (h *Handler) GetServiceType(ctx context.Context, request server.GetServiceTypeRequestObject) (server.GetServiceTypeResponseObject, error) {
	h.logger.DebugContext(ctx, "Getting service type", "id", request.ServiceTypeId)

	// Call service layer
	result, err := h.service.ServiceType().Get(ctx, request.ServiceTypeId)
	if err != nil {
		h.logServiceError(ctx, "Failed to get service type", err, "id", request.ServiceTypeId)
		return mapGetServiceErrorToHTTP(err), nil
	}

	// Return HTTP response
	return server.GetServiceType200JSONResponse(*result), nil
}
