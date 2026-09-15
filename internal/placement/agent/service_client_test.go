package agent

import (
	"context"
	"testing"

	agentmodel "github.com/dcm-project/control-plane/internal/agent/store/model"
)

type fakeReadyLister struct {
	agents agentmodel.AgentList
	err    error
}

func (f *fakeReadyLister) ListReady(_ context.Context) (agentmodel.AgentList, error) {
	return f.agents, f.err
}

// TestListReadyAgents_ThreadsAllFields guards against the exact bug this
// change fixed: an Agent field silently dropped on the way into the
// placement/agent.Info the policy engine ultimately sees.
func TestListReadyAgents_ThreadsAllFields(t *testing.T) {
	costLow := agentmodel.AgentCostLow
	lister := &fakeReadyLister{agents: agentmodel.AgentList{
		{Name: "agent-a", Environment: "prod", ServiceTypes: []string{"vm", "database"}, Cost: &costLow},
		{Name: "agent-b", Environment: "dev", ServiceTypes: nil, Cost: nil},
	}}
	client := NewServiceClient(lister)

	infos, err := client.ListReadyAgents(context.Background())
	if err != nil {
		t.Fatalf("ListReadyAgents returned unexpected error: %v", err)
	}
	if len(infos) != 2 {
		t.Fatalf("got %d agents, want 2", len(infos))
	}

	a := infos[0]
	if a.Name != "agent-a" || a.Environment != "prod" || a.Cost != "low" {
		t.Errorf("agent-a = %+v, want Name=agent-a Environment=prod Cost=low", a)
	}
	if len(a.ServiceTypes) != 2 || a.ServiceTypes[0] != "vm" || a.ServiceTypes[1] != "database" {
		t.Errorf("agent-a ServiceTypes = %v, want [vm database]", a.ServiceTypes)
	}

	b := infos[1]
	if b.Cost != "" {
		t.Errorf("agent-b Cost = %q, want \"\" (nil *AgentCost dereferenced safely)", b.Cost)
	}
	if b.ServiceTypes != nil {
		t.Errorf("agent-b ServiceTypes = %v, want nil (passed through unchanged)", b.ServiceTypes)
	}
}

func TestListReadyAgents_PropagatesStoreError(t *testing.T) {
	lister := &fakeReadyLister{err: context.DeadlineExceeded}
	client := NewServiceClient(lister)

	_, err := client.ListReadyAgents(context.Background())
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
}
