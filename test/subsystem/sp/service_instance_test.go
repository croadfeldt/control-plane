//go:build subsystem

package subsystem_test

import (
	"context"
	"net/http"
	"os"
	"time"

	"github.com/dcm-project/control-plane/api/sp/v1alpha1/resource_manager"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Service Instance API", func() {
	var ctx context.Context

	BeforeEach(func() {
		ctx = context.Background()
		resetWireMock()
	})

	Describe("Health Check", func() {
		It("returns healthy status", func() {
			baseURL := os.Getenv("API_URL")
			if baseURL == "" {
				baseURL = "http://localhost:8080/api/v1alpha1"
			}

			resp, err := http.Get(baseURL + "/health")
			Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
		})
	})

	Describe("Create Instance", func() {
		// The direct POST /service-type-instances endpoint never receives an
		// agent_name from the caller - it's resolved upstream by policy - so
		// it always 400s (see internal/sp/handlers/resource_manager and its
		// unit tests). Successful creation is only reachable via the
		// catalog -> placement -> policy -> SPRM agent-routed path, which is
		// what these tests exercise.
		It("creates an instance through the agent-routed path with agent_name populated", func() {
			agentName, instanceID := createInstanceViaAgent()

			getResp, err := rmApiClient.GetInstanceWithResponse(ctx, instanceID, nil)
			Expect(err).NotTo(HaveOccurred())
			Expect(getResp.StatusCode()).To(Equal(http.StatusOK))
			Expect(getResp.JSON200).NotTo(BeNil())
			Expect(getResp.JSON200.AgentName).NotTo(BeNil())
			Expect(*getResp.JSON200.AgentName).To(Equal(agentName))
		})

		It("returns 400 because the direct endpoint never supplies an agent name", func() {
			createResp, err := rmApiClient.CreateInstanceWithResponse(ctx, nil, resource_manager.ServiceTypeInstance{
				Spec: map[string]interface{}{"cpu": 1, "service_type": "vm"},
			})

			Expect(err).NotTo(HaveOccurred())
			Expect(createResp.StatusCode()).To(Equal(http.StatusBadRequest))
		})
	})

	Describe("Get Instance", func() {
		It("returns 200 for existing instance", func() {
			_, instanceID := createInstanceViaAgent()

			getResp, err := rmApiClient.GetInstanceWithResponse(ctx, instanceID, nil)

			Expect(err).NotTo(HaveOccurred())
			Expect(getResp.StatusCode()).To(Equal(http.StatusOK))
			Expect(getResp.JSON200).NotTo(BeNil())
			Expect(*getResp.JSON200.Id).To(Equal(instanceID))
		})

		It("returns 404 for non-existent instance", func() {
			nonExistentID := uuid.New().String()
			getResp, err := rmApiClient.GetInstanceWithResponse(ctx, nonExistentID, nil)

			Expect(err).NotTo(HaveOccurred())
			Expect(getResp.StatusCode()).To(Equal(http.StatusNotFound))
		})
	})

	Describe("List Instances", func() {
		It("returns created instances in the list", func() {
			agentName, instanceID := createInstanceViaAgent()

			listResp, err := rmApiClient.ListInstancesWithResponse(ctx, &resource_manager.ListInstancesParams{AgentName: &agentName})

			Expect(err).NotTo(HaveOccurred())
			Expect(listResp.StatusCode()).To(Equal(http.StatusOK))
			Expect(listResp.JSON200.Instances).NotTo(BeNil())

			ids := make([]string, len(*listResp.JSON200.Instances))
			for i, inst := range *listResp.JSON200.Instances {
				ids[i] = *inst.Id
			}
			Expect(ids).To(ContainElement(instanceID))
		})

		It("respects max page size parameter", func() {
			maxPageSize := 10
			params := &resource_manager.ListInstancesParams{
				MaxPageSize: &maxPageSize,
			}

			listResp, err := rmApiClient.ListInstancesWithResponse(ctx, params)

			Expect(err).NotTo(HaveOccurred())
			Expect(listResp.StatusCode()).To(Equal(http.StatusOK))

			if listResp.JSON200.Instances != nil {
				Expect(len(*listResp.JSON200.Instances)).To(BeNumerically("<=", maxPageSize))
			}
		})

		It("returns 400 for invalid max page size", func() {
			invalidPageSize := 1000
			params := &resource_manager.ListInstancesParams{
				MaxPageSize: &invalidPageSize,
			}

			listResp, err := rmApiClient.ListInstancesWithResponse(ctx, params)

			Expect(err).NotTo(HaveOccurred())
			Expect(listResp.StatusCode()).To(Equal(http.StatusBadRequest))
		})

		It("handles invalid page token gracefully", func() {
			invalidToken := "invalid-base64-token"
			params := &resource_manager.ListInstancesParams{
				PageToken: &invalidToken,
			}

			listResp, err := rmApiClient.ListInstancesWithResponse(ctx, params)
			Expect(err).NotTo(HaveOccurred())
			Expect(listResp.StatusCode()).To(Equal(http.StatusOK))
		})
	})

	Describe("Delete Instance", func() {
		It("returns 204 and instance is removed", func() {
			_, instanceID := createInstanceViaAgent()

			deleteResp, err := rmApiClient.DeleteInstanceWithResponse(ctx, instanceID, nil)

			Expect(err).NotTo(HaveOccurred())
			Expect(deleteResp.StatusCode()).To(Equal(http.StatusNoContent))

			getResp, err := rmApiClient.GetInstanceWithResponse(ctx, instanceID, nil)
			Expect(err).NotTo(HaveOccurred())
			Expect(getResp.StatusCode()).To(Equal(http.StatusNotFound))
		})

		It("returns 404 when deleting non-existent instance", func() {
			nonExistentID := uuid.New().String()
			deleteResp, err := rmApiClient.DeleteInstanceWithResponse(ctx, nonExistentID, nil)

			Expect(err).NotTo(HaveOccurred())
			Expect(deleteResp.StatusCode()).To(Equal(http.StatusNotFound))
		})
	})

	Describe("Deferred Deletion", func() {
		var instID, agentName string

		createInstance := func() {
			agentName, instID = createInstanceViaAgent()
		}

		It("marks instance SCHEDULED on deferred delete", func() {
			createInstance()

			deferred := true
			deleteResp, err := rmApiClient.DeleteInstanceWithResponse(ctx, instID, &resource_manager.DeleteInstanceParams{
				Deferred: &deferred,
			})

			Expect(err).NotTo(HaveOccurred())
			Expect(deleteResp.StatusCode()).To(Equal(http.StatusNoContent))

			showDeleted := true
			getResp, err := rmApiClient.GetInstanceWithResponse(ctx, instID, &resource_manager.GetInstanceParams{
				ShowDeleted: &showDeleted,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(getResp.StatusCode()).To(Equal(http.StatusOK))
			Expect(getResp.JSON200.DeletionStatus).NotTo(BeNil())
			Expect(string(*getResp.JSON200.DeletionStatus)).To(Equal("SCHEDULED"))
		})

		It("excludes soft-deleted instances from default LIST", func() {
			createInstance()

			deferred := true
			deleteResp, err := rmApiClient.DeleteInstanceWithResponse(ctx, instID, &resource_manager.DeleteInstanceParams{
				Deferred: &deferred,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(deleteResp.StatusCode()).To(Equal(http.StatusNoContent))

			listResp, err := rmApiClient.ListInstancesWithResponse(ctx, &resource_manager.ListInstancesParams{AgentName: &agentName})
			Expect(err).NotTo(HaveOccurred())
			Expect(listResp.StatusCode()).To(Equal(http.StatusOK))

			if listResp.JSON200.Instances != nil {
				for _, inst := range *listResp.JSON200.Instances {
					Expect(*inst.Id).NotTo(Equal(instID))
				}
			}
		})

		It("includes soft-deleted instances in LIST with show_deleted=true", func() {
			createInstance()

			deferred := true
			deleteResp, err := rmApiClient.DeleteInstanceWithResponse(ctx, instID, &resource_manager.DeleteInstanceParams{
				Deferred: &deferred,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(deleteResp.StatusCode()).To(Equal(http.StatusNoContent))

			showDeleted := true
			listResp, err := rmApiClient.ListInstancesWithResponse(ctx, &resource_manager.ListInstancesParams{
				ShowDeleted: &showDeleted,
				AgentName:   &agentName,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(listResp.StatusCode()).To(Equal(http.StatusOK))
			Expect(listResp.JSON200.Instances).NotTo(BeNil())

			ids := make([]string, len(*listResp.JSON200.Instances))
			for i, inst := range *listResp.JSON200.Instances {
				ids[i] = *inst.Id
			}
			Expect(ids).To(ContainElement(instID))
		})

		It("can hard-delete a soft-deleted instance", func() {
			createInstance()

			deferred := true
			deleteResp, err := rmApiClient.DeleteInstanceWithResponse(ctx, instID, &resource_manager.DeleteInstanceParams{
				Deferred: &deferred,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(deleteResp.StatusCode()).To(Equal(http.StatusNoContent))

			deleteResp, err = rmApiClient.DeleteInstanceWithResponse(ctx, instID, nil)
			Expect(err).NotTo(HaveOccurred())
			Expect(deleteResp.StatusCode()).To(Equal(http.StatusNoContent))

			// A non-deferred delete of an agent-routed instance only
			// completes once the agent's deletion-acknowledged event
			// arrives (see service_type_instance.go's DeleteInstance /
			// consumer.ResponseConsumer.handleDeletionAcknowledged); with
			// no live agent in this stack, simulate that confirmation.
			acknowledgeDeletion(instID, agentName)

			showDeleted := true
			Eventually(func(g Gomega) int {
				getResp, err := rmApiClient.GetInstanceWithResponse(ctx, instID, &resource_manager.GetInstanceParams{
					ShowDeleted: &showDeleted,
				})
				g.Expect(err).NotTo(HaveOccurred())
				return getResp.StatusCode()
			}).WithTimeout(10 * time.Second).WithPolling(500 * time.Millisecond).Should(Equal(http.StatusNotFound))
		})
	})
})
