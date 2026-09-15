package service_test

import (
	"context"
	"fmt"
	"log/slog"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/dcm-project/control-plane/api/catalog/v1alpha1/servicetypes/three_tier_app_demo"
	"github.com/dcm-project/control-plane/internal/catalog/config"
	"github.com/dcm-project/control-plane/internal/catalog/service"
	"github.com/dcm-project/control-plane/internal/catalog/store"
	"github.com/dcm-project/control-plane/internal/catalog/store/model"
	"github.com/dcm-project/control-plane/internal/catalog/testutil"
)

var _ = Describe("Seed", func() {
	var (
		db        *gorm.DB
		dataStore store.Store
		svc       service.Service
	)

	BeforeEach(func() {
		var err error
		db, err = gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
			Logger: logger.Discard,
		})
		Expect(err).ToNot(HaveOccurred())

		err = db.Exec("PRAGMA foreign_keys = ON").Error
		Expect(err).ToNot(HaveOccurred())

		err = db.AutoMigrate(&model.ServiceType{}, &model.CatalogItem{})
		Expect(err).ToNot(HaveOccurred())

		dataStore = store.NewStore(db, slog.Default())
		svc, err = service.NewService(dataStore, &mockPMClient{}, config.DefaultSeedConfig(), slog.Default())
		Expect(err).ToNot(HaveOccurred())
	})

	AfterEach(func() {
		sqlDB, err := db.DB()
		Expect(err).ToNot(HaveOccurred())
		_ = sqlDB.Close()
	})

	Describe("Seed", func() {
		Describe("Service Types", func() {
			It("seeds all service types", func() {
				ctx := context.Background()

				err := svc.Seed(ctx)
				Expect(err).ToNot(HaveOccurred())

				var serviceTypes []model.ServiceType
				err = db.Find(&serviceTypes).Error
				Expect(err).ToNot(HaveOccurred())
				Expect(serviceTypes).To(HaveLen(7))

				ids := make([]string, len(serviceTypes))
				for i, st := range serviceTypes {
					ids[i] = st.ID
				}
				Expect(ids).To(ConsistOf("three-tier-app-demo", "vm", "container", "database", "cluster", "storage", "network"))
			})

			It("inserts missing service types when upgrading a partially seeded database", func() {
				ctx := context.Background()
				legacyIDs := []string{"three-tier-app-demo", "vm", "container", "database", "cluster", "storage"}
				for _, id := range legacyIDs {
					st := model.ServiceType{
						ID:          id,
						ApiVersion:  "v1alpha1",
						ServiceType: id,
						Spec:        map[string]any{},
						Path:        "service-types/" + id,
					}
					_, err := dataStore.ServiceType().Create(ctx, st)
					Expect(err).ToNot(HaveOccurred())
				}

				err := svc.Seed(ctx)
				Expect(err).ToNot(HaveOccurred())

				var count int64
				err = db.Model(&model.ServiceType{}).Count(&count).Error
				Expect(err).ToNot(HaveOccurred())
				Expect(count).To(Equal(int64(7)))

				network, err := dataStore.ServiceType().Get(ctx, "network")
				Expect(err).ToNot(HaveOccurred())
				Expect(network.ServiceType).To(Equal("network"))
				Expect(network.Spec).To(HaveKey("ports"))
				Expect(network.Spec).To(HaveKey("routing_level"))

				err = svc.Seed(ctx)
				Expect(err).ToNot(HaveOccurred())

				err = db.Model(&model.ServiceType{}).Count(&count).Error
				Expect(err).ToNot(HaveOccurred())
				Expect(count).To(Equal(int64(7)))
			})

			DescribeTable("seeds service type with correct spec keys",
				func(id string, expectedKeys []string) {
					ctx := context.Background()

					err := svc.Seed(ctx)
					Expect(err).ToNot(HaveOccurred())

					var st model.ServiceType
					err = db.Where("id = ?", id).First(&st).Error
					Expect(err).ToNot(HaveOccurred())
					Expect(st.ServiceType).To(Equal(id))
					Expect(st.Path).To(Equal("service-types/" + id))
					for _, key := range expectedKeys {
						Expect(st.Spec).To(HaveKey(key))
					}
				},
				Entry("vm", "vm", []string{"vcpu", "memory", "storage", "guest_os", "access", "ip"}),
				Entry("container", "container", []string{"image", "resources", "process", "network", "endpoints"}),
				Entry("database", "database", []string{"engine", "version", "resources", "connection_string"}),
				Entry("cluster", "cluster", []string{"version", "api_endpoint", "console_url", "kubeconfig"}),
				Entry("storage", "storage", []string{"capacity", "volume_name"}),
				Entry("network", "network", []string{"ports", "routing_level", "endpoints"}),
			)
		})

		Describe("Pet Clinic", func() {
			It("is idempotent when called multiple times", func() {
				ctx := context.Background()

				err := svc.Seed(ctx)
				Expect(err).ToNot(HaveOccurred())

				err = svc.Seed(ctx)
				Expect(err).ToNot(HaveOccurred())

				var count int64
				err = db.Model(&model.CatalogItem{}).Where("id = ?", "pet-clinic").Count(&count).Error
				Expect(err).ToNot(HaveOccurred())
				Expect(count).To(Equal(int64(1)))
			})

			It("seeds when table is empty", func() {
				ctx := context.Background()

				err := svc.Seed(ctx)
				Expect(err).ToNot(HaveOccurred())

				ci, err := dataStore.CatalogItem().Get(ctx, "pet-clinic")
				Expect(err).ToNot(HaveOccurred())
				Expect(ci.ID).To(Equal("pet-clinic"))
				Expect(ci.DisplayName).To(Equal("Pet Clinic"))
				Expect(ci.Path).To(Equal("catalog-items/pet-clinic"))
				Expect(ci.Spec.Resources[0].ServiceType).To(Equal("three-tier-app-demo"))
				Expect(ci.Spec.Resources[0].Fields).To(HaveLen(5))

				// Verify key field configs
				fieldPaths := make([]string, len(ci.Spec.Resources[0].Fields))
				for i, f := range ci.Spec.Resources[0].Fields {
					fieldPaths[i] = f.Path
				}
				Expect(fieldPaths).To(ContainElement("metadata.labels.region"))
				Expect(fieldPaths).To(ContainElement("database.engine"))
				Expect(fieldPaths).To(ContainElement("database.version"))
				Expect(fieldPaths).To(ContainElement("app.image"))
				Expect(fieldPaths).To(ContainElement("web.image"))

				// Verify region field uses configured values
				regionField := findFieldByPath(ci.Spec.Resources[0].Fields, "metadata.labels.region")
				Expect(regionField).ToNot(BeNil())
				Expect(regionField.Editable).To(BeTrue())
				Expect(regionField.Default).To(BeNil())
				Expect(regionField.ValidationSchema).ToNot(BeNil())
				Expect(regionField.ValidationSchema["type"]).To(Equal("string"))
				regionEnum, ok := regionField.ValidationSchema["enum"].([]any)
				Expect(ok).To(BeTrue(), "expected ValidationSchema.enum for region to be []any")
				Expect(regionEnum).To(ConsistOf("region-a", "region-b"))

				// Verify database.engine is editable and has validation schema enum
				dbEngineField := findFieldByPath(ci.Spec.Resources[0].Fields, "database.engine")
				Expect(dbEngineField).ToNot(BeNil())
				Expect(dbEngineField.Editable).To(BeTrue())
				Expect(dbEngineField.Default).To(Equal(three_tier_app_demo.DefaultDatabaseEngine))
				Expect(dbEngineField.ValidationSchema).ToNot(BeNil())
				Expect(dbEngineField.ValidationSchema["type"]).To(Equal("string"))
				enumVals, ok := dbEngineField.ValidationSchema["enum"].([]any)
				Expect(ok).To(BeTrue(), "expected ValidationSchema.enum for database.engine to be []any")
				Expect(enumVals).To(ConsistOf("postgres", "mysql"))

				// Verify database.version has dependsOn on database.engine and is properly constrained
				dbVersionField := findFieldByPath(ci.Spec.Resources[0].Fields, "database.version")
				Expect(dbVersionField).ToNot(BeNil())
				Expect(dbVersionField.Editable).To(BeTrue())
				Expect(dbVersionField.Default).To(Equal(three_tier_app_demo.DefaultDatabaseVersion))
				Expect(dbVersionField.ValidationSchema).ToNot(BeNil())
				Expect(dbVersionField.ValidationSchema["type"]).To(Equal("string"))
				Expect(dbVersionField.DependsOn).ToNot(BeNil())
				Expect(dbVersionField.DependsOn.Path).To(Equal("database.engine"))
				Expect(dbVersionField.DependsOn.AllowedValues).To(HaveKey("postgres"))
				Expect(dbVersionField.DependsOn.AllowedValues["postgres"]).To(ConsistOf("18", "17"))
				Expect(dbVersionField.DependsOn.AllowedValues).To(HaveKey("mysql"))
				Expect(dbVersionField.DependsOn.AllowedValues["mysql"]).To(ConsistOf("8.4", "8.3", "8"))

				// Verify app.image and web.image fixed defaults
				appImageField := findFieldByPath(ci.Spec.Resources[0].Fields, "app.image")
				Expect(appImageField).ToNot(BeNil())
				Expect(appImageField.Default).To(Equal(three_tier_app_demo.AppImage))
				Expect(appImageField.Editable).To(BeFalse())

				webImageField := findFieldByPath(ci.Spec.Resources[0].Fields, "web.image")
				Expect(webImageField).ToNot(BeNil())
				Expect(webImageField.Default).To(Equal(three_tier_app_demo.WebImage))
				Expect(webImageField.Editable).To(BeFalse())
			})
		})

		Describe("when catalog items exist", func() {
			It("does not seed", func() {
				ctx := context.Background()

				createTestServiceType := func(id, serviceType string) {
					st := model.ServiceType{
						ID:          id,
						ApiVersion:  "v1alpha1",
						ServiceType: serviceType,
						Spec:        map[string]any{},
						Path:        fmt.Sprintf("service-types/%s", id),
					}
					_, err := dataStore.ServiceType().Create(ctx, st)
					Expect(err).ToNot(HaveOccurred())
				}
				createTestServiceType("three-tier-app-demo", "three-tier-app-demo")
				createTestServiceType("vm-st", "vm")

				// Create an existing catalog item
				ci := model.CatalogItem{
					ID:          "existing-item",
					ApiVersion:  "v1alpha1",
					DisplayName: "Existing",
					Spec:        testutil.ModelCatalogSpec("vm", []model.FieldConfiguration{}),
					Path:        "catalog-items/existing-item",
				}
				_, err := dataStore.CatalogItem().Create(ctx, ci)
				Expect(err).ToNot(HaveOccurred())

				err = svc.Seed(ctx)
				Expect(err).ToNot(HaveOccurred())

				// Default seed items should NOT have been added
				_, err = dataStore.CatalogItem().Get(ctx, "pet-clinic")
				Expect(err).To(Equal(store.ErrCatalogItemNotFound))
			})
		})
	})
})

func findFieldByPath(fields []model.FieldConfiguration, path string) *model.FieldConfiguration {
	for i := range fields {
		if fields[i].Path == path {
			return &fields[i]
		}
	}
	return nil
}
