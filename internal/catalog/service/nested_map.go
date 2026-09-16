package service

import (
	"fmt"
	"strconv"
	"strings"
)

// stripSpecPrefix removes the "spec." prefix from a path if present
func stripSpecPrefix(path string) string {
	return strings.TrimPrefix(path, "spec.")
}

// pathSegment is one dot-separated path piece; hasIndex means name[index] into a slice.
type pathSegment struct {
	name     string
	index    int
	hasIndex bool
}

// maxPathArrayIndex caps slice growth from path indexes (avoids OOM / MaxInt+1 panic).
const maxPathArrayIndex = 1024

// parsePathSegment parses "key" or "key[n]"; malformed brackets return an error.
func parsePathSegment(seg string) (pathSegment, error) {
	if seg == "" {
		return pathSegment{}, fmt.Errorf("path segment cannot be empty")
	}
	if !strings.ContainsAny(seg, "[]") {
		return pathSegment{name: seg}, nil
	}

	open := strings.IndexByte(seg, '[')
	close := strings.IndexByte(seg, ']')
	if open <= 0 || close != len(seg)-1 || close < open+2 {
		return pathSegment{}, fmt.Errorf("invalid path segment %q: expected name[index]", seg)
	}
	if strings.ContainsAny(seg[:open], "[]") || strings.ContainsAny(seg[open+1:close], "[]") {
		return pathSegment{}, fmt.Errorf("invalid path segment %q: expected name[index]", seg)
	}

	idx, err := strconv.Atoi(seg[open+1 : close])
	if err != nil || idx < 0 {
		return pathSegment{}, fmt.Errorf("invalid path segment %q: index must be a non-negative integer", seg)
	}
	if idx > maxPathArrayIndex {
		return pathSegment{}, fmt.Errorf("invalid path segment %q: index must be <= %d", seg, maxPathArrayIndex)
	}
	return pathSegment{name: seg[:open], index: idx, hasIndex: true}, nil
}

// setNestedValue writes value at a dotted path, creating maps/slices as needed.
func setNestedValue(m map[string]any, path string, value any) error {
	path = stripSpecPrefix(path)
	if path == "" {
		return fmt.Errorf("path cannot be empty")
	}
	parts := strings.Split(path, ".")
	current := m

	for i := 0; i < len(parts)-1; i++ {
		seg, err := parsePathSegment(parts[i])
		if err != nil {
			return err
		}
		pathSoFar := strings.Join(parts[:i+1], ".")

		if !seg.hasIndex {
			next, exists := current[seg.name]
			if !exists {
				newMap := make(map[string]any)
				current[seg.name] = newMap
				current = newMap
				continue
			}
			nextMap, ok := next.(map[string]any)
			if !ok {
				return fmt.Errorf("path segment %q is not a map", pathSoFar)
			}
			current = nextMap
			continue
		}

		current, err = ensureSliceElementMap(current, seg.name, seg.index, pathSoFar)
		if err != nil {
			return err
		}
	}

	last, err := parsePathSegment(parts[len(parts)-1])
	if err != nil {
		return err
	}
	if !last.hasIndex {
		current[last.name] = value
		return nil
	}

	raw, exists := current[last.name]
	var slice []any
	if !exists {
		slice = make([]any, last.index+1)
	} else {
		var ok bool
		slice, ok = raw.([]any)
		if !ok {
			return fmt.Errorf("path segment %q is not an array", path)
		}
		if last.index >= len(slice) {
			grown := make([]any, last.index+1)
			copy(grown, slice)
			slice = grown
		}
	}
	slice[last.index] = value
	current[last.name] = slice
	return nil
}

// getNestedValue reads the value at a dotted path (supports name[index] segments).
func getNestedValue(m map[string]any, path string) (any, error) {
	path = stripSpecPrefix(path)
	if path == "" {
		return nil, fmt.Errorf("path cannot be empty")
	}
	parts := strings.Split(path, ".")
	current := m

	for i := 0; i < len(parts)-1; i++ {
		seg, err := parsePathSegment(parts[i])
		if err != nil {
			return nil, err
		}
		pathSoFar := strings.Join(parts[:i+1], ".")

		next, exists := current[seg.name]
		if !exists {
			return nil, fmt.Errorf("path segment %q not found", pathSoFar)
		}

		if !seg.hasIndex {
			nextMap, ok := next.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("path segment %q is not a map", pathSoFar)
			}
			current = nextMap
			continue
		}

		slice, ok := next.([]any)
		if !ok {
			return nil, fmt.Errorf("path segment %q is not an array", pathSoFar)
		}
		if seg.index >= len(slice) {
			return nil, fmt.Errorf("path segment %q: index %d out of range", pathSoFar, seg.index)
		}
		elem, ok := slice[seg.index].(map[string]any)
		if !ok {
			return nil, fmt.Errorf("path segment %q is not a map", pathSoFar)
		}
		current = elem
	}

	last, err := parsePathSegment(parts[len(parts)-1])
	if err != nil {
		return nil, err
	}

	next, exists := current[last.name]
	if !exists {
		return nil, fmt.Errorf("path %q not found", path)
	}
	if !last.hasIndex {
		return next, nil
	}

	slice, ok := next.([]any)
	if !ok {
		return nil, fmt.Errorf("path %q is not an array", path)
	}
	if last.index >= len(slice) {
		return nil, fmt.Errorf("path %q: index %d out of range", path, last.index)
	}
	return slice[last.index], nil
}

// ensureSliceElementMap returns the map at parent[name][index], creating/growing the slice as needed.
func ensureSliceElementMap(parent map[string]any, name string, index int, pathSoFar string) (map[string]any, error) {
	raw, exists := parent[name]
	var slice []any
	if !exists {
		slice = make([]any, index+1)
	} else {
		var ok bool
		slice, ok = raw.([]any)
		if !ok {
			return nil, fmt.Errorf("path segment %q is not an array", pathSoFar)
		}
		if index >= len(slice) {
			grown := make([]any, index+1)
			copy(grown, slice)
			slice = grown
		}
	}

	if slice[index] == nil {
		elem := make(map[string]any)
		slice[index] = elem
		parent[name] = slice
		return elem, nil
	}
	elem, ok := slice[index].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("path segment %q is not a map", pathSoFar)
	}
	parent[name] = slice
	return elem, nil
}
