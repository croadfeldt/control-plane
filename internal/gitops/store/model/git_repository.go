// Package model defines the GORM database models for gitops resources.
package model

import (
	"time"
)

// GitRepository represents a git repository configuration in the database.
type GitRepository struct {
	ID               string     `gorm:"column:id;primaryKey;type:varchar(63)"`
	ApiVersion       string     `gorm:"column:api_version;not null"`
	DisplayName      string     `gorm:"column:display_name;not null;uniqueIndex"`
	URL              string     `gorm:"column:url;not null"`
	Branch           string     `gorm:"column:branch;not null;default:main"`
	Path             string     `gorm:"column:path;not null;default:."`
	IntervalSeconds  int        `gorm:"column:interval_seconds;not null;default:60"`
	SyncState        string     `gorm:"column:sync_state;not null;default:PENDING"`
	LastSyncedCommit string     `gorm:"column:last_synced_commit"`
	StatusMessage    string     `gorm:"column:status_message"`
	LastSyncTime     *time.Time `gorm:"column:last_sync_time"`
	CreateTime       time.Time  `gorm:"column:create_time;autoCreateTime"`
	UpdateTime       time.Time  `gorm:"column:update_time;autoUpdateTime"`
}

// GitRepositoryList is a slice of GitRepository for list results.
type GitRepositoryList []GitRepository
