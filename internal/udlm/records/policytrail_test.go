package records

import (
	"context"
	"reflect"
	"testing"
)

const (
	businessService = "6c1f5a0e-2b7d-4c3a-9e41-0d8f7a6b5c21"
	changeCalendar  = "b3a9d2c4-7e15-4f60-8a2b-9c4d1e6f7a80"
	calendarHead    = "sha256:1111111111111111111111111111111111111111111111111111111111111111"
)

func sources(t *testing.T, prov map[string]any, path string) []string {
	t.Helper()
	entries, _ := prov[path].([]any)
	out := make([]string, 0, len(entries))
	for i, e := range entries {
		entry := e.(map[string]any)
		if got := entry["sequence"]; got != float64(i+1) {
			t.Errorf("%s entry %d has sequence %v", path, i, got)
		}
		out = append(out, stringOf(entry["source"].(map[string]any)["id"]))
	}
	return out
}

// Each changed field is put down to whoever changed it: placement for what it
// bound before policy ran and for anything left over, and each policy by its own
// id for what its patch set. A policy that read outside records is still the
// source; the records it read are cited beside the record.
func TestRequestedRecordNamesEachPolicy(t *testing.T) {
	ctx := context.Background()
	w, st := testWriter(t)
	entity := "2f7c9a41-8e3d-4b6a-9c15-7d2e4f8a1b03"
	intent := map[string]any{
		"service_type": "vm", "metadata": map[string]any{"name": "app-01"},
		"cpu": map[string]any{"count": 4}, "memory": map[string]any{"size": "16Gi"},
	}
	w.Intent(ctx, IntentInput{EntityUUID: entity, Spec: intent, Name: "app-01"})

	// Placement bound the instance size before policy ran.
	input := map[string]any{"service_type": "vm", "cpu": map[string]any{"count": 4}, "memory": map[string]any{"size": "16Gi"}, "instance_size": "m"}
	// The topology policy set the sockets; it consulted nothing.
	afterTopology := map[string]any{"service_type": "vm", "cpu": map[string]any{"count": 4, "sockets": 1}, "memory": map[string]any{"size": "16Gi"}, "instance_size": "m"}
	// The impact policy read the business service and the change calendar, and
	// set the storage tier and changed the sockets again.
	afterImpact := map[string]any{"service_type": "vm", "cpu": map[string]any{"count": 4, "sockets": 2}, "memory": map[string]any{"size": "16Gi"}, "instance_size": "m", "storage_tier": "gold"}
	w.Requested(ctx, RequestedInput{
		EntityUUID: entity, Spec: afterImpact, AgentName: "agent-a", PolicyInput: input,
		Policies: []PolicyOutcome{
			{ID: "cpu-topology", Spec: afterTopology},
			{ID: "freeze-check", Reason: "no freeze covers app-01", Consulted: []Citation{{UUID: changeCalendar, Version: "3", Head: calendarHead}}},
			{ID: "business-impact", Spec: afterImpact, Reason: "impact tier high", Consulted: []Citation{
				{UUID: businessService, Version: "1"},
				{UUID: changeCalendar, Version: "3", Head: calendarHead}, // read by both; cited once
			}},
		},
	})

	rec, err := st.Latest(ctx, entity, "Requested")
	if err != nil {
		t.Fatalf("no requested record: %v", err)
	}
	if err := Validate(rec.Body); err != nil {
		t.Fatalf("requested record invalid: %v", err)
	}
	prov := rec.Body["provenance"].(map[string]any)
	for path, want := range map[string][]string{
		"instance_size": {"dcm/placement"},
		"cpu.sockets":   {"dcm/policies/cpu-topology", "dcm/policies/business-impact"},
		"storage_tier":  {"dcm/policies/business-impact"},
	} {
		if got := sources(t, prov, path); !reflect.DeepEqual(got, want) {
			t.Errorf("%s set by %v, want %v", path, got, want)
		}
	}
	second := prov["cpu.sockets"].([]any)[1].(map[string]any)
	if second["previous_value"] != float64(1) {
		t.Errorf("the second cpu.sockets entry should carry the value it replaced, got %v", second["previous_value"])
	}

	wantPolicies := []any{
		map[string]any{"policy": "dcm/policies/cpu-topology", "decision": "allow", "fields": []any{"cpu.sockets"}},
		map[string]any{"policy": "dcm/policies/freeze-check", "decision": "allow", "reason": "no freeze covers app-01"},
		map[string]any{"policy": "dcm/policies/business-impact", "decision": "allow", "fields": []any{"cpu.sockets", "storage_tier"}, "reason": "impact tier high"},
	}
	if got := rec.Body["policies"]; !reflect.DeepEqual(got, wantPolicies) {
		t.Errorf("policies = %v\nwant       %v", got, wantPolicies)
	}
	applied := rec.Body["assembly"].(map[string]any)["applied"].([]any)
	appliedIDs := make([]string, 0, len(applied))
	for _, a := range applied {
		appliedIDs = append(appliedIDs, stringOf(a.(map[string]any)["source"].(map[string]any)["id"]))
	}
	if want := []string{"dcm/placement", "dcm/policies/cpu-topology", "dcm/policies/business-impact"}; !reflect.DeepEqual(appliedIDs, want) {
		t.Errorf("assembly.applied sources = %v, want %v", appliedIDs, want)
	}

	// The citations are beside the record, in a valid udlm_context facet, and
	// not in the record: the record's schema has no place for them.
	if rec.Context == nil {
		t.Fatal("the requested record has no context facet")
	}
	if err := ValidateContext(rec.Context); err != nil {
		t.Fatalf("context facet invalid: %v", err)
	}
	wantConsulted := []any{
		map[string]any{"uuid": changeCalendar, "version": "3", "head": calendarHead},
		map[string]any{"uuid": businessService, "version": "1"},
	}
	if got := rec.Context["consulted"]; !reflect.DeepEqual(got, wantConsulted) {
		t.Errorf("consulted = %v\nwant        %v", got, wantConsulted)
	}
	wantDecisions := []any{
		map[string]any{"policy_uuid": "dcm/policies/cpu-topology", "outcome": "transform"},
		map[string]any{"policy_uuid": "dcm/policies/freeze-check", "outcome": "permit"},
		map[string]any{"policy_uuid": "dcm/policies/business-impact", "outcome": "transform"},
	}
	if got := rec.Context["policy_decisions"]; !reflect.DeepEqual(got, wantDecisions) {
		t.Errorf("policy_decisions = %v\nwant               %v", got, wantDecisions)
	}
	if got := rec.Context["principal"].(map[string]any)["id"]; got != anonymousActor {
		t.Errorf("principal = %v, want the actor the intent record names", got)
	}
	if got := rec.Context["pathway_ref"].(map[string]any)["request_id"]; got != rec.Body["root_request_uuid"] {
		t.Errorf("pathway request_id = %v, want the root request %v", got, rec.Body["root_request_uuid"])
	}
	if _, inRecord := rec.Body["consulted"]; inRecord {
		t.Error("consulted must not be written into the record")
	}
}

