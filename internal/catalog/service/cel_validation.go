package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"

	"github.com/dcm-project/control-plane/api/catalog/v1alpha1/servicetypes"
	"github.com/dcm-project/control-plane/internal/catalog/store"
	"github.com/dcm-project/control-plane/internal/catalog/store/model"
	"github.com/dcm-project/control-plane/internal/cel"
)

// serviceTypeTemplateHasField reports whether fieldName exists on the service type template spec.
func serviceTypeTemplateHasField(st *model.ServiceType, fieldName string) bool {
	if st == nil || st.Spec == nil {
		return false
	}
	_, err := getNestedValue(st.Spec, fieldName)
	return err == nil
}

func validateCELReferenceValue(
	ctx context.Context,
	store store.Store,
	resourcesByName map[string]model.CatalogResource,
	consumerResourceName string,
	fieldPath string,
	value any,
) error {
	str, ok := value.(string)
	if !ok {
		return nil
	}

	ref, isCEL, err := cel.ParseReference(str)
	if err != nil {
		if errors.Is(err, cel.ErrInvalidReference) {
			return fmt.Errorf("%w: %q", ErrInvalidCELExpression, str)
		}
		return err
	}
	if !isCEL {
		return nil
	}

	if ref.ResourceName == consumerResourceName {
		return fmt.Errorf("%w: field %s", ErrCELSelfReference, fieldPath)
	}

	source, ok := resourcesByName[ref.ResourceName]
	if !ok {
		return fmt.Errorf("%w: %s", ErrCELResourceNotFound, ref.ResourceName)
	}

	consumer := resourcesByName[consumerResourceName]
	if !slices.Contains(consumer.RequiresResources, ref.ResourceName) {
		return fmt.Errorf("%w: field %s references %s", ErrCELRequiresResourceMissing, fieldPath, ref.ResourceName)
	}

	sourceST, err := store.ServiceType().GetByServiceType(ctx, source.ServiceType)
	if err != nil {
		return ErrServiceTypeNotFound
	}

	// Typed binding against the UDLM registry (docs/udlm-native.md, increment 5).
	// When the source service type projects a UDLM class, the registry is the
	// authority: the reference must name one of the outputs that class declares,
	// a path into the output must fit its type, and a plain reference must have a
	// type the consumer field can take.
	if _, hasClass := servicetypes.LookupUDLMType(source.ServiceType); hasClass {
		consumerST, err := store.ServiceType().GetByServiceType(ctx, consumer.ServiceType)
		if err != nil {
			consumerST = nil
		}
		return validateTypedOutputBinding(ctx, source.ServiceType, sourceST, ref, consumerST, fieldPath)
	}

	// Interim check for service types with no UDLM class: field key presence on
	// the service type template only. Enhancement #99 will add separate output
	// definitions on the service type (parallel to input schemas); until then CEL
	// cannot distinguish declared outputs from input fields for these types.
	if !serviceTypeTemplateHasField(sourceST, ref.OutputField) {
		return fmt.Errorf("%w: service type %q has no field %q for %s.%s",
			ErrCELServiceTypeOutputNotFound, source.ServiceType, ref.OutputField, ref.ResourceName, ref.OutputField)
	}
	return nil
}

// validateTypedOutputBinding checks a reference against the outputs the source's
// UDLM class declares (servicetypes.UDLMOutputs, generated from the registry).
func validateTypedOutputBinding(ctx context.Context, sourceServiceType string, sourceST *model.ServiceType, ref cel.Reference, consumerST *model.ServiceType, fieldPath string) error {
	top, rest := splitOutputPath(ref.OutputField)
	out, ok := servicetypes.LookupUDLMOutput(sourceServiceType, top)
	if !ok {
		// An input field of the source is not bindable; an unknown name is unknown.
		kind := ErrCELServiceTypeOutputNotFound
		if serviceTypeTemplateHasField(sourceST, top) {
			kind = ErrCELOutputNotDeclared
		}
		return fmt.Errorf("%w: %s.%s: service type %q (UDLM class) declares outputs %v",
			kind, ref.ResourceName, ref.OutputField, sourceServiceType, declaredOutputNames(sourceServiceType))
	}
	if out.Sensitive {
		slog.InfoContext(ctx, "audit: sensitive output bound",
			"source_resource", ref.ResourceName, "output_field", top, "consumer_field", fieldPath)
	}
	if rest != "" {
		// A path into the output: only an array (index) or object (key) can be walked.
		switch {
		case strings.HasPrefix(rest, "[") && out.Type != "array" && out.Type != "":
			return fmt.Errorf("%w: %s.%s indexes output %q, which is %s, not an array",
				ErrCELOutputTypeMismatch, ref.ResourceName, ref.OutputField, top, out.Type)
		case strings.HasPrefix(rest, ".") && out.Type != "object" && out.Type != "":
			return fmt.Errorf("%w: %s.%s walks into output %q, which is %s, not an object",
				ErrCELOutputTypeMismatch, ref.ResourceName, ref.OutputField, top, out.Type)
		}
		return nil
	}
	if consumerST == nil || consumerST.Spec == nil || out.Type == "" {
		return nil
	}
	// A consumer field that is not on its template has nothing to compare against.
	if expected, lookupErr := getNestedValue(consumerST.Spec, fieldPath); lookupErr == nil {
		if want := jsonTypeOf(expected); want != "" && !outputTypeBinds(out.Type, want) {
			return fmt.Errorf("%w: %s.%s is %s, consumer field %s is %s",
				ErrCELOutputTypeMismatch, ref.ResourceName, ref.OutputField, out.Type, fieldPath, want)
		}
	}
	return nil
}

// splitOutputPath separates the output name from any path into it
// ("ip[0].address" -> "ip", "[0].address"; "host" -> "host", "").
func splitOutputPath(field string) (string, string) {
	if i := strings.IndexAny(field, ".["); i >= 0 {
		return field[:i], field[i:]
	}
	return field, ""
}

func declaredOutputNames(slug string) []string {
	names := make([]string, 0, len(servicetypes.UDLMOutputs[slug]))
	for n := range servicetypes.UDLMOutputs[slug] {
		names = append(names, n)
	}
	slices.Sort(names)
	return names
}

// jsonTypeOf names the JSON type of a template value; "" when unknown or null.
func jsonTypeOf(v any) string {
	switch v.(type) {
	case string:
		return "string"
	case float64, float32, int, int32, int64:
		return "number"
	case bool:
		return "boolean"
	case []any:
		return "array"
	case map[string]any:
		return "object"
	default:
		return ""
	}
}

// outputTypeBinds reports whether a declared output type can bind to a consumer
// field of the given JSON type.
func outputTypeBinds(outputType, fieldType string) bool {
	switch outputType {
	case "integer", "number":
		return fieldType == "number"
	default:
		return outputType == fieldType
	}
}

func catalogResourcesByName(resources []model.CatalogResource) map[string]model.CatalogResource {
	byName := make(map[string]model.CatalogResource, len(resources))
	for _, r := range resources {
		byName[r.Name] = r
	}
	return byName
}
