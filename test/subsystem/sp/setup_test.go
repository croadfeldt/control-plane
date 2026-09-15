//go:build subsystem

package subsystem_test

import (
	"context"
	_ "embed"
	"encoding/json"
	"net/http"
	"os"
	"strings"

	agentapi "github.com/dcm-project/control-plane/api/agent/v1alpha1"
	catalogapi "github.com/dcm-project/control-plane/api/catalog/v1alpha1"
	policyapi "github.com/dcm-project/control-plane/api/policy/v1alpha1"
	"github.com/dcm-project/control-plane/api/sp/v1alpha1/resource_manager"
	"github.com/dcm-project/control-plane/internal/catalog/testutil"
	"github.com/dcm-project/control-plane/internal/sp/messaging"
	"github.com/google/uuid"
	"github.com/nats-io/nats.go"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// agentSelectingPolicyRego, twoAgentSelectingPolicyRego, and
// threeAgentSelectingPolicyRego are Rego policy templates for the test-data
// below, with placeholder agent names substituted in at runtime. See
// createAgentSelectingPolicy, createTwoAgentPolicy, and createThreeAgentPolicy.
var (
	//go:embed testdata/agent_selecting_policy.rego
	agentSelectingPolicyRego string

	//go:embed testdata/two_agent_selecting_policy.rego
	twoAgentSelectingPolicyRego string

	//go:embed testdata/three_agent_selecting_policy.rego
	threeAgentSelectingPolicyRego string
)

func wireMockURL() string {
	if url := os.Getenv("WIREMOCK_URL"); url != "" {
		return url
	}
	return "http://localhost:9090"
}

func resetWireMock() {
	req, _ := http.NewRequest(http.MethodDelete, wireMockURL()+"/__admin/mappings", nil)
	http.DefaultClient.Do(req)
}

func natsURL() string {
	if url := os.Getenv("NATS_URL"); url != "" {
		return url
	}
	return "nats://localhost:4222"
}

// publishResponseEvent simulates a real agent's response for resourceID,
// since this stack has no live agent to send one. It publishes directly to
// messaging.ResponseSubject - the same wire contract consumer.ResponseConsumer
// listens on - rather than going through internal/sp/messaging.Publisher,
// which is a control-plane outbound (create/delete/cancel request) client,
// not a stand-in for an agent's own response producer.
func publishResponseEvent(ceType, resourceID, agentName string) {
	nc, err := nats.Connect(natsURL())
	ExpectWithOffset(1, err).NotTo(HaveOccurred())
	defer nc.Close()

	envelope := map[string]any{
		"specversion": messaging.CESpecVersion,
		"type":        ceType,
		"source":      "sp-subsystem-test-agent",
		"id":          uuid.New().String(),
		"data": map[string]string{
			"resource_id": resourceID,
			"agent_name":  agentName,
		},
	}
	data, err := json.Marshal(envelope)
	ExpectWithOffset(1, err).NotTo(HaveOccurred())

	ExpectWithOffset(1, nc.Publish(messaging.ResponseSubject, data)).To(Succeed())
	ExpectWithOffset(1, nc.Flush()).To(Succeed())
}

// acknowledgeDeletion simulates the real agent's confirmation that a
// resource's physical deletion is complete.
func acknowledgeDeletion(resourceID, agentName string) {
	publishResponseEvent(messaging.CETypeDeletionAcknowledged, resourceID, agentName)
}

func ptr[T any](v T) *T {
	return &v
}

// registerReadyAgent registers a new agent (unique name) supporting
// serviceType and returns its name. Registration marks the agent "ready"
// immediately, no heartbeat required.
func registerReadyAgent(serviceType string) string {
	name := "sp-subsystem-agent-" + uuid.New().String()[:8]
	body := agentapi.AgentRegistrationRequest{
		Name:         name,
		Environment:  "test",
		ServiceTypes: []string{serviceType},
		Cost:         agentapi.AgentRegistrationRequestCostMedium,
		TopicName:    "dcm.agent." + name,
	}

	resp, err := agentApiClient.CreateAgentWithResponse(context.Background(), body)
	ExpectWithOffset(1, err).NotTo(HaveOccurred())
	ExpectWithOffset(1, resp.StatusCode()).To(Equal(http.StatusCreated))

	return name
}

// createAgentSelectingPolicy creates an enabled policy that deterministically
// selects agentName whenever it appears in available_agents. Returns the
// policy ID for later cleanup.
func createAgentSelectingPolicy(agentName string) string {
	regoCode := strings.ReplaceAll(agentSelectingPolicyRego, "__AGENT_NAME__", agentName)

	body := policyapi.Policy{
		DisplayName: ptr("sp-subsystem agent selector " + agentName),
		PolicyType:  ptr(policyapi.GLOBAL),
		Priority:    ptr(int32(500)),
		Enabled:     ptr(true),
		RegoCode:    ptr(regoCode),
	}

	resp, err := policyApiClient.CreatePolicyWithResponse(context.Background(), &policyapi.CreatePolicyParams{}, body)
	ExpectWithOffset(1, err).NotTo(HaveOccurred())
	ExpectWithOffset(1, resp.StatusCode()).To(Equal(http.StatusCreated))

	return *resp.JSON201.Id
}

// createTwoAgentPolicy creates an enabled policy that prefers agentA over
// agentB, picking whichever is still present in available_agents. Policy
// evaluation pre-filters exclude_agents out of available_agents before Rego
// runs, so once the sweep excludes agentA the same policy resolves to
// agentB - driving both initial routing and self-heal re-evaluation with one
// policy. The names are baked in explicitly (rather than picking
// available_agents[0]) because the shared test DB accumulates "ready" agents
// left behind by other specs across the suite run, and index 0 isn't
// guaranteed to be this test's own agent. Returns the policy ID for cleanup.
func createTwoAgentPolicy(agentA, agentB string) string {
	regoCode := strings.NewReplacer(
		"__AGENT_A__", agentA,
		"__AGENT_B__", agentB,
	).Replace(twoAgentSelectingPolicyRego)

	body := policyapi.Policy{
		DisplayName: ptr("sp-subsystem two-agent selector"),
		PolicyType:  ptr(policyapi.GLOBAL),
		Priority:    ptr(int32(500)),
		Enabled:     ptr(true),
		RegoCode:    ptr(regoCode),
	}

	resp, err := policyApiClient.CreatePolicyWithResponse(context.Background(), &policyapi.CreatePolicyParams{}, body)
	ExpectWithOffset(1, err).NotTo(HaveOccurred())
	ExpectWithOffset(1, resp.StatusCode()).To(Equal(http.StatusCreated))

	return *resp.JSON201.Id
}

// createThreeAgentPolicy extends createTwoAgentPolicy's prefer-A/fallback-B
// rules with an unconditional "prefer C" rule. Policy evaluation
// pre-filters available_agents by service-type capability before Rego
// runs, so as long as only agentC is registered for the service type under
// evaluation (e.g. "database"), the C rule only ever fires for that
// resource - no service-type branching needed inside Rego itself. Returns
// the policy ID for cleanup.
func createThreeAgentPolicy(agentA, agentB, agentC string) string {
	regoCode := strings.NewReplacer(
		"__AGENT_A__", agentA,
		"__AGENT_B__", agentB,
		"__AGENT_C__", agentC,
	).Replace(threeAgentSelectingPolicyRego)

	body := policyapi.Policy{
		DisplayName: ptr("sp-subsystem three-agent selector"),
		PolicyType:  ptr(policyapi.GLOBAL),
		Priority:    ptr(int32(500)),
		Enabled:     ptr(true),
		RegoCode:    ptr(regoCode),
	}

	resp, err := policyApiClient.CreatePolicyWithResponse(context.Background(), &policyapi.CreatePolicyParams{}, body)
	ExpectWithOffset(1, err).NotTo(HaveOccurred())
	ExpectWithOffset(1, resp.StatusCode()).To(Equal(http.StatusCreated))

	return *resp.JSON201.Id
}

// createTestCatalogItem creates a single-resource catalog item for
// serviceType with a single editable "vcpu.count" field, matching the
// seeded "vm" service type schema.
func createTestCatalogItem(serviceType string) string {
	id := "sp-subsystem-ci-" + uuid.New().String()[:8]
	editable := true
	fields := []catalogapi.FieldConfiguration{{
		Path:        "vcpu.count",
		DisplayName: ptr("vCPU Count"),
		Editable:    &editable,
		Default:     float64(2),
		ValidationSchema: &map[string]any{
			"type":    "number",
			"minimum": float64(1),
			"maximum": float64(16),
		},
	}}

	params := &catalogapi.CreateCatalogItemParams{Id: &id}
	body := catalogapi.CatalogItem{
		ApiVersion:  ptr("v1alpha1"),
		DisplayName: ptr("SP subsystem test item " + id),
		Spec:        testutil.PtrCatalogSpec(serviceType, fields),
	}

	resp, err := catalogApiClient.CreateCatalogItemWithResponse(context.Background(), params, body)
	ExpectWithOffset(1, err).NotTo(HaveOccurred())
	ExpectWithOffset(1, resp.StatusCode()).To(Equal(http.StatusCreated))

	return id
}

type siblingResource struct {
	Name        string
	ServiceType string
}

// createSiblingCatalogItem builds a multi-resource CatalogItem, one entry
// per siblingResource, with no RequiresResources (so every resource lands
// at dag_level 0 and is provisioned concurrently, per assignDagLevels) and
// no Fields (CatalogResource.Fields is optional - buildResourceSpecFromFields's
// per-field defaulting loop is a no-op on an empty slice). Returns the
// catalog item ID.
func createSiblingCatalogItem(resources ...siblingResource) string {
	// display_name is capped at 63 chars by the schema: keep both the ID
	// and the prefix short enough that id's own length doesn't push the
	// concatenated display_name over that limit.
	id := "sp-subsystem-ci-sib-" + uuid.New().String()[:8]
	apiResources := make([]catalogapi.CatalogResource, len(resources))
	for i, r := range resources {
		apiResources[i] = catalogapi.CatalogResource{
			Name:        r.Name,
			ServiceType: r.ServiceType,
		}
	}

	params := &catalogapi.CreateCatalogItemParams{Id: &id}
	body := catalogapi.CatalogItem{
		ApiVersion:  ptr("v1alpha1"),
		DisplayName: ptr("SP subsystem sibling item " + id),
		Spec:        &catalogapi.CatalogItemSpec{Resources: apiResources},
	}

	resp, err := catalogApiClient.CreateCatalogItemWithResponse(context.Background(), params, body)
	ExpectWithOffset(1, err).NotTo(HaveOccurred())
	ExpectWithOffset(1, resp.StatusCode()).To(Equal(http.StatusCreated))

	return id
}

// listInstanceIDsByAgent returns the IDs of every instance currently on
// agentName, for callers that only care about the ID *set* - e.g. a pair of
// same-service-type siblings sharing one agent, where individual identity
// doesn't matter as long as both move together. Takes a Gomega (Default
// outside a poll, the closure's g inside one) rather than asserting via the
// package-level ExpectWithOffset, so a transient API error inside an
// Eventually/Consistently triggers a normal retry instead of hard-failing
// the spec.
func listInstanceIDsByAgent(g Gomega, agentName string) []string {
	listResp, err := rmApiClient.ListInstancesWithResponse(context.Background(), &resource_manager.ListInstancesParams{AgentName: &agentName})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(listResp.StatusCode()).To(Equal(http.StatusOK))
	if listResp.JSON200.Instances == nil {
		return nil
	}

	ids := make([]string, 0, len(*listResp.JSON200.Instances))
	for _, inst := range *listResp.JSON200.Instances {
		if inst.Id != nil {
			ids = append(ids, *inst.Id)
		}
	}
	return ids
}

// findInstanceByAgentAndServiceType looks up the single instance on
// agentName with serviceType, for siblings distinguished by service type
// rather than by ID set. See listInstanceIDsByAgent for why it takes a
// Gomega instead of using the package-level Expect.
func findInstanceByAgentAndServiceType(g Gomega, agentName, serviceType string) string {
	listResp, err := rmApiClient.ListInstancesWithResponse(context.Background(), &resource_manager.ListInstancesParams{
		AgentName:   &agentName,
		ServiceType: &serviceType,
	})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(listResp.StatusCode()).To(Equal(http.StatusOK))
	g.Expect(listResp.JSON200.Instances).NotTo(BeNil())
	g.Expect(*listResp.JSON200.Instances).To(HaveLen(1), "expected exactly one %q instance on agent %q", serviceType, agentName)

	return *(*listResp.JSON200.Instances)[0].Id
}

// createInstanceViaAgent drives instance creation through the real
// production path: register a ready agent, install a policy that routes to
// it, create a catalog item, and create a catalog item instance. Placement
// resolves the agent via policy and calls the in-process SPRM client with a
// real agent name - the only path CreateInstance's agent_name validation
// allows through. Returns the agent name and the resulting resource_manager
// instance ID (found by matching agent_name, since catalog item instances
// don't expose the underlying resource ID).
func createInstanceViaAgent() (agentName, instanceID string) {
	const serviceType = "vm"

	agentName = registerReadyAgent(serviceType)
	policyID := createAgentSelectingPolicy(agentName)
	DeferCleanup(func() {
		_, _ = policyApiClient.DeletePolicyWithResponse(context.Background(), policyID)
	})

	catalogItemID := createTestCatalogItem(serviceType)

	instID := "sp-subsystem-inst-" + uuid.New().String()[:8]
	instParams := &catalogapi.CreateCatalogItemInstanceParams{Id: &instID}
	instBody := catalogapi.CatalogItemInstance{
		ApiVersion:  "v1alpha1",
		DisplayName: instID, // display_name is capped at 63 chars by the schema
		Spec: catalogapi.CatalogItemInstanceSpec{
			CatalogItemId: catalogItemID,
			UserValues:    []catalogapi.UserValue{},
		},
	}

	instResp, err := catalogApiClient.CreateCatalogItemInstanceWithResponse(context.Background(), instParams, instBody)
	ExpectWithOffset(1, err).NotTo(HaveOccurred())
	ExpectWithOffset(1, instResp.StatusCode()).To(Equal(http.StatusCreated))

	DeferCleanup(func() {
		_, _ = catalogApiClient.DeleteCatalogItemInstanceWithResponse(context.Background(), instID)
	})

	return agentName, findInstanceByAgentName(agentName)
}

// findInstanceByAgentName looks up the resource_manager instance for a
// freshly minted, per-test agent name via the API's own agent_name filter,
// rather than scanning a fixed-size page of ListInstances: the shared
// suite-wide DB accumulates instances across specs (ordered oldest-first by
// create_time), so a fresh instance can fall off a fixed first page and be
// misreported as "not found" instead of a real routing failure.
func findInstanceByAgentName(agentName string) string {
	listResp, err := rmApiClient.ListInstancesWithResponse(context.Background(), &resource_manager.ListInstancesParams{AgentName: &agentName})
	ExpectWithOffset(1, err).NotTo(HaveOccurred())
	ExpectWithOffset(1, listResp.StatusCode()).To(Equal(http.StatusOK))
	ExpectWithOffset(1, listResp.JSON200.Instances).NotTo(BeNil())
	ExpectWithOffset(1, *listResp.JSON200.Instances).To(HaveLen(1), "expected exactly one instance for agent %q", agentName)

	return *(*listResp.JSON200.Instances)[0].Id
}

// findInstanceByEitherAgent is findInstanceByAgentName for the self-heal
// test, which doesn't know upfront which of its two freshly registered
// agents the policy actually selected.
func findInstanceByEitherAgent(agentA, agentB string) (instanceID, matchedAgent string) {
	listResp, err := rmApiClient.ListInstancesWithResponse(context.Background(), &resource_manager.ListInstancesParams{AgentName: &agentA})
	ExpectWithOffset(1, err).NotTo(HaveOccurred())
	ExpectWithOffset(1, listResp.StatusCode()).To(Equal(http.StatusOK))
	ExpectWithOffset(1, listResp.JSON200.Instances).NotTo(BeNil())
	if len(*listResp.JSON200.Instances) == 1 {
		return *(*listResp.JSON200.Instances)[0].Id, agentA
	}

	listResp, err = rmApiClient.ListInstancesWithResponse(context.Background(), &resource_manager.ListInstancesParams{AgentName: &agentB})
	ExpectWithOffset(1, err).NotTo(HaveOccurred())
	ExpectWithOffset(1, listResp.StatusCode()).To(Equal(http.StatusOK))
	ExpectWithOffset(1, listResp.JSON200.Instances).NotTo(BeNil())
	ExpectWithOffset(1, *listResp.JSON200.Instances).To(HaveLen(1), "instance routed to neither agent %q nor %q", agentA, agentB)

	return *(*listResp.JSON200.Instances)[0].Id, agentB
}
