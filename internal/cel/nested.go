package cel

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// GetNestedValue reads a value from m using a dotted path.
//
// Path syntax:
//   - "connection_string"           → m["connection_string"]
//   - "config.host"                 → m["config"]["host"]
//   - "disks[0].name"               → m["disks"][0]["name"]
//
// A leading "spec." prefix is stripped (catalog paths often include it).
func GetNestedValue(m map[string]any, path string) (any, error) {
	path = strings.TrimPrefix(path, "spec.")
	if path == "" {
		return nil, fmt.Errorf("path cannot be empty")
	}

	parts := strings.Split(path, ".")
	current := m

	for i, part := range parts {
		seg, err := parsePathSegment(part)
		if err != nil {
			return nil, err
		}

		pathSoFar := strings.Join(parts[:i+1], ".")
		isLast := i == len(parts)-1

		value, err := readField(current, seg, path, pathSoFar, isLast)
		if err != nil {
			return nil, err
		}
		if isLast {
			return value, nil
		}

		// Intermediate segments must resolve to another map we can keep walking.
		next, ok := value.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("path segment %q is not a map", pathSoFar)
		}
		current = next
	}

	return nil, fmt.Errorf("path cannot be empty")
}

// pathSegment is one dot-separated piece of a path, e.g. "disks" or "disks[0]".
type pathSegment struct {
	name     string
	index    int
	hasIndex bool
}

// parsePathSegment splits "name" or "name[index]" into its parts.
func parsePathSegment(part string) (pathSegment, error) {
	if part == "" {
		return pathSegment{}, fmt.Errorf("path segment cannot be empty")
	}
	// Fast path: plain key with no array index.
	if !strings.ContainsAny(part, "[]") {
		return pathSegment{name: part}, nil
	}

	// Expect exactly one [index] suffix, e.g. "disks[0]".
	open := strings.IndexByte(part, '[')
	end := strings.IndexByte(part, ']')
	if open <= 0 || end != len(part)-1 || end < open+2 {
		return pathSegment{}, fmt.Errorf("invalid path segment %q: expected name[index]", part)
	}
	if strings.ContainsAny(part[:open], "[]") || strings.ContainsAny(part[open+1:end], "[]") {
		return pathSegment{}, fmt.Errorf("invalid path segment %q: expected name[index]", part)
	}

	idx, err := strconv.Atoi(part[open+1 : end])
	if err != nil || idx < 0 {
		return pathSegment{}, fmt.Errorf("invalid path segment %q: index must be a non-negative integer", part)
	}
	return pathSegment{name: part[:open], index: idx, hasIndex: true}, nil
}

// readField looks up seg.name in current and optionally indexes into an array.
func readField(current map[string]any, seg pathSegment, fullPath, pathSoFar string, isLast bool) (any, error) {
	value, exists := current[seg.name]
	if !exists {
		if isLast {
			return nil, fmt.Errorf("path %q not found", fullPath)
		}
		return nil, fmt.Errorf("path segment %q not found", pathSoFar)
	}
	if !seg.hasIndex {
		return value, nil
	}
	return indexField(value, seg.index, fullPath, pathSoFar, isLast)
}

// indexField selects one element from a []any slice.
func indexField(value any, index int, fullPath, pathSoFar string, isLast bool) (any, error) {
	slice, ok := value.([]any)
	if !ok {
		if isLast {
			return nil, fmt.Errorf("path %q is not an array", fullPath)
		}
		return nil, fmt.Errorf("path segment %q is not an array", pathSoFar)
	}
	if index >= len(slice) {
		if isLast {
			return nil, fmt.Errorf("path %q: index %d out of range", fullPath, index)
		}
		return nil, fmt.Errorf("path segment %q: index %d out of range", pathSoFar, index)
	}
	return slice[index], nil
}

func deepCopyMap(src map[string]any) (map[string]any, error) {
	data, err := json.Marshal(src)
	if err != nil {
		return nil, err
	}
	var dst map[string]any
	if err := json.Unmarshal(data, &dst); err != nil {
		return nil, err
	}
	return dst, nil
}
