// Package agent provides types and an adapter boundary for listing ready
// agents, mirroring the policy and sprm adapter packages.
package agent

import "context"

// Info is the subset of agent metadata exposed to PlacementService for
// policy evaluation. Cost is "" when the agent didn't report one (it's an
// optional field at registration, unlike Name/ServiceTypes).
type Info struct {
	Name         string
	Environment  string
	ServiceTypes []string
	Cost         string
}

// Client is the port PlacementService uses to list ready agents.
type Client interface {
	ListReadyAgents(ctx context.Context) ([]Info, error)
}
