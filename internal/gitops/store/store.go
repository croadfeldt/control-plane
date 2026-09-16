// Package store provides the data persistence layer for gitops resources.
package store

import "gorm.io/gorm"

type Store interface {
	Close() error
	GitRepository() GitRepository
	ManagedInstance() ManagedInstance
}

type DataStore struct {
	db              *gorm.DB
	gitRepository   GitRepository
	managedInstance ManagedInstance
}

func NewStore(db *gorm.DB) Store {
	return &DataStore{
		db:              db,
		gitRepository:   NewGitRepository(db),
		managedInstance: NewManagedInstance(db),
	}
}

func (s *DataStore) Close() error {
	sqlDB, err := s.db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}

func (s *DataStore) GitRepository() GitRepository {
	return s.gitRepository
}

func (s *DataStore) ManagedInstance() ManagedInstance {
	return s.managedInstance
}
