package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/dcm-project/control-plane/internal/cel"
	"github.com/dcm-project/control-plane/internal/placement/logging"
	"github.com/dcm-project/control-plane/internal/placement/sprm"
	"github.com/dcm-project/control-plane/internal/placement/store"
	"github.com/dcm-project/control-plane/internal/placement/types"
)

// OnResourceRunning progresses create orchestration for a run after a resource
// reaches RUNNING state.
func (s *PlacementService) OnResourceRunning(ctx context.Context, event types.ResourceStatusEvent) error {
	log := logging.FromContext(ctx)
	resourceID := event.ResourceID

	// Step 1: Load the resource that emitted RUNNING and persist its status.
	resource, err := s.store.Resource().Get(ctx, resourceID)
	if err != nil {
		if errors.Is(err, store.ErrResourceNotFound) {
			return NewNotFoundError(fmt.Sprintf("resource %s not found", resourceID))
		}
		return NewInternalError(fmt.Sprintf("failed to load resource %s: %v", resourceID, err))
	}
	if resourceStatusBlocksCreateProgression(resource.Status) {
		log.Debug("Ignoring RUNNING callback during teardown or terminal state",
			"run_id", resource.RunID,
			"resource_id", resourceID,
			"status", resource.Status,
		)
		return nil
	}
	if err := s.store.Resource().UpdateStatus(ctx, resourceID, types.ResourceStatusRunning); err != nil {
		return NewInternalError(fmt.Sprintf("failed to set RUNNING status for resource %s: %v", resourceID, err))
	}

	// Step 2: Reload the full run
	resources, err := s.store.Resource().ListByRunID(ctx, resource.RunID)
	if err != nil {
		return NewInternalError(fmt.Sprintf("failed to list run %s resources: %v", resource.RunID, err))
	}
	for i := range resources {
		if resources[i].ID == resourceID {
			resources[i].Status = types.ResourceStatusRunning
			break
		}
	}

	// Step 3: Find PENDING resources whose dependencies are all RUNNING.
	ready := pendingResourcesReadyAtLowestLevel(resources)
	if len(ready) == 0 {
		return nil
	}

	// Step 4: Provision only the next DAG level resource(s)
	outputsByName, err := runningResourceOutputsByName(ctx, s.sprm, resources)
	if err != nil {
		return handleSPRMError(err)
	}
	for _, r := range ready {
		refs, err := cel.CollectReferences(r.Spec)
		if err != nil {
			return NewValidationError(fmt.Sprintf("resource %s: %v", r.Name, err))
		}
		for _, ref := range refs {
			log.Info("audit: CEL output field consumed",
				"run_id", r.RunID,
				"resource_id", r.ID,
				"resource_name", r.Name,
				"source_resource", ref.ResourceName,
				"output_field", ref.OutputField,
			)
		}

		// Step 5: Bind apply-time CEL references from RUNNING dependency outputs.
		boundSpec, err := cel.BindReferences(r.Spec, outputsByName)
		if err != nil {
			return NewValidationError(fmt.Sprintf("resource %s: %v", r.Name, err))
		}

		availableAgents, err := s.listAvailableAgents(ctx)
		if err != nil {
			return err
		}

		// Step 6: Re-evaluate policy at provision time so agent selection reflects
		// current policy state and the bound spec.
		evaluated, err := s.evaluateResourcePolicy(ctx, r.ID, boundSpec, availableAgents)
		if err != nil {
			return err
		}
		if err := s.store.Resource().UpdatePlacementDecision(ctx, r.ID, evaluated.SelectedAgent, evaluated.Status); err != nil {
			return NewInternalError(fmt.Sprintf("failed to update placement decision for resource %s: %v", r.ID, err))
		}

		// Step 7: Provision via SPRM using the evaluated spec (not the raw stored spec).
		log.Debug("Provisioning next DAG level resource via SPRM",
			"run_id", r.RunID,
			"resource_id", r.ID,
			"name", r.Name,
			"dag_level", r.DagLevel,
			"agent", evaluated.SelectedAgent,
		)
		if _, err := s.sprm.CreateResource(ctx, sprm.CreateResourceRequest{
			ID:        r.ID,
			Spec:      evaluated.EvaluatedSpec,
			AgentName: evaluated.SelectedAgent,
		}); err != nil {
			log.Error("Failed to progress DAG create after RUNNING",
				"run_id", r.RunID,
				"resource_id", r.ID,
				"dag_level", r.DagLevel,
				"error", err,
			)
			return handleSPRMError(err)
		}
	}
	return nil
}

