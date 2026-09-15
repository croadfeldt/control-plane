//go:build subsystem

package subsystem_test

import (
	"context"
	"net/http"
	"time"

	catalogapi "github.com/dcm-project/control-plane/api/catalog/v1alpha1"
	"github.com/dcm-project/control-plane/internal/sp/messaging"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// createCatalogItemInstance creates a catalog item instance from
// catalogItemID and registers its own cleanup. Shared by every self-heal
// test below (which don't otherwise need the resulting instance/agent
// identity createInstanceViaAgent returns, since they look resources up via
// findInstanceByAgentName/listInstanceIDsByAgent/
// findInstanceByAgentAndServiceType instead).
func createCatalogItemInstance(catalogItemID string) {
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
}

// Self-heal loop: a pending instance whose agent never acknowledges it gets
// automatically re-routed to an alternate agent once AGENT_PENDING_REQUEST_TIMEOUT
// elapses, driven by the real sweep against the real NATS/JetStream broker
// (docker-compose.yaml tunes both timeouts down for this test).
var _ = Describe("Self-heal on pending timeout", func() {
	BeforeEach(func() {
		resetWireMock()
	})

	It("reassigns a never-acknowledged pending instance to the alternate ready agent", func() {
		const serviceType = "vm"

		agentA := registerReadyAgent(serviceType)
		agentB := registerReadyAgent(serviceType)
		policyID := createTwoAgentPolicy(agentA, agentB)
		DeferCleanup(func() {
			_, _ = policyApiClient.DeletePolicyWithResponse(context.Background(), policyID)
		})

		catalogItemID := createTestCatalogItem(serviceType)
		createCatalogItemInstance(catalogItemID)

		// agentA/agentB are freshly minted per-test names: querying by
		// agent_name directly (rather than scanning a fixed-size page of
		// ListInstances, oldest-first) finds the instance regardless of how
		// many instances the shared suite-wide DB has accumulated.
		instanceID, initialAgent := findInstanceByEitherAgent(agentA, agentB)

		expectedAlternate := agentB
		if initialAgent == agentB {
			expectedAlternate = agentA
		}

		// Never acknowledge creation: past AGENT_PENDING_REQUEST_TIMEOUT the
		// sweep must exclude initialAgent, re-evaluate, and land on the only
		// other ready agent - with a fresh pending cycle, not stuck/failed.
		// Polled as a single (agent, status) pair rather than two separate
		// Eventually calls, so a transient in-between state can't pass either
		// check on its own.
		type instanceState struct {
			AgentName string
			Status    string
		}
		Eventually(func(g Gomega) instanceState {
			getResp, err := rmApiClient.GetInstanceWithResponse(context.Background(), instanceID, nil)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(getResp.StatusCode()).To(Equal(http.StatusOK))
			var st instanceState
			if getResp.JSON200.AgentName != nil {
				st.AgentName = *getResp.JSON200.AgentName
			}
			if getResp.JSON200.Status != nil {
				st.Status = *getResp.JSON200.Status
			}
			return st
		}).WithTimeout(30 * time.Second).WithPolling(1 * time.Second).
			Should(Equal(instanceState{AgentName: expectedAlternate, Status: "pending"}))
	})
})

// Sibling reassignment: reassignExcludedSiblings (placement.go) proactively
// reassigns a run-sibling still pointed at an excluded agent instead of
// waiting for its own independent sweep timeout. The first three tests
// exercise that path directly against the real NATS-driven sweep and the
// real ReassignAndReset CAS; the last is a single-resource complement
// covering the sweep's retry-exhaustion path (see its own comment) - see
// PR #37 review thread r3761347505.
var _ = Describe("Sibling reassignment during self-heal", func() {
	BeforeEach(func() {
		resetWireMock()
	})

	It("proactively reassigns a cancelled run-sibling stuck on the excluded agent", func() {
		agentA := registerReadyAgent("vm")
		agentB := registerReadyAgent("vm")
		policyID := createTwoAgentPolicy(agentA, agentB)
		DeferCleanup(func() {
			_, _ = policyApiClient.DeletePolicyWithResponse(context.Background(), policyID)
		})

		catalogItemID := createSiblingCatalogItem(
			siblingResource{Name: "primary", ServiceType: "vm"},
			siblingResource{Name: "sibling", ServiceType: "vm"},
		)
		createCatalogItemInstance(catalogItemID)

		// createTwoAgentPolicy deterministically prefers agentA, so both
		// same-service-type siblings land there synchronously as part of
		// instance creation (CreateRun provisions dag_level-0 resources
		// inline, before the instance-creation call returns).
		initialIDs := listInstanceIDsByAgent(Default, agentA)
		Expect(initialIDs).To(HaveLen(2))
		primaryID, siblingID := initialIDs[0], initialIDs[1]

		// Drive the sibling to "cancelled" via the real queued -> cancelled
		// event path, without ever touching the primary. Leaving it
		// "pending" instead would be ambiguous: both siblings start pending
		// at essentially the same instant, so sweepPending's own per-row
		// query would independently heal a still-pending sibling in the
		// same tick it catches the primary, reaching the same end state
		// even with reassignExcludedSiblings deleted entirely. "cancelled",
		// unlike "pending", is never independently revisited by either
		// sweep (sweepPending only scans status=pending; sweepQueued only
		// scans status=queued), so the sibling can *only* move again via
		// reassignExcludedSiblings's CAS, which explicitly allows pending
		// OR cancelled.
		// This is racing the primary's 5s pending timeout: if the sibling
		// were still "queued" (not yet "cancelled") when the primary's sweep
		// fires, reassignExcludedSiblings would skip it that tick, since
		// "queued" isn't CAS-eligible either. In a healthy stack these two
		// event round-trips settle in well under a second, so this isn't
		// expected to flake; it would only surface if the consumer were
		// already failing to process events (e.g. a DB error triggering the
		// nak-and-redeliver path in handleRequestQueued), which would be a
		// real bug worth surfacing as a failure anyway.
		publishResponseEvent(messaging.CETypeRequestQueued, siblingID, agentA)
		eventuallyStatus(siblingID).Should(Equal("queued"))
		publishResponseEvent(messaging.CETypeCancelAcknowledged, siblingID, agentA)
		eventuallyStatus(siblingID).Should(Equal("cancelled"))

		// Never acknowledge the primary: past the pending timeout the sweep
		// excludes agentA for it, reassigns it to agentB, and
		// reassignExcludedSiblings proactively reassigns the cancelled
		// sibling too - both land on agentB with a fresh pending cycle, not
		// stuck on agentA or split across agents.
		Eventually(func(g Gomega) []string {
			return listInstanceIDsByAgent(g, agentB)
		}).WithTimeout(30 * time.Second).WithPolling(1 * time.Second).Should(ConsistOf(primaryID, siblingID))
		Expect(listInstanceIDsByAgent(Default, agentA)).To(BeEmpty())

		for _, id := range []string{primaryID, siblingID} {
			getResp, err := rmApiClient.GetInstanceWithResponse(context.Background(), id, nil)
			Expect(err).NotTo(HaveOccurred())
			Expect(getResp.StatusCode()).To(Equal(http.StatusOK))
			Expect(getResp.JSON200.Status).NotTo(BeNil())
			Expect(*getResp.JSON200.Status).To(Equal("pending"))
		}
	})

	It("leaves an already-provisioning sibling on the excluded agent untouched", func() {
		agentA := registerReadyAgent("vm")
		agentB := registerReadyAgent("vm")
		policyID := createTwoAgentPolicy(agentA, agentB)
		DeferCleanup(func() {
			_, _ = policyApiClient.DeletePolicyWithResponse(context.Background(), policyID)
		})

		catalogItemID := createSiblingCatalogItem(
			siblingResource{Name: "primary", ServiceType: "vm"},
			siblingResource{Name: "sibling", ServiceType: "vm"},
		)
		createCatalogItemInstance(catalogItemID)

		initialIDs := listInstanceIDsByAgent(Default, agentA)
		Expect(initialIDs).To(HaveLen(2))
		ackedID, stillPendingID := initialIDs[0], initialIDs[1]

		// Move one sibling to "provisioning" - no longer CAS-eligible for
		// ReassignAndReset - before the pending timeout fires; leave the
		// other pending.
		publishResponseEvent(messaging.CETypeCreationAcknowledged, ackedID, agentA)
		eventuallyStatus(ackedID).Should(Equal("provisioning"))

		// Past the pending timeout: sweepPending only ever picks up
		// stillPendingID (ackedID no longer matches its status=pending
		// filter), which self-heals to agentB. Whether or not
		// reassignExcludedSiblings also attempts ackedID as part of that
		// (its CAS would reject it, since it's no longer pending/cancelled),
		// the observable guarantee this asserts - a provisioning instance is
		// never reassigned by self-heal - holds either way.
		Eventually(func(g Gomega) []string {
			return listInstanceIDsByAgent(g, agentB)
		}).WithTimeout(30 * time.Second).WithPolling(1 * time.Second).Should(ConsistOf(stillPendingID))

		getResp, err := rmApiClient.GetInstanceWithResponse(context.Background(), ackedID, nil)
		Expect(err).NotTo(HaveOccurred())
		Expect(getResp.StatusCode()).To(Equal(http.StatusOK))
		Expect(getResp.JSON200.AgentName).NotTo(BeNil())
		Expect(*getResp.JSON200.AgentName).To(Equal(agentA))
		Expect(getResp.JSON200.Status).NotTo(BeNil())
		Expect(*getResp.JSON200.Status).To(Equal("provisioning"))
	})

	It("leaves a sibling on a different, never-excluded agent untouched", func() {
		agentA := registerReadyAgent("vm")
		agentB := registerReadyAgent("vm")
		agentC := registerReadyAgent("database")
		policyID := createThreeAgentPolicy(agentA, agentB, agentC)
		DeferCleanup(func() {
			_, _ = policyApiClient.DeletePolicyWithResponse(context.Background(), policyID)
		})

		catalogItemID := createSiblingCatalogItem(
			siblingResource{Name: "primary", ServiceType: "vm"},
			siblingResource{Name: "sibling", ServiceType: "database"},
		)
		createCatalogItemInstance(catalogItemID)

		primaryID := findInstanceByAgentAndServiceType(Default, agentA, "vm")
		siblingID := findInstanceByAgentAndServiceType(Default, agentC, "database")

		// Never acknowledge either. Past the pending timeout, the primary
		// (on excluded agentA) moves to agentB.
		Eventually(func(g Gomega) *string {
			getResp, err := rmApiClient.GetInstanceWithResponse(context.Background(), primaryID, nil)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(getResp.StatusCode()).To(Equal(http.StatusOK))
			return getResp.JSON200.AgentName
		}).WithTimeout(30 * time.Second).WithPolling(1 * time.Second).Should(HaveValue(Equal(agentB)))

		// The sibling's agent_name never moves off agentC, during the
		// primary's reassignment window or after: reassignExcludedSiblings
		// is the only thing in this flow that could move it, and agentC
		// was never in the excluded set. (Its own independent pending
		// timeout may separately drive its *status* toward "failed" once
		// retries exhaust, since agentC is the only database-capable
		// agent - that's covered by the retries-exhausted test below and
		// is irrelevant to the agent_name assertion here.)
		Consistently(func(g Gomega) *string {
			getResp, err := rmApiClient.GetInstanceWithResponse(context.Background(), siblingID, nil)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(getResp.StatusCode()).To(Equal(http.StatusOK))
			return getResp.JSON200.AgentName
		}).WithTimeout(5 * time.Second).WithPolling(1 * time.Second).Should(HaveValue(Equal(agentC)))
	})

	It("marks a pending instance failed once retries are exhausted with no alternate agent", func() {
		agentA := registerReadyAgent("vm")
		policyID := createAgentSelectingPolicy(agentA)
		DeferCleanup(func() {
			_, _ = policyApiClient.DeletePolicyWithResponse(context.Background(), policyID)
		})

		catalogItemID := createTestCatalogItem("vm")
		createCatalogItemInstance(catalogItemID)

		instanceID := findInstanceByAgentName(agentA)

		// Never acknowledge: with no alternate agent, every self-heal
		// attempt fails. This is a single, non-sibling resource - it
		// exercises the sweep's own retry-exhaustion -> markFailedFrom path
		// end-to-end (real DB CAS, real NATS-driven sweep), not the sibling
		// mechanism the other tests in this Describe target; sweep_test.go
		// covers the exhaustion timing/count logic at the unit level.
		Eventually(func(g Gomega) string {
			getResp, err := rmApiClient.GetInstanceWithResponse(context.Background(), instanceID, nil)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(getResp.StatusCode()).To(Equal(http.StatusOK))
			g.Expect(getResp.JSON200.Status).NotTo(BeNil())
			return *getResp.JSON200.Status
		}).WithTimeout(45 * time.Second).WithPolling(1 * time.Second).Should(Equal("failed"))
	})
})
