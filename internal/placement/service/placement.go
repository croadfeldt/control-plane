// Package service implements the core business logic for resource placement.
package service

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	placementagent "github.com/dcm-project/control-plane/internal/placement/agent"
	"github.com/dcm-project/control-plane/internal/placement/logging"
	"github.com/dcm-project/control-plane/internal/placement/policy"
	"github.com/dcm-project/control-plane/internal/placement/sprm"
	"github.com/dcm-project/control-plane/internal/placement/store"
	"github.com/dcm-project/control-plane/internal/placement/store/model"
	"github.com/dcm-project/control-plane/internal/placement/types"
	"github.com/google/uuid"
)

const resourceRollbackTimeout = 10 * time.Second

// PlacementService handles business logic for placement request management.
type PlacementService struct {
	store       store.Store
	policy      policy.Client
	sprm        sprm.Client
	agentClient placementagent.Client
}

// NewPlacementService creates a new PlacementService with the given store, policy client, and SPRM client.
func NewPlacementService(store store.Store, policyClient policy.Client, sprmClient sprm.Client, opts ...func(*PlacementService)) *PlacementService {
	ps := &PlacementService{
		store:  store,
		policy: policyClient,
		sprm:   sprmClient,
	}
	for _, opt := range opts {
		opt(ps)
	}
	return ps
}

// WithAgentClient sets the agent client used to list ready agents for
// policy evaluation.
func WithAgentClient(client placementagent.Client) func(*PlacementService) {
	return func(ps *PlacementService) {
		ps.agentClient = client
	}
}

// CreateRun executes a placement run for one or more resources.
func (s *PlacementService) CreateRun(ctx context.Context, req *types.CreateRunRequest) (*types.Run, error) {
	log := logging.FromContext(ctx)

	// step 1: validate inputs
	if err := s.validateCreateRunRequest(ctx, req); err != nil {
		return nil, err
	}

	// step 2: assign dag levels
	resourceNameDagLevelMap, err := assignDagLevels(req.Resources)
	if err != nil {
		return nil, err
	}

	runID := req.RunId
	log.Debug("Creating run",
		"run_id", runID,
		"catalog_item_instance_id", req.CatalogItemInstanceId,
		"resource_count", len(req.Resources),
	)

	type preparedResource struct {
		resource      model.Resource
		evaluatedSpec map[string]any
	}

	availableAgents, err := s.listAvailableAgents(ctx)
	if err != nil {
		log.Error("Failed to list available agents", "error", err)
		return nil, err
	}

	// step 3: evaluate policy for each resource
	prepared := make([]preparedResource, 0, len(req.Resources))
	for _, resource := range req.Resources {
		// Get or generate ID
		resourceID := getOrGenerateStringId(resource.ID)
		// Generate path
		path := fmt.Sprintf("resources/%s", resourceID)

		// Evaluate spec with policy engine
		log.Debug("Evaluating policy", "run_id", runID, "resource_id", resourceID, "name", resource.Name)
		evaluated, err := s.evaluateResourcePolicy(ctx, resourceID, resource.Spec, availableAgents)
		if err != nil {
			return nil, err
		}

		prepared = append(prepared, preparedResource{
			resource: model.Resource{
				ID:                    resourceID,
				RunID:                 runID,
				CatalogItemInstanceId: req.CatalogItemInstanceId,
				Name:                  resource.Name,
				Spec:                  resource.Spec,
				RequiresResources:     append([]string(nil), resource.RequiresResources...),
				DagLevel:              resourceNameDagLevelMap[resource.Name],
				Status:                types.ResourceStatusPending,
				Path:                  path,
				AgentName:             &evaluated.SelectedAgent,
				ApprovalStatus:        &evaluated.Status,
			},
			evaluatedSpec: evaluated.EvaluatedSpec,
		})
	}

	// step 4: persist resources in store
	rows := make([]model.Resource, 0, len(prepared))
	for _, p := range prepared {
		rows = append(rows, p.resource)
	}
	createdModels, err := s.store.Resource().CreateBatch(ctx, rows)
	if err != nil {
		_ = s.rollbackRunDelete(runID)
		if errors.Is(err, store.ErrResourceIdExist) {
			log.Warn("Duplicate resource ID in create run", "run_id", runID)
			return nil, NewConflictError("one or more resources already exists")
		}
		log.Error("Failed to create resources in store", "run_id", runID, "error", err)
		return nil, NewInternalError(fmt.Sprintf("failed to create database records for run %s: %v", runID, err))
	}

	// step 5: provision dag_level 0 synchronously; higher levels are progressed by
	// OnResourceRunning status callbacks.
	slices.SortFunc(prepared, func(a, b preparedResource) int {
		if a.resource.DagLevel != b.resource.DagLevel {
			return cmp.Compare(a.resource.DagLevel, b.resource.DagLevel)
		}
		return cmp.Compare(a.resource.Name, b.resource.Name)
	})

	var provisionedIDs []string
	for _, p := range prepared {
		if p.resource.DagLevel != 0 {
			continue
		}
		sprmRequest := sprm.CreateResourceRequest{
			ID:        p.resource.ID,
			Spec:      p.evaluatedSpec,
			AgentName: *p.resource.AgentName,
		}
		log.Debug("Provisioning resource via SPRM",
			"run_id", runID,
			"resource_id", p.resource.ID,
			"name", p.resource.Name,
		)
		if _, err := s.sprm.CreateResource(ctx, sprmRequest); err != nil {
			// SPRM call failed, rollback provisioned resources and DB records
			log.Error("SPRM provisioning failed, rolling back create run",
				"run_id", runID,
				"resource_id", p.resource.ID,
				"error", err,
			)
			s.rollbackProvisioned(provisionedIDs)
			if delErr := s.rollbackRunDelete(runID); delErr != nil {
				log.Error("Failed to rollback run after SPRM error",
					"run_id", runID,
					"db_error", delErr,
					"sprm_error", err,
				)
			}
			return nil, handleSPRMError(err)
		}
		provisionedIDs = append(provisionedIDs, p.resource.ID)
	}

	log.Info("Run created successfully",
		"run_id", runID,
		"catalog_item_instance_id", req.CatalogItemInstanceId,
		"resource_count", len(createdModels),
		"level0_provisioned", len(provisionedIDs),
	)
	return storeModelsToRun(createdModels), nil
}

