//go:build subsystem

package subsystem_test

import (
	"context"
	"net/http"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	v1alpha1 "github.com/dcm-project/control-plane/api/catalog/v1alpha1"
	"github.com/dcm-project/control-plane/internal/catalog/testutil"
	"github.com/google/uuid"
)

var _ = Describe("CatalogItem API", func() {
	BeforeEach(func() {
		resetWireMock()
	})

	Describe("Create", func() {
		It("creates with valid spec and returns 201 with auto-generated uid and correct path", func() {
			id := "ci-create-" + uuid.NewString()[:8]
			item := createTestCatalogItem(id, "Test Item", "vm", nil)

			Expect(item.Uid).NotTo(BeNil())
			Expect(*item.Uid).To(Equal(id))
			Expect(item.Path).NotTo(BeNil())
			Expect(*item.Path).To(Equal("catalog-items/" + id))
			Expect(*item.DisplayName).To(Equal("Test Item"))
			Expect(item.Spec.Resources[0].ServiceType).To(Equal("vm"))
		})

		It("uses user-specified ID when provided", func() {
			id := "my-custom-id-" + uuid.NewString()[:8]
			item := createTestCatalogItem(id, "Custom ID Item", "vm", nil)

			Expect(*item.Uid).To(Equal(id))
			Expect(*item.Path).To(Equal("catalog-items/" + id))
		})

		It("returns 409 on duplicate ID", func() {
			id := "ci-dup-" + uuid.NewString()[:8]
			createTestCatalogItem(id, "First", "vm", nil)

			params := &v1alpha1.CreateCatalogItemParams{Id: &id}
			body := v1alpha1.CatalogItem{
				ApiVersion:  stringPtr("v1alpha1"),
				DisplayName: stringPtr("Second"),
				Spec:        testutil.PtrCatalogSpec("vm", []v1alpha1.FieldConfiguration{defaultField()}),
			}
			resp, err := apiClient.CreateCatalogItemWithResponse(context.Background(), params, body)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode()).To(Equal(http.StatusConflict))
			Expect(resp.JSON409).NotTo(BeNil())
		})

		It("returns 400 for non-existent service type", func() {
			id := "ci-badst-" + uuid.NewString()[:8]
			params := &v1alpha1.CreateCatalogItemParams{Id: &id}
			body := v1alpha1.CatalogItem{
				ApiVersion:  stringPtr("v1alpha1"),
				DisplayName: stringPtr("Bad ST"),
				Spec:        testutil.PtrCatalogSpec("nonexistent-service-type", nil),
			}
			resp, err := apiClient.CreateCatalogItemWithResponse(context.Background(), params, body)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode()).To(Equal(http.StatusBadRequest))
		})
	})

	Describe("Get", func() {
		It("returns 200 for existing item", func() {
			id := "ci-get-" + uuid.NewString()[:8]
			createTestCatalogItem(id, "Get Me", "vm", nil)

			resp, err := apiClient.GetCatalogItemWithResponse(context.Background(), id)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode()).To(Equal(http.StatusOK))
			Expect(resp.JSON200).NotTo(BeNil())
			Expect(*resp.JSON200.Uid).To(Equal(id))
			Expect(*resp.JSON200.DisplayName).To(Equal("Get Me"))
		})

		It("returns 404 for non-existent item", func() {
			resp, err := apiClient.GetCatalogItemWithResponse(context.Background(), "does-not-exist")
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode()).To(Equal(http.StatusNotFound))
		})
	})

	Describe("List", func() {
		It("returns seeded pet-clinic plus created items", func() {
			id := "ci-list-" + uuid.NewString()[:8]
			createTestCatalogItem(id, "Listed Item", "vm", nil)

			maxPage := int32(1000)
			resp, err := apiClient.ListCatalogItemsWithResponse(context.Background(), &v1alpha1.ListCatalogItemsParams{
				MaxPageSize: &maxPage,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode()).To(Equal(http.StatusOK))
			Expect(resp.JSON200).NotTo(BeNil())

			uids := make([]string, len(resp.JSON200.Results))
			for i, item := range resp.JSON200.Results {
				uids[i] = *item.Uid
			}
			Expect(uids).To(ContainElement("pet-clinic"))
			Expect(uids).To(ContainElement(id))
		})

		It("filters by service_type", func() {
			id := "ci-filter-" + uuid.NewString()[:8]
			createTestCatalogItem(id, "Filtered", "database", nil)

			st := "database"
			resp, err := apiClient.ListCatalogItemsWithResponse(context.Background(), &v1alpha1.ListCatalogItemsParams{
				ServiceType: &st,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode()).To(Equal(http.StatusOK))
			Expect(resp.JSON200).NotTo(BeNil())

			for _, item := range resp.JSON200.Results {
				Expect(item.Spec.Resources[0].ServiceType).To(Equal("database"))
			}
			uids := make([]string, len(resp.JSON200.Results))
			for i, item := range resp.JSON200.Results {
				uids[i] = *item.Uid
			}
			Expect(uids).To(ContainElement(id))
		})
	})

	Describe("Update", func() {
		It("updates display_name and returns 200", func() {
			id := "ci-update-" + uuid.NewString()[:8]
			createTestCatalogItem(id, "Original Name", "vm", nil)

			displayName := "Updated Name"
			updateBody := v1alpha1.CatalogItem{
				DisplayName: &displayName,
			}
			resp, err := apiClient.UpdateCatalogItemWithApplicationMergePatchPlusJSONBodyWithResponse(
				context.Background(), id, updateBody,
			)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode()).To(Equal(http.StatusOK))
			Expect(resp.JSON200).NotTo(BeNil())
			Expect(*resp.JSON200.DisplayName).To(Equal("Updated Name"))

			// Verify via GET
			getResp, err := apiClient.GetCatalogItemWithResponse(context.Background(), id)
			Expect(err).NotTo(HaveOccurred())
			Expect(*getResp.JSON200.DisplayName).To(Equal("Updated Name"))
		})

		It("returns 404 for non-existent item", func() {
			displayName := "Updated Name"
			updateBody := v1alpha1.CatalogItem{
				DisplayName: &displayName,
			}
			resp, err := apiClient.UpdateCatalogItemWithApplicationMergePatchPlusJSONBodyWithResponse(
				context.Background(), "does-not-exist", updateBody,
			)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode()).To(Equal(http.StatusNotFound))
		})

		It("returns 400 when changing immutable service_type", func() {
			id := "ci-immutable-" + uuid.NewString()[:8]
			createTestCatalogItem(id, "Immutable ST", "vm", nil)

			spec := testutil.CatalogSpec("database", nil)
			updateBody := v1alpha1.CatalogItem{
				Spec: &spec,
			}
			resp, err := apiClient.UpdateCatalogItemWithApplicationMergePatchPlusJSONBodyWithResponse(
				context.Background(), id, updateBody,
			)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode()).To(Equal(http.StatusBadRequest))
		})
	})

	Describe("Delete", func() {
		It("returns 204 and item is gone", func() {
			id := "ci-delete-" + uuid.NewString()[:8]
			createTestCatalogItem(id, "Delete Me", "vm", nil)

			resp, err := apiClient.DeleteCatalogItemWithResponse(context.Background(), id)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode()).To(Equal(http.StatusNoContent))

			// Verify it's gone
			getResp, err := apiClient.GetCatalogItemWithResponse(context.Background(), id)
			Expect(err).NotTo(HaveOccurred())
			Expect(getResp.StatusCode()).To(Equal(http.StatusNotFound))
		})

		It("returns 404 for non-existent item", func() {
			resp, err := apiClient.DeleteCatalogItemWithResponse(context.Background(), "does-not-exist")
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode()).To(Equal(http.StatusNotFound))
		})

		It("returns 409 when item has instances", func() {
			stubPMCreateResource()

			id := "ci-has-inst-" + uuid.NewString()[:8]
			createTestCatalogItem(id, "Has Instances", "vm", nil)

			// Create an instance
			instID := "inst-" + uuid.NewString()[:8]
			instParams := &v1alpha1.CreateCatalogItemInstanceParams{Id: &instID}
			instBody := v1alpha1.CatalogItemInstance{
				ApiVersion:  "v1alpha1",
				DisplayName: "Test Instance",
				Spec: v1alpha1.CatalogItemInstanceSpec{
					CatalogItemId: id,
					UserValues:    []v1alpha1.UserValue{},
				},
			}
			instResp, err := apiClient.CreateCatalogItemInstanceWithResponse(context.Background(), instParams, instBody)
			Expect(err).NotTo(HaveOccurred())
			Expect(instResp.StatusCode()).To(Equal(http.StatusCreated))

			// Try to delete catalog item
			resp, err := apiClient.DeleteCatalogItemWithResponse(context.Background(), id)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode()).To(Equal(http.StatusConflict))
			Expect(resp.JSON409).NotTo(BeNil())
		})
	})
})
