package store

import (
	"context"

	"github.com/dcm-project/control-plane/internal/gitops/store/model"
	"gorm.io/gorm"
)

// ManagedInstance provides persistence for the repo->instance ownership mapping.
type ManagedInstance interface {
	// ListByRepo returns all instance IDs managed by the given git repository.
	ListByRepo(ctx context.Context, gitRepositoryID string) ([]string, error)
	// Add records that the given instance is managed by the given git repository.
	Add(ctx context.Context, gitRepositoryID, instanceID string) error
	// Remove removes the ownership record for the given instance under the given repo.
	Remove(ctx context.Context, gitRepositoryID, instanceID string) error
}

type ManagedInstanceStore struct {
	db *gorm.DB
}

var _ ManagedInstance = (*ManagedInstanceStore)(nil)

func NewManagedInstance(db *gorm.DB) ManagedInstance {
	return &ManagedInstanceStore{db: db}
}

func (s *ManagedInstanceStore) ListByRepo(ctx context.Context, gitRepositoryID string) ([]string, error) {
	var ids []string
	err := s.db.WithContext(ctx).
		Model(&model.ManagedInstance{}).
		Where("git_repository_id = ?", gitRepositoryID).
		Order("instance_id ASC").
		Pluck("instance_id", &ids).Error
	if err != nil {
		return nil, err
	}
	if ids == nil {
		ids = []string{}
	}
	return ids, nil
}

func (s *ManagedInstanceStore) Add(ctx context.Context, gitRepositoryID, instanceID string) error {
	record := model.ManagedInstance{
		GitRepositoryID: gitRepositoryID,
		InstanceID:      instanceID,
	}
	return s.db.WithContext(ctx).Create(&record).Error
}

func (s *ManagedInstanceStore) Remove(ctx context.Context, gitRepositoryID, instanceID string) error {
	return s.db.WithContext(ctx).
		Where("git_repository_id = ? AND instance_id = ?", gitRepositoryID, instanceID).
		Delete(&model.ManagedInstance{}).Error
}
