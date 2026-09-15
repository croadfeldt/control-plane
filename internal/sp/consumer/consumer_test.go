package consumer_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	cloudevents "github.com/cloudevents/sdk-go/v2/event"
	agentmodel "github.com/dcm-project/control-plane/internal/agent/store/model"
	placementtypes "github.com/dcm-project/control-plane/internal/placement/types"
	"github.com/dcm-project/control-plane/internal/sp/consumer"
	"github.com/dcm-project/control-plane/internal/sp/store"
	"github.com/dcm-project/control-plane/internal/sp/store/model"
	"github.com/google/uuid"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type statusConsumerTestBG struct {
	ctx    context.Context
	cancel context.CancelFunc
}

// startStatusConsumerTestContext keeps context.WithCancel out of BeforeEach (fatcontext).
func startStatusConsumerTestContext(bg *statusConsumerTestBG) {
	bg.ctx, bg.cancel = context.WithCancel(context.Background())
}

var _ = Describe("StatusConsumer", func() {
	var (
		db        *gorm.DB
		dataStore store.Store
		nc        *nats.Conn
		js        jetstream.JetStream
		sc        *consumer.StatusConsumer
		bg        statusConsumerTestBG
		natsURL   string
		streamID  string
	)

	BeforeEach(func() {
		var err error

		// Setup in-memory database
		db, err = gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{
			Logger: logger.Default.LogMode(logger.Silent),
		})
		Expect(err).NotTo(HaveOccurred())
		sqlDB, err := db.DB()
		Expect(err).NotTo(HaveOccurred())
		sqlDB.SetMaxOpenConns(1)
		Expect(db.AutoMigrate(&agentmodel.Agent{}, &model.ServiceTypeInstance{})).To(Succeed())
		dataStore = store.NewStore(db)

		// Use the NATS test server URL from suite_test.go
		natsURL = testNATSServer.ClientURL()

		// Connect a publisher client with JetStream
		nc, err = nats.Connect(natsURL)
		Expect(err).NotTo(HaveOccurred())
		js, err = jetstream.New(nc)
		Expect(err).NotTo(HaveOccurred())

		// Use unique stream/consumer names per test to avoid conflicts
		streamID = uuid.New().String()[:8]

		// Create and start the consumer
		sc, err = consumer.New(natsURL, "dcm.*", dataStore,
			consumer.SetStreamName("test-stream-"+streamID),
			consumer.SetConsumerName("test-consumer-"+streamID),
		)
		Expect(err).NotTo(HaveOccurred())

		startStatusConsumerTestContext(&bg)
		Expect(sc.Start(bg.ctx)).To(Succeed())
	})

	AfterEach(func() {
		sc.Stop()
		_ = js.DeleteStream(context.Background(), "test-stream-"+streamID)
		nc.Close()
		bg.cancel()
		sqlDB, _ := db.DB()
		_ = sqlDB.Close()
	})

	publishStatusEvent := func(providerName, serviceType, instanceID, status, message string) {
		event := cloudevents.New()
		event.SetID(uuid.New().String())
		event.SetSource(fmt.Sprintf("dcm/providers/%s", providerName))
		event.SetType(fmt.Sprintf("dcm.status.%s", serviceType))
		event.SetTime(time.Now())

		payload := consumer.StatusEvent{
			Id:        instanceID,
			Status:    status,
			Message:   message,
			Timestamp: time.Now(),
		}
		Expect(event.SetData(cloudevents.ApplicationJSON, payload)).To(Succeed())

		data, err := json.Marshal(event)
		Expect(err).NotTo(HaveOccurred())

		subject := fmt.Sprintf("dcm.%s", serviceType)
		_, err = js.Publish(bg.ctx, subject, data)
		Expect(err).NotTo(HaveOccurred())
	}

	createInstance := func(instanceID string) {
		instance := model.ServiceTypeInstance{
			ID:           instanceID,
			Status:       "provisioning",
			InstanceName: "test-instance",
			Spec:         map[string]any{"cpu": "2"},
		}
		_, err := dataStore.ServiceTypeInstance().Create(bg.ctx, instance)
		Expect(err).NotTo(HaveOccurred())
	}

	It("invokes onRunning for RUNNING events with or without output_spec", func() {
		sc.Stop()
		Expect(js.DeleteStream(bg.ctx, "test-stream-"+streamID)).To(Succeed())

		runningCalls := 0
		scWithHandler, err := consumer.New(natsURL, "dcm.*", dataStore,
			consumer.SetStreamName("test-stream-"+streamID),
			consumer.SetConsumerName("test-consumer-handler-"+streamID),
			consumer.SetPlacementStatusHandlers(
				func(_ context.Context, _ placementtypes.ResourceStatusEvent) error {
					runningCalls++
					return nil
				},
				nil,
				nil,
			),
		)
		Expect(err).NotTo(HaveOccurred())
		Expect(scWithHandler.Start(bg.ctx)).To(Succeed())
		defer scWithHandler.Stop()

		instanceID := uuid.New().String()
		createInstance(instanceID)

		publishStatusEvent("kubevirt-sp", "vm", instanceID, "RUNNING", "VM is running without outputs")
		Eventually(func() int { return runningCalls }, 2*time.Second, 100*time.Millisecond).Should(Equal(1))

		event := cloudevents.New()
		event.SetID(uuid.New().String())
		event.SetSource("dcm/providers/kubevirt-sp")
		event.SetType("dcm.status.vm")
		event.SetTime(time.Now())
		payload := consumer.StatusEvent{
			Id:         instanceID,
			Status:     "RUNNING",
			Message:    "VM is running with outputs",
			OutputSpec: map[string]any{"connection_string": "postgres://db:5432/orders"},
			Timestamp:  time.Now(),
		}
		Expect(event.SetData(cloudevents.ApplicationJSON, payload)).To(Succeed())
		data, err := json.Marshal(event)
		Expect(err).NotTo(HaveOccurred())
		_, err = js.Publish(bg.ctx, "dcm.vm", data)
		Expect(err).NotTo(HaveOccurred())

		Eventually(func() int { return runningCalls }, 2*time.Second, 100*time.Millisecond).Should(Equal(2))
	})

	It("persists output_spec from status events", func() {
		instanceID := uuid.New().String()
		createInstance(instanceID)

		event := cloudevents.New()
		event.SetID(uuid.New().String())
		event.SetSource("dcm/providers/kubevirt-sp")
		event.SetType("dcm.status.vm")
		event.SetTime(time.Now())
		payload := consumer.StatusEvent{
			Id:      instanceID,
			Status:  "RUNNING",
			Message: "VM is running",
			OutputSpec: map[string]any{
				"connection_string": "postgres://db:5432/orders",
			},
			Timestamp: time.Now(),
		}
		Expect(event.SetData(cloudevents.ApplicationJSON, payload)).To(Succeed())
		data, err := json.Marshal(event)
		Expect(err).NotTo(HaveOccurred())
		_, err = js.Publish(bg.ctx, "dcm.vm", data)
		Expect(err).NotTo(HaveOccurred())

		Eventually(func() map[string]any {
			var inst model.ServiceTypeInstance
			db.Where("id = ?", instanceID).First(&inst)
			return inst.OutputSpec
		}, 2*time.Second, 100*time.Millisecond).Should(Equal(map[string]any{
			"connection_string": "postgres://db:5432/orders",
		}))
	})

	It("updates instance status on valid status event", func() {
		instanceID := uuid.New().String()
		createInstance(instanceID)

		publishStatusEvent("kubevirt-sp", "vm", instanceID, "RUNNING", "VM is running")

		Eventually(func() string {
			var inst model.ServiceTypeInstance
			db.Where("id = ?", instanceID).First(&inst)
			return inst.Status
		}, 2*time.Second, 100*time.Millisecond).Should(Equal("running"))
	})

	It("normalizes mixed-case status casing from external producers to lowercase", func() {
		// The external provider CloudEvent producer is not under our control and
		// may send any casing; the DB's status column must stay consistently
		// lowercase regardless (see internal/sp/store/model/status.go).
		instanceID := uuid.New().String()
		createInstance(instanceID)

		publishStatusEvent("kubevirt-sp", "vm", instanceID, "Running", "VM is running")

		Eventually(func() string {
			var inst model.ServiceTypeInstance
			db.Where("id = ?", instanceID).First(&inst)
			return inst.Status
		}, 2*time.Second, 100*time.Millisecond).Should(Equal("running"))
	})

	It("updates status message along with status", func() {
		instanceID := uuid.New().String()
		createInstance(instanceID)

		publishStatusEvent("kubevirt-sp", "vm", instanceID, "FAILED", "VM crashed unexpectedly")

		Eventually(func() string {
			var inst model.ServiceTypeInstance
			db.Where("id = ?", instanceID).First(&inst)
			return inst.StatusMessage
		}, 2*time.Second, 100*time.Millisecond).Should(Equal("VM crashed unexpectedly"))
	})

	It("stores the status from a FAILED event as lowercase", func() {
		instanceID := uuid.New().String()
		createInstance(instanceID)

		publishStatusEvent("kubevirt-sp", "vm", instanceID, "FAILED", "VM crashed unexpectedly")

		Eventually(func() string {
			var inst model.ServiceTypeInstance
			db.Where("id = ?", instanceID).First(&inst)
			return inst.Status
		}, 2*time.Second, 100*time.Millisecond).Should(Equal("failed"))
	})

	It("handles events for non-existent instances gracefully", func() {
		publishStatusEvent("kubevirt-sp", "vm", "non-existent-id", "RUNNING", "VM is running")

		// Give it time to process - no panic expected
		time.Sleep(200 * time.Millisecond)
	})

	It("discards malformed CloudEvent messages", func() {
		_, err := js.Publish(bg.ctx, "dcm.vm", []byte("not-valid-json"))
		Expect(err).NotTo(HaveOccurred())

		// Give it time to process - no panic expected
		time.Sleep(200 * time.Millisecond)
	})

	It("handles multiple sequential status updates", func() {
		instanceID := uuid.New().String()
		createInstance(instanceID)

		publishStatusEvent("kubevirt-sp", "vm", instanceID, "PROVISIONING", "Starting VM")

		Eventually(func() string {
			var inst model.ServiceTypeInstance
			db.Where("id = ?", instanceID).First(&inst)
			return inst.Status
		}, 2*time.Second, 100*time.Millisecond).Should(Equal("provisioning"))

		time.Sleep(200 * time.Millisecond)

		publishStatusEvent("kubevirt-sp", "vm", instanceID, "RUNNING", "VM is running")

		Eventually(func() string {
			var inst model.ServiceTypeInstance
			db.Where("id = ?", instanceID).First(&inst)
			return inst.Status
		}, 2*time.Second, 100*time.Millisecond).Should(Equal("running"))
	})

	It("Check succeeds while connected", func() {
		Expect(sc.Check(bg.ctx)).To(Succeed())
	})

	It("Check returns immediately when the context is already done", func() {
		expired, cancel := context.WithTimeout(bg.ctx, time.Nanosecond)
		defer cancel()
		time.Sleep(time.Millisecond)

		err := sc.Check(expired)
		Expect(err).To(HaveOccurred())
		Expect(errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled)).To(BeTrue())
	})
})
