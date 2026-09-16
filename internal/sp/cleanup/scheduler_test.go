package cleanup_test

import (
	"context"
	"time"

	agentstore "github.com/dcm-project/control-plane/internal/agent/store/agent"
	agentmodel "github.com/dcm-project/control-plane/internal/agent/store/model"
	"github.com/dcm-project/control-plane/internal/sp/cleanup"
	"github.com/dcm-project/control-plane/internal/sp/config"
	"github.com/dcm-project/control-plane/internal/sp/messaging"
	"github.com/dcm-project/control-plane/internal/sp/store"
	"github.com/dcm-project/control-plane/internal/sp/store/model"
	"github.com/dcm-project/control-plane/internal/sp/testutil"
	"github.com/google/uuid"
	"github.com/nats-io/nats.go/jetstream"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// stubJetStream acknowledges every publish so tests can exercise the
// publish-then-wait-for-ack path without a real NATS server.
type stubJetStream struct {
	jetstream.JetStream
	publishErr error
}

func (s *stubJetStream) Publish(_ context.Context, _ string, _ []byte, _ ...jetstream.PublishOpt) (*jetstream.PubAck, error) {
	if s.publishErr != nil {
		return nil, s.publishErr
	}
	return &jetstream.PubAck{}, nil
}

var _ = Describe("Scheduler", func() {
	var (
		db        *gorm.DB
		dataStore store.Store
		scheduler *cleanup.Scheduler
		ctx       context.Context
	)

	BeforeEach(func() {
		var err error
		db, err = gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
			Logger: logger.Default.LogMode(logger.Silent),
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(db.AutoMigrate(&agentmodel.Agent{}, &model.ServiceTypeInstance{})).To(Succeed())

		for _, name := range []string{"audit-agent", "unregistered-agent", "mismatch-agent"} {
			Expect(db.Create(&agentmodel.Agent{ID: uuid.New().String(), Name: name, TopicName: "dcm.agent." + name}).Error).NotTo(HaveOccurred())
		}

		dataStore = store.NewStore(db, store.WithServiceTypeInstanceRetry(testutil.FastServiceTypeInstanceRetry()...))

		cfg := &config.CleanupConfig{
			Interval:   1 * time.Minute,
			MaxRetries: 3,
		}
		scheduler = cleanup.NewScheduler(dataStore, nil, nil, cfg)
		ctx = context.Background()
	})

	AfterEach(func() {
		sqlDB, err := db.DB()
		Expect(err).NotTo(HaveOccurred())
		Expect(sqlDB.Close()).To(Succeed())
	})

	Describe("Agent-only cleanup (all instances are agent-routed)", func() {
		It("marks DELETED for agent-routed instance", func() {
			agentName := "audit-agent"
			inst := model.ServiceTypeInstance{
				ID:           uuid.New().String(),
				ServiceType:  "vm",
				Status:       "PROVISIONING",
				InstanceName: "audit-inst",
				Spec:         map[string]any{"cpu": 1},
				AgentName:    &agentName,
			}
			Expect(db.Create(&inst).Error).NotTo(HaveOccurred())
			Expect(dataStore.ServiceTypeInstance().MarkForDeletion(ctx, inst.ID)).To(Succeed())

			scheduler.ProcessPendingDeletions(ctx)

			found, err := dataStore.ServiceTypeInstance().Get(ctx, inst.ID, true)
			Expect(err).NotTo(HaveOccurred())
			Expect(*found.DeletionStatus).To(Equal("DELETED"))
		})

		It("marks DELETED when agent not registered", func() {
			agentName := "unregistered-agent"
			inst := model.ServiceTypeInstance{
				ID:           uuid.New().String(),
				ServiceType:  "vm",
				Status:       "PROVISIONING",
				InstanceName: "orphan-inst",
				Spec:         map[string]any{"cpu": 1},
				AgentName:    &agentName,
			}
			Expect(db.Create(&inst).Error).NotTo(HaveOccurred())
			Expect(dataStore.ServiceTypeInstance().MarkForDeletion(ctx, inst.ID)).To(Succeed())

			scheduler.ProcessPendingDeletions(ctx)

			found, err := dataStore.ServiceTypeInstance().Get(ctx, inst.ID, true)
			Expect(err).NotTo(HaveOccurred())
			Expect(*found.DeletionStatus).To(Equal("DELETED"))
		})

		It("marks DELETED for agent-routed instance regardless of service_type", func() {
			agentName := "mismatch-agent"
			inst := model.ServiceTypeInstance{
				ID:           uuid.New().String(),
				ServiceType:  "storage",
				Status:       "PROVISIONING",
				InstanceName: "mismatch-inst",
				Spec:         map[string]any{"size": 100},
				AgentName:    &agentName,
			}
			Expect(db.Create(&inst).Error).NotTo(HaveOccurred())
			Expect(dataStore.ServiceTypeInstance().MarkForDeletion(ctx, inst.ID)).To(Succeed())

			scheduler.ProcessPendingDeletions(ctx)

			found, err := dataStore.ServiceTypeInstance().Get(ctx, inst.ID, true)
			Expect(err).NotTo(HaveOccurred())
			Expect(*found.DeletionStatus).To(Equal("DELETED"))
		})

		It("treats instance without agent_name as agent-routed (agent-only world)", func() {
			inst := model.ServiceTypeInstance{
				ID:           uuid.New().String(),
				ServiceType:  "vm",
				Status:       "PROVISIONING",
				InstanceName: "no-agent-inst",
				Spec:         map[string]any{"cpu": 1},
			}
			Expect(db.Create(&inst).Error).NotTo(HaveOccurred())
			Expect(dataStore.ServiceTypeInstance().MarkForDeletion(ctx, inst.ID)).To(Succeed())

			scheduler.ProcessPendingDeletions(ctx)

			found, err := dataStore.ServiceTypeInstance().Get(ctx, inst.ID, true)
			Expect(err).NotTo(HaveOccurred())
			Expect(*found.DeletionStatus).To(Equal("DELETED"))
		})
	})

	Describe("Ack-driven deletion (agent + publisher configured)", func() {
		It("stays SCHEDULED and does not mark DELETED after a successful publish, awaiting the agent's ack", func() {
			agentName := "audit-agent"
			pub := messaging.NewPublisher(&stubJetStream{})
			schedulerWithAgent := cleanup.NewScheduler(dataStore, pub, agentstore.NewAgent(db), &config.CleanupConfig{MaxRetries: 3})

			inst := model.ServiceTypeInstance{
				ID:           uuid.New().String(),
				ServiceType:  "vm",
				Status:       "deleting",
				InstanceName: "ack-driven-inst",
				Spec:         map[string]any{"cpu": 1},
				AgentName:    &agentName,
			}
			Expect(db.Create(&inst).Error).NotTo(HaveOccurred())
			Expect(dataStore.ServiceTypeInstance().MarkForDeletion(ctx, inst.ID)).To(Succeed())

			schedulerWithAgent.ProcessPendingDeletions(ctx)

			found, err := dataStore.ServiceTypeInstance().Get(ctx, inst.ID, true)
			Expect(err).NotTo(HaveOccurred())
			Expect(*found.DeletionStatus).To(Equal("SCHEDULED"))
			Expect(found.RetryCount).To(Equal(1))
		})

		It("marks FAILED for manual intervention once retries are exhausted", func() {
			agentName := "audit-agent"
			pub := messaging.NewPublisher(&stubJetStream{})
			schedulerWithAgent := cleanup.NewScheduler(dataStore, pub, agentstore.NewAgent(db), &config.CleanupConfig{MaxRetries: 2})

			inst := model.ServiceTypeInstance{
				ID:           uuid.New().String(),
				ServiceType:  "vm",
				Status:       "deleting",
				InstanceName: "exhausted-inst",
				Spec:         map[string]any{"cpu": 1},
				AgentName:    &agentName,
				RetryCount:   2,
			}
			Expect(db.Create(&inst).Error).NotTo(HaveOccurred())
			Expect(dataStore.ServiceTypeInstance().MarkForDeletion(ctx, inst.ID)).To(Succeed())
			// MarkForDeletion resets retry_count; simulate prior attempts explicitly.
			Expect(db.Model(&inst).Update("retry_count", 2).Error).NotTo(HaveOccurred())

			schedulerWithAgent.ProcessPendingDeletions(ctx)

			found, err := dataStore.ServiceTypeInstance().Get(ctx, inst.ID, true)
			Expect(err).NotTo(HaveOccurred())
			Expect(*found.DeletionStatus).To(Equal("FAILED"))
		})
	})
})