// GetRun returns a run by run_id.
func (s *PlacementService) GetRun(ctx context.Context, runID string) (*types.Run, error) {
	log := logging.FromContext(ctx)
	resources, err := s.store.Resource().ListByRunID(ctx, runID)
	if err != nil {
		log.Error("Failed to get run", "run_id", runID, "error", err)
		return nil, NewInternalError(fmt.Sprintf("failed to retrieve run %s: %v", runID, err))
	}
	if len(resources) == 0 {
		return nil, NewNotFoundError(fmt.Sprintf("run %s not found", runID))
	}
	return storeModelsToRun(resources), nil
}

// ListRun lists runs (resources grouped by run_id).
// Pagination is by distinct run_id.
func (s *PlacementService) ListRun(ctx context.Context, opts *store.ResourceListOptions) (*types.ListRunResult, error) {
	log := logging.FromContext(ctx)
	result, err := s.store.Resource().ListRun(ctx, opts)
	if err != nil {
		log.Error("Failed to list runs", "error", err)
		return nil, NewInternalError(fmt.Sprintf("failed to list runs: %v", err))
	}

	byRun := make(map[string]model.ResourceList)
	order := make([]string, 0)
	for i := range result.Resources {
		r := result.Resources[i]
		if _, ok := byRun[r.RunID]; !ok {
			order = append(order, r.RunID)
		}
		byRun[r.RunID] = append(byRun[r.RunID], r)
	}

	runs := make([]types.Run, 0, len(order))
	for _, runID := range order {
		run := storeModelsToRun(byRun[runID])
		if run != nil {
			runs = append(runs, *run)
		}
	}

	return &types.ListRunResult{
		Runs:          runs,
		NextPageToken: result.NextPageToken,
	}, nil
}

