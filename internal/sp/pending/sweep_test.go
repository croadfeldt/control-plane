package pending_test

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	agentstore "github.com/dcm-project/control-plane/internal/agent/store/agent"
	agentmodel "github.com/dcm-project/control-plane/internal/agent/store/model"
	"github.com/dcm-project/control-plane/internal/sp/messaging"
	"github.com/dcm-project/control-plane/internal/sp/pending"
	"github.com/dcm-project/control-plane/internal/sp/store/model"
	"github.com/google/uuid"
	"github.com/nats-io/nats.go/jetstream"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type failingJetStream struct {
	jetstream.JetStream
	publishErr error
}

func (m *failingJetStream) Publish(_ context.Context, _ string, _ []byte, _ ...jetstream.PublishOpt) (*jetstream.PubAck, error) {
	return nil, m.publishErr
}

// limitToSingleConn pins the pool to one physical connection. sqlite's
// ":memory:" DSN gives each new physical connection its own empty database,
// so once the sweep's background goroutine and the test's own Eventually/
// Consistently assertions query concurrently, a second pooled connection
// would see "no such table" instead of the migrated schema.
func limitToSingleConn(d *gorm.DB) {
	sqlDB, err := d.DB()
	Expect(err).NotTo(HaveOccurred())
	sqlDB.SetMaxOpenConns(1)
}

// syncBuffer wraps bytes.Buffer with a mutex: the sweep writes logs from
// its own background goroutine while a test concurrently polls the
// buffer's contents via Eventually, which a plain bytes.Buffer doesn't
// support safely.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// fakeReevaluator records ReEvaluateWithExclude invocations so tests can
// assert the self-healing loop actually calls into placement with the right
// arguments, without depending on the real policy/SPRM stack.
type fakeReevaluator struct {
	mu    sync.Mutex
	calls []reevalCall
	err   error
}

type reevalCall struct {
	resourceID    string
	excludeAgents []string
}

func (f *fakeReevaluator) ReEvaluateWithExclude(_ context.Context, resourceID string, excludeAgents []string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, reevalCall{resourceID: resourceID, excludeAgents: excludeAgents})
	return f.err
}

func (f *fakeReevaluator) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

func (f *fakeReevaluator) lastCall() reevalCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[len(f.calls)-1]
}

