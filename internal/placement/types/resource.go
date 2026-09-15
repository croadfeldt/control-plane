// Package types holds domain models for placement resources.
package types

import "time"

// ResourceInput is one node in a CreateRun request graph.
type ResourceInput struct {
	ID                *string        `json:"id,omitempty"`
	Name              string         `json:"name"`
	Spec              map[string]any `json:"spec"`
	RequiresResources []string       `json:"requires_resources,omitempty"`
}

// CreateRunRequest is the input for CreateRun.
type CreateRunRequest struct {
	CatalogItemInstanceId string          `json:"catalog_item_instance_id"`
	RunId                 string          `json:"run_id"`
	Resources             []ResourceInput `json:"resources"`
}

// Resource is a placement resource row within a run.
type Resource struct {
	AgentName             *string        `json:"agent_name,omitempty"`
	ApprovalStatus        *string        `json:"approval_status,omitempty"`
	CatalogItemInstanceId string         `json:"catalog_item_instance_id"`
	CreateTime            *time.Time     `json:"create_time,omitempty"`
	DagLevel              int            `json:"dag_level"`
	Id                    *string        `json:"id,omitempty"`
	Name                  string         `json:"name"`
	Path                  *string        `json:"path,omitempty"`
	RequiresResources     []string       `json:"requires_resources,omitempty"`
	RunId                 string         `json:"run_id"`
	Spec                  map[string]any `json:"spec"`
	Status                string         `json:"status,omitempty"`
	UpdateTime            *time.Time     `json:"update_time,omitempty"`
}

// Run is one placement request with nested resource rows.
type Run struct {
	CatalogItemInstanceId string     `json:"catalog_item_instance_id"`
	Resources             []Resource `json:"resources"`
	RunId                 string     `json:"run_id"`
}

// ListRunResult is a paginated list of runs.
type ListRunResult struct {
	NextPageToken *string `json:"next_page_token,omitempty"`
	Runs          []Run   `json:"runs"`
}
