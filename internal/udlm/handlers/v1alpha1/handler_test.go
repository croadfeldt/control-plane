package v1alpha1_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
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
	if err := db.AutoMigrate(&records.StateRecord{}); err != nil {
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
		return records.Type{ResourceType: "Machine.VM", Version: "2.0.0"}, true
	}
	w := records.NewWriter(st, lookup, "75ccf4ff-3e8d-4963-bc51-459ae1014cb7", slog.Default())
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
