package opa

// EvaluationResult represents the result from OPA evaluation
type EvaluationResult struct {
	Result  map[string]any
	Defined bool
}

// AgentConstraints represents constraints on which agents are allowed
type AgentConstraints struct {
	AllowList              []string `json:"allow_list,omitempty"`
	Patterns               []string `json:"patterns,omitempty"`
	EnvironmentConstraints []string `json:"environment_constraints,omitempty"`
}

// PolicyDecision represents the expected output from OPA policies
type PolicyDecision struct {
	Rejected         bool              `json:"rejected"`
	RejectionReason  string            `json:"rejection_reason,omitempty"`
	Patch            map[string]any    `json:"patch,omitempty"`
	Constraints      map[string]any    `json:"constraints,omitempty"`
	SelectedAgent    string            `json:"selected_agent,omitempty"`
	AgentConstraints *AgentConstraints `json:"agent_constraints,omitempty"`
}

// ParsePolicyDecision extracts a PolicyDecision from the OPA evaluation result
func ParsePolicyDecision(result map[string]any) *PolicyDecision {
	decision := &PolicyDecision{}

	if rejected, ok := result["rejected"].(bool); ok {
		decision.Rejected = rejected
	}

	if reason, ok := result["rejection_reason"].(string); ok {
		decision.RejectionReason = reason
	}

	if patch, ok := result["patch"].(map[string]any); ok {
		decision.Patch = patch
	}

	if constraints, ok := result["constraints"].(map[string]any); ok {
		decision.Constraints = constraints
	}

	if agent, ok := result["selected_agent"].(string); ok {
		decision.SelectedAgent = agent
	}

	if ac, ok := result["agent_constraints"].(map[string]any); ok {
		agentConstraints := &AgentConstraints{}
		if allowList, ok := ac["allow_list"].([]any); ok {
			for _, item := range allowList {
				if s, ok := item.(string); ok {
					agentConstraints.AllowList = append(agentConstraints.AllowList, s)
				}
			}
		}
		if patterns, ok := ac["patterns"].([]any); ok {
			for _, item := range patterns {
				if s, ok := item.(string); ok {
					agentConstraints.Patterns = append(agentConstraints.Patterns, s)
				}
			}
		}
		if envConstraints, ok := ac["environment_constraints"].([]any); ok {
			for _, item := range envConstraints {
				if s, ok := item.(string); ok {
					agentConstraints.EnvironmentConstraints = append(agentConstraints.EnvironmentConstraints, s)
				}
			}
		}
		decision.AgentConstraints = agentConstraints
	}

	return decision
}
