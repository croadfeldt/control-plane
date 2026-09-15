// Package sprm provides types and an adapter boundary for SP resource instances.
package sprm

import (
	"context"
	"fmt"
)

// CreateResourceRequest is the input for creating a service type instance.
type CreateResourceRequest struct {
	ID        string         `json:"id"`
	Spec      map[string]any `json:"spec"`
	AgentName string         `json:"agent_name,omitempty"`
}

// CreateResourceResponse is the result of creating a service type instance.
type CreateResourceResponse struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

// GetOutputSpecResponse is the persisted provider output for an instance.
type GetOutputSpecResponse struct {
	OutputSpec map[string]any `json:"output_spec"`
}

// Client is the port PlacementService uses to manage SP resource instances.
type Client interface {
	CreateResource(ctx context.Context, req CreateResourceRequest) (*CreateResourceResponse, error)
	GetOutputSpec(ctx context.Context, resourceID string) (*GetOutputSpecResponse, error)
	DeleteResource(ctx context.Context, resourceId string) error
	DeleteResourceDeferred(ctx context.Context, resourceId string) error
	// ReassignResource re-points an existing resource at a new agent and
	// re-triggers provisioning. Used by the self-healing loop.
	//
	// expectedCurrentAgent must be the agent the caller observed this
	// resource on when it decided to reassign it (e.g. the excluded/failed
	// agent), not a value re-read at call time: it's CASed against the
	// live agent_name at the SP layer specifically to catch a concurrent
	// reassignment that happened in between, so re-deriving it fresh here
	// would silently defeat that guard.
	ReassignResource(ctx context.Context, resourceId string, agentName string, expectedCurrentAgent string) error
}

// HTTPError carries a status code and message from the SPRM adapter.
type HTTPError struct {
	StatusCode int
	Body       string
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("sprm returned status %d: %s", e.StatusCode, e.Body)
}
