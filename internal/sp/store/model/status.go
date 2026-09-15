package model

// Status values for ServiceTypeInstance.Status: the canonical lowercase
// lifecycle states written by the agent-based provisioning flow (create
// service, sweep, response consumer). Kept as constants so the states used
// across packages can't drift in casing.
//
// The legacy provider StatusConsumer (internal/sp/consumer/consumer.go)
// receives status strings from external CloudEvent producers it does not
// control; it normalizes incoming values to this lowercase convention at
// ingestion rather than trusting upstream casing.
const (
	StatusPending         = "pending"
	StatusQueued          = "queued"
	StatusProvisioning    = "provisioning"
	StatusRunning         = "running"
	StatusDeleting        = "deleting"
	StatusCancelled       = "cancelled"
	StatusFailed          = "failed"
	StatusPendingDeletion = "pending_deletion"
)
