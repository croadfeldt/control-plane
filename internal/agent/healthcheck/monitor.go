// Package healthcheck monitors agent heartbeats and marks agents unavailable.
package healthcheck

import (
	"context"
	"log/slog"
	"sync"
	"time"

	agentstore "github.com/dcm-project/control-plane/internal/agent/store/agent"
)

type Monitor struct {
	store            agentstore.Agent
	heartbeatTimeout time.Duration
	interval         time.Duration
	stopCh           chan struct{}
	stopOnce         sync.Once
	wg               sync.WaitGroup
}

func NewMonitor(store agentstore.Agent, heartbeatTimeout, interval time.Duration) *Monitor {
	return &Monitor{
		store:            store,
		heartbeatTimeout: heartbeatTimeout,
		interval:         interval,
		stopCh:           make(chan struct{}),
	}
}

func (m *Monitor) Start(ctx context.Context) {
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		m.sweep(ctx)

		ticker := time.NewTicker(m.interval)
		defer ticker.Stop()
		for {
			select {
			case <-m.stopCh:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				m.sweep(ctx)
			}
		}
	}()
}

func (m *Monitor) Stop() {
	m.stopOnce.Do(func() { close(m.stopCh) })
	m.wg.Wait()
}

// sweep flips stale agents to Unavailable via a single atomic conditional
// UPDATE (MarkStaleUnavailable) instead of listing agents and then writing
// per-agent based on that earlier snapshot: a read-then-write here would
// leave a window in which a heartbeat landing between the read and the
// write gets clobbered back to Unavailable by this sweep.
func (m *Monitor) sweep(ctx context.Context) {
	cutoff := time.Now().Add(-m.heartbeatTimeout)
	if err := m.store.MarkStaleUnavailable(ctx, cutoff); err != nil {
		slog.Error("health monitor: failed to mark stale agents unavailable", "error", err)
	}
}
