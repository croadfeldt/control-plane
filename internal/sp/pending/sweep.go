// Package pending sweeps timed-out pending and queued service instances.
package pending

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	agentstore "github.com/dcm-project/control-plane/internal/agent/store/agent"
	agentmodel "github.com/dcm-project/control-plane/internal/agent/store/model"
	"github.com/dcm-project/control-plane/internal/sp/messaging"
	"github.com/dcm-project/control-plane/internal/sp/store/model"
	"gorm.io/gorm"
)

var errAgentNotReady = errors.New("agent not ready")

// Reevaluator re-routes an existing resource to a different agent, excluding
// the given agent names, and re-triggers provisioning. Implemented by
// placement's PlacementService; this interface keeps the sp/pending package
// free of a compile-time dependency on the placement domain.
type Reevaluator interface {
	ReEvaluateWithExclude(ctx context.Context, resourceID string, excludeAgents []string) error
}

type Sweep struct {
	db             *gorm.DB
	publisher      *messaging.Publisher
	agentStore     agentstore.Agent
	reevaluator    Reevaluator
	pendingTimeout time.Duration
	queuedTimeout  time.Duration
	maxRetries     int
	interval       time.Duration
	stopCh         chan struct{}
	stopOnce       sync.Once
	wg             sync.WaitGroup
}

func NewSweep(db *gorm.DB, publisher *messaging.Publisher, agentSt agentstore.Agent, reevaluator Reevaluator, pendingTimeout, queuedTimeout, interval time.Duration, maxRetries int) *Sweep {
	return &Sweep{
		db:             db,
		publisher:      publisher,
		agentStore:     agentSt,
		reevaluator:    reevaluator,
		pendingTimeout: pendingTimeout,
		queuedTimeout:  queuedTimeout,
		maxRetries:     maxRetries,
		interval:       interval,
		stopCh:         make(chan struct{}),
	}
}

func (s *Sweep) Start(ctx context.Context) {
	ctx, cancel := context.WithCancel(ctx)
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer cancel()
		s.sweep(ctx)
		ticker := time.NewTicker(s.interval)
		defer ticker.Stop()
		for {
			select {
			case <-s.stopCh:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.sweep(ctx)
			}
		}
	}()
}

func (s *Sweep) Stop() {
	s.stopOnce.Do(func() { close(s.stopCh) })
	s.wg.Wait()
}

func (s *Sweep) sweep(ctx context.Context) {
	s.sweepPending(ctx)
	s.sweepQueued(ctx)
}

func (s *Sweep) sweepPending(ctx context.Context) {
	cutoff := time.Now().Add(-s.pendingTimeout)
	var instances []model.ServiceTypeInstance
	// deletion_status filter mirrors sweepQueued: a deferred DeleteInstance
	// never touches Status, so a "pending" instance can have a delete
	// already SCHEDULED against it and must not be picked up for self-healing.
	err := s.db.WithContext(ctx).Where("status = ? AND pending_started_at < ? AND agent_name IS NOT NULL AND (deletion_status IS NULL OR deletion_status = '')", model.StatusPending, cutoff).
		Find(&instances).Error
	if err != nil {
		slog.Error("sweep: failed to query pending instances", "error", err)
		return
	}

	for i := range instances {
		s.retryPendingInstance(ctx, &instances[i], cutoff)
	}
}

// retryPendingInstance implements the self-healing loop for a timed-out
// pending instance: instead of blindly re-publishing to the same agent that
// already failed to pick it up, it asks the placement layer to re-evaluate
// policy excluding that agent and re-provision against a different one.
// Only when no alternate agent is available (either because retries are
// exhausted or re-evaluation itself fails) does the instance get marked
// "failed".
func (s *Sweep) retryPendingInstance(ctx context.Context, inst *model.ServiceTypeInstance, cutoff time.Time) {
	log := slog.With("instance_id", inst.ID)

	if s.reevaluator == nil {
		log.Warn("sweep: no reevaluator configured, cannot self-heal pending instance")
		return
	}

	if inst.RetryCount >= s.maxRetries {
		// Still claim (same CAS as the non-exhausted path below) before the
		// final attempt: without it, two control-plane replicas could both
		// read this instance past maxRetries and both call selfHeal
		// concurrently for the "final attempt", double-provisioning it.
		claimed, err := s.claimRetry(ctx, inst.ID, cutoff)
		if err != nil {
			log.Error("sweep: failed to claim final retry attempt", "error", err)
			return
		}
		if !claimed {
			log.Debug("sweep: final retry attempt already claimed by another sweep")
			return
		}
		if err := s.selfHeal(ctx, inst); err == nil {
			log.Info("sweep: found alternate agent on final attempt, instance resumed")
			return
		}
		applied, err := s.markFailedFrom(ctx, inst.ID, model.StatusPending)
		if err != nil {
			log.Error("sweep: failed to mark instance as failed", "error", err)
			return
		}
		if !applied {
			log.Debug("sweep: instance already moved on before it could be marked failed")
			return
		}
		log.Info("sweep: pending instance retries exhausted, no viable agent found")
		return
	}

	claimed, err := s.claimRetry(ctx, inst.ID, cutoff)
	if err != nil {
		log.Error("sweep: failed to claim retry", "error", err)
		return
	}
	if !claimed {
		log.Debug("sweep: instance already claimed by another sweep")
		return
	}

	if err := s.selfHeal(ctx, inst); err != nil {
		log.Warn("sweep: re-evaluation to a new agent failed, will retry next cycle", "error", err)
		return
	}
	log.Info("sweep: pending instance re-routed to a new agent")
}

