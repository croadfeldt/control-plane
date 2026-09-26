package types

import "time"

// ResourceStatusEvent is the placement callback payload for a resource status transition.
type ResourceStatusEvent struct {
	ResourceID string
	Status     string
	OutputSpec map[string]any
	// Timestamp is the producer's event time when the status event carried one.
	Timestamp time.Time
}
