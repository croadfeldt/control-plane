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

func buildGraphSpec(builder *service.SpecBuilder, ctx context.Context, catalogItemId string, userValues []v1alpha1.UserValue) (map[string]any, error) {
	graph, err := builder.BuildResourceGraph(ctx, catalogItemId, userValues)
	if err != nil {
		return nil, err
	}
	if len(graph) == 0 {
		return nil, fmt.Errorf("empty graph")
	}
	return graph[0].Spec, nil
}

var _ = Describe("SpecBuilder (via CatalogItemInstance Create)", func() {
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
		err = db.Exec("PRAGMA foreign_keys = ON").Error
		Expect(err).ToNot(HaveOccurred())
		err = db.AutoMigrate(&model.ServiceType{}, &model.CatalogItem{}, &model.CatalogItemInstance{})
		Expect(err).ToNot(HaveOccurred())
		str = store.NewStore(db, slog.Default())
		svc, err = service.NewService(str, &mockPMClient{}, config.DefaultSeedConfig(), slog.Default())
		Expect(err).ToNot(HaveOccurred())

		// Seed ServiceType with a rich spec
		ensureServiceTypeWithSpec(ctx, str, "vm-spec-builder", "vm-sb", map[string]any{
			"vcpu":   map[string]any{"count": float64(1)},
			"memory": map[string]any{"size_gb": float64(2)},
			"disk":   map[string]any{"size_gb": float64(50)},
		})
	})

	AfterEach(func() {
		if str != nil {
			Expect(str.Close()).To(Succeed())
		}
	})

	Describe("full chain resolution", func() {
		It("should resolve ServiceType → CatalogItem → Instance", func() {
			ensureCatalogItemWithFields(ctx, str, "ci-chain", "vm-sb", []model.FieldConfiguration{
				{Path: "spec.vcpu.count", Default: float64(4), Editable: true},
				{Path: "spec.memory.size_gb", Default: float64(8), Editable: true},
			})

			req := &service.CreateCatalogItemInstanceRequest{
				ApiVersion:  "v1alpha1",
				DisplayName: "Chain Test",
				Spec: v1alpha1.CatalogItemInstanceSpec{
					CatalogItemId: "ci-chain",
					UserValues: []v1alpha1.UserValue{
						{Resource: testutil.DefaultResourceName, Path: "spec.vcpu.count", Value: float64(16)},
					},
				},
			}

			result, err := svc.CatalogItemInstance().Create(ctx, req)
			Expect(err).ToNot(HaveOccurred())
			Expect(result).ToNot(BeNil())
		})
	})

	Describe("validation errors", func() {
		It("should reject user_value path that doesn't match any CatalogItem field", func() {
			ensureCatalogItemWithFields(ctx, str, "ci-bad-path", "vm-sb", []model.FieldConfiguration{
				{Path: "spec.vcpu.count", Default: float64(2), Editable: true},
			})

			req := &service.CreateCatalogItemInstanceRequest{
				ApiVersion:  "v1alpha1",
				DisplayName: "Bad Path",
				Spec: v1alpha1.CatalogItemInstanceSpec{
					CatalogItemId: "ci-bad-path",
					UserValues: []v1alpha1.UserValue{
						{Resource: testutil.DefaultResourceName, Path: "spec.network.bandwidth", Value: float64(100)},
					},
				},
			}

			_, err := svc.CatalogItemInstance().Create(ctx, req)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("user value path not found"))
		})

		It("should reject user_value for non-editable field", func() {
			ensureCatalogItemWithFields(ctx, str, "ci-not-editable", "vm-sb", []model.FieldConfiguration{
				{Path: "spec.disk.size_gb", Default: float64(50), Editable: false},
			})

			req := &service.CreateCatalogItemInstanceRequest{
				ApiVersion:  "v1alpha1",
				DisplayName: "Not Editable",
				Spec: v1alpha1.CatalogItemInstanceSpec{
					CatalogItemId: "ci-not-editable",
					UserValues: []v1alpha1.UserValue{
						{Resource: testutil.DefaultResourceName, Path: "spec.disk.size_gb", Value: float64(100)},
					},
				},
			}

			_, err := svc.CatalogItemInstance().Create(ctx, req)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("not editable"))
		})

		It("should reject user_value that fails validation_schema", func() {
			ensureCatalogItemWithFields(ctx, str, "ci-schema-fail", "vm-sb", []model.FieldConfiguration{
				{
					Path:     "spec.vcpu.count",
					Default:  float64(2),
					Editable: true,
					ValidationSchema: map[string]any{
						"type":    "integer",
						"minimum": float64(1),
						"maximum": float64(16),
					},
				},
			})

			req := &service.CreateCatalogItemInstanceRequest{
				ApiVersion:  "v1alpha1",
				DisplayName: "Schema Fail",
				Spec: v1alpha1.CatalogItemInstanceSpec{
					CatalogItemId: "ci-schema-fail",
					UserValues: []v1alpha1.UserValue{
						{Resource: testutil.DefaultResourceName, Path: "spec.vcpu.count", Value: float64(32)},
					},
				},
			}

			_, err := svc.CatalogItemInstance().Create(ctx, req)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("validation failed"))
		})

		It("should accept user_value that passes validation_schema", func() {
			ensureCatalogItemWithFields(ctx, str, "ci-schema-pass", "vm-sb", []model.FieldConfiguration{
				{
					Path:     "spec.vcpu.count",
					Default:  float64(2),
					Editable: true,
					ValidationSchema: map[string]any{
						"type":    "integer",
						"minimum": float64(1),
						"maximum": float64(16),
					},
				},
			})

			req := &service.CreateCatalogItemInstanceRequest{
				ApiVersion:  "v1alpha1",
				DisplayName: "Schema Pass",
				Spec: v1alpha1.CatalogItemInstanceSpec{
					CatalogItemId: "ci-schema-pass",
					UserValues: []v1alpha1.UserValue{
						{Resource: testutil.DefaultResourceName, Path: "spec.vcpu.count", Value: float64(8)},
					},
				},
			}

			result, err := svc.CatalogItemInstance().Create(ctx, req)
			Expect(err).ToNot(HaveOccurred())
			Expect(result).ToNot(BeNil())
		})

		It("should reject user_value that violates depends_on constraint", func() {
			ensureCatalogItemWithFields(ctx, str, "ci-depends-fail", "vm-sb", []model.FieldConfiguration{
				{Path: "spec.vcpu.count", Default: float64(2), Editable: true},
				{
					Path:     "spec.memory.size_gb",
					Default:  float64(4),
					Editable: true,
					DependsOn: &model.DependsOn{
						Path: "spec.vcpu.count",
						AllowedValues: map[string][]any{
							"2": {float64(4), float64(8)},
							"4": {float64(8), float64(16)},
						},
					},
				},
			})

			req := &service.CreateCatalogItemInstanceRequest{
				ApiVersion:  "v1alpha1",
				DisplayName: "DependsOn Fail",
				Spec: v1alpha1.CatalogItemInstanceSpec{
					CatalogItemId: "ci-depends-fail",
					UserValues: []v1alpha1.UserValue{
						{Resource: testutil.DefaultResourceName, Path: "spec.memory.size_gb", Value: float64(32)},
					},
				},
			}

			_, err := svc.CatalogItemInstance().Create(ctx, req)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("depends_on"))
		})

		It("should accept user_value that satisfies depends_on constraint", func() {
			ensureCatalogItemWithFields(ctx, str, "ci-depends-pass", "vm-sb", []model.FieldConfiguration{
				{Path: "spec.vcpu.count", Default: float64(2), Editable: true},
				{
					Path:     "spec.memory.size_gb",
					Default:  float64(4),
					Editable: true,
					DependsOn: &model.DependsOn{
						Path: "spec.vcpu.count",
						AllowedValues: map[string][]any{
							"2": {float64(4), float64(8)},
							"4": {float64(8), float64(16)},
						},
					},
				},
			})

			req := &service.CreateCatalogItemInstanceRequest{
				ApiVersion:  "v1alpha1",
				DisplayName: "DependsOn Pass",
				Spec: v1alpha1.CatalogItemInstanceSpec{
					CatalogItemId: "ci-depends-pass",
					UserValues: []v1alpha1.UserValue{
						{Resource: testutil.DefaultResourceName, Path: "spec.memory.size_gb", Value: float64(8)},
					},
				},
			}

			result, err := svc.CatalogItemInstance().Create(ctx, req)
			Expect(err).ToNot(HaveOccurred())
			Expect(result).ToNot(BeNil())
		})

		It("should validate depends_on against updated source value from user_values", func() {
			ensureCatalogItemWithFields(ctx, str, "ci-depends-updated", "vm-sb", []model.FieldConfiguration{
				{Path: "spec.vcpu.count", Default: float64(2), Editable: true},
				{
					Path:     "spec.memory.size_gb",
					Default:  float64(4),
					Editable: true,
					DependsOn: &model.DependsOn{
						Path: "spec.vcpu.count",
						AllowedValues: map[string][]any{
							"2": {float64(4), float64(8)},
							"4": {float64(8), float64(16)},
						},
					},
				},
			})

			req := &service.CreateCatalogItemInstanceRequest{
				ApiVersion:  "v1alpha1",
				DisplayName: "DependsOn Updated Source",
				Spec: v1alpha1.CatalogItemInstanceSpec{
					CatalogItemId: "ci-depends-updated",
					UserValues: []v1alpha1.UserValue{
						{Resource: testutil.DefaultResourceName, Path: "spec.vcpu.count", Value: float64(4)},
						{Resource: testutil.DefaultResourceName, Path: "spec.memory.size_gb", Value: float64(16)},
					},
				},
			}

			result, err := svc.CatalogItemInstance().Create(ctx, req)
			Expect(err).ToNot(HaveOccurred())
			Expect(result).ToNot(BeNil())
		})

		It("should validate depends_on with source field listed after dependent in user_values", func() {
			ensureCatalogItemWithFields(ctx, str, "ci-depends-order", "vm-sb", []model.FieldConfiguration{
				{Path: "spec.vcpu.count", Default: float64(2), Editable: true},
				{
					Path:     "spec.memory.size_gb",
					Default:  float64(4),
					Editable: true,
					DependsOn: &model.DependsOn{
						Path: "spec.vcpu.count",
						AllowedValues: map[string][]any{
							"4": {float64(8), float64(16)},
						},
					},
				},
			})

			// memory depends on vcpu, but memory is listed first — must still validate against vcpu=4
			req := &service.CreateCatalogItemInstanceRequest{
				ApiVersion:  "v1alpha1",
				DisplayName: "DependsOn Reverse Order",
				Spec: v1alpha1.CatalogItemInstanceSpec{
					CatalogItemId: "ci-depends-order",
					UserValues: []v1alpha1.UserValue{
						{Resource: testutil.DefaultResourceName, Path: "spec.memory.size_gb", Value: float64(16)},
						{Resource: testutil.DefaultResourceName, Path: "spec.vcpu.count", Value: float64(4)},
					},
				},
			}

			result, err := svc.CatalogItemInstance().Create(ctx, req)
			Expect(err).ToNot(HaveOccurred())
			Expect(result).ToNot(BeNil())
		})

		It("should reject depends_on when source value has no allowed_values entry", func() {
			ensureCatalogItemWithFields(ctx, str, "ci-depends-no-key", "vm-sb", []model.FieldConfiguration{
				{Path: "spec.vcpu.count", Default: float64(2), Editable: true},
				{
					Path:     "spec.memory.size_gb",
					Default:  float64(4),
					Editable: true,
					DependsOn: &model.DependsOn{
						Path: "spec.vcpu.count",
						AllowedValues: map[string][]any{
							"2": {float64(4), float64(8)},
						},
					},
				},
			})

			req := &service.CreateCatalogItemInstanceRequest{
				ApiVersion:  "v1alpha1",
				DisplayName: "DependsOn No Key",
				Spec: v1alpha1.CatalogItemInstanceSpec{
					CatalogItemId: "ci-depends-no-key",
					UserValues: []v1alpha1.UserValue{
						{Resource: testutil.DefaultResourceName, Path: "spec.vcpu.count", Value: float64(8)},
						{Resource: testutil.DefaultResourceName, Path: "spec.memory.size_gb", Value: float64(4)},
					},
				},
			}

			_, err := svc.CatalogItemInstance().Create(ctx, req)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("no allowed values defined"))
		})
	})
})

