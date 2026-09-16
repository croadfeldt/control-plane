package service

import (
	"cmp"
	"context"
	"fmt"
	"slices"

	"github.com/dcm-project/control-plane/internal/placement/sprm"
	"github.com/dcm-project/control-plane/internal/placement/store/model"
	"github.com/dcm-project/control-plane/internal/placement/types"
)

// pendingResourcesReadyAtLowestLevel returns PENDING resources whose
// requires_resources are all RUNNING, limited to the lowest ready dag_level.
func pendingResourcesReadyAtLowestLevel(resources model.ResourceList) []model.Resource {
	byName := make(map[string]model.Resource, len(resources))
	for _, r := range resources {
		byName[r.Name] = r
	}

	ready := make([]model.Resource, 0)
	for _, r := range resources {
		if r.Status != types.ResourceStatusPending {
			continue
		}
		depsReady := true
		for _, dep := range r.RequiresResources {
			d, ok := byName[dep]
			if !ok || d.Status != types.ResourceStatusRunning {
				depsReady = false
				break
			}
		}
		if depsReady {
			ready = append(ready, r)
		}
	}

	slices.SortFunc(ready, func(a, b model.Resource) int {
		if a.DagLevel != b.DagLevel {
			return cmp.Compare(a.DagLevel, b.DagLevel)
		}
		return cmp.Compare(a.Name, b.Name)
	})
	if len(ready) == 0 {
		return ready
	}

	lowestLevel := ready[0].DagLevel
	atLevel := make([]model.Resource, 0, len(ready))
	for _, r := range ready {
		if r.DagLevel != lowestLevel {
			break
		}
		atLevel = append(atLevel, r)
	}
	return atLevel
}

// runningResourceOutputsByName loads output_spec for each RUNNING resource from SPRM.
func runningResourceOutputsByName(ctx context.Context, sprmClient sprm.Client, resources model.ResourceList) (map[string]map[string]any, error) {
	outputs := make(map[string]map[string]any)
	for _, r := range resources {
		if r.Status != types.ResourceStatusRunning {
			continue
		}
		resp, err := sprmClient.GetOutputSpec(ctx, r.ID)
		if err != nil {
			return nil, fmt.Errorf("resource %s: %w", r.Name, err)
		}
		if len(resp.OutputSpec) > 0 {
			outputs[r.Name] = resp.OutputSpec
		}
	}
	return outputs, nil
}

// highestPendingDeletionLevel returns the highest dag_level among PENDING_DELETION
// resources, or -1 when none remain.
func highestPendingDeletionLevel(resources []model.Resource) int {
	nextLevel := -1
	for _, r := range resources {
		if r.Status != types.ResourceStatusPendingDeletion {
			continue
		}
		if r.DagLevel > nextLevel {
			nextLevel = r.DagLevel
		}
	}
	return nextLevel
}

// resourcesAtDeletionLevel returns PENDING_DELETION resources at the given dag_level.
func resourcesAtDeletionLevel(resources []model.Resource, level int) []model.Resource {
	atLevel := make([]model.Resource, 0)
	for _, r := range resources {
		if r.DagLevel == level && r.Status == types.ResourceStatusPendingDeletion {
			atLevel = append(atLevel, r)
		}
	}
	return atLevel
}

// resourceStatusBlocksCreateProgression reports whether a late RUNNING callback
// must not restart create orchestration (for example during teardown).
func resourceStatusBlocksCreateProgression(status string) bool {
	switch status {
	case types.ResourceStatusPendingDeletion,
		types.ResourceStatusDeleting,
		types.ResourceStatusDeleted,
		types.ResourceStatusFailed:
		return true
	default:
		return false
	}
}

// allResourcesDeleted reports whether every resource in the run is DELETED.
func allResourcesDeleted(resources []model.Resource) bool {
	if len(resources) == 0 {
		return false
	}
	for _, r := range resources {
		if r.Status != types.ResourceStatusDeleted {
			return false
		}
	}
	return true
}
