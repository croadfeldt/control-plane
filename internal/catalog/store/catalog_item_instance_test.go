package store_test

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/dcm-project/control-plane/internal/catalog/store"
	"github.com/dcm-project/control-plane/internal/catalog/store/model"
	"github.com/dcm-project/control-plane/internal/catalog/testutil"
)

var _ = Describe("CatalogItemInstance Store", func() {
	var (
		db                       *gorm.DB
		catalogItemInstanceStore store.CatalogItemInstanceStore
		catalogItemStore         store.CatalogItemStore
		serviceTypeStore         store.ServiceTypeStore
		createTestServiceType    func(id, serviceType string)
		createTestCatalogItem    func(id, serviceType string)
	)

	BeforeEach(func() {
		// Create in-memory SQLite database
		var err error
		db, err = gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
			Logger: logger.Discard,
		})
		Expect(err).ToNot(HaveOccurred())

		// Enable foreign key constraints in SQLite
		err = db.Exec("PRAGMA foreign_keys = ON").Error
		Expect(err).ToNot(HaveOccurred())

		// Auto-migrate all related models to create foreign key constraints
		err = db.AutoMigrate(&model.ServiceType{}, &model.CatalogItem{}, &model.CatalogItemInstance{})
		Expect(err).ToNot(HaveOccurred())

		catalogItemInstanceStore = store.NewCatalogItemInstanceStore(db, slog.Default())
		catalogItemStore = store.NewCatalogItemStore(db, slog.Default())
		serviceTypeStore = store.NewServiceTypeStore(db, slog.Default())

		// Helper function to create prerequisite ServiceTypes
		createTestServiceType = func(id, serviceType string) {
			st := model.ServiceType{
				ID:          id,
				ApiVersion:  "v1alpha1",
				ServiceType: serviceType,
				Spec:        map[string]any{},
				Path:        fmt.Sprintf("service-types/%s", id),
			}
			_, err := serviceTypeStore.Create(context.Background(), st)
			Expect(err).ToNot(HaveOccurred())
		}

		// Helper function to create prerequisite CatalogItems
		createTestCatalogItem = func(id, serviceType string) {
			ci := model.CatalogItem{
				ID:          id,
				ApiVersion:  "v1alpha1",
				DisplayName: fmt.Sprintf("Test %s", id),
				Spec:        testutil.ModelCatalogSpec(serviceType, []model.FieldConfiguration{}),
				Path:        fmt.Sprintf("catalog-items/%s", id),
			}
			_, err := catalogItemStore.Create(context.Background(), ci)
			Expect(err).ToNot(HaveOccurred())
		}
	})

	AfterEach(func() {
		sqlDB, err := db.DB()
		Expect(err).ToNot(HaveOccurred())
		_ = sqlDB.Close()
	})

	Describe("Create", func() {
		It("should create a new catalog item instance successfully", func() {
			// Create prerequisites
			createTestServiceType("vm-st", "vm")
			createTestCatalogItem("small-vm", "vm")

			cii := model.CatalogItemInstance{
				ID:          "my-vm-instance",
				ApiVersion:  "v1alpha1",
				DisplayName: "My VM",
				Spec: model.CatalogItemInstanceSpec{
					CatalogItemId: "small-vm",
					UserValues: []model.UserValue{
						{
							Path:  "spec.vcpu.count",
							Value: 4,
						},
					},
				},
				Path: "catalog-item-instances/my-vm-instance",
			}

			created, err := catalogItemInstanceStore.Create(context.Background(), cii)
			Expect(err).ToNot(HaveOccurred())
			Expect(created).ToNot(BeNil())

			// Verify it was created
			retrieved, err := catalogItemInstanceStore.Get(context.Background(), created.ID)
			Expect(err).ToNot(HaveOccurred())
			Expect(retrieved.ID).To(Equal(created.ID))
			Expect(retrieved.DisplayName).To(Equal(created.DisplayName))
			Expect(retrieved.Spec.CatalogItemId).To(Equal(created.Spec.CatalogItemId))
			Expect(retrieved.SpecCatalogItemId).To(Equal(created.SpecCatalogItemId))
			Expect(retrieved.RunID).To(Equal(created.RunID))
		})

		It("should return error when creating duplicate ID", func() {
			// Create prerequisites
			createTestServiceType("vm-st-dup", "vm")
			createTestCatalogItem("small-vm-dup", "vm")

			cii := model.CatalogItemInstance{
				ID:          "duplicate-cii",
				ApiVersion:  "v1alpha1",
				DisplayName: "Original",
				Spec: model.CatalogItemInstanceSpec{
					CatalogItemId: "small-vm-dup",
					UserValues:    []model.UserValue{},
				},
				Path: "catalog-item-instances/duplicate-cii",
			}

			_, err := catalogItemInstanceStore.Create(context.Background(), cii)
			Expect(err).ToNot(HaveOccurred())

			// Try to create again with same ID
			cii2 := model.CatalogItemInstance{
				ID:          "duplicate-cii",
				ApiVersion:  "v1alpha1",
				DisplayName: "Duplicate",
				Spec: model.CatalogItemInstanceSpec{
					CatalogItemId: "small-vm-dup",
					UserValues:    []model.UserValue{},
				},
				Path: "catalog-item-instances/duplicate-cii",
			}

			created2, err := catalogItemInstanceStore.Create(context.Background(), cii2)
			Expect(created2).To(BeNil())
			Expect(err).To(Equal(store.ErrCatalogItemInstanceIDTaken))
		})

		It("should return error when creating with non-existent catalog item", func() {
			cii := model.CatalogItemInstance{
				ID:          "invalid-ci-cii",
				ApiVersion:  "v1alpha1",
				DisplayName: "Invalid Catalog Item",
				Spec: model.CatalogItemInstanceSpec{
					CatalogItemId: "non-existent-catalog-item",
					UserValues:    []model.UserValue{},
				},
				Path: "catalog-item-instances/invalid-ci-cii",
			}

			_, err := catalogItemInstanceStore.Create(context.Background(), cii)
			Expect(err).To(Equal(store.ErrCatalogItemNotFoundRef))
		})

		It("should create instance with valid catalog item", func() {
			// Create prerequisites
			createTestServiceType("vm-st-valid", "vm")
			createTestCatalogItem("valid-ci", "vm")

			cii := model.CatalogItemInstance{
				ID:          "valid-cii",
				ApiVersion:  "v1alpha1",
				DisplayName: "Valid Instance",
				Spec: model.CatalogItemInstanceSpec{
					CatalogItemId: "valid-ci",
					UserValues:    []model.UserValue{},
				},
				Path: "catalog-item-instances/valid-cii",
			}

			_, err := catalogItemInstanceStore.Create(context.Background(), cii)
			Expect(err).ToNot(HaveOccurred())
		})
	})

	Describe("Get", func() {
		It("should retrieve an existing catalog item instance", func() {
			// Create prerequisites
			createTestServiceType("vm-st-get", "vm")
			createTestCatalogItem("small-vm-get", "vm")

			cii := model.CatalogItemInstance{
				ID:          "get-test-cii",
				ApiVersion:  "v1alpha1",
				DisplayName: "Test Instance",
				Spec: model.CatalogItemInstanceSpec{
					CatalogItemId: "small-vm-get",
					UserValues: []model.UserValue{
						{Path: "spec.vcpu.count", Value: 2},
					},
				},
				Path: "catalog-item-instances/get-test-cii",
			}

			created, err := catalogItemInstanceStore.Create(context.Background(), cii)
			Expect(err).ToNot(HaveOccurred())

			retrieved, err := catalogItemInstanceStore.Get(context.Background(), created.ID)
			Expect(err).ToNot(HaveOccurred())
			Expect(retrieved.ID).To(Equal(created.ID))
			Expect(retrieved.Spec.CatalogItemId).To(Equal("small-vm-get"))
			Expect(retrieved.RunID).To(Equal(created.RunID))
		})

		It("should return error for non-existent catalog item instance", func() {
			_, err := catalogItemInstanceStore.Get(context.Background(), "non-existent")
			Expect(err).To(Equal(store.ErrCatalogItemInstanceNotFound))
		})
	})

	Describe("Delete", func() {
		It("should delete an existing catalog item instance", func() {
			// Create prerequisites
			createTestServiceType("vm-st-del", "vm")
			createTestCatalogItem("small-vm-del", "vm")

			cii := model.CatalogItemInstance{
				ID:          "delete-test-cii",
				ApiVersion:  "v1alpha1",
				DisplayName: "To Delete",
				Spec: model.CatalogItemInstanceSpec{
					CatalogItemId: "small-vm-del",
					UserValues:    []model.UserValue{},
				},
				Path: "catalog-item-instances/delete-test-cii",
			}

			created, err := catalogItemInstanceStore.Create(context.Background(), cii)
			Expect(err).ToNot(HaveOccurred())

			err = catalogItemInstanceStore.Delete(context.Background(), created.ID)
			Expect(err).ToNot(HaveOccurred())

			// Verify deletion
			_, err = catalogItemInstanceStore.Get(context.Background(), created.ID)
			Expect(err).To(Equal(store.ErrCatalogItemInstanceNotFound))
		})

		It("should return error when deleting non-existent catalog item instance", func() {
			err := catalogItemInstanceStore.Delete(context.Background(), "non-existent")
			Expect(err).To(Equal(store.ErrCatalogItemInstanceNotFound))
		})
	})

	Describe("UpdateRunID", func() {
		It("should update run_id when expected value matches", func() {
			createTestServiceType("vm-st-upd", "vm")
			createTestCatalogItem("small-vm-upd", "vm")

			cii := model.CatalogItemInstance{
				ID:          "upd-res-cii",
				ApiVersion:  "v1alpha1",
				DisplayName: "Update RunID",
				Spec: model.CatalogItemInstanceSpec{
					CatalogItemId: "small-vm-upd",
					UserValues:    []model.UserValue{},
				},
				RunID: "old-run-id",
				Path:  "catalog-item-instances/upd-res-cii",
			}

			_, err := catalogItemInstanceStore.Create(context.Background(), cii)
			Expect(err).ToNot(HaveOccurred())

			updated, err := catalogItemInstanceStore.UpdateRunID(context.Background(), "upd-res-cii", "old-run-id", "new-run-id")
			Expect(err).ToNot(HaveOccurred())
			Expect(updated).ToNot(BeNil())
			Expect(updated.RunID).To(Equal("new-run-id"))

			// Verify persisted
			retrieved, err := catalogItemInstanceStore.Get(context.Background(), "upd-res-cii")
			Expect(err).ToNot(HaveOccurred())
			Expect(retrieved.RunID).To(Equal("new-run-id"))
			Expect(retrieved.DisplayName).To(Equal("Update RunID"))
		})

		It("should return conflict when expected run_id does not match", func() {
			createTestServiceType("vm-st-cas", "vm")
			createTestCatalogItem("small-vm-cas", "vm")

			cii := model.CatalogItemInstance{
				ID:          "cas-conflict-cii",
				ApiVersion:  "v1alpha1",
				DisplayName: "CAS Conflict",
				Spec: model.CatalogItemInstanceSpec{
					CatalogItemId: "small-vm-cas",
					UserValues:    []model.UserValue{},
				},
				RunID: "current-run-id",
				Path:  "catalog-item-instances/cas-conflict-cii",
			}

			_, err := catalogItemInstanceStore.Create(context.Background(), cii)
			Expect(err).ToNot(HaveOccurred())

			_, err = catalogItemInstanceStore.UpdateRunID(context.Background(), "cas-conflict-cii", "stale-run-id", "new-run-id")
			Expect(err).To(Equal(store.ErrCatalogItemInstanceConflict))

			// Verify run_id unchanged
			retrieved, err := catalogItemInstanceStore.Get(context.Background(), "cas-conflict-cii")
			Expect(err).ToNot(HaveOccurred())
			Expect(retrieved.RunID).To(Equal("current-run-id"))
		})

		It("should return not found for non-existent instance", func() {
			_, err := catalogItemInstanceStore.UpdateRunID(context.Background(), "non-existent", "old-id", "new-id")
			Expect(err).To(Equal(store.ErrCatalogItemInstanceNotFound))
		})
	})

	Describe("List", func() {
		It("should return empty list when no catalog item instances exist", func() {
			results, err := catalogItemInstanceStore.List(context.Background(), &store.CatalogItemInstanceListOptions{
				PageSize: 100,
			})
			Expect(err).ToNot(HaveOccurred())
			Expect(results.CatalogItemInstances).To(BeEmpty())
			Expect(results.NextPageToken).To(BeNil())
		})

		It("should list all catalog item instances", func() {
			// Create prerequisites
			createTestServiceType("vm-st-list", "vm")
			createTestCatalogItem("small-vm-list", "vm")

			// Create multiple catalog item instances
			for i := 1; i <= 3; i++ {
				cii := model.CatalogItemInstance{
					ID:          fmt.Sprintf("cii-%d", i),
					ApiVersion:  "v1alpha1",
					DisplayName: fmt.Sprintf("Instance %d", i),
					Spec: model.CatalogItemInstanceSpec{
						CatalogItemId: "small-vm-list",
						UserValues:    []model.UserValue{},
					},
					Path: fmt.Sprintf("catalog-item-instances/cii-%d", i),
				}
				time.Sleep(time.Millisecond)
				_, err := catalogItemInstanceStore.Create(context.Background(), cii)
				Expect(err).ToNot(HaveOccurred())
			}

			results, err := catalogItemInstanceStore.List(context.Background(), &store.CatalogItemInstanceListOptions{
				PageSize: 100,
			})
			Expect(err).ToNot(HaveOccurred())
			Expect(results.CatalogItemInstances).To(HaveLen(3))
			Expect(results.NextPageToken).To(BeNil())
		})

		It("should filter by catalog item ID", func() {
			// Create prerequisites
			createTestServiceType("vm-st-filter", "vm")
			createTestServiceType("db-st-filter", "db")
			createTestCatalogItem("small-vm-filter", "vm")
			createTestCatalogItem("small-db-filter", "db")

			// Create instances for different catalog items
			cii1 := model.CatalogItemInstance{
				ID:          "vm-instance-1",
				ApiVersion:  "v1alpha1",
				DisplayName: "VM Instance",
				Spec: model.CatalogItemInstanceSpec{
					CatalogItemId: "small-vm-filter",
					UserValues:    []model.UserValue{},
				},
				Path: "catalog-item-instances/vm-instance-1",
			}
			_, err := catalogItemInstanceStore.Create(context.Background(), cii1)
			Expect(err).ToNot(HaveOccurred())

			cii2 := model.CatalogItemInstance{
				ID:          "db-instance-1",
				ApiVersion:  "v1alpha1",
				DisplayName: "DB Instance",
				Spec: model.CatalogItemInstanceSpec{
					CatalogItemId: "small-db-filter",
					UserValues:    []model.UserValue{},
				},
				Path: "catalog-item-instances/db-instance-1",
			}
			_, err = catalogItemInstanceStore.Create(context.Background(), cii2)
			Expect(err).ToNot(HaveOccurred())

			// Filter for small-vm catalog item
			smallVMFilter := "small-vm-filter"
			results, err := catalogItemInstanceStore.List(context.Background(), &store.CatalogItemInstanceListOptions{
				PageSize:      100,
				CatalogItemId: &smallVMFilter,
			})
			Expect(err).ToNot(HaveOccurred())
			Expect(results.CatalogItemInstances).To(HaveLen(1))
			Expect(results.CatalogItemInstances[0].Spec.CatalogItemId).To(Equal("small-vm-filter"))

			// Filter for small-db catalog item
			smallDBFilter := "small-db-filter"
			results, err = catalogItemInstanceStore.List(context.Background(), &store.CatalogItemInstanceListOptions{
				PageSize:      100,
				CatalogItemId: &smallDBFilter,
			})
			Expect(err).ToNot(HaveOccurred())
			Expect(results.CatalogItemInstances).To(HaveLen(1))
			Expect(results.CatalogItemInstances[0].Spec.CatalogItemId).To(Equal("small-db-filter"))

			// Filter for non-existent catalog item
			nonExistentFilter := "non-existent"
			results, err = catalogItemInstanceStore.List(context.Background(), &store.CatalogItemInstanceListOptions{
				PageSize:      100,
				CatalogItemId: &nonExistentFilter,
			})
			Expect(err).ToNot(HaveOccurred())
			Expect(results.CatalogItemInstances).To(BeEmpty())
		})

		It("should handle pagination correctly", func() {
			// Create prerequisites
			createTestServiceType("vm-st-page", "vm")
			createTestCatalogItem("small-vm-page", "vm")

			// Create 6 catalog item instances
			for i := 1; i <= 6; i++ {
				cii := model.CatalogItemInstance{
					ID:          fmt.Sprintf("page-cii-%d", i),
					ApiVersion:  "v1alpha1",
					DisplayName: fmt.Sprintf("Instance %d", i),
					Spec: model.CatalogItemInstanceSpec{
						CatalogItemId: "small-vm-page",
						UserValues:    []model.UserValue{},
					},
					Path: fmt.Sprintf("catalog-item-instances/page-cii-%d", i),
				}
				time.Sleep(time.Millisecond)
				_, err := catalogItemInstanceStore.Create(context.Background(), cii)
				Expect(err).ToNot(HaveOccurred())
			}

			var pageToken *string
			for _, pageSize := range []int{3, 2} {
				results, err := catalogItemInstanceStore.List(context.Background(), &store.CatalogItemInstanceListOptions{
					PageSize:  pageSize,
					PageToken: pageToken,
				})
				Expect(err).ToNot(HaveOccurred())
				Expect(results.CatalogItemInstances).To(HaveLen(pageSize))
				Expect(results.NextPageToken).ToNot(BeNil())
				pageToken = results.NextPageToken
			}

			// Get last page (should have 1 item)
			lastPageResults, err := catalogItemInstanceStore.List(context.Background(), &store.CatalogItemInstanceListOptions{
				PageToken: pageToken,
				PageSize:  4,
			})
			Expect(err).ToNot(HaveOccurred())
			Expect(lastPageResults.CatalogItemInstances).To(HaveLen(1))
			Expect(lastPageResults.NextPageToken).To(BeNil())
		})
	})
})
