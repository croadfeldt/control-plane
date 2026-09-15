// Package store provides database access interfaces and implementations.
package store

import (
	"github.com/cenkalti/backoff/v5"
	rmstore "github.com/dcm-project/control-plane/internal/sp/store/resource_manager"
	"gorm.io/gorm"
)

type Store interface {
	Close() error
	ServiceTypeInstance() rmstore.ServiceTypeInstance
}

type DataStore struct {
	db       *gorm.DB
	instance rmstore.ServiceTypeInstance
}

type storeConfig struct {
	instanceRetry []backoff.RetryOption
}

// StoreOption configures NewStore.
type StoreOption func(*storeConfig)

// WithServiceTypeInstanceRetry sets backoff for service type instance Create/HardDelete.
func WithServiceTypeInstanceRetry(opts ...backoff.RetryOption) StoreOption {
	return func(c *storeConfig) {
		c.instanceRetry = append([]backoff.RetryOption(nil), opts...)
	}
}

// NewStore constructs the data store. Optional StoreOptions are for tests (e.g. fast DB retry).
func NewStore(db *gorm.DB, opts ...StoreOption) Store {
	cfg := &storeConfig{}
	for _, o := range opts {
		o(cfg)
	}
	var instance rmstore.ServiceTypeInstance
	if len(cfg.instanceRetry) > 0 {
		instance = rmstore.NewServiceTypeInstance(db, cfg.instanceRetry...)
	} else {
		instance = rmstore.NewServiceTypeInstance(db)
	}
	return &DataStore{
		db:       db,
		instance: instance,
	}
}

func (s *DataStore) Close() error {
	sqlDB, err := s.db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}

func (s *DataStore) ServiceTypeInstance() rmstore.ServiceTypeInstance {
	return s.instance
}
