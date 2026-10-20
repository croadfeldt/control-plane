package records

import (
	"fmt"
	"sort"
	"strings"
)

// A VM's disks are not a Machine.VM element. In UDLM they are a StorageLayout:
// its own entity, with its own records, that the VM points at through
// `layout_ref` and a `storage_layout` edge. The writer splits upstream's
// `storage.disks[]` out of a VM spec into that layout, so the VM's records carry
// the reference and the layout's records carry the disks.

const (
	layoutClass      = "StorageLayout"
	layoutRefElement = "layout_ref"
	disksPath        = "storage.disks"
	// bootDisk is upstream's rule: the root volume is the disk named "boot".
	bootDisk = "boot"
)

// diskKeys are the upstream Disk fields that have a home on a layout entry.
var diskKeys = map[string]string{"name": "name", "capacity": "size"}

// companion is a class record the writer writes beside the one the spec names.
type companion struct {
	t      Type
	fields map[string]any
}

// splitLayout removes a VM's disks from fields and returns them as StorageLayout
// fields. It returns nil when the spec has no disks or the service type has none.
func (w *Writer) splitLayout(serviceType string, fields map[string]any) (*companion, error) {
	if serviceType != "vm" {
		return nil, nil
	}
	raw, ok := getPath(fields, disksPath)
	if !ok || isEmpty(raw) {
		return nil, nil
	}
	if _, set := fields[layoutRefElement]; set {
		return nil, fmt.Errorf("the spec sets both %s and %s; a VM's disks come from one of them", disksPath, layoutRefElement)
	}
	disks, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("%s is not a list", disksPath)
	}
	if w.classes == nil {
		return nil, fmt.Errorf("%s needs the %s class, which the writer was not given", disksPath, layoutClass)
	}
	t, ok := w.classes(layoutClass)
	if !ok {
		return nil, fmt.Errorf("%s needs the %s class, which the writer does not know", disksPath, layoutClass)
	}
	entries := make([]any, 0, len(disks))
	for i, d := range disks {
		entry, err := layoutEntry(d)
		if err != nil {
			return nil, fmt.Errorf("%s[%d]: %w", disksPath, i, err)
		}
		entries = append(entries, entry)
	}
	takePath(fields, disksPath)
	return &companion{t: t, fields: map[string]any{"entries": entries}}, nil
}

// layoutEntry maps one upstream disk to a layout entry. A disk field with no
// home on the entry is refused, not dropped: the layout's dispatch receipt must
// not claim less than the provider was sent (DSP-004).
func layoutEntry(d any) (map[string]any, error) {
	disk, ok := d.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("not an object")
	}
	var homeless []string
	entry := map[string]any{}
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
		return nil, fmt.Errorf("fields with no home on a %s entry: %s", layoutClass, strings.Join(homeless, ", "))
	}
	if entry["name"] == bootDisk {
		entry["role"] = "boot"
	}
	return entry, nil
}

// layoutRef is the URF reference to a layout entity (identifier-scheme §9).
func layoutRef(entity string) string { return "uuid/" + entity }

// layoutEntityOf returns the layout entity a VM's fields reference, when the
// reference is one the writer made (uuid form). A handle the consumer wrote is
// not a layout the writer expanded, so it returns empty.
func layoutEntityOf(fields map[string]any) string {
	ref, ok := strings.CutPrefix(stringOf(fields[layoutRefElement]), "uuid/")
	if !ok {
		return ""
	}
	if i := strings.IndexByte(ref, '@'); i >= 0 {
		ref = ref[:i]
	}
	return ref
}
