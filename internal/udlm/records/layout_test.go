package records

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

func vmWithDisks(disks ...any) map[string]any {
	return map[string]any{
		"service_type": "vm",
		"vcpu":         map[string]any{"count": 2},
		"memory":       map[string]any{"size": "4Gi"},
		"guest_os":     map[string]any{"type": "rhel-9"},
		"storage":      map[string]any{"disks": disks},
	}
}

func disk(name, capacity string) map[string]any {
	return map[string]any{"name": name, "capacity": capacity}
}

// A VM's disks become a StorageLayout entity with its own intent, requested and
// realized records; the VM's records reference it and never carry the disks.
func TestWriterExpandsVMDisksIntoAStorageLayout(t *testing.T) {
	ctx := context.Background()
	w, st := testWriter(t)
	vm := "9a7c9a41-8e3d-4b6a-9c15-7d2e4f8a1b03"
	spec := vmWithDisks(disk("boot", "100GB"), disk("data", "500GB"))
	w.Intent(ctx, IntentInput{EntityUUID: vm, Spec: spec, Name: "app-01"})
	w.Requested(ctx, RequestedInput{EntityUUID: vm, Spec: spec, AgentName: "agent-a"})
	w.Realized(ctx, RealizedInput{EntityUUID: vm, Outputs: map[string]any{"primary_ip": "192.0.2.10"}, AgentName: "agent-a"})
	w.Realized(ctx, RealizedInput{EntityUUID: vm, Outputs: map[string]any{"primary_ip": "192.0.2.11"}, AgentName: "agent-a"})

	vmRecs, _ := st.ListByEntity(ctx, vm)
	if got := statesOf(vmRecs); got != "Intent,Requested,Realized,Realized" {
		t.Fatalf("vm states = %s", got)
	}
	intentFields := vmRecs[0].Body["fields"].(map[string]any)
	layout := layoutEntityOf(intentFields)
	if layout == "" {
		t.Fatalf("vm intent should reference its layout: %v", intentFields)
	}
	for _, r := range vmRecs {
		f := r.Body["fields"].(map[string]any)
		if _, ok := f["storage"]; ok {
			t.Errorf("%s vm record still carries storage: %v", r.State, f["storage"])
		}
		if layoutEntityOf(f) != layout {
			t.Errorf("%s vm record references %q, want %q", r.State, f["layout_ref"], layout)
		}
	}
	vmReq := vmRecs[1].Body
	if got := fmt.Sprint(vmReq["dispatch"].(map[string]any)["admitted"]); strings.Contains(got, "layout_ref") {
		t.Errorf("the layout reference did not cross to the provider, so the receipt must not list it: %s", got)
	}
	edge := fmt.Sprint(vmReq["dependencies"])
	if !strings.Contains(edge, layout) || !strings.Contains(edge, "storage_layout") {
		t.Errorf("vm requested record should carry the storage_layout edge: %s", edge)
	}

	layoutRecs, _ := st.ListByEntity(ctx, layout)
	// The second VM report changes only the VM's outputs; the layout has none, so
	// it is not written again.
	if got := statesOf(layoutRecs); got != "Intent,Requested,Realized" {
		t.Fatalf("layout states = %s", got)
	}
	for _, r := range layoutRecs {
		if r.ResourceType != "StorageLayout" {
			t.Errorf("layout record resource_type = %s", r.ResourceType)
		}
		if err := Validate(r.Body); err != nil {
			t.Errorf("layout %s record invalid: %v", r.State, err)
		}
		if err := Verify(r.Body); err != nil {
			t.Errorf("layout %s record does not verify: %v", r.State, err)
		}
	}
	if layoutRecs[0].Body["metadata"].(map[string]any)["display_name"] != "app-01 disks" {
		t.Errorf("layout display name = %v", layoutRecs[0].Body["metadata"])
	}
	entries := fmt.Sprint(layoutRecs[0].Body["fields"].(map[string]any)["entries"])
	if entries != "[map[name:boot role:boot size:100GB] map[name:data size:500GB]]" {
		t.Errorf("layout entries = %s", entries)
	}
	req := layoutRecs[1].Body
	// DEP-015: the layout is a child of the VM's order, so it names the VM's
	// requested record as its root.
	if req["root_request_uuid"] != vmReq["record_uuid"] {
		t.Errorf("layout root_request_uuid = %v, want the vm's requested record %v", req["root_request_uuid"], vmReq["record_uuid"])
	}
	d := req["dispatch"].(map[string]any)
	if d["provider"] != "dcm/agents/agent-a" || fmt.Sprint(d["admitted"]) != "[entries[0].name entries[0].role entries[0].size entries[1].name entries[1].size]" {
		t.Errorf("layout receipt = %v", d)
	}
	if out := layoutRecs[2].Body["outputs"].(map[string]any); len(out) != 0 {
		t.Errorf("the agent reports no volumes, so the layout's outputs must be empty: %v", out)
	}
}

func TestWriterRefusesDisksItCannotPlace(t *testing.T) {
	ctx := context.Background()
	cases := map[string]map[string]any{
		"extra disk field": vmWithDisks(map[string]any{"name": "boot", "capacity": "10GB", "bus": "virtio"}),
		"disks and a layout_ref": func() map[string]any {
			s := vmWithDisks(disk("boot", "10GB"))
			s["layout_ref"] = "cexample/layouts/web"
			return s
		}(),
	}
	for name, spec := range cases {
		t.Run(name, func(t *testing.T) {
			w, st := testWriter(t)
			vm := "aa7c9a41-8e3d-4b6a-9c15-7d2e4f8a1b03"
			w.Intent(ctx, IntentInput{EntityUUID: vm, Spec: spec})
			if recs, _ := st.ListByEntity(ctx, vm); len(recs) != 0 {
				t.Fatalf("want no record, got %d", len(recs))
			}
		})
	}
}

func statesOf(recs []StateRecord) string {
	s := make([]string, len(recs))
	for i := range recs {
		s[i] = recs[i].State
	}
	return strings.Join(s, ",")
}