var _ = Describe("BuildResourceGraph (single resource)", func() {
	var (
		ctx     context.Context
		db      *gorm.DB
		str     store.Store
		builder *service.SpecBuilder
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
		err = db.AutoMigrate(&model.ServiceType{}, &model.CatalogItem{}, &model.CatalogItemInstance{})
		Expect(err).ToNot(HaveOccurred())
		str = store.NewStore(db, slog.Default())
		builder = service.NewSpecBuilderForTest(str)

		ensureServiceTypeWithSpec(ctx, str, "vm-direct", "vm-d", map[string]any{
			"vcpu":   map[string]any{"count": float64(1)},
			"memory": map[string]any{"size_gb": float64(2)},
			"disk":   map[string]any{"size_gb": float64(50)},
		})
	})

	AfterEach(func() {
		if str != nil {
			Expect(str.Close()).To(Succeed())
		}
	})

	Describe("spec construction", func() {
		It("should return error when catalog item does not exist", func() {
			_, err := buildGraphSpec(builder, ctx, "nonexistent", nil)
			Expect(err).To(MatchError(service.ErrCatalogItemNotFoundForInstance))
		})

		It("should return the ServiceType spec with defaults applied when no user values given", func() {
			ensureCatalogItemWithFields(ctx, str, "ci-direct-defaults", "vm-d", []model.FieldConfiguration{
				{Path: "spec.vcpu.count", Default: float64(4), Editable: true},
				{Path: "spec.memory.size_gb", Default: float64(8), Editable: false},
			})

			result, err := buildGraphSpec(builder, ctx, "ci-direct-defaults", nil)
			Expect(err).ToNot(HaveOccurred())

			vcpu := result["vcpu"].(map[string]any)
			memory := result["memory"].(map[string]any)
			disk := result["disk"].(map[string]any)

			Expect(vcpu["count"]).To(BeNumerically("==", 4))
			Expect(memory["size_gb"]).To(BeNumerically("==", 8))
			// disk should remain at ServiceType base value (no CatalogItem field for it)
			Expect(disk["size_gb"]).To(BeNumerically("==", 50))
		})

		It("should set service_type in the returned spec", func() {
			ensureCatalogItemWithFields(ctx, str, "ci-direct-st", "vm-d", []model.FieldConfiguration{})

			result, err := buildGraphSpec(builder, ctx, "ci-direct-st", nil)
			Expect(err).ToNot(HaveOccurred())
			Expect(result["service_type"]).To(Equal("vm-d"))
		})

		It("should override defaults with user values", func() {
			ensureCatalogItemWithFields(ctx, str, "ci-direct-override", "vm-d", []model.FieldConfiguration{
				{Path: "spec.vcpu.count", Default: float64(4), Editable: true},
				{Path: "spec.memory.size_gb", Default: float64(8), Editable: true},
			})

			userValues := []v1alpha1.UserValue{
				{Resource: testutil.DefaultResourceName, Path: "spec.vcpu.count", Value: float64(16)},
			}

			result, err := buildGraphSpec(builder, ctx, "ci-direct-override", userValues)
			Expect(err).ToNot(HaveOccurred())

			vcpu := result["vcpu"].(map[string]any)
			memory := result["memory"].(map[string]any)

			// user value overrides default
			Expect(vcpu["count"]).To(BeNumerically("==", 16))
			// default still applied
			Expect(memory["size_gb"]).To(BeNumerically("==", 8))
		})

		It("should preserve ServiceType spec values not covered by CatalogItem fields", func() {
			ensureCatalogItemWithFields(ctx, str, "ci-direct-preserve", "vm-d", []model.FieldConfiguration{
				{Path: "spec.vcpu.count", Default: float64(4), Editable: true},
			})

			result, err := buildGraphSpec(builder, ctx, "ci-direct-preserve", nil)
			Expect(err).ToNot(HaveOccurred())

			// disk and memory should remain at ServiceType base values
			disk := result["disk"].(map[string]any)
			Expect(disk["size_gb"]).To(BeNumerically("==", 50))
			memory := result["memory"].(map[string]any)
			Expect(memory["size_gb"]).To(BeNumerically("==", 2))
		})
	})

	Describe("validation", func() {
		It("should reject user_value path not in CatalogItem fields", func() {
			ensureCatalogItemWithFields(ctx, str, "ci-direct-badpath", "vm-d", []model.FieldConfiguration{
				{Path: "spec.vcpu.count", Default: float64(2), Editable: true},
			})

			userValues := []v1alpha1.UserValue{
				{Resource: testutil.DefaultResourceName, Path: "spec.network.bandwidth", Value: float64(100)},
			}

			_, err := buildGraphSpec(builder, ctx, "ci-direct-badpath", userValues)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("user value path not found"))
		})

		It("should reject user_value for non-editable field", func() {
			ensureCatalogItemWithFields(ctx, str, "ci-direct-noedit", "vm-d", []model.FieldConfiguration{
				{Path: "spec.disk.size_gb", Default: float64(50), Editable: false},
			})

			userValues := []v1alpha1.UserValue{
				{Resource: testutil.DefaultResourceName, Path: "spec.disk.size_gb", Value: float64(100)},
			}

			_, err := buildGraphSpec(builder, ctx, "ci-direct-noedit", userValues)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("not editable"))
		})

		It("should reject invalid field default against validation_schema", func() {
			ensureCatalogItemWithFields(ctx, str, "ci-direct-default-schemafail", "vm-d", []model.FieldConfiguration{
				{
					Path:     "spec.vcpu.count",
					Default:  float64(32),
					Editable: true,
					ValidationSchema: map[string]any{
						"type":    "integer",
						"minimum": float64(1),
						"maximum": float64(16),
					},
				},
			})

			_, err := buildGraphSpec(builder, ctx, "ci-direct-default-schemafail", nil)
			Expect(err).To(HaveOccurred())
			Expect(errors.Is(err, service.ErrFieldDefaultValidationFailed)).To(BeTrue())
			Expect(err.Error()).To(ContainSubstring("spec.vcpu.count"))
		})

		It("should reject user_value failing validation_schema", func() {
			ensureCatalogItemWithFields(ctx, str, "ci-direct-schemafail", "vm-d", []model.FieldConfiguration{
				{
					Path:     "spec.vcpu.count",
					Default:  float64(2),
					Editable: true,
					ValidationSchema: map[string]any{
						"type":    "integer",
						"minimum": float64(1),
						"maximum": float64(16),
					},
				},
			})

			userValues := []v1alpha1.UserValue{
				{Resource: testutil.DefaultResourceName, Path: "spec.vcpu.count", Value: float64(32)},
			}

			_, err := buildGraphSpec(builder, ctx, "ci-direct-schemafail", userValues)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("validation failed"))
		})

		It("should accept user_value passing validation_schema", func() {
			ensureCatalogItemWithFields(ctx, str, "ci-direct-schemapass", "vm-d", []model.FieldConfiguration{
				{
					Path:     "spec.vcpu.count",
					Default:  float64(2),
					Editable: true,
					ValidationSchema: map[string]any{
						"type":    "integer",
						"minimum": float64(1),
						"maximum": float64(16),
					},
				},
			})

			userValues := []v1alpha1.UserValue{
				{Resource: testutil.DefaultResourceName, Path: "spec.vcpu.count", Value: float64(8)},
			}

			result, err := buildGraphSpec(builder, ctx, "ci-direct-schemapass", userValues)
			Expect(err).ToNot(HaveOccurred())

			vcpu := result["vcpu"].(map[string]any)
			Expect(vcpu["count"]).To(BeNumerically("==", 8))
		})

		It("should reject depends_on violation", func() {
			ensureCatalogItemWithFields(ctx, str, "ci-direct-depfail", "vm-d", []model.FieldConfiguration{
				{Path: "spec.vcpu.count", Default: float64(2), Editable: true},
				{
					Path:     "spec.memory.size_gb",
					Default:  float64(4),
					Editable: true,
					DependsOn: &model.DependsOn{
						Path: "spec.vcpu.count",
						AllowedValues: map[string][]any{
							"2": {float64(4), float64(8)},
						},
					},
				},
			})

			userValues := []v1alpha1.UserValue{
				{Resource: testutil.DefaultResourceName, Path: "spec.memory.size_gb", Value: float64(32)},
			}

			_, err := buildGraphSpec(builder, ctx, "ci-direct-depfail", userValues)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("depends_on"))
		})

		It("should accept depends_on when value is allowed", func() {
			ensureCatalogItemWithFields(ctx, str, "ci-direct-deppass", "vm-d", []model.FieldConfiguration{
				{Path: "spec.vcpu.count", Default: float64(2), Editable: true},
				{
					Path:     "spec.memory.size_gb",
					Default:  float64(4),
					Editable: true,
					DependsOn: &model.DependsOn{
						Path: "spec.vcpu.count",
						AllowedValues: map[string][]any{
							"2": {float64(4), float64(8)},
						},
					},
				},
			})

			userValues := []v1alpha1.UserValue{
				{Resource: testutil.DefaultResourceName, Path: "spec.memory.size_gb", Value: float64(8)},
			}

			result, err := buildGraphSpec(builder, ctx, "ci-direct-deppass", userValues)
			Expect(err).ToNot(HaveOccurred())

			memory := result["memory"].(map[string]any)
			Expect(memory["size_gb"]).To(BeNumerically("==", 8))
		})

		It("should validate depends_on against user-provided source value", func() {
			ensureCatalogItemWithFields(ctx, str, "ci-direct-depsrc", "vm-d", []model.FieldConfiguration{
				{Path: "spec.vcpu.count", Default: float64(2), Editable: true},
				{
					Path:     "spec.memory.size_gb",
					Default:  float64(4),
					Editable: true,
					DependsOn: &model.DependsOn{
						Path: "spec.vcpu.count",
						AllowedValues: map[string][]any{
							"4": {float64(8), float64(16)},
						},
					},
				},
			})

			userValues := []v1alpha1.UserValue{
				{Resource: testutil.DefaultResourceName, Path: "spec.vcpu.count", Value: float64(4)},
				{Resource: testutil.DefaultResourceName, Path: "spec.memory.size_gb", Value: float64(16)},
			}

			result, err := buildGraphSpec(builder, ctx, "ci-direct-depsrc", userValues)
			Expect(err).ToNot(HaveOccurred())

			vcpu := result["vcpu"].(map[string]any)
			memory := result["memory"].(map[string]any)
			Expect(vcpu["count"]).To(BeNumerically("==", 4))
			Expect(memory["size_gb"]).To(BeNumerically("==", 16))
		})

		It("should reject depends_on when source value has no allowed_values entry", func() {
			ensureCatalogItemWithFields(ctx, str, "ci-direct-depnokey", "vm-d", []model.FieldConfiguration{
				{Path: "spec.vcpu.count", Default: float64(2), Editable: true},
				{
					Path:     "spec.memory.size_gb",
					Default:  float64(4),
					Editable: true,
					DependsOn: &model.DependsOn{
						Path: "spec.vcpu.count",
						AllowedValues: map[string][]any{
							"2": {float64(4), float64(8)},
						},
					},
				},
			})

			userValues := []v1alpha1.UserValue{
				{Resource: testutil.DefaultResourceName, Path: "spec.vcpu.count", Value: float64(8)},
				{Resource: testutil.DefaultResourceName, Path: "spec.memory.size_gb", Value: float64(4)},
			}

			_, err := buildGraphSpec(builder, ctx, "ci-direct-depnokey", userValues)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("no allowed values defined"))
		})

		It("should apply multiple user values correctly", func() {
			ensureCatalogItemWithFields(ctx, str, "ci-direct-multi", "vm-d", []model.FieldConfiguration{
				{Path: "spec.vcpu.count", Default: float64(2), Editable: true},
				{Path: "spec.memory.size_gb", Default: float64(4), Editable: true},
				{Path: "spec.disk.size_gb", Default: float64(100), Editable: true},
			})

			userValues := []v1alpha1.UserValue{
				{Resource: testutil.DefaultResourceName, Path: "spec.vcpu.count", Value: float64(8)},
				{Resource: testutil.DefaultResourceName, Path: "spec.memory.size_gb", Value: float64(16)},
				{Resource: testutil.DefaultResourceName, Path: "spec.disk.size_gb", Value: float64(200)},
			}

			result, err := buildGraphSpec(builder, ctx, "ci-direct-multi", userValues)
			Expect(err).ToNot(HaveOccurred())

			Expect(result["service_type"]).To(Equal("vm-d"))
			Expect(result["vcpu"].(map[string]any)["count"]).To(BeNumerically("==", 8))
			Expect(result["memory"].(map[string]any)["size_gb"]).To(BeNumerically("==", 16))
			Expect(result["disk"].(map[string]any)["size_gb"]).To(BeNumerically("==", 200))
		})
	})
})