// DeleteRun starts deletion for a run by run_id.
func (s *PlacementService) DeleteRun(ctx context.Context, runID string) error {
	log := logging.FromContext(ctx)
	log.Debug("Deleting run", "run_id", runID)

	resources, err := s.store.Resource().ListByRunID(ctx, runID)
	if err != nil {
		log.Error("Failed to list resources for delete", "run_id", runID, "error", err)
		return NewInternalError(fmt.Sprintf("failed to retrieve run for deletion: %v", err))
	}
	if len(resources) == 0 {
		return NewNotFoundError(fmt.Sprintf("run %s not found", runID))
	}

	// Mark all resources in the run as PENDING_DELETION
	if err := s.store.Resource().UpdateStatusByRunID(ctx, runID, types.ResourceStatusPendingDeletion); err != nil {
		log.Error("Failed to mark resources as pending deletion", "run_id", runID, "error", err)
		return NewInternalError(fmt.Sprintf("failed to mark run %s as pending deletion: %v", runID, err))
	}

	return s.progressRunDeletion(ctx, runID)
}

// RehydrateResource re-evaluates an existing resource against current policies
// and creates a new resource under newRunID. The old resource is deleted after
// the new one is successfully provisioned.
// TODO: Rehydrate all resources in the run (e.g. via CreateRun), then delete the
// old run in reverse DAG order (similar to DeleteRun) instead of migrating run_id.
func (s *PlacementService) RehydrateResource(ctx context.Context, runID, newRunID string) (*types.Resource, error) {
	log := logging.FromContext(ctx)
	log.Debug("Rehydrating run",
		"run_id", runID,
		"new_run_id", newRunID,
	)
	if runID == "" {
		return nil, NewValidationError("run_id is required")
	}
	if newRunID == "" {
		return nil, NewValidationError("new_run_id is required")
	}

	resources, err := s.store.Resource().ListByRunID(ctx, runID)
	if err != nil {
		log.Error("Failed to list resources for rehydration", "run_id", runID, "error", err)
		return nil, NewInternalError(fmt.Sprintf("failed to list resources for run %s: %v", runID, err))
	}
	if len(resources) == 0 {
		return nil, NewNotFoundError(fmt.Sprintf("run %s not found", runID))
	}
	if len(resources) != 1 {
		return nil, NewValidationError(fmt.Sprintf("rehydrate currently supports only single-resource runs, got %d resources", len(resources)))
	}

	// Step 1: Retrieve the old resource
	oldResource := resources[0]
	resourceID := oldResource.ID
	// Generate UUID for the replacement resource
	newResourceID := uuid.New().String()

	// Step 2: Re-evaluate the original spec through policy
	availableAgents, err := s.listAvailableAgents(ctx)
	if err != nil {
		log.Error("Failed to list available agents for rehydration", "error", err)
		return nil, err
	}
	log.Debug("Re-evaluating policy for rehydration", "resource_id", resourceID)
	evaluated, err := s.evaluateResourcePolicy(ctx, resourceID, oldResource.Spec, availableAgents)
	if err != nil {
		return nil, err
	}
	// Step 3: Create new resource in DB
	newPath := fmt.Sprintf("resources/%s", newResourceID)
	newResource := model.Resource{
		ID:                    newResourceID,
		RunID:                 newRunID,
		CatalogItemInstanceId: oldResource.CatalogItemInstanceId,
		Name:                  oldResource.Name,
		Spec:                  oldResource.Spec,
		RequiresResources:     append([]string(nil), oldResource.RequiresResources...),
		DagLevel:              oldResource.DagLevel,
		Status:                types.ResourceStatusPending,
		Path:                  newPath,
		ApprovalStatus:        &evaluated.Status,
		AgentName:             &evaluated.SelectedAgent,
	}

	// Step 3: Create new resource in DB
	created, err := s.store.Resource().Create(ctx, newResource)
	if err != nil {
		if errors.Is(err, store.ErrResourceIdExist) {
			log.Warn("Duplicate new resource ID during rehydration", "new_resource_id", newResourceID)
			return nil, NewConflictError(fmt.Sprintf("resource with id %s already exists", newResourceID))
		}
		log.Error("Failed to create new resource during rehydration", "new_resource_id", newResourceID, "error", err)
		return nil, NewInternalError(fmt.Sprintf("failed to create database record for resource %s: %v", newResourceID, err))
	}

	// Step 4: Provision new resource in SPRM
	sprmRequest := sprm.CreateResourceRequest{
		ID:        newResourceID,
		Spec:      evaluated.EvaluatedSpec,
		AgentName: evaluated.SelectedAgent,
	}
	if _, err = s.sprm.CreateResource(ctx, sprmRequest); err != nil {
		log.Error("SPRM provisioning failed during rehydration, rolling back", "new_resource_id", newResourceID, "error", err)
		// Rollback the new DB record
		if delErr := s.rollbackResourceDelete(newResourceID); delErr != nil {
			log.Error("Failed to rollback new resource after SPRM error",
				"new_resource_id", newResourceID,
				"db_error", delErr,
				"sprm_error", err,
			)
		}
		return nil, handleSPRMError(err)
	}

	// Step 5: Delete old resource from SPRM (deferred - non-blocking)
	if err := s.sprm.DeleteResourceDeferred(ctx, resourceID); err != nil {
		log.Error("SPRM deferred deletion failed during rehydration (non-blocking)",
			"resource_id", resourceID,
			"error", err,
		)
	}
	// Step 6: Delete old resource from DB
	if err := s.store.Resource().Delete(ctx, resourceID); err != nil {
		log.Error("Failed to delete old resource from DB during rehydration (non-blocking)",
			"resource_id", resourceID,
			"error", err,
		)
	}

	log.Info("Run rehydrated successfully",
		"old_run_id", runID,
		"new_run_id", newRunID,
		"old_resource_id", resourceID,
		"new_resource_id", newResourceID,
		"catalog_item_instance_id", oldResource.CatalogItemInstanceId,
		"agent", evaluated.SelectedAgent,
		"approval_status", evaluated.Status,
	)

	res := storeModelToResource(created)
	return &res, nil
}

