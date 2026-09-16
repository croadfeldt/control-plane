package service

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/brunoga/deep/v4"
	"github.com/dcm-project/control-plane/internal/policy/logging"
	"github.com/dcm-project/control-plane/internal/policy/opa"
	"github.com/dcm-project/control-plane/internal/policy/store"
	"github.com/dcm-project/control-plane/internal/policy/store/model"
)

type EvaluationStatus string

const (
	EvaluationStatusApproved EvaluationStatus = "APPROVED"
	EvaluationStatusModified EvaluationStatus = "MODIFIED"
)

type EvaluationService interface {
	EvaluateRequest(ctx context.Context, req *EvaluationRequest) (*EvaluationResponse, error)
}

// AgentInfo is the subset of agent metadata policies need. Cost is "" when
// the agent didn't report one.
type AgentInfo struct {
	Name         string
	Environment  string
	ServiceTypes []string
	Cost         string
}

type EvaluationRequest struct {
	ServiceInstance map[string]any
	RequestLabels   map[string]string
	AvailableAgents []AgentInfo
	ExcludeAgents   []string
}

type EvaluationResponse struct {
	EvaluatedServiceInstance map[string]any
	SelectedAgent            string
	Status                   EvaluationStatus
}

type evaluationService struct {
	policyStore store.Policy
	engine      opa.Engine
}

func NewEvaluationService(policyStore store.Policy, engine opa.Engine) EvaluationService {
	return &evaluationService{
		policyStore: policyStore,
		engine:      engine,
	}
}

func (s *evaluationService) EvaluateRequest(ctx context.Context, req *EvaluationRequest) (*EvaluationResponse, error) {
	log := logging.FromContext(ctx)
	log.Debug("Starting policy evaluation", "label_count", len(req.RequestLabels))

	currentSpec, err := deep.Copy(req.ServiceInstance)
	if err != nil {
		return nil, NewInternalError("Failed to make a deep copy of the service instance spec", err.Error(), err)
	}

	constraintCtx := NewConstraintContext()

	totalAgents := len(req.AvailableAgents)
	availableAgents := filterExcluded(req.AvailableAgents, req.ExcludeAgents)

	// ValidateAgent (constraints.go) skips its membership check on an empty
	// available_agents list, so reject explicitly instead of letting a
	// policy pick an unvalidated agent. totalAgents == 0 (no agent client
	// configured) is left alone - that fail-open is intentional.
	if totalAgents > 0 && len(availableAgents) == 0 {
		log.Warn("All available agents were excluded", "excluded_count", len(req.ExcludeAgents))
		return nil, NewAllAgentsExcludedError(len(req.ExcludeAgents))
	}

	// Filtered here once rather than trusting every Rego policy to check
	// capability itself, so an incapable agent doesn't surface later as an
	// SP-side provisioning failure.
	serviceType, hasServiceType, err := resourceServiceType(req.ServiceInstance)
	if err != nil {
		return nil, NewInvalidArgumentError("Invalid service_type in resource spec", err.Error())
	}
	if hasServiceType {
		capableAgents := filterByServiceType(availableAgents, serviceType)
		if len(availableAgents) > 0 && len(capableAgents) == 0 {
			log.Warn("No available agent supports the requested service type",
				"service_type", serviceType, "agents_after_exclude", len(availableAgents))
			return nil, NewNoCapableAgentError(serviceType, len(availableAgents))
		}
		availableAgents = capableAgents
	}

	selectedAgent := ""

	var pageToken *string
	policiesEvaluated := 0
	policiesSkipped := 0
	for {
		policyListResult, err := s.policyStore.List(ctx, &store.PolicyListOptions{
			Filter: &store.PolicyFilter{
				Enabled: boolPtr(true),
			},
			PageSize:  1000,
			PageToken: pageToken,
		})
		if err != nil {
			log.Error("Failed to retrieve policies for evaluation", "error", err)
			return nil, NewInternalError("Failed to retrieve policies", err.Error(), err)
		}

		for _, policy := range policyListResult.Policies {
			if !MatchesLabelSelector(policy.LabelSelector, req.RequestLabels) {
				policiesSkipped++
				continue
			}

			log.Debug("Evaluating policy", "policy_id", policy.ID, "policy_type", policy.PolicyType, "priority", policy.Priority)

			currentSpec, selectedAgent, err = s.evaluatePolicy(ctx, &policy, currentSpec, selectedAgent, availableAgents, req.ExcludeAgents, constraintCtx)
			if err != nil {
				log.Warn("Policy evaluation failed", "policy_id", policy.ID, "error", err)
				return nil, err
			}
			policiesEvaluated++
		}

		if policyListResult.NextPageToken == "" {
			break
		}
		pageToken = &policyListResult.NextPageToken
	}

	status := EvaluationStatusApproved
	if !deep.Equal(req.ServiceInstance, currentSpec) {
		status = EvaluationStatusModified
	}

	log.Info("Policy evaluation completed",
		"status", status,
		"policies_evaluated", policiesEvaluated,
		"policies_skipped", policiesSkipped,
		"selected_agent", selectedAgent,
	)

	return &EvaluationResponse{
		EvaluatedServiceInstance: currentSpec,
		SelectedAgent:            selectedAgent,
		Status:                   status,
	}, nil
}

