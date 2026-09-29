package service

import (
	"reflect"
	"testing"

	"github.com/dcm-project/control-plane/internal/catalog/store/model"
)

// The generated table files vm/container/cluster under compute, storage under
// storage and database under data (ADR-082). These tests pin the inheritance
// rules; the table's contents come from the registry.
func TestGroupsOf(t *testing.T) {
	if got := groupsOf("vm"); !reflect.DeepEqual(got, []string{"compute"}) {
		t.Errorf("vm groups = %v", got)
	}
	if got := groupsOf("no-such-type"); got != nil {
		t.Errorf("unknown slug must have no groups, got %v", got)
	}
}

func TestCatalogItemGroupsIsTheUnion(t *testing.T) {
	spec := model.CatalogItemSpec{Resources: []model.CatalogResource{
		{Name: "app", ServiceType: "vm"},
		{Name: "db", ServiceType: "database"},
		{Name: "job", ServiceType: "container"},
	}}
	if got := catalogItemGroups(spec); !reflect.DeepEqual(got, []string{"compute", "data"}) {
		t.Errorf("union = %v, want [compute data]", got)
	}
	if got := catalogItemGroups(model.CatalogItemSpec{}); got != nil {
		t.Errorf("no resources, no groups; got %v", got)
	}
	if !filedUnder([]string{"compute", "storage"}, "storage") || filedUnder([]string{"compute"}, "storage") {
		t.Error("filedUnder membership")
	}
}
