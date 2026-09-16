package service

import (
	"errors"
	"fmt"
)

type ErrorCode string

// Error codes are RFC 7807 "type" URIs, matching the convention used by
// internal/sp/service/errors.go, so the agent and SP domains don't diverge
// on error identifier format.
const (
	ErrCodeNotFound       ErrorCode = "https://dcm.example.com/errors/not-found"
	ErrCodeValidation     ErrorCode = "https://dcm.example.com/errors/validation"
	ErrCodeConflict       ErrorCode = "https://dcm.example.com/errors/conflict"
	ErrCodeNotImplemented ErrorCode = "https://dcm.example.com/errors/not-implemented"
)

type ServiceError struct {
	Code    ErrorCode
	Message string
}

func (e *ServiceError) Error() string {
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

func NewNotFoundError(msg string) *ServiceError {
	return &ServiceError{Code: ErrCodeNotFound, Message: msg}
}

func NewValidationError(msg string) *ServiceError {
	return &ServiceError{Code: ErrCodeValidation, Message: msg}
}

func NewConflictError(msg string) *ServiceError {
	return &ServiceError{Code: ErrCodeConflict, Message: msg}
}

func NewNotImplementedError() *ServiceError {
	return &ServiceError{Code: ErrCodeNotImplemented, Message: "not implemented"}
}

// IsClientError returns true if err is a ServiceError representing a
// client-side (4xx) problem, mirroring internal/sp/service.IsClientError.
// ErrCodeNotImplemented is deliberately excluded: it's a server-side gap
// (501), not something the caller did wrong. If svcErr is non-nil it is
// populated with the unwrapped error.
func IsClientError(err error, svcErr **ServiceError) bool {
	if !errors.As(err, svcErr) {
		return false
	}
	switch (*svcErr).Code {
	case ErrCodeValidation, ErrCodeNotFound, ErrCodeConflict:
		return true
	}
	return false
}
