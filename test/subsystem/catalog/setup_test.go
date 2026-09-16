//go:build subsystem

package subsystem_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	. "github.com/onsi/gomega"

	v1alpha1 "github.com/dcm-project/control-plane/api/catalog/v1alpha1"
	"github.com/dcm-project/control-plane/internal/catalog/testutil"
)

// --- WireMock helpers ---

func resetWireMock() {
	req, err := http.NewRequest(http.MethodPost, wireMockURL+"/__admin/reset", nil)
	ExpectWithOffset(1, err).NotTo(HaveOccurred())
	resp, err := httpClient.Do(req)
	ExpectWithOffset(1, err).NotTo(HaveOccurred())
	defer resp.Body.Close()
	ExpectWithOffset(1, resp.StatusCode).To(Equal(http.StatusOK))
}

func stubPMCreateResource() {
	stub := map[string]any{
		"request": map[string]any{
			"method":  "POST",
			"urlPath": "/api/v1alpha1/runs",
		},
		"response": map[string]any{
			"status": 202,
			"headers": map[string]string{
				"Content-Type": "application/json",
			},
			"jsonBody": map[string]any{
				"run_id":                   "pm-run-1",
				"catalog_item_instance_id": "ignored",
				"resources": []map[string]any{
					{
						"id":   "pm-resource-1",
						"name": "main",
						"path": "resources/pm-resource-1",
						"spec": map[string]any{},
					},
				},
			},
		},
	}
	postWireMockMapping(stub)
}

func stubPMRehydrateResource() {
	stub := map[string]any{
		"request": map[string]any{
			"method":         "POST",
			"urlPathPattern": "/api/v1alpha1/runs/.*:rehydrate",
		},
		"response": map[string]any{
			"status": 202,
			"headers": map[string]string{
				"Content-Type": "application/json",
			},
			"jsonBody": map[string]any{
				"id":   "pm-rehydrated-resource",
				"path": "resources/pm-rehydrated-resource",
				"spec": map[string]any{},
			},
		},
	}
	postWireMockMapping(stub)
}

func stubPMRehydrateResourceFailure() {
	stub := map[string]any{
		"request": map[string]any{
			"method":         "POST",
			"urlPathPattern": "/api/v1alpha1/runs/.*:rehydrate",
		},
		"response": map[string]any{
			"status": 500,
			"headers": map[string]string{
				"Content-Type": "application/json",
			},
			"jsonBody": map[string]any{
				"type":   string(v1alpha1.INTERNAL),
				"title":  "Internal Server Error",
				"status": 500,
			},
		},
	}
	postWireMockMapping(stub)
}

func stubPMDeleteResource() {
	stub := map[string]any{
		"request": map[string]any{
			"method":         "DELETE",
			"urlPathPattern": "/api/v1alpha1/runs/.*",
		},
		"response": map[string]any{
			"status": 204,
		},
	}
	postWireMockMapping(stub)
}

func stubPMCreateResourcePolicyRejected() {
	stub := map[string]any{
		"request": map[string]any{
			"method":  "POST",
			"urlPath": "/api/v1alpha1/runs",
		},
		"response": map[string]any{
			"status": 406,
			"headers": map[string]string{
				"Content-Type": "application/json",
			},
			"jsonBody": map[string]any{
				"type":   string(v1alpha1.FAILEDPRECONDITION),
				"title":  "Policy rejected",
				"status": 406,
				"detail": "Request rejected by Policy Engine",
			},
		},
	}
	postWireMockMapping(stub)
}

func stubPMCreateResourceProviderError() {
	stub := map[string]any{
		"request": map[string]any{
			"method":  "POST",
			"urlPath": "/api/v1alpha1/runs",
		},
		"response": map[string]any{
			"status": 422,
			"headers": map[string]string{
				"Content-Type": "application/json",
			},
			"jsonBody": map[string]any{
				"type":   string(v1alpha1.FAILEDPRECONDITION),
				"title":  "Provider error",
				"status": 422,
				"detail": "SPRM provider error",
			},
		},
	}
	postWireMockMapping(stub)
}

func stubPMRehydrateResourcePolicyRejected() {
	stub := map[string]any{
		"request": map[string]any{
			"method":         "POST",
			"urlPathPattern": "/api/v1alpha1/runs/.*:rehydrate",
		},
		"response": map[string]any{
			"status": 406,
			"headers": map[string]string{
				"Content-Type": "application/json",
			},
			"jsonBody": map[string]any{
				"type":   string(v1alpha1.FAILEDPRECONDITION),
				"title":  "Policy rejected",
				"status": 406,
				"detail": "Request rejected by Policy Engine",
			},
		},
	}
	postWireMockMapping(stub)
}

func stubPMRehydrateResourceProviderError() {
	stub := map[string]any{
		"request": map[string]any{
			"method":         "POST",
			"urlPathPattern": "/api/v1alpha1/runs/.*:rehydrate",
		},
		"response": map[string]any{
			"status": 422,
			"headers": map[string]string{
				"Content-Type": "application/json",
			},
			"jsonBody": map[string]any{
				"type":   string(v1alpha1.FAILEDPRECONDITION),
				"title":  "Provider error",
				"status": 422,
				"detail": "SPRM provider error",
			},
		},
	}
	postWireMockMapping(stub)
}

