package service

import (
	"reflect"
	"testing"

	"github.com/dcm-project/control-plane/internal/catalog/store/model"
)

// The generated table files Machine.VM, Container and KubernetesCluster under
// compute; Storage.Volume and Data.Database are not filed until their families'
// rename PRs land in udlm (ADR-082). These tests pin the inheritance rules, not
// the table's contents.
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
		{Name: "db", ServiceType: "database"}, // filed nowhere yet
		{Name: "job", ServiceType: "container"},
	}}
	if got := catalogItemGroups(spec); !reflect.DeepEqual(got, []string{"compute"}) {
		t.Errorf("union = %v, want [compute]", got)
	}
	if got := catalogItemGroups(model.CatalogItemSpec{}); got != nil {
		t.Errorf("no resources, no groups; got %v", got)
	}
	if !filedUnder([]string{"compute", "storage"}, "storage") || filedUnder([]string{"compute"}, "storage") {
		t.Error("filedUnder membership")
	}
}
