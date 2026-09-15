package resource_manager

import (
	"github.com/dcm-project/control-plane/api/sp/v1alpha1/resource_manager"
	server "github.com/dcm-project/control-plane/internal/sp/api/resource_manager"
)

// convertServerToAPI converts a server ServiceTypeInstance to an API ServiceTypeInstance.
func convertServerToAPI(src *server.ServiceTypeInstance) *resource_manager.ServiceTypeInstance {
	return &resource_manager.ServiceTypeInstance{
		Id:   src.Id,
		Spec: src.Spec,
	}
}

// convertAPIToServer converts an API ServiceTypeInstance to a server ServiceTypeInstance.
func convertAPIToServer(src *resource_manager.ServiceTypeInstance) server.ServiceTypeInstance {
	result := server.ServiceTypeInstance{
		Id:         src.Id,
		Path:       src.Path,
		AgentName:  src.AgentName,
		Status:     src.Status,
		Spec:       src.Spec,
		CreateTime: src.CreateTime,
		UpdateTime: src.UpdateTime,
	}

	if src.DeletionStatus != nil {
		ds := server.ServiceTypeInstanceDeletionStatus(*src.DeletionStatus)
		result.DeletionStatus = &ds
	}

	return result
}

// convertAPIListToServer converts a slice of API ServiceTypeInstance to server ServiceTypeInstance.
func convertAPIListToServer(src *[]resource_manager.ServiceTypeInstance) []server.ServiceTypeInstance {
	if src == nil {
		return nil
	}
	result := make([]server.ServiceTypeInstance, len(*src))
	for i, inst := range *src {
		result[i] = convertAPIToServer(&inst)
	}
	return result
}
