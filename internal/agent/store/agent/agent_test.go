package agent_test

import (
	"context"
	"time"

	"github.com/dcm-project/control-plane/internal/agent/store/agent"
	"github.com/dcm-project/control-plane/internal/agent/store/model"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var _ = Describe("Agent Store", func() {
	var (
		db         *gorm.DB
		agentStore agent.Agent
		ctx        context.Context
	)

	BeforeEach(func() {
		var err error
		db, err = gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
			Logger: logger.Default.LogMode(logger.Silent),
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(db.AutoMigrate(&model.Agent{})).To(Succeed())

		agentStore = agent.NewAgent(db)
		ctx = context.Background()
	})

	AfterEach(func() {
		sqlDB, _ := db.DB()
		_ = sqlDB.Close()
	})

	Describe("Create", func() {
		It("persists the agent and returns generated ID", func() {
			a := newAgent("create-test")
			created, err := agentStore.Create(ctx, a)

			Expect(err).NotTo(HaveOccurred())
			Expect(created.ID).To(Equal(a.ID))
			Expect(created.Name).To(Equal("create-test"))
			Expect(created.TopicName).To(Equal("dcm.agent.create-test"))
			Expect(created.HealthStatus).To(Equal(model.AgentHealthStatusReady))
		})

		It("rejects duplicate names with ErrAgentConflict (K)", func() {
			a1 := newAgent("duplicate-name")
			_, err := agentStore.Create(ctx, a1)
			Expect(err).NotTo(HaveOccurred())

			a2 := newAgent("duplicate-name")
			_, err = agentStore.Create(ctx, a2)
			Expect(err).To(MatchError(agent.ErrAgentConflict))
		})

		It("rejects duplicate topic_name across different agent names with ErrAgentConflict (K)", func() {
			a1 := newAgent("agent-one")
			_, err := agentStore.Create(ctx, a1)
			Expect(err).NotTo(HaveOccurred())

			a2 := newAgent("agent-two")
			a2.TopicName = a1.TopicName
			_, err = agentStore.Create(ctx, a2)
			Expect(err).To(MatchError(agent.ErrAgentConflict))
		})
	})

	Describe("Get", func() {
		It("retrieves by ID", func() {
			a := newAgent("get-test")
			_, err := agentStore.Create(ctx, a)
			Expect(err).NotTo(HaveOccurred())

			found, err := agentStore.Get(ctx, a.ID)

			Expect(err).NotTo(HaveOccurred())
			Expect(found.Name).To(Equal("get-test"))
		})

		It("returns ErrAgentNotFound for missing ID", func() {
			_, err := agentStore.Get(ctx, uuid.New().String())

			Expect(err).To(Equal(agent.ErrAgentNotFound))
		})
	})

	Describe("GetByName", func() {
		It("retrieves by name", func() {
			a := newAgent("named-agent")
			_, err := agentStore.Create(ctx, a)
			Expect(err).NotTo(HaveOccurred())

			found, err := agentStore.GetByName(ctx, "named-agent")

			Expect(err).NotTo(HaveOccurred())
			Expect(found.ID).To(Equal(a.ID))
		})

		It("returns ErrAgentNotFound for missing name", func() {
			_, err := agentStore.GetByName(ctx, "non-existent")

			Expect(err).To(Equal(agent.ErrAgentNotFound))
		})
	})

	Describe("List", func() {
		It("returns all agents when filter is nil", func() {
			_, err := agentStore.Create(ctx, newAgent("a1"))
			Expect(err).NotTo(HaveOccurred())
			_, err = agentStore.Create(ctx, newAgent("a2"))
			Expect(err).NotTo(HaveOccurred())

			result, err := agentStore.List(ctx, nil, nil)

			Expect(err).NotTo(HaveOccurred())
			Expect(result.Agents).To(HaveLen(2))
			Expect(result.NextPageToken).To(BeEmpty())
		})

		It("filters by health_status", func() {
			a1 := newAgent("ready-agent")
			_, err := agentStore.Create(ctx, a1)
			Expect(err).NotTo(HaveOccurred())

			a2 := newAgent("unavailable-agent")
			a2.HealthStatus = model.AgentHealthStatusUnavailable
			_, err = agentStore.Create(ctx, a2)
			Expect(err).NotTo(HaveOccurred())

			readyStatus := model.AgentHealthStatusReady
			result, err := agentStore.List(ctx, &agent.AgentFilter{HealthStatus: &readyStatus}, nil)

			Expect(err).NotTo(HaveOccurred())
			Expect(result.Agents).To(HaveLen(1))
			Expect(result.Agents[0].Name).To(Equal("ready-agent"))
		})

		It("respects the requested page size", func() {
			for i := 0; i < 3; i++ {
				_, err := agentStore.Create(ctx, newAgent(uuid.New().String()))
				Expect(err).NotTo(HaveOccurred())
			}

			result, err := agentStore.List(ctx, nil, &agent.Pagination{Limit: 2})

			Expect(err).NotTo(HaveOccurred())
			Expect(result.Agents).To(HaveLen(2))
		})

		// Cursor pagination: real multi-page listing with a NextPageToken
		// round-trip, mirroring internal/policy/store/pagination_test.go.
		It("returns a NextPageToken when more results exist, and paginates through all agents with it", func() {
			names := []string{"page-agent-1", "page-agent-2", "page-agent-3", "page-agent-4", "page-agent-5"}
			for _, n := range names {
				_, err := agentStore.Create(ctx, newAgent(n))
				Expect(err).NotTo(HaveOccurred())
			}

			var seen []string
			pageToken := ""
			for {
				result, err := agentStore.List(ctx, nil, &agent.Pagination{Limit: 2, PageToken: pageToken})
				Expect(err).NotTo(HaveOccurred())
				for _, a := range result.Agents {
					seen = append(seen, a.Name)
				}
				if result.NextPageToken == "" {
					break
				}
				pageToken = result.NextPageToken
			}

			Expect(seen).To(Equal(names), "name ASC ordering must be stable across pages so the cursor never skips or repeats a row")
		})

		It("returns an empty NextPageToken on the last page", func() {
			for i := 0; i < 2; i++ {
				_, err := agentStore.Create(ctx, newAgent(uuid.New().String()))
				Expect(err).NotTo(HaveOccurred())
			}

			result, err := agentStore.List(ctx, nil, &agent.Pagination{Limit: 2})

			Expect(err).NotTo(HaveOccurred())
			Expect(result.Agents).To(HaveLen(2))
			Expect(result.NextPageToken).To(BeEmpty())
		})

		It("returns ErrInvalidPageToken for a malformed page_token", func() {
			_, err := agentStore.List(ctx, nil, &agent.Pagination{Limit: 2, PageToken: "not-valid-base64!!!"})

			Expect(err).To(MatchError(agent.ErrInvalidPageToken))
		})
	})

	Describe("Update", func() {
		It("updates fields and returns updated agent", func() {
			a := newAgent("to-update")
			_, err := agentStore.Create(ctx, a)
			Expect(err).NotTo(HaveOccurred())

			a.Environment = "staging"
			updated, err := agentStore.Update(ctx, a)

			Expect(err).NotTo(HaveOccurred())
			Expect(updated.Environment).To(Equal("staging"))
		})

		It("returns ErrAgentNotFound for non-existing agent", func() {
			a := newAgent("non-existing")
			_, err := agentStore.Update(ctx, a)

			Expect(err).To(Equal(agent.ErrAgentNotFound))
		})

		It("does not clobber a concurrently-applied newer heartbeat (R2 S6: finding #3)", func() {
			// Simulates RegisterOrUpdate racing a genuine heartbeat: the
			// heartbeat (UpdateHeartbeatIfNewer) lands first with a newer
			// timestamp reporting "congested"; RegisterOrUpdate's own
			// Update, built from a snapshot read before that heartbeat
			// landed, must not unconditionally revert health_status back to
			// "ready" with its own (older) timestamp.
			a := newAgent("racing-agent")
			a.HealthStatus = model.AgentHealthStatusReady
			olderHB := time.Now()
			a.LastHeartbeat = &olderHB
			_, err := agentStore.Create(ctx, a)
			Expect(err).NotTo(HaveOccurred())

			newerHB := olderHB.Add(time.Second)
			applied, err := agentStore.UpdateHeartbeatIfNewer(ctx, a.ID, newerHB, model.AgentHealthStatusCongested)
			Expect(err).NotTo(HaveOccurred())
			Expect(applied).To(BeTrue())

			// RegisterOrUpdate's stale snapshot: still thinks last_heartbeat
			// is olderHB and wants to force health_status back to "ready".
			a.HealthStatus = model.AgentHealthStatusReady
			a.LastHeartbeat = &olderHB
			updated, err := agentStore.Update(ctx, a)
			Expect(err).NotTo(HaveOccurred())

			Expect(updated.HealthStatus).To(Equal(model.AgentHealthStatusCongested))
			Expect(updated.LastHeartbeat.Unix()).To(Equal(newerHB.Unix()))
		})
	})

	Describe("Delete", func() {
		It("removes the agent", func() {
			a := newAgent("to-delete")
			_, err := agentStore.Create(ctx, a)
			Expect(err).NotTo(HaveOccurred())

			err = agentStore.Delete(ctx, a.ID)

			Expect(err).NotTo(HaveOccurred())

			_, err = agentStore.Get(ctx, a.ID)
			Expect(err).To(Equal(agent.ErrAgentNotFound))
		})

		It("returns ErrAgentNotFound for missing ID", func() {
			err := agentStore.Delete(ctx, uuid.New().String())

			Expect(err).To(Equal(agent.ErrAgentNotFound))
		})
	})

	Describe("UpdateHeartbeatIfNewer", func() {
		It("applies when there is no prior heartbeat", func() {
			a := newAgent("first-heartbeat")
			_, err := agentStore.Create(ctx, a)
			Expect(err).NotTo(HaveOccurred())

			applied, err := agentStore.UpdateHeartbeatIfNewer(ctx, a.ID, time.Now(), model.AgentHealthStatusReady)
			Expect(err).NotTo(HaveOccurred())
			Expect(applied).To(BeTrue())
		})

		It("applies a newer timestamp and rejects a stale one, closing the read-then-write race (L)", func() {
			a := newAgent("monotonic-heartbeat")
			_, err := agentStore.Create(ctx, a)
			Expect(err).NotTo(HaveOccurred())

			newer := time.Now()
			applied, err := agentStore.UpdateHeartbeatIfNewer(ctx, a.ID, newer, model.AgentHealthStatusReady)
			Expect(err).NotTo(HaveOccurred())
			Expect(applied).To(BeTrue())

			stale := newer.Add(-1 * time.Hour)
			applied, err = agentStore.UpdateHeartbeatIfNewer(ctx, a.ID, stale, model.AgentHealthStatusCongested)
			Expect(err).NotTo(HaveOccurred())
			Expect(applied).To(BeFalse())

			found, err := agentStore.Get(ctx, a.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(found.LastHeartbeat).NotTo(BeNil())
			Expect(*found.LastHeartbeat).To(BeTemporally("~", newer, time.Millisecond))
			// The stale write's health_status must not have landed either -
			// the whole update (both columns) is guarded by the same CAS.
			Expect(found.HealthStatus).To(Equal(model.AgentHealthStatusReady))
		})

		It("returns applied=false for an unknown agent", func() {
			applied, err := agentStore.UpdateHeartbeatIfNewer(ctx, uuid.New().String(), time.Now(), model.AgentHealthStatusReady)
			Expect(err).NotTo(HaveOccurred())
			Expect(applied).To(BeFalse())
		})
	})

	Describe("MarkStaleUnavailable", func() {
		It("flips agents whose heartbeat is older than cutoff to Unavailable in one pass", func() {
			stale := newAgent("stale-agent")
			_, err := agentStore.Create(ctx, stale)
			Expect(err).NotTo(HaveOccurred())
			oldHB := time.Now().Add(-1 * time.Hour)
			_, err = agentStore.UpdateHeartbeatIfNewer(ctx, stale.ID, oldHB, model.AgentHealthStatusReady)
			Expect(err).NotTo(HaveOccurred())

			fresh := newAgent("fresh-agent")
			_, err = agentStore.Create(ctx, fresh)
			Expect(err).NotTo(HaveOccurred())
			_, err = agentStore.UpdateHeartbeatIfNewer(ctx, fresh.ID, time.Now(), model.AgentHealthStatusReady)
			Expect(err).NotTo(HaveOccurred())

			cutoff := time.Now().Add(-30 * time.Minute)
			Expect(agentStore.MarkStaleUnavailable(ctx, cutoff)).To(Succeed())

			staleFound, err := agentStore.Get(ctx, stale.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(staleFound.HealthStatus).To(Equal(model.AgentHealthStatusUnavailable))

			freshFound, err := agentStore.Get(ctx, fresh.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(freshFound.HealthStatus).To(Equal(model.AgentHealthStatusReady))
		})

		It("does not clobber a heartbeat that lands concurrently with the sweep (M)", func() {
			// Simulates the race the read-then-write version of sweep() was
			// vulnerable to: MarkStaleUnavailable re-checks staleness in the
			// same statement as the write, so a heartbeat recorded AFTER
			// cutoff was computed (but before/at the same moment the sweep
			// runs) is never overwritten back to Unavailable.
			a := newAgent("race-agent")
			_, err := agentStore.Create(ctx, a)
			Expect(err).NotTo(HaveOccurred())

			cutoff := time.Now()
			_, err = agentStore.UpdateHeartbeatIfNewer(ctx, a.ID, time.Now(), model.AgentHealthStatusReady)
			Expect(err).NotTo(HaveOccurred())

			Expect(agentStore.MarkStaleUnavailable(ctx, cutoff)).To(Succeed())

			found, err := agentStore.Get(ctx, a.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(found.HealthStatus).To(Equal(model.AgentHealthStatusReady))
		})
	})

	Describe("ListReady", func() {
		It("returns only agents with health_status ready", func() {
			a1 := newAgent("ready-1")
			_, err := agentStore.Create(ctx, a1)
			Expect(err).NotTo(HaveOccurred())

			a2 := newAgent("congested-1")
			a2.HealthStatus = model.AgentHealthStatusCongested
			_, err = agentStore.Create(ctx, a2)
			Expect(err).NotTo(HaveOccurred())

			a3 := newAgent("ready-2")
			_, err = agentStore.Create(ctx, a3)
			Expect(err).NotTo(HaveOccurred())

			agents, err := agentStore.ListReady(ctx)

			Expect(err).NotTo(HaveOccurred())
			Expect(agents).To(HaveLen(2))
		})
	})
})

func newAgent(name string) model.Agent {
	return model.Agent{
		ID:           uuid.New().String(),
		Name:         name,
		Environment:  "production",
		ServiceTypes: []string{"vm", "container"},
		TopicName:    "dcm.agent." + name,
		HealthStatus: model.AgentHealthStatusReady,
	}
}
