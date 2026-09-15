// Package cel provides restricted ${resource.output} reference parsing and apply-time binding.
package cel

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// ErrInvalidReference indicates a string is not a valid restricted CEL reference.
var ErrInvalidReference = errors.New("invalid CEL expression: must match ${resourceName.outputField}")

var referencePattern = regexp.MustCompile(`^\$\{([a-zA-Z_][a-zA-Z0-9_]*)\.([a-zA-Z_][a-zA-Z0-9_]*(?:\[\d+\])?(?:\.[a-zA-Z_][a-zA-Z0-9_]*(?:\[\d+\])?)*)\}$`)

// Reference is a parsed ${resourceName.outputField} expression.
type Reference struct {
	ResourceName string
	OutputField  string
}

// ParseReference parses a restricted CEL reference string.
// Returns isReference=false when value is a plain string with no CEL syntax.
func ParseReference(value string) (Reference, bool, error) {
	if !strings.Contains(value, "${") {
		return Reference{}, false, nil
	}
	matches := referencePattern.FindStringSubmatch(value)
	if matches == nil {
		return Reference{}, true, fmt.Errorf("%w: %q", ErrInvalidReference, value)
	}
	return Reference{
		ResourceName: matches[1],
		OutputField:  matches[2],
	}, true, nil
}

// CollectReferences returns unique CEL references found in a nested spec.
func CollectReferences(spec map[string]any) ([]Reference, error) {
	if spec == nil {
		return nil, nil
	}

	seen := make(map[string]Reference)
	if err := collectReferencesValue(spec, seen); err != nil {
		return nil, err
	}

	refs := make([]Reference, 0, len(seen))
	for _, ref := range seen {
		refs = append(refs, ref)
	}
	sort.Slice(refs, func(i, j int) bool {
		if refs[i].ResourceName == refs[j].ResourceName {
			return refs[i].OutputField < refs[j].OutputField
		}
		return refs[i].ResourceName < refs[j].ResourceName
	})
	return refs, nil
}

func collectReferencesValue(value any, seen map[string]Reference) error {
	switch v := value.(type) {
	case map[string]any:
		for _, child := range v {
			if err := collectReferencesValue(child, seen); err != nil {
				return err
			}
		}
	case []any:
		for _, child := range v {
			if err := collectReferencesValue(child, seen); err != nil {
				return err
			}
		}
	case string:
		ref, isReference, err := ParseReference(v)
		if err != nil {
			return err
		}
		if isReference {
			seen[ref.ResourceName+"."+ref.OutputField] = ref
		}
	}
	return nil
}
