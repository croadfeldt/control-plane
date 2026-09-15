package service

import (
	"context"
	"errors"

	"github.com/dcm-project/control-plane/internal/placement/sprm"
	"github.com/dcm-project/control-plane/internal/placement/store/model"
	"github.com/dcm-project/control-plane/internal/placement/types"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

type sprmOutputMock struct {
	specs            map[string]map[string]any
	getOutputSpecErr error
}

func (m *sprmOutputMock) CreateResource(context.Context, sprm.CreateResourceRequest) (*sprm.CreateResourceResponse, error) {
	return nil, nil
}

func (m *sprmOutputMock) GetOutputSpec(_ context.Context, resourceID string) (*sprm.GetOutputSpecResponse, error) {
	if m.getOutputSpecErr != nil {
		return nil, m.getOutputSpecErr
	}
	return &sprm.GetOutputSpecResponse{OutputSpec: m.specs[resourceID]}, nil
}

func (m *sprmOutputMock) DeleteResource(context.Context, string) error { return nil }

func (m *sprmOutputMock) DeleteResourceDeferred(context.Context, string) error { return nil }

func (m *sprmOutputMock) ReassignResource(context.Context, string, string, string) error { return nil }

var _ = Describe("DAG orchestration helpers", func() {
	It("returns pending resources whose dependencies are running", func() {
		resources := model.ResourceList{
			{ID: "db", Name: "db", Status: types.ResourceStatusRunning, DagLevel: 0},
			{ID: "app", Name: "app", Status: types.ResourceStatusPending, DagLevel: 1, RequiresResources: []string{"db"}},
			{ID: "cache", Name: "cache", Status: types.ResourceStatusPending, DagLevel: 1, RequiresResources: []string{"worker"}},
		}

		ready := pendingResourcesReadyAtLowestLevel(resources)
		Expect(ready).To(HaveLen(1))
		Expect(ready[0].Name).To(Equal("app"))
	})

	It("returns only the lowest ready dag level when multiple levels are ready", func() {
		resources := model.ResourceList{
			{ID: "db", Name: "db", Status: types.ResourceStatusRunning, DagLevel: 0},
			{ID: "b-id", Name: "b", Status: types.ResourceStatusPending, DagLevel: 1, RequiresResources: []string{"db"}},
			{ID: "c-id", Name: "c", Status: types.ResourceStatusPending, DagLevel: 2, RequiresResources: []string{"db"}},
		}

		ready := pendingResourcesReadyAtLowestLevel(resources)
		Expect(ready).To(HaveLen(1))
		Expect(ready[0].Name).To(Equal("b"))
	})

	It("loads output_spec for RUNNING resources via SPRM", func() {
		mock := &sprmOutputMock{specs: map[string]map[string]any{
			"db-id":  {"connection_string": "postgres://db"},
			"vpc-id": {"subnet_id": "subnet-1"},
		}}
		resources := model.ResourceList{
			{ID: "db-id", Name: "db", Status: types.ResourceStatusRunning},
			{ID: "vpc-id", Name: "vpc", Status: types.ResourceStatusRunning},
			{ID: "app-id", Name: "app", Status: types.ResourceStatusPending},
		}

		outputs, err := runningResourceOutputsByName(context.Background(), mock, resources)
		Expect(err).NotTo(HaveOccurred())
		Expect(outputs).To(Equal(map[string]map[string]any{
			"db":  {"connection_string": "postgres://db"},
			"vpc": {"subnet_id": "subnet-1"},
		}))
	})

	It("returns an error when SPRM GetOutputSpec fails", func() {
		mock := &sprmOutputMock{specs: map[string]map[string]any{}}
		mock.getOutputSpecErr = errors.New("sprm unavailable")
		resources := model.ResourceList{
			{ID: "db-id", Name: "db", Status: types.ResourceStatusRunning},
		}

		_, err := runningResourceOutputsByName(context.Background(), mock, resources)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("db"))
	})

	It("skips RUNNING resources with empty output_spec", func() {
		mock := &sprmOutputMock{specs: map[string]map[string]any{
			"db-id": {},
		}}
		resources := model.ResourceList{
			{ID: "db-id", Name: "db", Status: types.ResourceStatusRunning},
		}

		outputs, err := runningResourceOutputsByName(context.Background(), mock, resources)
		Expect(err).NotTo(HaveOccurred())
		Expect(outputs).To(BeEmpty())
	})

	It("selects the highest pending deletion level", func() {
		resources := []model.Resource{
			{Name: "db", Status: types.ResourceStatusPendingDeletion, DagLevel: 0},
			{Name: "app", Status: types.ResourceStatusPendingDeletion, DagLevel: 1},
			{Name: "cache", Status: types.ResourceStatusRunning, DagLevel: 2},
		}
		Expect(highestPendingDeletionLevel(resources)).To(Equal(1))
	})

	It("returns resources at a pending deletion level", func() {
		resources := []model.Resource{
			{Name: "db", Status: types.ResourceStatusPendingDeletion, DagLevel: 1},
			{Name: "app", Status: types.ResourceStatusPendingDeletion, DagLevel: 1},
			{Name: "cache", Status: types.ResourceStatusPendingDeletion, DagLevel: 0},
		}
		atLevel := resourcesAtDeletionLevel(resources, 1)
		Expect(atLevel).To(HaveLen(2))
		Expect(atLevel[0].Name).To(Equal("db"))
		Expect(atLevel[1].Name).To(Equal("app"))
	})

	It("reports when all resources are deleted", func() {
		Expect(allResourcesDeleted([]model.Resource{
			{Status: types.ResourceStatusDeleted},
			{Status: types.ResourceStatusDeleted},
		})).To(BeTrue())
		Expect(allResourcesDeleted([]model.Resource{
			{Status: types.ResourceStatusDeleted},
			{Status: types.ResourceStatusRunning},
		})).To(BeFalse())
	})

	It("blocks create progression during teardown statuses", func() {
		Expect(resourceStatusBlocksCreateProgression(types.ResourceStatusPendingDeletion)).To(BeTrue())
		Expect(resourceStatusBlocksCreateProgression(types.ResourceStatusDeleting)).To(BeTrue())
		Expect(resourceStatusBlocksCreateProgression(types.ResourceStatusDeleted)).To(BeTrue())
		Expect(resourceStatusBlocksCreateProgression(types.ResourceStatusFailed)).To(BeTrue())
		Expect(resourceStatusBlocksCreateProgression(types.ResourceStatusRunning)).To(BeFalse())
		Expect(resourceStatusBlocksCreateProgression(types.ResourceStatusPending)).To(BeFalse())
	})
})
