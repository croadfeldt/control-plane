package records

import (
	"reflect"
	"sort"
)

// Field-level provenance (state-record.schema.json $defs.provenance): keyed by
// dot-path into the record, an ordered list of who set the value and when. The
// registry's worked example attributes the intent's fields to the actor, the
// requested record's changed fields to the policy that changed them, and the
// realized record's outputs to the provider (`outputs.<name>`). This file
// produces exactly those three kinds of entry.

// leafPaths flattens nested objects to dot-paths. Arrays and scalars are leaves,
// as in the registry's example (`outputs.ip_addresses`, `cpu.count`).
func leafPaths(m map[string]any, prefix string) map[string]any {
	out := map[string]any{}
	for k, v := range m {
		path := k
		if prefix != "" {
			path = prefix + "." + k
		}
		if sub, ok := v.(map[string]any); ok && len(sub) > 0 {
			for p, lv := range leafPaths(sub, path) {
				out[p] = lv
			}
			continue
		}
		out[path] = v
	}
	return out
}

// sortedKeys returns the map's keys in order, for deterministic records.
func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// entry builds one provenance entry.
func entry(kind, id, op string, sequence int, timestamp string, previous any, hasPrevious bool) map[string]any {
	e := map[string]any{
		"source":         map[string]any{"kind": kind, "id": id},
		"operation_type": op,
		"sequence":       sequence,
		"timestamp":      timestamp,
	}
	if hasPrevious {
		e["previous_value"] = previous
	}
	return e
}

// setAll attributes every leaf of fields to one source, sequence 1.
func setAll(fields map[string]any, kind, id, timestamp string) map[string]any {
	prov := map[string]any{}
	for _, path := range sortedKeys(leafPaths(fields, "")) {
		prov[path] = []any{entry(kind, id, "set", 1, timestamp, nil, false)}
	}
	return prov
}

// diffProvenance attributes to one source every leaf of after that differs from
// before: added or changed paths get a `set` (with the previous value when
// there was one), paths that disappeared get a `remove`. It returns the
// provenance and the changed paths, sorted.
func diffProvenance(before, after map[string]any, kind, id, timestamp string) (map[string]any, []string) {
	prov := map[string]any{}
	b, a := leafPaths(before, ""), leafPaths(after, "")
	var changed []string
	for _, path := range sortedKeys(a) {
		prev, had := b[path]
		if had && reflect.DeepEqual(prev, a[path]) {
			continue
		}
		prov[path] = []any{entry(kind, id, "set", 1, timestamp, prev, had)}
		changed = append(changed, path)
	}
	for _, path := range sortedKeys(b) {
		if _, still := a[path]; !still {
			prov[path] = []any{entry(kind, id, "remove", 1, timestamp, b[path], true)}
			changed = append(changed, path)
		}
	}
	sort.Strings(changed)
	return prov, changed
}

// outputsProvenance attributes `outputs.<path>` to the provider. With a previous
// realized record, an unchanged output keeps its earlier entries (its origin run
// and time survive), a changed one appends an entry carrying the previous
// value, and one that disappeared appends a `remove`.
func outputsProvenance(previous, current map[string]any, previousProv map[string]any, providerID, timestamp string) map[string]any {
	prov := map[string]any{}
	p, c := leafPaths(previous, "outputs"), leafPaths(current, "outputs")
	for _, path := range sortedKeys(c) {
		earlier, _ := previousProv[path].([]any)
		prev, had := p[path]
		if had && reflect.DeepEqual(prev, c[path]) && len(earlier) > 0 {
			prov[path] = earlier
			continue
		}
		prov[path] = append(append([]any{}, earlier...), entry("provider", providerID, "set", len(earlier)+1, timestamp, prev, had))
	}
	for _, path := range sortedKeys(p) {
		if _, still := c[path]; !still {
			earlier, _ := previousProv[path].([]any)
			prov[path] = append(append([]any{}, earlier...), entry("provider", providerID, "remove", len(earlier)+1, timestamp, p[path], true))
		}
	}
	return prov
}