func stubPMCreateResourceFailure() {
	stub := map[string]any{
		"request": map[string]any{
			"method":  "POST",
			"urlPath": "/api/v1alpha1/runs",
		},
		"response": map[string]any{
			"status": 500,
			"headers": map[string]string{
				"Content-Type": "application/json",
			},
			"jsonBody": map[string]any{
				"type":   string(v1alpha1.INTERNAL),
				"title":  "Internal Server Error",
				"status": 500,
			},
		},
	}
	postWireMockMapping(stub)
}

func stubPMDeleteResourceFailure() {
	stub := map[string]any{
		"request": map[string]any{
			"method":         "DELETE",
			"urlPathPattern": "/api/v1alpha1/runs/.*",
		},
		"response": map[string]any{
			"status": 500,
			"headers": map[string]string{
				"Content-Type": "application/json",
			},
			"jsonBody": map[string]any{
				"type":   string(v1alpha1.INTERNAL),
				"title":  "Internal Server Error",
				"status": 500,
			},
		},
	}
	postWireMockMapping(stub)
}

func verifyPMCreateResourceCalled(expectedCount int) {
	verifyWireMockRequestCountByURLPath("POST", "/api/v1alpha1/runs", expectedCount)
}

func verifyPMDeleteResourceCalled(expectedCount int) {
	verifyWireMockRequestCount("DELETE", "/api/v1alpha1/runs/.*", expectedCount)
}

func verifyPMRehydrateResourceCalled(expectedCount int) {
	verifyWireMockRequestCount("POST", "/api/v1alpha1/runs/.*:rehydrate", expectedCount)
}

func verifyWireMockRequestCount(method, urlPattern string, expectedCount int) {
	verifyWireMockRequestCountMatcher(map[string]any{
		"method":     method,
		"urlPattern": urlPattern + ".*",
	}, expectedCount)
}

func verifyWireMockRequestCountByURLPath(method, urlPath string, expectedCount int) {
	verifyWireMockRequestCountMatcher(map[string]any{
		"method":  method,
		"urlPath": urlPath,
	}, expectedCount)
}

func verifyWireMockRequestCountMatcher(matcher map[string]any, expectedCount int) {
	data, err := json.Marshal(matcher)
	ExpectWithOffset(2, err).NotTo(HaveOccurred())

	req, err := http.NewRequest(http.MethodPost, wireMockURL+"/__admin/requests/count", bytes.NewReader(data))
	ExpectWithOffset(2, err).NotTo(HaveOccurred())
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	ExpectWithOffset(2, err).NotTo(HaveOccurred())
	defer resp.Body.Close()

	var result map[string]any
	ExpectWithOffset(2, json.NewDecoder(resp.Body).Decode(&result)).To(Succeed())
	ExpectWithOffset(2, int(result["count"].(float64))).To(Equal(expectedCount))
}

func postWireMockMapping(stub map[string]any) {
	data, err := json.Marshal(stub)
	ExpectWithOffset(2, err).NotTo(HaveOccurred())

	req, err := http.NewRequest(http.MethodPost, wireMockURL+"/__admin/mappings", bytes.NewReader(data))
	ExpectWithOffset(2, err).NotTo(HaveOccurred())
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	ExpectWithOffset(2, err).NotTo(HaveOccurred())
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	ExpectWithOffset(2, resp.StatusCode).To(Equal(http.StatusCreated), fmt.Sprintf("WireMock stub creation failed: %s", string(body)))
}

// --- Test helpers ---

func stringPtr(s string) *string {
	return &s
}

func boolPtr(b bool) *bool {
	return &b
}

func defaultField() v1alpha1.FieldConfiguration {
	return v1alpha1.FieldConfiguration{
		Path:        "vcpu.count",
		DisplayName: stringPtr("vCPU Count"),
		Editable:    boolPtr(true),
		Default:     float64(2),
		ValidationSchema: &map[string]any{
			"type":    "number",
			"minimum": float64(1),
			"maximum": float64(16),
		},
	}
}

func createTestCatalogItem(id, displayName, serviceType string, fields []v1alpha1.FieldConfiguration) *v1alpha1.CatalogItem {
	if len(fields) == 0 {
		fields = []v1alpha1.FieldConfiguration{defaultField()}
	}
	params := &v1alpha1.CreateCatalogItemParams{Id: &id}
	body := v1alpha1.CatalogItem{
		ApiVersion:  stringPtr("v1alpha1"),
		DisplayName: &displayName,
		Spec:        testutil.PtrCatalogSpec(serviceType, fields),
	}
	resp, err := apiClient.CreateCatalogItemWithResponse(context.Background(), params, body)
	ExpectWithOffset(1, err).NotTo(HaveOccurred())
	ExpectWithOffset(1, resp.StatusCode()).To(Equal(http.StatusCreated), fmt.Sprintf("create catalog item failed: %s", string(resp.Body)))
	return resp.JSON201
}