func (s *evaluationService) evaluatePolicy(
	ctx context.Context,
	policy *model.Policy,
	currentSpec map[string]any,
	selectedAgent string,
	availableAgents []AgentInfo,
	excludeAgents []string,
	constraintCtx *ConstraintContext,
) (map[string]any, string, error) {
	log := logging.FromContext(ctx)
	opaInput := map[string]any{
		"spec": currentSpec,
	}
	if len(availableAgents) > 0 {
		// Structured objects, not bare name strings, so Rego can reason
		// about an agent's environment/capability/cost too.
		agentsForOPA := make([]map[string]any, len(availableAgents))
		for i, a := range availableAgents {
			serviceTypes := a.ServiceTypes
			if serviceTypes == nil {
				serviceTypes = []string{} // avoid Rego null
			}
			agentsForOPA[i] = map[string]any{
				"name":          a.Name,
				"environment":   a.Environment,
				"service_types": serviceTypes,
				"cost":          a.Cost,
			}
		}
		opaInput["available_agents"] = agentsForOPA
	}
	// available_agents is already pre-filtered by filterExcluded, so
	// policies can't see which agents were excluded from it (F38). Passing
	// exclude_agents too lets Rego implement fallback logic that reasons
	// about exclusions themselves (e.g. preferring an excluded agent's
	// region peer), instead of only seeing the post-filter result.
	if len(excludeAgents) > 0 {
		opaInput["exclude_agents"] = excludeAgents
	}
	if selectedAgent != "" {
		opaInput["agent"] = selectedAgent
	}
	if constraints := constraintCtx.GetConstraintsMap(); constraints != nil {
		opaInput["constraints"] = constraints
	}
	if agentConstraints := constraintCtx.GetAgentConstraintsMap(); agentConstraints != nil {
		opaInput["agent_constraints"] = agentConstraints
	}

	evalResult, err := s.engine.EvaluatePolicy(ctx, policy.ID, opaInput)
	if err != nil {
		return nil, "", NewInternalError(
			fmt.Sprintf("Failed to evaluate policy '%s'", policy.ID),
			err.Error(),
			err,
		)
	}

	if !evalResult.Defined {
		log.Debug("Policy returned undefined result, skipping", "policy_id", policy.ID)
		return currentSpec, selectedAgent, nil
	}

	decision := opa.ParsePolicyDecision(evalResult.Result)

	if decision.Rejected {
		log.Info("Policy rejected request", "policy_id", policy.ID, "reason", decision.RejectionReason)
		return nil, "", NewPolicyRejectedError(policy.ID, decision.RejectionReason)
	}

	if decision.Constraints != nil {
		if err := constraintCtx.MergeConstraints(decision.Constraints, policy.ID); err != nil {
			var conflictErr *ConstraintConflictError
			if errors.As(err, &conflictErr) {
				return nil, "", NewConstraintConflictError(
					policy.ID, conflictErr.FieldPath, conflictErr.SetByPolicy, conflictErr.Reason,
				)
			}
			return nil, "", NewConstraintConflictError(policy.ID, "", "", err.Error())
		}
	}

	if decision.AgentConstraints != nil {
		ac := &AccumulatedAgentConstraints{
			AllowList:              decision.AgentConstraints.AllowList,
			Patterns:               decision.AgentConstraints.Patterns,
			EnvironmentConstraints: decision.AgentConstraints.EnvironmentConstraints,
		}
		if err := constraintCtx.MergeAgentConstraints(ac, policy.ID); err != nil {
			return nil, "", NewAgentConstraintError(policy.ID, err.Error())
		}
	}

	if decision.Patch != nil {
		violations := constraintCtx.ValidatePatch(decision.Patch)
		if len(violations) > 0 {
			return nil, "", NewConstraintViolationError(policy.ID, violations)
		}

		currentSpec, err = mergePatch(currentSpec, decision.Patch)
		if err != nil {
			return nil, "", NewInternalError("Failed to merge patch into current spec", err.Error(), err)
		}
		log.Debug("Policy patch applied", "policy_id", policy.ID)
	}

	if decision.SelectedAgent != "" {
		if err := constraintCtx.ValidateAgent(decision.SelectedAgent, agentNames(availableAgents)); err != nil {
			return nil, "", NewAgentConstraintError(policy.ID, err.Error())
		}
		// A lookup miss here only happens when availableAgents was empty
		// (ValidateAgent didn't enforce membership either), so there's
		// nothing to validate the environment against.
		if env, ok := agentEnvironment(availableAgents, decision.SelectedAgent); ok {
			if err := constraintCtx.ValidateAgentEnvironment(env); err != nil {
				return nil, "", NewAgentConstraintError(policy.ID, err.Error())
			}
		}
		log.Debug("Policy selected agent", "policy_id", policy.ID, "agent", decision.SelectedAgent)
		selectedAgent = decision.SelectedAgent
	}

	return currentSpec, selectedAgent, nil
}

