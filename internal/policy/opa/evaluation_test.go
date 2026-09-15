package opa

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParsePolicyDecision(t *testing.T) {
	tests := []struct {
		name     string
		result   map[string]interface{}
		expected *PolicyDecision
	}{
		{
			name: "approval with patch",
			result: map[string]interface{}{
				"rejected": false,
				"patch": map[string]interface{}{
					"region": "us-east-1",
				},
				"selected_agent": "aws-agent",
			},
			expected: &PolicyDecision{
				Rejected: false,
				Patch: map[string]interface{}{
					"region": "us-east-1",
				},
				SelectedAgent: "aws-agent",
			},
		},
		{
			name: "approval with patch and constraints",
			result: map[string]interface{}{
				"rejected": false,
				"patch": map[string]interface{}{
					"region": "us-east-1",
				},
				"constraints": map[string]interface{}{
					"region": map[string]interface{}{
						"const": "us-east-1",
					},
				},
			},
			expected: &PolicyDecision{
				Rejected: false,
				Patch: map[string]interface{}{
					"region": "us-east-1",
				},
				Constraints: map[string]interface{}{
					"region": map[string]interface{}{
						"const": "us-east-1",
					},
				},
			},
		},
		{
			name: "approval with agent constraints",
			result: map[string]interface{}{
				"rejected": false,
				"agent_constraints": map[string]interface{}{
					"allow_list": []interface{}{"aws", "gcp"},
					"patterns":   []interface{}{"^(aws|gcp)$"},
				},
			},
			expected: &PolicyDecision{
				Rejected: false,
				AgentConstraints: &AgentConstraints{
					AllowList: []string{"aws", "gcp"},
					Patterns:  []string{"^(aws|gcp)$"},
				},
			},
		},
		{
			name: "rejection with reason",
			result: map[string]interface{}{
				"rejected":         true,
				"rejection_reason": "Security policy violation",
			},
			expected: &PolicyDecision{
				Rejected:        true,
				RejectionReason: "Security policy violation",
			},
		},
		{
			name:   "empty result",
			result: map[string]interface{}{},
			expected: &PolicyDecision{
				Rejected: false,
			},
		},
		{
			name: "partial fields - patch only",
			result: map[string]interface{}{
				"rejected": false,
				"patch": map[string]interface{}{
					"instance_type": "t3.medium",
				},
			},
			expected: &PolicyDecision{
				Rejected: false,
				Patch: map[string]interface{}{
					"instance_type": "t3.medium",
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			decision := ParsePolicyDecision(tt.result)
			assert.Equal(t, tt.expected.Rejected, decision.Rejected)
			assert.Equal(t, tt.expected.RejectionReason, decision.RejectionReason)
			assert.Equal(t, tt.expected.Patch, decision.Patch)
			assert.Equal(t, tt.expected.Constraints, decision.Constraints)
			assert.Equal(t, tt.expected.SelectedAgent, decision.SelectedAgent)
			if tt.expected.AgentConstraints != nil {
				assert.NotNil(t, decision.AgentConstraints)
				assert.Equal(t, tt.expected.AgentConstraints.AllowList, decision.AgentConstraints.AllowList)
				assert.Equal(t, tt.expected.AgentConstraints.Patterns, decision.AgentConstraints.Patterns)
			} else {
				assert.Nil(t, decision.AgentConstraints)
			}
		})
	}
}
