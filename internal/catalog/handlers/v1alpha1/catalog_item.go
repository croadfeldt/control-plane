package v1alpha1

import (
	"context"

	"github.com/dcm-project/control-plane/api/catalog/v1alpha1"
	"github.com/dcm-project/control-plane/internal/catalog/api/server"
	"github.com/dcm-project/control-plane/internal/catalog/service"
)

const (
	supportedAPIVersion = "v1alpha1"
)

func (h *Handler) ListCatalogItems(ctx context.Context, request server.ListCatalogItemsRequestObject) (server.ListCatalogItemsResponseObject, error) {
	h.logger.DebugContext(ctx, "Listing catalog items")

	// Build service request from HTTP params
	opts := service.CatalogItemListOptions{
		PageToken:   request.Params.PageToken,
		MaxPageSize: request.Params.MaxPageSize,
		ServiceType: request.Params.ServiceType,
		FiledUnder:  request.Params.FiledUnder,
	}

	// Call service layer
	result, err := h.service.CatalogItem().List(ctx, opts)
	if err != nil {
		if isInvalidPageToken(err) {
			h.logger.WarnContext(ctx, "Invalid page_token", "error", err)
			return server.ListCatalogItems400JSONResponse{
				BadRequestJSONResponse: invalidPageTokenBadRequest(err),
			}, nil
		}
		h.logger.ErrorContext(ctx, "Failed to list catalog items", "error", err)
		return server.ListCatalogItems500JSONResponse{
			InternalServerErrorJSONResponse: server.InternalServerErrorJSONResponse{
				Type:   v1alpha1.INTERNAL,
				Status: 500,
				Title:  "Internal Server Error",
				Detail: stringPtr(err.Error()),
			},
		}, nil
	}

	h.logger.DebugContext(ctx, "Listed catalog items", "count", len(result.CatalogItems))

	// Return HTTP response
	response := server.ListCatalogItems200JSONResponse(v1alpha1.CatalogItemList{
		Results: result.CatalogItems,
	})
	if result.NextPageToken != nil {
		response.NextPageToken = *result.NextPageToken
	}
	return response, nil
}

func (h *Handler) CreateCatalogItem(ctx context.Context, request server.CreateCatalogItemRequestObject) (server.CreateCatalogItemResponseObject, error) {
	h.logger.InfoContext(ctx, "Creating catalog item", "id", request.Params.Id)

	// Build service request from HTTP params
	req, err := validateAndBuildCreateCatalogItemRequest(request)
	if err != nil {
		h.logger.WarnContext(ctx, "Invalid create catalog item request", "error", err)
		return server.CreateCatalogItem400JSONResponse(v1alpha1.Error{
			Type:   v1alpha1.INVALIDARGUMENT,
			Status: 400,
			Title:  "Bad Request",
			Detail: stringPtr(err.Error()),
		}), nil
	}

	// Call service layer
	result, err := h.service.CatalogItem().Create(ctx, req)
	if err != nil {
		h.logServiceError(ctx, "Failed to create catalog item", err)
		return mapCreateCatalogItemErrorToHTTP(err), nil
	}

	h.logger.InfoContext(ctx, "Created catalog item", "id", request.Params.Id)

	// Return HTTP response
	return server.CreateCatalogItem201JSONResponse(*result), nil
}

func validateAndBuildCreateCatalogItemRequest(request server.CreateCatalogItemRequestObject) (*service.CreateCatalogItemRequest, error) {
	if request.Body.ApiVersion == nil || *request.Body.ApiVersion != supportedAPIVersion {
		return nil, ErrInvalidAPIVersion
	}
	if request.Body.DisplayName == nil || *request.Body.DisplayName == "" {
		return nil, ErrInvalidDisplayName
	}
	if request.Body.Spec == nil {
		return nil, ErrEmptySpec
	}
	if len(request.Body.Spec.Resources) == 0 {
		return nil, ErrEmptyResources
	}
	return &service.CreateCatalogItemRequest{
		ID:          request.Params.Id,
		ApiVersion:  *request.Body.ApiVersion,
		DisplayName: *request.Body.DisplayName,
		Spec:        *request.Body.Spec,
	}, nil
}

func (h *Handler) GetCatalogItem(ctx context.Context, request server.GetCatalogItemRequestObject) (server.GetCatalogItemResponseObject, error) {
	h.logger.DebugContext(ctx, "Getting catalog item", "id", request.CatalogItemId)

	// Call service layer
	result, err := h.service.CatalogItem().Get(ctx, request.CatalogItemId)
	if err != nil {
		h.logServiceError(ctx, "Failed to get catalog item", err, "id", request.CatalogItemId)
		return mapGetCatalogItemErrorToHTTP(err), nil
	}

	// Return HTTP response
	return server.GetCatalogItem200JSONResponse(*result), nil
}

func (h *Handler) UpdateCatalogItem(ctx context.Context, request server.UpdateCatalogItemRequestObject) (server.UpdateCatalogItemResponseObject, error) {
	h.logger.InfoContext(ctx, "Updating catalog item", "id", request.CatalogItemId)

	// Body is already a CatalogItem (partial update via JSON merge patch)
	updateReq := &service.UpdateCatalogItemRequest{
		DisplayName: request.Body.DisplayName,
		Spec:        request.Body.Spec,
	}

	// Call service layer
	result, err := h.service.CatalogItem().Update(ctx, request.CatalogItemId, updateReq)
	if err != nil {
		h.logServiceError(ctx, "Failed to update catalog item", err, "id", request.CatalogItemId)
		return mapUpdateCatalogItemErrorToHTTP(err), nil
	}

	h.logger.InfoContext(ctx, "Updated catalog item", "id", request.CatalogItemId)

	// Return HTTP response
	return server.UpdateCatalogItem200JSONResponse(*result), nil
}

func (h *Handler) DeleteCatalogItem(ctx context.Context, request server.DeleteCatalogItemRequestObject) (server.DeleteCatalogItemResponseObject, error) {
	h.logger.InfoContext(ctx, "Deleting catalog item", "id", request.CatalogItemId)

	// Call service layer
	err := h.service.CatalogItem().Delete(ctx, request.CatalogItemId)
	if err != nil {
		h.logServiceError(ctx, "Failed to delete catalog item", err, "id", request.CatalogItemId)
		return mapDeleteCatalogItemErrorToHTTP(err), nil
	}

	h.logger.InfoContext(ctx, "Deleted catalog item", "id", request.CatalogItemId)

	// Return HTTP 204 No Content response
	return server.DeleteCatalogItem204Response{}, nil
}