func mergePatch(base, patch map[string]any) (map[string]any, error) {
	result, err := deep.Copy(base)
	if err != nil {
		return nil, err
	}

	for key, patchValue := range patch {
		if patchValue == nil {
			delete(result, key)
			continue
		}

		patchMap, patchIsMap := patchValue.(map[string]any)
		baseValue, baseExists := result[key]
		baseMap, baseIsMap := baseValue.(map[string]any)

		if patchIsMap && baseExists && baseIsMap {
			result[key], err = mergePatch(baseMap, patchMap)
			if err != nil {
				return nil, err
			}
		} else {
			result[key] = patchValue
		}
	}

	return result, nil
}

// resourceServiceType extracts "service_type" from the resource spec. ok is
// false when absent (skip capability filtering). A present-but-malformed
// value (non-string/empty) is a validation error rather than a skip, matching
// internal/sp's CreateInstance check on the same field.
func resourceServiceType(serviceInstance map[string]any) (serviceType string, ok bool, err error) {
	v, present := serviceInstance["service_type"]
	if !present {
		return "", false, nil
	}
	s, isString := v.(string)
	if !isString {
		return "", false, fmt.Errorf("spec.service_type must be a string, got %T", v)
	}
	if strings.TrimSpace(s) == "" {
		return "", false, errors.New("spec.service_type must not be empty")
	}
	return s, true, nil
}

// filterByServiceType keeps only agents whose ServiceTypes includes
// serviceType. An agent with no ServiceTypes never matches - registration
// requires a non-empty list, so empty means "not a wildcard".
func filterByServiceType(available []AgentInfo, serviceType string) []AgentInfo {
	var filtered []AgentInfo
	for _, a := range available {
		if slices.Contains(a.ServiceTypes, serviceType) {
			filtered = append(filtered, a)
		}
	}
	return filtered
}

func filterExcluded(available []AgentInfo, excluded []string) []AgentInfo {
	if len(excluded) == 0 {
		return available
	}
	excludeSet := make(map[string]bool, len(excluded))
	for _, e := range excluded {
		excludeSet[e] = true
	}
	var filtered []AgentInfo
	for _, a := range available {
		if !excludeSet[a.Name] {
			filtered = append(filtered, a)
		}
	}
	return filtered
}

// agentNames extracts just the Name field, for callers (ValidateAgent) that
// only need membership-by-name and shouldn't otherwise depend on AgentInfo.
func agentNames(agents []AgentInfo) []string {
	if len(agents) == 0 {
		return nil
	}
	names := make([]string, len(agents))
	for i, a := range agents {
		names[i] = a.Name
	}
	return names
}

// agentEnvironment looks up the Environment of the AgentInfo with the given
// name. ok is false if no match is found (including when agents is empty).
func agentEnvironment(agents []AgentInfo, name string) (env string, ok bool) {
	for _, a := range agents {
		if a.Name == name {
			return a.Environment, true
		}
	}
	return "", false
}

func boolPtr(b bool) *bool {
	return &b
}