var _ = Describe("BuildResourceGraph (multi-resource)", func() {
	var (
		ctx     context.Context
		db      *gorm.DB
		str     store.Store
		builder *service.SpecBuilder
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
		err = db.AutoMigrate(&model.ServiceType{}, &model.CatalogItem{}, &model.CatalogItemInstance{})
		Expect(err).ToNot(HaveOccurred())
		str = store.NewStore(db, slog.Default())
		builder = service.NewSpecBuilderForTest(str)

		ensureServiceTypeWithSpec(ctx, str, "db-st", "database", map[string]any{
			"engine":  "postgres",
			"version": "14",
		})
		ensureServiceTypeWithSpec(ctx, str, "ctr-st", "container", map[string]any{
			"image": map[string]any{"reference": "nginx"},
		})
	})

	AfterEach(func() {
		if str != nil {
			Expect(str.Close()).To(Succeed())
		}
	})

	It("should resolve multi-resource catalog item with per-resource user values", func() {
		ci := model.CatalogItem{
			ID:          "dev-app",
			ApiVersion:  "v1alpha1",
			DisplayName: "Dev App",
			Spec: model.CatalogItemSpec{
				Resources: []model.CatalogResource{
					{
						Name:        "ordersDb",
						ServiceType: "database",
						Fields: []model.FieldConfiguration{
							{Path: "engine", Default: "postgres", Editable: true},
							{Path: "version", Default: "16", Editable: true},
						},
					},
					{
						Name:              "app",
						ServiceType:       "container",
						RequiresResources: []string{"ordersDb"},
						Fields: []model.FieldConfiguration{
							{Path: "image.reference", Default: "registry.example.com/app:1.0"},
						},
					},
				},
			},
			Path: "catalog-items/dev-app",
		}
		_, err := str.CatalogItem().Create(ctx, ci)
		Expect(err).ToNot(HaveOccurred())

		resource := "ordersDb"
		graph, err := builder.BuildResourceGraph(ctx, "dev-app", []v1alpha1.UserValue{
			{Resource: resource, Path: "version", Value: "17"},
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(graph).To(HaveLen(2))
		Expect(graph[0].Name).To(Equal("ordersDb"))
		Expect(graph[0].Spec["version"]).To(Equal("17"))
		Expect(graph[1].Name).To(Equal("app"))
		Expect(graph[1].RequiresResources).To(Equal([]string{"ordersDb"}))
	})
})

var _ = Describe("Storage spec building", func() {
	var (
		ctx     context.Context
		db      *gorm.DB
		str     store.Store
		builder *service.SpecBuilder
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
		err = db.AutoMigrate(&model.ServiceType{}, &model.CatalogItem{}, &model.CatalogItemInstance{})
		Expect(err).ToNot(HaveOccurred())
		str = store.NewStore(db, slog.Default())
		builder = service.NewSpecBuilderForTest(str)

		ensureServiceTypeWithSpec(ctx, str, "storage-test", "storage", map[string]any{
			"capacity":    "",
			"volume_name": "",
		})
	})

	AfterEach(func() {
		if str != nil {
			Expect(str.Close()).To(Succeed())
		}
	})

	It("should build storage spec with capacity from user_values via SpecBuilder", func() {
		ensureCatalogItemWithFields(ctx, str, "bronze-storage-sb", "storage", []model.FieldConfiguration{
			{Path: "capacity", Editable: true},
		})

		result, err := buildGraphSpec(builder, ctx, "bronze-storage-sb", []v1alpha1.UserValue{
			{Resource: testutil.DefaultResourceName, Path: "capacity", Value: "100Gi"},
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(result).To(HaveKey("capacity"))
		Expect(result["capacity"]).To(Equal("100Gi"))
		Expect(result["service_type"]).To(Equal("storage"))
	})

	It("should apply provider_hints.kubernetes.storage_class default", func() {
		ensureCatalogItemWithFields(ctx, str, "storage-with-hints", "storage", []model.FieldConfiguration{
			{Path: "capacity", Editable: true},
			{
				Path:     "provider_hints.kubernetes.storage_class",
				Editable: false,
				Default:  "ceph-rbd",
			},
		})

		result, err := buildGraphSpec(builder, ctx, "storage-with-hints", []v1alpha1.UserValue{
			{Resource: testutil.DefaultResourceName, Path: "capacity", Value: "50Gi"},
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(result).To(HaveKey("provider_hints"))
		providerHints, ok := result["provider_hints"].(map[string]any)
		Expect(ok).To(BeTrue())
		Expect(providerHints).To(HaveKey("kubernetes"))
		k8sHints, ok := providerHints["kubernetes"].(map[string]any)
		Expect(ok).To(BeTrue())
		Expect(k8sHints["storage_class"]).To(Equal("ceph-rbd"))
		Expect(result["capacity"]).To(Equal("50Gi"))
	})
})