var _ = Describe("Pending Sweep", func() {
	var (
		db    *gorm.DB
		sweep *pending.Sweep
		ctx   context.Context
	)

	AfterEach(func() {
		sweep.Stop()
		sqlDB, _ := db.DB()
		_ = sqlDB.Close()
	})

	newDB := func() *gorm.DB {
		d, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
			Logger: logger.Default.LogMode(logger.Silent),
		})
		Expect(err).NotTo(HaveOccurred())
		limitToSingleConn(d)
		Expect(d.AutoMigrate(&agentmodel.Agent{}, &model.ServiceTypeInstance{})).To(Succeed())
		Expect(d.Create(&agentmodel.Agent{
			ID: uuid.New().String(), Name: "test-agent",
			TopicName: "dcm.agent.test-agent", HealthStatus: agentmodel.AgentHealthStatusReady,
		}).Error).NotTo(HaveOccurred())
		return d
	}

	pendingInstance := func(retryCount int) model.ServiceTypeInstance {
		pastTime := time.Now().Add(-1 * time.Minute)
		agentName := "test-agent"
		return model.ServiceTypeInstance{
			ID:          uuid.New().String(),
			ServiceType: "vm", Status: "pending", InstanceName: "sweep-test",
			Spec: map[string]any{"cpu": 2}, AgentName: &agentName,
			PendingStartedAt: &pastTime, RetryCount: retryCount,
		}
	}

	It("does not consume retries when reevaluator is nil", func() {
		db = newDB()
		sweep = pending.NewSweep(db, nil, nil, nil, 30*time.Second, 60*time.Second, 5*time.Millisecond, 3)
		ctx = context.Background()

		inst := pendingInstance(0)
		Expect(db.Create(&inst).Error).NotTo(HaveOccurred())

		sweep.Start(ctx)

		Consistently(func() int {
			var updated model.ServiceTypeInstance
			Expect(db.First(&updated, "id = ?", inst.ID).Error).NotTo(HaveOccurred())
			return updated.RetryCount
		}, 200*time.Millisecond, 20*time.Millisecond).Should(Equal(0))

		var updated model.ServiceTypeInstance
		Expect(db.First(&updated, "id = ?", inst.ID).Error).NotTo(HaveOccurred())
		Expect(updated.Status).To(Equal("pending"))
	})

	It("re-routes to a new agent via reevaluator when retries remain", func() {
		db = newDB()
		reeval := &fakeReevaluator{}
		sweep = pending.NewSweep(db, nil, nil, reeval, 30*time.Second, 60*time.Second, 5*time.Millisecond, 3)
		ctx = context.Background()

		inst := pendingInstance(0)
		Expect(db.Create(&inst).Error).NotTo(HaveOccurred())

		sweep.Start(ctx)

		Eventually(reeval.callCount, time.Second, 10*time.Millisecond).Should(BeNumerically(">=", 1))
		call := reeval.lastCall()
		Expect(call.resourceID).To(Equal(inst.ID))
		Expect(call.excludeAgents).To(ConsistOf("test-agent"))

		var updated model.ServiceTypeInstance
		Expect(db.First(&updated, "id = ?", inst.ID).Error).NotTo(HaveOccurred())
		Expect(updated.Status).To(Equal("pending"))
		Expect(updated.RetryCount).To(BeNumerically(">=", 1))
	})

	It("keeps instance pending and retries again when reevaluation fails", func() {
		db = newDB()
		reeval := &fakeReevaluator{err: fmt.Errorf("no agent available")}
		sweep = pending.NewSweep(db, nil, nil, reeval, 30*time.Second, 60*time.Second, 5*time.Millisecond, 3)
		ctx = context.Background()

		inst := pendingInstance(0)
		Expect(db.Create(&inst).Error).NotTo(HaveOccurred())

		sweep.Start(ctx)

		Eventually(reeval.callCount, time.Second, 10*time.Millisecond).Should(BeNumerically(">=", 1))

		var updated model.ServiceTypeInstance
		Expect(db.First(&updated, "id = ?", inst.ID).Error).NotTo(HaveOccurred())
		Expect(updated.Status).To(Equal("pending"))
	})

	It("marks as failed when retries exhausted and no alternate agent exists", func() {
		db = newDB()
		reeval := &fakeReevaluator{err: fmt.Errorf("no agent available")}
		sweep = pending.NewSweep(db, nil, nil, reeval, 30*time.Second, 60*time.Second, 5*time.Millisecond, 3)
		ctx = context.Background()

		inst := pendingInstance(3)
		Expect(db.Create(&inst).Error).NotTo(HaveOccurred())

		sweep.Start(ctx)

		Eventually(func() string {
			var updated model.ServiceTypeInstance
			Expect(db.First(&updated, "id = ?", inst.ID).Error).NotTo(HaveOccurred())
			return updated.Status
		}, time.Second, 10*time.Millisecond).Should(Equal("failed"))

		var updated model.ServiceTypeInstance
		Expect(db.First(&updated, "id = ?", inst.ID).Error).NotTo(HaveOccurred())
		Expect(updated.StatusMessage).To(Equal("retries exhausted"))
	})

	It("resumes on the final attempt when reevaluation finds an alternate agent", func() {
		db = newDB()
		reeval := &fakeReevaluator{}
		sweep = pending.NewSweep(db, nil, nil, reeval, 30*time.Second, 60*time.Second, 5*time.Millisecond, 3)
		ctx = context.Background()

		inst := pendingInstance(3)
		Expect(db.Create(&inst).Error).NotTo(HaveOccurred())

		sweep.Start(ctx)

		Eventually(reeval.callCount, time.Second, 10*time.Millisecond).Should(BeNumerically(">=", 1))

		Consistently(func() string {
			var updated model.ServiceTypeInstance
			Expect(db.First(&updated, "id = ?", inst.ID).Error).NotTo(HaveOccurred())
			return updated.Status
		}, 200*time.Millisecond, 20*time.Millisecond).ShouldNot(Equal("failed"))
	})

	It("never self-heals a pending instance that already has a delete scheduled against it (R2 S2: finding #1)", func() {
		// A deferred DeleteInstance never touches Status, so a timed-out
		// "pending" instance can have a delete already SCHEDULED.
		// sweepPending must exclude it, like sweepQueued already does.
		db = newDB()
		reeval := &fakeReevaluator{}
		sweep = pending.NewSweep(db, nil, nil, reeval, 30*time.Second, 60*time.Second, 5*time.Millisecond, 3)
		ctx = context.Background()

		inst := pendingInstance(0)
		deletionStatus := "SCHEDULED"
		inst.DeletionStatus = &deletionStatus
		Expect(db.Create(&inst).Error).NotTo(HaveOccurred())

		sweep.Start(ctx)

		Consistently(func() int {
			return reeval.callCount()
		}, 300*time.Millisecond, 20*time.Millisecond).Should(Equal(0))

		var updated model.ServiceTypeInstance
		Expect(db.First(&updated, "id = ?", inst.ID).Error).NotTo(HaveOccurred())
		Expect(updated.Status).To(Equal("pending"))
		Expect(updated.AgentName).NotTo(BeNil())
		Expect(*updated.AgentName).To(Equal("test-agent"))
	})
})

