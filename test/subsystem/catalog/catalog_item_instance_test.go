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

var _ = Describe("CatalogItemInstance API", func() {
	var catalogItemID string

	BeforeEach(func() {
		resetWireMock()
		stubPMCreateResource()
		stubPMDeleteResource()

		catalogItemID = "ci-inst-" + uuid.NewString()[:8]
		editableTrue := true
		editableFalse := false
		createTestCatalogItem(catalogItemID, "Instance Test Item", "vm", []v1alpha1.FieldConfiguration{
			{
				Path:        "vcpu.count",
				DisplayName: stringPtr("vCPU Count"),
				Editable:    &editableTrue,
				Default:     float64(2),
				ValidationSchema: &map[string]any{
					"type":    "number",
					"minimum": float64(1),
					"maximum": float64(16),
				},
			},
			{
				Path:        "memory.size_gb",
				DisplayName: stringPtr("Memory GB"),
				Editable:    &editableFalse,
				Default:     float64(4),
			},
		})
	})

	Describe("Create", func() {
		It("creates with valid spec and returns 201, PM.CreateRun called once", func() {
			instID := "inst-create-" + uuid.NewString()[:8]
			params := &v1alpha1.CreateCatalogItemInstanceParams{Id: &instID}
			body := v1alpha1.CatalogItemInstance{
				ApiVersion:  "v1alpha1",
				DisplayName: "My Instance",
				Spec: v1alpha1.CatalogItemInstanceSpec{
					CatalogItemId: catalogItemID,
					UserValues: []v1alpha1.UserValue{
						{Resource: testutil.DefaultResourceName, Path: "vcpu.count", Value: float64(4)},
					},
				},
			}

			resp, err := apiClient.CreateCatalogItemInstanceWithResponse(context.Background(), params, body)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode()).To(Equal(http.StatusCreated))
			Expect(resp.JSON201).NotTo(BeNil())
			Expect(*resp.JSON201.Uid).To(Equal(instID))
			Expect(*resp.JSON201.Path).To(Equal("catalog-item-instances/" + instID))
			Expect(resp.JSON201.RunId).NotTo(BeNil())
			Expect(*resp.JSON201.RunId).To(MatchRegexp(`^[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}$`))

			verifyPMCreateResourceCalled(1)
		})

		It("uses user-specified ID", func() {
			instID := "my-custom-inst-" + uuid.NewString()[:8]
			params := &v1alpha1.CreateCatalogItemInstanceParams{Id: &instID}
			body := v1alpha1.CatalogItemInstance{
				ApiVersion:  "v1alpha1",
				DisplayName: "Custom ID Instance",
				Spec: v1alpha1.CatalogItemInstanceSpec{
					CatalogItemId: catalogItemID,
					UserValues:    []v1alpha1.UserValue{},
				},
			}

			resp, err := apiClient.CreateCatalogItemInstanceWithResponse(context.Background(), params, body)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode()).To(Equal(http.StatusCreated))
			Expect(*resp.JSON201.Uid).To(Equal(instID))
			Expect(resp.JSON201.RunId).NotTo(BeNil())
			Expect(*resp.JSON201.RunId).To(MatchRegexp(`^[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}$`))
		})

		It("returns 400 for non-existent catalog_item_id", func() {
			instID := "inst-bad-ci-" + uuid.NewString()[:8]
			params := &v1alpha1.CreateCatalogItemInstanceParams{Id: &instID}
			body := v1alpha1.CatalogItemInstance{
				ApiVersion:  "v1alpha1",
				DisplayName: "Bad Catalog Item",
				Spec: v1alpha1.CatalogItemInstanceSpec{
					CatalogItemId: "nonexistent-catalog-item",
					UserValues:    []v1alpha1.UserValue{},
				},
			}

			resp, err := apiClient.CreateCatalogItemInstanceWithResponse(context.Background(), params, body)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode()).To(Equal(http.StatusBadRequest))
			verifyPMCreateResourceCalled(0)
		})

		It("returns 400 for user_value path not found in catalog item fields", func() {
			instID := "inst-bad-path-" + uuid.NewString()[:8]
			params := &v1alpha1.CreateCatalogItemInstanceParams{Id: &instID}
			body := v1alpha1.CatalogItemInstance{
				ApiVersion:  "v1alpha1",
				DisplayName: "Bad Path",
				Spec: v1alpha1.CatalogItemInstanceSpec{
					CatalogItemId: catalogItemID,
					UserValues: []v1alpha1.UserValue{
						{Resource: testutil.DefaultResourceName, Path: "nonexistent.path", Value: "bad"},
					},
				},
			}

			resp, err := apiClient.CreateCatalogItemInstanceWithResponse(context.Background(), params, body)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode()).To(Equal(http.StatusBadRequest))
			verifyPMCreateResourceCalled(0)
		})

		It("returns 400 for non-editable field override", func() {
			instID := "inst-non-edit-" + uuid.NewString()[:8]
			params := &v1alpha1.CreateCatalogItemInstanceParams{Id: &instID}
			body := v1alpha1.CatalogItemInstance{
				ApiVersion:  "v1alpha1",
				DisplayName: "Non-Editable Override",
				Spec: v1alpha1.CatalogItemInstanceSpec{
					CatalogItemId: catalogItemID,
					UserValues: []v1alpha1.UserValue{
						{Resource: testutil.DefaultResourceName, Path: "memory.size_gb", Value: float64(8)},
					},
				},
			}

			resp, err := apiClient.CreateCatalogItemInstanceWithResponse(context.Background(), params, body)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode()).To(Equal(http.StatusBadRequest))
			verifyPMCreateResourceCalled(0)
		})

		It("returns 400 for validation_schema failure", func() {
			instID := "inst-val-fail-" + uuid.NewString()[:8]
			params := &v1alpha1.CreateCatalogItemInstanceParams{Id: &instID}
			body := v1alpha1.CatalogItemInstance{
				ApiVersion:  "v1alpha1",
				DisplayName: "Validation Failure",
				Spec: v1alpha1.CatalogItemInstanceSpec{
					CatalogItemId: catalogItemID,
					UserValues: []v1alpha1.UserValue{
						{Resource: testutil.DefaultResourceName, Path: "vcpu.count", Value: float64(99)}, // exceeds maximum of 16
					},
				},
			}

			resp, err := apiClient.CreateCatalogItemInstanceWithResponse(context.Background(), params, body)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode()).To(Equal(http.StatusBadRequest))
			verifyPMCreateResourceCalled(0)
		})

		It("returns 400 for depends_on violation using seeded pet-clinic", func() {
			instID := "inst-depends-" + uuid.NewString()[:8]
			params := &v1alpha1.CreateCatalogItemInstanceParams{Id: &instID}
			body := v1alpha1.CatalogItemInstance{
				ApiVersion:  "v1alpha1",
				DisplayName: "DependsOn Violation",
				Spec: v1alpha1.CatalogItemInstanceSpec{
					CatalogItemId: "pet-clinic",
					UserValues: []v1alpha1.UserValue{
						{Resource: "app", Path: "database.engine", Value: "postgres"},
						{Resource: "app", Path: "database.version", Value: "8.4"}, // 8.4 is only allowed for mysql, not postgres
					},
				},
			}

			resp, err := apiClient.CreateCatalogItemInstanceWithResponse(context.Background(), params, body)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode()).To(Equal(http.StatusBadRequest))
			verifyPMCreateResourceCalled(0)
		})
	})

	Describe("Get", func() {
		It("returns 200 for existing instance", func() {
			instID := "inst-get-" + uuid.NewString()[:8]
			params := &v1alpha1.CreateCatalogItemInstanceParams{Id: &instID}
			body := v1alpha1.CatalogItemInstance{
				ApiVersion:  "v1alpha1",
				DisplayName: "Get Instance",
				Spec: v1alpha1.CatalogItemInstanceSpec{
					CatalogItemId: catalogItemID,
					UserValues:    []v1alpha1.UserValue{},
				},
			}
			createResp, err := apiClient.CreateCatalogItemInstanceWithResponse(context.Background(), params, body)
			Expect(err).NotTo(HaveOccurred())
			Expect(createResp.StatusCode()).To(Equal(http.StatusCreated))

			resp, err := apiClient.GetCatalogItemInstanceWithResponse(context.Background(), instID)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode()).To(Equal(http.StatusOK))
			Expect(resp.JSON200).NotTo(BeNil())
			Expect(*resp.JSON200.Uid).To(Equal(instID))
			Expect(resp.JSON200.DisplayName).To(Equal("Get Instance"))
			Expect(resp.JSON200.RunId).NotTo(BeNil())
			Expect(*resp.JSON200.RunId).To(Equal(*createResp.JSON201.RunId))
		})

		It("returns 404 for non-existent instance", func() {
			resp, err := apiClient.GetCatalogItemInstanceWithResponse(context.Background(), "does-not-exist")
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode()).To(Equal(http.StatusNotFound))
		})
	})

	Describe("List", func() {
		It("returns created instances", func() {
			instID := "inst-list-" + uuid.NewString()[:8]
			params := &v1alpha1.CreateCatalogItemInstanceParams{Id: &instID}
			body := v1alpha1.CatalogItemInstance{
				ApiVersion:  "v1alpha1",
				DisplayName: "Listed Instance",
				Spec: v1alpha1.CatalogItemInstanceSpec{
					CatalogItemId: catalogItemID,
					UserValues:    []v1alpha1.UserValue{},
				},
			}
			createResp, err := apiClient.CreateCatalogItemInstanceWithResponse(context.Background(), params, body)
			Expect(err).NotTo(HaveOccurred())
			Expect(createResp.StatusCode()).To(Equal(http.StatusCreated))

			resp, err := apiClient.ListCatalogItemInstancesWithResponse(context.Background(), &v1alpha1.ListCatalogItemInstancesParams{})
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode()).To(Equal(http.StatusOK))
			Expect(resp.JSON200).NotTo(BeNil())

			uids := make([]string, len(resp.JSON200.Results))
			for i, inst := range resp.JSON200.Results {
				uids[i] = *inst.Uid
			}
			Expect(uids).To(ContainElement(instID))
		})

		It("paginates results", func() {
			// Create 3 instances for this catalog item
			var createdIDs []string
			for range 6 {
				instID := "inst-page-" + uuid.NewString()[:8]
				createdIDs = append(createdIDs, instID)
				p := &v1alpha1.CreateCatalogItemInstanceParams{Id: &instID}
				b := v1alpha1.CatalogItemInstance{
					ApiVersion:  "v1alpha1",
					DisplayName: "Paginated Instance",
					Spec: v1alpha1.CatalogItemInstanceSpec{
						CatalogItemId: catalogItemID,
						UserValues:    []v1alpha1.UserValue{},
					},
				}
				r, err := apiClient.CreateCatalogItemInstanceWithResponse(context.Background(), p, b)
				Expect(err).NotTo(HaveOccurred())
				Expect(r.StatusCode()).To(Equal(http.StatusCreated))
			}

			var allUIDs []string
			readBlockSizes := []int32{2, 3, 4}
			var token *string
			for i, pageSize := range readBlockSizes {
				resp, err := apiClient.ListCatalogItemInstancesWithResponse(context.Background(), &v1alpha1.ListCatalogItemInstancesParams{
					CatalogItemId: &catalogItemID,
					MaxPageSize:   &pageSize,
					PageToken:     token,
				})
				Expect(err).NotTo(HaveOccurred())
				Expect(resp.StatusCode()).To(Equal(http.StatusOK))
				Expect(resp.JSON200).NotTo(BeNil())
				if i == len(readBlockSizes)-1 {
					Expect(resp.JSON200.NextPageToken).To(BeEmpty())
					Expect(resp.JSON200.Results).To(HaveLen(1))
				} else {
					Expect(resp.JSON200.NextPageToken).NotTo(BeEmpty())
					Expect(resp.JSON200.Results).To(HaveLen(int(pageSize)))
				}
				for _, inst := range resp.JSON200.Results {
					allUIDs = append(allUIDs, *inst.Uid)
				}
				token = stringPtr(resp.JSON200.NextPageToken)
			}

			for _, id := range createdIDs {
				Expect(allUIDs).To(ContainElement(id))
			}
		})

		It("filters by catalog_item_id", func() {
			// Create a second catalog item
			otherCI := "ci-other-" + uuid.NewString()[:8]
			createTestCatalogItem(otherCI, "Other CI", "vm", nil)

			instID1 := "inst-f1-" + uuid.NewString()[:8]
			instID2 := "inst-f2-" + uuid.NewString()[:8]

			for _, pair := range []struct {
				id, ciID string
			}{
				{instID1, catalogItemID},
				{instID2, otherCI},
			} {
				p := &v1alpha1.CreateCatalogItemInstanceParams{Id: &pair.id}
				b := v1alpha1.CatalogItemInstance{
					ApiVersion:  "v1alpha1",
					DisplayName: "Filter Test",
					Spec: v1alpha1.CatalogItemInstanceSpec{
						CatalogItemId: pair.ciID,
						UserValues:    []v1alpha1.UserValue{},
					},
				}
				r, err := apiClient.CreateCatalogItemInstanceWithResponse(context.Background(), p, b)
				Expect(err).NotTo(HaveOccurred())
				Expect(r.StatusCode()).To(Equal(http.StatusCreated))
			}

			resp, err := apiClient.ListCatalogItemInstancesWithResponse(context.Background(), &v1alpha1.ListCatalogItemInstancesParams{
				CatalogItemId: &catalogItemID,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode()).To(Equal(http.StatusOK))

			for _, inst := range resp.JSON200.Results {
				Expect(inst.Spec.CatalogItemId).To(Equal(catalogItemID))
			}
			uids := make([]string, len(resp.JSON200.Results))
			for i, inst := range resp.JSON200.Results {
				uids[i] = *inst.Uid
			}
			Expect(uids).To(ContainElement(instID1))
			Expect(uids).NotTo(ContainElement(instID2))
		})
	})

	Describe("Delete", func() {
		It("returns 204 and calls PM.DeleteResource", func() {
			instID := "inst-del-" + uuid.NewString()[:8]
			params := &v1alpha1.CreateCatalogItemInstanceParams{Id: &instID}
			body := v1alpha1.CatalogItemInstance{
				ApiVersion:  "v1alpha1",
				DisplayName: "Delete Instance",
				Spec: v1alpha1.CatalogItemInstanceSpec{
					CatalogItemId: catalogItemID,
					UserValues:    []v1alpha1.UserValue{},
				},
			}
			createResp, err := apiClient.CreateCatalogItemInstanceWithResponse(context.Background(), params, body)
			Expect(err).NotTo(HaveOccurred())
			Expect(createResp.StatusCode()).To(Equal(http.StatusCreated))

			resp, err := apiClient.DeleteCatalogItemInstanceWithResponse(context.Background(), instID)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode()).To(Equal(http.StatusNoContent))

			verifyPMDeleteResourceCalled(1)

			// Verify it's gone
			getResp, err := apiClient.GetCatalogItemInstanceWithResponse(context.Background(), instID)
			Expect(err).NotTo(HaveOccurred())
			Expect(getResp.StatusCode()).To(Equal(http.StatusNotFound))
		})

		It("returns 404 for non-existent instance", func() {
			resp, err := apiClient.DeleteCatalogItemInstanceWithResponse(context.Background(), "does-not-exist")
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode()).To(Equal(http.StatusNotFound))
		})
	})

	Describe("Rehydrate", func() {
		BeforeEach(func() {
			stubPMRehydrateResource()
		})

		It("returns 200 with updated run_id", func() {
			instID := "inst-rehy-" + uuid.NewString()[:8]
			params := &v1alpha1.CreateCatalogItemInstanceParams{Id: &instID}
			body := v1alpha1.CatalogItemInstance{
				ApiVersion:  "v1alpha1",
				DisplayName: "Rehydrate Instance",
				Spec: v1alpha1.CatalogItemInstanceSpec{
					CatalogItemId: catalogItemID,
					UserValues:    []v1alpha1.UserValue{},
				},
			}
			createResp, err := apiClient.CreateCatalogItemInstanceWithResponse(context.Background(), params, body)
			Expect(err).NotTo(HaveOccurred())
			Expect(createResp.StatusCode()).To(Equal(http.StatusCreated))
			oldRunID := *createResp.JSON201.RunId

			resp, err := apiClient.RehydrateCatalogItemInstanceWithResponse(context.Background(), instID)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode()).To(Equal(http.StatusOK))
			Expect(resp.JSON200).NotTo(BeNil())
			Expect(*resp.JSON200.Uid).To(Equal(instID))
			Expect(resp.JSON200.RunId).NotTo(BeNil())
			Expect(*resp.JSON200.RunId).NotTo(Equal(oldRunID))
			Expect(*resp.JSON200.RunId).To(MatchRegexp(`^[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}$`))

			verifyPMRehydrateResourceCalled(1)

			// Verify run_id is persisted
			getResp, err := apiClient.GetCatalogItemInstanceWithResponse(context.Background(), instID)
			Expect(err).NotTo(HaveOccurred())
			Expect(getResp.StatusCode()).To(Equal(http.StatusOK))
			Expect(*getResp.JSON200.RunId).To(Equal(*resp.JSON200.RunId))
		})

		It("returns 404 for non-existent instance", func() {
			resp, err := apiClient.RehydrateCatalogItemInstanceWithResponse(context.Background(), "does-not-exist")
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode()).To(Equal(http.StatusNotFound))
		})

		It("returns 406 when PM rehydrate returns policy rejected, run_id unchanged", func() {
			instID := "inst-rehy-policy-" + uuid.NewString()[:8]
			params := &v1alpha1.CreateCatalogItemInstanceParams{Id: &instID}
			body := v1alpha1.CatalogItemInstance{
				ApiVersion:  "v1alpha1",
				DisplayName: "Rehydrate Policy Rejected",
				Spec: v1alpha1.CatalogItemInstanceSpec{
					CatalogItemId: catalogItemID,
					UserValues:    []v1alpha1.UserValue{},
				},
			}
			createResp, err := apiClient.CreateCatalogItemInstanceWithResponse(context.Background(), params, body)
			Expect(err).NotTo(HaveOccurred())
			Expect(createResp.StatusCode()).To(Equal(http.StatusCreated))
			oldRunID := *createResp.JSON201.RunId

			// Reset WireMock and stub rehydrate as policy rejected
			resetWireMock()
			stubPMCreateResource()
			stubPMDeleteResource()
			stubPMRehydrateResourcePolicyRejected()

			resp, err := apiClient.RehydrateCatalogItemInstanceWithResponse(context.Background(), instID)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode()).To(Equal(http.StatusNotAcceptable))

			// Verify run_id is unchanged
			getResp, err := apiClient.GetCatalogItemInstanceWithResponse(context.Background(), instID)
			Expect(err).NotTo(HaveOccurred())
			Expect(getResp.StatusCode()).To(Equal(http.StatusOK))
			Expect(*getResp.JSON200.RunId).To(Equal(oldRunID))
		})

		It("returns 422 when PM rehydrate returns provider error, run_id unchanged", func() {
			instID := "inst-rehy-provider-" + uuid.NewString()[:8]
			params := &v1alpha1.CreateCatalogItemInstanceParams{Id: &instID}
			body := v1alpha1.CatalogItemInstance{
				ApiVersion:  "v1alpha1",
				DisplayName: "Rehydrate Provider Error",
				Spec: v1alpha1.CatalogItemInstanceSpec{
					CatalogItemId: catalogItemID,
					UserValues:    []v1alpha1.UserValue{},
				},
			}
			createResp, err := apiClient.CreateCatalogItemInstanceWithResponse(context.Background(), params, body)
			Expect(err).NotTo(HaveOccurred())
			Expect(createResp.StatusCode()).To(Equal(http.StatusCreated))
			oldRunID := *createResp.JSON201.RunId

			// Reset WireMock and stub rehydrate as provider error
			resetWireMock()
			stubPMCreateResource()
			stubPMDeleteResource()
			stubPMRehydrateResourceProviderError()

			resp, err := apiClient.RehydrateCatalogItemInstanceWithResponse(context.Background(), instID)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode()).To(Equal(http.StatusUnprocessableEntity))

			// Verify run_id is unchanged
			getResp, err := apiClient.GetCatalogItemInstanceWithResponse(context.Background(), instID)
			Expect(err).NotTo(HaveOccurred())
			Expect(getResp.StatusCode()).To(Equal(http.StatusOK))
			Expect(*getResp.JSON200.RunId).To(Equal(oldRunID))
		})

		It("returns 500 when PM rehydrate fails, run_id unchanged", func() {
			instID := "inst-rehy-fail-" + uuid.NewString()[:8]
			params := &v1alpha1.CreateCatalogItemInstanceParams{Id: &instID}
			body := v1alpha1.CatalogItemInstance{
				ApiVersion:  "v1alpha1",
				DisplayName: "Rehydrate Failure",
				Spec: v1alpha1.CatalogItemInstanceSpec{
					CatalogItemId: catalogItemID,
					UserValues:    []v1alpha1.UserValue{},
				},
			}
			createResp, err := apiClient.CreateCatalogItemInstanceWithResponse(context.Background(), params, body)
			Expect(err).NotTo(HaveOccurred())
			Expect(createResp.StatusCode()).To(Equal(http.StatusCreated))
			oldRunID := *createResp.JSON201.RunId

			// Reset WireMock and stub rehydrate as failure
			resetWireMock()
			stubPMCreateResource()
			stubPMDeleteResource()
			stubPMRehydrateResourceFailure()

			resp, err := apiClient.RehydrateCatalogItemInstanceWithResponse(context.Background(), instID)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode()).To(Equal(http.StatusInternalServerError))

			// Verify run_id is unchanged
			getResp, err := apiClient.GetCatalogItemInstanceWithResponse(context.Background(), instID)
			Expect(err).NotTo(HaveOccurred())
			Expect(getResp.StatusCode()).To(Equal(http.StatusOK))
			Expect(*getResp.JSON200.RunId).To(Equal(oldRunID))
		})
	})

	Describe("PlacementManager failures", func() {
		It("returns 406 when PM create returns policy rejected, instance not persisted", func() {
			resetWireMock()
			stubPMCreateResourcePolicyRejected()

			instID := "inst-pm-policy-" + uuid.NewString()[:8]
			params := &v1alpha1.CreateCatalogItemInstanceParams{Id: &instID}
			body := v1alpha1.CatalogItemInstance{
				ApiVersion:  "v1alpha1",
				DisplayName: "PM Policy Rejected",
				Spec: v1alpha1.CatalogItemInstanceSpec{
					CatalogItemId: catalogItemID,
					UserValues:    []v1alpha1.UserValue{},
				},
			}

			resp, err := apiClient.CreateCatalogItemInstanceWithResponse(context.Background(), params, body)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode()).To(Equal(http.StatusNotAcceptable))

			// Verify instance was not persisted
			getResp, err := apiClient.GetCatalogItemInstanceWithResponse(context.Background(), instID)
			Expect(err).NotTo(HaveOccurred())
			Expect(getResp.StatusCode()).To(Equal(http.StatusNotFound))
		})

		It("returns 422 when PM create returns provider error, instance not persisted", func() {
			resetWireMock()
			stubPMCreateResourceProviderError()

			instID := "inst-pm-provider-" + uuid.NewString()[:8]
			params := &v1alpha1.CreateCatalogItemInstanceParams{Id: &instID}
			body := v1alpha1.CatalogItemInstance{
				ApiVersion:  "v1alpha1",
				DisplayName: "PM Provider Error",
				Spec: v1alpha1.CatalogItemInstanceSpec{
					CatalogItemId: catalogItemID,
					UserValues:    []v1alpha1.UserValue{},
				},
			}

			resp, err := apiClient.CreateCatalogItemInstanceWithResponse(context.Background(), params, body)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode()).To(Equal(http.StatusUnprocessableEntity))

			// Verify instance was not persisted
			getResp, err := apiClient.GetCatalogItemInstanceWithResponse(context.Background(), instID)
			Expect(err).NotTo(HaveOccurred())
			Expect(getResp.StatusCode()).To(Equal(http.StatusNotFound))
		})

		It("returns 500 when PM create fails, instance not persisted", func() {
			resetWireMock()
			stubPMCreateResourceFailure()

			instID := "inst-pm-fail-" + uuid.NewString()[:8]
			params := &v1alpha1.CreateCatalogItemInstanceParams{Id: &instID}
			body := v1alpha1.CatalogItemInstance{
				ApiVersion:  "v1alpha1",
				DisplayName: "PM Failure",
				Spec: v1alpha1.CatalogItemInstanceSpec{
					CatalogItemId: catalogItemID,
					UserValues:    []v1alpha1.UserValue{},
				},
			}

			resp, err := apiClient.CreateCatalogItemInstanceWithResponse(context.Background(), params, body)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode()).To(Equal(http.StatusInternalServerError))

			// Verify instance was not persisted
			getResp, err := apiClient.GetCatalogItemInstanceWithResponse(context.Background(), instID)
			Expect(err).NotTo(HaveOccurred())
			Expect(getResp.StatusCode()).To(Equal(http.StatusNotFound))
		})

		It("returns 500 when PM delete fails, instance still exists", func() {
			// First create successfully
			instID := "inst-pm-del-fail-" + uuid.NewString()[:8]
			params := &v1alpha1.CreateCatalogItemInstanceParams{Id: &instID}
			body := v1alpha1.CatalogItemInstance{
				ApiVersion:  "v1alpha1",
				DisplayName: "PM Delete Failure",
				Spec: v1alpha1.CatalogItemInstanceSpec{
					CatalogItemId: catalogItemID,
					UserValues:    []v1alpha1.UserValue{},
				},
			}
			createResp, err := apiClient.CreateCatalogItemInstanceWithResponse(context.Background(), params, body)
			Expect(err).NotTo(HaveOccurred())
			Expect(createResp.StatusCode()).To(Equal(http.StatusCreated))

			// Now reset WireMock and stub delete as failure
			resetWireMock()
			stubPMDeleteResourceFailure()

			resp, err := apiClient.DeleteCatalogItemInstanceWithResponse(context.Background(), instID)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode()).To(Equal(http.StatusInternalServerError))

			// Verify instance still exists
			getResp, err := apiClient.GetCatalogItemInstanceWithResponse(context.Background(), instID)
			Expect(err).NotTo(HaveOccurred())
			Expect(getResp.StatusCode()).To(Equal(http.StatusOK))
		})
	})
})
