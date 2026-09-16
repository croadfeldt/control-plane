package resource_manager_test

import (
	"context"

	agentmodel "github.com/dcm-project/control-plane/internal/agent/store/model"
	server "github.com/dcm-project/control-plane/internal/sp/api/resource_manager"
	rmhandlers "github.com/dcm-project/control-plane/internal/sp/handlers/resource_manager"
	rmsvc "github.com/dcm-project/control-plane/internal/sp/service/resource_manager"
	"github.com/dcm-project/control-plane/internal/sp/store"
	"github.com/dcm-project/control-plane/internal/sp/store/model"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var _ = Describe("Resource Manager Handler", func() {
	var (
		db      *gorm.DB
		handler *rmhandlers.Handler
		ctx     context.Context
	)

	BeforeEach(func() {
		var err error
		db, err = gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
			Logger: logger.Default.LogMode(logger.Silent),
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(db.AutoMigrate(&agentmodel.Agent{}, &model.ServiceTypeInstance{})).To(Succeed())

		dataStore := store.NewStore(db)
		instanceService := rmsvc.NewInstanceService(dataStore, nil, nil)
		handler = rmhandlers.NewHandler(instanceService)
		ctx = context.Background()
	})

	AfterEach(func() {
		sqlDB, _ := db.DB()
		_ = sqlDB.Close()
	})

	Describe("CreateInstance", func() {
		// This endpoint never receives an agent_name from the caller - it's
		// resolved upstream by placement/SPRM - so it always calls
		// CreateInstance with agentName="", which must be rejected.
		It("returns 400 because this endpoint never supplies an agent name", func() {
			req := server.CreateInstanceRequestObject{
				Body: &server.ServiceTypeInstance{
					Spec: map[string]interface{}{"cpu": 2, "memory": "4GB", "service_type": "vm"},
				},
			}

			resp, err := handler.CreateInstance(ctx, req)

			Expect(err).NotTo(HaveOccurred())
			_, ok := resp.(server.CreateInstance400ApplicationProblemPlusJSONResponse)
			Expect(ok).To(BeTrue())
		})

		It("returns 400 when spec is missing service_type", func() {
			req := server.CreateInstanceRequestObject{
				Body: &server.ServiceTypeInstance{
					Spec: map[string]interface{}{"cpu": 2},
				},
			}

			resp, err := handler.CreateInstance(ctx, req)

			Expect(err).NotTo(HaveOccurred())
			_, ok := resp.(server.CreateInstance400ApplicationProblemPlusJSONResponse)
			Expect(ok).To(BeTrue())
		})

		It("returns 400 when spec.service_type is not a string", func() {
			req := server.CreateInstanceRequestObject{
				Body: &server.ServiceTypeInstance{
					Spec: map[string]interface{}{"cpu": 2, "service_type": 42},
				},
			}

			resp, err := handler.CreateInstance(ctx, req)

			Expect(err).NotTo(HaveOccurred())
			_, ok := resp.(server.CreateInstance400ApplicationProblemPlusJSONResponse)
			Expect(ok).To(BeTrue())
		})

		It("returns 400 when spec.service_type is an empty string", func() {
			req := server.CreateInstanceRequestObject{
				Body: &server.ServiceTypeInstance{
					Spec: map[string]interface{}{"cpu": 2, "service_type": ""},
				},
			}

			resp, err := handler.CreateInstance(ctx, req)

			Expect(err).NotTo(HaveOccurred())
			_, ok := resp.(server.CreateInstance400ApplicationProblemPlusJSONResponse)
			Expect(ok).To(BeTrue())
		})
	})

	Describe("GetInstance", func() {
		It("returns instance", func() {
			instanceID := uuid.New().String()
			db.Create(&model.ServiceTypeInstance{
				ID:          instanceID,
				ServiceType: "vm",
				Status:      "pending",
				Spec:        map[string]any{"cpu": 2, "service_type": "vm"},
			})

			req := server.GetInstanceRequestObject{
				InstanceId: instanceID,
			}

			resp, err := handler.GetInstance(ctx, req)

			Expect(err).NotTo(HaveOccurred())
			jsonResp, ok := resp.(server.GetInstance200JSONResponse)
			Expect(ok).To(BeTrue())
			Expect(*jsonResp.Id).To(Equal(instanceID))
		})

		It("returns 404 for non-existent instance", func() {
			req := server.GetInstanceRequestObject{
				InstanceId: uuid.New().String(),
			}

			resp, err := handler.GetInstance(ctx, req)

			Expect(err).NotTo(HaveOccurred())
			_, ok := resp.(server.GetInstance404ApplicationProblemPlusJSONResponse)
			Expect(ok).To(BeTrue())
		})
	})

	Describe("ListInstances", func() {
		It("returns empty list initially", func() {
			req := server.ListInstancesRequestObject{}

			resp, err := handler.ListInstances(ctx, req)

			Expect(err).NotTo(HaveOccurred())
			jsonResp, ok := resp.(server.ListInstances200JSONResponse)
			Expect(ok).To(BeTrue())
			Expect(*jsonResp.Instances).To(BeEmpty())
		})

		It("returns instances", func() {
			for i := 0; i < 3; i++ {
				db.Create(&model.ServiceTypeInstance{
					ID:          uuid.New().String(),
					ServiceType: "vm",
					Status:      "pending",
					Spec:        map[string]any{"cpu": 1, "service_type": "vm"},
				})
			}

			resp, err := handler.ListInstances(ctx, server.ListInstancesRequestObject{})

			Expect(err).NotTo(HaveOccurred())
			jsonResp, ok := resp.(server.ListInstances200JSONResponse)
			Expect(ok).To(BeTrue())
			Expect(*jsonResp.Instances).To(HaveLen(3))
		})

		It("filters by service type and agent name independently, without swapping them", func() {
			agentA, agentB := "agent-a", "agent-b"
			vmID, dbID := uuid.New().String(), uuid.New().String()
			db.Create(&model.ServiceTypeInstance{
				ID:          vmID,
				ServiceType: "vm",
				AgentName:   &agentA,
				Status:      "pending",
				Spec:        map[string]any{"cpu": 1, "service_type": "vm"},
			})
			db.Create(&model.ServiceTypeInstance{
				ID:          dbID,
				ServiceType: "db",
				AgentName:   &agentB,
				Status:      "pending",
				Spec:        map[string]any{"cpu": 1, "service_type": "db"},
			})

			serviceType := "vm"
			resp, err := handler.ListInstances(ctx, server.ListInstancesRequestObject{
				Params: server.ListInstancesParams{ServiceType: &serviceType},
			})
			Expect(err).NotTo(HaveOccurred())
			jsonResp, ok := resp.(server.ListInstances200JSONResponse)
			Expect(ok).To(BeTrue())
			Expect(*jsonResp.Instances).To(HaveLen(1))
			Expect(*(*jsonResp.Instances)[0].Id).To(Equal(vmID))

			resp, err = handler.ListInstances(ctx, server.ListInstancesRequestObject{
				Params: server.ListInstancesParams{AgentName: &agentB},
			})
			Expect(err).NotTo(HaveOccurred())
			jsonResp, ok = resp.(server.ListInstances200JSONResponse)
			Expect(ok).To(BeTrue())
			Expect(*jsonResp.Instances).To(HaveLen(1))
			Expect(*(*jsonResp.Instances)[0].Id).To(Equal(dbID))
			Expect(*(*jsonResp.Instances)[0].AgentName).To(Equal(agentB))
		})

		It("respects max page size and returns next page token", func() {
			for i := 0; i < 5; i++ {
				db.Create(&model.ServiceTypeInstance{
					ID:          uuid.New().String(),
					ServiceType: "vm",
					Status:      "pending",
					Spec:        map[string]any{"cpu": 1, "service_type": "vm"},
				})
			}

			// First page: request 2 items
			maxPageSize := 2
			req := server.ListInstancesRequestObject{
				Params: server.ListInstancesParams{MaxPageSize: &maxPageSize},
			}

			resp, err := handler.ListInstances(ctx, req)

			Expect(err).NotTo(HaveOccurred())
			jsonResp, ok := resp.(server.ListInstances200JSONResponse)
			Expect(ok).To(BeTrue())
			Expect(*jsonResp.Instances).To(HaveLen(2))
			Expect(jsonResp.NextPageToken).NotTo(BeNil())
			Expect(*jsonResp.NextPageToken).NotTo(BeEmpty())

			// Second page: use the page token
			req2 := server.ListInstancesRequestObject{
				Params: server.ListInstancesParams{
					MaxPageSize: &maxPageSize,
					PageToken:   jsonResp.NextPageToken,
				},
			}

			resp2, err := handler.ListInstances(ctx, req2)

			Expect(err).NotTo(HaveOccurred())
			jsonResp2, ok := resp2.(server.ListInstances200JSONResponse)
			Expect(ok).To(BeTrue())
			Expect(*jsonResp2.Instances).To(HaveLen(2))
			Expect(jsonResp2.NextPageToken).NotTo(BeNil())

			// Third page: should have 1 item and no next token
			req3 := server.ListInstancesRequestObject{
				Params: server.ListInstancesParams{
					MaxPageSize: &maxPageSize,
					PageToken:   jsonResp2.NextPageToken,
				},
			}

			resp3, err := handler.ListInstances(ctx, req3)

			Expect(err).NotTo(HaveOccurred())
			jsonResp3, ok := resp3.(server.ListInstances200JSONResponse)
			Expect(ok).To(BeTrue())
			Expect(*jsonResp3.Instances).To(HaveLen(1))
			Expect(jsonResp3.NextPageToken).To(BeNil())
		})
	})

	Describe("DeleteInstance", func() {
		It("deletes instance and returns 204", func() {
			instanceID := uuid.New().String()
			db.Create(&model.ServiceTypeInstance{
				ID:          instanceID,
				ServiceType: "vm",
				Status:      "pending",
				Spec:        map[string]any{"cpu": 2, "service_type": "vm"},
			})

			req := server.DeleteInstanceRequestObject{
				InstanceId: instanceID,
			}

			resp, err := handler.DeleteInstance(ctx, req)

			Expect(err).NotTo(HaveOccurred())
			_, ok := resp.(server.DeleteInstance204Response)
			Expect(ok).To(BeTrue())

			// Verify it's deleted
			getResp, _ := handler.GetInstance(ctx, server.GetInstanceRequestObject{InstanceId: instanceID})
			_, ok = getResp.(server.GetInstance404ApplicationProblemPlusJSONResponse)
			Expect(ok).To(BeTrue())
		})

		It("returns 404 for non-existent instance", func() {
			req := server.DeleteInstanceRequestObject{
				InstanceId: uuid.New().String(),
			}

			resp, err := handler.DeleteInstance(ctx, req)

			Expect(err).NotTo(HaveOccurred())
			_, ok := resp.(server.DeleteInstance404ApplicationProblemPlusJSONResponse)
			Expect(ok).To(BeTrue())
		})
	})
})
