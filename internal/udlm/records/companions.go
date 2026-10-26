package records

import (
	"fmt"
	"strings"
)

// A companion is a class record the writer writes beside the one the spec
// names, because the spec expands into it: a cluster's node groups are
// KubernetesNodePool records contained by the cluster. (A VM's disks were once
// a StorageLayout companion; since UDLM row 112 they are the machine's own
// `disks` element, mapped in fieldmap.go.)

// companion is a class record the writer writes beside the one the spec names.
type companion struct {
	t      Type
	name   string // the pool name
	fields map[string]any
}

// expansion is what a spec splits into beside its own fields.
type expansion struct {
	pools []companion
}

// class looks up a companion class, naming the spec path that needs it.
func (w *Writer) class(resourceType, path string) (Type, error) {
	if w.classes == nil {
		return Type{}, fmt.Errorf("%s needs the %s class, which the writer was not given", path, resourceType)
	}
	t, ok := w.classes(resourceType)
	if !ok {
		return Type{}, fmt.Errorf("%s needs the %s class, which the writer does not know", path, resourceType)
	}
	return t, nil
}

// entityRef is the URF reference to an entity by uuid (identifier-scheme §9).
func entityRef(entity string) string { return "uuid/" + entity }

// refEntity returns the entity a reference names, when it is in the uuid form
// the writer makes. A handle the consumer wrote is not one the writer made, so
// it returns empty.
func refEntity(v any) string {
	ref, ok := strings.CutPrefix(stringOf(v), "uuid/")
	if !ok {
		return ""
	}
	if i := strings.IndexByte(ref, '@'); i >= 0 {
		ref = ref[:i]
	}
	return ref
}

// refEdges are the reference elements the writer fills, with the edge each
// stands for on the record (the class's declared relationship). The reference
// never crosses to the provider, so it is kept off the dispatch receipt.
var refEdges = map[string]struct{ edgeType, relation string }{
	clusterRefElement: {"contained_by", "cluster"},
}
