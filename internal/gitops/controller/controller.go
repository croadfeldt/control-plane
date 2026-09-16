// Package controller implements the GitOps reconciliation loop.
package controller

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/dcm-project/control-plane/internal/gitops/store"
	gitopsmodel "github.com/dcm-project/control-plane/internal/gitops/store/model"
)

// Controller manages per-GitRepository reconciliation goroutines.
type Controller struct {
	reconciler   *Reconciler
	store        store.Store
	pollInterval time.Duration // how often to reload repos from DB
	stopCh       chan struct{}
	wg           sync.WaitGroup

	// repoLocks prevents concurrent reconciliations of the same repository.
	repoLocks   map[string]*sync.Mutex
	repoLocksMu sync.Mutex
}

// NewController creates a new Controller.
// pollInterval controls how often the controller reloads the list of GitRepositories from the DB.
// It panics if pollInterval is not positive, since time.NewTicker requires a positive duration.
func NewController(reconciler *Reconciler, gitopsStore store.Store, pollInterval time.Duration) *Controller {
	if pollInterval <= 0 {
		panic(fmt.Sprintf("gitops controller: pollInterval must be positive, got %s", pollInterval))
	}
	return &Controller{
		reconciler:   reconciler,
		store:        gitopsStore,
		pollInterval: pollInterval,
		stopCh:       make(chan struct{}),
		repoLocks:    make(map[string]*sync.Mutex),
	}
}

// Start begins the main controller loop.
func (c *Controller) Start(ctx context.Context) {
	c.wg.Add(1)
	go c.run(ctx)
}

// Stop gracefully stops the controller and waits for all goroutines to finish.
func (c *Controller) Stop() {
	close(c.stopCh)
	c.wg.Wait()
}

func (c *Controller) run(ctx context.Context) {
	defer c.wg.Done()

	// Run immediately on start, then on ticker
	c.reconcileAll(ctx)

	ticker := time.NewTicker(c.pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-c.stopCh:
			return
		case <-ticker.C:
			c.reconcileAll(ctx)
		}
	}
}

func (c *Controller) reconcileAll(ctx context.Context) {
	repos, err := c.store.GitRepository().ListAll(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to list git repositories", "error", err)
		return
	}

	for _, repo := range repos {
		select {
		case <-ctx.Done():
			return
		case <-c.stopCh:
			return
		default:
		}

		// Check if it's time to reconcile based on interval
		if repo.LastSyncTime != nil {
			nextSync := repo.LastSyncTime.Add(time.Duration(repo.IntervalSeconds) * time.Second)
			if time.Now().Before(nextSync) {
				continue
			}
		}

		c.reconcileOne(ctx, repo)
	}
}

func (c *Controller) reconcileOne(ctx context.Context, repo gitopsmodel.GitRepository) {
	mu := c.repoLock(repo.ID)
	if !mu.TryLock() {
		slog.DebugContext(ctx, "Skipping repo, reconciliation already in progress", "repo_id", repo.ID)
		return
	}
	defer mu.Unlock()

	if err := c.reconciler.Reconcile(ctx, repo); err != nil {
		slog.ErrorContext(ctx, "Reconciliation failed", "repo_id", repo.ID, "error", err)
	}
}

// repoLock returns a per-repository mutex, creating one if it doesn't exist.
func (c *Controller) repoLock(repoID string) *sync.Mutex {
	c.repoLocksMu.Lock()
	defer c.repoLocksMu.Unlock()

	mu, ok := c.repoLocks[repoID]
	if !ok {
		mu = &sync.Mutex{}
		c.repoLocks[repoID] = mu
	}
	return mu
}
