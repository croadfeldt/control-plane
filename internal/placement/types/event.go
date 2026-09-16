package types

// ResourceStatusEvent is the placement callback payload for a resource status transition.
type ResourceStatusEvent struct {
	ResourceID string
	Status     string
	OutputSpec map[string]any
}
