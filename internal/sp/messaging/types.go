// Package messaging provides CloudEvent types and a JetStream publisher for agent communication.
package messaging

const (
	CETypeCreateRequest  = "dcm.request.create"
	CETypeDeleteRequest  = "dcm.request.delete"
	CETypeCancelRequest  = "dcm.request.cancel"
	CESource             = "dcm/control-plane"
	CESpecVersion        = "1.0"
	StreamName           = "dcm-agent-requests"
	StreamSubjectBinding = "dcm.agent.>"
)

// Agent response CE types. These are emitted by the agent (not the control
// plane) on the responses subject/stream; kept here as constants - alongside
// the request types above - so the consumer switch in
// internal/sp/consumer/response_consumer.go can't silently drift from what
// the agent actually sends.
const (
	CETypeCreationAcknowledged = "dcm.agent.creation-acknowledged"
	CETypeError                = "dcm.agent.error"
	CETypeRequestQueued        = "dcm.agent.request-queued"
	CETypeDeletionAcknowledged = "dcm.agent.deletion-acknowledged"
	CETypeCancelAcknowledged   = "dcm.agent.cancel-acknowledged"
	CETypeCancelRejected       = "dcm.agent.cancel-rejected"
)

// ResponseStreamName/ResponseSubject are the wire contract for the agent
// response stream, consumed by consumer.ResponseConsumer. Exported here
// (rather than kept private to that package) so other producers of these
// events - real agents, and tests simulating one - publish to the same
// subject the consumer actually listens on.
const (
	ResponseStreamName = "dcm-agent-responses"
	ResponseSubject    = "dcm.agents.responses"
)

type CreatePayload struct {
	ResourceID  string         `json:"resource_id"`
	ServiceType string         `json:"service_type"`
	Spec        map[string]any `json:"spec"`
}

type DeletePayload struct {
	ResourceID  string `json:"resource_id"`
	ServiceType string `json:"service_type"`
}

type CancelPayload struct {
	ResourceID  string `json:"resource_id"`
	ServiceType string `json:"service_type"`
}
