package service_test

import (
	"context"
	"log/slog"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	agentmodel "github.com/dcm-project/control-plane/internal/agent/store/model"
	placementagent "github.com/dcm-project/control-plane/internal/placement/agent"
	"github.com/dcm-project/control-plane/internal/placement/service"
	"github.com/dcm-project/control-plane/internal/placement/store"
	"github.com/dcm-project/control-plane/internal/placement/store/model"
	"github.com/dcm-project/control-plane/internal/placement/types"
	"github.com/dcm-project/control-plane/internal/udlm/records"
	"github.com/google/uuid"
)

var _ = Describe("UDLM per-state records", func() {
	var (
		db           *gorm.DB
		recordStore  records.Store
		placementSvc *service.PlacementService
		ctx          context.Context
	)

	BeforeEach(func() {
		var err error
		db, err = gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
		Expect(err).NotTo(HaveOccurred())
		Expect(db.AutoMigrate(&agentmodel.Agent{}, &model.Resource{}, &records.StateRecord{})).To(Succeed())
		Expect(db.Create(&agentmodel.Agent{ID: uuid.New().String(), Name: "default-agent", TopicName: "dcm.agent.default-agent"}).Error).To(Succeed())

		recordStore = records.NewStore(db)
		lookup := func(slug string) (records.Type, bool) {
			if slug == "vm" {
				return records.Type{ResourceType: "Machine.VM", Version: "2.0.0"}, true
			}
			return records.Type{}, false
		}
		writer := records.NewWriter(recordStore, lookup, "00000000-0000-4000-8000-000000000000", slog.Default())
		agents := &mockAgentClient{agents: []placementagent.Info{{Name: "default-agent", Environment: "prod", ServiceTypes: []string{"vm"}, Cost: "low"}}}
		placementSvc = service.NewPlacementService(store.NewStore(db), &mockPolicyClient{}, &mockSPRMClient{},
			service.WithAgentClient(agents), service.WithRecordWriter(writer))
		ctx = context.Background()
	})

	AfterEach(func() {
		sqlDB, _ := db.DB()
		_ = sqlDB.Close()
	})

	It("writes intent and requested on CreateRun and realized on RUNNING, chained and valid", func() {
		spec := map[string]any{
			"service_type": "vm",
			"metadata":     map[string]any{"name": "app-01"},
			"cpu":          map[string]any{"count": 2},
			"memory":       map[string]any{"size": "4Gi"},
		}
		run, err := placementSvc.CreateRun(ctx, singleResourceRun("catalog-1", spec, nil))
		Expect(err).NotTo(HaveOccurred())
		entity := *run.Resources[0].Id

		recs, err := recordStore.ListByEntity(ctx, entity)
		Expect(err).NotTo(HaveOccurred())
		Expect(recs).To(HaveLen(2))
		Expect(recs[0].State).To(Equal("Intent"))
		Expect(recs[1].State).To(Equal("Requested"))
		Expect(recs[0].Body["fields"]).NotTo(HaveKey("service_type"))
		Expect(recs[0].Body["fields"]).To(HaveKey("cpu"))
		Expect(recs[0].ResourceType).To(Equal("Machine.VM"))
		Expect(recs[1].Body["intent_ref"]).To(Equal(recs[0].RecordUUID))

		eventTime := time.Date(2026, 9, 26, 10, 0, 0, 0, time.UTC)
		Expect(placementSvc.OnResourceRunning(ctx, types.ResourceStatusEvent{
			ResourceID: entity,
			Status:     types.ResourceStatusRunning,
			OutputSpec: map[string]any{"primary_ip": "10.0.0.5"},
			Timestamp:  eventTime,
		})).To(Succeed())

		recs, err = recordStore.ListByEntity(ctx, entity)
		Expect(err).NotTo(HaveOccurred())
		Expect(recs).To(HaveLen(3))
		realized := recs[2].Body
		Expect(recs[2].State).To(Equal("Realized"))
		Expect(realized["requested_ref"]).To(Equal(recs[1].RecordUUID))
		Expect(realized["provider"]).To(Equal("dcm/agents/default-agent"))
		Expect(realized["outputs"]).To(HaveKeyWithValue("primary_ip", "10.0.0.5"))
		Expect(realized["at"]).To(Equal("2026-09-26T10:00:00Z"))
		Expect(realized["provenance"]).To(HaveKey("outputs.primary_ip"))
		Expect(recs[0].Body["provenance"]).To(HaveKey("cpu.count"))
		Expect(recs[2].Body["metadata"].(map[string]any)["notes"].([]any)[0].(map[string]any)["text"]).To(Equal("placement run " + run.RunId))
		for i := range recs {
			Expect(records.Validate(recs[i].Body)).To(Succeed(), "record %d", i)
			Expect(records.Verify(recs[i].Body)).To(Succeed(), "record %d", i)
		}
		Expect(realized["integrity"].(map[string]any)["previous"]).To(Equal(recs[1].Head))
	})

	It("never blocks the placement path when a resource has no UDLM class", func() {
		run, err := placementSvc.CreateRun(ctx, singleResourceRun("catalog-2", map[string]any{"service_type": "network", "ports": []any{}}, nil))
		Expect(err).NotTo(HaveOccurred())
		recs, err := recordStore.ListByEntity(ctx, *run.Resources[0].Id)
		Expect(err).NotTo(HaveOccurred())
		Expect(recs).To(BeEmpty())
	})
})
