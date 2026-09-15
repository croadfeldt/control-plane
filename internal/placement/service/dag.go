package service

import (
	"fmt"

	"github.com/dcm-project/control-plane/internal/placement/types"
)

// assignDagLevels computes topological levels from requires_resources.
// Returns a map of resource name → dag_level, or an error on unknown deps / cycles.
func assignDagLevels(resources []types.ResourceInput) (map[string]int, error) {
	byName := make(map[string]types.ResourceInput, len(resources))
	indegree := make(map[string]int, len(resources))
	dependents := make(map[string][]string, len(resources))

	for _, r := range resources {
		if r.Name == "" {
			return nil, NewValidationError("resource name is required")
		}
		if _, exists := byName[r.Name]; exists {
			return nil, NewValidationError(fmt.Sprintf("duplicate resource name %q", r.Name))
		}
		byName[r.Name] = r
		indegree[r.Name] = 0
	}

	for _, r := range resources {
		seen := make(map[string]struct{}, len(r.RequiresResources))
		for _, dep := range r.RequiresResources {
			if _, ok := byName[dep]; !ok {
				return nil, NewValidationError(fmt.Sprintf("resource %q requires unknown resource %q", r.Name, dep))
			}
			if _, dup := seen[dep]; dup {
				continue
			}
			seen[dep] = struct{}{}
			indegree[r.Name]++
			dependents[dep] = append(dependents[dep], r.Name)
		}
	}

	levels := make(map[string]int, len(resources))
	queue := make([]string, 0, len(resources))
	for name, deg := range indegree {
		if deg == 0 {
			queue = append(queue, name)
			levels[name] = 0
		}
	}

	processed := 0
	for len(queue) > 0 {
		name := queue[0]
		queue = queue[1:]
		processed++
		for _, child := range dependents[name] {
			indegree[child]--
			if candidate := levels[name] + 1; candidate > levels[child] {
				levels[child] = candidate
			}
			if indegree[child] == 0 {
				queue = append(queue, child)
			}
		}
	}

	if processed != len(resources) {
		return nil, NewValidationError("circular dependency detected in resources graph")
	}
	return levels, nil
}