// selfHeal asks the placement layer to pick a new agent (excluding the
// instance's current agent, if any) and re-provision against it.
func (s *Sweep) selfHeal(ctx context.Context, inst *model.ServiceTypeInstance) error {
	var excludeAgents []string
	if inst.AgentName != nil {
		excludeAgents = []string{*inst.AgentName}
	}
	return s.reevaluator.ReEvaluateWithExclude(ctx, inst.ID, excludeAgents)
}

// markFailedFrom CAS-guards the terminal "failed" transition on fromStatus,
// so a concurrent change elsewhere can't be silently clobbered. Returns
// applied=false (no error) if the instance had already moved off fromStatus.
func (s *Sweep) markFailedFrom(ctx context.Context, id string, fromStatus string) (bool, error) {
	result := s.db.WithContext(ctx).Model(&model.ServiceTypeInstance{}).
		Where("id = ? AND status = ?", id, fromStatus).
		Updates(map[string]any{"status": model.StatusFailed, "status_message": "retries exhausted"})
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}

// markPendingFrom CAS-guards transitioning an instance back to "pending"
// (with a fresh pending_started_at) from fromStatus, so a failed self-heal
// attempt out of "cancelled" doesn't strand the instance in a status
// neither sweepPending nor sweepQueued ever revisits.
func (s *Sweep) markPendingFrom(ctx context.Context, id string, fromStatus string) (bool, error) {
	result := s.db.WithContext(ctx).Model(&model.ServiceTypeInstance{}).
		Where("id = ? AND status = ?", id, fromStatus).
		Updates(map[string]any{"status": model.StatusPending, "pending_started_at": time.Now()})
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}

// claimRetry atomically increments retry_count and resets the pending timer.
// The cutoff check (matching sweepPending's own cutoff) closes a race where
// two sweep replicas read the same stale instance: the second claim's WHERE
// clause fails against the fresh pending_started_at the first just wrote.
func (s *Sweep) claimRetry(ctx context.Context, id string, cutoff time.Time) (bool, error) {
	result := s.db.WithContext(ctx).Model(&model.ServiceTypeInstance{}).
		Where("id = ? AND status = ? AND pending_started_at < ?", id, model.StatusPending, cutoff).
		Updates(map[string]any{
			"retry_count":        gorm.Expr("retry_count + 1"),
			"pending_started_at": time.Now(),
		})
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}

func (s *Sweep) sweepQueued(ctx context.Context) {
	cutoff := time.Now().Add(-s.queuedTimeout)
	var instances []model.ServiceTypeInstance
	err := s.db.WithContext(ctx).Where("status = ? AND pending_started_at < ? AND agent_name IS NOT NULL AND (deletion_status IS NULL OR deletion_status = '')", model.StatusQueued, cutoff).
		Find(&instances).Error
	if err != nil {
		slog.Error("sweep: failed to query queued instances", "error", err)
		return
	}

	for i := range instances {
		s.cancelQueuedInstance(ctx, &instances[i], cutoff)
	}
}

