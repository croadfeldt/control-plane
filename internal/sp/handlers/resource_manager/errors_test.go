package resource_manager

import (
	server "github.com/dcm-project/control-plane/internal/sp/api/resource_manager"
	"github.com/dcm-project/control-plane/internal/sp/service"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("handleDeleteInstanceError", func() {
	It("maps a ProvisioningError to 422 instead of the generic 500 default (R2 S7: finding #1)", func() {
		// DeleteInstance's non-deferred path returns this when publishing
		// the delete to the agent fails: a transient, client-actionable
		// failure to carry out the delete, not an internal server bug.
		resp := handleDeleteInstanceError(service.NewProvisioningError("failed to publish delete for instance x: nats unavailable"))

		typedResp, ok := resp.(server.DeleteInstance422ApplicationProblemPlusJSONResponse)
		Expect(ok).To(BeTrue())
		Expect(typedResp.Type).To(Equal("provisioning-error"))
	})

	It("still maps unrecognized errors to the generic 500 default", func() {
		resp := handleDeleteInstanceError(service.NewInternalError("boom"))

		defResp, ok := resp.(server.DeleteInstancedefaultApplicationProblemPlusJSONResponse)
		Expect(ok).To(BeTrue())
		Expect(defResp.StatusCode).To(Equal(500))
	})
})
