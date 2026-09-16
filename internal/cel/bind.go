package cel

import "fmt"

// BindReferences returns a deep copy of spec with ${resource.output} strings replaced
// by values from outputsByResource (resource name -> producer output map).
func BindReferences(spec map[string]any, outputsByResource map[string]map[string]any) (map[string]any, error) {
	if spec == nil {
		return nil, nil
	}
	bound, err := deepCopyMap(spec)
	if err != nil {
		return nil, fmt.Errorf("failed to copy spec for CEL binding: %w", err)
	}
	if err := bindValue(bound, outputsByResource); err != nil {
		return nil, err
	}
	return bound, nil
}

func bindValue(value any, outputsByResource map[string]map[string]any) error {
	switch v := value.(type) {
	case map[string]any:
		for key, child := range v {
			if str, ok := child.(string); ok {
				resolved, err := bindString(str, outputsByResource)
				if err != nil {
					return err
				}
				v[key] = resolved
				continue
			}
			if err := bindValue(child, outputsByResource); err != nil {
				return err
			}
		}
	case []any:
		for i, child := range v {
			if str, ok := child.(string); ok {
				resolved, err := bindString(str, outputsByResource)
				if err != nil {
					return err
				}
				v[i] = resolved
				continue
			}
			if err := bindValue(child, outputsByResource); err != nil {
				return err
			}
		}
	}
	return nil
}

func bindString(value string, outputsByResource map[string]map[string]any) (any, error) {
	ref, isReference, err := ParseReference(value)
	if err != nil {
		return nil, err
	}
	if !isReference {
		return value, nil
	}

	producer, ok := outputsByResource[ref.ResourceName]
	if !ok {
		return nil, fmt.Errorf("CEL dependency %q is not ready", ref.ResourceName)
	}
	output, err := GetNestedValue(producer, ref.OutputField)
	if err != nil {
		return nil, fmt.Errorf("CEL output %s.%s: %w", ref.ResourceName, ref.OutputField, err)
	}
	if output == nil {
		return nil, fmt.Errorf("CEL output %s.%s is not available", ref.ResourceName, ref.OutputField)
	}
	str, ok := output.(string)
	if !ok {
		return nil, fmt.Errorf("CEL output %s.%s must be a string", ref.ResourceName, ref.OutputField)
	}
	if str == "" {
		return nil, fmt.Errorf("CEL output %s.%s is not available", ref.ResourceName, ref.OutputField)
	}
	return str, nil
}
