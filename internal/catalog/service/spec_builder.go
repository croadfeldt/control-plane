package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/dcm-project/control-plane/api/catalog/v1alpha1"
	"github.com/dcm-project/control-plane/internal/catalog/store"
	"github.com/dcm-project/control-plane/internal/catalog/store/model"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

// ServiceTypeKey is the key for the service_type field in the spec map
const ServiceTypeKey = "service_type"

// ResolvedResource is a catalog resource after resolution.
type ResolvedResource struct {
	Name              string
	ServiceType       string
	RequiresResources []string
	Spec              map[string]any
}

// specBuilder resolves the reference chain and constructs the final resource spec
type specBuilder struct {
	store store.Store
}

// newSpecBuilder creates a new specBuilder
func newSpecBuilder(store store.Store) *specBuilder {
	return &specBuilder{store: store}
}

// BuildResourceGraph resolves a catalog item to an effective resource graph.
// Each node includes merged specs and requires_resources edges for placement.
func (b *specBuilder) BuildResourceGraph(ctx context.Context, catalogItemId string, userValues []v1alpha1.UserValue) ([]ResolvedResource, error) {
	// 1. Look up CatalogItem
	catalogItem, err := b.store.CatalogItem().Get(ctx, catalogItemId)
	if err != nil {
		if errors.Is(err, store.ErrCatalogItemNotFound) {
			return nil, ErrCatalogItemNotFoundForInstance
		}
		return nil, err
	}

	// 2. Validate user_values against catalog item resources (paths, resources, CEL rules)
	if err := validateUserValuesForCatalogItem(catalogItem.Spec, userValues); err != nil {
		return nil, err
	}

	// 3. Resolve each catalog resource into an effective spec node
	out := make([]ResolvedResource, 0, len(catalogItem.Spec.Resources))
	resourcesByName := catalogResourcesByName(catalogItem.Spec.Resources)
	for _, resource := range catalogItem.Spec.Resources {
		resourceUserValues := userValuesForResource(userValues, resource.Name)
		specMap, err := b.buildResourceSpecFromFields(ctx, resourcesByName, resource, resourceUserValues)
		if err != nil {
			return nil, fmt.Errorf("resource %s: %w", resource.Name, err)
		}
		out = append(out, ResolvedResource{
			Name:              resource.Name,
			ServiceType:       resource.ServiceType,
			RequiresResources: append([]string(nil), resource.RequiresResources...),
			Spec:              specMap,
		})
	}
	return out, nil
}

