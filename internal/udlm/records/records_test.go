package records

import (
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Parity vectors computed with croadfeldt/udlm registry/tools/integrity_chain.py
// head_for() on 2026-09-26. If these move, the two canonicalizers disagree.
var vectorRecord = map[string]any{
	"record_type": "intent_record",
	"state":       "Intent",
	"fields": map[string]any{
		"cpu":    map[string]any{"count": 4},
		"memory": map[string]any{"size": "16Gi"},
		"flag":   true,
		"n":      nil,
		"list":   []any{1, "a", 2.5},
	},
	"at": "2026-09-26T00:00:00Z",
}

func TestHeadMatchesRegistryCanonicalizer(t *testing.T) {
	rt, err := roundTrip(vectorRecord)
	if err != nil {
		t.Fatal(err)
	}
	rec := rt.(map[string]any)
	root, err := Head(rec, nil)
	if err != nil {
		t.Fatal(err)
	}
	if root != "sha256:b13a9ec6b4acc28e665d29560f8f2522913556f236c04a7af8f377a8012aa0e5" {
		t.Errorf("root head = %s", root)
	}
	prev := "sha256:0000000000000000000000000000000000000000000000000000000000000000"
	linked, err := Head(rec, &prev)
	if err != nil {
		t.Fatal(err)
	}
	if linked != "sha256:2dc330015656482765061540adfaf8ce873125e3a7f4e250ac9e21675c00ba51" {
		t.Errorf("linked head = %s", linked)
	}
	rec["integrity"] = map[string]any{"algorithm": Algorithm, "head": "x", "previous": nil}
	again, err := Head(rec, nil)
	if err != nil {
		t.Fatal(err)
	}
	if again != root {
		t.Errorf("head must exclude integrity itself: %s != %s", again, root)
	}
	if err := Seal(rec, nil); err != nil {
		t.Fatal(err)
	}
	if err := Verify(rec); err != nil {
		t.Errorf("sealed record should verify: %v", err)
	}
	rec["fields"].(map[string]any)["cpu"] = map[string]any{"count": 8}
	if err := Verify(rec); err == nil {
		t.Error("tampered record should not verify")
	}
}

func TestJCSRefusals(t *testing.T) {
	for name, v := range map[string]any{
		"non-ascii key": map[string]any{"ключ": 1},
		"big int":       map[string]any{"n": float64(1 << 54)},
		"exponent":      map[string]any{"n": 1e-9},
	} {
		if _, err := jcsBytes(v); err == nil {
			t.Errorf("%s should be refused", name)
		}
	}
	b, err := jcsBytes(map[string]any{"b": "x\ny\"z", "a": 1.0, "c": []any{true, nil}})
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != `{"a":1,"b":"x\ny\"z","c":[true,null]}` {
		t.Errorf("canonical form = %s", b)
	}
}

func TestValidateRejectsWrongShape(t *testing.T) {
	bad := map[string]any{
		"record_type": "realized_record", "state": "Realized",
		"entity_uuid": "2f7c9a41-8e3d-4b6a-9c15-7d2e4f8a1b03", "record_uuid": "01a09398-433d-756d-a0a3-f5fee7023864",
		"tenant_uuid": "75ccf4ff-3e8d-4963-bc51-459ae1014cb7", "conforms_to": "udlm/0.1",
		"resource_type": "Machine.VM", "type_version": "2.0.0", "at": "2026-09-26T00:00:00Z",
		"time_source": "test", "fields": map[string]any{},
	}
	err := Validate(bad)
	if err == nil || !strings.Contains(err.Error(), "outputs") && !strings.Contains(err.Error(), "required") {
		t.Errorf("realized record without outputs/requested_ref/provider must fail, got %v", err)
	}
}

func testWriter(t *testing.T) (*Writer, Store) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&StateRecord{}); err != nil {
		t.Fatal(err)
	}
	st := NewStore(db)
	types := func(s string) (Type, bool) {
		if s == "vm" {
			return Type{ResourceType: "Machine.VM", Version: "2.0.0"}, true
		}
		return Type{}, false
	}
	w := NewWriter(st, types, "75ccf4ff-3e8d-4963-bc51-459ae1014cb7", slog.Default())
	base := time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC)
	w.now = func() time.Time { base = base.Add(time.Second); return base }
	return w, st
}

