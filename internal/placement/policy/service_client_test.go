package policy

import (
	"context"
	"errors"
	"net/http"
	"testing"

	policyservice "github.com/dcm-project/control-plane/internal/policy/service"
)

// TestMapEvaluationError checks the ServiceError.Type-to-status mapping,
// including the new NewNoCapableAgentError/NewAllAgentsExcludedError paths
// (both ErrorTypeRejected), and that a non-ServiceError is wrapped rather
// than silently mapped to a default status.
func TestMapEvaluationError(t *testing.T) {
	cases := []struct {
		name           string
		err            error
		wantStatusCode int // 0 means "expect no *HTTPError at all"
	}{
		{
			name:           "invalid argument maps to 400",
			err:            policyservice.NewInvalidArgumentError("bad spec", "detail"),
			wantStatusCode: http.StatusBadRequest,
		},
		{
			name:           "policy rejected maps to 406",
			err:            policyservice.NewPolicyRejectedError("policy-1", "denied"),
			wantStatusCode: http.StatusNotAcceptable,
		},
		{
			name:           "no capable agent maps to 406 (same bucket as policy rejection)",
			err:            policyservice.NewNoCapableAgentError("database", 1),
			wantStatusCode: http.StatusNotAcceptable,
		},
		{
			name:           "all agents excluded maps to 406 (same bucket as policy rejection)",
			err:            policyservice.NewAllAgentsExcludedError(2),
			wantStatusCode: http.StatusNotAcceptable,
		},
		{
			name:           "policy conflict maps to 409",
			err:            policyservice.NewPolicyConflictError("low-prio", "field", "high-prio"),
			wantStatusCode: http.StatusConflict,
		},
		{
			name:           "internal error maps to the 500 default",
			err:            policyservice.NewInternalError("failed", "detail", nil),
			wantStatusCode: http.StatusInternalServerError,
		},
		{
			name:           "a non-ServiceError is wrapped rather than mapped to an HTTPError",
			err:            errors.New("some unrelated infra error"),
			wantStatusCode: 0,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := mapEvaluationError(tc.err)

			var httpErr *HTTPError
			ok := errors.As(got, &httpErr)

			if tc.wantStatusCode == 0 {
				if ok {
					t.Fatalf("expected a non-HTTPError wrapped error, got *HTTPError{%d, %q}", httpErr.StatusCode, httpErr.Body)
				}
				return
			}

			if !ok {
				t.Fatalf("expected *HTTPError, got %T: %v", got, got)
			}
			if httpErr.StatusCode != tc.wantStatusCode {
				t.Errorf("StatusCode = %d, want %d", httpErr.StatusCode, tc.wantStatusCode)
			}
		})
	}
}

// capturingEvaluationService records the EvaluationRequest it was called
// with, so tests can assert on the AgentInfo conversion Evaluate performs.
type capturingEvaluationService struct {
	captured *policyservice.EvaluationRequest
	response *policyservice.EvaluationResponse
	err      error
}

func (c *capturingEvaluationService) EvaluateRequest(_ context.Context, req *policyservice.EvaluationRequest) (*policyservice.EvaluationResponse, error) {
	c.captured = req
	if c.err != nil {
		return nil, c.err
	}
	if c.response != nil {
		return c.response, nil
	}
	return &policyservice.EvaluationResponse{}, nil
}

// TestServiceClient_Evaluate_ThreadsAgentInfo guards against the exact bug
// this change fixed: an AgentInfo field (Name/Environment/ServiceTypes/Cost)
// silently dropped on the way from the placement/policy adapter into
// policyservice.EvaluationRequest.
func TestServiceClient_Evaluate_ThreadsAgentInfo(t *testing.T) {
	eval := &capturingEvaluationService{}
	client := NewServiceClient(eval)

	req := EvaluateRequest{
		Spec: map[string]any{"service_type": "vm"},
		AvailableAgents: []AgentInfo{
			{Name: "agent-a", Environment: "prod", ServiceTypes: []string{"vm", "database"}, Cost: "low"},
		},
		ExcludeAgents: []string{"agent-b"},
	}

	_, err := client.Evaluate(context.Background(), req)
	if err != nil {
		t.Fatalf("Evaluate returned unexpected error: %v", err)
	}

	if eval.captured == nil {
		t.Fatal("EvaluationService was not called")
	}
	if len(eval.captured.AvailableAgents) != 1 {
		t.Fatalf("AvailableAgents = %d agents, want 1", len(eval.captured.AvailableAgents))
	}
	got := eval.captured.AvailableAgents[0]
	want := policyservice.AgentInfo{Name: "agent-a", Environment: "prod", ServiceTypes: []string{"vm", "database"}, Cost: "low"}
	if got.Name != want.Name || got.Environment != want.Environment || got.Cost != want.Cost {
		t.Errorf("AgentInfo = %+v, want %+v", got, want)
	}
	if len(got.ServiceTypes) != 2 || got.ServiceTypes[0] != "vm" || got.ServiceTypes[1] != "database" {
		t.Errorf("ServiceTypes = %v, want [vm database]", got.ServiceTypes)
	}
	if len(eval.captured.ExcludeAgents) != 1 || eval.captured.ExcludeAgents[0] != "agent-b" {
		t.Errorf("ExcludeAgents = %v, want [agent-b]", eval.captured.ExcludeAgents)
	}
}

func TestServiceClient_Evaluate_MapsErrorOnFailure(t *testing.T) {
	eval := &capturingEvaluationService{err: policyservice.NewNoCapableAgentError("database", 0)}
	client := NewServiceClient(eval)

	_, err := client.Evaluate(context.Background(), EvaluateRequest{Spec: map[string]any{}})

	var httpErr *HTTPError
	if !errors.As(err, &httpErr) {
		t.Fatalf("expected *HTTPError, got %T: %v", err, err)
	}
	if httpErr.StatusCode != http.StatusNotAcceptable {
		t.Errorf("StatusCode = %d, want %d", httpErr.StatusCode, http.StatusNotAcceptable)
	}
}