var _ = Describe("Queued Sweep", func() {
	var (
		db    *gorm.DB
		sweep *pending.Sweep
		ctx   context.Context
	)

	BeforeEach(func() {
		var err error
		db, err = gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
			Logger: logger.Default.LogMode(logger.Silent),
		})
		Expect(err).NotTo(HaveOccurred())
		limitToSingleConn(db)
		Expect(db.AutoMigrate(&agentmodel.Agent{}, &model.ServiceTypeInstance{})).To(Succeed())
		Expect(db.Create(&agentmodel.Agent{ID: uuid.New().String(), Name: "test-agent", TopicName: "dcm.agent.test-agent"}).Error).NotTo(HaveOccurred())

		sweep = pending.NewSweep(db, nil, nil, nil, 30*time.Second, 60*time.Second, 5*time.Millisecond, 3)
		ctx = context.Background()
	})

	AfterEach(func() {
		sweep.Stop()
		sqlDB, _ := db.DB()
		_ = sqlDB.Close()
	})

	It("cancels request after queued timeout", func() {
		pastTime := time.Now().Add(-2 * time.Minute)
		agentName := "test-agent"
		instance := model.ServiceTypeInstance{
			ID:               uuid.New().String(),
			ServiceType:      "vm",
			Status:           "queued",
			InstanceName:     "queued-sweep",
			Spec:             map[string]any{"cpu": 2},
			AgentName:        &agentName,
			PendingStartedAt: &pastTime,
		}
		Expect(db.Create(&instance).Error).NotTo(HaveOccurred())

		sweep.Start(ctx)

		Eventually(func() string {
			var updated model.ServiceTypeInstance
			Expect(db.First(&updated, "id = ?", instance.ID).Error).NotTo(HaveOccurred())
			return updated.Status
		}, time.Second, 10*time.Millisecond).Should(Equal("cancelled"))
	})

	It("logs the transition when a queued instance times out and is cancelled", func() {
		pastTime := time.Now().Add(-2 * time.Minute)
		agentName := "test-agent"
		instance := model.ServiceTypeInstance{
			ID:               uuid.New().String(),
			ServiceType:      "vm",
			Status:           "queued",
			InstanceName:     "queued-sweep-log",
			Spec:             map[string]any{"cpu": 2},
			AgentName:        &agentName,
			PendingStartedAt: &pastTime,
		}
		Expect(db.Create(&instance).Error).NotTo(HaveOccurred())

		var buf syncBuffer
		prevLogger := slog.Default()
		slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
		defer slog.SetDefault(prevLogger)

		sweep.Start(ctx)

		Eventually(buf.String, time.Second, 10*time.Millisecond).Should(SatisfyAll(
			ContainSubstring("timed out waiting for agent acknowledgement"),
			ContainSubstring("instance_id="+instance.ID),
			ContainSubstring("status=cancelled"),
			ContainSubstring("agent_name="+agentName),
		))
	})

	It("skips deletion requests", func() {
		pastTime := time.Now().Add(-2 * time.Minute)
		agentName := "test-agent"
		deletionStatus := "pending_deletion"
		instance := model.ServiceTypeInstance{
			ID:               uuid.New().String(),
			ServiceType:      "vm",
			Status:           "queued",
			InstanceName:     "queued-delete",
			Spec:             map[string]any{"cpu": 2},
			AgentName:        &agentName,
			PendingStartedAt: &pastTime,
			DeletionStatus:   &deletionStatus,
		}
		Expect(db.Create(&instance).Error).NotTo(HaveOccurred())

		sweep.Start(ctx)

		Consistently(func() string {
			var updated model.ServiceTypeInstance
			Expect(db.First(&updated, "id = ?", instance.ID).Error).NotTo(HaveOccurred())
			return updated.Status
		}, 200*time.Millisecond, 20*time.Millisecond).Should(Equal("queued"))
	})
})

