package model

import (
	"time"
)

// CatalogItemInstance represents a catalog item instance in the database
type CatalogItemInstance struct {
	ID          string                  `gorm:"column:id;primaryKey"`
	ApiVersion  string                  `gorm:"column:api_version;not null"`
	DisplayName string                  `gorm:"column:display_name;not null"`
	Spec        CatalogItemInstanceSpec `gorm:"column:spec;type:jsonb;not null;serializer:json"`
	Path        string                  `gorm:"column:path;not null"`
	CreateTime  time.Time               `gorm:"column:create_time;autoCreateTime"`
	UpdateTime  time.Time               `gorm:"column:update_time;autoUpdateTime"`
	// RunID is the Placement run id
	RunID string `gorm:"column:run_id"`

	// Indexed field for filtering
	SpecCatalogItemId string       `gorm:"column:spec_catalog_item_id;not null;index"`
	CatalogItemRef    *CatalogItem `gorm:"foreignKey:SpecCatalogItemId;references:ID;constraint:OnDelete:RESTRICT"`
}

// CatalogItemInstanceList is a slice of CatalogItemInstance for list results
type CatalogItemInstanceList []CatalogItemInstance

// CatalogItemInstanceSpec represents the spec field of a catalog item instance
type CatalogItemInstanceSpec struct {
	CatalogItemId string      `json:"catalog_item_id"`
	UserValues    []UserValue `json:"user_values"`
}

// UserValue represents a user-provided value for a field
type UserValue struct {
	Resource string `json:"resource,omitempty"`
	Path     string `json:"path"`
	Value    any    `json:"value"`
}
