package service_test

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/dcm-project/control-plane/api/catalog/v1alpha1"
	"github.com/dcm-project/control-plane/internal/catalog/config"
	"github.com/dcm-project/control-plane/internal/catalog/service"
	"github.com/dcm-project/control-plane/internal/catalog/store"
	"github.com/dcm-project/control-plane/internal/catalog/store/model"
	"github.com/dcm-project/control-plane/internal/catalog/testutil"
)

func ensureServiceType(ctx context.Context, str store.Store, id, serviceType string) {
	st := model.ServiceType{
		ID:          id,
		ApiVersion:  "v1alpha1",
		ServiceType: serviceType,
		Spec:        map[string]any{"x": 1},
		Path:        fmt.Sprintf("service-types/%s", id),
	}
	_, err := str.ServiceType().Create(ctx, st)
	if err != nil {
		// May already exist (duplicate id or service_type)
		return
	}
}

func devAppCatalogItemSpec() v1alpha1.CatalogItemSpec {
	requiresOrdersDb := []string{"ordersDb"}
	return v1alpha1.CatalogItemSpec{
		Resources: []v1alpha1.CatalogResource{
			{
				Name:        "ordersDb",
				ServiceType: "database",
				Fields: &[]v1alpha1.FieldConfiguration{
					{Path: "engine", Default: "postgres"},
					{Path: "version", Default: "16"},
				},
			},
			{
				Name:              "app",
				ServiceType:       "container",
				RequiresResources: &requiresOrdersDb,
				Fields: &[]v1alpha1.FieldConfiguration{
					{Path: "image", Default: "registry.example.com/app:1.0"},
				},
			},
		},
	}
}

