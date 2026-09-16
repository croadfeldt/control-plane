//go:build subsystem

package subsystem_test

import (
	"io"
	"net/http"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// TC-30: Auth disabled mode ignores garbage auth headers.
// Unlike the unit test in internal/auth/middleware_test.go, this verifies
// the behavior end-to-end through the full HTTP stack with AUTH_DISABLED=true
// set via environment variable in the compose config.
var _ = Describe("Auth disabled mode ignores garbage headers", func() {
	baseURL := strings.TrimRight(envOrDefault("CATALOG_MANAGER_URL", "http://localhost:28080"), "/") + "/api/v1alpha1"

	doGet := func(path string, headers map[string]string) *http.Response {
		GinkgoHelper()
		req, err := http.NewRequest(http.MethodGet, baseURL+path, nil)
		Expect(err).NotTo(HaveOccurred())
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		resp, err := httpClient.Do(req)
		Expect(err).NotTo(HaveOccurred())
		return resp
	}

	It("succeeds with a garbage Bearer token on a protected endpoint", func() {
		resp := doGet("/service-types", map[string]string{
			"Authorization": "Bearer garbage.invalid.token",
		})
		defer resp.Body.Close()
		Expect(resp.StatusCode).To(Equal(http.StatusOK))
		Expect(resp.Header.Get("Content-Type")).To(ContainSubstring("application/json"))
	})

	It("succeeds with a wrong proxy secret and garbage user", func() {
		resp := doGet("/catalog-items", map[string]string{
			"X-Auth-Proxy-Secret": "completely-wrong-secret",
			"X-Forwarded-User":   "garbage-user-id",
		})
		defer resp.Body.Close()
		Expect(resp.StatusCode).To(Equal(http.StatusOK))
		Expect(resp.Header.Get("Content-Type")).To(ContainSubstring("application/json"))
	})

	It("succeeds with all garbage auth headers combined", func() {
		resp := doGet("/service-types", map[string]string{
			"Authorization":                  "Bearer garbage.invalid.token",
			"X-Auth-Proxy-Secret":            "wrong-secret",
			"X-Forwarded-User":               "garbage-user",
			"X-Forwarded-Preferred-Username": "garbage-username",
		})
		defer resp.Body.Close()
		Expect(resp.StatusCode).To(Equal(http.StatusOK))

		body, err := io.ReadAll(resp.Body)
		Expect(err).NotTo(HaveOccurred())
		Expect(body).NotTo(BeEmpty())
	})

	It("succeeds with an empty Bearer token value", func() {
		resp := doGet("/catalog-items", map[string]string{
			"Authorization": "Bearer ",
		})
		defer resp.Body.Close()
		Expect(resp.StatusCode).To(Equal(http.StatusOK))
	})
})