var _ = Describe("Queued Sweep Cancellation", func() {
	var (
		db    *gorm.DB
		sweep *pending.Sweep
		ctx   context.Context
	)

	AfterEach(func() {
		sweep.Stop()
		sqlDB, _ := db.DB()
		_ = sqlDB.Close()
	})

	queuedInstance := func(agentName string) model.ServiceTypeInstance {
		pastTime := time.Now().Add(-2 * time.Minute)
		return model.ServiceTypeInstance{
			ID:          uuid.New().String(),
			ServiceType: "vm", Status: "queued", InstanceName: "queued-cancel-test",
			Spec: map[string]any{"cpu": 2}, AgentName: &agentName,
			PendingStartedAt: &pastTime,
		}
	}

	It("still cancels (claims) a queued instance even when the best-effort agent-cancel publish fails", func() {
		// The CAS claim runs BEFORE the publish attempt (see
		// cancelQueuedInstance): a failure to notify the old agent is a
		// courtesy best-effort, not a precondition for committing to move
		// away from it, so the instance still transitions to "cancelled"
		// even though the publish below always fails.
		var err error
		db, err = gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
		Expect(err).NotTo(HaveOccurred())
		limitToSingleConn(db)
		Expect(db.AutoMigrate(&agentmodel.Agent{}, &model.ServiceTypeInstance{})).To(Succeed())
		Expect(db.Create(&agentmodel.Agent{
			ID: uuid.New().String(), Name: "test-agent",
			TopicName: "dcm.agent.test-agent", HealthStatus: agentmodel.AgentHealthStatusReady,
		}).Error).NotTo(HaveOccurred())

		js := &failingJetStream{publishErr: fmt.Errorf("nats: connection closed")}
		pub := messaging.NewPublisher(js)
		agentSt := agentstore.NewAgent(db)
		// reevaluator is nil, so once the CAS claims the instance the sweep
		// stops there (no self-heal attempted) - isolating this test to just
		// the claim-vs-publish ordering.
		sweep = pending.NewSweep(db, pub, agentSt, nil, 30*time.Second, 60*time.Second, 5*time.Millisecond, 3)
		ctx = context.Background()

		inst := queuedInstance("test-agent")
		Expect(db.Create(&inst).Error).NotTo(HaveOccurred())

		sweep.Start(ctx)

		Eventually(func() string {
			var updated model.ServiceTypeInstance
			Expect(db.First(&updated, "id = ?", inst.ID).Error).NotTo(HaveOccurred())
			return updated.Status
		}, time.Second, 10*time.Millisecond).Should(Equal("cancelled"))
	})

	It("cancels locally when agent is not found", func() {
		var err error
		db, err = gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
		Expect(err).NotTo(HaveOccurred())
		limitToSingleConn(db)
		Expect(db.AutoMigrate(&agentmodel.Agent{}, &model.ServiceTypeInstance{})).To(Succeed())

		js := &failingJetStream{publishErr: fmt.Errorf("should not be called")}
		pub := messaging.NewPublisher(js)
		agentSt := agentstore.NewAgent(db)
		sweep = pending.NewSweep(db, pub, agentSt, nil, 30*time.Second, 60*time.Second, 5*time.Millisecond, 3)
		ctx = context.Background()

		inst := queuedInstance("nonexistent-agent")
		Expect(db.Create(&inst).Error).NotTo(HaveOccurred())

		sweep.Start(ctx)

		Eventually(func() string {
			var updated model.ServiceTypeInstance
			Expect(db.First(&updated, "id = ?", inst.ID).Error).NotTo(HaveOccurred())
			return updated.Status
		}, time.Second, 10*time.Millisecond).Should(Equal("cancelled"))
	})

	It("cancels locally when agent is unavailable", func() {
		var err error
		db, err = gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
		Expect(err).NotTo(HaveOccurred())
		limitToSingleConn(db)
		Expect(db.AutoMigrate(&agentmodel.Agent{}, &model.ServiceTypeInstance{})).To(Succeed())
		Expect(db.Create(&agentmodel.Agent{
			ID: uuid.New().String(), Name: "test-agent",
			TopicName: "dcm.agent.test-agent", HealthStatus: agentmodel.AgentHealthStatusUnavailable,
		}).Error).NotTo(HaveOccurred())

		js := &failingJetStream{publishErr: fmt.Errorf("should not be called")}
		pub := messaging.NewPublisher(js)
		agentSt := agentstore.NewAgent(db)
		sweep = pending.NewSweep(db, pub, agentSt, nil, 30*time.Second, 60*time.Second, 5*time.Millisecond, 3)
		ctx = context.Background()

		inst := queuedInstance("test-agent")
		Expect(db.Create(&inst).Error).NotTo(HaveOccurred())

		sweep.Start(ctx)

		Eventually(func() string {
			var updated model.ServiceTypeInstance
			Expect(db.First(&updated, "id = ?", inst.ID).Error).NotTo(HaveOccurred())
			return updated.Status
		}, time.Second, 10*time.Millisecond).Should(Equal("cancelled"))
	})

	It("marks a queued instance failed once retries are exhausted instead of bouncing forever", func() {
		var err error
		db, err = gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
		Expect(err).NotTo(HaveOccurred())
		limitToSingleConn(db)
		Expect(db.AutoMigrate(&agentmodel.Agent{}, &model.ServiceTypeInstance{})).To(Succeed())

		reeval := &fakeReevaluator{err: fmt.Errorf("no agent available")}
		sweep = pending.NewSweep(db, nil, nil, reeval, 30*time.Second, 60*time.Second, 5*time.Millisecond, 3)
		ctx = context.Background()

		inst := queuedInstance("nonexistent-agent")
		inst.RetryCount = 3
		Expect(db.Create(&inst).Error).NotTo(HaveOccurred())

		sweep.Start(ctx)

		Eventually(func() string {
			var updated model.ServiceTypeInstance
			Expect(db.First(&updated, "id = ?", inst.ID).Error).NotTo(HaveOccurred())
			return updated.Status
		}, time.Second, 10*time.Millisecond).Should(Equal("failed"))
	})

	It("self-heals a cancelled instance when the reevaluator finds an alternate agent", func() {
		var err error
		db, err = gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
		Expect(err).NotTo(HaveOccurred())
		limitToSingleConn(db)
		Expect(db.AutoMigrate(&agentmodel.Agent{}, &model.ServiceTypeInstance{})).To(Succeed())

		reeval := &fakeReevaluator{}
		sweep = pending.NewSweep(db, nil, nil, reeval, 30*time.Second, 60*time.Second, 5*time.Millisecond, 3)
		ctx = context.Background()

		inst := queuedInstance("nonexistent-agent")
		Expect(db.Create(&inst).Error).NotTo(HaveOccurred())

		sweep.Start(ctx)

		Eventually(reeval.callCount, time.Second, 10*time.Millisecond).Should(BeNumerically(">=", 1))
		call := reeval.lastCall()
		Expect(call.resourceID).To(Equal(inst.ID))
		Expect(call.excludeAgents).To(ConsistOf("nonexistent-agent"))
	})

	It("falls back a cancelled instance to pending for a later retry instead of stranding it, when self-heal fails with retries left (R2 S3: finding #4)", func() {
		// Neither sweepPending (selects "pending") nor sweepQueued (selects
		// "queued") ever revisits a "cancelled" row, so if the immediate
		// self-heal attempt fails while retry budget remains, the instance
		// must fall back to "pending" (picked up again by sweepPending next
		// cycle) rather than staying "cancelled" forever.
		var err error
		db, err = gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
		Expect(err).NotTo(HaveOccurred())
		limitToSingleConn(db)
		Expect(db.AutoMigrate(&agentmodel.Agent{}, &model.ServiceTypeInstance{})).To(Succeed())

		reeval := &fakeReevaluator{err: fmt.Errorf("no agent available")}
		sweep = pending.NewSweep(db, nil, nil, reeval, 30*time.Second, 60*time.Second, 5*time.Millisecond, 3)
		ctx = context.Background()

		inst := queuedInstance("nonexistent-agent")
		Expect(db.Create(&inst).Error).NotTo(HaveOccurred())

		sweep.Start(ctx)

		Eventually(func() string {
			var updated model.ServiceTypeInstance
			Expect(db.First(&updated, "id = ?", inst.ID).Error).NotTo(HaveOccurred())
			return updated.Status
		}, time.Second, 10*time.Millisecond).Should(Equal("pending"))

		var updated model.ServiceTypeInstance
		Expect(db.First(&updated, "id = ?", inst.ID).Error).NotTo(HaveOccurred())
		Expect(updated.RetryCount).To(Equal(1))
		Expect(updated.PendingStartedAt).NotTo(BeNil())
	})
})
