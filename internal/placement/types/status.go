package types

// Resource lifecycle statuses stored on placement resources.
const (
	ResourceStatusPending         = "PENDING"
	ResourceStatusRunning         = "RUNNING"
	ResourceStatusDeleting        = "DELETING"
	ResourceStatusDeleted         = "DELETED"
	ResourceStatusFailed          = "FAILED"
	ResourceStatusPendingDeletion = "PENDING_DELETION"
)
