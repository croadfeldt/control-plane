package records

import (
	"fmt"
	"sort"
	"strings"
)

// The control plane's service types are upstream's (dcm-project); their field
// names are not always the UDLM class's. A record carries the class's names, so
// the writer maps a service-type spec onto the class before the record is built,
// and maps a provider's outputs the same way. Each entry below is a field that
// has a true counterpart in the class; nothing is renamed on a guess.
//
// A field with no counterpart is not dropped and not renamed: it is listed in
// noClassHome, and a spec that carries it is refused with the reason. Dropping it
// would make the dispatch receipt (DSP-004) claim less than the provider was
// sent; filing it under a same-named element whose meaning differs would make the
// record claim something false (Machine.VM `storage` is a requirements
// descriptor; upstream's `storage.disks[]` is per-disk shape, which is the
// machine's `disks` element, mapped below).

// fieldRenames maps a service-type field (dot path) to the class element path.
var fieldRenames = map[string]map[string]string{
	"vm":        {"vcpu": "cpu"},
	"cluster":   {"version": "release"},
	"container": {"process.command": "command", "process.args": "args"},
}

// outputRenames maps a provider output name to the class's declared output.
var outputRenames = map[string]map[string]string{
	"cluster": {"api_endpoint": "api_url"},
	"storage": {"volume_name": "volume_handle"},
}

// noClassHome names service-type fields the UDLM class has no element for yet,
// each with where it belongs, so the refusal says what would make it legal.
var noClassHome = map[string]map[string]string{
	"vm": {
		"access": "an SSH key has no Machine element yet (pending a UDLM ruling)",
	},
}

// mapFields returns the spec's fields under the class's names, or an error naming
// every field that has no home in the class.
func mapFields(serviceType string, fields map[string]any) (map[string]any, error) {
	if missing := homeless(serviceType, fields); len(missing) > 0 {
		return nil, fmt.Errorf("service type %q carries fields with no home in its UDLM class: %s",
			serviceType, strings.Join(missing, "; "))
	}
	out := cloneMap(fields)
	if serviceType == "vm" {
		if err := mapDisks(out); err != nil {
			return nil, err
		}
	}
	for from, to := range fieldRenames[serviceType] {
		v, ok := takePath(out, from)
		if !ok {
			continue
		}
		if _, clash := getPath(out, to); clash {
			return nil, fmt.Errorf("service type %q sets both %q and %q, which map to the same class element", serviceType, from, to)
		}
		setPath(out, to, v)
	}
	return out, nil
}

// mapOutputs renames a provider's outputs to the class's declared names.
func mapOutputs(serviceType string, outputs map[string]any) map[string]any {
	renames := outputRenames[serviceType]
	if len(renames) == 0 {
		return outputs
	}
	out := make(map[string]any, len(outputs))
	for k, v := range outputs {
		if to, ok := renames[k]; ok {
			k = to
		}
		out[k] = v
	}
	return out
}

func homeless(serviceType string, fields map[string]any) []string {
	var missing []string
	for path, why := range noClassHome[serviceType] {
		if v, ok := getPath(fields, path); ok && !isEmpty(v) {
			missing = append(missing, path+" ("+why+")")
		}
	}
	sort.Strings(missing)
	return missing
}

func isEmpty(v any) bool {
	switch x := v.(type) {
	case nil:
		return true
	case map[string]any:
		return len(x) == 0
	case []any:
		return len(x) == 0
	case string:
		return x == ""
	}
	return false
}

func getPath(m map[string]any, path string) (any, bool) {
	parts := strings.Split(path, ".")
	var cur any = m
	for _, p := range parts {
		mm, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		if cur, ok = mm[p]; !ok {
			return nil, false
		}
	}
	return cur, true
}

// takePath removes the value at path and prunes parents left empty.
func takePath(m map[string]any, path string) (any, bool) {
	parts := strings.Split(path, ".")
	parent := m
	for _, p := range parts[:len(parts)-1] {
		next, ok := parent[p].(map[string]any)
		if !ok {
			return nil, false
		}
		parent = next
	}
	last := parts[len(parts)-1]
	v, ok := parent[last]
	if !ok {
		return nil, false
	}
	delete(parent, last)
	if len(parts) > 1 && len(parent) == 0 {
		_, _ = takePath(m, strings.Join(parts[:len(parts)-1], "."))
	}
	return v, true
}

func setPath(m map[string]any, path string, v any) {
	parts := strings.Split(path, ".")
	cur := m
	for _, p := range parts[:len(parts)-1] {
		next, ok := cur[p].(map[string]any)
		if !ok {
			next = map[string]any{}
			cur[p] = next
		}
		cur = next
	}
	cur[parts[len(parts)-1]] = v
}

func cloneMap(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		if mm, ok := v.(map[string]any); ok {
			out[k] = cloneMap(mm)
			continue
		}
		out[k] = v
	}
	return out
}

// Upstream's VM carries its disks as `storage.disks[]` ({name, capacity});
// Machine carries them as `disks[]` ({name, size, role}), since UDLM row 112.
// Upstream's rule is that the root volume is the disk named "boot", which is
// the `boot` role. A disk field with no home on the entry is refused, not
// dropped (DSP-004).
const (
	disksPath = "storage.disks"
	bootDisk  = "boot"
)

var diskKeys = map[string]string{"name": "name", "capacity": "size"}

func mapDisks(out map[string]any) error {
	raw, ok := getPath(out, disksPath)
	if !ok || isEmpty(raw) {
		return nil
	}
	if _, set := out["disks"]; set {
		return fmt.Errorf("the spec sets both %s and disks; a VM's disks come from one of them", disksPath)
	}
	list, ok := raw.([]any)
	if !ok {
		return fmt.Errorf("%s is not a list", disksPath)
	}
	disks := make([]any, 0, len(list))
	for i, d := range list {
		disk, ok := d.(map[string]any)
		if !ok {
			return fmt.Errorf("%s[%d]: not an object", disksPath, i)
		}
		entry := map[string]any{}
		var homeless []string
		for k, v := range disk {
			to, ok := diskKeys[k]
			if !ok {
				homeless = append(homeless, k)
				continue
			}
			entry[to] = v
		}
		if len(homeless) > 0 {
			sort.Strings(homeless)
			return fmt.Errorf("%s[%d]: fields with no home on a Machine disk: %s", disksPath, i, strings.Join(homeless, ", "))
		}
		if entry["name"] == bootDisk {
			entry["role"] = "boot"
		}
		disks = append(disks, entry)
	}
	takePath(out, disksPath)
	out["disks"] = disks
	return nil
}
