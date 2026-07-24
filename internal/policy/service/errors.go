package service

import (
	"errors"
	"fmt"
	"strings"

	"github.com/dcm-project/control-plane/api/policy/v1alpha1"
	"github.com/dcm-project/control-plane/internal/policy/store"
	"github.com/dcm-project/control-plane/internal/policy/store/model"
	"gorm.io/gorm"
)

// ErrorType represents the type of service error
type ErrorType string

const (
	ErrorTypeInvalidArgument    ErrorType = "https://dcm.example.com/errors/invalid-argument"
	ErrorTypeNotFound           ErrorType = "https://dcm.example.com/errors/not-found"
	ErrorTypeAlreadyExists      ErrorType = "https://dcm.example.com/errors/already-exists"
	ErrorTypeInternal           ErrorType = "https://dcm.example.com/errors/internal"
	ErrorTypeFailedPrecondition ErrorType = "https://dcm.example.com/errors/failed-precondition"
	ErrorTypeRejected           ErrorType = "https://dcm.example.com/errors/rejected"        // Policy evaluation rejected
	ErrorTypePolicyConflict     ErrorType = "https://dcm.example.com/errors/policy-conflict" // Policy constraint conflict
)

// ServiceError represents a structured error from the service layer
type ServiceError struct {
	Type    ErrorType
	Message string
	Detail  string
	Err     error
}

func (e *ServiceError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s: %s (caused by: %v)", e.Type, e.Message, e.Err)
	}
	return fmt.Sprintf("%s: %s", e.Type, e.Message)
}

func (e *ServiceError) Unwrap() error {
	return e.Err
}

func processPolicyStoreError(err error, dbPolicy model.Policy, operation string) *ServiceError {
	// Check for duplicate ID error first
	if errors.Is(err, store.ErrPolicyIDTaken) {
		return NewPolicyAlreadyExistsError(dbPolicy.ID)
	}
	if errors.Is(err, store.ErrDisplayNamePolicyTypeTaken) {
		return NewPolicyDisplayNamePolicyTypeTakenError(dbPolicy.DisplayName, v1alpha1.PolicyPolicyType(dbPolicy.PolicyType))
	}
	if errors.Is(err, store.ErrPriorityPolicyTypeTaken) {
		return NewPolicyPriorityPolicyTypeTakenError(dbPolicy.Priority, v1alpha1.PolicyPolicyType(dbPolicy.PolicyType))
	}
	if errors.Is(err, store.ErrPolicyNotFound) || errors.Is(err, gorm.ErrRecordNotFound) {
		return NewPolicyNotFoundError(dbPolicy.ID)
	}
	return NewInternalError(fmt.Sprintf("Failed to %s policy", operation), err.Error(), err)
}

// NewInvalidArgumentError creates a new invalid argument error
func NewInvalidArgumentError(message, detail string) *ServiceError {
	return &ServiceError{
		Type:    ErrorTypeInvalidArgument,
		Message: message,
		Detail:  detail,
	}
}

func NewPolicyNotFoundError(policyID string) *ServiceError {
	return NewNotFoundError("Policy not found", fmt.Sprintf("Policy with ID '%s' does not exist", policyID))
}

// NewNotFoundError creates a new not found error
func NewNotFoundError(message, detail string) *ServiceError {
	return &ServiceError{
		Type:    ErrorTypeNotFound,
		Message: message,
		Detail:  detail,
	}
}

func NewPolicyAlreadyExistsError(policyID string) *ServiceError {
	return NewAlreadyExistsError("Policy already exists", fmt.Sprintf("A policy with ID '%s' already exists", policyID))
}

func NewPolicyDisplayNamePolicyTypeTakenError(displayName string, policyType v1alpha1.PolicyPolicyType) *ServiceError {
	return NewAlreadyExistsError(
		"Policy display name and policy type already exists",
		fmt.Sprintf("A policy with display name '%s' and policy type '%s' already exists", displayName, string(policyType)),
	)
}

