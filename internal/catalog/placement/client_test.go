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

	Describe("CreateResource", func() {
		Context("when the server returns success", func() {
			BeforeEach(func() {
				server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					Expect(r.Method).To(Equal(http.MethodPost))
					Expect(r.URL.Path).To(Equal("/api/v1alpha1/resources"))
					Expect(r.URL.Query().Get("id")).To(Equal("my-resource"))

					var body map[string]any
					err := json.NewDecoder(r.Body).Decode(&body)
					Expect(err).ToNot(HaveOccurred())
					Expect(body["catalog_item_instance_id"]).To(Equal("instance-123"))

					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusCreated)
					_ = json.NewEncoder(w).Encode(map[string]any{
						"id":                       "pm-resource-id",
						"path":                     "resources/pm-resource-id",
						"catalog_item_instance_id": "instance-123",
						"spec":                     map[string]any{"vcpu": map[string]any{"count": float64(4)}},
					})
				}))
				client = newTestClient(server.URL)
			})

			It("returns the created resource", func() {
				resource, err := client.CreateResource(ctx, placement.CreateResourceRequest{
					CatalogItemInstanceID: "instance-123",
					Spec:                  map[string]any{"vcpu": map[string]any{"count": float64(4)}},
				}, "my-resource")

				Expect(err).ToNot(HaveOccurred())
				Expect(resource.ID).To(Equal("pm-resource-id"))
				Expect(resource.Path).To(Equal("resources/pm-resource-id"))
			})
		})

		Context("when no ID is provided", func() {
			BeforeEach(func() {
				server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					Expect(r.URL.Path).To(Equal("/api/v1alpha1/resources"))
					Expect(r.URL.Query().Get("id")).To(BeEmpty())

					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusCreated)
					_ = json.NewEncoder(w).Encode(map[string]any{
						"id":                       "auto-generated-id",
						"path":                     "resources/auto-generated-id",
						"catalog_item_instance_id": "instance-456",
						"spec":                     map[string]any{},
					})
				}))
				client = newTestClient(server.URL)
			})

			It("sends no id query param and returns the resource", func() {
				resource, err := client.CreateResource(ctx, placement.CreateResourceRequest{
					CatalogItemInstanceID: "instance-456",
					Spec:                  map[string]any{},
				}, "")

				Expect(err).ToNot(HaveOccurred())
				Expect(resource.ID).To(Equal("auto-generated-id"))
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

			It("returns a PlacementError with StatusCode 500", func() {
				resource, err := client.CreateResource(ctx, placement.CreateResourceRequest{
					CatalogItemInstanceID: "instance-789",
					Spec:                  map[string]any{},
				}, "")

				Expect(err).To(HaveOccurred())
				Expect(resource).To(BeNil())
				var pmErr *placement.PlacementError
				Expect(errors.As(err, &pmErr)).To(BeTrue())
				Expect(pmErr.StatusCode).To(Equal(500))
			})
		})

		Context("when the server returns 406 policy rejected", func() {
			BeforeEach(func() {
				server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusNotAcceptable)
					_, _ = w.Write([]byte(`{"title": "policy rejected", "type": "https://dcm.example.com/errors/failed-precondition"}`))
				}))
				client = newTestClient(server.URL)
			})

			It("returns a PlacementError with StatusCode 406", func() {
				resource, err := client.CreateResource(ctx, placement.CreateResourceRequest{
					CatalogItemInstanceID: "instance-policy",
					Spec:                  map[string]any{},
				}, "")

				Expect(err).To(HaveOccurred())
				Expect(resource).To(BeNil())
				var pmErr *placement.PlacementError
				Expect(errors.As(err, &pmErr)).To(BeTrue())
				Expect(pmErr.StatusCode).To(Equal(406))
			})
		})

		Context("when the server returns 422 provider error", func() {
			BeforeEach(func() {
				server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusUnprocessableEntity)
					_, _ = w.Write([]byte(`{"title": "provider error", "type": "https://dcm.example.com/errors/failed-precondition"}`))
				}))
				client = newTestClient(server.URL)
			})

			It("returns a PlacementError with StatusCode 422", func() {
				resource, err := client.CreateResource(ctx, placement.CreateResourceRequest{
					CatalogItemInstanceID: "instance-provider",
					Spec:                  map[string]any{},
				}, "")

				Expect(err).To(HaveOccurred())
				Expect(resource).To(BeNil())
				var pmErr *placement.PlacementError
				Expect(errors.As(err, &pmErr)).To(BeTrue())
				Expect(pmErr.StatusCode).To(Equal(422))
			})
		})
	})

	Describe("RehydrateResource", func() {
		Context("when the server returns success", func() {
			BeforeEach(func() {
				server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					Expect(r.Method).To(Equal(http.MethodPost))
					Expect(r.URL.Path).To(Equal("/api/v1alpha1/resources/old-resource-id:rehydrate"))

					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusOK)
					_ = json.NewEncoder(w).Encode(map[string]any{
						"id":   "new-resource-id",
						"path": "resources/new-resource-id",
						"spec": map[string]any{},
					})
				}))
				client = newTestClient(server.URL)
			})

			It("returns the rehydrated resource", func() {
				resource, err := client.RehydrateResource(ctx, "old-resource-id", "new-resource-id")

				Expect(err).ToNot(HaveOccurred())
				Expect(resource).ToNot(BeNil())
				Expect(resource.ID).To(Equal("new-resource-id"))
			})
		})

		Context("when the server returns 406 policy rejected", func() {
			BeforeEach(func() {
				server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusNotAcceptable)
					_, _ = w.Write([]byte(`{"title": "policy rejected", "type": "https://dcm.example.com/errors/failed-precondition"}`))
				}))
				client = newTestClient(server.URL)
			})

			It("returns a PlacementError with StatusCode 406", func() {
				resource, err := client.RehydrateResource(ctx, "old-id", "new-id")

				Expect(err).To(HaveOccurred())
				Expect(resource).To(BeNil())
				var pmErr *placement.PlacementError
				Expect(errors.As(err, &pmErr)).To(BeTrue())
				Expect(pmErr.StatusCode).To(Equal(406))
			})
		})

		Context("when the server returns 422 provider error", func() {
			BeforeEach(func() {
				server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusUnprocessableEntity)
					_, _ = w.Write([]byte(`{"title": "provider error", "type": "https://dcm.example.com/errors/failed-precondition"}`))
				}))
				client = newTestClient(server.URL)
			})

			It("returns a PlacementError with StatusCode 422", func() {
				resource, err := client.RehydrateResource(ctx, "old-id", "new-id")

				Expect(err).To(HaveOccurred())
				Expect(resource).To(BeNil())
				var pmErr *placement.PlacementError
				Expect(errors.As(err, &pmErr)).To(BeTrue())
				Expect(pmErr.StatusCode).To(Equal(422))
			})
		})

		Context("when the server returns 500", func() {
			BeforeEach(func() {
				server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusInternalServerError)
					_, _ = w.Write([]byte(`{"title": "internal error", "type": "internal"}`))
				}))
				client = newTestClient(server.URL)
			})

			It("returns a PlacementError with StatusCode 500", func() {
				resource, err := client.RehydrateResource(ctx, "old-id", "new-id")

				Expect(err).To(HaveOccurred())
				Expect(resource).To(BeNil())
				var pmErr *placement.PlacementError
				Expect(errors.As(err, &pmErr)).To(BeTrue())
				Expect(pmErr.StatusCode).To(Equal(500))
			})
		})
	})

	Describe("DeleteResource", func() {
		Context("when the server returns success", func() {
			BeforeEach(func() {
				server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					Expect(r.Method).To(Equal(http.MethodDelete))
					Expect(r.URL.Path).To(Equal("/api/v1alpha1/resources/pm-resource-id"))
					w.WriteHeader(http.StatusNoContent)
				}))
				client = newTestClient(server.URL)
			})

			It("succeeds without error", func() {
				err := client.DeleteResource(ctx, "pm-resource-id")
				Expect(err).ToNot(HaveOccurred())
			})
		})

		Context("when the resource is not found", func() {
			BeforeEach(func() {
				server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusNotFound)
					_, _ = w.Write([]byte(`{"title": "not found", "type": "not_found"}`))
				}))
				client = newTestClient(server.URL)
			})

			It("returns an error", func() {
				err := client.DeleteResource(ctx, "nonexistent")
				Expect(err).To(HaveOccurred())
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

			It("returns an error", func() {
				err := client.DeleteResource(ctx, "some-id")
				Expect(err).To(HaveOccurred())
			})
		})
	})
})
