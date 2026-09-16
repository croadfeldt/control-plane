package service

import (
	"fmt"
	"time"

	"github.com/dcm-project/control-plane/internal/placement/store/model"
	"github.com/dcm-project/control-plane/internal/placement/types"
)

// storeModelToResource converts a database model to an API response type
func storeModelToResource(m *model.Resource) types.Resource {
	idStr := m.ID
	path := m.Path
	if path == "" {
		path = fmt.Sprintf("resources/%s", idStr)
	}

	return types.Resource{
		Id:                    &idStr,
		Path:                  &path,
		RunId:                 m.RunID,
		CatalogItemInstanceId: m.CatalogItemInstanceId,
		Name:                  m.Name,
		Spec:                  m.Spec,
		RequiresResources:     append([]string(nil), m.RequiresResources...),
		DagLevel:              m.DagLevel,
		Status:                m.Status,
		AgentName:             m.AgentName,
		ApprovalStatus:        m.ApprovalStatus,
		CreateTime:            PtrTime(m.CreateTime),
		UpdateTime:            PtrTime(m.UpdateTime),
	}
}

func storeModelsToRun(resources model.ResourceList) *types.Run {
	if len(resources) == 0 {
		return nil
	}
	out := &types.Run{
		RunId:                 resources[0].RunID,
		CatalogItemInstanceId: resources[0].CatalogItemInstanceId,
		Resources:             make([]types.Resource, 0, len(resources)),
	}
	for i := range resources {
		out.Resources = append(out.Resources, storeModelToResource(&resources[i]))
	}
	return out
}

// Helper functions for pointer conversions

func PtrTime(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}
