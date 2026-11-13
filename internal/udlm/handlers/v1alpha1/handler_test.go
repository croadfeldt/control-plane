package v1alpha1_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	server "github.com/dcm-project/control-plane/internal/udlm/api/server"
	handlers "github.com/dcm-project/control-plane/internal/udlm/handlers/v1alpha1"
	"github.com/dcm-project/control-plane/internal/udlm/records"
)

const entity = "2f7c9a41-8e3d-4b6a-9c15-7d2e4f8a1b03"

func newServer(t *testing.T) (http.Handler, records.Store) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&records.StateRecord{}, &records.EntityKey{}); err != nil {
		t.Fatal(err)
	}
	st := records.NewStore(db)
	router := chi.NewRouter()
	server.HandlerFromMuxWithBaseURL(server.NewStrictHandler(handlers.NewHandler(st), nil), router, "/api/v1alpha1")
	return router, st
}

func seed(t *testing.T, st records.Store) {
	t.Helper()
	ctx := context.Background()
	lookup := func(string) (records.Type, bool) {
		return records.Type{ResourceType: "Machine.VM", Version: "2.0.0", Elements: []string{"cpu", "memory", "networks", "guest_os", "firmware", "layout_ref", "placement", "run_state", "storage", "storage_tier", "boot_order", "instance_size"}}, true
	}
	w := records.NewWriter(st, lookup, nil, "75ccf4ff-3e8d-4963-bc51-459ae1014cb7", slog.Default())
	spec := map[string]any{"service_type": "vm", "cpu": map[string]any{"count": 4}}
	w.Intent(ctx, records.IntentInput{EntityUUID: entity, Spec: spec, Name: "app-01"})
	w.Requested(ctx, records.RequestedInput{EntityUUID: entity, Spec: spec, AgentName: "agent-a"})
	w.Realized(ctx, records.RealizedInput{EntityUUID: entity, Outputs: map[string]any{"primary_ip": "192.0.2.55"}, AgentName: "agent-a"})
}

func get(t *testing.T, h http.Handler, path string) (int, map[string]any) {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	var body map[string]any
	if rec.Body.Len() > 0 {
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("%s: body is not JSON: %v: %s", path, err, rec.Body.String())
		}
	}
	return rec.Code, body
}

func TestEntityViewAndRecords(t *testing.T) {
	h, st := newServer(t)
	seed(t, st)

	code, view := get(t, h, "/api/v1alpha1/udlm/entities/"+entity)
	if code != http.StatusOK {
		t.Fatalf("view: %d %v", code, view)
	}
	if view["lifecycle_state"] != "Realized" || view["uuid"] != entity || view["resource_type"] != "Machine.VM" {
		t.Errorf("view envelope: %v %v %v", view["lifecycle_state"], view["uuid"], view["resource_type"])
	}
	states := view["states"].(map[string]any)
	for _, s := range []string{"intent", "requested", "realized"} {
		if states[s] == nil {
			t.Errorf("view lacks states.%s", s)
		}
	}
	if states["realized"].(map[string]any)["outputs"].(map[string]any)["primary_ip"] != "192.0.2.55" {
		t.Error("realized snapshot should carry the outputs")
	}
	if view["integrity"] == nil || view["metadata"] == nil {
		t.Error("carried blocks (integrity from realized, metadata from the first state that has it) missing")
	}
	if err := records.ValidateView(view); err != nil {
		t.Errorf("served view does not validate against entity-view.schema.json: %v", err)
	}

	code, list := get(t, h, "/api/v1alpha1/udlm/entities/"+entity+"/records")
	if code != http.StatusOK {
		t.Fatalf("records: %d %v", code, list)
	}
	recs := list["records"].([]any)
	if len(recs) != 3 {
		t.Fatalf("expected 3 records, got %d", len(recs))
	}
	for i, r := range recs {
		body := r.(map[string]any)
		if err := records.Validate(body); err != nil {
			t.Errorf("served record %d does not validate: %v", i, err)
		}
		if err := records.Verify(body); err != nil {
			t.Errorf("served record %d does not verify: %v", i, err)
		}
	}
	if recs[0].(map[string]any)["state"] != "Intent" || recs[2].(map[string]any)["state"] != "Realized" {
		t.Error("records must be oldest first")
	}

	code, page := get(t, h, "/api/v1alpha1/udlm/entities?max_page_size=10")
	if code != http.StatusOK {
		t.Fatalf("list: %d %v", code, page)
	}
	entities := page["entities"].([]any)
	if len(entities) != 1 {
		t.Fatalf("expected one entity, got %d", len(entities))
	}
	first := entities[0].(map[string]any)
	if first["entity_uuid"] != entity || first["lifecycle_state"] != "Realized" || first["record_count"] != float64(3) {
		t.Errorf("summary: %v", first)
	}
}

