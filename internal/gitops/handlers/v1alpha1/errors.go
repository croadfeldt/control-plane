package v1alpha1

import (
	"errors"

	v1alpha1 "github.com/dcm-project/control-plane/api/gitops/v1alpha1"
	"github.com/dcm-project/control-plane/internal/gitops/api/server"
	"github.com/dcm-project/control-plane/internal/gitops/service"
)

func buildError(status int32, errType v1alpha1.ErrorType, title, detail string) v1alpha1.Error {
	return v1alpha1.Error{
		Status: status,
		Type:   errType,
		Title:  title,
		Detail: &detail,
	}
}

func handleListError(err error) server.ListGitRepositoriesResponseObject {
	if errors.Is(err, service.ErrInvalidArgument) {
		return server.ListGitRepositories400JSONResponse{
			BadRequestJSONResponse: server.BadRequestJSONResponse(buildError(
				400, v1alpha1.INVALIDARGUMENT, "Invalid request", err.Error(),
			)),
		}
	}
	return server.ListGitRepositories500JSONResponse{
		InternalServerErrorJSONResponse: server.InternalServerErrorJSONResponse(buildError(
			500, v1alpha1.INTERNAL, "Internal server error", err.Error(),
		)),
	}
}

func handleCreateError(err error) server.CreateGitRepositoryResponseObject {
	switch {
	case errors.Is(err, service.ErrInvalidArgument):
		return server.CreateGitRepository400JSONResponse(buildError(
			400, v1alpha1.INVALIDARGUMENT, "Invalid request", err.Error(),
		))
	case errors.Is(err, service.ErrAlreadyExists):
		return server.CreateGitRepository409JSONResponse{
			AlreadyExistsJSONResponse: server.AlreadyExistsJSONResponse(buildError(
				409, v1alpha1.ALREADYEXISTS, "Resource already exists", err.Error(),
			)),
		}
	default:
		return server.CreateGitRepository500JSONResponse{
			InternalServerErrorJSONResponse: server.InternalServerErrorJSONResponse(buildError(
				500, v1alpha1.INTERNAL, "Internal server error", err.Error(),
			)),
		}
	}
}

func handleGetError(err error) server.GetGitRepositoryResponseObject {
	if errors.Is(err, service.ErrNotFound) {
		return server.GetGitRepository404JSONResponse{
			NotFoundJSONResponse: server.NotFoundJSONResponse(buildError(
				404, v1alpha1.NOTFOUND, "Resource not found", err.Error(),
			)),
		}
	}
	return server.GetGitRepository500JSONResponse{
		InternalServerErrorJSONResponse: server.InternalServerErrorJSONResponse(buildError(
			500, v1alpha1.INTERNAL, "Internal server error", err.Error(),
		)),
	}
}

func handleUpdateError(err error) server.UpdateGitRepositoryResponseObject {
	switch {
	case errors.Is(err, service.ErrInvalidArgument):
		return server.UpdateGitRepository400JSONResponse(buildError(
			400, v1alpha1.INVALIDARGUMENT, "Invalid request", err.Error(),
		))
	case errors.Is(err, service.ErrNotFound):
		return server.UpdateGitRepository404JSONResponse{
			NotFoundJSONResponse: server.NotFoundJSONResponse(buildError(
				404, v1alpha1.NOTFOUND, "Resource not found", err.Error(),
			)),
		}
	default:
		return server.UpdateGitRepository500JSONResponse{
			InternalServerErrorJSONResponse: server.InternalServerErrorJSONResponse(buildError(
				500, v1alpha1.INTERNAL, "Internal server error", err.Error(),
			)),
		}
	}
}

func handleDeleteError(err error) server.DeleteGitRepositoryResponseObject {
	if errors.Is(err, service.ErrNotFound) {
		return server.DeleteGitRepository404JSONResponse{
			NotFoundJSONResponse: server.NotFoundJSONResponse(buildError(
				404, v1alpha1.NOTFOUND, "Resource not found", err.Error(),
			)),
		}
	}
	return server.DeleteGitRepository500JSONResponse{
		InternalServerErrorJSONResponse: server.InternalServerErrorJSONResponse(buildError(
			500, v1alpha1.INTERNAL, "Internal server error", err.Error(),
		)),
	}
}