// buildResourceSpecFromFields merges a catalog resource's field configuration and
// instance user values onto the service type base spec, producing the effective
// spec for one node in the resource graph.
//
// Merge order: service type spec → catalog field defaults → user values.
// CEL references (${resource.output}) in defaults and editable user_values are
// validated at merge time when the full resource graph is known.
func (b *specBuilder) buildResourceSpecFromFields(
	ctx context.Context,
	resourcesByName map[string]model.CatalogResource,
	resource model.CatalogResource,
	userValues []v1alpha1.UserValue,
) (map[string]any, error) {
	serviceTypeName := resource.ServiceType
	fields := resource.Fields

	// 1. Look up ServiceType by resource's service_type
	serviceType, err := b.store.ServiceType().GetByServiceType(ctx, serviceTypeName)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve service type %q: %w", serviceTypeName, err)
	}

	// 2. Deep-copy ServiceType spec as base template
	specMap, err := deepCopyMap(serviceType.Spec)
	if err != nil {
		return nil, fmt.Errorf("failed to copy service type spec: %w", err)
	}

	// 2.1. Set service_type from the ServiceType instance
	specMap[ServiceTypeKey] = serviceType.ServiceType

	// 3. Build a lookup map of catalog resource fields by path
	fieldsByPath := make(map[string]model.FieldConfiguration)
	for _, field := range fields {
		fieldsByPath[field.Path] = field
	}

	// 4. Apply catalog field defaults (CEL, schema validation, then overlay)
	for _, field := range fields {
		if field.Default == nil {
			continue
		}
		// Validate CEL wiring in catalog defaults
		if err := validateCELReferenceValue(ctx, b.store, resourcesByName, resource.Name, field.Path, field.Default); err != nil {
			return nil, err
		}
		if field.ValidationSchema != nil {
			if err := validateAgainstSchema(field.ValidationSchema, field.Default); err != nil {
				return nil, fmt.Errorf("%w: %s: %s", ErrFieldDefaultValidationFailed, field.Path, err.Error())
			}
		}
		if err := setNestedValue(specMap, field.Path, field.Default); err != nil {
			return nil, fmt.Errorf("failed to set default for field %q: %w", field.Path, err)
		}
	}

	// 5. Apply user_values on top (with path, editable, CEL and schema validation)
	for _, uv := range userValues {
		// Validate: user_value path must match a catalog resource field
		field, ok := fieldsByPath[uv.Path]
		if !ok {
			return nil, fmt.Errorf("%w: %s", ErrUserValuePathNotFound, uv.Path)
		}

		// Validate: field must be editable
		if !field.Editable {
			return nil, fmt.Errorf("%w: %s", ErrUserValueNotEditable, uv.Path)
		}

		// Validate CEL wiring overrides in user values
		if err := validateCELReferenceValue(ctx, b.store, resourcesByName, resource.Name, uv.Path, uv.Value); err != nil {
			return nil, err
		}

		// Validate: if field has a validation_schema, validate the value against it
		if field.ValidationSchema != nil {
			if err := validateAgainstSchema(field.ValidationSchema, uv.Value); err != nil {
				return nil, fmt.Errorf("%w: %s: %s", ErrUserValueValidationFailed, uv.Path, err.Error())
			}
		}

		// Apply the user value
		if err := setNestedValue(specMap, uv.Path, uv.Value); err != nil {
			return nil, fmt.Errorf("failed to set user value for field %q: %w", uv.Path, err)
		}
	}

	// 6. Validate depends_on constraints against final spec (all user values applied)
	for _, uv := range userValues {
		field := fieldsByPath[uv.Path]
		if field.DependsOn != nil {
			if err := validateDependsOn(specMap, field.DependsOn, uv.Path, uv.Value); err != nil {
				return nil, err
			}
		}
	}

	return specMap, nil
}

// deepCopyMap creates a deep copy of a map[string]any by marshaling/unmarshaling JSON
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

// validateAgainstSchema validates a value against a JSON Schema
func validateAgainstSchema(schema map[string]any, value any) error {
	c := jsonschema.NewCompiler()
	if err := c.AddResource("schema.json", schema); err != nil {
		return fmt.Errorf("failed to add schema resource: %w", err)
	}
	sch, err := c.Compile("schema.json")
	if err != nil {
		return fmt.Errorf("failed to compile schema: %w", err)
	}

	// JSON Schema validation requires the value to go through JSON round-trip
	// to ensure types match (e.g., int vs float64)
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("failed to marshal value: %w", err)
	}
	var jsonValue any
	if err := json.Unmarshal(data, &jsonValue); err != nil {
		return fmt.Errorf("failed to unmarshal value: %w", err)
	}

	return sch.Validate(jsonValue)
}

// validateDependsOn validates a user value against a field's depends_on constraint.
// It looks up the source field's current value in the spec, then checks that the
// user's value is among the allowed values for that source value.
func validateDependsOn(specMap map[string]any, dep *model.DependsOn, fieldPath string, userValue any) error {
	sourceValue, err := getNestedValue(specMap, dep.Path)
	if err != nil {
		return fmt.Errorf("%w: %s: source field %s not resolved", ErrUserValueDependsOnViolation, fieldPath, dep.Path)
	}

	sourceKey := fmt.Sprintf("%v", sourceValue)
	allowed, ok := dep.AllowedValues[sourceKey]
	if !ok {
		return fmt.Errorf("%w: %s: no allowed values defined for %s=%s", ErrUserValueDependsOnViolation, fieldPath, dep.Path, sourceKey)
	}

	if !containsValue(allowed, userValue) {
		return fmt.Errorf("%w: %s: value not in allowed options for %s=%s", ErrUserValueDependsOnViolation, fieldPath, dep.Path, sourceKey)
	}

	return nil
}

// containsValue checks if target is present in arr using JSON comparison
// to handle type differences (e.g., float64 vs int).
func containsValue(arr []any, target any) bool {
	targetJSON, err := json.Marshal(target)
	if err != nil {
		return false
	}
	for _, v := range arr {
		vJSON, err := json.Marshal(v)
		if err != nil {
			continue
		}
		if string(targetJSON) == string(vJSON) {
			return true
		}
	}
	return false
}
