package service

import (
	"context"
	"fmt"
	"slices"

	"github.com/dcm-project/control-plane/api/catalog/v1alpha1"
	"github.com/dcm-project/control-plane/internal/catalog/store"
	"github.com/dcm-project/control-plane/internal/catalog/store/model"
)

// validateCatalogItemSpec checks a catalog item spec on create and update.
// Validates resources, required fields, and delegates to
// resource-specific rules. Does not build or order a DAG — that is
// placement's job at instance time.
func validateCatalogItemSpec(ctx context.Context, store store.Store, spec model.CatalogItemSpec) error {
	return validateCatalogResources(ctx, store, spec.Resources)
}

// validateCatalogResources validates a catalog at authoring time:
// unique resource names, resolvable service types, valid requires_resources
// references, per-resource depends_on cycles, and no cycles in
// requires_resources. CEL in field defaults is validated at instance merge time.
func validateCatalogResources(ctx context.Context, store store.Store, resources []model.CatalogResource) error {
	if len(resources) == 0 {
		return fmt.Errorf("%w: resources must not be empty", ErrCatalogItemSpecConflict)
	}

	seen := make(map[string]bool, len(resources))
	for _, r := range resources {
		if r.Name == "" {
			return fmt.Errorf("%w: resource name is required", ErrCatalogItemSpecConflict)
		}
		if seen[r.Name] {
			return fmt.Errorf("%w: %s", ErrCatalogItemResourceNameTaken, r.Name)
		}
		seen[r.Name] = true

		if r.ServiceType == "" {
			return fmt.Errorf("%w: resource %s service_type is required", ErrCatalogItemSpecConflict, r.Name)
		}
		if _, err := store.ServiceType().GetByServiceType(ctx, r.ServiceType); err != nil {
			return ErrServiceTypeNotFound
		}
		if err := validateFieldDependsOnCycles(r.Fields); err != nil {
			return fmt.Errorf("resource %s: %w", r.Name, err)
		}
	}

	for _, r := range resources {
		for _, dep := range r.RequiresResources {
			if !seen[dep] {
				return fmt.Errorf("%w: %s", ErrCatalogItemRequiresResourceNotFound, dep)
			}
		}
	}

	if err := validateRequiresResourcesCycles(resources); err != nil {
		return err
	}
	return nil
}

// detectDirectedCycle reports a cycle in a directed graph where edges[node] lists
// predecessor nodes that node depends on (each must be satisfied before node).
func detectDirectedCycle(edges map[string][]string, cycleErr error) error {
	if len(edges) == 0 {
		return nil
	}

	const (
		unvisited = 0
		visiting  = 1
		visited   = 2
	)
	state := make(map[string]int)

	var visit func(node string) error
	visit = func(node string) error {
		if state[node] == visited {
			return nil
		}
		if state[node] == visiting {
			return fmt.Errorf("%w: cycle involving %s", cycleErr, node)
		}
		state[node] = visiting
		for _, dep := range edges[node] {
			if err := visit(dep); err != nil {
				return err
			}
		}
		state[node] = visited
		return nil
	}

	for node := range edges {
		if err := visit(node); err != nil {
			return err
		}
	}
	return nil
}

// validateFieldDependsOnCycles checks that every depends_on path references an existing
// field and that there are no cyclic depends_on references within one field set.
func validateFieldDependsOnCycles(fields []model.FieldConfiguration) error {
	knownPaths := make(map[string]bool, len(fields))
	for _, f := range fields {
		if knownPaths[f.Path] {
			return fmt.Errorf("%w: %s", ErrCatalogItemFieldPathTaken, f.Path)
		}
		knownPaths[f.Path] = true
	}

	edges := make(map[string][]string)
	for _, f := range fields {
		if f.DependsOn == nil {
			continue
		}
		depPath := f.DependsOn.Path
		if !knownPaths[depPath] {
			return fmt.Errorf("%w: field %s depends_on path %q not found in fields", ErrDependsOnPathNotFound, f.Path, depPath)
		}
		edges[f.Path] = []string{depPath}
	}

	return detectDirectedCycle(edges, ErrDependsOnCycleDetected)
}

// validateRequiresResourcesCycles detects cycles in requires_resources edges.
// Authoring-time guard only; placement repeats DAG validation when admitting a run.
func validateRequiresResourcesCycles(resources []model.CatalogResource) error {
	edges := make(map[string][]string, len(resources))
	for _, r := range resources {
		edges[r.Name] = append([]string(nil), r.RequiresResources...)
	}
	return detectDirectedCycle(edges, ErrCatalogItemRequiresCycle)
}

// validateCatalogImmutable ensures structure is not changed on
// update (resource names, service types, requires_resources). Field defaults and
// validation rules within each resource may still change.
func validateCatalogImmutable(existing, updated model.CatalogItemSpec) error {
	if len(existing.Resources) != len(updated.Resources) {
		return ErrImmutableSpecStructureUpdate
	}

	updatedByName := make(map[string]model.CatalogResource, len(updated.Resources))
	for _, r := range updated.Resources {
		updatedByName[r.Name] = r
	}

	for _, oldR := range existing.Resources {
		newR, ok := updatedByName[oldR.Name]
		if !ok {
			return ErrImmutableSpecStructureUpdate
		}
		if oldR.ServiceType != newR.ServiceType ||
			!slices.Equal(oldR.RequiresResources, newR.RequiresResources) {
			return ErrImmutableSpecStructureUpdate
		}
	}
	return nil
}

// userValuesForResource returns user values that target the given resource name.
func userValuesForResource(userValues []v1alpha1.UserValue, resourceName string) []v1alpha1.UserValue {
	out := make([]v1alpha1.UserValue, 0)
	for _, uv := range userValues {
		if uv.Resource == resourceName {
			out = append(out, uv)
		}
	}
	return out
}

// validateUserValuesForCatalogItem checks instance user_values against the catalog.
func validateUserValuesForCatalogItem(spec model.CatalogItemSpec, userValues []v1alpha1.UserValue) error {
	known := make(map[string]bool, len(spec.Resources))
	for _, r := range spec.Resources {
		known[r.Name] = true
	}
	for _, uv := range userValues {
		if uv.Resource == "" {
			return ErrUserValueResourceRequired
		}
		if !known[uv.Resource] {
			return fmt.Errorf("%w: %s", ErrUserValueResourceNotFound, uv.Resource)
		}
	}
	return nil
}