func (s *PlacementService) rollbackProvisioned(resourceIDs []string) {
	for _, id := range resourceIDs {
		rbCtx, cancel := context.WithTimeout(context.Background(), resourceRollbackTimeout)
		if err := s.sprm.DeleteResource(rbCtx, id); err != nil {
			logging.FromContext(rbCtx).Error("Failed to tear down provisioned resource during create rollback",
				"resource_id", id,
				"error", err,
			)
		}
		cancel()
	}
}

func (s *PlacementService) rollbackRunDelete(runID string) error {
	rbCtx, cancel := context.WithTimeout(context.Background(), resourceRollbackTimeout)
	defer cancel()
	err := s.store.Resource().DeleteByRunID(rbCtx, runID)
	if errors.Is(err, store.ErrResourceNotFound) {
		return nil
	}
	return err
}

func (s *PlacementService) rollbackResourceDelete(id string) error {
	rbCtx, cancel := context.WithTimeout(context.Background(), resourceRollbackTimeout)
	defer cancel()
	return s.store.Resource().Delete(rbCtx, id)
}

// ReEvaluateWithExclude re-evaluates placement for an existing resource,
// excluding the given agents (typically an agent that just failed or timed
// out), and re-provisions the resource against the newly selected agent.
// This is the core of the self-healing loop invoked by the pending/queued
// sweep: it does not create a new resource, it re-points the existing one.
//
// It also proactively reassigns any run-sibling still pointed at an
// excluded agent, best-effort, instead of leaving it to wait for its own
// independent sweep timeout.
func (s *PlacementService) ReEvaluateWithExclude(ctx context.Context, resourceID string, excludeAgents []string) error {
	log := logging.FromContext(ctx)

	resource, err := s.store.Resource().Get(ctx, resourceID)
	if err != nil {
		if errors.Is(err, store.ErrResourceNotFound) {
			return NewNotFoundError(fmt.Sprintf("resource %s not found", resourceID))
		}
		return NewInternalError(fmt.Sprintf("failed to get resource: %v", err))
	}

	availableAgents, err := s.listAvailableAgents(ctx)
	if err != nil {
		log.Error("Failed to list available agents for re-evaluation", "error", err)
		return err
	}

	// The agent this resource was on when the caller (the pending/queued
	// sweep) decided to reassign it: see reassignOne's expectedCurrentAgent
	// doc for why this must be the caller's observation, not a fresh read.
	var expectedCurrentAgent string
	if resource.AgentName != nil {
		expectedCurrentAgent = *resource.AgentName
	}

	newAgent, err := s.reassignOne(ctx, resourceID, resource.Spec, expectedCurrentAgent, excludeAgents, availableAgents)
	if err != nil {
		return err
	}
	log.Info("Resource re-evaluated and reassigned", "resource_id", resourceID, "new_agent", newAgent)

	s.reassignExcludedSiblings(ctx, resource, excludeAgents, availableAgents)

	return nil
}

