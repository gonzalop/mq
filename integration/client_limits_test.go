package mq_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/gonzalop/mq"
)

// TestClientLimits_Validation verifies client-side validation for Topic Length and Payload Size.
func TestClientLimits_Validation(t *testing.T) {
	t.Parallel()
	server, cleanup := startMosquitto(t, "")
	defer cleanup()

	t.Run("MaxTopicLength", func(t *testing.T) {
		// client with limit 10
		c, err := mq.Dial(server,
			mq.WithClientID("limit-topic-client"),
			mq.WithMaxTopicLength(10),
		)
		if err != nil {
			t.Fatalf("Failed to connect: %v", err)
		}
		defer c.Disconnect(context.Background())

		// Valid topic (10 chars)
		if token := c.Publish("1234567890", []byte("ok"), mq.WithQoS(1)); token.Error() != nil {
			t.Errorf("Valid topic failed: %v", token.Error())
		}

		// Invalid topic (11 chars)
		if token := c.Publish("12345678901", []byte("fail"), mq.WithQoS(1)); token.Error() == nil {
			t.Error("Expected error for topic > 10 chars, got nil")
		} else if !strings.Contains(token.Error().Error(), "topic length") {
			t.Errorf("Expected 'topic length' error, got: %v", token.Error())
		}
	})

	t.Run("MaxPayloadSize", func(t *testing.T) {
		// client with limit 50
		c, err := mq.Dial(server,
			mq.WithClientID("limit-payload-client"),
			mq.WithMaxPayloadSize(50),
		)
		if err != nil {
			t.Fatalf("Failed to connect: %v", err)
		}
		defer c.Disconnect(context.Background())

		// Valid payload (50 bytes)
		if token := c.Publish("topic", make([]byte, 50), mq.WithQoS(1)); token.Error() != nil {
			t.Errorf("Valid payload failed: %v", token.Error())
		}

		// Invalid payload (51 bytes)
		if token := c.Publish("topic", make([]byte, 51), mq.WithQoS(1)); token.Error() == nil {
			t.Error("Expected error for payload > 50 bytes, got nil")
		} else if !strings.Contains(token.Error().Error(), "payload size") {
			t.Errorf("Expected 'payload size' error, got: %v", token.Error())
		}
	})
}

// TestClientLimits_IncomingPacket verifies the client disconnects if it receives a packet larger than MaxIncomingPacket.
// Uses MQTT v3.1.1 to force the server to possibly send oversized packets since v3 doesn't negotiate max packet size.
func TestClientLimits_IncomingPacket(t *testing.T) {
	t.Parallel()
	server, cleanup := startMosquitto(t, "")
	defer cleanup()

	topic := "data/heavy"

	// 1. Setup a "victim" client with a small Incoming Packet limit (e.g., 100 bytes).
	// We use ProtocolV311 to ensure the server doesn't filter messages based on V5 properties immediately.
	connectionLost := make(chan error, 1)

	victim, err := mq.Dial(server,
		mq.WithClientID("victim-client"),
		mq.WithProtocolVersion(mq.ProtocolV311),
		mq.WithMaxIncomingPacket(100), // Strict limit
		mq.WithOnConnectionLost(func(c *mq.Client, err error) {
			connectionLost <- err
		}),
		mq.WithAutoReconnect(false), // Do not reconnect, we want to verify the disconnect
	)
	if err != nil {
		t.Fatalf("Victim failed to connect: %v", err)
	}
	defer func() { _ = victim.Disconnect(context.Background()) }()

	// Subscribe to a topic
	if token := victim.Subscribe(topic, 1, nil); token.Wait(context.Background()) != nil {
		t.Fatalf("Victim failed to subscribe: %v", token.Error())
	}

	// 2. Setup an "attacker" client to send a large message (e.g., 200 bytes).
	attacker, err := mq.Dial(server, mq.WithClientID("attacker-client"))
	if err != nil {
		t.Fatalf("Attacker failed to connect: %v", err)
	}
	defer attacker.Disconnect(context.Background())

	largePayload := make([]byte, 200)
	if token := attacker.Publish(topic, largePayload, mq.WithQoS(1)); token.Wait(context.Background()) != nil {
		t.Fatalf("Attacker failed to publish: %v", token.Error())
	}

	// 3. Verify the victim client disconnects with "packet size exceeded" error.
	select {
	case err := <-connectionLost:
		if err == nil {
			t.Error("Victim disconnected with nil error")
		} else {
			// In V3.1.1, the server might just close the connection without a reason code packet,
			// result in "connection lost" or "EOF".
			t.Logf("Got disconnect error: %v", err)
			msg := err.Error()
			if !strings.Contains(msg, "packet size") &&
				!strings.Contains(msg, "connection lost") &&
				!strings.Contains(msg, "EOF") {
				t.Errorf("Expected 'packet size' or 'connection lost' error, got: %v", err)
			}
		}
	case <-time.After(3 * time.Second):
		t.Error("Timeout: Victim client did not disconnect after receiving oversized packet")
	}
}
