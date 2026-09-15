//go:build subsystem

package subsystem_test

import (
	"context"
	"net/http"
	"time"

	"github.com/dcm-project/control-plane/api/sp/v1alpha1/resource_manager"
	"github.com/dcm-project/control-plane/internal/sp/messaging"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// eventuallyStatus polls the instance's status via the API against a real
// NATS/JetStream broker: the response consumer processes the published
// event asynchronously, so the transition isn't guaranteed to be visible
// immediately after publish. ShowDeleted is always set: pending_deletion is
// one of the statuses this helper polls for, and GetInstance 404s on
// soft-deleted instances otherwise.
//
// Assertions run through the polled func(g Gomega) rather than the package-
// level Expect, so a single transient failure (e.g. a momentary non-200)
// is treated as "try again next poll" instead of aborting the spec outright.
func eventuallyStatus(instanceID string) AsyncAssertion {
	return Eventually(func(g Gomega) string {
		params := &resource_manager.GetInstanceParams{ShowDeleted: ptr(true)}
		getResp, err := rmApiClient.GetInstanceWithResponse(context.Background(), instanceID, params)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(getResp.StatusCode()).To(Equal(http.StatusOK))
		g.Expect(getResp.JSON200.Status).NotTo(BeNil())
		return *getResp.JSON200.Status
	}).WithTimeout(10 * time.Second).WithPolling(500 * time.Millisecond)
}

var _ = Describe("Agent response events", func() {
	BeforeEach(func() {
		resetWireMock()
	})

	It("moves a pending instance to provisioning on creation-acknowledged", func() {
		agentName, instanceID := createInstanceViaAgent()

		publishResponseEvent(messaging.CETypeCreationAcknowledged, instanceID, agentName)

		eventuallyStatus(instanceID).Should(Equal("provisioning"))
	})

	It("moves a pending instance to queued on request-queued", func() {
		agentName, instanceID := createInstanceViaAgent()

		publishResponseEvent(messaging.CETypeRequestQueued, instanceID, agentName)

		eventuallyStatus(instanceID).Should(Equal("queued"))
	})

	It("moves an instance to failed on error", func() {
		agentName, instanceID := createInstanceViaAgent()

		publishResponseEvent(messaging.CETypeError, instanceID, agentName)

		eventuallyStatus(instanceID).Should(Equal("failed"))
	})

	It("moves a queued instance to cancelled on cancel-acknowledged", func() {
		agentName, instanceID := createInstanceViaAgent()
		publishResponseEvent(messaging.CETypeRequestQueued, instanceID, agentName)
		eventuallyStatus(instanceID).Should(Equal("queued"))

		publishResponseEvent(messaging.CETypeCancelAcknowledged, instanceID, agentName)

		eventuallyStatus(instanceID).Should(Equal("cancelled"))
	})

	It("moves a queued instance to pending_deletion on cancel-rejected", func() {
		agentName, instanceID := createInstanceViaAgent()
		publishResponseEvent(messaging.CETypeRequestQueued, instanceID, agentName)
		eventuallyStatus(instanceID).Should(Equal("queued"))

		publishResponseEvent(messaging.CETypeCancelRejected, instanceID, agentName)

		eventuallyStatus(instanceID).Should(Equal("pending_deletion"))

		// The instanceStatus/eventuallyStatus helpers above pass
		// ShowDeleted:true because pending_deletion is soft-deleted; verify
		// that's actually load-bearing, not just a harmless default, by
		// confirming the plain (default-filtered) GET eventually 404s.
		Eventually(func(g Gomega) int {
			getResp, err := rmApiClient.GetInstanceWithResponse(context.Background(), instanceID, nil)
			g.Expect(err).NotTo(HaveOccurred())
			return getResp.StatusCode()
		}).WithTimeout(5 * time.Second).WithPolling(100 * time.Millisecond).Should(Equal(http.StatusNotFound))
	})
})
