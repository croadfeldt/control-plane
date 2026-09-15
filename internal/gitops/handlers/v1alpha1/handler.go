// Package v1alpha1 handles v1alpha1 API requests for GitRepository CRUD operations.
package v1alpha1

import (
	"context"
	"log/slog"

	v1alpha1 "github.com/dcm-project/control-plane/api/gitops/v1alpha1"
	"github.com/dcm-project/control-plane/internal/gitops/api/server"
	"github.com/dcm-project/control-plane/internal/gitops/service"
)

type Handler struct {
	service service.GitRepositoryService
}

// Ensure Handler implements StrictServerInterface
var _ server.StrictServerInterface = (*Handler)(nil)

func NewHandler(svc service.GitRepositoryService) *Handler {
	return &Handler{service: svc}
}

func (h *Handler) ListGitRepositories(ctx context.Context, request server.ListGitRepositoriesRequestObject) (server.ListGitRepositoriesResponseObject, error) {
	result, err := h.service.List(ctx, request.Params.PageToken, request.Params.MaxPageSize)
	if err != nil {
		return handleListError(err), nil
	}
	return server.ListGitRepositories200JSONResponse(*result), nil
}

func (h *Handler) CreateGitRepository(ctx context.Context, request server.CreateGitRepositoryRequestObject) (server.CreateGitRepositoryResponseObject, error) {
	if request.Body == nil {
		return server.CreateGitRepository400JSONResponse(buildError(
			400, v1alpha1.INVALIDARGUMENT, "Invalid request body", "Request body is required",
		)), nil
	}

	created, err := h.service.Create(ctx, *request.Body, request.Params.Id)
	if err != nil {
		return handleCreateError(err), nil
	}

	slog.InfoContext(ctx, "GitRepository created", "id", *created.Uid)
	return server.CreateGitRepository201JSONResponse(*created), nil
}

func (h *Handler) GetGitRepository(ctx context.Context, request server.GetGitRepositoryRequestObject) (server.GetGitRepositoryResponseObject, error) {
	repo, err := h.service.Get(ctx, request.GitRepositoryId)
	if err != nil {
		return handleGetError(err), nil
	}
	return server.GetGitRepository200JSONResponse(*repo), nil
}

func (h *Handler) UpdateGitRepository(ctx context.Context, request server.UpdateGitRepositoryRequestObject) (server.UpdateGitRepositoryResponseObject, error) {
	if request.Body == nil {
		return server.UpdateGitRepository400JSONResponse(buildError(
			400, v1alpha1.INVALIDARGUMENT, "Invalid request body", "Request body is required",
		)), nil
	}

	updated, err := h.service.Update(ctx, request.GitRepositoryId, *request.Body)
	if err != nil {
		return handleUpdateError(err), nil
	}

	slog.InfoContext(ctx, "GitRepository updated", "id", request.GitRepositoryId)
	return server.UpdateGitRepository200JSONResponse(*updated), nil
}

func (h *Handler) DeleteGitRepository(ctx context.Context, request server.DeleteGitRepositoryRequestObject) (server.DeleteGitRepositoryResponseObject, error) {
	err := h.service.Delete(ctx, request.GitRepositoryId)
	if err != nil {
		return handleDeleteError(err), nil
	}

	slog.InfoContext(ctx, "GitRepository deleted", "id", request.GitRepositoryId)
	return server.DeleteGitRepository204Response{}, nil
}
