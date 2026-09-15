package app

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"

	"github.com/dcm-project/control-plane/internal/auth"
	"github.com/go-chi/chi/v5"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type stubChecker struct {
	name string
	err  error
}

func (s stubChecker) Name() string { return s.name }

func (s stubChecker) Check(context.Context) error { return s.err }

var _ = Describe("Monolith health", func() {
	var router chi.Router

	serveHealth := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, auth.MonolithHealthPath, nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}

	It("returns ok when postgres is reachable and nats is disabled", func() {
		router = chi.NewRouter()
		registerMonolithHealth(router, stubChecker{name: "database"})

		rec := serveHealth()
		Expect(rec.Code).To(Equal(http.StatusOK))

		var body healthResponse
		Expect(json.NewDecoder(rec.Body).Decode(&body)).To(Succeed())
		Expect(body.Status).To(Equal("ok"))
		Expect(body.Path).To(Equal(auth.MonolithHealthPath))
		Expect(body.Checks).To(Equal(map[string]string{"database": "ok"}))
	})

	It("returns ok when postgres and nats are reachable", func() {
		router = chi.NewRouter()
		registerMonolithHealth(router,
			stubChecker{name: "database"},
			stubChecker{name: "nats"},
		)

		rec := serveHealth()
		Expect(rec.Code).To(Equal(http.StatusOK))

		var body healthResponse
		Expect(json.NewDecoder(rec.Body).Decode(&body)).To(Succeed())
		Expect(body.Status).To(Equal("ok"))
		Expect(body.Checks).To(Equal(map[string]string{
			"database": "ok",
			"nats":     "ok",
		}))
	})

	It("returns 503 when postgres is unreachable", func() {
		router = chi.NewRouter()
		registerMonolithHealth(router,
			stubChecker{name: "database", err: errors.New("db down")},
			stubChecker{name: "nats"},
		)

		rec := serveHealth()
		Expect(rec.Code).To(Equal(http.StatusServiceUnavailable))

		var body healthResponse
		Expect(json.NewDecoder(rec.Body).Decode(&body)).To(Succeed())
		Expect(body.Status).To(Equal("unhealthy"))
		Expect(body.Checks).To(Equal(map[string]string{
			"database": "unavailable",
			"nats":     "ok",
		}))
	})

	It("returns 503 when nats is unreachable", func() {
		router = chi.NewRouter()
		registerMonolithHealth(router,
			stubChecker{name: "database"},
			stubChecker{name: "nats", err: errors.New("nats down")},
		)

		rec := serveHealth()
		Expect(rec.Code).To(Equal(http.StatusServiceUnavailable))

		var body healthResponse
		Expect(json.NewDecoder(rec.Body).Decode(&body)).To(Succeed())
		Expect(body.Status).To(Equal("unhealthy"))
		Expect(body.Checks).To(Equal(map[string]string{
			"database": "ok",
			"nats":     "unavailable",
		}))
	})

	It("returns 503 when database and nats are both unreachable", func() {
		router = chi.NewRouter()
		registerMonolithHealth(router,
			stubChecker{name: "database", err: errors.New("db down")},
			stubChecker{name: "nats", err: errors.New("nats down")},
		)

		rec := serveHealth()
		Expect(rec.Code).To(Equal(http.StatusServiceUnavailable))

		var body healthResponse
		Expect(json.NewDecoder(rec.Body).Decode(&body)).To(Succeed())
		Expect(body.Status).To(Equal("unhealthy"))
		Expect(body.Checks).To(Equal(map[string]string{
			"database": "unavailable",
			"nats":     "unavailable",
		}))
	})

	It("returns 503 when the request context is already cancelled", func() {
		router = chi.NewRouter()
		registerMonolithHealth(router,
			stubChecker{name: "database"},
			stubChecker{name: "nats"},
		)

		req := httptest.NewRequest(http.MethodGet, auth.MonolithHealthPath, nil)
		ctx, cancel := context.WithCancel(req.Context())
		cancel()
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		Expect(rec.Code).To(Equal(http.StatusServiceUnavailable))

		var body healthResponse
		Expect(json.NewDecoder(rec.Body).Decode(&body)).To(Succeed())
		Expect(body.Status).To(Equal("unhealthy"))
		Expect(body.Checks).To(Equal(map[string]string{
			"database": "unavailable",
			"nats":     "unavailable",
		}))
	})

	It("returns 503 when no checkers are configured", func() {
		router = chi.NewRouter()
		registerMonolithHealth(router)

		rec := serveHealth()
		Expect(rec.Code).To(Equal(http.StatusServiceUnavailable))

		var body healthResponse
		Expect(json.NewDecoder(rec.Body).Decode(&body)).To(Succeed())
		Expect(body.Status).To(Equal("unhealthy"))
	})
})

var _ = Describe("PostgresChecker", func() {
	It("reports database as its name", func() {
		Expect(NewPostgresChecker(nil).Name()).To(Equal("database"))
	})

	It("pings through a gorm DB", func() {
		gdb, err := gorm.Open(sqlite.Open("file::memory:"), &gorm.Config{
			Logger: logger.Default.LogMode(logger.Silent),
		})
		Expect(err).NotTo(HaveOccurred())

		c := NewPostgresChecker(gdb)
		Expect(c.Check(context.Background())).To(Succeed())
	})

	It("returns an error when the database is not configured", func() {
		c := NewPostgresChecker(nil)
		Expect(c.Check(context.Background())).To(MatchError("database not configured"))
	})
})

var _ = Describe("NATSChecker", func() {
	It("reports nats as its name", func() {
		Expect(NewNATSChecker(stubChecker{name: "ignored"}).Name()).To(Equal("nats"))
	})

	It("delegates Check to the wrapped pinger", func() {
		c := NewNATSChecker(stubChecker{err: errors.New("nats down")})
		Expect(c.Check(context.Background())).To(MatchError("nats down"))
	})
})
