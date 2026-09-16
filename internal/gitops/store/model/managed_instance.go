package model

import "time"

// ManagedInstance records that a CatalogItemInstance is managed by a GitRepository.
type ManagedInstance struct {
	GitRepositoryID string    `gorm:"column:git_repository_id;primaryKey;type:varchar(63)"`
	InstanceID      string    `gorm:"column:instance_id;primaryKey;type:varchar(63)"`
	CreateTime      time.Time `gorm:"column:create_time;autoCreateTime"`

	GitRepositoryRef *GitRepository `gorm:"foreignKey:GitRepositoryID;references:ID;constraint:OnDelete:CASCADE"`
}

func (ManagedInstance) TableName() string {
	return "gitops_managed_instances"
}
