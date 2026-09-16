package v1alpha1

import (
	"bytes"
	"context"
	"log/slog"
	"strings"

	server "github.com/dcm-project/control-plane/internal/agent/api/server"
	"github.com/dcm-project/control-plane/internal/agent/service"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("error response mappers", func() {
	Describe("createErrorResponse", func() {
		It("maps a validation error to 400", func() {
			resp, err := createErrorResponse(service.NewValidationError("bad name"))
			Expect(err).NotTo(HaveOccurred())
			typed, ok := resp.(server.CreateAgent400ApplicationProblemPlusJSONResponse)
			Expect(ok).To(BeTrue())
			Expect(typed.Type).To(Equal("validation-error"))
		})

		It("maps a conflict error to a typed 409, not the generic default", func() {
			resp, err := createErrorResponse(service.NewConflictError("already registered"))
			Expect(err).NotTo(HaveOccurred())
			typed, ok := resp.(server.CreateAgent409ApplicationProblemPlusJSONResponse)
			Expect(ok).To(BeTrue())
			Expect(typed.Type).To(Equal("conflict"))
		})

		It("still maps unrecognized errors to the generic 500 default", func() {
			resp, err := createErrorResponse(service.NewNotImplementedError())
			Expect(err).NotTo(HaveOccurred())
			def, ok := resp.(server.CreateAgentdefaultApplicationProblemPlusJSONResponse)
			Expect(ok).To(BeTrue())
			Expect(def.StatusCode).To(Equal(500))
			Expect(def.Body.Detail).To(HaveValue(Equal(internalErrorDetail)))
		})
	})

	Describe("getErrorResponse", func() {
		It("maps a not-found error to 404", func() {
			resp, err := getErrorResponse(service.NewNotFoundError("agent not found"))
			Expect(err).NotTo(HaveOccurred())
			_, ok := resp.(server.GetAgent404ApplicationProblemPlusJSONResponse)
			Expect(ok).To(BeTrue())
		})

		It("maps an unrecognized error to the generic 500 default", func() {
			resp, err := getErrorResponse(service.NewValidationError("shouldn't happen here"))
			Expect(err).NotTo(HaveOccurred())
			def, ok := resp.(server.GetAgentdefaultApplicationProblemPlusJSONResponse)
			Expect(ok).To(BeTrue())
			Expect(def.StatusCode).To(Equal(500))
		})
	})

	Describe("hbErrorResponse", func() {
		It("maps a not-found error to 404", func() {
			resp, err := hbErrorResponse(service.NewNotFoundError("agent not found"))
			Expect(err).NotTo(HaveOccurred())
			_, ok := resp.(server.AgentHeartbeat404ApplicationProblemPlusJSONResponse)
			Expect(ok).To(BeTrue())
		})
	})

	Describe("listErrorResponse", func() {
		It("maps a validation error to 400", func() {
			resp, err := listErrorResponse(service.NewValidationError("bad page token"))
			Expect(err).NotTo(HaveOccurred())
			_, ok := resp.(server.ListAgents400ApplicationProblemPlusJSONResponse)
			Expect(ok).To(BeTrue())
		})
	})

	Describe("logServiceError", func() {
		// Captures level via a text handler rather than asserting on
		// ServiceError type directly, so this exercises the same
		// service.IsClientError path the mappers above rely on.
		logLevel := func(err error) string {
			var buf bytes.Buffer
			prev := slog.Default()
			slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
			defer slog.SetDefault(prev)

			logServiceError(context.Background(), "op failed", err)
			out := buf.String()
			switch {
			case strings.Contains(out, "level=WARN"):
				return "WARN"
			case strings.Contains(out, "level=ERROR"):
				return "ERROR"
			default:
				return "UNKNOWN"
			}
		}

		It("logs a mapped 4xx ServiceError at Warn", func() {
			Expect(logLevel(service.NewConflictError("already registered"))).To(Equal("WARN"))
		})

		It("logs a ServiceError with no 4xx mapping (e.g. not-implemented) at Error, since it's a hidden failure too", func() {
			Expect(logLevel(service.NewNotImplementedError())).To(Equal("ERROR"))
		})

		It("logs a raw, non-ServiceError failure at Error", func() {
			Expect(logLevel(context.DeadlineExceeded)).To(Equal("ERROR"))
		})
	})
})
