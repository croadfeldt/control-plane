package service

import (
	"errors"

	"github.com/dcm-project/control-plane/api/catalog/v1alpha1"
	"github.com/dcm-project/control-plane/internal/catalog/store"
	"github.com/dcm-project/control-plane/internal/catalog/store/model"
)

// toStoreModel converts a CreateServiceTypeRequest to a store model
func toStoreModel(id, path string, req *CreateServiceTypeRequest) model.ServiceType {
	storeModel := model.ServiceType{
		ID:          id,
		ApiVersion:  req.ApiVersion,
		ServiceType: req.ServiceType,
		Spec:        req.Spec,
		Path:        path,
	}

	// Convert metadata if present
	if req.Metadata != nil && req.Metadata.Labels != nil {
		storeModel.Metadata = model.Metadata{
			Labels: *req.Metadata.Labels,
		}
	}

	return storeModel
}

// toAPIType converts a store model to an API type
func toAPIType(m *model.ServiceType) v1alpha1.ServiceType {
	apiType := v1alpha1.ServiceType{
		ApiVersion:  m.ApiVersion,
		ServiceType: m.ServiceType,
		Spec:        m.Spec,
		Path:        &m.Path,
		Uid:         &m.ID,
		CreateTime:  &m.CreateTime,
		UpdateTime:  &m.UpdateTime,
	}
	if groups := groupsOf(m.ServiceType); groups != nil {
		apiType.FiledUnder = &groups
	}

	// Convert metadata if present
	if m.Metadata.Labels != nil {
		labels := m.Metadata.Labels
		apiType.Metadata = &struct {
			Labels *map[string]string `json:"labels,omitempty"`
		}{
			Labels: &labels,
		}
	}

	return apiType
}

// mapStoreError converts store errors to service domain errors
func mapStoreError(err error) error {
	if err == nil {
		return nil
	}

	switch {
	case errors.Is(err, store.ErrServiceTypeNotFound):
		return ErrServiceTypeNotFound
	case errors.Is(err, store.ErrServiceTypeIDTaken):
		return ErrServiceTypeIDTaken
	case errors.Is(err, store.ErrServiceTypeServiceTypeTaken):
		return ErrServiceTypeNameTaken
	default:
		return err
	}
}
