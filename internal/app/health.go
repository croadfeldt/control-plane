package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/dcm-project/control-plane/internal/auth"
	"github.com/go-chi/chi/v5"
	"gorm.io/gorm"
)

const (
	healthCheckTimeout = 2 * time.Second
	// checkerTimeout caps each dependency check so one slow check cannot
	// consume the full overall health budget.
	checkerTimeout = 1 * time.Second
)

type healthResponse struct {
	Status string            `json:"status"`
	Path   string            `json:"path"`
	Checks map[string]string `json:"checks,omitempty"`
}

// Checker verifies a dependency used by the monolith health endpoint.
type Checker interface {
	Name() string
	Check(ctx context.Context) error
}

type postgresChecker struct {
	db *gorm.DB
}

// NewPostgresChecker returns a Checker that pings the database via GORM's SQL pool.
func NewPostgresChecker(db *gorm.DB) Checker {
	return postgresChecker{db: db}
}

func (c postgresChecker) Name() string { return "database" }

func (c postgresChecker) Check(ctx context.Context) error {
	if c.db == nil {
		return fmt.Errorf("database not configured")
	}
	sqlDB, err := c.db.DB()
	if err != nil {
		return err
	}
	return sqlDB.PingContext(ctx)
}

type natsPinger interface {
	Check(ctx context.Context) error
}

type natsChecker struct {
	c natsPinger
}

// NewNATSChecker returns a Checker that verifies NATS connectivity.
func NewNATSChecker(c natsPinger) Checker {
	return natsChecker{c: c}
}

func (c natsChecker) Name() string { return "nats" }

func (c natsChecker) Check(ctx context.Context) error {
	return c.c.Check(ctx)
}

func registerMonolithHealth(router chi.Router, checkers ...Checker) {
	router.Get(auth.MonolithHealthPath, func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), healthCheckTimeout)
		defer cancel()

		checks := map[string]string{}
		healthy := len(checkers) > 0

		for _, checker := range checkers {
			if ctx.Err() != nil {
				checks[checker.Name()] = "unavailable"
				healthy = false
				continue
			}

			checkCtx, checkCancel := context.WithTimeout(ctx, checkerTimeout)
			err := checker.Check(checkCtx)
			checkCancel()
			if err != nil {
				checks[checker.Name()] = "unavailable"
				healthy = false
				continue
			}
			checks[checker.Name()] = "ok"
		}

		status := "ok"
		code := http.StatusOK
		if !healthy {
			status = "unhealthy"
			code = http.StatusServiceUnavailable
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		_ = json.NewEncoder(w).Encode(healthResponse{
			Status: status,
			Path:   auth.MonolithHealthPath,
			Checks: checks,
		})
	})
}