// reassignOne evaluates policy for a single resource excluding
// excludeAgents and, on success, reassigns it in SPRM and persists the new
// agent_name. Shared by ReEvaluateWithExclude for the primary resource and
// by reassignExcludedSiblings for its run-siblings.
//
// expectedCurrentAgent is the agent this resource was observed on by the
// caller (primary: the resource's own record; sibling: the sibling's own
// record) at decision time. It's passed through to SPRM/SP unchanged so the
// eventual CAS there guards against the exact race this function runs
// concurrently with: another healer reassigning the same resource/instance
// between this function's policy evaluation and its own reassignment call.
func (s *PlacementService) reassignOne(ctx context.Context, resourceID string, spec map[string]any, expectedCurrentAgent string, excludeAgents []string, availableAgents []policy.AgentInfo) (string, error) {
	log := logging.FromContext(ctx)

	policyResponse, err := s.policy.Evaluate(ctx, policy.EvaluateRequest{
		Spec:            spec,
		ExcludeAgents:   excludeAgents,
		AvailableAgents: availableAgents,
	})
	if err != nil {
		log.Error("Re-evaluation failed", "resource_id", resourceID, "error", err)
		return "", handlePolicyError(err)
	}

	if policyResponse.SelectedAgent == "" {
		return "", NewPolicyInternalError("re-evaluation found no available agent")
	}

	// Defensive re-check: nothing stops a misbehaving policy from returning
	// an excluded agent anyway, which would reassign the instance right
	// back to the agent it was just excluded to avoid.
	for _, excluded := range excludeAgents {
		if policyResponse.SelectedAgent == excluded {
			return "", NewPolicyInternalError(fmt.Sprintf("re-evaluation selected excluded agent %q", excluded))
		}
	}

	if err := s.sprm.ReassignResource(ctx, resourceID, policyResponse.SelectedAgent, expectedCurrentAgent); err != nil {
		log.Error("Failed to reassign resource to new agent", "resource_id", resourceID, "new_agent", policyResponse.SelectedAgent, "error", err)
		return "", handleSPRMError(err)
	}

	// Propagated rather than swallowed: leaving this silent would let
	// Resource.agent_name go stale, and retrying is safe since this is
	// idempotent (ReassignAndReset is a CAS).
	if err := s.store.Resource().UpdateAgentName(ctx, resourceID, policyResponse.SelectedAgent); err != nil {
		log.Error("Failed to update resource agent_name after re-evaluation", "resource_id", resourceID, "error", err)
		return "", NewInternalError(fmt.Sprintf("failed to update resource agent_name: %v", err))
	}

	return policyResponse.SelectedAgent, nil
}

