package service_test

import (
	"context"
	"time"

	api "github.com/dcm-project/control-plane/api/agent/v1alpha1"
	"github.com/dcm-project/control-plane/internal/agent/service"
	"github.com/dcm-project/control-plane/internal/agent/store/model"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	agentstore "github.com/dcm-project/control-plane/internal/agent/store/agent"
)

var _ = Describe("AgentService", func() {
	var (
		db         *gorm.DB
		store      agentstore.Agent
		svc        *service.AgentService
		ctx        context.Context
		defaultLag int64
	)

	BeforeEach(func() {
		var err error
		db, err = gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
			Logger: logger.Default.LogMode(logger.Silent),
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(db.AutoMigrate(&model.Agent{})).To(Succeed())

		store = agentstore.NewAgent(db)
		defaultLag = 100
		svc = service.NewAgentService(store, defaultLag)
		ctx = context.Background()
	})

	AfterEach(func() {
		sqlDB, _ := db.DB()
		_ = sqlDB.Close()
	})

	Describe("RegisterOrUpdate", func() {
		It("creates new agent with valid payload", func() {
			req := validRegistrationRequest("new-agent")

			result, created, err := svc.RegisterOrUpdate(ctx, req)

			Expect(err).NotTo(HaveOccurred())
			Expect(created).To(BeTrue())
			Expect(result).NotTo(BeNil())
			Expect(result.AgentId).NotTo(BeNil())
			Expect(*result.Name).To(Equal("new-agent"))
		})

		It("returns 200 and updates existing agent by name", func() {
			req := validRegistrationRequest("existing-agent")
			_, _, err := svc.RegisterOrUpdate(ctx, req)
			Expect(err).NotTo(HaveOccurred())

			req.Environment = "staging"
			result, created, err := svc.RegisterOrUpdate(ctx, req)

			Expect(err).NotTo(HaveOccurred())
			Expect(created).To(BeFalse())
			Expect(result).NotTo(BeNil())
		})

		It("round-trips the cost enum value on create and re-registration", func() {
			req := validRegistrationRequest("cost-agent")
			req.Cost = api.AgentRegistrationRequestCostHigh

			result, _, err := svc.RegisterOrUpdate(ctx, req)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Cost).NotTo(BeNil())
			Expect(*result.Cost).To(Equal(api.AgentCostHigh))

			req.Cost = api.AgentRegistrationRequestCostLow
			result, _, err = svc.RegisterOrUpdate(ctx, req)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Cost).NotTo(BeNil())
			Expect(*result.Cost).To(Equal(api.AgentCostLow))
		})

		It("allows empty service_types on re-registration", func() {
			req := validRegistrationRequest("re-reg-agent")
			_, _, err := svc.RegisterOrUpdate(ctx, req)
			Expect(err).NotTo(HaveOccurred())

			req.ServiceTypes = []string{}
			_, _, err = svc.RegisterOrUpdate(ctx, req)

			Expect(err).NotTo(HaveOccurred())
		})

		It("rejects topic_name not starting with dcm.agent.", func() {
			req := validRegistrationRequest("bad-topic")
			req.TopicName = "invalid.topic.name"

			_, _, err := svc.RegisterOrUpdate(ctx, req)

			Expect(err).To(HaveOccurred())
			svcErr, ok := err.(*service.ServiceError)
			Expect(ok).To(BeTrue())
			Expect(svcErr.Code).To(Equal(service.ErrCodeValidation))
		})

		It("returns a conflict error when a different agent already owns the topic_name (K)", func() {
			first := validRegistrationRequest("topic-owner")
			_, _, err := svc.RegisterOrUpdate(ctx, first)
			Expect(err).NotTo(HaveOccurred())

			second := validRegistrationRequest("topic-squatter")
			second.TopicName = first.TopicName

			_, _, err = svc.RegisterOrUpdate(ctx, second)

			Expect(err).To(HaveOccurred())
			svcErr, ok := err.(*service.ServiceError)
			Expect(ok).To(BeTrue())
			Expect(svcErr.Code).To(Equal(service.ErrCodeConflict))
		})
	})

	Describe("Heartbeat", func() {
		It("marks Ready when consumer_lag < threshold", func() {
			req := validRegistrationRequest("ready-agent")
			result, _, err := svc.RegisterOrUpdate(ctx, req)
			Expect(err).NotTo(HaveOccurred())

			now := time.Now()
			hbReq := api.HeartbeatRequest{
				ConsumerLag: 0,
				Timestamp:   now,
			}

			updated, err := svc.Heartbeat(ctx, *result.AgentId, hbReq)

			Expect(err).NotTo(HaveOccurred())
			Expect(updated).NotTo(BeNil())
			Expect(*updated.HealthStatus).To(Equal(api.AgentHealthStatus("ready")))
		})

		It("marks Congested when consumer_lag >= threshold", func() {
			req := validRegistrationRequest("congested-agent")
			result, _, err := svc.RegisterOrUpdate(ctx, req)
			Expect(err).NotTo(HaveOccurred())

			now := time.Now()
			hbReq := api.HeartbeatRequest{
				ConsumerLag: defaultLag + 1,
				Timestamp:   now,
			}

			updated, err := svc.Heartbeat(ctx, *result.AgentId, hbReq)

			Expect(err).NotTo(HaveOccurred())
			Expect(updated).NotTo(BeNil())
			Expect(*updated.HealthStatus).To(Equal(api.AgentHealthStatus("congested")))
		})

		It("ignores stale heartbeat timestamp without overwriting the newer one (L)", func() {
			req := validRegistrationRequest("stale-hb-agent")
			result, _, err := svc.RegisterOrUpdate(ctx, req)
			Expect(err).NotTo(HaveOccurred())

			now := time.Now()
			hbReq := api.HeartbeatRequest{
				ConsumerLag: 0,
				Timestamp:   now,
			}
			_, err = svc.Heartbeat(ctx, *result.AgentId, hbReq)
			Expect(err).NotTo(HaveOccurred())

			staleReq := api.HeartbeatRequest{
				ConsumerLag: 0,
				Timestamp:   now.Add(-1 * time.Hour),
			}
			updated, err := svc.Heartbeat(ctx, *result.AgentId, staleReq)

			Expect(err).NotTo(HaveOccurred())
			// The atomic CAS (UpdateHeartbeatIfNewer) must leave the
			// previously-recorded, newer timestamp in place rather than
			// letting the stale write land.
			Expect(updated.LastHeartbeat).NotTo(BeNil())
			Expect(*updated.LastHeartbeat).To(BeTemporally("~", now, time.Second))
		})

		It("returns not-found for unknown agent", func() {
			hbReq := api.HeartbeatRequest{
				ConsumerLag: 0,
				Timestamp:   time.Now(),
			}

			_, err := svc.Heartbeat(ctx, uuid.New().String(), hbReq)

			Expect(err).To(HaveOccurred())
			svcErr, ok := err.(*service.ServiceError)
			Expect(ok).To(BeTrue())
			Expect(svcErr.Code).To(Equal(service.ErrCodeNotFound))
		})
	})

	Describe("List", func() {
		It("paginates through all agents via the returned NextPageToken", func() {
			names := []string{"list-agent-1", "list-agent-2", "list-agent-3"}
			for _, n := range names {
				_, _, err := svc.RegisterOrUpdate(ctx, validRegistrationRequest(n))
				Expect(err).NotTo(HaveOccurred())
			}

			first, err := svc.List(ctx, "", 2, "")
			Expect(err).NotTo(HaveOccurred())
			Expect(first.Agents).To(HaveLen(2))
			Expect(first.NextPageToken).NotTo(BeEmpty())

			second, err := svc.List(ctx, "", 2, first.NextPageToken)
			Expect(err).NotTo(HaveOccurred())
			Expect(second.Agents).To(HaveLen(1))
			Expect(second.NextPageToken).To(BeEmpty())

			seen := make([]string, 0, len(first.Agents)+len(second.Agents))
			for _, a := range append(first.Agents, second.Agents...) {
				seen = append(seen, *a.Name)
			}
			Expect(seen).To(Equal(names))
		})

		It("returns a validation error for a malformed page_token", func() {
			_, err := svc.List(ctx, "", 10, "not-valid-base64!!!")

			Expect(err).To(HaveOccurred())
			svcErr, ok := err.(*service.ServiceError)
			Expect(ok).To(BeTrue())
			Expect(svcErr.Code).To(Equal(service.ErrCodeValidation))
		})
	})
})

func validRegistrationRequest(name string) api.AgentRegistrationRequest {
	return api.AgentRegistrationRequest{
		Name:         name,
		TopicName:    "dcm.agent." + name,
		ServiceTypes: []string{"vm", "container"},
		Environment:  "production",
		Cost:         api.AgentRegistrationRequestCostMedium,
	}
}