// With no policy outcome the record reads as it did before: one placement
// source, and no context facet.
func TestRequestedRecordWithoutPoliciesIsUnchanged(t *testing.T) {
	ctx := context.Background()
	w, st := testWriter(t)
	entity := "2f7c9a41-8e3d-4b6a-9c15-7d2e4f8a1b03"
	spec := map[string]any{"service_type": "vm", "cpu": map[string]any{"count": 4}, "memory": map[string]any{"size": "16Gi"}}
	w.Intent(ctx, IntentInput{EntityUUID: entity, Spec: spec, Name: "app-01"})
	sent := map[string]any{"service_type": "vm", "cpu": map[string]any{"count": 4, "sockets": 1}, "memory": map[string]any{"size": "16Gi"}}
	w.Requested(ctx, RequestedInput{EntityUUID: entity, Spec: sent, AgentName: "agent-a", PolicyInput: spec})

	rec, err := st.Latest(ctx, entity, "Requested")
	if err != nil {
		t.Fatalf("no requested record: %v", err)
	}
	if got := sources(t, rec.Body["provenance"].(map[string]any), "cpu.sockets"); !reflect.DeepEqual(got, []string{"dcm/placement"}) {
		t.Errorf("cpu.sockets set by %v, want dcm/placement", got)
	}
	if _, has := rec.Body["policies"]; has {
		t.Error("policies[] written although no policy decided anything")
	}
	if rec.Context != nil {
		t.Errorf("context facet written although no policy decided anything: %v", rec.Context)
	}
}

// A citation the facet schema refuses stops the write: the record is not
// stored without the evidence its policies named.
func TestRequestedRecordRefusedWithBadCitation(t *testing.T) {
	ctx := context.Background()
	w, st := testWriter(t)
	entity := "2f7c9a41-8e3d-4b6a-9c15-7d2e4f8a1b03"
	spec := map[string]any{"service_type": "vm", "cpu": map[string]any{"count": 4}, "memory": map[string]any{"size": "16Gi"}}
	w.Intent(ctx, IntentInput{EntityUUID: entity, Spec: spec, Name: "app-01"})
	w.Requested(ctx, RequestedInput{EntityUUID: entity, Spec: spec, AgentName: "agent-a", Policies: []PolicyOutcome{
		{ID: "freeze-check", Consulted: []Citation{{UUID: "not-a-uuid", Version: "1"}}},
	}})
	if _, err := st.Latest(ctx, entity, "Requested"); err == nil {
		t.Error("a requested record was stored with a citation the facet schema refuses")
	}
}
