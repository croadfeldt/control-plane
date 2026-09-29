package service

import (
	"errors"

	"github.com/dcm-project/control-plane/api/catalog/v1alpha1"
	"github.com/dcm-project/control-plane/internal/catalog/store"
	"github.com/dcm-project/control-plane/internal/catalog/store/model"
)

// catalogItemToStoreModel converts a CreateCatalogItemRequest to a store model
func catalogItemToStoreModel(id, path string, req *CreateCatalogItemRequest) model.CatalogItem {
	spec := catalogItemSpecAPIToModel(req.Spec)

	return model.CatalogItem{
		ID:          id,
		ApiVersion:  req.ApiVersion,
		DisplayName: req.DisplayName,
		Spec:        spec,
		Path:        path,
	}
}

// catalogItemToAPIType converts a store model to an API type
func catalogItemToAPIType(m *model.CatalogItem) v1alpha1.CatalogItem {
	spec := catalogItemSpecModelToAPI(m.Spec)
	item := v1alpha1.CatalogItem{
		ApiVersion:  &m.ApiVersion,
		DisplayName: &m.DisplayName,
		Spec:        &spec,
		Path:        &m.Path,
		Uid:         &m.ID,
		CreateTime:  &m.CreateTime,
		UpdateTime:  &m.UpdateTime,
	}
	if groups := catalogItemGroups(m.Spec); groups != nil {
		item.FiledUnder = &groups
	}
	return item
}

// mapCatalogItemStoreError converts store errors to service domain errors
func mapCatalogItemStoreError(err error) error {
	if err == nil {
		return nil
	}

	switch {
	case errors.Is(err, store.ErrCatalogItemNotFound):
		return ErrCatalogItemNotFound
	case errors.Is(err, store.ErrCatalogItemIDTaken):
		return ErrCatalogItemIDTaken
	case errors.Is(err, store.ErrCatalogItemHasInstances):
		return ErrCatalogItemHasInstances
	case errors.Is(err, store.ErrServiceTypeNotFound):
		return ErrServiceTypeNotFound
	default:
		return err
	}
}
