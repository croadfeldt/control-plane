// Package policy provides types and an adapter boundary for policy evaluation.
package policy

import (
	"context"
	"fmt"
)

// AgentInfo is the subset of agent metadata passed through to policy
// evaluation. Kept as a parallel type rather than importing
// internal/policy/service.AgentInfo directly, per this package's convention
// of not depending on that package's types.
type AgentInfo struct {
	Name         string   `json:"name"`
	Environment  string   `json:"environment"`
	ServiceTypes []string `json:"service_types,omitempty"`
	Cost         string   `json:"cost,omitempty"`
}

// EvaluateRequest is the input for policy evaluation.
type EvaluateRequest struct {
	Spec            map[string]any `json:"spec"`
	AvailableAgents []AgentInfo    `json:"available_agents,omitempty"`
	ExcludeAgents   []string       `json:"exclude_agents,omitempty"`
}

// EvaluateResponse is the result of policy evaluation.
type EvaluateResponse struct {
	Status        string         `json:"status"`
	SelectedAgent string         `json:"selected_agent"`
	EvaluatedSpec map[string]any `json:"evaluated_spec"`
}

// Client is the port PlacementService uses to evaluate policies.
type Client interface {
	Evaluate(ctx context.Context, req EvaluateRequest) (*EvaluateResponse, error)
}

// HTTPError carries a status code and message from the evaluation adapter.
type HTTPError struct {
	StatusCode int
	Body       string
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("policy evaluation returned status %d: %s", e.StatusCode, e.Body)
}
