package messaging_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/dcm-project/control-plane/internal/sp/messaging"
	natsserver "github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// flakyJetStream fails the first N Publish calls with a transient-looking
// error, then delegates to the real JetStream so the publisher's retry
// logic (F9) can be exercised against a real stream/backend.
type flakyJetStream struct {
	jetstream.JetStream
	failures int
	calls    int
}

func (f *flakyJetStream) Publish(ctx context.Context, subject string, data []byte, opts ...jetstream.PublishOpt) (*jetstream.PubAck, error) {
	f.calls++
	if f.calls <= f.failures {
		return nil, errors.New("simulated transient nats error")
	}
	return f.JetStream.Publish(ctx, subject, data, opts...)
}

var _ = Describe("Publisher", func() {
	var (
		ns        *natsserver.Server
		nc        *nats.Conn
		js        jetstream.JetStream
		publisher *messaging.Publisher
		ctx       context.Context
	)

	BeforeEach(func() {
		opts := &natsserver.Options{
			Host:      "127.0.0.1",
			Port:      -1,
			JetStream: true,
			StoreDir:  GinkgoT().TempDir(),
		}
		var err error
		ns, err = natsserver.NewServer(opts)
		Expect(err).NotTo(HaveOccurred())
		ns.Start()
		Expect(ns.ReadyForConnections(2 * time.Second)).To(BeTrue())

		nc, err = nats.Connect(ns.ClientURL())
		Expect(err).NotTo(HaveOccurred())

		js, err = jetstream.New(nc)
		Expect(err).NotTo(HaveOccurred())

		ctx = context.Background()

		_, err = js.CreateStream(ctx, jetstream.StreamConfig{
			Name:     messaging.StreamName,
			Subjects: []string{messaging.StreamSubjectBinding},
		})
		Expect(err).NotTo(HaveOccurred())

		publisher = messaging.NewPublisher(js)
	})

	AfterEach(func() {
		if nc != nil {
			nc.Close()
		}
		if ns != nil {
			ns.Shutdown()
		}
	})

	fetchMessage := func(stream jetstream.Stream, seq uint64) jetstream.RawStreamMsg {
		raw, err := stream.GetMsg(ctx, seq)
		Expect(err).NotTo(HaveOccurred())
		return *raw
	}

	Describe("Create", func() {
		It("publishes to the agent's registered topic_name subject", func() {
			agentTopic := "dcm.agent.prod-eu-west-1"
			payload := messaging.CreatePayload{
				ResourceID:  "res-1",
				ServiceType: "vm",
				Spec:        map[string]any{"cpu": 4},
			}

			err := publisher.PublishCreate(ctx, agentTopic, payload)
			Expect(err).NotTo(HaveOccurred())

			stream, err := js.Stream(ctx, messaging.StreamName)
			Expect(err).NotTo(HaveOccurred())

			raw := fetchMessage(stream, 1)
			Expect(raw.Subject).To(Equal(agentTopic))

			var envelope map[string]any
			Expect(json.Unmarshal(raw.Data, &envelope)).To(Succeed())
			Expect(envelope["type"]).To(Equal("dcm.request.create"))
			Expect(envelope["source"]).To(Equal("dcm/control-plane"))
			Expect(envelope["subject"]).To(Equal(agentTopic))
			Expect(envelope["specversion"]).To(Equal("1.0"))

			data := envelope["data"].(map[string]any)
			Expect(data["resource_id"]).To(Equal("res-1"))
			Expect(data["service_type"]).To(Equal("vm"))
			Expect(data["spec"]).To(HaveKeyWithValue("cpu", float64(4)))
		})
	})

	Describe("Delete", func() {
		It("publishes to the agent's registered topic_name subject (NOT .cancel)", func() {
			agentTopic := "dcm.agent.prod-eu-west-1"
			payload := messaging.DeletePayload{
				ResourceID:  "res-1",
				ServiceType: "vm",
			}

			err := publisher.PublishDelete(ctx, agentTopic, payload)
			Expect(err).NotTo(HaveOccurred())

			stream, err := js.Stream(ctx, messaging.StreamName)
			Expect(err).NotTo(HaveOccurred())

			raw := fetchMessage(stream, 1)
			Expect(raw.Subject).To(Equal(agentTopic))

			var envelope map[string]any
			Expect(json.Unmarshal(raw.Data, &envelope)).To(Succeed())
			Expect(envelope["type"]).To(Equal("dcm.request.delete"))
			Expect(envelope["source"]).To(Equal("dcm/control-plane"))
			Expect(envelope["subject"]).To(Equal(agentTopic))

			data := envelope["data"].(map[string]any)
			Expect(data["resource_id"]).To(Equal("res-1"))
			Expect(data["service_type"]).To(Equal("vm"))
		})
	})

	Describe("Cancel", func() {
		It("publishes to {topic_name}.cancel subject", func() {
			agentTopic := "dcm.agent.prod-eu-west-1"
			cancelSubject := agentTopic + ".cancel"
			payload := messaging.CancelPayload{
				ResourceID:  "res-1",
				ServiceType: "vm",
			}

			err := publisher.PublishCancel(ctx, cancelSubject, payload)
			Expect(err).NotTo(HaveOccurred())

			stream, err := js.Stream(ctx, messaging.StreamName)
			Expect(err).NotTo(HaveOccurred())

			raw := fetchMessage(stream, 1)
			Expect(raw.Subject).To(Equal(cancelSubject))

			var envelope map[string]any
			Expect(json.Unmarshal(raw.Data, &envelope)).To(Succeed())
			Expect(envelope["type"]).To(Equal("dcm.request.cancel"))
			Expect(envelope["source"]).To(Equal("dcm/control-plane"))
			Expect(envelope["subject"]).To(Equal(cancelSubject))

			data := envelope["data"].(map[string]any)
			Expect(data["resource_id"]).To(Equal("res-1"))
			Expect(data["service_type"]).To(Equal("vm"))
		})

		It("cancel subject must end with .cancel suffix", func() {
			agentTopic := "dcm.agent.my-custom-topic"
			cancelSubject := agentTopic + ".cancel"
			payload := messaging.CancelPayload{ResourceID: "res-2", ServiceType: "container"}

			err := publisher.PublishCancel(ctx, cancelSubject, payload)
			Expect(err).NotTo(HaveOccurred())

			stream, err := js.Stream(ctx, messaging.StreamName)
			Expect(err).NotTo(HaveOccurred())

			raw := fetchMessage(stream, 1)
			Expect(raw.Subject).To(HaveSuffix(".cancel"))
		})
	})

	Describe("Nats-Msg-Id dedup header", func() {
		It("sets Nats-Msg-Id to the same value as the CloudEvent id", func() {
			agentTopic := "dcm.agent.prod-eu-west-1"
			payload := messaging.CreatePayload{ResourceID: "res-1", ServiceType: "vm", Spec: map[string]any{}}

			err := publisher.PublishCreate(ctx, agentTopic, payload)
			Expect(err).NotTo(HaveOccurred())

			stream, err := js.Stream(ctx, messaging.StreamName)
			Expect(err).NotTo(HaveOccurred())
			raw := fetchMessage(stream, 1)

			var envelope map[string]any
			Expect(json.Unmarshal(raw.Data, &envelope)).To(Succeed())
			ceID, ok := envelope["id"].(string)
			Expect(ok).To(BeTrue())
			Expect(ceID).NotTo(BeEmpty())

			Expect(raw.Header.Get("Nats-Msg-Id")).To(Equal(ceID))
		})
	})

	Describe("Publish retry (transient failures)", func() {
		It("retries on transient publish errors and eventually succeeds", func() {
			flaky := &flakyJetStream{JetStream: js, failures: 2}
			retryingPublisher := messaging.NewPublisher(flaky)

			agentTopic := "dcm.agent.retry-target"
			payload := messaging.CreatePayload{ResourceID: "res-retry", ServiceType: "vm", Spec: map[string]any{}}

			err := retryingPublisher.PublishCreate(ctx, agentTopic, payload)
			Expect(err).NotTo(HaveOccurred())
			Expect(flaky.calls).To(Equal(3))

			stream, err := js.Stream(ctx, messaging.StreamName)
			Expect(err).NotTo(HaveOccurred())
			raw := fetchMessage(stream, 1)
			Expect(raw.Subject).To(Equal(agentTopic))
		})

		It("returns an error once retries are exhausted", func() {
			flaky := &flakyJetStream{JetStream: js, failures: 99}
			retryingPublisher := messaging.NewPublisher(flaky)

			err := retryingPublisher.PublishCreate(ctx, "dcm.agent.always-fails", messaging.CreatePayload{
				ResourceID: "res-fail", ServiceType: "vm", Spec: map[string]any{},
			})
			Expect(err).To(HaveOccurred())
		})
	})

	Describe("Logging", func() {
		It("logs the published event on success with instance_id, subject, event_type and ce_id", func() {
			var buf bytes.Buffer
			prevLogger := slog.Default()
			slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
			defer slog.SetDefault(prevLogger)

			agentTopic := "dcm.agent.prod-eu-west-1"
			payload := messaging.CreatePayload{ResourceID: "res-log-1", ServiceType: "vm", Spec: map[string]any{}}

			Expect(publisher.PublishCreate(ctx, agentTopic, payload)).To(Succeed())

			Expect(buf.String()).To(SatisfyAll(
				ContainSubstring("event published"),
				ContainSubstring("instance_id=res-log-1"),
				ContainSubstring("subject="+agentTopic),
				ContainSubstring("event_type=dcm.request.create"),
				ContainSubstring("ce_id="),
			))
		})

		It("logs the publish failure once retries are exhausted", func() {
			var buf bytes.Buffer
			prevLogger := slog.Default()
			slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
			defer slog.SetDefault(prevLogger)

			flaky := &flakyJetStream{JetStream: js, failures: 99}
			failingPublisher := messaging.NewPublisher(flaky)

			err := failingPublisher.PublishCreate(ctx, "dcm.agent.always-fails", messaging.CreatePayload{
				ResourceID: "res-log-fail", ServiceType: "vm", Spec: map[string]any{},
			})
			Expect(err).To(HaveOccurred())

			Expect(buf.String()).To(SatisfyAll(
				ContainSubstring("event publish failed"),
				ContainSubstring("instance_id=res-log-fail"),
			))
		})
	})

	Describe("Stream retention", func() {
		It("EnsureStream configures the agent request stream with WorkQueuePolicy", func() {
			// BeforeEach already created the stream with default (limits)
			// retention; JetStream disallows changing retention policy on an
			// existing stream, so start from a clean slate for this test.
			Expect(js.DeleteStream(ctx, messaging.StreamName)).To(Succeed())

			Expect(publisher.EnsureStream(ctx)).To(Succeed())

			stream, err := js.Stream(ctx, messaging.StreamName)
			Expect(err).NotTo(HaveOccurred())
			info, err := stream.Info(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(info.Config.Retention).To(Equal(jetstream.WorkQueuePolicy))
		})
	})

	Describe("Topic routing uses registered topic_name, not agent name", func() {
		It("different agents with different topic_names route correctly", func() {
			topicA := "dcm.agent.custom-alpha"
			topicB := "dcm.agent.custom-beta"

			err := publisher.PublishCreate(ctx, topicA, messaging.CreatePayload{
				ResourceID: "res-a", ServiceType: "vm", Spec: map[string]any{},
			})
			Expect(err).NotTo(HaveOccurred())

			err = publisher.PublishCreate(ctx, topicB, messaging.CreatePayload{
				ResourceID: "res-b", ServiceType: "container", Spec: map[string]any{},
			})
			Expect(err).NotTo(HaveOccurred())

			stream, err := js.Stream(ctx, messaging.StreamName)
			Expect(err).NotTo(HaveOccurred())

			rawA := fetchMessage(stream, 1)
			Expect(rawA.Subject).To(Equal(topicA))

			rawB := fetchMessage(stream, 2)
			Expect(rawB.Subject).To(Equal(topicB))
		})
	})
})
