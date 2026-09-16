// Package v1alpha1 implements HTTP handlers for the Agent API.
package v1alpha1

import (
	"context"

	api "github.com/dcm-project/control-plane/api/agent/v1alpha1"
	server "github.com/dcm-project/control-plane/internal/agent/api/server"
	"github.com/dcm-project/control-plane/internal/agent/service"
)

type Handler struct {
	agentService *service.AgentService
}

func NewHandler(agentService *service.AgentService) *Handler {
	return &Handler{agentService: agentService}
}

var _ server.StrictServerInterface = (*Handler)(nil)

func (h *Handler) ListAgents(ctx context.Context, req server.ListAgentsRequestObject) (server.ListAgentsResponseObject, error) {
	var healthStatus string
	if req.Params.HealthStatus != nil {
		healthStatus = string(*req.Params.HealthStatus)
	}
	pageSize := 0
	if req.Params.MaxPageSize != nil {
		pageSize = *req.Params.MaxPageSize
	}
	var pageToken string
	if req.Params.PageToken != nil {
		pageToken = *req.Params.PageToken
	}

	result, err := h.agentService.List(ctx, healthStatus, pageSize, pageToken)
	if err != nil {
		logServiceError(ctx, "ListAgents failed", err)
		return listErrorResponse(err)
	}

	sAgents := apiAgentsToServer(result.Agents)
	resp := server.AgentList{Agents: &sAgents}
	if result.NextPageToken != "" {
		resp.NextPageToken = &result.NextPageToken
	}
	return server.ListAgents200JSONResponse(resp), nil
}

func (h *Handler) CreateAgent(ctx context.Context, req server.CreateAgentRequestObject) (server.CreateAgentResponseObject, error) {
	apiReq := api.AgentRegistrationRequest{
		Name:         req.Body.Name,
		TopicName:    req.Body.TopicName,
		ServiceTypes: req.Body.ServiceTypes,
		Environment:  req.Body.Environment,
		Cost:         api.AgentRegistrationRequestCost(req.Body.Cost),
	}

	agent, created, err := h.agentService.RegisterOrUpdate(ctx, apiReq)
	if err != nil {
		logServiceError(ctx, "RegisterOrUpdate failed", err)
		return createErrorResponse(err)
	}

	sa := apiAgentToServer(agent)
	if created {
		return server.CreateAgent201JSONResponse(sa), nil
	}
	return server.CreateAgent200JSONResponse(sa), nil
}

func (h *Handler) GetAgent(ctx context.Context, req server.GetAgentRequestObject) (server.GetAgentResponseObject, error) {
	agent, err := h.agentService.Get(ctx, req.AgentId)
	if err != nil {
		logServiceError(ctx, "GetAgent failed", err, "agent_id", req.AgentId)
		return getErrorResponse(err)
	}
	return server.GetAgent200JSONResponse(apiAgentToServer(agent)), nil
}

func (h *Handler) AgentHeartbeat(ctx context.Context, req server.AgentHeartbeatRequestObject) (server.AgentHeartbeatResponseObject, error) {
	apiReq := api.HeartbeatRequest{
		ConsumerLag: req.Body.ConsumerLag,
		Timestamp:   req.Body.Timestamp,
	}

	agent, err := h.agentService.Heartbeat(ctx, req.AgentId, apiReq)
	if err != nil {
		logServiceError(ctx, "AgentHeartbeat failed", err, "agent_id", req.AgentId)
		return hbErrorResponse(err)
	}
	return server.AgentHeartbeat200JSONResponse(apiAgentToServer(agent)), nil
}
