package v1alpha1

import (
	"context"
	"errors"
	"log/slog"

	server "github.com/dcm-project/control-plane/internal/agent/api/server"
	"github.com/dcm-project/control-plane/internal/agent/service"
)

// logServiceError logs at Warn for a *service.ServiceError that maps to a
// 4xx response (service.IsClientError) and Error otherwise, so severity
// matches whether the internalErrorDetail response below is hiding a real
// failure. A ServiceError code with no 4xx mapping (e.g.
// ErrCodeNotImplemented, which none of these mappers currently handle and
// which falls through to a 500 below) is deliberately logged at Error, not
// Warn, since it also represents a hidden failure from the caller's view.
func logServiceError(ctx context.Context, msg string, err error, attrs ...any) {
	args := append([]any{"error", err}, attrs...)
	var svcErr *service.ServiceError
	if service.IsClientError(err, &svcErr) {
		slog.WarnContext(ctx, msg, args...)
		return
	}
	slog.ErrorContext(ctx, msg, args...)
}

// newError builds an RFC 7807 error body. errType is a short slug for the
// "type" field, distinct from svcErr.Code (the longer internal URI).
func newError(errType, title, detail string, status int) server.Error {
	return server.Error{Type: errType, Title: title, Detail: &detail, Status: &status}
}

// internalErrorDetail is the fixed 5xx response detail: the real error is
// logged server-side by logServiceError before these functions are called,
// so the client-facing body never echoes it back.
const internalErrorDetail = "an internal error occurred"

// createErrorResponse uses errors.As, not a raw type assertion, so a
// *service.ServiceError wrapped by fmt.Errorf("...: %w", err) is still recognized.
func createErrorResponse(err error) (server.CreateAgentResponseObject, error) {
	var svcErr *service.ServiceError
	if errors.As(err, &svcErr) {
		switch svcErr.Code {
		case service.ErrCodeValidation:
			return server.CreateAgent400ApplicationProblemPlusJSONResponse(
				newError("validation-error", "Invalid request", svcErr.Message, 400)), nil
		case service.ErrCodeConflict:
			return server.CreateAgent409ApplicationProblemPlusJSONResponse(
				newError("conflict", "Agent already registered", svcErr.Message, 409)), nil
		}
	}
	return server.CreateAgentdefaultApplicationProblemPlusJSONResponse{
		Body:       newError("create-error", "Failed to register agent", internalErrorDetail, 500),
		StatusCode: 500,
	}, nil
}

func getErrorResponse(err error) (server.GetAgentResponseObject, error) {
	var svcErr *service.ServiceError
	if errors.As(err, &svcErr) && svcErr.Code == service.ErrCodeNotFound {
		return server.GetAgent404ApplicationProblemPlusJSONResponse(
			newError("not-found", "Agent not found", svcErr.Message, 404)), nil
	}
	return server.GetAgentdefaultApplicationProblemPlusJSONResponse{
		Body:       newError("get-error", "Failed to get agent", internalErrorDetail, 500),
		StatusCode: 500,
	}, nil
}

func hbErrorResponse(err error) (server.AgentHeartbeatResponseObject, error) {
	var svcErr *service.ServiceError
	if errors.As(err, &svcErr) && svcErr.Code == service.ErrCodeNotFound {
		return server.AgentHeartbeat404ApplicationProblemPlusJSONResponse(
			newError("not-found", "Agent not found", svcErr.Message, 404)), nil
	}
	return server.AgentHeartbeatdefaultApplicationProblemPlusJSONResponse{
		Body:       newError("heartbeat-error", "Failed to record heartbeat", internalErrorDetail, 500),
		StatusCode: 500,
	}, nil
}

func listErrorResponse(err error) (server.ListAgentsResponseObject, error) {
	var svcErr *service.ServiceError
	if errors.As(err, &svcErr) && svcErr.Code == service.ErrCodeValidation {
		return server.ListAgents400ApplicationProblemPlusJSONResponse(
			newError("validation-error", "Invalid request", svcErr.Message, 400)), nil
	}
	return server.ListAgentsdefaultApplicationProblemPlusJSONResponse{
		Body:       newError("list-error", "Failed to list agents", internalErrorDetail, 500),
		StatusCode: 500,
	}, nil
}