// reassignExcludedSiblings proactively reassigns run-siblings of resource
// that are still pointed at an excluded agent. Best-effort: a sibling
// failure is logged and skipped, never propagated to the caller, since the
// primary resource's own reassignment (already done by the time this runs)
// is what the self-heal loop depends on for its retry decision.
//
// reassignOne's underlying CAS (ReassignAndReset) only accepts
// pending/cancelled instances, so a sibling that's actively
// provisioning/running is automatically left alone. A queued sibling is
// also left alone here — its cancel-then-heal transition is handled by
// sweepQueued on its own timeout, which this deliberately doesn't
// replicate to keep this change bounded.
func (s *PlacementService) reassignExcludedSiblings(ctx context.Context, resource *model.Resource, excludeAgents []string, availableAgents []policy.AgentInfo) {
	log := logging.FromContext(ctx)

	// Goes through the public GetRun rather than the store directly: it's
	// the same run-scoped fetch either way (resource is already a known
	// member of this RunID, so GetRun's zero-resources NotFoundError can't
	// trigger here), and this is what the reviewer explicitly asked for.
	run, err := s.GetRun(ctx, resource.RunID)
	if err != nil {
		log.Error("Failed to get run for proactive sibling reassignment", "run_id", resource.RunID, "error", err)
		return
	}

	for _, sibling := range run.Resources {
		siblingID := ""
		if sibling.Id != nil {
			siblingID = *sibling.Id
		}
		if siblingID == resource.ID || sibling.AgentName == nil || !slices.Contains(excludeAgents, *sibling.AgentName) {
			continue
		}
		newAgent, err := s.reassignOne(ctx, siblingID, sibling.Spec, *sibling.AgentName, excludeAgents, availableAgents)
		if err != nil {
			log.Warn("Best-effort sibling reassignment failed, will retry on its own sweep timeout",
				"resource_id", siblingID, "run_id", resource.RunID, "error", err)
			continue
		}
		log.Info("Run-sibling proactively reassigned", "resource_id", siblingID, "run_id", resource.RunID, "new_agent", newAgent)
	}
}

// listAvailableAgents returns the current ready agents for policy evaluation,
// or nil if no agent client is configured. A listing failure is returned as a
// hard error rather than degrading to an empty slice: ConstraintContext and
// EvaluatePolicies treat an empty AvailableAgents as "skip membership/
// environment validation", so silently falling back there would turn an
// operational error into a fail-open placement decision.
func (s *PlacementService) listAvailableAgents(ctx context.Context) ([]policy.AgentInfo, error) {
	if s.agentClient == nil {
		return nil, nil
	}
	agents, err := s.agentClient.ListReadyAgents(ctx)
	if err != nil {
		return nil, NewInternalError(fmt.Sprintf("failed to list available agents: %v", err))
	}
	return toPolicyAgentInfo(agents), nil
}

// toPolicyAgentInfo maps the placement/agent package's Info to the parallel
// policy.AgentInfo used by policy.EvaluateRequest.
func toPolicyAgentInfo(agents []placementagent.Info) []policy.AgentInfo {
	out := make([]policy.AgentInfo, len(agents))
	for i, a := range agents {
		out[i] = policy.AgentInfo{Name: a.Name, Environment: a.Environment, ServiceTypes: a.ServiceTypes, Cost: a.Cost}
	}
	return out
}

func getOrGenerateStringId(id *string) string {
	if id != nil && *id != "" {
		return *id
	}
	// Generate UUID if not provided
	return uuid.New().String()
}

// evaluateResourcePolicy runs the policy engine against a resource spec and returns
// the selected agent, approval status, and evaluated spec for SPRM provisioning.
func (s *PlacementService) evaluateResourcePolicy(ctx context.Context, resourceID string, spec map[string]any, availableAgents []policy.AgentInfo) (*policy.EvaluateResponse, error) {
	log := logging.FromContext(ctx)
	log.Debug("Evaluating policy", "resource_id", resourceID)
	policyResponse, err := s.policy.Evaluate(ctx, policy.EvaluateRequest{
		Spec:            spec,
		AvailableAgents: availableAgents,
	})
	if err != nil {
		log.Error("Policy evaluation failed", "resource_id", resourceID, "error", err)
		return nil, handlePolicyError(err)
	}
	if policyResponse.SelectedAgent == "" {
		log.Error("Policy response missing selected agent",
			"resource_id", resourceID,
			"status", policyResponse.Status,
		)
		return nil, NewPolicyInternalError("policy response missing selected agent")
	}
	return policyResponse, nil
}
