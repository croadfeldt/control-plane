package service

import (
	"context"
	"errors"
	"fmt"
	"slices"

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

	// Interim check: field key presence on the service type template only.
	// Enhancement #99 will add separate output definitions on the service type (parallel
	// to input schemas); until then CEL cannot distinguish declared outputs from input
	// fields (e.g. ${db.engine} passes if engine exists on the template).
	if !serviceTypeTemplateHasField(sourceST, ref.OutputField) {
		return fmt.Errorf("%w: service type %q has no field %q for %s.%s",
			ErrCELServiceTypeOutputNotFound, source.ServiceType, ref.OutputField, ref.ResourceName, ref.OutputField)
	}

	return nil
}

func catalogResourcesByName(resources []model.CatalogResource) map[string]model.CatalogResource {
	byName := make(map[string]model.CatalogResource, len(resources))
	for _, r := range resources {
		byName[r.Name] = r
	}
	return byName
}
