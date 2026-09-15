package healthcheck_test

import (
	"context"
	"time"

	"github.com/dcm-project/control-plane/internal/agent/healthcheck"
	"github.com/dcm-project/control-plane/internal/agent/store/model"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	agentstore "github.com/dcm-project/control-plane/internal/agent/store/agent"
)

var _ = Describe("Health Monitor", func() {
	var (
		db      *gorm.DB
		store   agentstore.Agent
		monitor *healthcheck.Monitor
		ctx     context.Context
	)

	BeforeEach(func() {
		var err error
		db, err = gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
			Logger: logger.Default.LogMode(logger.Silent),
		})
		Expect(err).NotTo(HaveOccurred())
		// sqlite's ":memory:" DSN gives each new physical connection its own
		// empty database, so once the monitor's background sweep goroutine
		// and the test's own Eventually/Consistently assertions query
		// concurrently, a second pooled connection would see "no such
		// table" instead of the migrated schema.
		sqlDB, err := db.DB()
		Expect(err).NotTo(HaveOccurred())
		sqlDB.SetMaxOpenConns(1)
		Expect(db.AutoMigrate(&model.Agent{})).To(Succeed())

		store = agentstore.NewAgent(db)
		monitor = healthcheck.NewMonitor(store, 30*time.Second, 5*time.Second)
		ctx = context.Background()
	})

	AfterEach(func() {
		sqlDB, _ := db.DB()
		_ = sqlDB.Close()
	})

	It("marks agent Unavailable when heartbeat expires", func() {
		expiredHB := time.Now().Add(-1 * time.Minute)
		a := model.Agent{
			ID:            uuid.New().String(),
			Name:          "expired-agent",
			TopicName:     "dcm.agent.expired-agent",
			HealthStatus:  model.AgentHealthStatusReady,
			LastHeartbeat: &expiredHB,
		}
		_, err := store.Create(ctx, a)
		Expect(err).NotTo(HaveOccurred())

		monitor.Start(ctx)
		defer monitor.Stop()

		Eventually(func() model.AgentHealthStatus {
			updated, err := store.Get(ctx, a.ID)
			Expect(err).NotTo(HaveOccurred())
			return updated.HealthStatus
		}).WithTimeout(2 * time.Second).WithPolling(20 * time.Millisecond).Should(Equal(model.AgentHealthStatusUnavailable))
	})

	It("uses created_at as grace period for new agents without heartbeat", func() {
		a := model.Agent{
			ID:           uuid.New().String(),
			Name:         "new-agent",
			TopicName:    "dcm.agent.new-agent",
			HealthStatus: model.AgentHealthStatusReady,
		}
		_, err := store.Create(ctx, a)
		Expect(err).NotTo(HaveOccurred())

		monitor.Start(ctx)
		defer monitor.Stop()

		Consistently(func() model.AgentHealthStatus {
			updated, err := store.Get(ctx, a.ID)
			Expect(err).NotTo(HaveOccurred())
			return updated.HealthStatus
		}, 200*time.Millisecond, 20*time.Millisecond).Should(Equal(model.AgentHealthStatusReady))
	})

	It("does not mark already-unavailable agents", func() {
		expiredHB := time.Now().Add(-1 * time.Minute)
		a := model.Agent{
			ID:            uuid.New().String(),
			Name:          "already-unavailable",
			TopicName:     "dcm.agent.already-unavailable",
			HealthStatus:  model.AgentHealthStatusUnavailable,
			LastHeartbeat: &expiredHB,
		}
		_, err := store.Create(ctx, a)
		Expect(err).NotTo(HaveOccurred())

		monitor.Start(ctx)
		defer monitor.Stop()

		Consistently(func() model.AgentHealthStatus {
			updated, err := store.Get(ctx, a.ID)
			Expect(err).NotTo(HaveOccurred())
			return updated.HealthStatus
		}, 200*time.Millisecond, 20*time.Millisecond).Should(Equal(model.AgentHealthStatusUnavailable))
	})

	It("re-registration clears Unavailable", func() {
		expiredHB := time.Now().Add(-1 * time.Minute)
		a := model.Agent{
			ID:            uuid.New().String(),
			Name:          "re-reg-agent",
			TopicName:     "dcm.agent.re-reg-agent",
			HealthStatus:  model.AgentHealthStatusUnavailable,
			LastHeartbeat: &expiredHB,
		}
		_, err := store.Create(ctx, a)
		Expect(err).NotTo(HaveOccurred())

		// Update's health_status/last_heartbeat write is CAS-guarded by the
		// same monotonicity rule as UpdateHeartbeatIfNewer, so - matching
		// how the real caller (RegisterOrUpdate) always supplies a fresh
		// time.Now() - the re-registration must carry a newer timestamp
		// than what's already stored for it to actually clear Unavailable.
		freshHB := time.Now()
		a.HealthStatus = model.AgentHealthStatusReady
		a.LastHeartbeat = &freshHB
		_, err = store.Update(ctx, a)
		Expect(err).NotTo(HaveOccurred())

		updated, err := store.Get(ctx, a.ID)
		Expect(err).NotTo(HaveOccurred())
		Expect(updated.HealthStatus).To(Equal(model.AgentHealthStatusReady))
	})
})
