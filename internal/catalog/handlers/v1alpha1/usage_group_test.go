package v1alpha1_test

import (
	"context"
	"log/slog"
	"testing"

	"github.com/dcm-project/control-plane/api/catalog/v1alpha1/servicetypes"
	"github.com/dcm-project/control-plane/internal/catalog/api/server"
	handlers "github.com/dcm-project/control-plane/internal/catalog/handlers/v1alpha1"
)

// /usage-groups serves the generated vocabulary as is: same terms, same order,
// no store behind it.
func TestListUsageGroupsServesTheVocabulary(t *testing.T) {
	h := handlers.NewHandler(nil, slog.Default())
	resp, err := h.ListUsageGroups(context.Background(), server.ListUsageGroupsRequestObject{})
	if err != nil {
		t.Fatal(err)
	}
	list, ok := resp.(server.ListUsageGroups200JSONResponse)
	if !ok {
		t.Fatalf("unexpected response %T", resp)
	}
	if len(list.Results) != len(servicetypes.UDLMUsageGroups) || len(list.Results) == 0 {
		t.Fatalf("got %d groups, want %d", len(list.Results), len(servicetypes.UDLMUsageGroups))
	}
	for i, g := range servicetypes.UDLMUsageGroups {
		if list.Results[i].Term != g.Term || list.Results[i].Definition != g.Definition {
			t.Errorf("group %d = %+v, want %+v", i, list.Results[i], g)
		}
	}
}
