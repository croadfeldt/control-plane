package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/dcm-project/control-plane/internal/policy/opa"
	"github.com/dcm-project/control-plane/internal/policy/store/model"
)

// These tests run a policy that consults an information provider over HTTP on
// the real embedded engine. testdata/business_context.rego is the policy the
// homelab deploys, with the provider's address left as a placeholder.

const (
	serviceUUID  = "6c1f5a0e-2b7d-4c3a-9e41-0d8f7a6b5c21"
	calendarUUID = "b3a9d2c4-7e15-4f60-8a2b-9c4d1e6f7a80"
	someHead     = "sha256:1111111111111111111111111111111111111111111111111111111111111111"
)

// providerAnswer is a /search answer in the provider's shape.
func providerAnswer(calendarCitation map[string]any) map[string]any {
	return map[string]any{
		"matched": true,
		"business_service": map[string]any{
			"uuid": serviceUUID, "version": "1", "handle": "estate/business-services/estate-knowledge",
			"impact_tier": "high", "owner_group": "platform",
			"integrity": map[string]any{"head": someHead},
		},
		"freeze": map[string]any{
			"frozen": true, "name": "Thanksgiving week", "calendar_ref": "estate/calendars/homelab-change",
			"until": "2026-11-30T14:00:00Z", "calendars_read": []any{calendarCitation},
		},
	}
}

func consultingService(t *testing.T, providerURL string) EvaluationService {
	t.Helper()
	raw, err := os.ReadFile("testdata/business_context.rego")
	if err != nil {
		t.Fatal(err)
	}
	engine := opa.NewEngine()
	code := strings.ReplaceAll(string(raw), "__PROVIDER__", providerURL)
	if err := engine.Compile(context.Background(), []opa.PolicyModule{{ID: "business-context", RegoCode: code}}); err != nil {
		t.Fatalf("the policy does not compile: %v", err)
	}
	policies := &mockPolicyStore{policies: []model.Policy{{ID: "business-context", Enabled: true, PolicyType: "GLOBAL", Priority: 100}}}
	return NewEvaluationService(policies, engine)
}

func request() *EvaluationRequest {
	return &EvaluationRequest{ServiceInstance: map[string]any{
		"service_type": "container", "metadata": map[string]any{"name": "app-estate-mcpo"},
	}}
}

func provider(t *testing.T, answer map[string]any) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var asked map[string]any
		if err := json.NewDecoder(r.Body).Decode(&asked); err != nil || asked["handle"] != "app-estate-mcpo" || r.URL.Path != "/search" {
			t.Errorf("the policy asked %s %v; want /search for the request's name", r.URL.Path, asked)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(answer)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestPolicyCitesTheRecordsItConsulted(t *testing.T) {
	srv := provider(t, providerAnswer(map[string]any{"uuid": calendarUUID, "version": "3", "head": someHead}))
	response, err := consultingService(t, srv.URL).EvaluateRequest(context.Background(), request())
	if err != nil {
		t.Fatalf("evaluation failed: %v", err)
	}
	if len(response.Policies) != 1 {
		t.Fatalf("got %d policy outcomes, want 1", len(response.Policies))
	}
	outcome := response.Policies[0]
	want := []opa.Citation{
		{UUID: serviceUUID, Version: "1", Head: someHead},
		{UUID: calendarUUID, Version: "3", Head: someHead},
	}
	if len(outcome.Consulted) != 2 || outcome.Consulted[0] != want[0] || outcome.Consulted[1] != want[1] {
		t.Errorf("consulted = %v, want %v", outcome.Consulted, want)
	}
	for _, part := range []string{"impact tier high", "change freeze Thanksgiving week", "until 2026-11-30T14:00:00Z"} {
		if !strings.Contains(outcome.Reason, part) {
			t.Errorf("reason %q does not say %q", outcome.Reason, part)
		}
	}
	// A freeze defers and never refuses: the request was not rejected, and a
	// policy that only read and decided leaves the spec as it was.
	if response.Status != EvaluationStatusApproved || outcome.Spec != nil {
		t.Errorf("status %s, patched spec %v; want an approved, unpatched request", response.Status, outcome.Spec)
	}
}

// A citation is uuid, version and head. A policy that passes anything else
// through fails the evaluation: the extra key is neither dropped nor recorded.
func TestPolicyWithMalformedCitationFailsEvaluation(t *testing.T) {
	srv := provider(t, providerAnswer(map[string]any{"uuid": calendarUUID, "version": "3", "provider": "estate/providers/mock"}))
	_, err := consultingService(t, srv.URL).EvaluateRequest(context.Background(), request())
	if err == nil || !strings.Contains(err.Error(), "invalid decision") {
		t.Fatalf("got %v, want the policy's decision refused as invalid", err)
	}
}

// A provider that does not answer must not stop placement.
func TestPolicyGoesOnWhenTheProviderIsDown(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	srv.Close()
	response, err := consultingService(t, srv.URL).EvaluateRequest(context.Background(), request())
	if err != nil {
		t.Fatalf("evaluation failed with the provider down: %v", err)
	}
	if len(response.Policies) != 1 || len(response.Policies[0].Consulted) != 0 || !strings.Contains(response.Policies[0].Reason, "gave no answer") {
		t.Errorf("outcome = %+v; want one outcome that says nothing was consulted", response.Policies)
	}
}
