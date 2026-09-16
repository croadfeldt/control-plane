// Package model defines the Agent domain model for GORM persistence.
package model

import (
	"time"
)

type AgentHealthStatus string

const (
	AgentHealthStatusReady       AgentHealthStatus = "ready"
	AgentHealthStatusCongested   AgentHealthStatus = "congested"
	AgentHealthStatusUnavailable AgentHealthStatus = "unavailable"
)

// AgentCost is the relative cost weight an agent reports for placement
// decisions. It mirrors the enum enforced by the OpenAPI request-validation
// middleware in front of /agents; Go code does not re-validate membership.
type AgentCost string

const (
	AgentCostLow        AgentCost = "low"
	AgentCostMediumLow  AgentCost = "medium-low"
	AgentCostMedium     AgentCost = "medium"
	AgentCostMediumHigh AgentCost = "medium-high"
	AgentCostHigh       AgentCost = "high"
)

type Agent struct {
	ID            string            `gorm:"primaryKey;type:varchar(63)"`
	Name          string            `gorm:"uniqueIndex;not null"`
	Environment   string            `gorm:"column:environment"`
	ServiceTypes  []string          `gorm:"column:service_types;serializer:json"`
	Cost          *AgentCost        `gorm:"column:cost;type:varchar(16)"`
	TopicName     string            `gorm:"column:topic_name;not null;uniqueIndex"`
	HealthStatus  AgentHealthStatus `gorm:"column:health_status;default:ready;index:idx_health_heartbeat"`
	LastHeartbeat *time.Time        `gorm:"column:last_heartbeat;index:idx_health_heartbeat"`
	CreateTime    time.Time         `gorm:"column:create_time;autoCreateTime"`
	UpdateTime    time.Time         `gorm:"column:update_time;autoUpdateTime"`
}

type AgentList []Agent
