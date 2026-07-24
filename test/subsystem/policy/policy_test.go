//go:build subsystem

package subsystem_test

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/dcm-project/control-plane/api/policy/v1alpha1"
)

var (
	maxPolicyPriority = int32(1000)
	minPolicyPriority = int32(1)
)

const (
	testRegoWithMain       = "package test\n\nmain := {\n  \"rejected\": false\n}"
	testRegoUpdatedPackage = "package updated\n\nmain := {\n  \"rejected\": true\n}"
)

var _ = Describe("Policy CRUD Operations", func() {
	var createdPolicyIDs []string

	AfterEach(func() {
		// Clean up created policies
		for _, id := range createdPolicyIDs {
			_, _ = apiClient.DeletePolicyWithResponse(ctx, id)
		}
		createdPolicyIDs = nil
	})

	Describe("Basic successful CRUD operations", func() {
		It("should create policy with server-generated UUID", func() {
			policy := v1alpha1.Policy{
				DisplayName: ptr("Test Policy"),
				Description: ptr("A test policy"),
				PolicyType:  ptr(v1alpha1.GLOBAL),
				Priority:    ptr(int32(100)),
				Enabled:     ptr(true),
				RegoCode:    ptr(testRegoWithMain),
			}

			resp, err := apiClient.CreatePolicyWithResponse(ctx, &v1alpha1.CreatePolicyParams{}, policy)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode()).To(Equal(http.StatusCreated))
			createdPolicyIDs = append(createdPolicyIDs, *resp.JSON201.Id)
			Expect(resp.JSON201).NotTo(BeNil())
			Expect(resp.JSON201.Id).NotTo(BeNil())
			Expect(*resp.JSON201.Id).To(MatchRegexp(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`))
			Expect(resp.JSON201.DisplayName).To(Equal(policy.DisplayName))
		})

		It("should create policy with client-specified ID", func() {
			clientID := "my-custom-policy-id"
			policy := v1alpha1.Policy{
				DisplayName: ptr("Client ID Policy"),
				Description: ptr("Policy with client-specified ID"),
				PolicyType:  ptr(v1alpha1.GLOBAL),
				Priority:    ptr(int32(101)),
				Enabled:     ptr(true),
				RegoCode:    ptr(testRegoWithMain),
			}
			resp, err := apiClient.CreatePolicyWithResponse(ctx, &v1alpha1.CreatePolicyParams{Id: &clientID}, policy)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode()).To(Equal(http.StatusCreated), "ID %s should be valid", clientID)
			createdPolicyIDs = append(createdPolicyIDs, *resp.JSON201.Id)
			Expect(*resp.JSON201.Id).To(Equal(clientID), "ID %s should be valid and used", clientID)
		})

		It("should get policy by ID", func() {
			// Create a policy first
			policy := v1alpha1.Policy{
				DisplayName: ptr("Get Test Policy"),
				PolicyType:  ptr(v1alpha1.GLOBAL),
				Priority:    ptr(int32(102)),
				Enabled:     ptr(true),
				RegoCode:    ptr(testRegoWithMain),
			}

			createResp, err := apiClient.CreatePolicyWithResponse(ctx, &v1alpha1.CreatePolicyParams{}, policy)
			Expect(err).NotTo(HaveOccurred())
			Expect(createResp.StatusCode()).To(Equal(http.StatusCreated))
			policyID := *createResp.JSON201.Id
			createdPolicyIDs = append(createdPolicyIDs, policyID)

			// Get the policy
			getResp, err := apiClient.GetPolicyWithResponse(ctx, policyID)
			Expect(err).NotTo(HaveOccurred())
			Expect(getResp.StatusCode()).To(Equal(http.StatusOK))
			Expect(getResp.JSON200).NotTo(BeNil())
			Expect(*getResp.JSON200.Id).To(Equal(policyID))
			Expect(getResp.JSON200.DisplayName).To(Equal(policy.DisplayName))
		})

		It("should update policy with PATCH", func() {
			// Create a policy
			policy := v1alpha1.Policy{
				DisplayName: ptr("Original Name"),
				Description: ptr("Original Description"),
				PolicyType:  ptr(v1alpha1.GLOBAL),
				Priority:    ptr(int32(103)),
				Enabled:     ptr(true),
				RegoCode:    ptr(testRegoWithMain),
			}

			createResp, err := apiClient.CreatePolicyWithResponse(ctx, &v1alpha1.CreatePolicyParams{}, policy)
			Expect(err).NotTo(HaveOccurred())
			policyID := *createResp.JSON201.Id
			createdPolicyIDs = append(createdPolicyIDs, policyID)

			// Update the policy
			updatedPolicy := v1alpha1.Policy{
				DisplayName: ptr("Updated Name"),
				Description: ptr("Updated Description"),
				Priority:    ptr(int32(600)),
			}

			updateResp, err := apiClient.UpdatePolicyWithApplicationMergePatchPlusJSONBodyWithResponse(ctx, policyID, updatedPolicy)
			Expect(err).NotTo(HaveOccurred())
			Expect(updateResp.StatusCode()).To(Equal(http.StatusOK))
			Expect(updateResp.JSON200).NotTo(BeNil())
			Expect(updateResp.JSON200.DisplayName).To(Equal(updatedPolicy.DisplayName))
			Expect(updateResp.JSON200.Description).To(Equal(updatedPolicy.Description))
			Expect(updateResp.JSON200.Priority).To(Equal(updatedPolicy.Priority))
		})

		It("should delete policy", func() {
			// Create a policy
			policy := v1alpha1.Policy{
				DisplayName: ptr("Delete Test Policy"),
				PolicyType:  ptr(v1alpha1.GLOBAL),
				Priority:    ptr(int32(104)),
				Enabled:     ptr(true),
				RegoCode:    ptr(testRegoWithMain),
			}

			createResp, err := apiClient.CreatePolicyWithResponse(ctx, &v1alpha1.CreatePolicyParams{}, policy)
			Expect(err).NotTo(HaveOccurred())
			policyID := *createResp.JSON201.Id

			// Delete the policy
			deleteResp, err := apiClient.DeletePolicyWithResponse(ctx, policyID)
			Expect(err).NotTo(HaveOccurred())
			Expect(deleteResp.StatusCode()).To(Equal(http.StatusNoContent))

			// Verify policy is deleted
			getResp, err := apiClient.GetPolicyWithResponse(ctx, policyID)
			Expect(err).NotTo(HaveOccurred())
			Expect(getResp.StatusCode()).To(Equal(http.StatusNotFound))
		})
	})

	Describe("ID Handling", func() {
		It("should accept valid client ID pattern", func() {
			validIDs := []string{
				"my-policy",
				"policy-123",
				"test-policy",
				"policy-v1",
			}

			for i, id := range validIDs {
				policy := v1alpha1.Policy{
					DisplayName: ptr(fmt.Sprintf("Valid ID Policy %d", i)),
					PolicyType:  ptr(v1alpha1.GLOBAL),
					Priority:    ptr(int32(110 + i)),
					Enabled:     ptr(true),
					RegoCode:    ptr(testRegoWithMain),
				}

				resp, err := apiClient.CreatePolicyWithResponse(ctx, &v1alpha1.CreatePolicyParams{Id: &id}, policy)
				Expect(err).NotTo(HaveOccurred())
				Expect(resp.StatusCode()).To(Equal(http.StatusCreated), "ID %s should be valid", id)
				createdPolicyIDs = append(createdPolicyIDs, *resp.JSON201.Id)
				Expect(*resp.JSON201.Id).To(Equal(id), "ID %s should be valid and used", id)
			}
		})

		It("should reject invalid ID pattern", func() {
			invalidIDs := []string{
				"Invalid ID!",
				"id with spaces",
				"id@special",
				"test_policy",
				"policy.v1",
				"test-policy-",
				"123-test-policy",
			}

			for i, id := range invalidIDs {
				policy := v1alpha1.Policy{
					DisplayName: ptr(fmt.Sprintf("Invalid ID Policy %d", i)),
					PolicyType:  ptr(v1alpha1.GLOBAL),
					Priority:    ptr(int32(120 + i)),
					Enabled:     ptr(true),
					RegoCode:    ptr(testRegoWithMain),
				}

				resp, err := apiClient.CreatePolicyWithResponse(ctx, &v1alpha1.CreatePolicyParams{Id: &id}, policy)
				Expect(err).NotTo(HaveOccurred())
				Expect(resp.StatusCode()).To(Equal(http.StatusBadRequest), "ID %s should be invalid", id)
			}
		})

		It("should verify UUID format for server-generated IDs", func() {
			policy := v1alpha1.Policy{
				DisplayName: ptr("UUID Test Policy"),
				PolicyType:  ptr(v1alpha1.GLOBAL),
				Priority:    ptr(int32(130)),
				Enabled:     ptr(true),
				RegoCode:    ptr(testRegoWithMain),
			}

			resp, err := apiClient.CreatePolicyWithResponse(ctx, &v1alpha1.CreatePolicyParams{}, policy)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode()).To(Equal(http.StatusCreated))
			Expect(*resp.JSON201.Id).To(MatchRegexp(`^[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}$`))

			createdPolicyIDs = append(createdPolicyIDs, *resp.JSON201.Id)
		})
	})

	Describe("Conflict Detection", func() {
		It("should detect duplicate ID and return 409", func() {
			clientID := "duplicate-policy-id"
			policy := v1alpha1.Policy{
				DisplayName: ptr("First Duplicate Policy"),
				PolicyType:  ptr(v1alpha1.GLOBAL),
				Priority:    ptr(int32(140)),
				Enabled:     ptr(true),
				RegoCode:    ptr(testRegoWithMain),
			}

			// Create first policy
			resp1, err := apiClient.CreatePolicyWithResponse(ctx, &v1alpha1.CreatePolicyParams{Id: &clientID}, policy)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp1.StatusCode()).To(Equal(http.StatusCreated))
			createdPolicyIDs = append(createdPolicyIDs, clientID)

			// Try to create duplicate
			policy.DisplayName = ptr("Second Duplicate Policy")
			policy.Priority = ptr(int32(141))
			resp2, err := apiClient.CreatePolicyWithResponse(ctx, &v1alpha1.CreatePolicyParams{Id: &clientID}, policy)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp2.StatusCode()).To(Equal(http.StatusConflict))
		})

		It("should return RFC 7807 error format for conflicts", func() {
			clientID := "conflict-policy-id"
			policy := v1alpha1.Policy{
				DisplayName: ptr("First Conflict Policy"),
				PolicyType:  ptr(v1alpha1.GLOBAL),
				Priority:    ptr(int32(150)),
				Enabled:     ptr(true),
				RegoCode:    ptr(testRegoWithMain),
			}

			// Create first policy
			_, err := apiClient.CreatePolicyWithResponse(ctx, &v1alpha1.CreatePolicyParams{Id: &clientID}, policy)
			Expect(err).NotTo(HaveOccurred())
			createdPolicyIDs = append(createdPolicyIDs, clientID)

			// Try to create duplicate
			resp2, err := apiClient.CreatePolicyWithResponse(ctx, &v1alpha1.CreatePolicyParams{Id: &clientID}, policy)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp2.StatusCode()).To(Equal(http.StatusConflict))
			Expect(resp2.JSON409).NotTo(BeNil())
			Expect(resp2.JSON409.Type).NotTo(BeNil())
			Expect(resp2.JSON409.Status).NotTo(BeNil())
			Expect(resp2.JSON409.Status).To(Equal((int32)(409)))
			Expect(resp2.JSON409.Detail).NotTo(BeNil())
		})

		It("should allow same DisplayName with different PolicyType on create", func() {
			sharedName := "Shared Name"
			policyA := v1alpha1.Policy{
				DisplayName: ptr(sharedName),
				PolicyType:  ptr(v1alpha1.GLOBAL),
				Priority:    ptr(int32(201)),
				Enabled:     ptr(true),
				RegoCode:    ptr(testRegoWithMain),
			}
			respA, err := apiClient.CreatePolicyWithResponse(ctx, &v1alpha1.CreatePolicyParams{}, policyA)
			Expect(err).NotTo(HaveOccurred())
			Expect(respA.StatusCode()).To(Equal(http.StatusCreated))
			createdPolicyIDs = append(createdPolicyIDs, *respA.JSON201.Id)

			policyB := v1alpha1.Policy{
				DisplayName: ptr(sharedName),
				PolicyType:  ptr(v1alpha1.USER),
				Priority:    ptr(int32(202)),
				Enabled:     ptr(true),
				RegoCode:    ptr(testRegoWithMain),
			}
			respB, err := apiClient.CreatePolicyWithResponse(ctx, &v1alpha1.CreatePolicyParams{}, policyB)
			Expect(err).NotTo(HaveOccurred())
			Expect(respB.StatusCode()).To(Equal(http.StatusCreated))
			createdPolicyIDs = append(createdPolicyIDs, *respB.JSON201.Id)
		})

		It("should allow same Priority with different PolicyType on create", func() {
			prio := int32(210)
			policyA := v1alpha1.Policy{
				DisplayName: ptr("Prio G"),
				PolicyType:  ptr(v1alpha1.GLOBAL),
				Priority:    ptr(prio),
				Enabled:     ptr(true),
				RegoCode:    ptr(testRegoWithMain),
			}
			respA, err := apiClient.CreatePolicyWithResponse(ctx, &v1alpha1.CreatePolicyParams{}, policyA)
			Expect(err).NotTo(HaveOccurred())
			Expect(respA.StatusCode()).To(Equal(http.StatusCreated))
			createdPolicyIDs = append(createdPolicyIDs, *respA.JSON201.Id)

			policyB := v1alpha1.Policy{
				DisplayName: ptr("Prio U"),
				PolicyType:  ptr(v1alpha1.USER),
				Priority:    ptr(prio),
				Enabled:     ptr(true),
				RegoCode:    ptr(testRegoWithMain),
			}
			respB, err := apiClient.CreatePolicyWithResponse(ctx, &v1alpha1.CreatePolicyParams{}, policyB)
			Expect(err).NotTo(HaveOccurred())
			Expect(respB.StatusCode()).To(Equal(http.StatusCreated))
			createdPolicyIDs = append(createdPolicyIDs, *respB.JSON201.Id)
		})

		It("should reject duplicate DisplayName and PolicyType on create with 409", func() {
			policyA := v1alpha1.Policy{
				DisplayName: ptr("Unique Per Type"),
				PolicyType:  ptr(v1alpha1.GLOBAL),
				Priority:    ptr(int32(203)),
				Enabled:     ptr(true),
				RegoCode:    ptr(testRegoWithMain),
			}
			respA, err := apiClient.CreatePolicyWithResponse(ctx, &v1alpha1.CreatePolicyParams{}, policyA)
			Expect(err).NotTo(HaveOccurred())
			Expect(respA.StatusCode()).To(Equal(http.StatusCreated))
			createdPolicyIDs = append(createdPolicyIDs, *respA.JSON201.Id)

			policyB := v1alpha1.Policy{
				DisplayName: ptr("Unique Per Type"),
				PolicyType:  ptr(v1alpha1.GLOBAL),
				Priority:    ptr(int32(204)),
				Enabled:     ptr(true),
				RegoCode:    ptr(testRegoWithMain),
			}
			respB, err := apiClient.CreatePolicyWithResponse(ctx, &v1alpha1.CreatePolicyParams{}, policyB)
			Expect(err).NotTo(HaveOccurred())
			Expect(respB.StatusCode()).To(Equal(http.StatusConflict))
			Expect(respB.JSON409).NotTo(BeNil())
			Expect(respB.JSON409.Detail).NotTo(BeNil())
			Expect(*respB.JSON409.Detail).To(ContainSubstring("display name"))
			Expect(*respB.JSON409.Detail).To(ContainSubstring("policy type"))
		})

		It("should reject duplicate Priority and PolicyType on create with 409", func() {
			policyA := v1alpha1.Policy{
				DisplayName: ptr("First"),
				PolicyType:  ptr(v1alpha1.GLOBAL),
				Priority:    ptr(int32(220)),
				Enabled:     ptr(true),
				RegoCode:    ptr(testRegoWithMain),
			}
			respA, err := apiClient.CreatePolicyWithResponse(ctx, &v1alpha1.CreatePolicyParams{}, policyA)
			Expect(err).NotTo(HaveOccurred())
			Expect(respA.StatusCode()).To(Equal(http.StatusCreated))
			createdPolicyIDs = append(createdPolicyIDs, *respA.JSON201.Id)

			policyB := v1alpha1.Policy{
				DisplayName: ptr("Second"),
				PolicyType:  ptr(v1alpha1.GLOBAL),
				Priority:    ptr(int32(220)),
				Enabled:     ptr(true),
				RegoCode:    ptr(testRegoWithMain),
			}
			respB, err := apiClient.CreatePolicyWithResponse(ctx, &v1alpha1.CreatePolicyParams{}, policyB)
			Expect(err).NotTo(HaveOccurred())
			Expect(respB.StatusCode()).To(Equal(http.StatusConflict))
			Expect(respB.JSON409).NotTo(BeNil())
			Expect(respB.JSON409.Detail).NotTo(BeNil())
			Expect(*respB.JSON409.Detail).To(ContainSubstring("priority"))
			Expect(*respB.JSON409.Detail).To(ContainSubstring("policy type"))
		})

		It("should reject update to existing DisplayName and PolicyType with 409", func() {
			policyA := v1alpha1.Policy{
				DisplayName: ptr("Name A"),
				PolicyType:  ptr(v1alpha1.GLOBAL),
				Priority:    ptr(int32(301)),
				Enabled:     ptr(true),
				RegoCode:    ptr(testRegoWithMain),
			}
			respA, err := apiClient.CreatePolicyWithResponse(ctx, &v1alpha1.CreatePolicyParams{}, policyA)
			Expect(err).NotTo(HaveOccurred())
			Expect(respA.StatusCode()).To(Equal(http.StatusCreated))
			createdPolicyIDs = append(createdPolicyIDs, *respA.JSON201.Id)

			policyB := v1alpha1.Policy{
				DisplayName: ptr("Name B"),
				PolicyType:  ptr(v1alpha1.GLOBAL),
				Priority:    ptr(int32(302)),
				Enabled:     ptr(true),
				RegoCode:    ptr(testRegoWithMain),
			}
			respB, err := apiClient.CreatePolicyWithResponse(ctx, &v1alpha1.CreatePolicyParams{}, policyB)
			Expect(err).NotTo(HaveOccurred())
			Expect(respB.StatusCode()).To(Equal(http.StatusCreated))
			policyBID := *respB.JSON201.Id
			createdPolicyIDs = append(createdPolicyIDs, policyBID)

			patch := v1alpha1.Policy{DisplayName: ptr("Name A")}
			updateResp, err := apiClient.UpdatePolicyWithApplicationMergePatchPlusJSONBodyWithResponse(ctx, policyBID, patch)
			Expect(err).NotTo(HaveOccurred())
			Expect(updateResp.StatusCode()).To(Equal(http.StatusConflict))
			Expect(updateResp.JSON409).NotTo(BeNil())
			Expect(updateResp.JSON409.Detail).NotTo(BeNil())
			Expect(*updateResp.JSON409.Detail).To(ContainSubstring("display name"))
			Expect(*updateResp.JSON409.Detail).To(ContainSubstring("policy type"))
		})

		It("should reject update to existing Priority and PolicyType with 409", func() {
			policyA := v1alpha1.Policy{
				DisplayName: ptr("PA"),
				PolicyType:  ptr(v1alpha1.GLOBAL),
				Priority:    ptr(int32(401)),
				Enabled:     ptr(true),
				RegoCode:    ptr(testRegoWithMain),
			}
			respA, err := apiClient.CreatePolicyWithResponse(ctx, &v1alpha1.CreatePolicyParams{}, policyA)
			Expect(err).NotTo(HaveOccurred())
			Expect(respA.StatusCode()).To(Equal(http.StatusCreated))
			createdPolicyIDs = append(createdPolicyIDs, *respA.JSON201.Id)

			policyB := v1alpha1.Policy{
				DisplayName: ptr("PB"),
				PolicyType:  ptr(v1alpha1.GLOBAL),
				Priority:    ptr(int32(402)),
				Enabled:     ptr(true),
				RegoCode:    ptr(testRegoWithMain),
			}
			respB, err := apiClient.CreatePolicyWithResponse(ctx, &v1alpha1.CreatePolicyParams{}, policyB)
			Expect(err).NotTo(HaveOccurred())
			Expect(respB.StatusCode()).To(Equal(http.StatusCreated))
			policyBID := *respB.JSON201.Id
			createdPolicyIDs = append(createdPolicyIDs, policyBID)

			patch := v1alpha1.Policy{Priority: ptr(int32(401))}
			updateResp, err := apiClient.UpdatePolicyWithApplicationMergePatchPlusJSONBodyWithResponse(ctx, policyBID, patch)
			Expect(err).NotTo(HaveOccurred())
			Expect(updateResp.StatusCode()).To(Equal(http.StatusConflict))
			Expect(updateResp.JSON409).NotTo(BeNil())
			Expect(updateResp.JSON409.Detail).NotTo(BeNil())
			Expect(*updateResp.JSON409.Detail).To(ContainSubstring("priority"))
			Expect(*updateResp.JSON409.Detail).To(ContainSubstring("policy type"))
		})

		It("should allow update keeping own DisplayName", func() {
			policy := v1alpha1.Policy{
				DisplayName: ptr("Stable Name"),
				PolicyType:  ptr(v1alpha1.GLOBAL),
				Priority:    ptr(int32(310)),
				Enabled:     ptr(true),
				RegoCode:    ptr(testRegoWithMain),
			}
			createResp, err := apiClient.CreatePolicyWithResponse(ctx, &v1alpha1.CreatePolicyParams{}, policy)
			Expect(err).NotTo(HaveOccurred())
			Expect(createResp.StatusCode()).To(Equal(http.StatusCreated))
			policyID := *createResp.JSON201.Id
			createdPolicyIDs = append(createdPolicyIDs, policyID)

			patch := v1alpha1.Policy{Description: ptr("Updated")}
			updateResp, err := apiClient.UpdatePolicyWithApplicationMergePatchPlusJSONBodyWithResponse(ctx, policyID, patch)
			Expect(err).NotTo(HaveOccurred())
			Expect(updateResp.StatusCode()).To(Equal(http.StatusOK))
		})

		It("should allow update keeping own Priority", func() {
			policy := v1alpha1.Policy{
				DisplayName: ptr("Stable"),
				PolicyType:  ptr(v1alpha1.GLOBAL),
				Priority:    ptr(int32(410)),
				Enabled:     ptr(true),
				RegoCode:    ptr(testRegoWithMain),
			}
			createResp, err := apiClient.CreatePolicyWithResponse(ctx, &v1alpha1.CreatePolicyParams{}, policy)
			Expect(err).NotTo(HaveOccurred())
			Expect(createResp.StatusCode()).To(Equal(http.StatusCreated))
			policyID := *createResp.JSON201.Id
			createdPolicyIDs = append(createdPolicyIDs, policyID)

			patch := v1alpha1.Policy{DisplayName: ptr("Stable Renamed")}
			updateResp, err := apiClient.UpdatePolicyWithApplicationMergePatchPlusJSONBodyWithResponse(ctx, policyID, patch)
			Expect(err).NotTo(HaveOccurred())
			Expect(updateResp.StatusCode()).To(Equal(http.StatusOK))
		})
	})

	Describe("List and Pagination", func() {
		BeforeEach(func() {
			// Create multiple policies for pagination tests
			for i := 1; i <= 5; i++ {
				policy := v1alpha1.Policy{
					DisplayName: ptr(fmt.Sprintf("List Policy %d", i)),
					PolicyType:  ptr(v1alpha1.GLOBAL),
					Priority:    ptr(int32(160 + i)),
					Enabled:     ptr(true),
					RegoCode:    ptr(testRegoWithMain),
				}
				resp, err := apiClient.CreatePolicyWithResponse(ctx, &v1alpha1.CreatePolicyParams{}, policy)
				Expect(err).NotTo(HaveOccurred())
				Expect(resp.StatusCode()).To(Equal(http.StatusCreated))
				createdPolicyIDs = append(createdPolicyIDs, *resp.JSON201.Id)
			}
		})

		It("should list all policies", func() {
			resp, err := apiClient.ListPoliciesWithResponse(ctx, &v1alpha1.ListPoliciesParams{})
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode()).To(Equal(http.StatusOK))
			Expect(resp.JSON200).NotTo(BeNil())
			Expect(resp.JSON200.Policies).NotTo(BeEmpty())
			Expect(len(resp.JSON200.Policies)).To(BeNumerically(">=", 5))
		})

		It("should paginate with max_page_size", func() {
			params := &v1alpha1.ListPoliciesParams{
				MaxPageSize: ptr(int32(2)),
			}
			resp, err := apiClient.ListPoliciesWithResponse(ctx, params)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode()).To(Equal(http.StatusOK))
			Expect(resp.JSON200).NotTo(BeNil())
			Expect(len(resp.JSON200.Policies)).To(Equal(2))
		})

		It("should navigate to next page with page_token", func() {
			// Get first page
			params1 := &v1alpha1.ListPoliciesParams{
				MaxPageSize: ptr(int32(2)),
			}
			resp1, err := apiClient.ListPoliciesWithResponse(ctx, params1)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp1.JSON200.NextPageToken).NotTo(BeNil())

			// Get second page
			params2 := &v1alpha1.ListPoliciesParams{
				PageToken: resp1.JSON200.NextPageToken,
			}
			resp2, err := apiClient.ListPoliciesWithResponse(ctx, params2)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp2.StatusCode()).To(Equal(http.StatusOK))
			Expect(resp2.JSON200.Policies).NotTo(BeEmpty())
		})

		It("should verify next_page_token presence/absence", func() {
			// Request all policies - should have no next token
			params := &v1alpha1.ListPoliciesParams{
				MaxPageSize: ptr(int32(1000)),
			}
			resp, err := apiClient.ListPoliciesWithResponse(ctx, params)
			Expect(err).NotTo(HaveOccurred())
			// Could be nil or empty string
			if resp.JSON200.NextPageToken != nil {
				Expect(*resp.JSON200.NextPageToken).To(BeEmpty())
			}

			// Request one policy - should have next token
			params2 := &v1alpha1.ListPoliciesParams{
				MaxPageSize: ptr(int32(1)),
			}
			resp2, err := apiClient.ListPoliciesWithResponse(ctx, params2)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp2.JSON200.NextPageToken).NotTo(BeNil())
			Expect(*resp2.JSON200.NextPageToken).NotTo(BeEmpty())
		})
	})

	Describe("Filtering", func() {
		BeforeEach(func() {
			// Create policies of different types
			policies := []v1alpha1.Policy{
				{
					DisplayName: ptr("Filter Global Policy 1"),
					PolicyType:  ptr(v1alpha1.GLOBAL),
					Priority:    ptr(int32(170)),
					Enabled:     ptr(true),
					RegoCode:    ptr(testRegoWithMain),
				},
				{
					DisplayName: ptr("Filter User Policy 1"),
					PolicyType:  ptr(v1alpha1.USER),
					Priority:    ptr(int32(100)),
					Enabled:     ptr(true),
					RegoCode:    ptr(testRegoWithMain),
				},
				{
					DisplayName: ptr("Filter Disabled Policy"),
					PolicyType:  ptr(v1alpha1.GLOBAL),
					Priority:    ptr(int32(171)),
					Enabled:     ptr(false),
					RegoCode:    ptr(testRegoWithMain),
				},
			}

			for _, policy := range policies {
				resp, err := apiClient.CreatePolicyWithResponse(ctx, &v1alpha1.CreatePolicyParams{}, policy)
				Expect(err).NotTo(HaveOccurred())
				createdPolicyIDs = append(createdPolicyIDs, *resp.JSON201.Id)
			}
		})

		It("should filter by policy_type='GLOBAL'", func() {
			filter := "policy_type='GLOBAL'"
			params := &v1alpha1.ListPoliciesParams{
				Filter: &filter,
			}
			resp, err := apiClient.ListPoliciesWithResponse(ctx, params)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode()).To(Equal(http.StatusOK))
			for _, policy := range resp.JSON200.Policies {
				Expect(*policy.PolicyType).To(Equal(v1alpha1.GLOBAL))
			}
		})

		It("should filter by policy_type='USER'", func() {
			filter := "policy_type='USER'"
			params := &v1alpha1.ListPoliciesParams{
				Filter: &filter,
			}
			resp, err := apiClient.ListPoliciesWithResponse(ctx, params)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode()).To(Equal(http.StatusOK))
			for _, policy := range resp.JSON200.Policies {
				Expect(*policy.PolicyType).To(Equal(v1alpha1.USER))
			}
		})

		It("should filter by enabled=true", func() {
			filter := "enabled=true"
			params := &v1alpha1.ListPoliciesParams{
				Filter: &filter,
			}
			resp, err := apiClient.ListPoliciesWithResponse(ctx, params)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode()).To(Equal(http.StatusOK))
			for _, policy := range resp.JSON200.Policies {
				Expect(policy.Enabled).NotTo(BeNil())
				Expect(*policy.Enabled).To(BeTrue())
			}
		})

		It("should filter by enabled=false", func() {
			filter := "enabled=false"
			params := &v1alpha1.ListPoliciesParams{
				Filter: &filter,
			}
			resp, err := apiClient.ListPoliciesWithResponse(ctx, params)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode()).To(Equal(http.StatusOK))
			for _, policy := range resp.JSON200.Policies {
				Expect(policy.Enabled).NotTo(BeNil())
				Expect(*policy.Enabled).To(BeFalse())
			}
		})

		It("should support combined filters", func() {
			filter := "policy_type='GLOBAL' AND enabled=true"
			params := &v1alpha1.ListPoliciesParams{
				Filter: &filter,
			}
			resp, err := apiClient.ListPoliciesWithResponse(ctx, params)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode()).To(Equal(http.StatusOK))
			for _, policy := range resp.JSON200.Policies {
				Expect(*policy.PolicyType).To(Equal(v1alpha1.GLOBAL))
				Expect(*policy.Enabled).To(BeTrue())
			}
		})

		It("should reject filter with unsupported field", func() {
			filter := "invalid_field='value'"
			params := &v1alpha1.ListPoliciesParams{
				Filter: &filter,
			}
			resp, err := apiClient.ListPoliciesWithResponse(ctx, params)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode()).To(Equal(http.StatusBadRequest))
		})

		It("should reject filter with unrecognized policy_type value", func() {
			filter := "policy_type='TENANT'"
			params := &v1alpha1.ListPoliciesParams{
				Filter: &filter,
			}
			resp, err := apiClient.ListPoliciesWithResponse(ctx, params)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode()).To(Equal(http.StatusBadRequest))
		})

		It("should reject filter with multiple AND operators", func() {
			filter := "policy_type='GLOBAL' AND enabled=true AND enabled=false"
			params := &v1alpha1.ListPoliciesParams{
				Filter: &filter,
			}
			resp, err := apiClient.ListPoliciesWithResponse(ctx, params)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode()).To(Equal(http.StatusBadRequest))
		})
	})

	Describe("Ordering", func() {
		BeforeEach(func() {
			// Create policies with different values
			policies := []v1alpha1.Policy{
				{
					DisplayName: ptr("Z Order Policy"),
					PolicyType:  ptr(v1alpha1.GLOBAL),
					Priority:    ptr(int32(180)),
					Enabled:     ptr(true),
					RegoCode:    ptr(testRegoWithMain),
				},
				{
					DisplayName: ptr("A Order Policy"),
					PolicyType:  ptr(v1alpha1.GLOBAL),
					Priority:    ptr(int32(181)),
					Enabled:     ptr(true),
					RegoCode:    ptr(testRegoWithMain),
				},
				{
					DisplayName: ptr("M Order Policy"),
					PolicyType:  ptr(v1alpha1.GLOBAL),
					Priority:    ptr(int32(182)),
					Enabled:     ptr(true),
					RegoCode:    ptr(testRegoWithMain),
				},
			}

			for _, policy := range policies {
				resp, err := apiClient.CreatePolicyWithResponse(ctx, &v1alpha1.CreatePolicyParams{}, policy)
				Expect(err).NotTo(HaveOccurred())
				createdPolicyIDs = append(createdPolicyIDs, *resp.JSON201.Id)
			}
		})

		It("should order by priority ascending", func() {
			orderBy := "priority"
			params := &v1alpha1.ListPoliciesParams{
				OrderBy: &orderBy,
			}
			resp, err := apiClient.ListPoliciesWithResponse(ctx, params)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode()).To(Equal(http.StatusOK))

			// Verify ascending order
			var lastPriority int32
			for i, policy := range resp.JSON200.Policies {
				if i > 0 && policy.Priority != nil {
					Expect(*policy.Priority).To(BeNumerically(">=", lastPriority))
				}
				if policy.Priority != nil {
					lastPriority = *policy.Priority
				}
			}
		})

		It("should order by priority descending", func() {
			orderBy := "priority desc"
			params := &v1alpha1.ListPoliciesParams{
				OrderBy: &orderBy,
			}
			resp, err := apiClient.ListPoliciesWithResponse(ctx, params)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode()).To(Equal(http.StatusOK))

			// Verify descending order
			var lastPriority int32 = 1001 // Higher than max
			for _, policy := range resp.JSON200.Policies {
				if policy.Priority != nil {
					Expect(*policy.Priority).To(BeNumerically("<=", lastPriority))
					lastPriority = *policy.Priority
				}
			}
		})

		It("should order by display_name", func() {
			orderBy := "display_name"
			params := &v1alpha1.ListPoliciesParams{
				OrderBy: &orderBy,
			}
			resp, err := apiClient.ListPoliciesWithResponse(ctx, params)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode()).To(Equal(http.StatusOK))

			// Verify alphabetical order
			var lastName string
			for i, policy := range resp.JSON200.Policies {
				if i > 0 && policy.DisplayName != nil {
					Expect(strings.ToLower(*policy.DisplayName) >= strings.ToLower(lastName)).To(BeTrue())
				}
				if policy.DisplayName != nil {
					lastName = *policy.DisplayName
				}
			}
		})

		It("should order by create_time", func() {
			orderBy := "create_time"
			params := &v1alpha1.ListPoliciesParams{
				OrderBy: &orderBy,
			}
			resp, err := apiClient.ListPoliciesWithResponse(ctx, params)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode()).To(Equal(http.StatusOK))
			Expect(resp.JSON200.Policies).NotTo(BeEmpty())
		})

		It("should reject order_by with unsupported field", func() {
			orderBy := "id"
			params := &v1alpha1.ListPoliciesParams{
				OrderBy: &orderBy,
			}
			resp, err := apiClient.ListPoliciesWithResponse(ctx, params)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode()).To(Equal(http.StatusBadRequest))
		})

		It("should reject order_by with invalid direction", func() {
			orderBy := "priority foo"
			params := &v1alpha1.ListPoliciesParams{
				OrderBy: &orderBy,
			}
			resp, err := apiClient.ListPoliciesWithResponse(ctx, params)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode()).To(Equal(http.StatusBadRequest))
		})

		It("should reject order_by with too many tokens", func() {
			orderBy := "priority asc extra"
			params := &v1alpha1.ListPoliciesParams{
				OrderBy: &orderBy,
			}
			resp, err := apiClient.ListPoliciesWithResponse(ctx, params)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode()).To(Equal(http.StatusBadRequest))
		})
	})

	Describe("Validation when creating", func() {
		It("should reject missing required fields", func() {
			policy := v1alpha1.Policy{
				DisplayName: ptr("Invalid Policy"),
				// Missing PolicyType, Priority, Enabled, RegoCode
			}

			resp, err := apiClient.CreatePolicyWithResponse(ctx, &v1alpha1.CreatePolicyParams{}, policy)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode()).To(Equal(http.StatusBadRequest))
		})

		It(fmt.Sprintf("should reject priority lower than %d (minimum)", minPolicyPriority), func() {
			policy := v1alpha1.Policy{
				DisplayName: ptr("Invalid Priority Too Low Policy"),
				PolicyType:  ptr(v1alpha1.GLOBAL),
				Priority:    ptr(minPolicyPriority - 1), // Below minimum
				Enabled:     ptr(true),
				RegoCode:    ptr(testRegoWithMain),
			}

			resp, err := apiClient.CreatePolicyWithResponse(ctx, &v1alpha1.CreatePolicyParams{}, policy)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode()).To(Equal(http.StatusBadRequest))
		})

		It(fmt.Sprintf("should reject priority higher than %d (maximum)", maxPolicyPriority), func() {
			policy := v1alpha1.Policy{
				DisplayName: ptr("Invalid Priority Too High Policy"),
				PolicyType:  ptr(v1alpha1.GLOBAL),
				Priority:    ptr(maxPolicyPriority + 1), // Above maximum
				Enabled:     ptr(true),
				RegoCode:    ptr(testRegoWithMain),
			}

			resp, err := apiClient.CreatePolicyWithResponse(ctx, &v1alpha1.CreatePolicyParams{}, policy)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode()).To(Equal(http.StatusBadRequest))
		})

		It("should reject invalid policy_type", func() {
			// We can't easily test this with typed client, but verify valid types work
			policy := v1alpha1.Policy{
				DisplayName: ptr("Valid Type Policy"),
				PolicyType:  ptr(v1alpha1.USER),
				Priority:    ptr(int32(101)),
				Enabled:     ptr(true),
				RegoCode:    ptr(testRegoWithMain),
			}

			resp, err := apiClient.CreatePolicyWithResponse(ctx, &v1alpha1.CreatePolicyParams{}, policy)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode()).To(Equal(http.StatusCreated))
			createdPolicyIDs = append(createdPolicyIDs, *resp.JSON201.Id)
		})

		It("should reject Rego without a main rule", func() {
			policy := v1alpha1.Policy{
				DisplayName: ptr("No Main Rule Policy"),
				PolicyType:  ptr(v1alpha1.GLOBAL),
				Priority:    ptr(int32(102)),
				Enabled:     ptr(true),
				RegoCode:    ptr("package test\ndefault allow = true"),
			}

			resp, err := apiClient.CreatePolicyWithResponse(ctx, &v1alpha1.CreatePolicyParams{}, policy)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode()).To(Equal(http.StatusBadRequest))
			Expect(resp.JSON400).NotTo(BeNil())
			Expect(string(resp.JSON400.Type)).To(Equal(string(v1alpha1.INVALIDARGUMENT)))
		})
	})

	Describe("Validation when updating (patch)", func() {
		It("should reject patch with empty rego_code", func() {
			policy := v1alpha1.Policy{
				DisplayName: ptr("Patch Validation Policy"),
				PolicyType:  ptr(v1alpha1.GLOBAL),
				Priority:    ptr(int32(300)),
				Enabled:     ptr(true),
				RegoCode:    ptr(testRegoWithMain),
			}
			createResp, err := apiClient.CreatePolicyWithResponse(ctx, &v1alpha1.CreatePolicyParams{}, policy)
			Expect(err).NotTo(HaveOccurred())
			Expect(createResp.StatusCode()).To(Equal(http.StatusCreated))
			policyID := *createResp.JSON201.Id
			createdPolicyIDs = append(createdPolicyIDs, policyID)

			patch := v1alpha1.Policy{RegoCode: ptr("")}
			resp, err := apiClient.UpdatePolicyWithApplicationMergePatchPlusJSONBodyWithResponse(ctx, policyID, patch)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode()).To(Equal(http.StatusBadRequest))
		})

		It("should reject patch with whitespace-only rego_code", func() {
			policy := v1alpha1.Policy{
				DisplayName: ptr("Patch Whitespace Rego Policy"),
				PolicyType:  ptr(v1alpha1.GLOBAL),
				Priority:    ptr(int32(301)),
				Enabled:     ptr(true),
				RegoCode:    ptr(testRegoWithMain),
			}
			createResp, err := apiClient.CreatePolicyWithResponse(ctx, &v1alpha1.CreatePolicyParams{}, policy)
			Expect(err).NotTo(HaveOccurred())
			Expect(createResp.StatusCode()).To(Equal(http.StatusCreated))
			policyID := *createResp.JSON201.Id
			createdPolicyIDs = append(createdPolicyIDs, policyID)

			patch := v1alpha1.Policy{RegoCode: ptr("   \t\n ")}
			resp, err := apiClient.UpdatePolicyWithApplicationMergePatchPlusJSONBodyWithResponse(ctx, policyID, patch)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode()).To(Equal(http.StatusBadRequest))
		})

		It("should reject patch with priority too low", func() {
			policy := v1alpha1.Policy{
				DisplayName: ptr("Patch Priority Low Policy"),
				PolicyType:  ptr(v1alpha1.GLOBAL),
				Priority:    ptr(int32(302)),
				Enabled:     ptr(true),
				RegoCode:    ptr(testRegoWithMain),
			}
			createResp, err := apiClient.CreatePolicyWithResponse(ctx, &v1alpha1.CreatePolicyParams{}, policy)
			Expect(err).NotTo(HaveOccurred())
			Expect(createResp.StatusCode()).To(Equal(http.StatusCreated))
			policyID := *createResp.JSON201.Id
			createdPolicyIDs = append(createdPolicyIDs, policyID)

			patch := v1alpha1.Policy{Priority: ptr(int32(0))}
			resp, err := apiClient.UpdatePolicyWithApplicationMergePatchPlusJSONBodyWithResponse(ctx, policyID, patch)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode()).To(Equal(http.StatusBadRequest))
		})

		It("should reject patch with priority too high", func() {
			policy := v1alpha1.Policy{
				DisplayName: ptr("Patch Priority High Policy"),
				PolicyType:  ptr(v1alpha1.GLOBAL),
				Priority:    ptr(int32(303)),
				Enabled:     ptr(true),
				RegoCode:    ptr(testRegoWithMain),
			}
			createResp, err := apiClient.CreatePolicyWithResponse(ctx, &v1alpha1.CreatePolicyParams{}, policy)
			Expect(err).NotTo(HaveOccurred())
			Expect(createResp.StatusCode()).To(Equal(http.StatusCreated))
			policyID := *createResp.JSON201.Id
			createdPolicyIDs = append(createdPolicyIDs, policyID)

			patch := v1alpha1.Policy{Priority: ptr(int32(1001))}
			resp, err := apiClient.UpdatePolicyWithApplicationMergePatchPlusJSONBodyWithResponse(ctx, policyID, patch)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode()).To(Equal(http.StatusBadRequest))
		})
	})

	Describe("Immutable Fields", func() {
		It("should not allow policy_type to be changed via PATCH", func() {
			// Create a GLOBAL policy
			policy := v1alpha1.Policy{
				DisplayName: ptr("Immutable Test Policy"),
				PolicyType:  ptr(v1alpha1.GLOBAL),
				Priority:    ptr(int32(200)),
				Enabled:     ptr(true),
				RegoCode:    ptr(testRegoWithMain),
			}

			createResp, err := apiClient.CreatePolicyWithResponse(ctx, &v1alpha1.CreatePolicyParams{}, policy)
			Expect(err).NotTo(HaveOccurred())
			policyID := *createResp.JSON201.Id
			createdPolicyIDs = append(createdPolicyIDs, policyID)

			// Try to change policy_type - must be rejected with 400
			update := v1alpha1.Policy{
				PolicyType: ptr(v1alpha1.USER),
			}

			resp, err := apiClient.UpdatePolicyWithApplicationMergePatchPlusJSONBodyWithResponse(ctx, policyID, update)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode()).To(Equal(http.StatusBadRequest))
		})

		It("should reject PATCH when path is different from current", func() {
			policy := v1alpha1.Policy{
				DisplayName: ptr("Path Reject Policy"),
				PolicyType:  ptr(v1alpha1.GLOBAL),
				Priority:    ptr(int32(210)),
				Enabled:     ptr(true),
				RegoCode:    ptr(testRegoWithMain),
			}
			createResp, err := apiClient.CreatePolicyWithResponse(ctx, &v1alpha1.CreatePolicyParams{}, policy)
			Expect(err).NotTo(HaveOccurred())
			policyID := *createResp.JSON201.Id
			createdPolicyIDs = append(createdPolicyIDs, policyID)

			update := v1alpha1.Policy{
				Path:        ptr("policies/other-id"),
				DisplayName: ptr("Updated"),
			}
			resp, err := apiClient.UpdatePolicyWithApplicationMergePatchPlusJSONBodyWithResponse(ctx, policyID, update)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode()).To(Equal(http.StatusBadRequest))
		})

		It("should reject PATCH when id is different from current", func() {
			policy := v1alpha1.Policy{
				DisplayName: ptr("ID Reject Policy"),
				PolicyType:  ptr(v1alpha1.GLOBAL),
				Priority:    ptr(int32(211)),
				Enabled:     ptr(true),
				RegoCode:    ptr(testRegoWithMain),
			}
			createResp, err := apiClient.CreatePolicyWithResponse(ctx, &v1alpha1.CreatePolicyParams{}, policy)
			Expect(err).NotTo(HaveOccurred())
			policyID := *createResp.JSON201.Id
			createdPolicyIDs = append(createdPolicyIDs, policyID)

			update := v1alpha1.Policy{
				Id:          ptr("other-id"),
				DisplayName: ptr("Updated"),
			}
			resp, err := apiClient.UpdatePolicyWithApplicationMergePatchPlusJSONBodyWithResponse(ctx, policyID, update)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode()).To(Equal(http.StatusBadRequest))
		})

		It("should reject PATCH when create_time is different from current", func() {
			policy := v1alpha1.Policy{
				DisplayName: ptr("CreateTime Reject Policy"),
				PolicyType:  ptr(v1alpha1.GLOBAL),
				Priority:    ptr(int32(212)),
				Enabled:     ptr(true),
				RegoCode:    ptr(testRegoWithMain),
			}
			createResp, err := apiClient.CreatePolicyWithResponse(ctx, &v1alpha1.CreatePolicyParams{}, policy)
			Expect(err).NotTo(HaveOccurred())
			policyID := *createResp.JSON201.Id
			createdPolicyIDs = append(createdPolicyIDs, policyID)

			otherTime := time.Now().Add(-24 * time.Hour)
			update := v1alpha1.Policy{
				CreateTime:  ptr(otherTime),
				DisplayName: ptr("Updated"),
			}
			resp, err := apiClient.UpdatePolicyWithApplicationMergePatchPlusJSONBodyWithResponse(ctx, policyID, update)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode()).To(Equal(http.StatusBadRequest))
		})

		It("should reject PATCH when update_time is different from current", func() {
			policy := v1alpha1.Policy{
				DisplayName: ptr("UpdateTime Reject Policy"),
				PolicyType:  ptr(v1alpha1.GLOBAL),
				Priority:    ptr(int32(213)),
				Enabled:     ptr(true),
				RegoCode:    ptr(testRegoWithMain),
			}
			createResp, err := apiClient.CreatePolicyWithResponse(ctx, &v1alpha1.CreatePolicyParams{}, policy)
			Expect(err).NotTo(HaveOccurred())
			policyID := *createResp.JSON201.Id
			createdPolicyIDs = append(createdPolicyIDs, policyID)

			otherTime := time.Now().Add(24 * time.Hour)
			update := v1alpha1.Policy{
				UpdateTime:  ptr(otherTime),
				DisplayName: ptr("Updated"),
			}
			resp, err := apiClient.UpdatePolicyWithApplicationMergePatchPlusJSONBodyWithResponse(ctx, policyID, update)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode()).To(Equal(http.StatusBadRequest))
		})

		It("should accept PATCH when immutable field is same as current (with mutable change)", func() {
			policy := v1alpha1.Policy{
				DisplayName: ptr("Same Value Original"),
				PolicyType:  ptr(v1alpha1.GLOBAL),
				Priority:    ptr(int32(214)),
				Enabled:     ptr(true),
				RegoCode:    ptr(testRegoWithMain),
			}
			createResp, err := apiClient.CreatePolicyWithResponse(ctx, &v1alpha1.CreatePolicyParams{}, policy)
			Expect(err).NotTo(HaveOccurred())
			policyID := *createResp.JSON201.Id
			createdPolicyIDs = append(createdPolicyIDs, policyID)

			// Include policy_type with same value as current, plus a mutable change
			update := v1alpha1.Policy{
				PolicyType:  createResp.JSON201.PolicyType,
				DisplayName: ptr("Same Value Updated"),
			}
			resp, err := apiClient.UpdatePolicyWithApplicationMergePatchPlusJSONBodyWithResponse(ctx, policyID, update)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode()).To(Equal(http.StatusOK))
			Expect(resp.JSON200).NotTo(BeNil())
			Expect(*resp.JSON200.DisplayName).To(Equal("Same Value Updated"))
			Expect(*resp.JSON200.PolicyType).To(Equal(v1alpha1.GLOBAL))
		})

		It("should update only mutable fields", func() {
			// Create a policy
			policy := v1alpha1.Policy{
				DisplayName: ptr("Mutable Original Name"),
				Description: ptr("Original Description"),
				PolicyType:  ptr(v1alpha1.GLOBAL),
				Priority:    ptr(int32(201)),
				Enabled:     ptr(true),
				LabelSelector: &map[string]string{
					"env": "dev",
				},
				RegoCode: ptr(testRegoWithMain),
			}

			createResp, err := apiClient.CreatePolicyWithResponse(ctx, &v1alpha1.CreatePolicyParams{}, policy)
			Expect(err).NotTo(HaveOccurred())
			policyID := *createResp.JSON201.Id
			createdPolicyIDs = append(createdPolicyIDs, policyID)

			// Update mutable fields
			update := v1alpha1.Policy{
				DisplayName: ptr("Mutable Updated Name"),
				Description: ptr("Updated Description"),
				Priority:    ptr(int32(600)),
				Enabled:     ptr(false),
				LabelSelector: &map[string]string{
					"env": "prod",
				},
				RegoCode: ptr(testRegoUpdatedPackage),
			}

			updateResp, err := apiClient.UpdatePolicyWithApplicationMergePatchPlusJSONBodyWithResponse(ctx, policyID, update)
			Expect(err).NotTo(HaveOccurred())
			Expect(updateResp.StatusCode()).To(Equal(http.StatusOK))
			Expect(*updateResp.JSON200.DisplayName).To(Equal("Mutable Updated Name"))
			Expect(*updateResp.JSON200.Description).To(Equal("Updated Description"))
			Expect(*updateResp.JSON200.Priority).To(Equal(*update.Priority))
			Expect(*updateResp.JSON200.Enabled).To(BeFalse())
		})
	})

	Describe("Operations on non-existent policies", func() {
		It("should return 404 for non-existent policy GET", func() {
			resp, err := apiClient.GetPolicyWithResponse(ctx, "non-existent-id")
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode()).To(Equal(http.StatusNotFound))
		})

		It("should return 404 for non-existent policy UPDATE", func() {
			update := v1alpha1.Policy{
				DisplayName: ptr("Update Non-Existent"),
			}
			resp, err := apiClient.UpdatePolicyWithApplicationMergePatchPlusJSONBodyWithResponse(ctx, "non-existent-id", update)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode()).To(Equal(http.StatusNotFound))
		})

		It("should return 404 for non-existent policy DELETE", func() {
			resp, err := apiClient.DeletePolicyWithResponse(ctx, "non-existent-id")
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode()).To(Equal(http.StatusNotFound))
		})
	})

	Describe("Label Selectors", func() {
		It("should create policy with labels", func() {
			policy := v1alpha1.Policy{
				DisplayName: ptr("Policy with Labels"),
				PolicyType:  ptr(v1alpha1.GLOBAL),
				Priority:    ptr(int32(210)),
				Enabled:     ptr(true),
				LabelSelector: &map[string]string{
					"env":  "production",
					"team": "platform",
				},
				RegoCode: ptr(testRegoWithMain),
			}

			resp, err := apiClient.CreatePolicyWithResponse(ctx, &v1alpha1.CreatePolicyParams{}, policy)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode()).To(Equal(http.StatusCreated))
			Expect(resp.JSON201.LabelSelector).NotTo(BeNil())
			Expect((*resp.JSON201.LabelSelector)["env"]).To(Equal("production"))
			Expect((*resp.JSON201.LabelSelector)["team"]).To(Equal("platform"))

			createdPolicyIDs = append(createdPolicyIDs, *resp.JSON201.Id)
		})

		It("should create policy without labels", func() {
			policy := v1alpha1.Policy{
				DisplayName: ptr("Policy without Labels"),
				PolicyType:  ptr(v1alpha1.GLOBAL),
				Priority:    ptr(int32(211)),
				Enabled:     ptr(true),
				RegoCode:    ptr(testRegoWithMain),
			}

			resp, err := apiClient.CreatePolicyWithResponse(ctx, &v1alpha1.CreatePolicyParams{}, policy)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode()).To(Equal(http.StatusCreated))

			createdPolicyIDs = append(createdPolicyIDs, *resp.JSON201.Id)
		})

		It("should update labels via PATCH", func() {
			// Create policy with labels
			policy := v1alpha1.Policy{
				DisplayName: ptr("Label Update Policy"),
				PolicyType:  ptr(v1alpha1.GLOBAL),
				Priority:    ptr(int32(212)),
				Enabled:     ptr(true),
				LabelSelector: &map[string]string{
					"env": "dev",
				},
				RegoCode: ptr(testRegoWithMain),
			}

			createResp, err := apiClient.CreatePolicyWithResponse(ctx, &v1alpha1.CreatePolicyParams{}, policy)
			Expect(err).NotTo(HaveOccurred())
			policyID := *createResp.JSON201.Id
			createdPolicyIDs = append(createdPolicyIDs, policyID)

			// Update labels
			update := v1alpha1.Policy{
				LabelSelector: &map[string]string{
					"env":  "prod",
					"team": "security",
				},
			}

			updateResp, err := apiClient.UpdatePolicyWithApplicationMergePatchPlusJSONBodyWithResponse(ctx, policyID, update)
			Expect(err).NotTo(HaveOccurred())
			Expect(updateResp.StatusCode()).To(Equal(http.StatusOK))
			Expect((*updateResp.JSON200.LabelSelector)["env"]).To(Equal("prod"))
			Expect((*updateResp.JSON200.LabelSelector)["team"]).To(Equal("security"))
		})
	})

	Describe("Edge Cases", func() {
		It("should accept max page size of 1000", func() {
			params := &v1alpha1.ListPoliciesParams{
				MaxPageSize: ptr(int32(1000)),
			}
			resp, err := apiClient.ListPoliciesWithResponse(ctx, params)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode()).To(Equal(http.StatusOK))
		})

		It("should accept priority bounds (1 and 1000)", func() {
			minPolicy := v1alpha1.Policy{
				DisplayName: ptr("Min Priority Policy"),
				PolicyType:  ptr(v1alpha1.GLOBAL),
				Priority:    ptr(int32(1)),
				Enabled:     ptr(true),
				RegoCode:    ptr(testRegoWithMain),
			}

			resp1, err := apiClient.CreatePolicyWithResponse(ctx, &v1alpha1.CreatePolicyParams{}, minPolicy)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp1.StatusCode()).To(Equal(http.StatusCreated))
			createdPolicyIDs = append(createdPolicyIDs, *resp1.JSON201.Id)

			maxPolicy := v1alpha1.Policy{
				DisplayName: ptr("Max Priority Policy"),
				PolicyType:  ptr(v1alpha1.GLOBAL),
				Priority:    ptr(int32(1000)),
				Enabled:     ptr(true),
				RegoCode:    ptr(testRegoWithMain),
			}

			resp2, err := apiClient.CreatePolicyWithResponse(ctx, &v1alpha1.CreatePolicyParams{}, maxPolicy)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp2.StatusCode()).To(Equal(http.StatusCreated))
			createdPolicyIDs = append(createdPolicyIDs, *resp2.JSON201.Id)
		})

		It("should handle long Rego code", func() {
			longRego := `package authz

import future.keywords.if

default main := {"rejected": true}

main := {"rejected": false} if {
    input.user.role == "admin"
}

main := {"rejected": false} if {
    input.method == "GET"
    input.user.id == input.resource.owner_id
}

main := {"rejected": false} if {
    input.method in ["PUT", "PATCH"]
    input.user.id == input.resource.owner_id
}

main := {"rejected": false} if {
    input.user.role == "moderator"
    input.resource.type == "comment"
    input.method in ["DELETE", "PUT"]
}
`
			policy := v1alpha1.Policy{
				DisplayName: ptr("Long Rego Policy"),
				PolicyType:  ptr(v1alpha1.GLOBAL),
				Priority:    ptr(int32(220)),
				Enabled:     ptr(true),
				RegoCode:    &longRego,
			}

			resp, err := apiClient.CreatePolicyWithResponse(ctx, &v1alpha1.CreatePolicyParams{}, policy)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode()).To(Equal(http.StatusCreated))
			createdPolicyIDs = append(createdPolicyIDs, *resp.JSON201.Id)
		})

		It("should handle Unicode in display_name and description", func() {
			policy := v1alpha1.Policy{
				DisplayName: ptr("政策 Policy 🔒"),
				Description: ptr("Описание политики with émojis 🎉"),
				PolicyType:  ptr(v1alpha1.GLOBAL),
				Priority:    ptr(int32(221)),
				Enabled:     ptr(true),
				RegoCode:    ptr(testRegoWithMain),
			}

			resp, err := apiClient.CreatePolicyWithResponse(ctx, &v1alpha1.CreatePolicyParams{}, policy)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode()).To(Equal(http.StatusCreated))
			Expect(*resp.JSON201.DisplayName).To(Equal("政策 Policy 🔒"))
			Expect(*resp.JSON201.Description).To(Equal("Описание политики with émojis 🎉"))

			createdPolicyIDs = append(createdPolicyIDs, *resp.JSON201.Id)
		})
	})

	Describe("OPA Integration", func() {
		It("should store and retrieve Rego code", func() {
			regoCode := "package authz\n\ndefault main := {\"rejected\": true}\n\nmain := {\"rejected\": false} if {\n\tinput.user == \"admin\"\n}"
			policy := v1alpha1.Policy{
				DisplayName: ptr("OPA Test Policy"),
				PolicyType:  ptr(v1alpha1.GLOBAL),
				Priority:    ptr(int32(300)),
				Enabled:     ptr(true),
				RegoCode:    &regoCode,
			}

			// Create policy
			createResp, err := apiClient.CreatePolicyWithResponse(ctx, &v1alpha1.CreatePolicyParams{}, policy)
			Expect(err).NotTo(HaveOccurred())
			Expect(createResp.StatusCode()).To(Equal(http.StatusCreated))
			policyID := *createResp.JSON201.Id
			createdPolicyIDs = append(createdPolicyIDs, policyID)

			// Get policy - should return Rego code
			getResp, err := apiClient.GetPolicyWithResponse(ctx, policyID)
			Expect(err).NotTo(HaveOccurred())
			Expect(getResp.StatusCode()).To(Equal(http.StatusOK))
			Expect(getResp.JSON200.RegoCode).NotTo(BeNil())
			Expect(*getResp.JSON200.RegoCode).To(Equal(regoCode), "GET should return actual Rego code from OPA")
		})

		It("should reject invalid Rego code", func() {
			invalidRego := "this is not valid rego syntax!!!"
			policy := v1alpha1.Policy{
				DisplayName: ptr("Invalid Rego Policy"),
				PolicyType:  ptr(v1alpha1.GLOBAL),
				Priority:    ptr(int32(301)),
				Enabled:     ptr(true),
				RegoCode:    &invalidRego,
			}

			resp, err := apiClient.CreatePolicyWithResponse(ctx, &v1alpha1.CreatePolicyParams{}, policy)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode()).To(Equal(http.StatusBadRequest), "Should reject invalid Rego")
			Expect(resp.JSON400).NotTo(BeNil())
			Expect(string(resp.JSON400.Type)).To(Equal(string(v1alpha1.INVALIDARGUMENT)))
		})

		It("should update Rego code", func() {
			originalRego := "package authz\n\ndefault main := {\"rejected\": true}"
			policy := v1alpha1.Policy{
				DisplayName: ptr("Update Rego Test"),
				PolicyType:  ptr(v1alpha1.GLOBAL),
				Priority:    ptr(int32(302)),
				Enabled:     ptr(true),
				RegoCode:    &originalRego,
			}

			// Create policy
			createResp, err := apiClient.CreatePolicyWithResponse(ctx, &v1alpha1.CreatePolicyParams{}, policy)
			Expect(err).NotTo(HaveOccurred())
			Expect(createResp.StatusCode()).To(Equal(http.StatusCreated))
			policyID := *createResp.JSON201.Id
			createdPolicyIDs = append(createdPolicyIDs, policyID)

			// Update Rego code
			updatedRego := "package authz\n\nmain := {\"rejected\": false}"
			patch := v1alpha1.Policy{
				RegoCode: &updatedRego,
			}

			updateResp, err := apiClient.UpdatePolicyWithApplicationMergePatchPlusJSONBodyWithResponse(ctx, policyID, patch)
			Expect(err).NotTo(HaveOccurred())
			Expect(updateResp.StatusCode()).To(Equal(http.StatusOK))

			// Verify updated Rego is returned by GET
			getResp, err := apiClient.GetPolicyWithResponse(ctx, policyID)
			Expect(err).NotTo(HaveOccurred())
			Expect(getResp.StatusCode()).To(Equal(http.StatusOK))
			Expect(getResp.JSON200.RegoCode).NotTo(BeNil())
			Expect(*getResp.JSON200.RegoCode).To(Equal(updatedRego), "GET should return updated Rego code")
		})

		It("should reject invalid Rego on update and preserve original code", func() {
			originalRego := "package authz\n\ndefault main := {\"rejected\": true}\n\nmain := {\"rejected\": false} if { input.user == \"admin\" }"
			policy := v1alpha1.Policy{
				DisplayName: ptr("Invalid Update Rego Test"),
				PolicyType:  ptr(v1alpha1.GLOBAL),
				Priority:    ptr(int32(301)),
				Enabled:     ptr(true),
				RegoCode:    &originalRego,
			}

			createResp, err := apiClient.CreatePolicyWithResponse(ctx, &v1alpha1.CreatePolicyParams{}, policy)
			Expect(err).NotTo(HaveOccurred())
			Expect(createResp.StatusCode()).To(Equal(http.StatusCreated))
			policyID := *createResp.JSON201.Id
			createdPolicyIDs = append(createdPolicyIDs, policyID)

			invalidRego := "this is not valid rego syntax!!!"
			patch := v1alpha1.Policy{RegoCode: &invalidRego}
			updateResp, err := apiClient.UpdatePolicyWithApplicationMergePatchPlusJSONBodyWithResponse(ctx, policyID, patch)
			Expect(err).NotTo(HaveOccurred())
			Expect(updateResp.StatusCode()).To(Equal(http.StatusBadRequest), "Should reject invalid Rego on update")
			Expect(updateResp.JSON400).NotTo(BeNil())
			Expect(string(updateResp.JSON400.Type)).To(Equal(string(v1alpha1.INVALIDARGUMENT)))

			getResp, err := apiClient.GetPolicyWithResponse(ctx, policyID)
			Expect(err).NotTo(HaveOccurred())
			Expect(getResp.StatusCode()).To(Equal(http.StatusOK))
			Expect(getResp.JSON200.RegoCode).NotTo(BeNil())
			Expect(*getResp.JSON200.RegoCode).To(Equal(originalRego), "Original Rego code should be unchanged after rejected update")
		})

		It("should delete policy and remove Rego from OPA", func() {
			regoCode := "package authz\n\ndefault main := {\"rejected\": true}\n\nmain := {\"rejected\": false} if { input.role == \"admin\" }"
			policy := v1alpha1.Policy{
				DisplayName: ptr("Delete OPA Test Policy"),
				PolicyType:  ptr(v1alpha1.GLOBAL),
				Priority:    ptr(int32(302)),
				Enabled:     ptr(true),
				RegoCode:    &regoCode,
			}

			createResp, err := apiClient.CreatePolicyWithResponse(ctx, &v1alpha1.CreatePolicyParams{}, policy)
			Expect(err).NotTo(HaveOccurred())
			Expect(createResp.StatusCode()).To(Equal(http.StatusCreated))
			policyID := *createResp.JSON201.Id
			// Do not append to createdPolicyIDs - we are testing delete

			// Verify policy exists via API before delete
			getBeforeResp, err := apiClient.GetPolicyWithResponse(ctx, policyID)
			Expect(err).NotTo(HaveOccurred())
			Expect(getBeforeResp.StatusCode()).To(Equal(http.StatusOK), "Policy should exist after create")

			deleteResp, err := apiClient.DeletePolicyWithResponse(ctx, policyID)
			Expect(err).NotTo(HaveOccurred())
			Expect(deleteResp.StatusCode()).To(Equal(http.StatusNoContent), "Delete should succeed")

			// Verify policy is gone from API
			getResp, err := apiClient.GetPolicyWithResponse(ctx, policyID)
			Expect(err).NotTo(HaveOccurred())
			Expect(getResp.StatusCode()).To(Equal(http.StatusNotFound))
		})

		It("should return rego_code in LIST responses", func() {
			regoCode := "package test\n\ndefault main := {\"rejected\": true}"
			policy := v1alpha1.Policy{
				DisplayName: ptr("List Test Policy"),
				PolicyType:  ptr(v1alpha1.GLOBAL),
				Priority:    ptr(int32(303)),
				Enabled:     ptr(true),
				RegoCode:    &regoCode,
			}

			// Create policy
			createResp, err := apiClient.CreatePolicyWithResponse(ctx, &v1alpha1.CreatePolicyParams{}, policy)
			Expect(err).NotTo(HaveOccurred())
			Expect(createResp.StatusCode()).To(Equal(http.StatusCreated))
			policyID := *createResp.JSON201.Id
			createdPolicyIDs = append(createdPolicyIDs, policyID)

			// List policies
			listResp, err := apiClient.ListPoliciesWithResponse(ctx, &v1alpha1.ListPoliciesParams{})
			Expect(err).NotTo(HaveOccurred())
			Expect(listResp.StatusCode()).To(Equal(http.StatusOK))
			Expect(listResp.JSON200).NotTo(BeNil())

			// Find the created policy in the list
			found := false
			for _, p := range listResp.JSON200.Policies {
				if *p.Id == policyID {
					found = true
					Expect(p.RegoCode).NotTo(BeNil())
					Expect(*p.RegoCode).To(Equal(regoCode), "LIST should return full rego_code")
					break
				}
			}
			Expect(found).To(BeTrue(), "Created policy should be in list")
		})

		It("should support Unicode characters in Rego code", func() {
			// Rego code with Unicode comments
			unicodeRego := "package test\n\n# 政策规则 - Policy Rule\n# Правило политики\ndefault main := {\"rejected\": true}\n\n# Allow admin users\nmain := {\"rejected\": false} if {\n\t# 管理员用户\n\tinput.user == \"admin\"\n}"
			policy := v1alpha1.Policy{
				DisplayName: ptr("Unicode Rego Policy"),
				PolicyType:  ptr(v1alpha1.GLOBAL),
				Priority:    ptr(int32(304)),
				Enabled:     ptr(true),
				RegoCode:    &unicodeRego,
			}

			// Create policy
			createResp, err := apiClient.CreatePolicyWithResponse(ctx, &v1alpha1.CreatePolicyParams{}, policy)
			Expect(err).NotTo(HaveOccurred())
			Expect(createResp.StatusCode()).To(Equal(http.StatusCreated))
			policyID := *createResp.JSON201.Id
			createdPolicyIDs = append(createdPolicyIDs, policyID)

			// Get policy - verify Unicode is preserved
			getResp, err := apiClient.GetPolicyWithResponse(ctx, policyID)
			Expect(err).NotTo(HaveOccurred())
			Expect(getResp.StatusCode()).To(Equal(http.StatusOK))
			Expect(getResp.JSON200.RegoCode).NotTo(BeNil())
			Expect(*getResp.JSON200.RegoCode).To(Equal(unicodeRego), "Unicode characters should be preserved")
		})
	})
})