var _ = Describe("CatalogItem Service", func() {
	var (
		ctx           context.Context
		db            *gorm.DB
		str           store.Store
		svc           service.Service
		serviceTypeVM = "vm"
	)

	BeforeEach(func() {
		ctx = context.Background()
		var err error
		db, err = gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
			Logger: logger.Discard,
		})
		Expect(err).ToNot(HaveOccurred())
		err = db.Exec("PRAGMA foreign_keys = ON").Error
		Expect(err).ToNot(HaveOccurred())
		err = db.AutoMigrate(&model.ServiceType{}, &model.CatalogItem{})
		Expect(err).ToNot(HaveOccurred())
		str = store.NewStore(db, slog.Default())
		svc, err = service.NewService(str, &mockPMClient{}, config.DefaultSeedConfig(), slog.Default())
		Expect(err).ToNot(HaveOccurred())
		// Ensure service types exist for catalog item FK
		ensureServiceType(ctx, str, "vm-st", "vm")
		ensureServiceType(ctx, str, "container-st", "container")
	})

	AfterEach(func() {
		if str != nil {
			Expect(str.Close()).To(Succeed())
		}
	})

	Describe("Create", func() {
		Context("with valid user-provided DNS-1123 ID", func() {
			It("should create a catalog item with the provided ID", func() {
				userID := "my-catalog-item"
				displayName := "Test Catalog Item"
				req := &service.CreateCatalogItemRequest{
					ID:          &userID,
					ApiVersion:  "v1alpha1",
					DisplayName: displayName,
					Spec: testutil.CatalogSpec("vm", []v1alpha1.FieldConfiguration{
						{Path: "spec.vcpu.count", Default: 2},
					}),
				}

				result, err := svc.CatalogItem().Create(ctx, req)
				Expect(err).ToNot(HaveOccurred())
				Expect(result).ToNot(BeNil())
				Expect(*result.Uid).To(Equal(userID))
				Expect(*result.DisplayName).To(Equal(displayName))
				Expect(result.Spec.Resources[0].ServiceType).To(Equal(serviceTypeVM))
				Expect(*result.Spec.Resources[0].Fields).To(HaveLen(1))
			})
		})

		Context("without ID (auto-generate UUID)", func() {
			It("should auto-generate a UUID for the catalog item", func() {
				req := &service.CreateCatalogItemRequest{
					ApiVersion:  "v1alpha1",
					DisplayName: "Auto ID Item",
					Spec: testutil.CatalogSpec("container", []v1alpha1.FieldConfiguration{
						{Path: "spec.image", Default: "nginx"},
					}),
				}

				result, err := svc.CatalogItem().Create(ctx, req)
				Expect(err).ToNot(HaveOccurred())
				Expect(result.Uid).ToNot(BeNil())
				Expect(*result.Uid).To(MatchRegexp(`^[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}$`))
			})
		})

		Context("when store returns duplicate ID error", func() {
			It("should return ErrCatalogItemIDTaken", func() {
				id := "taken-id"
				req1 := &service.CreateCatalogItemRequest{
					ID:          &id,
					ApiVersion:  "v1alpha1",
					DisplayName: "First",
					Spec: testutil.CatalogSpec("vm", []v1alpha1.FieldConfiguration{
						{Path: "spec.vcpu", Default: 2},
					}),
				}
				_, err := svc.CatalogItem().Create(ctx, req1)
				Expect(err).ToNot(HaveOccurred())

				req2 := &service.CreateCatalogItemRequest{
					ID:          &id,
					ApiVersion:  "v1alpha1",
					DisplayName: "Second",
					Spec: testutil.CatalogSpec("container", []v1alpha1.FieldConfiguration{
						{Path: "spec.image", Default: "nginx"},
					}),
				}
				result, err := svc.CatalogItem().Create(ctx, req2)
				Expect(err).To(Equal(service.ErrCatalogItemIDTaken))
				Expect(result).To(BeNil())
			})
		})

		Context("when store returns service type not found error", func() {
			It("should return ErrServiceTypeNotFound", func() {
				req := &service.CreateCatalogItemRequest{
					ApiVersion:  "v1alpha1",
					DisplayName: "Nonexistent Service Type",
					Spec: testutil.CatalogSpec("nonexistent", []v1alpha1.FieldConfiguration{
						{Path: "spec.vcpu", Default: 2},
					}),
				}
				result, err := svc.CatalogItem().Create(ctx, req)
				Expect(err).To(Equal(service.ErrServiceTypeNotFound))
				Expect(result).To(BeNil())
			})
		})
	})

	Describe("List", func() {
		Context("without filters", func() {
			It("should return all catalog items", func() {
				_, err := svc.CatalogItem().Create(ctx, &service.CreateCatalogItemRequest{
					ApiVersion:  "v1alpha1",
					DisplayName: "Item 1",
					Spec:        testutil.CatalogSpecVM([]v1alpha1.FieldConfiguration{{Path: "spec.vcpu", Default: 2}}),
				})
				Expect(err).ToNot(HaveOccurred())
				_, err = svc.CatalogItem().Create(ctx, &service.CreateCatalogItemRequest{
					ApiVersion:  "v1alpha1",
					DisplayName: "Item 2",
					Spec:        testutil.CatalogSpecContainer([]v1alpha1.FieldConfiguration{{Path: "spec.image", Default: "nginx"}}),
				})
				Expect(err).ToNot(HaveOccurred())

				result, err := svc.CatalogItem().List(ctx, service.CatalogItemListOptions{})
				Expect(err).ToNot(HaveOccurred())
				Expect(result.CatalogItems).To(HaveLen(2))
			})
		})

		Context("with service_type filter", func() {
			It("should filter by service_type", func() {
				_, err := svc.CatalogItem().Create(ctx, &service.CreateCatalogItemRequest{
					ApiVersion:  "v1alpha1",
					DisplayName: "VM Item",
					Spec:        testutil.CatalogSpecVM([]v1alpha1.FieldConfiguration{{Path: "spec.vcpu", Default: 2}}),
				})
				Expect(err).ToNot(HaveOccurred())
				_, err = svc.CatalogItem().Create(ctx, &service.CreateCatalogItemRequest{
					ApiVersion:  "v1alpha1",
					DisplayName: "Container Item",
					Spec:        testutil.CatalogSpecContainer([]v1alpha1.FieldConfiguration{{Path: "spec.image", Default: "nginx"}}),
				})
				Expect(err).ToNot(HaveOccurred())

				svcType := "vm"
				result, err := svc.CatalogItem().List(ctx, service.CatalogItemListOptions{ServiceType: &svcType})
				Expect(err).ToNot(HaveOccurred())
				Expect(result.CatalogItems).To(HaveLen(1))
				Expect(result.CatalogItems[0].Spec.Resources[0].ServiceType).To(Equal(serviceTypeVM))
			})
		})

		Context("with pagination options", func() {
			It("should pass pagination parameters and return next page token when more results exist", func() {
				for i := range 6 {
					_, err := svc.CatalogItem().Create(ctx, &service.CreateCatalogItemRequest{
						ApiVersion:  "v1alpha1",
						DisplayName: fmt.Sprintf("Item %d", i),
						Spec:        testutil.CatalogSpec("vm", []v1alpha1.FieldConfiguration{{Path: "spec.vcpu", Default: 2}}),
					})
					Expect(err).ToNot(HaveOccurred())
				}

				maxPageSize := int32(2)
				result, err := svc.CatalogItem().List(ctx, service.CatalogItemListOptions{
					MaxPageSize: &maxPageSize,
				})
				Expect(err).ToNot(HaveOccurred())
				Expect(result.CatalogItems).To(HaveLen(2))
				Expect(result.NextPageToken).ToNot(BeNil())

				maxPageSize = int32(3)
				result, err = svc.CatalogItem().List(ctx, service.CatalogItemListOptions{
					MaxPageSize: &maxPageSize,
					PageToken:   result.NextPageToken,
				})
				Expect(err).ToNot(HaveOccurred())
				Expect(result.CatalogItems).To(HaveLen(3))
				Expect(result.NextPageToken).ToNot(BeNil())

				maxPageSize = int32(4)
				result, err = svc.CatalogItem().List(ctx, service.CatalogItemListOptions{
					MaxPageSize: &maxPageSize,
					PageToken:   result.NextPageToken,
				})
				Expect(err).ToNot(HaveOccurred())
				Expect(result.CatalogItems).To(HaveLen(1))
				Expect(result.NextPageToken).To(BeNil())
			})
		})
	})

	Describe("Get", func() {
		Context("with valid ID", func() {
			It("should return the catalog item", func() {
				created, err := svc.CatalogItem().Create(ctx, &service.CreateCatalogItemRequest{
					ApiVersion:  "v1alpha1",
					DisplayName: "Test Item",
					Spec:        testutil.CatalogSpecVM([]v1alpha1.FieldConfiguration{{Path: "spec.vcpu", Default: 2}}),
				})
				Expect(err).ToNot(HaveOccurred())
				Expect(created.Uid).ToNot(BeNil())

				result, err := svc.CatalogItem().Get(ctx, *created.Uid)
				Expect(err).ToNot(HaveOccurred())
				Expect(result).ToNot(BeNil())
				Expect(*result.Uid).To(Equal(*created.Uid))
				Expect(*result.DisplayName).To(Equal("Test Item"))
			})
		})

		Context("with non-existent ID", func() {
			It("should return ErrCatalogItemNotFound", func() {
				result, err := svc.CatalogItem().Get(ctx, "nonexistent")
				Expect(err).To(Equal(service.ErrCatalogItemNotFound))
				Expect(result).To(BeNil())
			})
		})
	})

	Describe("Update", func() {
		Context("updating display_name only", func() {
			It("should update the display_name", func() {
				id := "item1"
				_, err := svc.CatalogItem().Create(ctx, &service.CreateCatalogItemRequest{
					ID:          &id,
					ApiVersion:  "v1alpha1",
					DisplayName: "Old Name",
					Spec:        testutil.CatalogSpecVM([]v1alpha1.FieldConfiguration{{Path: "spec.vcpu", Default: 2}}),
				})
				Expect(err).ToNot(HaveOccurred())

				newDisplayName := "Updated Name"
				req := &service.UpdateCatalogItemRequest{
					DisplayName: &newDisplayName,
				}

				result, err := svc.CatalogItem().Update(ctx, "item1", req)
				Expect(err).ToNot(HaveOccurred())
				Expect(result).ToNot(BeNil())
				Expect(*result.DisplayName).To(Equal(newDisplayName))
			})
		})

		Context("updating spec.fields only", func() {
			It("should update the spec fields", func() {
				id := "item1"
				_, err := svc.CatalogItem().Create(ctx, &service.CreateCatalogItemRequest{
					ID:          &id,
					ApiVersion:  "v1alpha1",
					DisplayName: "Name",
					Spec:        testutil.CatalogSpecVM([]v1alpha1.FieldConfiguration{{Path: "spec.vcpu", Default: 2}}),
				})
				Expect(err).ToNot(HaveOccurred())

				newSpec := testutil.PtrCatalogSpec("vm", []v1alpha1.FieldConfiguration{
					{Path: "spec.vcpu", Default: 4},
					{Path: "spec.memory", Default: "8GB"},
				})
				req := &service.UpdateCatalogItemRequest{
					Spec: newSpec,
				}

				result, err := svc.CatalogItem().Update(ctx, "item1", req)
				Expect(err).ToNot(HaveOccurred())
				Expect(result).ToNot(BeNil())
				Expect(*result.Spec.Resources[0].Fields).To(HaveLen(2))
			})
		})

		Context("attempting to update spec.service_type (immutable)", func() {
			It("should return ErrImmutableSpecStructureUpdate", func() {
				id := "item1"
				_, err := svc.CatalogItem().Create(ctx, &service.CreateCatalogItemRequest{
					ID:          &id,
					ApiVersion:  "v1alpha1",
					DisplayName: "Name",
					Spec:        testutil.CatalogSpecVM([]v1alpha1.FieldConfiguration{{Path: "spec.vcpu", Default: 2}}),
				})
				Expect(err).ToNot(HaveOccurred())

				newSpec := testutil.PtrCatalogSpec("container", []v1alpha1.FieldConfiguration{
					{Path: "spec.image", Default: "nginx"},
				})
				req := &service.UpdateCatalogItemRequest{
					Spec: newSpec,
				}

				result, err := svc.CatalogItem().Update(ctx, "item1", req)
				Expect(err).To(Equal(service.ErrImmutableSpecStructureUpdate))
				Expect(result).To(BeNil())
			})
		})

		Context("with non-existent item", func() {
			It("should return ErrCatalogItemNotFound", func() {
				newName := "New Name"
				req := &service.UpdateCatalogItemRequest{
					DisplayName: &newName,
				}

				result, err := svc.CatalogItem().Update(ctx, "nonexistent", req)
				Expect(err).To(Equal(service.ErrCatalogItemNotFound))
				Expect(result).To(BeNil())
			})
		})
	})

	Describe("Create with cyclic depends_on", func() {
		It("should reject fields with cyclic depends_on references", func() {
			editable := true
			req := &service.CreateCatalogItemRequest{
				ApiVersion:  "v1alpha1",
				DisplayName: "Cyclic DependsOn",
				Spec: testutil.CatalogSpec("vm", []v1alpha1.FieldConfiguration{
					{
						Path:     "spec.vcpu.count",
						Default:  float64(2),
						Editable: &editable,
						DependsOn: &v1alpha1.FieldConfigurationDependsOn{
							Path: "spec.memory.size_gb",
							AllowedValues: map[string][]any{
								"4": {float64(2), float64(4)},
							},
						},
					},
					{
						Path:     "spec.memory.size_gb",
						Default:  float64(4),
						Editable: &editable,
						DependsOn: &v1alpha1.FieldConfigurationDependsOn{
							Path: "spec.vcpu.count",
							AllowedValues: map[string][]any{
								"2": {float64(4), float64(8)},
							},
						},
					},
				}),
			}

			result, err := svc.CatalogItem().Create(ctx, req)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("cycle"))
			Expect(result).To(BeNil())
		})

		It("should reject a three-field cycle (A -> B -> C -> A)", func() {
			editable := true
			req := &service.CreateCatalogItemRequest{
				ApiVersion:  "v1alpha1",
				DisplayName: "Three-Field Cycle",
				Spec: testutil.CatalogSpec("vm", []v1alpha1.FieldConfiguration{
					{
						Path:     "spec.vcpu.count",
						Default:  float64(2),
						Editable: &editable,
						DependsOn: &v1alpha1.FieldConfigurationDependsOn{
							Path: "spec.memory.size_gb",
							AllowedValues: map[string][]any{
								"4": {float64(2), float64(4)},
							},
						},
					},
					{
						Path:     "spec.memory.size_gb",
						Default:  float64(4),
						Editable: &editable,
						DependsOn: &v1alpha1.FieldConfigurationDependsOn{
							Path: "spec.disk.size_gb",
							AllowedValues: map[string][]any{
								"100": {float64(4), float64(8)},
							},
						},
					},
					{
						Path:     "spec.disk.size_gb",
						Default:  float64(100),
						Editable: &editable,
						DependsOn: &v1alpha1.FieldConfigurationDependsOn{
							Path: "spec.vcpu.count",
							AllowedValues: map[string][]any{
								"2": {float64(100), float64(200)},
							},
						},
					},
				}),
			}

			result, err := svc.CatalogItem().Create(ctx, req)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("cycle"))
			Expect(result).To(BeNil())
		})

		It("should accept fields without cyclic depends_on references", func() {
			editable := true
			req := &service.CreateCatalogItemRequest{
				ApiVersion:  "v1alpha1",
				DisplayName: "Valid DependsOn",
				Spec: testutil.CatalogSpec("vm", []v1alpha1.FieldConfiguration{
					{
						Path:     "spec.vcpu.count",
						Default:  float64(2),
						Editable: &editable,
					},
					{
						Path:     "spec.memory.size_gb",
						Default:  float64(4),
						Editable: &editable,
						DependsOn: &v1alpha1.FieldConfigurationDependsOn{
							Path: "spec.vcpu.count",
							AllowedValues: map[string][]any{
								"2": {float64(4), float64(8)},
								"4": {float64(8), float64(16)},
							},
						},
					},
				}),
			}

			result, err := svc.CatalogItem().Create(ctx, req)
			Expect(err).ToNot(HaveOccurred())
			Expect(result).ToNot(BeNil())
		})
	})

	Describe("Create with invalid depends_on path", func() {
		It("should reject when depends_on path does not reference an existing field", func() {
			editable := true
			req := &service.CreateCatalogItemRequest{
				ApiVersion:  "v1alpha1",
				DisplayName: "Invalid DependsOn Path",
				Spec: testutil.CatalogSpec("vm", []v1alpha1.FieldConfiguration{
					{
						Path:     "spec.memory.size_gb",
						Default:  float64(4),
						Editable: &editable,
						DependsOn: &v1alpha1.FieldConfigurationDependsOn{
							Path: "spec.region",
							AllowedValues: map[string][]any{
								"us-central1": {float64(4), float64(8)},
							},
						},
					},
				}),
			}

			result, err := svc.CatalogItem().Create(ctx, req)
			Expect(err).To(HaveOccurred())
			Expect(errors.Is(err, service.ErrDependsOnPathNotFound)).To(BeTrue())
			Expect(err.Error()).To(ContainSubstring("spec.region"))
			Expect(err.Error()).To(ContainSubstring("not found"))
			Expect(result).To(BeNil())
		})
	})

	Describe("Update with cyclic depends_on", func() {
		It("should reject update that introduces cyclic depends_on", func() {
			id := "item-cycle-update"
			editable := true
			_, err := svc.CatalogItem().Create(ctx, &service.CreateCatalogItemRequest{
				ID:          &id,
				ApiVersion:  "v1alpha1",
				DisplayName: "No Cycle",
				Spec: testutil.CatalogSpec("vm", []v1alpha1.FieldConfiguration{
					{Path: "spec.vcpu.count", Default: float64(2), Editable: &editable},
					{Path: "spec.memory.size_gb", Default: float64(4), Editable: &editable},
				}),
			})
			Expect(err).ToNot(HaveOccurred())

			updateSpec := testutil.PtrCatalogSpec("vm", []v1alpha1.FieldConfiguration{
				{
					Path:     "spec.vcpu.count",
					Default:  float64(2),
					Editable: &editable,
					DependsOn: &v1alpha1.FieldConfigurationDependsOn{
						Path: "spec.memory.size_gb",
						AllowedValues: map[string][]any{
							"4": {float64(2), float64(4)},
						},
					},
				},
				{
					Path:     "spec.memory.size_gb",
					Default:  float64(4),
					Editable: &editable,
					DependsOn: &v1alpha1.FieldConfigurationDependsOn{
						Path: "spec.vcpu.count",
						AllowedValues: map[string][]any{
							"2": {float64(4), float64(8)},
						},
					},
				},
			})

			result, err := svc.CatalogItem().Update(ctx, id, &service.UpdateCatalogItemRequest{
				Spec: updateSpec,
			})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("cycle"))
			Expect(result).To(BeNil())
		})
	})

	Describe("Create multi-resource catalog item", func() {
		BeforeEach(func() {
			ensureServiceType(ctx, str, "db-st", "database")
		})

		It("should create a multi-resource catalog item with resources", func() {
			id := "dev-app"
			req := &service.CreateCatalogItemRequest{
				ID:          &id,
				ApiVersion:  "v1alpha1",
				DisplayName: "Dev Application",
				Spec:        devAppCatalogItemSpec(),
			}

			result, err := svc.CatalogItem().Create(ctx, req)
			Expect(err).ToNot(HaveOccurred())
			Expect(result).ToNot(BeNil())
			Expect(*result.Uid).To(Equal(id))
			Expect(result.Spec).ToNot(BeNil())
			Expect(result.Spec.Resources).ToNot(BeNil())
			Expect(result.Spec.Resources).To(HaveLen(2))
			Expect((result.Spec.Resources)[0].Name).To(Equal("ordersDb"))
			Expect((result.Spec.Resources)[1].RequiresResources).ToNot(BeNil())
			Expect(*(result.Spec.Resources)[1].RequiresResources).To(Equal([]string{"ordersDb"}))
		})

		It("should round-trip multi-resource spec on get", func() {
			id := "dev-app-get"
			_, err := svc.CatalogItem().Create(ctx, &service.CreateCatalogItemRequest{
				ID:          &id,
				ApiVersion:  "v1alpha1",
				DisplayName: "Dev Application",
				Spec:        devAppCatalogItemSpec(),
			})
			Expect(err).ToNot(HaveOccurred())

			result, err := svc.CatalogItem().Get(ctx, id)
			Expect(err).ToNot(HaveOccurred())
			Expect(result.Spec.Resources).ToNot(BeNil())
			Expect(result.Spec.Resources).To(HaveLen(2))
		})

		It("should reject empty resources", func() {
			result, err := svc.CatalogItem().Create(ctx, &service.CreateCatalogItemRequest{
				ApiVersion:  "v1alpha1",
				DisplayName: "Empty resources",
				Spec:        v1alpha1.CatalogItemSpec{Resources: []v1alpha1.CatalogResource{}},
			})
			Expect(err).To(HaveOccurred())
			Expect(errors.Is(err, service.ErrCatalogItemSpecConflict)).To(BeTrue())
			Expect(result).To(BeNil())
		})

		It("should reject duplicate resource names", func() {
			spec := v1alpha1.CatalogItemSpec{
				Resources: []v1alpha1.CatalogResource{
					{Name: "ordersDb", ServiceType: "database"},
					{Name: "ordersDb", ServiceType: "container"},
				},
			}

			result, err := svc.CatalogItem().Create(ctx, &service.CreateCatalogItemRequest{
				ApiVersion:  "v1alpha1",
				DisplayName: "Duplicate names",
				Spec:        spec,
			})
			Expect(err).To(HaveOccurred())
			Expect(errors.Is(err, service.ErrCatalogItemResourceNameTaken)).To(BeTrue())
			Expect(result).To(BeNil())
		})

		It("should reject duplicate field paths within a resource", func() {
			spec := v1alpha1.CatalogItemSpec{
				Resources: []v1alpha1.CatalogResource{
					{
						Name:        "net",
						ServiceType: "vm",
						Fields: &[]v1alpha1.FieldConfiguration{
							{Path: "metadata.name", Default: "a"},
							{Path: "metadata.name", Default: "b"},
						},
					},
				},
			}

			result, err := svc.CatalogItem().Create(ctx, &service.CreateCatalogItemRequest{
				ApiVersion:  "v1alpha1",
				DisplayName: "Duplicate field paths",
				Spec:        spec,
			})
			Expect(err).To(HaveOccurred())
			Expect(errors.Is(err, service.ErrCatalogItemFieldPathTaken)).To(BeTrue())
			Expect(err.Error()).To(ContainSubstring("metadata.name"))
			Expect(result).To(BeNil())
		})

		It("should reject unknown requires_resources reference", func() {
			requiresMissing := []string{"missing"}
			spec := v1alpha1.CatalogItemSpec{
				Resources: []v1alpha1.CatalogResource{
					{
						Name:              "app",
						ServiceType:       "container",
						RequiresResources: &requiresMissing,
					},
				},
			}

			result, err := svc.CatalogItem().Create(ctx, &service.CreateCatalogItemRequest{
				ApiVersion:  "v1alpha1",
				DisplayName: "Bad requires",
				Spec:        spec,
			})
			Expect(err).To(HaveOccurred())
			Expect(errors.Is(err, service.ErrCatalogItemRequiresResourceNotFound)).To(BeTrue())
			Expect(result).To(BeNil())
		})

		It("should reject cyclic requires_resources", func() {
			requiresApp := []string{"app"}
			requiresDb := []string{"ordersDb"}
			spec := v1alpha1.CatalogItemSpec{
				Resources: []v1alpha1.CatalogResource{
					{Name: "ordersDb", ServiceType: "database", RequiresResources: &requiresApp},
					{Name: "app", ServiceType: "container", RequiresResources: &requiresDb},
				},
			}

			result, err := svc.CatalogItem().Create(ctx, &service.CreateCatalogItemRequest{
				ApiVersion:  "v1alpha1",
				DisplayName: "Cycle",
				Spec:        spec,
			})
			Expect(err).To(HaveOccurred())
			Expect(errors.Is(err, service.ErrCatalogItemRequiresCycle)).To(BeTrue())
			Expect(result).To(BeNil())
		})

		It("should reject resource with unknown service type", func() {
			spec := v1alpha1.CatalogItemSpec{
				Resources: []v1alpha1.CatalogResource{
					{Name: "ordersDb", ServiceType: "nonexistent"},
				},
			}

			result, err := svc.CatalogItem().Create(ctx, &service.CreateCatalogItemRequest{
				ApiVersion:  "v1alpha1",
				DisplayName: "Bad service type",
				Spec:        spec,
			})
			Expect(err).To(Equal(service.ErrServiceTypeNotFound))
			Expect(result).To(BeNil())
		})

		It("should reject cyclic depends_on within a resource fields", func() {
			editable := true
			spec := v1alpha1.CatalogItemSpec{
				Resources: []v1alpha1.CatalogResource{
					{
						Name:        "ordersDb",
						ServiceType: "database",
						Fields: &[]v1alpha1.FieldConfiguration{
							{
								Path:     "version",
								Default:  "16",
								Editable: &editable,
								DependsOn: &v1alpha1.FieldConfigurationDependsOn{
									Path: "engine",
									AllowedValues: map[string][]any{
										"postgres": {"14", "16"},
									},
								},
							},
							{
								Path:     "engine",
								Default:  "postgres",
								Editable: &editable,
								DependsOn: &v1alpha1.FieldConfigurationDependsOn{
									Path: "version",
									AllowedValues: map[string][]any{
										"16": {"postgres"},
									},
								},
							},
						},
					},
				},
			}

			result, err := svc.CatalogItem().Create(ctx, &service.CreateCatalogItemRequest{
				ApiVersion:  "v1alpha1",
				DisplayName: "Field cycle",
				Spec:        spec,
			})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("cycle"))
			Expect(result).To(BeNil())
		})
	})

	Describe("Update multi-resource catalog item", func() {
		BeforeEach(func() {
			ensureServiceType(ctx, str, "db-st", "database")
			id := "dev-app-update"
			_, err := svc.CatalogItem().Create(ctx, &service.CreateCatalogItemRequest{
				ID:          &id,
				ApiVersion:  "v1alpha1",
				DisplayName: "Dev Application",
				Spec:        devAppCatalogItemSpec(),
			})
			Expect(err).ToNot(HaveOccurred())
		})

		It("should update field defaults within a resource", func() {
			spec := devAppCatalogItemSpec()
			(spec.Resources)[0].Fields = &[]v1alpha1.FieldConfiguration{
				{Path: "engine", Default: "mysql"},
				{Path: "version", Default: "8.0"},
			}

			result, err := svc.CatalogItem().Update(ctx, "dev-app-update", &service.UpdateCatalogItemRequest{
				Spec: &spec,
			})
			Expect(err).ToNot(HaveOccurred())
			Expect(result).ToNot(BeNil())
			Expect(result.Spec.Resources).To(HaveLen(2))
			Expect(*(result.Spec.Resources)[0].Fields).To(HaveLen(2))
			Expect((*(result.Spec.Resources)[0].Fields)[0].Default).To(Equal("mysql"))
		})

		It("should reject changing resource name", func() {
			spec := devAppCatalogItemSpec()
			(spec.Resources)[0].Name = "renamedDb"

			result, err := svc.CatalogItem().Update(ctx, "dev-app-update", &service.UpdateCatalogItemRequest{
				Spec: &spec,
			})
			Expect(err).To(Equal(service.ErrImmutableSpecStructureUpdate))
			Expect(result).To(BeNil())
		})

		It("should reject changing resource service type", func() {
			spec := devAppCatalogItemSpec()
			(spec.Resources)[0].ServiceType = "vm"

			result, err := svc.CatalogItem().Update(ctx, "dev-app-update", &service.UpdateCatalogItemRequest{
				Spec: &spec,
			})
			Expect(err).To(Equal(service.ErrImmutableSpecStructureUpdate))
			Expect(result).To(BeNil())
		})

		It("should reject changing requires_resources", func() {
			spec := devAppCatalogItemSpec()
			empty := []string{}
			(spec.Resources)[1].RequiresResources = &empty

			result, err := svc.CatalogItem().Update(ctx, "dev-app-update", &service.UpdateCatalogItemRequest{
				Spec: &spec,
			})
			Expect(err).To(Equal(service.ErrImmutableSpecStructureUpdate))
			Expect(result).To(BeNil())
		})
	})

	Describe("List catalog items by resource service type", func() {
		BeforeEach(func() {
			ensureServiceType(ctx, str, "db-st", "database")
			ensureServiceType(ctx, str, "ctr-st", "container")
		})

		It("returns items where any resource matches the filter", func() {
			_, err := svc.CatalogItem().Create(ctx, &service.CreateCatalogItemRequest{
				ApiVersion:  "v1alpha1",
				DisplayName: "Single-resource VM",
				Spec:        testutil.CatalogSpecVM([]v1alpha1.FieldConfiguration{{Path: "spec.vcpu", Default: 2}}),
			})
			Expect(err).ToNot(HaveOccurred())
			_, err = svc.CatalogItem().Create(ctx, &service.CreateCatalogItemRequest{
				ApiVersion:  "v1alpha1",
				DisplayName: "Dev Application",
				Spec:        devAppCatalogItemSpec(),
			})
			Expect(err).ToNot(HaveOccurred())

			vmFilter := "vm"
			result, err := svc.CatalogItem().List(ctx, service.CatalogItemListOptions{
				ServiceType: &vmFilter,
			})
			Expect(err).ToNot(HaveOccurred())
			Expect(result.CatalogItems).To(HaveLen(1))
			Expect(*result.CatalogItems[0].DisplayName).To(Equal("Single-resource VM"))

			dbFilter := "database"
			result, err = svc.CatalogItem().List(ctx, service.CatalogItemListOptions{
				ServiceType: &dbFilter,
			})
			Expect(err).ToNot(HaveOccurred())
			Expect(result.CatalogItems).To(HaveLen(1))
			Expect(*result.CatalogItems[0].DisplayName).To(Equal("Dev Application"))

			containerFilter := "container"
			result, err = svc.CatalogItem().List(ctx, service.CatalogItemListOptions{
				ServiceType: &containerFilter,
			})
			Expect(err).ToNot(HaveOccurred())
			Expect(result.CatalogItems).To(HaveLen(1))
			Expect(*result.CatalogItems[0].DisplayName).To(Equal("Dev Application"))
		})
	})

	Describe("Delete", func() {
		Context("with existing item", func() {
			It("should delete the catalog item", func() {
				id := "item1"
				_, err := svc.CatalogItem().Create(ctx, &service.CreateCatalogItemRequest{
					ID:          &id,
					ApiVersion:  "v1alpha1",
					DisplayName: "To Delete",
					Spec:        testutil.CatalogSpecVM([]v1alpha1.FieldConfiguration{{Path: "spec.vcpu", Default: 2}}),
				})
				Expect(err).ToNot(HaveOccurred())

				err = svc.CatalogItem().Delete(ctx, "item1")
				Expect(err).ToNot(HaveOccurred())

				_, err = svc.CatalogItem().Get(ctx, "item1")
				Expect(err).To(Equal(service.ErrCatalogItemNotFound))
			})
		})

		Context("with non-existent item", func() {
			It("should return ErrCatalogItemNotFound", func() {
				err := svc.CatalogItem().Delete(ctx, "nonexistent")
				Expect(err).To(Equal(service.ErrCatalogItemNotFound))
			})
		})
	})
})