// cancelQueuedInstance handles a queued instance whose agent never
// acknowledged the create request in time: claims it via CAS, best-effort
// notifies the old agent to stand down, then self-heals to a different
// agent (or gives up once the shared retry budget is exhausted).
// "cancelled" is a transient bookkeeping state on the way to a fresh
// pending assignment, not a terminal outcome.
func (s *Sweep) cancelQueuedInstance(ctx context.Context, inst *model.ServiceTypeInstance, cutoff time.Time) {
	log := slog.With("instance_id", inst.ID)

	claimed, err := s.claimCancel(ctx, inst.ID, cutoff)
	if err != nil {
		log.Error("sweep: failed to cancel queued instance", "error", err)
		return
	}
	if !claimed {
		log.Debug("sweep: queued instance already moved by response consumer or another sweep")
		return
	}
	agentName := ""
	if inst.AgentName != nil {
		agentName = *inst.AgentName
	}
	log.Info("sweep: queued instance timed out waiting for agent acknowledgement, cancelling",
		"status", model.StatusCancelled, "agent_name", agentName, "retry_count", inst.RetryCount+1)

	s.notifyAgentOfCancel(ctx, inst)

	if s.reevaluator == nil {
		return
	}

	if inst.RetryCount+1 >= s.maxRetries {
		if err := s.selfHeal(ctx, inst); err == nil {
			log.Info("sweep: found alternate agent on final attempt, queued instance resumed")
			return
		}
		applied, err := s.markFailedFrom(ctx, inst.ID, model.StatusCancelled)
		if err != nil {
			log.Error("sweep: failed to mark instance as failed", "error", err)
			return
		}
		if !applied {
			log.Debug("sweep: instance already moved on before it could be marked failed")
			return
		}
		log.Info("sweep: queued instance retries exhausted, no viable agent found")
		return
	}

	if err := s.selfHeal(ctx, inst); err != nil {
		// Fall back to "pending" instead of leaving the instance stranded
		// in "cancelled": neither sweepPending nor sweepQueued ever
		// revisits that status, so the next sweepPending cycle retries it.
		applied, markErr := s.markPendingFrom(ctx, inst.ID, model.StatusCancelled)
		if markErr != nil {
			log.Error("sweep: failed to fall back cancelled instance to pending for retry", "error", markErr)
			return
		}
		if !applied {
			log.Debug("sweep: instance already moved on before it could fall back to pending", "self_heal_error", err)
			return
		}
		log.Info("sweep: no alternate agent available for cancelled instance, reverted to pending for next retry", "error", err)
		return
	}
	log.Info("sweep: cancelled instance re-routed to a new agent")
}

// claimCancel CAS-transitions a timed-out queued instance to "cancelled" and
// claims a retry, guarded by the same cutoff sweepQueued used to select it -
// so a horizontally-scaled control plane can't double-claim it, and it
// can't race the response consumer's own status-CAS.
func (s *Sweep) claimCancel(ctx context.Context, id string, cutoff time.Time) (bool, error) {
	result := s.db.WithContext(ctx).Model(&model.ServiceTypeInstance{}).
		Where("id = ? AND status = ? AND pending_started_at < ?", id, model.StatusQueued, cutoff).
		Updates(map[string]any{
			"status":      model.StatusCancelled,
			"retry_count": gorm.Expr("retry_count + 1"),
		})
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}

// notifyAgentOfCancel best-effort tells the old agent to stand down. It's a
// courtesy, not a precondition for the self-heal that follows, and always
// runs after claimCancel has already won: notifying before the claim would
// let every replica racing for it send a duplicate cancel to the agent.
func (s *Sweep) notifyAgentOfCancel(ctx context.Context, inst *model.ServiceTypeInstance) {
	if s.publisher == nil || s.agentStore == nil || inst.AgentName == nil {
		return
	}
	log := slog.With("instance_id", inst.ID)
	subject, err := s.resolveSubjectWithError(ctx, *inst.AgentName)
	switch {
	case err == nil:
		if pubErr := s.publisher.PublishCancel(ctx, subject+".cancel", messaging.CancelPayload{
			ResourceID:  inst.ID,
			ServiceType: inst.ServiceType,
		}); pubErr != nil {
			log.Warn("sweep: cancel publish failed, proceeding to self-heal anyway", "error", pubErr)
		}
	case errors.Is(err, agentstore.ErrAgentNotFound):
		log.Info("sweep: agent not found, cancelling locally", "agent_name", *inst.AgentName)
	case errors.Is(err, errAgentNotReady):
		log.Info("sweep: agent not ready, cancelling locally", "agent_name", *inst.AgentName)
	default:
		log.Warn("sweep: failed to resolve agent for cancel notification, proceeding to self-heal anyway", "agent_name", *inst.AgentName, "error", err)
	}
}

func (s *Sweep) resolveSubjectWithError(ctx context.Context, agentName string) (string, error) {
	agent, err := s.agentStore.GetByName(ctx, agentName)
	if err != nil {
		return "", err
	}
	if agent.HealthStatus != agentmodel.AgentHealthStatusReady {
		return "", errAgentNotReady
	}
	return agent.TopicName, nil
}