func NewPolicyPriorityPolicyTypeTakenError(priority int32, policyType v1alpha1.PolicyPolicyType) *ServiceError {
	return NewAlreadyExistsError(
		"Policy priority and policy type already exists",
		fmt.Sprintf("A policy with priority '%d' and policy type '%s' already exists", priority, string(policyType)),
	)
}

// NewAlreadyExistsError creates a new already exists error
func NewAlreadyExistsError(message, detail string) *ServiceError {
	return &ServiceError{
		Type:    ErrorTypeAlreadyExists,
		Message: message,
		Detail:  detail,
	}
}

// NewInternalError creates a new internal error
func NewInternalError(message, detail string, err error) *ServiceError {
	return &ServiceError{
		Type:    ErrorTypeInternal,
		Message: message,
		Detail:  detail,
		Err:     err,
	}
}

// NewFailedPreconditionError creates a new failed precondition error
func NewFailedPreconditionError(message, detail string) *ServiceError {
	return &ServiceError{
		Type:    ErrorTypeFailedPrecondition,
		Message: message,
		Detail:  detail,
	}
}

// NewPolicyRejectedError creates a new policy rejected error (406 Not Acceptable)
func NewPolicyRejectedError(policyID, reason string) *ServiceError {
	return &ServiceError{
		Type:    ErrorTypeRejected,
		Message: fmt.Sprintf("Request rejected by policy '%s'", policyID),
		Detail:  reason,
	}
}

// NewPolicyConflictError creates a new policy conflict error (409 Conflict)
func NewPolicyConflictError(lowerPolicyID, field, higherPolicyID string) *ServiceError {
	return &ServiceError{
		Type:    ErrorTypePolicyConflict,
		Message: fmt.Sprintf("Policy '%s' attempted to modify field '%s' which was set by higher-priority policy '%s'", lowerPolicyID, field, higherPolicyID),
		Detail:  fmt.Sprintf("Field '%s' is immutable after being set by policy '%s'", field, higherPolicyID),
	}
}

// NewConstraintViolationError creates a new constraint violation error (409 Conflict)
// This is used when a patch value violates accumulated constraints from higher-priority policies
func NewConstraintViolationError(policyID string, violations []ConstraintViolation) *ServiceError {
	parts := make([]string, len(violations))
	for i, v := range violations {
		parts[i] = fmt.Sprintf("field '%s': %s (constrained by policy '%s')", v.FieldPath, v.Reason, v.SetByPolicy)
	}
	detail := fmt.Sprintf("Constraint violations: %s", strings.Join(parts, "; "))
	return &ServiceError{
		Type:    ErrorTypePolicyConflict,
		Message: fmt.Sprintf("Policy '%s' produced values that violate constraints set by higher-priority policies", policyID),
		Detail:  detail,
	}
}

// NewConstraintConflictError creates a new constraint conflict error (409 Conflict)
// This is used when a lower-priority policy tries to loosen constraints set by a higher-priority policy
func NewConstraintConflictError(policyID, fieldPath, existingPolicyID, reason string) *ServiceError {
	return &ServiceError{
		Type:    ErrorTypePolicyConflict,
		Message: fmt.Sprintf("Policy '%s' attempted to loosen constraint on field '%s' set by higher-priority policy '%s'", policyID, fieldPath, existingPolicyID),
		Detail:  reason,
	}
}

// NewServiceProviderConstraintError creates a new SP constraint error (409 Conflict)
func NewServiceProviderConstraintError(policyID, detail string) *ServiceError {
	return &ServiceError{
		Type:    ErrorTypePolicyConflict,
		Message: fmt.Sprintf("Policy '%s' selected a service provider that violates constraints", policyID),
		Detail:  detail,
	}
}

// ConstraintViolation represents a single constraint violation
type ConstraintViolation struct {
	FieldPath   string
	Reason      string
	SetByPolicy string
}
