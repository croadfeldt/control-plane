package placement_test

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/dcm-project/control-plane/internal/catalog/placement"
)

func newTestClient(serverURL string) placement.Client {
	client, err := placement.NewClient(
		serverURL,
		10*time.Second,
		slog.Default(),
	)
	Expect(err).ToNot(HaveOccurred())
	return client
}

var _ = Describe("Placement Client", func() {
	var (
		ctx    context.Context
		server *httptest.Server
		client placement.Client
	)

	BeforeEach(func() {
		ctx = context.Background()
	})

	AfterEach(func() {
		if server != nil {
			server.Close()
		}
	})

	Describe("CreateRun", func() {
		Context("when the server returns success", func() {
			BeforeEach(func() {
				server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					Expect(r.Method).To(Equal(http.MethodPost))
					Expect(r.URL.Path).To(Equal("/api/v1alpha1/runs"))

					var body map[string]any
					err := json.NewDecoder(r.Body).Decode(&body)
					Expect(err).ToNot(HaveOccurred())
					Expect(body["catalog_item_instance_id"]).To(Equal("instance-123"))

					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusAccepted)
					_ = json.NewEncoder(w).Encode(map[string]any{
						"run_id":                   "pm-run-id",
						"catalog_item_instance_id": "instance-123",
						"resources": []map[string]any{
							{
								"id":   "pm-resource-id",
								"name": "main",
								"path": "resources/pm-resource-id",
								"spec": map[string]any{"vcpu": map[string]any{"count": float64(4)}},
							},
						},
					})
				}))
				client = newTestClient(server.URL)
			})

			It("returns the created run", func() {
				resourceID := "my-resource"
				run, err := client.CreateRun(ctx, placement.CreateRunRequest{
					CatalogItemInstanceId: "instance-123",
					RunId:                 "pm-run-id",
					Resources: []placement.ResourceInput{
						{
							ID:   &resourceID,
							Name: "main",
							Spec: map[string]any{"vcpu": map[string]any{"count": float64(4)}},
						},
					},
				})

				Expect(err).ToNot(HaveOccurred())
				Expect(run.RunID).To(Equal("pm-run-id"))
				Expect(run.Resources).To(HaveLen(1))
				Expect(run.Resources[0].ID).To(Equal("pm-resource-id"))
				Expect(run.Resources[0].Path).To(Equal("resources/pm-resource-id"))
			})
		})

		Context("when the server returns an error", func() {
			BeforeEach(func() {
				server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusInternalServerError)
					_, _ = w.Write([]byte(`{"title": "internal error", "type": "internal"}`))
				}))
				client = newTestClient(server.URL)
			})

			It("returns a PlacementError", func() {
				run, err := client.CreateRun(ctx, placement.CreateRunRequest{
					CatalogItemInstanceId: "instance-123",
					RunId:                 "run-instance-123",
					Resources: []placement.ResourceInput{
						{Name: "main", Spec: map[string]any{}},
					},
				})

				Expect(run).To(BeNil())
				var pmErr *placement.PlacementError
				Expect(errors.As(err, &pmErr)).To(BeTrue())
				Expect(pmErr.StatusCode).To(Equal(http.StatusInternalServerError))
			})
		})

		Context("when the server returns policy rejected", func() {
			BeforeEach(func() {
				server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					w.WriteHeader(http.StatusNotAcceptable)
					_, _ = w.Write([]byte(`policy rejected`))
				}))
				client = newTestClient(server.URL)
			})

			It("returns PlacementError with 406", func() {
				_, err := client.CreateRun(ctx, placement.CreateRunRequest{
					CatalogItemInstanceId: "instance-123",
					RunId:                 "run-instance-123",
					Resources: []placement.ResourceInput{
						{Name: "main", Spec: map[string]any{}},
					},
				})
				var pmErr *placement.PlacementError
				Expect(errors.As(err, &pmErr)).To(BeTrue())
				Expect(pmErr.StatusCode).To(Equal(http.StatusNotAcceptable))
			})
		})

		Context("when the server returns 422 provider error", func() {
			BeforeEach(func() {
				server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					w.WriteHeader(http.StatusUnprocessableEntity)
					_, _ = w.Write([]byte(`provider error`))
				}))
				client = newTestClient(server.URL)
			})

			It("returns PlacementError with 422", func() {
				_, err := client.CreateRun(ctx, placement.CreateRunRequest{
					CatalogItemInstanceId: "instance-123",
					RunId:                 "run-instance-123",
					Resources: []placement.ResourceInput{
						{Name: "main", Spec: map[string]any{}},
					},
				})
				var pmErr *placement.PlacementError
				Expect(errors.As(err, &pmErr)).To(BeTrue())
				Expect(pmErr.StatusCode).To(Equal(http.StatusUnprocessableEntity))
			})
		})
	})

	Describe("DeleteRun", func() {
		Context("when the server returns success", func() {
			BeforeEach(func() {
				server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					Expect(r.Method).To(Equal(http.MethodDelete))
					Expect(r.URL.Path).To(Equal("/api/v1alpha1/runs/pm-run-id"))
					w.WriteHeader(http.StatusNoContent)
				}))
				client = newTestClient(server.URL)
			})

			It("returns nil", func() {
				err := client.DeleteRun(ctx, "pm-run-id")
				Expect(err).ToNot(HaveOccurred())
			})
		})

		Context("when the server returns not found", func() {
			BeforeEach(func() {
				server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					w.WriteHeader(http.StatusNotFound)
					_, _ = w.Write([]byte(`not found`))
				}))
				client = newTestClient(server.URL)
			})

			It("returns an error", func() {
				err := client.DeleteRun(ctx, "nonexistent")
				Expect(err).To(HaveOccurred())
			})
		})

		Context("when the server returns an error", func() {
			BeforeEach(func() {
				server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					w.WriteHeader(http.StatusInternalServerError)
					_, _ = w.Write([]byte(`internal`))
				}))
				client = newTestClient(server.URL)
			})

			It("returns an error", func() {
				err := client.DeleteRun(ctx, "some-id")
				Expect(err).To(HaveOccurred())
			})
		})
	})
})
