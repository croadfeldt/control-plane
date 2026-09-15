package messaging

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/cenkalti/backoff/v5"
	"github.com/google/uuid"
	"github.com/nats-io/nats.go/jetstream"
)

type Publisher struct {
	js jetstream.JetStream
}

func NewPublisher(js jetstream.JetStream) *Publisher {
	return &Publisher{js: js}
}

// defaultPublishRetryOptions bounds retries to a few hundred milliseconds,
// since publish is called synchronously from request-handling paths.
//
// A fresh *backoff.ExponentialBackOff must be built on every call, not
// stored on the Publisher: it carries mutable state that Retry() mutates in
// place, and Publisher is shared across concurrent goroutines.
func defaultPublishRetryOptions() []backoff.RetryOption {
	b := backoff.NewExponentialBackOff()
	b.InitialInterval = 100 * time.Millisecond
	b.MaxInterval = 1 * time.Second
	b.Multiplier = 2.0
	return []backoff.RetryOption{
		backoff.WithBackOff(b),
		backoff.WithMaxTries(4), // 1 initial attempt + 3 retries
	}
}

// EnsureStream creates or updates the agent request stream. Call during startup.
// WorkQueuePolicy is used because these are point-to-point work queues (one
// durable consumer per stream, each message consumed and acked exactly
// once) - messages are removed once acked instead of retained forever.
func (p *Publisher) EnsureStream(ctx context.Context) error {
	_, err := p.js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:      StreamName,
		Subjects:  []string{StreamSubjectBinding},
		Retention: jetstream.WorkQueuePolicy,
	})
	if err != nil {
		return fmt.Errorf("ensure agent request stream: %w", err)
	}
	return nil
}

func (p *Publisher) PublishCreate(ctx context.Context, subject string, payload CreatePayload) error {
	return p.publish(ctx, subject, CETypeCreateRequest, payload.ResourceID, payload)
}

func (p *Publisher) PublishDelete(ctx context.Context, subject string, payload DeletePayload) error {
	return p.publish(ctx, subject, CETypeDeleteRequest, payload.ResourceID, payload)
}

func (p *Publisher) PublishCancel(ctx context.Context, subject string, payload CancelPayload) error {
	return p.publish(ctx, subject, CETypeCancelRequest, payload.ResourceID, payload)
}

// publish marshals the CloudEvent envelope and publishes it with a bounded
// retry. The CE envelope "id" is also set as the NATS Nats-Msg-Id dedup
// header, so a retried publish (by this backoff loop, or by a caller like
// the pending sweep re-publishing after a timeout) that reaches JetStream
// twice within the dedup window is deduplicated server-side rather than
// producing a duplicate create/delete/cancel request to the agent. Also
// logs the outcome once, centrally, for every call site.
func (p *Publisher) publish(ctx context.Context, subject, ceType, instanceID string, payload any) error {
	ceID := uuid.New().String()
	envelope := map[string]any{
		"specversion": CESpecVersion,
		"type":        ceType,
		"source":      CESource,
		"subject":     subject,
		"id":          ceID,
		"data":        payload,
	}
	log := slog.With("instance_id", instanceID, "subject", subject, "event_type", ceType, "ce_id", ceID)

	data, err := json.Marshal(envelope)
	if err != nil {
		log.Error("failed to marshal event, not publishing", "error", err)
		return err
	}

	operation := func() (struct{}, error) {
		_, pubErr := p.js.Publish(ctx, subject, data, jetstream.WithMsgID(ceID))
		return struct{}{}, pubErr
	}
	_, err = backoff.Retry(ctx, operation, defaultPublishRetryOptions()...)
	if err != nil {
		log.Error("event publish failed", "error", err)
		return err
	}
	log.Info("event published")
	return nil
}
