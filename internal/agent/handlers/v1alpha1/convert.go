package v1alpha1

import (
	api "github.com/dcm-project/control-plane/api/agent/v1alpha1"
	server "github.com/dcm-project/control-plane/internal/agent/api/server"
)

func apiAgentToServer(a *api.Agent) server.Agent {
	s := server.Agent{
		AgentId:       a.AgentId,
		Name:          a.Name,
		Environment:   a.Environment,
		TopicName:     a.TopicName,
		CreateTime:    a.CreateTime,
		UpdateTime:    a.UpdateTime,
		LastHeartbeat: a.LastHeartbeat,
	}
	if a.Cost != nil {
		cost := server.AgentCost(*a.Cost)
		s.Cost = &cost
	}
	if a.HealthStatus != nil {
		hs := server.AgentHealthStatus(*a.HealthStatus)
		s.HealthStatus = &hs
	}
	if a.ServiceTypes != nil {
		st := make([]string, len(*a.ServiceTypes))
		copy(st, *a.ServiceTypes)
		s.ServiceTypes = &st
	}
	return s
}

func apiAgentsToServer(agents []api.Agent) []server.Agent {
	out := make([]server.Agent, len(agents))
	for i := range agents {
		out[i] = apiAgentToServer(&agents[i])
	}
	return out
}
