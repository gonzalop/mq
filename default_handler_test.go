package mq

import (
	"testing"
	"time"

	"github.com/gonzalop/mq/internal/packets"
)

func TestDefaultPublishHandler(t *testing.T) {
	// Create a channel to signal when the handler is called
	handlerCalled := make(chan struct{})
	var receivedTopic string
	var receivedPayload []byte

	defaultHandler := func(c *Client, msg Message) {
		receivedTopic = msg.Topic
		receivedPayload = msg.Payload
		close(handlerCalled)
	}

	c := &Client{
		opts: &clientOptions{
			DefaultPublishHandler: defaultHandler,
			Logger:                testLogger(),
		},
		subscriptions: make(map[string]subscriptionEntry),
		outgoing:      make(chan packets.Packet, 10),
	}

	// Create a publish packet for a topic with NO subscription
	pkt := &packets.PublishPacket{
		Topic:    "unsubscribed/topic",
		Payload:  []byte("test-payload"),
		QoS:      0,
		PacketID: 0,
	}

	// Process the incoming packet
	c.handleIncoming(pkt)

	// Wait for handler to be called
	select {
	case <-handlerCalled:
		// Handler was called, verify content
		if receivedTopic != "unsubscribed/topic" {
			t.Errorf("expected topic 'unsubscribed/topic', got '%s'", receivedTopic)
		}
		if string(receivedPayload) != "test-payload" {
			t.Errorf("expected payload 'test-payload', got '%s'", string(receivedPayload))
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("timed out waiting for default handler to be called")
	}
}

func TestDefaultHandlerNotCalledIfSubscriptionExists(t *testing.T) {
	defaultCalled := make(chan struct{})
	subCalled := make(chan struct{})

	defaultHandler := func(c *Client, msg Message) {
		close(defaultCalled)
	}

	subHandler := func(c *Client, msg Message) {
		close(subCalled)
	}

	c := &Client{
		opts: &clientOptions{
			DefaultPublishHandler: defaultHandler,
			Logger:                testLogger(),
		},
		subscriptions: make(map[string]subscriptionEntry),
		outgoing:      make(chan packets.Packet, 10),
	}

	// Register a subscription
	c.subscriptions["subscribed/topic"] = subscriptionEntry{handler: subHandler}

	// Create a publish packet that MATCHES the subscription
	pkt := &packets.PublishPacket{
		Topic:    "subscribed/topic",
		Payload:  []byte("data"),
		QoS:      0,
		PacketID: 0,
	}

	// Process the incoming packet
	c.handleIncoming(pkt)

	// Verify only subscription handler is called
	select {
	case <-subCalled:
		// success
	case <-time.After(100 * time.Millisecond):
		t.Error("timed out waiting for subscription handler")
	}

	select {
	case <-defaultCalled:
		t.Error("default handler should NOT be called when subscription matches")
	default:
		// success
	}
}