func TestWriterChain(t *testing.T) {
	ctx := context.Background()
	w, st := testWriter(t)
	entity := "2f7c9a41-8e3d-4b6a-9c15-7d2e4f8a1b03"
	spec := map[string]any{
		"service_type": "vm", "metadata": map[string]any{"name": "app-01"},
		"cpu": map[string]any{"count": 4}, "memory": map[string]any{"size": "16Gi"},
	}
	w.Intent(ctx, IntentInput{EntityUUID: entity, Spec: spec, Name: "app-01"})
	evaluated := map[string]any{"service_type": "vm", "cpu": map[string]any{"count": 4, "sockets": 1}, "memory": map[string]any{"size": "16Gi"}}
	w.Requested(ctx, RequestedInput{EntityUUID: entity, Spec: evaluated, AgentName: "agent-a"})
	w.Realized(ctx, RealizedInput{EntityUUID: entity, Outputs: map[string]any{"primary_ip": "192.0.2.55"}, AgentName: "agent-a"})
	w.Realized(ctx, RealizedInput{EntityUUID: entity, Outputs: map[string]any{"primary_ip": "192.0.2.56"}, AgentName: "agent-a"})

	recs, err := st.ListByEntity(ctx, entity)
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 4 {
		t.Fatalf("expected intent, requested, realized, realized; got %d records", len(recs))
	}
	states := []string{recs[0].State, recs[1].State, recs[2].State, recs[3].State}
	if strings.Join(states, ",") != "Intent,Requested,Realized,Realized" {
		t.Errorf("states = %v", states)
	}
	// Every record validates and verifies. The chain is per state stream: the first
	// record of each state is a root (previous null); a superseding record's
	// previous is the head of the record it replaces.
	for i := range recs {
		body := recs[i].Body
		if err := Validate(body); err != nil {
			t.Errorf("record %d (%s) invalid: %v", i, recs[i].State, err)
		}
		if err := Verify(body); err != nil {
			t.Errorf("record %d (%s) does not verify: %v", i, recs[i].State, err)
		}
	}
	for i := 0; i < 3; i++ {
		if p := recs[i].Body["integrity"].(map[string]any)["previous"]; p != nil {
			t.Errorf("first %s record must be a chain root, previous = %v", recs[i].State, p)
		}
	}
	if p := recs[3].Body["integrity"].(map[string]any)["previous"]; p != recs[2].Head {
		t.Errorf("superseding realized record previous = %v, want the replaced realized head %s", p, recs[2].Head)
	}
	intent, requested, realized, realized2 := recs[0].Body, recs[1].Body, recs[2].Body, recs[3].Body
	if _, present := intent["fields"].(map[string]any)["service_type"]; present {
		t.Error("envelope key service_type must not land in fields")
	}
	if intent["metadata"].(map[string]any)["display_name"] != "app-01" {
		t.Error("intent should carry the display name")
	}
	if requested["intent_ref"] != intent["record_uuid"] {
		t.Error("requested must reference the intent record")
	}
	if realized["requested_ref"] != requested["record_uuid"] {
		t.Error("realized must reference the requested record")
	}
	if realized["provider"] != "dcm/agents/agent-a" {
		t.Errorf("realized provider = %v", realized["provider"])
	}
	if realized["fields"].(map[string]any)["cpu"].(map[string]any)["sockets"] != float64(1) {
		t.Error("realized fields must be the requested (evaluated) fields")
	}
	if realized2["supersedes"].([]any)[0] != realized["record_uuid"] || realized2["generation"] != float64(1) {
		t.Errorf("second realized must supersede the first within the same request cycle: %v gen %v", realized2["supersedes"], realized2["generation"])
	}
	if requested["intent_ref_head"] != intent["integrity"].(map[string]any)["head"] {
		t.Error("requested must carry the referenced intent's head (TRN-002)")
	}
	if realized["requested_ref_head"] != requested["integrity"].(map[string]any)["head"] {
		t.Error("realized must carry the referenced requested's head (TRN-002)")
	}
	if realized2["outputs"].(map[string]any)["primary_ip"] != "192.0.2.56" {
		t.Error("second realized must carry the new outputs")
	}
}

func TestWriterSkipsRepeatedRealizedReport(t *testing.T) {
	ctx := context.Background()
	w, st := testWriter(t)
	entity := "5f7c9a41-8e3d-4b6a-9c15-7d2e4f8a1b03"
	spec := map[string]any{"service_type": "vm", "cpu": map[string]any{"count": 1}}
	w.Intent(ctx, IntentInput{EntityUUID: entity, Spec: spec})
	w.Requested(ctx, RequestedInput{EntityUUID: entity, Spec: spec, AgentName: "a"})
	out := map[string]any{"primary_ip": "10.0.0.1", "ip_addresses": []any{"10.0.0.1"}}
	w.Realized(ctx, RealizedInput{EntityUUID: entity, Outputs: out, AgentName: "a"})
	w.Realized(ctx, RealizedInput{EntityUUID: entity, Outputs: out, AgentName: "a"}) // retried callback
	w.Realized(ctx, RealizedInput{EntityUUID: entity, Outputs: map[string]any{"primary_ip": "10.0.0.2", "ip_addresses": []any{"10.0.0.2"}}, AgentName: "a"})
	recs, _ := st.ListByEntity(ctx, entity)
	if len(recs) != 4 {
		t.Fatalf("a repeated report must not add a record: got %d records", len(recs))
	}
	if recs[3].Body["supersedes"] == nil || recs[3].Body["generation"] != float64(1) {
		t.Errorf("the changed report must supersede within the same cycle: supersedes %v generation %v", recs[3].Body["supersedes"], recs[3].Body["generation"])
	}
}

func TestWriterDropsWhatItCannotType(t *testing.T) {
	ctx := context.Background()
	w, st := testWriter(t)
	entity := "3f7c9a41-8e3d-4b6a-9c15-7d2e4f8a1b03"
	w.Intent(ctx, IntentInput{EntityUUID: entity, Spec: map[string]any{"service_type": "network"}})
	w.Realized(ctx, RealizedInput{EntityUUID: entity, AgentName: "a"}) // no requested record to reference
	recs, _ := st.ListByEntity(ctx, entity)
	if len(recs) != 0 {
		t.Errorf("unmapped service type and orphan realized must be dropped, got %d", len(recs))
	}
	var nilWriter *Writer
	nilWriter.Intent(ctx, IntentInput{}) // nil-safe
}
