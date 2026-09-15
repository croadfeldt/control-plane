package service

import (
	"context"
	"fmt"

	"github.com/dcm-project/control-plane/internal/placement/types"
)

// validateCreateRunRequest checks CreateRun request fields and that run_id is unused.
func (s *PlacementService) validateCreateRunRequest(ctx context.Context, req *types.CreateRunRequest) error {
	if req == nil {
		return NewValidationError("create run request is required")
	}
	if req.CatalogItemInstanceId == "" {
		return NewValidationError("catalog_item_instance_id is required")
	}
	if req.RunId == "" {
		return NewValidationError("run_id is required")
	}
	if len(req.Resources) == 0 {
		return NewValidationError("resources must contain at least one resource")
	}
	existing, err := s.store.Resource().ListByRunID(ctx, req.RunId)
	if err != nil {
		return NewInternalError(fmt.Sprintf("failed to check run_id uniqueness: %v", err))
	}
	if len(existing) > 0 {
		return NewConflictError(fmt.Sprintf("run with id %s already exists", req.RunId))
	}
	return nil
}
