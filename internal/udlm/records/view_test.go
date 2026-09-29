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
	records := []map[string]any{loadJSON(t, "app01-intent.json"), loadJSON(t, "app01-requested.json"), loadJSON(t, "app01-realized.json"), loadJSON(t, "app01-discovered.json")}
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
	// The requested snapshot carries the receipt and the root (rows 079/081).
	snap := got["states"].(map[string]any)["requested"].(map[string]any)
	if snap["dispatch"] == nil || snap["root_request_uuid"] == nil {
		t.Errorf("requested snapshot must carry dispatch and root_request_uuid: %v", snap)
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

// RHY-007: a discovered record carries the edges a sweep observed, named by the
// natural key the probe read. The view takes `dependencies` realized → requested →
// discovered, so a discovered-only entity shows its observed edges and an entity
// with a realized record shows the declared ones (drift is the comparison, §6).
func TestFoldObservedEdges(t *testing.T) {
	discovered := loadJSON(t, "app01-discovered.json")
	if err := Validate(discovered); err != nil {
		t.Fatalf("discovered record with an observed edge must validate: %v", err)
	}
	v := Fold([]map[string]any{discovered})
	if v["lifecycle_state"] != "Discovered" {
		t.Errorf("discovered-only lifecycle = %v", v["lifecycle_state"])
	}
	// Two observed edges: one resolved (target_uuid present), one not yet (key only).
	deps, _ := v["dependencies"].([]any)
	if len(deps) != 2 || deps[0].(map[string]any)["target_key"] == nil || deps[1].(map[string]any)["target_uuid"] != nil {
		t.Errorf("discovered-only view must carry both observed edges, the second unresolved: %v", v["dependencies"])
	}
	if err := ValidateView(v); err != nil {
		t.Errorf("discovered-only view must validate: %v", err)
	}

	realized := loadJSON(t, "app01-realized.json")
	v = Fold([]map[string]any{realized, discovered})
	deps, _ = v["dependencies"].([]any)
	if len(deps) == 0 || deps[0].(map[string]any)["strength"] == nil {
		t.Errorf("with a realized record the view takes the declared edges, not the observed ones: %v", v["dependencies"])
	}

	// The declared semantics are refused on an observed edge.
	bad := loadJSON(t, "app01-discovered.json")
	bad["dependencies"].([]any)[0].(map[string]any)["strength"] = "hard"
	if err := Validate(bad); err == nil {
		t.Error("an observed edge carrying `strength` must be refused (RHY-007)")
	}
}
