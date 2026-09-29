package records

import (
	"fmt"
	"strings"
)

// dispatchReceipt builds the requested record's `dispatch` block: the receipt
// of exactly what crossed to the provider (state-record.schema.json $defs.dispatch,
// DSP-004). Dispatch is zero trust — a provider receives nothing by default
// (DSP-001); the class it binds defines what is required (DSP-002); anything
// beyond that crosses only by a policy grant (DSP-003).
//
// The control plane sends the evaluated fields whole, so `admitted` is every
// leaf path of fields, in the same dot-path grammar the provenance uses. No
// policy engine grants or strips paths here yet, so `granted` and `stripped`
// are absent (the schema requires only provider and admitted). A path whose
// head is not an element of the bound class is refused — the record is not
// written — rather than admitted on the assumption that the service type was
// generated from the class correctly; that is the difference between a receipt
// and a claim.
func dispatchReceipt(provider string, fields map[string]any, t Type) (map[string]any, error) {
	if len(t.Elements) == 0 {
		return nil, fmt.Errorf("class %s declares no elements the writer knows; nothing may be admitted (DSP-001/002)", t.ResourceType)
	}
	elements := make(map[string]bool, len(t.Elements))
	for _, e := range t.Elements {
		elements[e] = true
	}
	paths := sortedKeys(leafPaths(fields, ""))
	admitted := make([]any, 0, len(paths))
	for _, p := range paths {
		if !elements[pathHead(p)] {
			return nil, fmt.Errorf("dispatched path %q is not an element of %s and no policy granted it (DSP-002)", p, t.ResourceType)
		}
		admitted = append(admitted, p)
	}
	return map[string]any{"provider": provider, "admitted": admitted}, nil
}

// pathHead is the first segment of a dot-path — the class element it lives
// under (`networks[0].vlan` → `networks`, `cpu.count` → `cpu`).
func pathHead(p string) string {
	if i := strings.IndexAny(p, ".["); i >= 0 {
		return p[:i]
	}
	return p
}
