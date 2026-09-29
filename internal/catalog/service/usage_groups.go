package service

import (
	"sort"

	"github.com/dcm-project/control-plane/api/catalog/v1alpha1/servicetypes"
	"github.com/dcm-project/control-plane/internal/catalog/store/model"
)

// Usage groups (udlm ADR-082): a class is filed under one or more groups — the
// catalog's shelves. A service type inherits its class's groups; a catalog item
// is on every shelf any of its resources is on. Nothing here is declared per
// item or stored; it is read from the generated table and can only change by
// regenerating from the registry.

// groupsOf returns the usage groups of a service type slug, sorted; nil when the
// slug has no UDLM class or the class is filed nowhere.
func groupsOf(serviceType string) []string {
	groups, ok := servicetypes.LookupUDLMGroups(serviceType)
	if !ok || len(groups) == 0 {
		return nil
	}
	out := append([]string(nil), groups...)
	sort.Strings(out)
	return out
}

// catalogItemGroups is the union of the groups of every resource's service type.
func catalogItemGroups(spec model.CatalogItemSpec) []string {
	seen := map[string]bool{}
	for _, r := range spec.Resources {
		for _, g := range groupsOf(r.ServiceType) {
			seen[g] = true
		}
	}
	if len(seen) == 0 {
		return nil
	}
	out := make([]string, 0, len(seen))
	for g := range seen {
		out = append(out, g)
	}
	sort.Strings(out)
	return out
}

// filedUnder reports whether group is one of groups.
func filedUnder(groups []string, group string) bool {
	for _, g := range groups {
		if g == group {
			return true
		}
	}
	return false
}
