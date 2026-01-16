package mq_test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/gonzalop/mq"
)

func TestBasicPublishSubscribe(t *testing.T) {
	t.Parallel()
	server, cleanup := startMosquitto(t, "")
	defer cleanup()

	// Connect client
	client, err := mq.Dial(server, mq.WithClientID("test-client"))
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer client.Disconnect(context.Background())

	// Subscribe to topic
	received := make(chan mq.Message, 1)
	token := client.Subscribe("test/topic", 1, func(c *mq.Client, msg mq.Message) {
		received <- msg
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := token.Wait(ctx); err != nil {
		t.Fatalf("Failed to subscribe: %v", err)
	}

	// Publish message
	pubToken := client.Publish("test/topic", []byte("hello world"), mq.WithQoS(1))
	if err := pubToken.Wait(ctx); err != nil {
		t.Fatalf("Failed to publish: %v", err)
	}

	// Wait for message
	select {
	case msg := <-received:
		if string(msg.Payload) != "hello world" {
			t.Errorf("Payload = %s, want 'hello world'", string(msg.Payload))
		}
		if msg.Topic != "test/topic" {
			t.Errorf("Topic = %s, want 'test/topic'", msg.Topic)
		}
		if msg.QoS != 1 {
			t.Errorf("QoS = %d, want 1", msg.QoS)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Timeout waiting for message")
	}
}

func TestQoS0PublishSubscribe(t *testing.T) {
	t.Parallel()
	server, cleanup := startMosquitto(t, "")
	defer cleanup()

	client, err := mq.Dial(server, mq.WithClientID("test-qos0"))
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer client.Disconnect(context.Background())

	received := make(chan mq.Message, 1)
	token := client.Subscribe("qos0/topic", 0, func(c *mq.Client, msg mq.Message) {
		received <- msg
	})

	if err := token.Wait(context.Background()); err != nil {
		t.Fatalf("Failed to subscribe: %v", err)
	}

	// QoS 0 - fire and forget
	client.Publish("qos0/topic", []byte("qos0 message"), mq.WithQoS(0))

	select {
	case msg := <-received:
		if string(msg.Payload) != "qos0 message" {
			t.Errorf("Payload = %s, want 'qos0 message'", string(msg.Payload))
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Timeout waiting for QoS 0 message")
	}
}

func TestQoS2PublishSubscribe(t *testing.T) {
	t.Parallel()
	server, cleanup := startMosquitto(t, "")
	defer cleanup()

	client, err := mq.Dial(server, mq.WithClientID("test-qos2"))
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer client.Disconnect(context.Background())

	received := make(chan mq.Message, 1)
	token := client.Subscribe("qos2/topic", 2, func(c *mq.Client, msg mq.Message) {
		received <- msg
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := token.Wait(ctx); err != nil {
		t.Fatalf("Failed to subscribe: %v", err)
	}

	// QoS 2 - exactly once
	pubToken := client.Publish("qos2/topic", []byte("qos2 message"), mq.WithQoS(2))
	if err := pubToken.Wait(ctx); err != nil {
		t.Fatalf("Failed to publish: %v", err)
	}

	select {
	case msg := <-received:
		if string(msg.Payload) != "qos2 message" {
			t.Errorf("Payload = %s, want 'qos2 message'", string(msg.Payload))
		}
		if msg.QoS != 2 {
			t.Errorf("QoS = %d, want 2", msg.QoS)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Timeout waiting for QoS 2 message")
	}
}

func TestWildcardSubscriptions(t *testing.T) {
	t.Parallel()
	server, cleanup := startMosquitto(t, "")
	defer cleanup()

	client, err := mq.Dial(server, mq.WithClientID("test-wildcards"))
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer client.Disconnect(context.Background())

	tests := []struct {
		name         string
		filter       string
		publishTopic string
		shouldMatch  bool
	}{
		{"single level wildcard", "sensors/+/temperature", "sensors/living-room/temperature", true},
		{"single level no match", "sensors/+/temperature", "sensors/living-room/humidity", false},
		{"multi level wildcard", "sensors/#", "sensors/living-room/temperature", true},
		{"multi level deep", "sensors/#", "sensors/living-room/temperature/current", true},
		{"multi level no match", "sensors/#", "devices/switch", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			received := make(chan mq.Message, 1)
			token := client.Subscribe(tt.filter, 1, func(c *mq.Client, msg mq.Message) {
				received <- msg
			})

			if err := token.Wait(context.Background()); err != nil {
				t.Fatalf("Failed to subscribe: %v", err)
			}

			// Publish message
			client.Publish(tt.publishTopic, []byte("test"), mq.WithQoS(1))

			// Check if message received
			select {
			case msg := <-received:
				if !tt.shouldMatch {
					t.Errorf("Received message on %s but shouldn't have matched filter %s", msg.Topic, tt.filter)
				}
				if msg.Topic != tt.publishTopic {
					t.Errorf("Topic = %s, want %s", msg.Topic, tt.publishTopic)
				}
			case <-time.After(2 * time.Second):
				if tt.shouldMatch {
					t.Errorf("Timeout waiting for message, should have matched")
				}
			}

			// Unsubscribe for next test
			client.Unsubscribe(tt.filter)
			time.Sleep(100 * time.Millisecond)
		})
	}
}

func TestRetainedMessages(t *testing.T) {
	t.Parallel()
	server, cleanup := startMosquitto(t, "")
	defer cleanup()

	topic := "retained/topic/" + t.Name()

	// First client publishes retained message
	client1, err := mq.Dial(server, mq.WithClientID("test-retain-pub-"+t.Name()))
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}

	token := client1.Publish(topic, []byte("retained message"),
		mq.WithQoS(1),
		mq.WithRetain(true))

	if err := token.Wait(context.Background()); err != nil {
		t.Fatalf("Failed to publish retained: %v", err)
	}

	client1.Disconnect(context.Background())

	// Second client subscribes and should receive retained message
	client2, err := mq.Dial(server, mq.WithClientID("test-retain-sub-"+t.Name()))
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer client2.Disconnect(context.Background())

	received := make(chan mq.Message, 1)
	subToken := client2.Subscribe(topic, 1, func(c *mq.Client, msg mq.Message) {
		received <- msg
	})

	if err := subToken.Wait(context.Background()); err != nil {
		t.Fatalf("Failed to subscribe: %v", err)
	}

	select {
	case msg := <-received:
		if string(msg.Payload) != "retained message" {
			t.Errorf("Payload = %s, want 'retained message'", string(msg.Payload))
		}
		if !msg.Retained {
			t.Error("Message should be marked as retained")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Timeout waiting for retained message")
	}
}

func TestMultipleSubscribers(t *testing.T) {
	t.Parallel()
	server, cleanup := startMosquitto(t, "")
	defer cleanup()

	topic := "multi/topic/" + t.Name()

	// Create 3 clients
	clients := make([]*mq.Client, 3)
	channels := make([]chan mq.Message, 3)

	for i := 0; i < 3; i++ {
		client, err := mq.Dial(server, mq.WithClientID(fmt.Sprintf("test-multi-%s-%d", t.Name(), i)))
		if err != nil {
			t.Fatalf("Failed to connect client %d: %v", i, err)
		}
		defer client.Disconnect(context.Background())

		clients[i] = client
		channels[i] = make(chan mq.Message, 1)

		ch := channels[i] // Capture for closure
		token := client.Subscribe(topic, 1, func(c *mq.Client, msg mq.Message) {
			ch <- msg
		})

		if err := token.Wait(context.Background()); err != nil {
			t.Fatalf("Failed to subscribe client %d: %v", i, err)
		}
	}

	// Publish one message
	clients[0].Publish(topic, []byte("broadcast"), mq.WithQoS(1))

	// All clients should receive it
	for i := 0; i < 3; i++ {
		select {
		case msg := <-channels[i]:
			if string(msg.Payload) != "broadcast" {
				t.Errorf("Client %d: Payload = %s, want 'broadcast'", i, string(msg.Payload))
			}
		case <-time.After(5 * time.Second):
			t.Errorf("Client %d: Timeout waiting for message", i)
		}
	}
}

func TestHighThroughput(t *testing.T) {
	t.Parallel()
	server, cleanup := startMosquitto(t, "")
	defer cleanup()

	client, err := mq.Dial(server, mq.WithClientID("test-throughput-"+t.Name()))
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer client.Disconnect(context.Background())

	topic := "throughput/topic/" + t.Name()
	const numMessages = 1000
	received := make(chan mq.Message, numMessages)
	var receivedCount int
	var mu sync.Mutex

	token := client.Subscribe(topic, 1, func(c *mq.Client, msg mq.Message) {
		mu.Lock()
		receivedCount++
		mu.Unlock()
		received <- msg
	})

	if err := token.Wait(context.Background()); err != nil {
		t.Fatalf("Failed to subscribe: %v", err)
	}

	// Publish many messages
	start := time.Now()
	for i := 0; i < numMessages; i++ {
		payload := fmt.Sprintf("message-%d", i)
		client.Publish(topic, []byte(payload), mq.WithQoS(1))
	}

	// Wait for all messages
	timeout := time.After(30 * time.Second)
	for i := 0; i < numMessages; i++ {
		select {
		case <-received:
			// Message received
		case <-timeout:
			t.Fatalf("Timeout: received %d/%d messages", receivedCount, numMessages)
		}
	}

	elapsed := time.Since(start)
	t.Logf("Sent and received %d messages in %v (%.0f msg/sec)",
		numMessages, elapsed, float64(numMessages)/elapsed.Seconds())

	mu.Lock()
	if receivedCount != numMessages {
		t.Errorf("Received %d messages, want %d", receivedCount, numMessages)
	}
	mu.Unlock()
}

func TestUnsubscribe(t *testing.T) {
	t.Parallel()
	server, cleanup := startMosquitto(t, "")
	defer cleanup()

	client, err := mq.Dial(server, mq.WithClientID("test-unsub-"+t.Name()))
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer client.Disconnect(context.Background())

	topic := "unsub/topic/" + t.Name()
	received := make(chan mq.Message, 10)
	token := client.Subscribe(topic, 1, func(c *mq.Client, msg mq.Message) {
		received <- msg
	})

	if err := token.Wait(context.Background()); err != nil {
		t.Fatalf("Failed to subscribe: %v", err)
	}

	// Publish first message
	client.Publish(topic, []byte("before unsub"), mq.WithQoS(1))

	select {
	case msg := <-received:
		if string(msg.Payload) != "before unsub" {
			t.Errorf("Payload = %s, want 'before unsub'", string(msg.Payload))
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Timeout waiting for first message")
	}

	// Unsubscribe
	unsubToken := client.Unsubscribe(topic)
	if err := unsubToken.Wait(context.Background()); err != nil {
		t.Fatalf("Failed to unsubscribe: %v", err)
	}

	// Publish second message
	client.Publish(topic, []byte("after unsub"), mq.WithQoS(1))

	// Should NOT receive this message
	select {
	case msg := <-received:
		t.Errorf("Received message after unsubscribe: %s", string(msg.Payload))
	case <-time.After(2 * time.Second):
		// Expected - no message received
	}
}

func TestCleanSession(t *testing.T) {
	t.Parallel()
	server, cleanup := startMosquitto(t, "")
	defer cleanup()

	clientID := "test-persistent-" + t.Name()
	topic := "persistent/topic/" + t.Name()

	// Connect with clean session = false
	client1, err := mq.Dial(server,
		mq.WithClientID(clientID),
		mq.WithCleanSession(false),
		mq.WithSessionExpiryInterval(0xFFFFFFFF))
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}

	// Subscribe
	token := client1.Subscribe(topic, 1, func(c *mq.Client, msg mq.Message) {})
	if err := token.Wait(context.Background()); err != nil {
		t.Fatalf("Failed to subscribe: %v", err)
	}

	// Give subscription time to register
	time.Sleep(500 * time.Millisecond)

	// Disconnect
	client1.Disconnect(context.Background())
	time.Sleep(500 * time.Millisecond)

	// Publish while disconnected
	publisher, _ := mq.Dial(server, mq.WithClientID("test-publisher-"+t.Name()))
	pubToken := publisher.Publish(topic, []byte("offline message"), mq.WithQoS(1))
	if err := pubToken.Wait(context.Background()); err != nil {
		t.Fatalf("Failed to publish: %v", err)
	}
	publisher.Disconnect(context.Background())
	time.Sleep(500 * time.Millisecond)

	received := make(chan mq.Message, 1)

	// Reconnect with same client ID and pre-register handler
	client2, err := mq.Dial(server,
		mq.WithClientID(clientID),
		mq.WithCleanSession(false),
		mq.WithSessionExpiryInterval(0xFFFFFFFF),
		mq.WithSubscription(topic, func(c *mq.Client, msg mq.Message) {
			received <- msg
		}))
	if err != nil {
		t.Fatalf("Failed to reconnect: %v", err)
	}
	defer client2.Disconnect(context.Background())

	// Give some time for queued messages to be delivered
	time.Sleep(500 * time.Millisecond)

	// Should receive the offline message
	select {
	case msg := <-received:
		if string(msg.Payload) != "offline message" {
			t.Errorf("Payload = %s, want 'offline message'", string(msg.Payload))
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Timeout waiting for offline message")
	}
}
