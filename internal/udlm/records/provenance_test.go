package records

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/dcm-project/control-plane/internal/auth"
)

func TestLeafPathsAndDiff(t *testing.T) {
	before := map[string]any{"cpu": map[string]any{"count": 4}, "memory": map[string]any{"size": "16Gi"}, "tags": []any{"a"}, "gone": true}
	after := map[string]any{"cpu": map[string]any{"count": 4, "sockets": 1}, "memory": map[string]any{"size": "32Gi"}, "tags": []any{"a"}}
	paths := leafPaths(after, "")
	if !reflect.DeepEqual(sortedKeys(paths), []string{"cpu.count", "cpu.sockets", "memory.size", "tags"}) {
		t.Errorf("leaf paths = %v", sortedKeys(paths))
	}
	prov, changed := diffProvenance(before, after, "policy", "dcm/placement", "2026-09-26T00:00:00Z")
	if !reflect.DeepEqual(changed, []string{"cpu.sockets", "gone", "memory.size"}) {
		t.Errorf("changed = %v", changed)
	}
	sockets := prov["cpu.sockets"].([]any)[0].(map[string]any)
	if _, has := sockets["previous_value"]; has || sockets["operation_type"] != "set" {
		t.Errorf("added path must be a set without previous_value: %v", sockets)
	}
	mem := prov["memory.size"].([]any)[0].(map[string]any)
	if mem["previous_value"] != "16Gi" {
		t.Errorf("changed path must carry the previous value: %v", mem)
	}
	gone := prov["gone"].([]any)[0].(map[string]any)
	if gone["operation_type"] != "remove" || gone["previous_value"] != true {
		t.Errorf("removed path must be a remove with the previous value: %v", gone)
	}
	if _, present := prov["cpu.count"]; present {
		t.Error("unchanged path must not be attributed")
	}
}

func TestOutputsProvenanceCarriesAcrossSupersede(t *testing.T) {
	first := outputsProvenance(nil, map[string]any{"primary_ip": "10.0.0.5", "hostname": "a"}, nil, "dcm/agents/x", "2026-09-26T00:00:01Z")
	if len(first) != 2 || first["outputs.hostname"].([]any)[0].(map[string]any)["sequence"] != 1 {
		t.Errorf("first provenance = %v", first)
	}
	second := outputsProvenance(
		map[string]any{"primary_ip": "10.0.0.5", "hostname": "a"},
		map[string]any{"primary_ip": "10.0.0.6"},
		first, "dcm/agents/x", "2026-09-26T00:00:02Z")
	ip := second["outputs.primary_ip"].([]any)
	if len(ip) != 2 || ip[0].(map[string]any)["timestamp"] != "2026-09-26T00:00:01Z" {
		t.Errorf("changed output must keep its origin entry and append: %v", ip)
	}
	if e := ip[1].(map[string]any); e["sequence"] != 2 || e["previous_value"] != "10.0.0.5" {
		t.Errorf("appended entry = %v", e)
	}
	host := second["outputs.hostname"].([]any)
	if len(host) != 2 || host[1].(map[string]any)["operation_type"] != "remove" {
		t.Errorf("disappeared output must get a remove: %v", host)
	}
	third := outputsProvenance(map[string]any{"primary_ip": "10.0.0.6"}, map[string]any{"primary_ip": "10.0.0.6"}, second, "dcm/agents/x", "2026-09-26T00:00:03Z")
	if !reflect.DeepEqual(third["outputs.primary_ip"], second["outputs.primary_ip"]) {
		t.Error("an unchanged output must keep its provenance unchanged")
	}
}

func TestWriterProvenance(t *testing.T) {
	w, st := testWriter(t)
	ctx := auth.WithActorInfo(context.Background(), auth.ActorInfo{ActorID: "alice", ActorType: "user"})
	entity := "4f7c9a41-8e3d-4b6a-9c15-7d2e4f8a1b03"
	w.Intent(ctx, IntentInput{EntityUUID: entity, Spec: map[string]any{"service_type": "vm", "cpu": map[string]any{"count": 4}, "memory": map[string]any{"size": "16Gi"}}})
	w.Requested(ctx, RequestedInput{EntityUUID: entity, AgentName: "a", Spec: map[string]any{"service_type": "vm", "cpu": map[string]any{"count": 4, "sockets": 1}, "memory": map[string]any{"size": "16Gi"}}})
	eventTime := time.Date(2026, 9, 26, 9, 30, 0, 0, time.UTC)
	w.Realized(ctx, RealizedInput{EntityUUID: entity, AgentName: "a", RunID: "run-1", At: eventTime, Outputs: map[string]any{"primary_ip": "10.0.0.5"}})
	w.Realized(ctx, RealizedInput{EntityUUID: entity, AgentName: "a", RunID: "run-1", At: eventTime.Add(time.Minute), Outputs: map[string]any{"primary_ip": "10.0.0.6"}})
	recs, err := st.ListByEntity(ctx, entity)
	if err != nil || len(recs) != 4 {
		t.Fatalf("records: %v %d", err, len(recs))
	}
	for i := range recs {
		if err := Validate(recs[i].Body); err != nil {
			t.Errorf("record %d invalid with provenance: %v", i, err)
		}
	}
	intentProv := recs[0].Body["provenance"].(map[string]any)
	cpu := intentProv["cpu.count"].([]any)[0].(map[string]any)
	if cpu["source"].(map[string]any)["kind"] != "actor" || cpu["source"].(map[string]any)["id"] != "dcm/actors/alice" {
		t.Errorf("intent fields must be attributed to the actor: %v", cpu)
	}
	reqProv := recs[1].Body["provenance"].(map[string]any)
	if _, ok := reqProv["cpu.sockets"]; !ok || len(reqProv) != 1 {
		t.Errorf("requested provenance must cover exactly what placement changed: %v", sortedKeys(reqProv))
	}
	applied := recs[1].Body["assembly"].(map[string]any)["applied"].([]any)[0].(map[string]any)
	if !reflect.DeepEqual(applied["fields"], []any{"cpu.sockets"}) {
		t.Errorf("assembly.applied fields = %v", applied["fields"])
	}
	realized := recs[2].Body
	if realized["at"] != "2026-09-26T09:30:00Z" {
		t.Errorf("realized at must be the event time, got %v", realized["at"])
	}
	ipProv := realized["provenance"].(map[string]any)["outputs.primary_ip"].([]any)
	if e := ipProv[0].(map[string]any); e["source"].(map[string]any)["id"] != "dcm/agents/a" || e["timestamp"] != "2026-09-26T09:30:00Z" {
		t.Errorf("output provenance = %v", e)
	}
	note := realized["metadata"].(map[string]any)["notes"].([]any)[0].(map[string]any)
	if note["text"] != "placement run run-1" {
		t.Errorf("run note = %v", note)
	}
	second := recs[3].Body["provenance"].(map[string]any)["outputs.primary_ip"].([]any)
	if len(second) != 2 || second[1].(map[string]any)["previous_value"] != "10.0.0.5" || second[1].(map[string]any)["timestamp"] != "2026-09-26T09:31:00Z" {
		t.Errorf("superseding record must advance the field's provenance: %v", second)
	}
}
