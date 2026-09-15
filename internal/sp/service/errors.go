// Package service provides shared types and helpers for business logic layers.
package service

import "errors"

// Error codes returned by service operations.
const (
	ErrCodeNotFound          = "https://dcm.example.com/errors/not-found"
	ErrCodeConflict          = "https://dcm.example.com/errors/conflict"
	ErrCodeValidation        = "https://dcm.example.com/errors/validation"
	ErrCodeProvisioningError = "https://dcm.example.com/errors/provisioning-error"
	ErrCodeInternal          = "https://dcm.example.com/errors/internal-error"
	ErrCodeUnavailable       = "https://dcm.example.com/errors/unavailable"
)

// ServiceError represents a business logic error with a code for HTTP mapping.
type ServiceError struct {
	Code    string
	Message string
}

func (e *ServiceError) Error() string {
	return e.Message
}

// Helper functions for creating ServiceErrors

func NewNotFoundError(message string) *ServiceError {
	return &ServiceError{
		Code:    ErrCodeNotFound,
		Message: message,
	}
}

func NewConflictError(message string) *ServiceError {
	return &ServiceError{
		Code:    ErrCodeConflict,
		Message: message,
	}
}

func NewValidationError(message string) *ServiceError {
	return &ServiceError{
		Code:    ErrCodeValidation,
		Message: message,
	}
}

func NewProvisioningError(message string) *ServiceError {
	return &ServiceError{
		Code:    ErrCodeProvisioningError,
		Message: message,
	}
}

func NewInternalError(message string) *ServiceError {
	return &ServiceError{
		Code:    ErrCodeInternal,
		Message: message,
	}
}

func NewUnavailableError(message string) *ServiceError {
	return &ServiceError{
		Code:    ErrCodeUnavailable,
		Message: message,
	}
}

// IsClientError returns true if err is a ServiceError representing a client-side
// (4xx) problem. If svcErr is non-nil it is populated with the unwrapped error.
func IsClientError(err error, svcErr **ServiceError) bool {
	if !errors.As(err, svcErr) {
		return false
	}
	switch (*svcErr).Code {
	case ErrCodeValidation, ErrCodeNotFound, ErrCodeConflict, ErrCodeProvisioningError:
		return true
	}
	return false
}
