//go:build subsystem

package subsystem_test

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// TC-08: Agent CRUD via control-plane API with JWT auth.
// Validates that authenticated users can register, list, get, and re-register
// agents through the control-plane API when auth is enabled (Config B).
var _ = Describe("Agent CRUD with JWT auth (TC-08)", func() {
	var token string

	BeforeEach(func() {
		adminToken := getKeycloakAdminToken()
		username := uniqueUsername()
		kcUserID := createKeycloakUser(adminToken, username, "testpass")
		DeferCleanup(func() { deleteKeycloakUser(adminToken, kcUserID) })
		token = getUserToken(username, "testpass")
	})

	It("registers, lists, gets, and re-registers an agent", func() {
		agentName := "tc08-crud-" + uuid.NewString()[:8]
		topicName := "dcm.agent." + agentName

		By("registering a new agent with Bearer token")
		createBody := `{
			"name": "` + agentName + `",
			"environment": "test",
			"service_types": ["vm", "container"],
			"cost": "low",
			"topic_name": "` + topicName + `"
		}`
		createResp := doRequestWithBody(http.MethodPost, "/agents", createBody, withBearerToken(token))
		defer createResp.Body.Close()
		Expect(createResp.StatusCode).To(Equal(http.StatusCreated))

		var created map[string]interface{}
		body, err := io.ReadAll(createResp.Body)
		Expect(err).NotTo(HaveOccurred())
		Expect(json.Unmarshal(body, &created)).To(Succeed())
		agentID, ok := created["agent_id"].(string)
		Expect(ok).To(BeTrue(), "response missing 'agent_id' field")
		Expect(agentID).NotTo(BeEmpty())

		By("listing agents includes the new one")
		listResp := doRequest(http.MethodGet, "/agents", withBearerToken(token))
		defer listResp.Body.Close()
		Expect(listResp.StatusCode).To(Equal(http.StatusOK))

		listBody, err := io.ReadAll(listResp.Body)
		Expect(err).NotTo(HaveOccurred())
		Expect(string(listBody)).To(ContainSubstring(agentName))

		By("getting the agent by ID")
		getResp := doRequest(http.MethodGet, "/agents/"+agentID, withBearerToken(token))
		defer getResp.Body.Close()
		Expect(getResp.StatusCode).To(Equal(http.StatusOK))

		var fetched map[string]interface{}
		getBody, err := io.ReadAll(getResp.Body)
		Expect(err).NotTo(HaveOccurred())
		Expect(json.Unmarshal(getBody, &fetched)).To(Succeed())
		Expect(fetched["name"]).To(Equal(agentName))

		By("re-registering the same agent returns 200 (idempotent)")
		reRegBody := `{
			"name": "` + agentName + `",
			"environment": "staging",
			"service_types": ["vm"],
			"cost": "medium",
			"topic_name": "` + topicName + `"
		}`
		reRegResp := doRequestWithBody(http.MethodPost, "/agents", reRegBody, withBearerToken(token))
		defer reRegResp.Body.Close()
		Expect(reRegResp.StatusCode).To(Equal(http.StatusOK))

		var reRegged map[string]interface{}
		reRegRespBody, err := io.ReadAll(reRegResp.Body)
		Expect(err).NotTo(HaveOccurred())
		Expect(json.Unmarshal(reRegRespBody, &reRegged)).To(Succeed())
		Expect(reRegged["agent_id"]).To(Equal(agentID))
	})

	It("rejects unauthenticated agent registration with 401", func() {
		createBody := `{
			"name": "should-fail",
			"environment": "test",
			"service_types": ["vm"],
			"cost": "low",
			"topic_name": "dcm.agent.should-fail"
		}`
		resp := doRequestWithBody(http.MethodPost, "/agents", createBody)
		problem := readProblemResponse(resp)
		Expect(resp.StatusCode).To(Equal(http.StatusUnauthorized))
		Expect(problem.Detail).To(Equal("missing authentication"))
	})

	It("rejects unauthenticated instance creation with 401 (TC-36 Step 3)", func() {
		createBody := `{
			"api_version": "v1alpha1",
			"display_name": "unauth-test-instance",
			"spec": {
				"catalog_item_id": "nonexistent",
				"user_values": []
			}
		}`
		resp := doRequestWithBody(http.MethodPost, "/catalog-item-instances", createBody)
		problem := readProblemResponse(resp)
		Expect(resp.StatusCode).To(Equal(http.StatusUnauthorized))
		Expect(problem.Detail).To(Equal("missing authentication"))
	})
})
