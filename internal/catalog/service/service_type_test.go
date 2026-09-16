package service_test

import (
	"context"
	"log/slog"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/dcm-project/control-plane/internal/catalog/config"
	"github.com/dcm-project/control-plane/internal/catalog/service"
	"github.com/dcm-project/control-plane/internal/catalog/store"
	"github.com/dcm-project/control-plane/internal/catalog/store/model"
)

var _ = Describe("ServiceType Service", func() {
	var (
		ctx context.Context
		db  *gorm.DB
		str store.Store
		svc service.Service
	)

	BeforeEach(func() {
		ctx = context.Background()
		var err error
		db, err = gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
			Logger: logger.Discard,
		})
		Expect(err).ToNot(HaveOccurred())
		err = db.AutoMigrate(&model.ServiceType{}, &model.CatalogItem{})
		Expect(err).ToNot(HaveOccurred())
		str = store.NewStore(db, slog.Default())
		svc, err = service.NewService(str, &mockPMClient{}, config.DefaultSeedConfig(), slog.Default())
		Expect(err).ToNot(HaveOccurred())
	})

	AfterEach(func() {
		if str != nil {
			Expect(str.Close()).To(Succeed())
		}
	})

	Describe("Create", func() {
		Context("with valid allowed service types", func() {
			It("should create a service type with 'vm'", func() {
				req := &service.CreateServiceTypeRequest{
					ApiVersion:  "v1alpha1",
					ServiceType: "vm",
					Spec:        map[string]any{"vcpu": map[string]any{"count": 2}},
				}

				result, err := svc.ServiceType().Create(ctx, req)
				Expect(err).ToNot(HaveOccurred())
				Expect(result).ToNot(BeNil())
				Expect(result.ServiceType).To(Equal("vm"))
			})

			It("should create a service type with 'container'", func() {
				req := &service.CreateServiceTypeRequest{
					ApiVersion:  "v1alpha1",
					ServiceType: "container",
					Spec:        map[string]any{"image": "nginx"},
				}

				result, err := svc.ServiceType().Create(ctx, req)
				Expect(err).ToNot(HaveOccurred())
				Expect(result.ServiceType).To(Equal("container"))
			})

			It("should create a service type with 'cluster'", func() {
				req := &service.CreateServiceTypeRequest{
					ApiVersion:  "v1alpha1",
					ServiceType: "cluster",
					Spec:        map[string]any{"nodes": 3},
				}

				result, err := svc.ServiceType().Create(ctx, req)
				Expect(err).ToNot(HaveOccurred())
				Expect(result.ServiceType).To(Equal("cluster"))
			})

			It("should create a service type with 'database'", func() {
				req := &service.CreateServiceTypeRequest{
					ApiVersion:  "v1alpha1",
					ServiceType: "database",
					Spec:        map[string]any{"engine": "postgres"},
				}

				result, err := svc.ServiceType().Create(ctx, req)
				Expect(err).ToNot(HaveOccurred())
				Expect(result.ServiceType).To(Equal("database"))
			})

			It("should create a service type with 'storage'", func() {
				req := &service.CreateServiceTypeRequest{
					ApiVersion:  "v1alpha1",
					ServiceType: "storage",
					Spec:        map[string]any{"capacity": "100Gi"},
				}

				result, err := svc.ServiceType().Create(ctx, req)
				Expect(err).ToNot(HaveOccurred())
				Expect(result).ToNot(BeNil())
				Expect(result.ServiceType).To(Equal("storage"))
				Expect(result.Spec).To(HaveKey("capacity"))
			})

			It("should create a service type with 'network'", func() {
				req := &service.CreateServiceTypeRequest{
					ApiVersion:  "v1alpha1",
					ServiceType: "network",
					Spec:        map[string]any{"ports": []map[string]any{{"port": 80, "target_port": 8080}}, "routing_level": "network"},
				}

				result, err := svc.ServiceType().Create(ctx, req)
				Expect(err).ToNot(HaveOccurred())
				Expect(result).ToNot(BeNil())
				Expect(result.ServiceType).To(Equal("network"))
				Expect(result.Spec).To(HaveKey("ports"))
				Expect(result.Spec).To(HaveKey("routing_level"))
			})
		})

		Context("with invalid service types", func() {
			It("should reject 'VM' (uppercase)", func() {
				req := &service.CreateServiceTypeRequest{
					ApiVersion:  "v1alpha1",
					ServiceType: "VM",
					Spec:        map[string]any{"vcpu": 2},
				}

				_, err := svc.ServiceType().Create(ctx, req)
				Expect(err).To(Equal(service.ErrInvalidServiceType))
			})

			It("should reject 'db'", func() {
				req := &service.CreateServiceTypeRequest{
					ApiVersion:  "v1alpha1",
					ServiceType: "db",
					Spec:        map[string]any{"engine": "mysql"},
				}

				_, err := svc.ServiceType().Create(ctx, req)
				Expect(err).To(Equal(service.ErrInvalidServiceType))
			})

			It("should reject 'invalid-type'", func() {
				req := &service.CreateServiceTypeRequest{
					ApiVersion:  "v1alpha1",
					ServiceType: "invalid-type",
					Spec:        map[string]any{"foo": "bar"},
				}

				_, err := svc.ServiceType().Create(ctx, req)
				Expect(err).To(Equal(service.ErrInvalidServiceType))
			})
		})

		Context("with ID validation", func() {
			It("should generate UUID when ID is not provided", func() {
				req := &service.CreateServiceTypeRequest{
					ApiVersion:  "v1alpha1",
					ServiceType: "vm",
					Spec:        map[string]any{"vcpu": 2},
				}

				result, err := svc.ServiceType().Create(ctx, req)
				Expect(err).ToNot(HaveOccurred())
				Expect(result.Uid).ToNot(BeNil())
				Expect(*result.Uid).ToNot(BeEmpty())
				Expect(*result.Path).To(Equal("service-types/" + *result.Uid))
			})

			It("should use valid user-provided ID (DNS-1123)", func() {
				userID := "my-service-type"
				req := &service.CreateServiceTypeRequest{
					ID:          &userID,
					ApiVersion:  "v1alpha1",
					ServiceType: "vm",
					Spec:        map[string]any{"vcpu": 2},
				}

				result, err := svc.ServiceType().Create(ctx, req)
				Expect(err).ToNot(HaveOccurred())
				Expect(*result.Uid).To(Equal(userID))

				// Verify via Get
				retrieved, err := svc.ServiceType().Get(ctx, userID)
				Expect(err).ToNot(HaveOccurred())
				Expect(*retrieved.Uid).To(Equal(userID))
			})
		})

		Context("with store errors", func() {
			It("should map ErrServiceTypeIDTaken", func() {
				id := "taken-id"
				req1 := &service.CreateServiceTypeRequest{
					ID:          &id,
					ApiVersion:  "v1alpha1",
					ServiceType: "vm",
					Spec:        map[string]any{"vcpu": 2},
				}
				_, err := svc.ServiceType().Create(ctx, req1)
				Expect(err).ToNot(HaveOccurred())

				req2 := &service.CreateServiceTypeRequest{
					ID:          &id,
					ApiVersion:  "v1alpha1",
					ServiceType: "container",
					Spec:        map[string]any{"image": "nginx"},
				}
				_, err = svc.ServiceType().Create(ctx, req2)
				Expect(err).To(Equal(service.ErrServiceTypeIDTaken))
			})

			It("should map ErrServiceTypeServiceTypeTaken", func() {
				req1 := &service.CreateServiceTypeRequest{
					ApiVersion:  "v1alpha1",
					ServiceType: "vm",
					Spec:        map[string]any{"vcpu": 2},
				}
				_, err := svc.ServiceType().Create(ctx, req1)
				Expect(err).ToNot(HaveOccurred())

				id2 := "another-vm-id"
				req2 := &service.CreateServiceTypeRequest{
					ID:          &id2,
					ApiVersion:  "v1alpha1",
					ServiceType: "vm",
					Spec:        map[string]any{"vcpu": 4},
				}
				_, err = svc.ServiceType().Create(ctx, req2)
				Expect(err).To(Equal(service.ErrServiceTypeNameTaken))
			})
		})

		Context("with metadata", func() {
			It("should handle metadata with labels", func() {
				labels := map[string]string{"env": "prod", "team": "platform"}
				req := &service.CreateServiceTypeRequest{
					ApiVersion:  "v1alpha1",
					ServiceType: "vm",
					Metadata: &struct {
						Labels *map[string]string `json:"labels,omitempty"`
					}{
						Labels: &labels,
					},
					Spec: map[string]any{"vcpu": 2},
				}

				result, err := svc.ServiceType().Create(ctx, req)
				Expect(err).ToNot(HaveOccurred())
				Expect(result.Metadata).ToNot(BeNil())
				Expect(result.Metadata.Labels).ToNot(BeNil())
				Expect(*result.Metadata.Labels).To(HaveKeyWithValue("env", "prod"))

				retrieved, err := svc.ServiceType().Get(ctx, *result.Uid)
				Expect(err).ToNot(HaveOccurred())
				Expect(retrieved.Metadata).ToNot(BeNil())
				Expect(retrieved.Metadata.Labels).ToNot(BeNil())
				Expect(*retrieved.Metadata.Labels).To(HaveKeyWithValue("env", "prod"))
				Expect(*retrieved.Metadata.Labels).To(HaveKeyWithValue("team", "platform"))
			})

			It("should handle nil metadata", func() {
				req := &service.CreateServiceTypeRequest{
					ApiVersion:  "v1alpha1",
					ServiceType: "vm",
					Metadata:    nil,
					Spec:        map[string]any{"vcpu": 2},
				}

				result, err := svc.ServiceType().Create(ctx, req)
				Expect(err).ToNot(HaveOccurred())
				Expect(result.Uid).ToNot(BeNil())
				retrieved, err := svc.ServiceType().Get(ctx, *result.Uid)
				Expect(err).ToNot(HaveOccurred())
				Expect(retrieved.Metadata).To(BeNil())
			})
		})
	})

	Describe("Get", func() {
		It("should retrieve the seeded storage service type", func() {
			err := svc.Seed(ctx)
			Expect(err).ToNot(HaveOccurred())

			result, err := svc.ServiceType().Get(ctx, "storage")
			Expect(err).ToNot(HaveOccurred())
			Expect(result).ToNot(BeNil())
			Expect(result.ServiceType).To(Equal("storage"))
			Expect(*result.Path).To(Equal("service-types/storage"))
			Expect(result.Spec).To(HaveKey("capacity"))
		})

		It("should retrieve a service type", func() {
			createReq := &service.CreateServiceTypeRequest{
				ApiVersion:  "v1alpha1",
				ServiceType: "vm",
				Spec:        map[string]any{"vcpu": 2},
			}
			created, err := svc.ServiceType().Create(ctx, createReq)
			Expect(err).ToNot(HaveOccurred())
			Expect(created.Uid).ToNot(BeNil())

			result, err := svc.ServiceType().Get(ctx, *created.Uid)
			Expect(err).ToNot(HaveOccurred())
			Expect(result).ToNot(BeNil())
			Expect(*result.Uid).To(Equal(*created.Uid))
			Expect(result.ServiceType).To(Equal("vm"))
		})

		It("should map ErrServiceTypeNotFound", func() {
			_, err := svc.ServiceType().Get(ctx, "non-existent")
			Expect(err).To(Equal(service.ErrServiceTypeNotFound))
		})
	})

	Describe("List", func() {
		It("lists seeded service types including storage", func() {
			err := svc.Seed(ctx)
			Expect(err).ToNot(HaveOccurred())

			result, err := svc.ServiceType().List(ctx, &service.ServiceTypeListOptions{})
			Expect(err).ToNot(HaveOccurred())
			Expect(result.ServiceTypes).To(HaveLen(7))

			types := make([]string, len(result.ServiceTypes))
			for i, st := range result.ServiceTypes {
				types[i] = st.ServiceType
			}
			Expect(types).To(ContainElement("storage"))
			Expect(types).To(ContainElement("network"))
		})

		It("should list service types", func() {
			for _, st := range []string{"vm", "container"} {
				_, err := svc.ServiceType().Create(ctx, &service.CreateServiceTypeRequest{
					ApiVersion:  "v1alpha1",
					ServiceType: st,
					Spec:        map[string]any{"x": 1},
				})
				Expect(err).ToNot(HaveOccurred())
			}

			result, err := svc.ServiceType().List(ctx, &service.ServiceTypeListOptions{})
			Expect(err).ToNot(HaveOccurred())
			Expect(result.ServiceTypes).To(HaveLen(2))
		})

		It("should handle empty list", func() {
			result, err := svc.ServiceType().List(ctx, &service.ServiceTypeListOptions{})
			Expect(err).ToNot(HaveOccurred())
			Expect(result.ServiceTypes).To(BeEmpty())
			Expect(result.NextPageToken).To(BeNil())
		})

		It("should paginate with page size and offset token", func() {
			for _, st := range []string{"vm", "container", "cluster", "database"} {
				_, err := svc.ServiceType().Create(ctx, &service.CreateServiceTypeRequest{
					ApiVersion:  "v1alpha1",
					ServiceType: st,
					Spec:        map[string]any{"x": 1},
				})
				Expect(err).ToNot(HaveOccurred())
			}

			pageSize1 := int32(1)
			result1, err := svc.ServiceType().List(ctx, &service.ServiceTypeListOptions{MaxPageSize: &pageSize1})
			Expect(err).ToNot(HaveOccurred())
			Expect(result1.ServiceTypes).To(HaveLen(1))
			Expect(result1.NextPageToken).ToNot(BeNil())

			pageSize2 := int32(2)
			result2, err := svc.ServiceType().List(ctx, &service.ServiceTypeListOptions{PageToken: result1.NextPageToken, MaxPageSize: &pageSize2})
			Expect(err).ToNot(HaveOccurred())
			Expect(result2.ServiceTypes).To(HaveLen(2))
			Expect(result2.NextPageToken).ToNot(BeNil())

			pageSize3 := int32(3)
			result3, err := svc.ServiceType().List(ctx, &service.ServiceTypeListOptions{PageToken: result2.NextPageToken, MaxPageSize: &pageSize3})
			Expect(err).ToNot(HaveOccurred())
			Expect(result3.ServiceTypes).To(HaveLen(1))
			Expect(result3.NextPageToken).To(BeNil())
		})
	})
})
