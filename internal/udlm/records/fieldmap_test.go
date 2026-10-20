package records

import (
	"context"
	"strings"
	"testing"
)

func TestMapFieldsRenamesToClassElements(t *testing.T) {
	got, err := mapFields("vm", map[string]any{
		"vcpu":     map[string]any{"count": 4.0},
		"memory":   map[string]any{"size": "16Gi"},
		"guest_os": map[string]any{"type": "rhel"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := got["vcpu"]; ok {
		t.Fatalf("vcpu left in place: %v", got)
	}
	if c, _ := getPath(got, "cpu.count"); c != 4.0 {
		t.Fatalf("cpu.count = %v, want 4", c)
	}
	got, err = mapFields("container", map[string]any{
		"image":   map[string]any{"reference": "quay.io/x/y:1"},
		"process": map[string]any{"command": []any{"run"}, "args": []any{"-v"}, "env": []any{}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := got["command"]; !ok {
		t.Fatalf("process.command not moved to command: %v", got)
	}
	if p, _ := got["process"].(map[string]any); p == nil || p["command"] != nil {
		t.Fatalf("process should keep env and lose command: %v", got)
	}
	got, _ = mapFields("cluster", map[string]any{"version": "4.18"})
	if got["release"] != "4.18" || got["version"] != nil {
		t.Fatalf("cluster version not renamed to release: %v", got)
	}
}

func TestMapFieldsRefusesFieldsWithNoClassHome(t *testing.T) {
	_, err := mapFields("vm", map[string]any{
		"vcpu":   map[string]any{"count": 2.0},
		"access": map[string]any{"ssh_public_key": "ssh-ed25519 AAAA"},
	})
	if err == nil || !strings.Contains(err.Error(), "access") {
		t.Fatalf("want a refusal naming access, got %v", err)
	}
	if _, err := mapFields("vm", map[string]any{"vcpu": map[string]any{"count": 2.0}, "storage": map[string]any{"disks": []any{}}}); err != nil {
		t.Fatalf("an empty disks list carries nothing and must not refuse: %v", err)
	}
}

func TestMapOutputsRenamesDeclaredOutputs(t *testing.T) {
	got := mapOutputs("storage", map[string]any{"volume_name": "pvc-1", "extra": true})
	if got["volume_handle"] != "pvc-1" || got["volume_name"] != nil || got["extra"] != true {
		t.Fatalf("storage outputs: %v", got)
	}
	got = mapOutputs("cluster", map[string]any{"api_endpoint": "https://api"})
	if got["api_url"] != "https://api" {
		t.Fatalf("cluster outputs: %v", got)
	}
}

// Upstream's request shapes produce valid, sealed records under the class's names:
// a VM ordered with `vcpu` is recorded as Machine.VM `cpu`, and a storage output
// `volume_name` is recorded as Volume's `volume_handle`.
func TestWriterRecordsUpstreamShapesUnderClassNames(t *testing.T) {
	ctx := context.Background()
	w, st := testWriter(t)
	inner := w.types
	w.types = func(s string) (Type, bool) {
		if s == "storage" {
			return Type{ResourceType: "Volume", Version: "0.13.0", Elements: []string{
				"access_mode", "capacity", "layout_entry", "retain_on_release", "storage_class", "volume_mode",
			}}, true
		}
		return inner(s)
	}
	vm := "7a7c9a41-8e3d-4b6a-9c15-7d2e4f8a1b03"
	spec := map[string]any{"service_type": "vm", "vcpu": map[string]any{"count": 2}, "memory": map[string]any{"size": "4Gi"}, "guest_os": map[string]any{"type": "rhel"}}
	w.Intent(ctx, IntentInput{EntityUUID: vm, Spec: spec})
	w.Requested(ctx, RequestedInput{EntityUUID: vm, Spec: spec, AgentName: "agent-a"})
	recs, _ := st.ListByEntity(ctx, vm)
	if len(recs) != 2 {
		t.Fatalf("vm: want intent + requested, got %d", len(recs))
	}
	fields, _ := recs[1].Body["fields"].(map[string]any)
	if _, ok := fields["cpu"]; !ok || fields["vcpu"] != nil {
		t.Fatalf("requested record should carry cpu, not vcpu: %v", fields)
	}
	vol := "8a7c9a41-8e3d-4b6a-9c15-7d2e4f8a1b03"
	sspec := map[string]any{"service_type": "storage", "capacity": "50Gi"}
	w.Intent(ctx, IntentInput{EntityUUID: vol, Spec: sspec})
	w.Requested(ctx, RequestedInput{EntityUUID: vol, Spec: sspec, AgentName: "agent-s"})
	w.Realized(ctx, RealizedInput{EntityUUID: vol, Outputs: map[string]any{"volume_name": "pvc-1"}, AgentName: "agent-s"})
	recs, _ = st.ListByEntity(ctx, vol)
	if len(recs) != 3 {
		t.Fatalf("storage: want intent, requested, realized; got %d", len(recs))
	}
	outputs, _ := recs[2].Body["outputs"].(map[string]any)
	if outputs["volume_handle"] != "pvc-1" || outputs["volume_name"] != nil {
		t.Fatalf("realized outputs should carry volume_handle: %v", outputs)
	}
	for _, r := range recs {
		if err := Validate(r.Body); err != nil {
			t.Fatalf("%s record invalid: %v", r.State, err)
		}
	}
}
