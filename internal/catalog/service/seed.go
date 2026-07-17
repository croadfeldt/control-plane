package service

import (
	"context"

	"github.com/dcm-project/control-plane/api/catalog/v1alpha1/servicetypes/cluster"
	"github.com/dcm-project/control-plane/api/catalog/v1alpha1/servicetypes/container"
	"github.com/dcm-project/control-plane/api/catalog/v1alpha1/servicetypes/database"
	"github.com/dcm-project/control-plane/api/catalog/v1alpha1/servicetypes/storage"
	"github.com/dcm-project/control-plane/api/catalog/v1alpha1/servicetypes/three_tier_app_demo"
	"github.com/dcm-project/control-plane/api/catalog/v1alpha1/servicetypes/vm"
	"github.com/dcm-project/control-plane/internal/catalog/store/model"
)

// Seed ensures required service types and default catalog items exist.
func (s *service) Seed(ctx context.Context) error {
	s.logger.InfoContext(ctx, "Seeding database with defaults")
	if err := s.store.ServiceType().SeedMissing(ctx, defaultServiceTypes()); err != nil {
		return err
	}
	return s.store.CatalogItem().SeedIfEmpty(ctx, s.defaultCatalogItems())
}

func defaultServiceTypes() []model.ServiceType {
	emptyNetwork := &three_tier_app_demo.Network{}
	return []model.ServiceType{
		{
			ID:          "three-tier-app-demo",
			ApiVersion:  "v1alpha1",
			ServiceType: "three-tier-app-demo",
			Spec: map[string]any{
				"database": three_tier_app_demo.DatabaseTier{Engine: three_tier_app_demo.DefaultDatabaseEngine, Version: three_tier_app_demo.DefaultDatabaseVersion, Network: emptyNetwork},
				"app":      three_tier_app_demo.AppTier{Image: "", Network: emptyNetwork},
				"web":      three_tier_app_demo.WebTier{Image: "", Network: emptyNetwork},
			},
			Path: "service-types/three-tier-app-demo",
		},
		{
			ID:          "vm",
			ApiVersion:  "v1alpha1",
			ServiceType: "vm",
			Spec: map[string]any{
				"instance_size": "",
				"vcpu":          vm.Vcpu{},
				"memory":        vm.Memory{},
				"guest_os":      vm.GuestOs{},
				"disks":         vm.Disk{},
				"placement":     vm.Placement{},
				"networks":      vm.Network{},
				"power":         vm.Power{},
			},
			Path: "service-types/vm",
		},
		{
			ID:          "container",
			ApiVersion:  "v1alpha1",
			ServiceType: "container",
			Spec: map[string]any{
				"image":     "",
				"command":   []string{},
				"args":      []string{},
				"resources": container.Resources{},
				"process":   container.Process{},
				"network":   container.Network{},
				"runtime":   container.Runtime{},
			},
			Path: "service-types/container",
		},
		{
			ID:          "database",
			ApiVersion:  "v1alpha1",
			ServiceType: "database",
			Spec: map[string]any{
				"engine":    "",
				"version":   "",
				"resources": database.Resources{},
			},
			Path: "service-types/database",
		},
		{
			ID:          "cluster",
			ApiVersion:  "v1alpha1",
			ServiceType: "cluster",
			Spec: map[string]any{
				"release":    "",
				"node_pools": cluster.NodePool{},
				"network":    cluster.Network{},
			},
			Path: "service-types/cluster",
		},
		{
			ID:          "storage",
			ApiVersion:  "v1alpha1",
			ServiceType: "storage",
			Spec: map[string]any{
				"capacity":          "",
				"access_mode":       storage.StorageSpecAccessMode(""),
				"volume_mode":       storage.StorageSpecVolumeMode(""),
				"storage_class":     "",
				"retain_on_release": false,
			},
			Path: "service-types/storage",
		},
	}
}

func (s *service) defaultCatalogItems() []model.CatalogItem {
	return []model.CatalogItem{
		s.petClinicCatalogItem(),
	}
}

func (s *service) petClinicCatalogItem() model.CatalogItem {
	return model.CatalogItem{
		ID:          "pet-clinic",
		ApiVersion:  "v1alpha1",
		DisplayName: "Pet Clinic",
		Path:        "catalog-items/pet-clinic",
		Spec: model.CatalogItemSpec{
			Resources: []model.CatalogResource{{
				Name:        "app",
				ServiceType: "three-tier-app-demo",
				Fields:      s.petClinicFields(),
			}},
		},
	}
}

func (s *service) petClinicFields() []model.FieldConfiguration {
	regionEnum := make([]any, len(s.seedConfig.RegionEnum))
	for i, v := range s.seedConfig.RegionEnum {
		regionEnum[i] = v
	}
	return []model.FieldConfiguration{
		fieldConfig("metadata.labels.region", "Region", true,
			optionalStringDefault(s.seedConfig.RegionDefault), map[string]any{"type": "string", "enum": regionEnum}, nil),
		fieldConfig("database.engine", "Database engine", true,
			three_tier_app_demo.DefaultDatabaseEngine,
			map[string]any{"type": "string", "enum": []any{"postgres", "mysql"}}, nil),
		fieldConfig("database.version", "Database version", true,
			three_tier_app_demo.DefaultDatabaseVersion,
			map[string]any{"type": "string"}, dependsOn("database.engine", map[string][]any{
				"postgres": {three_tier_app_demo.DefaultDatabaseVersion, "17"},
				"mysql":    {"8.4", "8.3", "8"},
			})),
		fieldConfig("app.image", "App image", false, three_tier_app_demo.AppImage, nil, nil),
		fieldConfig("web.image", "Web image", false, three_tier_app_demo.WebImage, nil, nil),
	}
}

func optionalStringDefault(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func fieldConfig(path, displayName string, editable bool, defaultVal any,
	validationSchema map[string]any, dependsOn *model.DependsOn,
) model.FieldConfiguration {
	return model.FieldConfiguration{
		Path:             path,
		DisplayName:      displayName,
		Editable:         editable,
		Default:          defaultVal,
		ValidationSchema: validationSchema,
		DependsOn:        dependsOn,
	}
}

func dependsOn(path string, allowedValues map[string][]any) *model.DependsOn {
	return &model.DependsOn{Path: path, AllowedValues: allowedValues}
}