func TestNotFound(t *testing.T) {
	h, _ := newServer(t)
	for _, path := range []string{
		"/api/v1alpha1/udlm/entities/2f7c9a41-8e3d-4b6a-9c15-7d2e4f8a1b03",
		"/api/v1alpha1/udlm/entities/2f7c9a41-8e3d-4b6a-9c15-7d2e4f8a1b03/records",
	} {
		code, body := get(t, h, path)
		if code != http.StatusNotFound || body["type"] != "not-found" {
			t.Errorf("%s: %d %v", path, code, body)
		}
	}
	code, page := get(t, h, "/api/v1alpha1/udlm/entities")
	if code != http.StatusOK || len(page["entities"].([]any)) != 0 {
		t.Errorf("empty list: %d %v", code, page)
	}
}

const tenant = "75ccf4ff-3e8d-4963-bc51-459ae1014cb7"

// observation is a host as a discovery sweep reports it: named by its machine
// id, with one edge to a disk named by its WWN.
func observation(at string) map[string]any {
	return map[string]any{
		"tenant_uuid":   tenant,
		"conforms_to":   "udlm/1.0",
		"resource_type": "Machine",
		"type_version":  "2.6.0",
		"at":            at,
		"time_source":   "discovery-sweep",
		"origin":        "discovered_derived",
		"fields":        map[string]any{"hostname": "host-01"},
		"correlation_ids": []any{
			map[string]any{"scheme": "machine_id", "value": "4c4c4544004d3610805ac4c04f4d3632"},
		},
		"dependencies": []any{
			map[string]any{"edge_type": "contained_by", "target_key": map[string]any{"scheme": "wwn", "value": "0x5000c500a1b2c3d4"}},
		},
	}
}

func post(t *testing.T, h http.Handler, body map[string]any) (int, map[string]any) {
	t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1alpha1/udlm/discovered", strings.NewReader(string(b)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("body is not JSON: %v: %s", err, rec.Body.String())
	}
	return rec.Code, out
}

