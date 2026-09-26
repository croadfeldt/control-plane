package records

import (
	"sort"
)

// The entity view is the merged read model, computed from an entity's per-state
// records and never stored (registry ENTITY-VIEW.md, ruling 071). This is a
// port of registry/tools/entity_view.py `_fold`, kept line for line so a view
// computed here equals one computed by the registry's tool on the same records
// (view_test.go checks that against the registry's worked example).

// stateOf maps record_type to the view's states key.
var stateOf = map[string]string{
	"intent_record":     "intent",
	"requested_record":  "requested",
	"realized_record":   "realized",
	"discovered_record": "discovered",
}

// snapshotKeys are the record keys a state's snapshot carries in the view.
var snapshotKeys = []string{"fields", "outputs", "at", "time_source", "origin", "provider", "roles", "assembly", "policies"}

// carried lists the view keys that live beside `fields` on a record, and which
// state's record the view takes each from, in preference order.
var carried = []struct {
	key   string
	order []string
}{
	{"status", []string{"realized", "requested", "discovered"}},
	{"dependencies", []string{"realized", "requested"}},
	{"sovereignty", []string{"realized"}},
	{"correlation_ids", []string{"realized", "discovered"}},
	{"adopted_standards", []string{"realized"}},
	{"portability", []string{"realized"}},
	{"expected_observation", []string{"realized"}},
	{"process", []string{"realized", "requested"}},
	{"priced_by", []string{"realized", "requested"}},
	{"constituents", []string{"realized", "requested"}},
	{"provider_preference", []string{"requested"}},
	{"metadata", []string{"realized", "requested", "intent", "discovered"}},
	{"integrity", []string{"realized"}},
}

// envelopeFromHead are the envelope keys the view copies from its head record.
var envelopeFromHead = []string{"handle", "tenant_uuid", "conforms_to", "resource_type", "type_version", "type_ref", "type_digest", "generation"}

// lifecycleOrder decides lifecycle_state: the most-realized state present wins.
var lifecycleOrder = []struct{ state, name string }{
	{"realized", "Realized"}, {"requested", "Requested"}, {"intent", "Intent"}, {"discovered", "Discovered"},
}

// Fold computes the entity view from one entity's records (bodies as stored).
// It returns nil when no record is a state record.
func Fold(records []map[string]any) map[string]any {
	byState := map[string][]map[string]any{}
	var stateOrder []string // first-appearance order, as the Python dict keeps it
	for _, r := range records {
		st, ok := stateOf[stringOf(r["record_type"])]
		if !ok {
			continue
		}
		if _, seen := byState[st]; !seen {
			stateOrder = append(stateOrder, st)
		}
		byState[st] = append(byState[st], r)
	}
	if len(byState) == 0 {
		return nil
	}
	latest := map[string]map[string]any{}
	for st, rs := range byState {
		latest[st] = latestOf(rs)
	}
	var head map[string]any
	for _, st := range []string{"realized", "requested", "intent", "discovered"} {
		if r, ok := latest[st]; ok {
			head = r
			break
		}
	}
	entity := stringOf(head["entity_uuid"])
	view := map[string]any{
		"uuid":        entity,
		"entity_uuid": entity,
		"states":      map[string]any{},
		"provenance":  map[string]any{},
	}
	for _, k := range envelopeFromHead {
		if v, ok := head[k]; ok {
			view[k] = v
		}
	}
	for _, lc := range lifecycleOrder {
		if _, ok := latest[lc.state]; ok {
			view["lifecycle_state"] = lc.name
			break
		}
	}
	states := view["states"].(map[string]any)
	provenance := view["provenance"].(map[string]any)
	for _, st := range stateOrder {
		rec := latest[st]
		snap := map[string]any{}
		for _, k := range snapshotKeys {
			if v, ok := rec[k]; ok {
				snap[k] = v
			}
		}
		states[st] = snap
		if prov, ok := rec["provenance"].(map[string]any); ok {
			for path, entries := range prov {
				list, _ := entries.([]any)
				existing, _ := provenance[path].([]any)
				provenance[path] = append(existing, list...)
			}
		}
	}
	for _, c := range carried {
		for _, st := range c.order {
			if rec, ok := latest[st]; ok {
				if v, ok := rec[c.key]; ok {
					view[c.key] = v
					break
				}
			}
		}
	}
	if _, ok := view["generation"]; ok {
		if realized, ok := latest["realized"]; ok {
			if g, ok := realized["generation"]; ok {
				view["observed_generation"] = g
			} else {
				view["observed_generation"] = view["generation"]
			}
		}
	}
	return view
}

// latestOf picks the latest record of one state: highest record_uuid (v7 is
// time-ordered), `at` as the tiebreak.
func latestOf(rs []map[string]any) map[string]any {
	sorted := make([]map[string]any, len(rs))
	copy(sorted, rs)
	sort.SliceStable(sorted, func(i, j int) bool {
		a, b := sorted[i], sorted[j]
		if ua, ub := stringOf(a["record_uuid"]), stringOf(b["record_uuid"]); ua != ub {
			return ua < ub
		}
		return stringOf(a["at"]) < stringOf(b["at"])
	})
	return sorted[len(sorted)-1]
}

// LifecycleState returns the lifecycle state a set of records puts the entity in.
func LifecycleState(records []map[string]any) string {
	present := map[string]bool{}
	for _, r := range records {
		if st, ok := stateOf[stringOf(r["record_type"])]; ok {
			present[st] = true
		}
	}
	for _, lc := range lifecycleOrder {
		if present[lc.state] {
			return lc.name
		}
	}
	return ""
}
