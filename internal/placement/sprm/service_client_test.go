package sprm

import (
	"errors"
	"net/http"
	"testing"

	spservice "github.com/dcm-project/control-plane/internal/sp/service"
)

// TestMapInstanceError checks both the status code mapping per
// service.ErrorCode and that 5xx bodies never leak the internal error message.
func TestMapInstanceError(t *testing.T) {
	cases := []struct {
		name           string
		err            error
		wantStatusCode int
		wantBodyExact  string // "" means "don't check exact body"
		wantBodyLeaks  bool
	}{
		{
			name:           "validation error maps to 400 and passes message through",
			err:            spservice.NewValidationError("spec.service_type is required"),
			wantStatusCode: http.StatusBadRequest,
			wantBodyExact:  "spec.service_type is required",
		},
		{
			name:           "not found error maps to 404 and passes message through",
			err:            spservice.NewNotFoundError("instance abc not found"),
			wantStatusCode: http.StatusNotFound,
			wantBodyExact:  "instance abc not found",
		},
		{
			name:           "conflict error maps to 409 and passes message through",
			err:            spservice.NewConflictError("instance abc is being deleted"),
			wantStatusCode: http.StatusConflict,
			wantBodyExact:  "instance abc is being deleted",
		},
		{
			name:           "provisioning error maps to 422 and passes message through",
			err:            spservice.NewProvisioningError("failed to publish create request"),
			wantStatusCode: http.StatusUnprocessableEntity,
			wantBodyExact:  "failed to publish create request",
		},
		{
			name:           "unavailable error maps to 503 with a sanitized body",
			err:            spservice.NewUnavailableError("nats publisher unavailable: dial tcp 10.0.0.1:4222: connect: connection refused"),
			wantStatusCode: http.StatusServiceUnavailable,
			wantBodyExact:  "service temporarily unavailable",
		},
		{
			name:           "internal error maps to 500 with a sanitized body, not the raw DB/NATS error",
			err:            spservice.NewInternalError("failed to retrieve instance: pq: connection reset by peer"),
			wantStatusCode: http.StatusInternalServerError,
			wantBodyExact:  "internal server error",
		},
		{
			name:           "a non-ServiceError is wrapped rather than mapped to an HTTPError",
			err:            errors.New("some unrelated infra error"),
			wantStatusCode: 0, // sentinel: expect no *HTTPError at all
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := mapInstanceError(tc.err)

			var httpErr *HTTPError
			ok := errors.As(got, &httpErr)

			if tc.wantStatusCode == 0 {
				if ok {
					t.Fatalf("expected a non-HTTPError wrapped error, got *HTTPError{%d, %q}", httpErr.StatusCode, httpErr.Body)
				}
				return
			}

			if !ok {
				t.Fatalf("expected *HTTPError, got %T: %v", got, got)
			}
			if httpErr.StatusCode != tc.wantStatusCode {
				t.Errorf("StatusCode = %d, want %d", httpErr.StatusCode, tc.wantStatusCode)
			}
			if tc.wantBodyExact != "" && httpErr.Body != tc.wantBodyExact {
				t.Errorf("Body = %q, want %q", httpErr.Body, tc.wantBodyExact)
			}
		})
	}
}
