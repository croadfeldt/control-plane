package agent

import (
	"context"

	agentmodel "github.com/dcm-project/control-plane/internal/agent/store/model"
)

// readyLister is the single method this adapter needs from agentstore.Agent.
// Depending on this narrow interface, rather than the full store interface,
// keeps the compile-time surface honest about what's actually used here.
type readyLister interface {
	ListReady(ctx context.Context) (agentmodel.AgentList, error)
}

type serviceClient struct {
	store readyLister
}

// NewServiceClient adapts the agent store for in-process use by PlacementService.
func NewServiceClient(store readyLister) Client {
	return &serviceClient{store: store}
}

func (c *serviceClient) ListReadyAgents(ctx context.Context) ([]Info, error) {
	agents, err := c.store.ListReady(ctx)
	if err != nil {
		return nil, err
	}
	infos := make([]Info, len(agents))
	for i, a := range agents {
		cost := ""
		if a.Cost != nil {
			cost = string(*a.Cost)
		}
		infos[i] = Info{Name: a.Name, Environment: a.Environment, ServiceTypes: a.ServiceTypes, Cost: cost}
	}
	return infos, nil
}
