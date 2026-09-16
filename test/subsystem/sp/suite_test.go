//go:build subsystem

package subsystem_test

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	agentClient "github.com/dcm-project/control-plane/pkg/agent/client"
	catalogClient "github.com/dcm-project/control-plane/pkg/catalog/client"
	policyClient "github.com/dcm-project/control-plane/pkg/policy/client"
	rmClient "github.com/dcm-project/control-plane/pkg/sp/client/resource_manager"
)

// Package-level API clients, shared across this suite's test files. All four
// domains (agent, catalog, policy, resource_manager) are served by the same
// control-plane binary at the same base URL.
var (
	rmApiClient      *rmClient.ClientWithResponses
	agentApiClient   *agentClient.ClientWithResponses
	catalogApiClient *catalogClient.ClientWithResponses
	policyApiClient  *policyClient.ClientWithResponses
)

func TestE2E(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "E2E Suite")
}

var _ = BeforeSuite(func() {
	baseURL := apiBaseURL()

	authEditor := func(_ context.Context, req *http.Request) error {
		req.Header.Set("X-Forwarded-User", "test-admin-sub")
		return nil
	}

	var err error
	rmApiClient, err = rmClient.NewClientWithResponses(baseURL, rmClient.WithRequestEditorFn(authEditor))
	Expect(err).NotTo(HaveOccurred())

	agentApiClient, err = agentClient.NewClientWithResponses(baseURL, agentClient.WithRequestEditorFn(authEditor))
	Expect(err).NotTo(HaveOccurred())

	catalogApiClient, err = catalogClient.NewClientWithResponses(baseURL, catalogClient.WithRequestEditorFn(authEditor))
	Expect(err).NotTo(HaveOccurred())

	policyApiClient, err = policyClient.NewClientWithResponses(baseURL, policyClient.WithRequestEditorFn(authEditor))
	Expect(err).NotTo(HaveOccurred())

	Eventually(func() error {
		resp, err := http.Get(baseURL + "/health")
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("health check returned %d", resp.StatusCode)
		}
		return nil
	}).WithTimeout(60 * time.Second).WithPolling(2 * time.Second).Should(Succeed())
})

func apiBaseURL() string {
	if url := os.Getenv("API_URL"); url != "" {
		return url
	}
	return "http://localhost:8080/api/v1alpha1"
}
