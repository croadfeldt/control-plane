package v1alpha1

import (
	"context"

	v1alpha1 "github.com/dcm-project/control-plane/api/catalog/v1alpha1"
	"github.com/dcm-project/control-plane/api/catalog/v1alpha1/servicetypes"
	"github.com/dcm-project/control-plane/internal/catalog/api/server"
)

// ListUsageGroups returns the registry's usage-group vocabulary — the shelves a
// service type or catalog item can be filed under (udlm ADR-082). It is a
// generated table, not stored data, so there is no service or store behind it.
func (h *Handler) ListUsageGroups(ctx context.Context, _ server.ListUsageGroupsRequestObject) (server.ListUsageGroupsResponseObject, error) {
	h.logger.DebugContext(ctx, "Listing usage groups")
	results := make([]v1alpha1.UsageGroup, 0, len(servicetypes.UDLMUsageGroups))
	for _, g := range servicetypes.UDLMUsageGroups {
		results = append(results, v1alpha1.UsageGroup{Term: g.Term, Definition: g.Definition})
	}
	return server.ListUsageGroups200JSONResponse(v1alpha1.UsageGroupList{Results: results}), nil
}
