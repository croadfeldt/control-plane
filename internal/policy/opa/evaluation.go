package opa

import (
	"fmt"
	"regexp"
)

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
	// Reason is the policy's own one-line account of what it decided, for the
	// requested record's policies[] entry. Optional.
	Reason string `json:"reason,omitempty"`
	// Consulted cites the records the policy read and that mattered to its
	// decision. ParsePolicyDecision does not fill it: ParseConsulted does, and
	// refuses a malformed list.
	Consulted []Citation `json:"consulted,omitempty"`
}

// Citation names one record a policy consulted: which record, which of its
// published versions, and its chain head when it has one. It is the entry shape
// of `consulted` in UDLM's udlm_context facet (ADR-059), and carries nothing
// else: which provider served the record is a fact on the record itself.
type Citation struct {
	UUID    string `json:"uuid"`
	Version string `json:"version"`
	Head    string `json:"head,omitempty"`
}

var (
	citationUUID = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	citationHead = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
)

// ParseConsulted reads the decision's `consulted` list. Absent means none. A
// list that is present must be exact: every entry an object with `uuid` (v4)
// and `version`, an optional `head` (sha256:<64 hex>), and no other key. A
// citation is evidence, so a malformed one is an error and never dropped
// quietly or passed through.
func ParseConsulted(result map[string]any) ([]Citation, error) {
	raw, present := result["consulted"]
	if !present || raw == nil {
		return nil, nil
	}
	items, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("consulted must be a list, got %T", raw)
	}
	out := make([]Citation, 0, len(items))
	for i, item := range items {
		entry, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("consulted[%d] must be an object, got %T", i, item)
		}
		var c Citation
		for key, value := range entry {
			text, isString := value.(string)
			if !isString {
				return nil, fmt.Errorf("consulted[%d].%s must be a string", i, key)
			}
			switch key {
			case "uuid":
				c.UUID = text
			case "version":
				c.Version = text
			case "head":
				c.Head = text
			default:
				return nil, fmt.Errorf("consulted[%d] carries %q; an entry is uuid, version and head only", i, key)
			}
		}
		if !citationUUID.MatchString(c.UUID) {
			return nil, fmt.Errorf("consulted[%d].uuid %q is not a v4 uuid", i, c.UUID)
		}
		if c.Version == "" {
			return nil, fmt.Errorf("consulted[%d] has no version", i)
		}
		if _, hasHead := entry["head"]; hasHead && !citationHead.MatchString(c.Head) {
			return nil, fmt.Errorf("consulted[%d].head %q is not sha256:<64 hex>", i, c.Head)
		}
		out = append(out, c)
	}
	return out, nil
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

	if reason, ok := result["reason"].(string); ok {
		decision.Reason = reason
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
