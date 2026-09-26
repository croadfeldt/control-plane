package records

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"
)

func loadJSON(t *testing.T, name string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	var v map[string]any
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	return v
}

// The registry's worked example (registry/examples/example-vm-app01-*-record.yaml)
// folded by registry/tools/entity_view.py build_views on 2026-09-26 is
// testdata/app01-view.json. The Go fold must produce the same document.
func TestFoldMatchesRegistryTool(t *testing.T) {
	records := []map[string]any{loadJSON(t, "app01-intent.json"), loadJSON(t, "app01-requested.json"), loadJSON(t, "app01-realized.json")}
	for i := range records {
		if err := Validate(records[i]); err != nil {
			t.Fatalf("example record %d is not valid against the vendored schema: %v", i, err)
		}
	}
	got := Fold(records)
	want := loadJSON(t, "app01-view.json")
	rt, err := roundTrip(got)
	if err != nil {
		t.Fatal(err)
	}
	gotRT := rt.(map[string]any)
	for k := range want {
		if !reflect.DeepEqual(gotRT[k], want[k]) {
			t.Errorf("view[%q] differs from the registry tool:\n got: %v\nwant: %v", k, gotRT[k], want[k])
		}
	}
	for k := range gotRT {
		if _, ok := want[k]; !ok {
			t.Errorf("view carries %q, which the registry tool does not produce", k)
		}
	}
	if err := ValidateView(got); err != nil {
		t.Errorf("view does not validate against entity-view.schema.json: %v", err)
	}
	if got["lifecycle_state"] != "Realized" || got["observed_generation"] != float64(1) {
		t.Errorf("lifecycle %v observed_generation %v", got["lifecycle_state"], got["observed_generation"])
	}
}

func TestFoldPartialChains(t *testing.T) {
	intent := loadJSON(t, "app01-intent.json")
	requested := loadJSON(t, "app01-requested.json")
	if v := Fold([]map[string]any{intent}); v["lifecycle_state"] != "Intent" || v["states"].(map[string]any)["intent"] == nil {
		t.Errorf("intent-only view: %v", v["lifecycle_state"])
	}
	v := Fold([]map[string]any{intent, requested})
	if v["lifecycle_state"] != "Requested" {
		t.Errorf("intent+requested view lifecycle = %v", v["lifecycle_state"])
	}
	if _, ok := v["observed_generation"]; ok {
		t.Error("observed_generation only exists once a realized record does")
	}
	if err := ValidateView(v); err != nil {
		t.Errorf("partial view must still validate: %v", err)
	}
	if Fold(nil) != nil || Fold([]map[string]any{{"record_type": "class"}}) != nil {
		t.Error("no state records means no view")
	}
	if LifecycleState([]map[string]any{intent, requested}) != "Requested" {
		t.Error("LifecycleState")
	}
}
