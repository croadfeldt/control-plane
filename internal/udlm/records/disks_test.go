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

// Upstream's storage.disks[] lands on the machine's own disks element (UDLM row
// 112): one record, no companion, the disks on the receipt.
func TestWriterMapsVMDisksOntoTheMachine(t *testing.T) {
	ctx := context.Background()
	w, st := testWriter(t)
	vm := "9a7c9a41-8e3d-4b6a-9c15-7d2e4f8a1b03"
	spec := vmWithDisks(disk("boot", "100GB"), disk("data", "500GB"))
	w.Intent(ctx, IntentInput{EntityUUID: vm, Spec: spec, Name: "app-01"})
	w.Requested(ctx, RequestedInput{EntityUUID: vm, Spec: spec, AgentName: "agent-a"})
	w.Realized(ctx, RealizedInput{EntityUUID: vm, Outputs: map[string]any{"primary_ip": "192.0.2.10"}, AgentName: "agent-a"})

	recs, _ := st.ListByEntity(ctx, vm)
	if got := statesOf(recs); got != "Intent,Requested,Realized" {
		t.Fatalf("vm states = %s", got)
	}
	for _, r := range recs {
		f := r.Body["fields"].(map[string]any)
		if _, ok := f["storage"]; ok {
			t.Errorf("%s record still carries storage: %v", r.State, f["storage"])
		}
		if got := fmt.Sprint(f["disks"]); got != "[map[name:boot role:boot size:100GB] map[name:data size:500GB]]" {
			t.Errorf("%s record disks = %s", r.State, got)
		}
		if err := Validate(r.Body); err != nil {
			t.Errorf("%s record invalid: %v", r.State, err)
		}
	}
	d := recs[1].Body["dispatch"].(map[string]any)
	if got := fmt.Sprint(d["admitted"]); !strings.Contains(got, "disks[0].name disks[0].role disks[0].size disks[1].name disks[1].size") {
		t.Errorf("receipt should admit the disks: %s", got)
	}
	if _, ok := recs[1].Body["dependencies"]; ok {
		t.Errorf("no companion means no edge: %v", recs[1].Body["dependencies"])
	}
	entities, _, _ := st.ListEntities(ctx, 10, "")
	if len(entities) != 1 {
		t.Errorf("want one entity, got %d", len(entities))
	}
}

func TestWriterRefusesDisksItCannotPlace(t *testing.T) {
	ctx := context.Background()
	cases := map[string]map[string]any{
		"extra disk field": vmWithDisks(map[string]any{"name": "boot", "capacity": "10GB", "bus": "virtio"}),
		"storage.disks and disks": func() map[string]any {
			s := vmWithDisks(disk("boot", "10GB"))
			s["disks"] = []any{map[string]any{"name": "root", "size": "10GB"}}
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
