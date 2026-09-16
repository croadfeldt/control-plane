package v1alpha1_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"

	api "github.com/dcm-project/control-plane/api/agent/v1alpha1"
	server "github.com/dcm-project/control-plane/internal/agent/api/server"
	handler "github.com/dcm-project/control-plane/internal/agent/handlers/v1alpha1"
	"github.com/dcm-project/control-plane/internal/agent/service"
	"github.com/dcm-project/control-plane/internal/agent/store/model"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	agentstore "github.com/dcm-project/control-plane/internal/agent/store/agent"
)

var _ = Describe("Agent Handler", func() {
	var (
		db     *gorm.DB
		router *chi.Mux
	)

	BeforeEach(func() {
		var err error
		db, err = gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
			Logger: logger.Default.LogMode(logger.Silent),
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(db.AutoMigrate(&model.Agent{})).To(Succeed())

		store := agentstore.NewAgent(db)
		svc := service.NewAgentService(store, 100)
		h := handler.NewHandler(svc)

		strictHandler := server.NewStrictHandler(h, nil)
		router = chi.NewRouter()
		server.HandlerFromMux(strictHandler, router)
	})

	AfterEach(func() {
		sqlDB, _ := db.DB()
		_ = sqlDB.Close()
	})

	Describe("POST /agents", func() {
		It("returns 201 on create", func() {
			body := `{"name":"test-agent","topic_name":"dcm.agent.test-agent","service_types":["vm"]}`
			req := httptest.NewRequest(http.MethodPost, "/agents", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()

			router.ServeHTTP(rec, req)

			Expect(rec.Code).To(Equal(http.StatusCreated))

			var agent api.Agent
			Expect(json.NewDecoder(rec.Body).Decode(&agent)).To(Succeed())
			Expect(agent.AgentId).NotTo(BeNil())
		})

		It("returns 200 on re-registration", func() {
			body := `{"name":"rereg-agent","topic_name":"dcm.agent.rereg-agent","service_types":["vm"]}`

			req1 := httptest.NewRequest(http.MethodPost, "/agents", strings.NewReader(body))
			req1.Header.Set("Content-Type", "application/json")
			rec1 := httptest.NewRecorder()
			router.ServeHTTP(rec1, req1)
			Expect(rec1.Code).To(Equal(http.StatusCreated))

			req2 := httptest.NewRequest(http.MethodPost, "/agents", strings.NewReader(body))
			req2.Header.Set("Content-Type", "application/json")
			rec2 := httptest.NewRecorder()
			router.ServeHTTP(rec2, req2)

			Expect(rec2.Code).To(Equal(http.StatusOK))
		})

		It("returns 400 on invalid body", func() {
			req := httptest.NewRequest(http.MethodPost, "/agents", strings.NewReader(`{invalid`))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()

			router.ServeHTTP(rec, req)

			Expect(rec.Code).To(Equal(http.StatusBadRequest))
		})

		It("returns 409 with an RFC 7807 body when another agent already owns the topic_name (K)", func() {
			body1 := `{"name":"topic-owner","topic_name":"dcm.agent.shared-topic","service_types":["vm"]}`
			req1 := httptest.NewRequest(http.MethodPost, "/agents", strings.NewReader(body1))
			req1.Header.Set("Content-Type", "application/json")
			rec1 := httptest.NewRecorder()
			router.ServeHTTP(rec1, req1)
			Expect(rec1.Code).To(Equal(http.StatusCreated))

			body2 := `{"name":"topic-squatter","topic_name":"dcm.agent.shared-topic","service_types":["vm"]}`
			req2 := httptest.NewRequest(http.MethodPost, "/agents", strings.NewReader(body2))
			req2.Header.Set("Content-Type", "application/json")
			rec2 := httptest.NewRecorder()
			router.ServeHTTP(rec2, req2)

			Expect(rec2.Code).To(Equal(http.StatusConflict))
			Expect(rec2.Header().Get("Content-Type")).To(Equal("application/problem+json"))

			var problem server.Error
			Expect(json.NewDecoder(rec2.Body).Decode(&problem)).To(Succeed())
			Expect(problem.Status).NotTo(BeNil())
			Expect(*problem.Status).To(Equal(409))
		})
	})

	Describe("GET /agents/{agentId}", func() {
		It("returns 200 on found", func() {
			body := `{"name":"get-agent","topic_name":"dcm.agent.get-agent","service_types":["vm"]}`
			reqCreate := httptest.NewRequest(http.MethodPost, "/agents", strings.NewReader(body))
			reqCreate.Header.Set("Content-Type", "application/json")
			recCreate := httptest.NewRecorder()
			router.ServeHTTP(recCreate, reqCreate)
			Expect(recCreate.Code).To(Equal(http.StatusCreated))

			var created api.Agent
			Expect(json.NewDecoder(recCreate.Body).Decode(&created)).To(Succeed())

			reqGet := httptest.NewRequest(http.MethodGet, "/agents/"+*created.AgentId, nil)
			recGet := httptest.NewRecorder()
			router.ServeHTTP(recGet, reqGet)

			Expect(recGet.Code).To(Equal(http.StatusOK))
		})

		It("returns 404 on not found", func() {
			req := httptest.NewRequest(http.MethodGet, "/agents/"+uuid.New().String(), nil)
			rec := httptest.NewRecorder()

			router.ServeHTTP(rec, req)

			Expect(rec.Code).To(Equal(http.StatusNotFound))
		})
	})

	Describe("GET /agents", func() {
		It("returns 200 with list", func() {
			req := httptest.NewRequest(http.MethodGet, "/agents", nil)
			rec := httptest.NewRecorder()

			router.ServeHTTP(rec, req)

			Expect(rec.Code).To(Equal(http.StatusOK))
		})

		It("returns an RFC 7807 problem body on internal failure instead of a raw error", func() {
			// Force the store call inside List to fail with a plain (non-
			// ServiceError) error, exercising the generic 500 fallback.
			sqlDB, err := db.DB()
			Expect(err).NotTo(HaveOccurred())
			Expect(sqlDB.Close()).To(Succeed())

			req := httptest.NewRequest(http.MethodGet, "/agents", nil)
			rec := httptest.NewRecorder()

			router.ServeHTTP(rec, req)

			Expect(rec.Code).To(Equal(http.StatusInternalServerError))
			Expect(rec.Header().Get("Content-Type")).To(Equal("application/problem+json"))

			var body server.Error
			Expect(json.NewDecoder(rec.Body).Decode(&body)).To(Succeed())
			Expect(body.Type).To(Equal("list-error"))
			Expect(body.Status).NotTo(BeNil())
			Expect(*body.Status).To(Equal(500))
		})
	})

	Describe("PUT /agents/{agentId}/heartbeat", func() {
		It("returns 200 on success", func() {
			body := `{"name":"hb-agent","topic_name":"dcm.agent.hb-agent","service_types":["vm"]}`
			reqCreate := httptest.NewRequest(http.MethodPost, "/agents", strings.NewReader(body))
			reqCreate.Header.Set("Content-Type", "application/json")
			recCreate := httptest.NewRecorder()
			router.ServeHTTP(recCreate, reqCreate)
			Expect(recCreate.Code).To(Equal(http.StatusCreated))

			var created api.Agent
			Expect(json.NewDecoder(recCreate.Body).Decode(&created)).To(Succeed())

			hbBody := `{"consumer_lag":0,"timestamp":"2026-07-31T10:00:00Z"}`
			reqHb := httptest.NewRequest(http.MethodPut, "/agents/"+*created.AgentId+"/heartbeat", strings.NewReader(hbBody))
			reqHb.Header.Set("Content-Type", "application/json")
			recHb := httptest.NewRecorder()
			router.ServeHTTP(recHb, reqHb)

			Expect(recHb.Code).To(Equal(http.StatusOK))
		})

		It("returns 404 on unknown agent", func() {
			hbBody := `{"consumer_lag":0,"timestamp":"2026-07-31T10:00:00Z"}`
			req := httptest.NewRequest(http.MethodPut, "/agents/"+uuid.New().String()+"/heartbeat", strings.NewReader(hbBody))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()

			router.ServeHTTP(rec, req)

			Expect(rec.Code).To(Equal(http.StatusNotFound))
		})
	})
})