// OnResourceDeleted progresses reverse-DAG deletion after a resource reaches DELETED.
func (s *PlacementService) OnResourceDeleted(ctx context.Context, resourceID string) error {
	resource, err := s.store.Resource().Get(ctx, resourceID)
	if err != nil {
		if errors.Is(err, store.ErrResourceNotFound) {
			return NewNotFoundError(fmt.Sprintf("resource %s not found", resourceID))
		}
		return NewInternalError(fmt.Sprintf("failed to load resource %s: %v", resourceID, err))
	}
	if err := s.store.Resource().UpdateStatus(ctx, resourceID, types.ResourceStatusDeleted); err != nil {
		return NewInternalError(fmt.Sprintf("failed to set DELETED status for resource %s: %v", resourceID, err))
	}
	return s.progressRunDeletion(ctx, resource.RunID)
}

func (s *PlacementService) progressRunDeletion(ctx context.Context, runID string) error {
	log := logging.FromContext(ctx)
	for {
		resources, err := s.store.Resource().ListByRunID(ctx, runID)
		if err != nil {
			return NewInternalError(fmt.Sprintf("failed to list run %s resources: %v", runID, err))
		}
		if len(resources) == 0 {
			return nil
		}

		// Wait until all in-flight deletions are finalized before starting the next dag level.
		for _, r := range resources {
			if r.Status == types.ResourceStatusDeleting {
				return nil
			}
		}

		nextLevel := highestPendingDeletionLevel(resources)
		if nextLevel < 0 {
			if allResourcesDeleted(resources) {
				if err := s.store.Resource().DeleteByRunID(ctx, runID); err != nil {
					if errors.Is(err, store.ErrResourceNotFound) {
						return nil
					}
					return NewInternalError(fmt.Sprintf("failed to clean up completed run %s: %v", runID, err))
				}
			}
			return nil
		}

		anyDeleting := false
		for _, r := range resourcesAtDeletionLevel(resources, nextLevel) {
			// Mark DELETING before dispatch so a fast DELETED callback can't be overwritten.
			if err := s.store.Resource().UpdateStatus(ctx, r.ID, types.ResourceStatusDeleting); err != nil {
				return NewInternalError(fmt.Sprintf("failed to set DELETING status for resource %s: %v", r.ID, err))
			}
			if err := s.sprm.DeleteResource(ctx, r.ID); err != nil {
				var httpErr *sprm.HTTPError
				if errors.As(err, &httpErr) && httpErr.StatusCode == http.StatusNotFound {
					log.Warn("SPRM resource already absent during reverse-DAG delete progression",
						"run_id", runID,
						"resource_id", r.ID,
						"dag_level", r.DagLevel,
					)
					// No DELETED event will arrive for absent resources; complete locally.
					if err := s.store.Resource().UpdateStatus(ctx, r.ID, types.ResourceStatusDeleted); err != nil {
						return NewInternalError(fmt.Sprintf("failed to set DELETED status for resource %s: %v", r.ID, err))
					}
				} else {
					return handleSPRMError(err)
				}
				continue
			}
			anyDeleting = true
		}
		if anyDeleting {
			return nil
			// else: entire level was already absent in SPRM, keep progressing now.
		}
	}
}

// OnResourceFailed halts progression and starts run teardown.
func (s *PlacementService) OnResourceFailed(ctx context.Context, resourceID string) error {
	resource, err := s.store.Resource().Get(ctx, resourceID)
	if err != nil {
		if errors.Is(err, store.ErrResourceNotFound) {
			return NewNotFoundError(fmt.Sprintf("resource %s not found", resourceID))
		}
		return NewInternalError(fmt.Sprintf("failed to load resource %s: %v", resourceID, err))
	}
	if err := s.store.Resource().UpdateStatus(ctx, resourceID, types.ResourceStatusFailed); err != nil {
		return NewInternalError(fmt.Sprintf("failed to set FAILED status for resource %s: %v", resourceID, err))
	}
	return s.DeleteRun(ctx, resource.RunID)
}