func TestDiscoveredIngest(t *testing.T) {
	h, _ := newServer(t)

	// A first observation issues the entity and roots its Discovered chain.
	code, first := post(t, h, observation("2026-11-13T04:00:00Z"))
	if code != http.StatusCreated {
		t.Fatalf("first: %d %v", code, first)
	}
	id := first["entity_uuid"].(string)
	if first["record_type"] != "discovered_record" || first["state"] != "Discovered" || first["generation"] != float64(1) {
		t.Errorf("first envelope: %v", first)
	}
	if first["integrity"].(map[string]any)["previous"] != nil || first["supersedes"] != nil {
		t.Errorf("the first record is a chain root and supersedes nothing: %v %v", first["integrity"], first["supersedes"])
	}
	if first["fields"].(map[string]any)["hostname"] != "host-01" || first["dependencies"] == nil {
		t.Errorf("the response must carry the whole record: %v", first)
	}

	// The same observation again stores nothing.
	code, again := post(t, h, observation("2026-11-13T04:00:00Z"))
	if code != http.StatusOK || again["record_uuid"] != first["record_uuid"] {
		t.Errorf("a repeat must return the stored record with 200: %d %v", code, again["record_uuid"])
	}

	// A later observation of the same key lands on the same entity and chains on.
	code, second := post(t, h, observation("2026-11-14T04:00:00Z"))
	if code != http.StatusCreated {
		t.Fatalf("second: %d %v", code, second)
	}
	if second["entity_uuid"] != id {
		t.Errorf("the same natural key must reach the same entity: %v != %v", second["entity_uuid"], id)
	}
	if second["generation"] != float64(2) ||
		second["integrity"].(map[string]any)["previous"] != first["integrity"].(map[string]any)["head"] ||
		second["supersedes"].([]any)[0] != first["record_uuid"] {
		t.Errorf("the second record must supersede and chain onto the first: %v", second)
	}

	code, list := get(t, h, "/api/v1alpha1/udlm/entities/"+id+"/records")
	if code != http.StatusOK {
		t.Fatalf("records: %d %v", code, list)
	}
	recs := list["records"].([]any)
	if len(recs) != 2 {
		t.Fatalf("expected 2 stored records, got %d", len(recs))
	}
	for i, r := range recs {
		if err := records.Validate(r.(map[string]any)); err != nil {
			t.Errorf("stored record %d does not validate: %v", i, err)
		}
		if err := records.Verify(r.(map[string]any)); err != nil {
			t.Errorf("stored record %d does not verify: %v", i, err)
		}
	}
	code, view := get(t, h, "/api/v1alpha1/udlm/entities/"+id)
	if code != http.StatusOK || view["lifecycle_state"] != "Discovered" {
		t.Errorf("view: %d %v", code, view["lifecycle_state"])
	}

	// The same key in another class or another tenant is another entity.
	other := observation("2026-11-13T04:00:00Z")
	other["tenant_uuid"] = "791e6713-656c-44bf-8a4b-865804eb0872"
	if code, rec := post(t, h, other); code != http.StatusCreated || rec["entity_uuid"] == id {
		t.Errorf("another tenant must get its own entity: %d %v", code, rec["entity_uuid"])
	}
}

func TestDiscoveredIngestRefusals(t *testing.T) {
	h, _ := newServer(t)
	for name, change := range map[string]func(map[string]any){
		"entity_uuid supplied": func(o map[string]any) { o["entity_uuid"] = entity },
		"integrity supplied":   func(o map[string]any) { o["integrity"] = map[string]any{"head": "sha256:00"} },
		"another state":        func(o map[string]any) { o["state"] = "Realized" },
		"no natural key":       func(o map[string]any) { o["correlation_ids"] = []any{} },
		"no tenant":            func(o map[string]any) { delete(o, "tenant_uuid") },
		"unknown key scheme":   func(o map[string]any) { o["correlation_ids"].([]any)[0].(map[string]any)["scheme"] = "nickname" },
		"an edge with a relation": func(o map[string]any) {
			o["dependencies"].([]any)[0].(map[string]any)["relation"] = "boot_disk"
		},
	} {
		o := observation("2026-11-13T04:00:00Z")
		change(o)
		code, body := post(t, h, o)
		if code != http.StatusBadRequest || body["type"] != "invalid-observation" {
			t.Errorf("%s: want 400 invalid-observation, got %d %v", name, code, body)
		}
	}
	if code, page := get(t, h, "/api/v1alpha1/udlm/entities"); code != http.StatusOK || len(page["entities"].([]any)) != 0 {
		t.Errorf("a refused observation must store nothing: %v", page)
	}
}

func TestDiscoveredIngestAmbiguousKeys(t *testing.T) {
	h, _ := newServer(t)
	a := observation("2026-11-13T04:00:00Z")
	b := observation("2026-11-13T04:00:00Z")
	b["correlation_ids"] = []any{map[string]any{"scheme": "serial_number", "value": "SN-0001"}}
	for _, o := range []map[string]any{a, b} {
		if code, body := post(t, h, o); code != http.StatusCreated {
			t.Fatalf("setup: %d %v", code, body)
		}
	}
	// One observation carrying both keys would join two entities; the store refuses to choose.
	both := observation("2026-11-15T04:00:00Z")
	both["correlation_ids"] = append(both["correlation_ids"].([]any), map[string]any{"scheme": "serial_number", "value": "SN-0001"})
	if code, body := post(t, h, both); code != http.StatusConflict || body["type"] != "ambiguous-entity" {
		t.Errorf("want 409 ambiguous-entity, got %d %v", code, body)
	}
}
