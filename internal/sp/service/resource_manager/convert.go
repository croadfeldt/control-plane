package resource_manager

import (
	"fmt"

	"github.com/dcm-project/control-plane/api/sp/v1alpha1/resource_manager"
	"github.com/dcm-project/control-plane/internal/sp/service"
	"github.com/dcm-project/control-plane/internal/sp/store/model"
)

// ModelToAPI converts a database model to an API response type.
func ModelToAPI(instance *model.ServiceTypeInstance) *resource_manager.ServiceTypeInstance {
	id := instance.ID
	path := fmt.Sprintf("service-type-instances/%s", id)

	result := &resource_manager.ServiceTypeInstance{
		Id:         &id,
		Path:       &path,
		AgentName:  instance.AgentName,
		Status:     &instance.Status,
		Spec:       instance.Spec,
		CreateTime: service.PtrTime(instance.CreateTime),
		UpdateTime: service.PtrTime(instance.UpdateTime),
	}

	if instance.DeletionStatus != nil {
		ds := resource_manager.ServiceTypeInstanceDeletionStatus(*instance.DeletionStatus)
		result.DeletionStatus = &ds
	}

	return result
}
