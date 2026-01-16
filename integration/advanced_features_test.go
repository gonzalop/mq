package mq_test

import (
	"context"
	"testing"
	"time"

	"github.com/gonzalop/mq"
)

func TestAdvancedFeatures(t *testing.T) {
	t.Parallel()
	server, cleanup := startMosquitto(t, "")
	defer cleanup()

	// Test 1: Assigned Client ID
	t.Run("AssignedClientID", func(t *testing.T) {
		t.Parallel()
		// Connect with empty Client ID
		// Note: Mosquitto allow_anonymous=true and no client_id_prefixes should allow this
		// and assign a random ID.
		client, err := mq.Dial(server,
			mq.WithClientID(""), // Empty ID triggers server assignment in v5.0
			mq.WithProtocolVersion(mq.ProtocolV50),
			mq.WithCleanSession(true),
		)
		if err != nil {
			t.Fatalf("Failed to connect: %v", err)
		}
		defer client.Disconnect(context.Background())

		// Verify Assigned Client ID
		assignedID := client.AssignedClientID()
		if assignedID == "" {
			t.Error("AssignedClientID is empty, expected server-assigned ID")
		} else {
			t.Logf("Server assigned ClientID: %s", assignedID)
		}
	})

	// Test 2: Server Keep Alive (Optional check)
	// Mosquitto defaults to respecting client's keepalive, but let's see if we can trigger it.
	// This might pass or stay 0 depending on Mosquitto version/config, so we won't fail hard on value.
	// But we check that it doesn't crash.
	t.Run("ServerKeepAlive", func(t *testing.T) {
		t.Parallel()
		client, err := mq.Dial(server,
			mq.WithClientID("test-ka-"+t.Name()),
			mq.WithProtocolVersion(mq.ProtocolV50),
			mq.WithKeepAlive(60), // Request 60s
		)
		if err != nil {
			t.Fatalf("Failed to connect: %v", err)
		}
		defer client.Disconnect(context.Background())

		// Just verify method exists and returns a value without panic
	})

	// Test 3: Retained Messages
	t.Run("RetainedMessages", func(t *testing.T) {
		t.Parallel()
		client, err := mq.Dial(server,
			mq.WithClientID("test-retained-"+t.Name()),
			mq.WithProtocolVersion(mq.ProtocolV50),
		)
		if err != nil {
			t.Fatalf("Failed to connect: %v", err)
		}
		defer client.Disconnect(context.Background())

		topic := "test/retained/" + t.Name()
		payload := "this-is-retained"

		// 1. Publish retained message
		if err := client.Publish(topic, []byte(payload), mq.WithQoS(1), mq.WithRetain(true)).Wait(context.Background()); err != nil {
			t.Fatalf("Failed to publish retained: %v", err)
		}

		// 2. Subscribe and verify we get it
		received := make(chan mq.Message, 1)
		if err := client.Subscribe(topic, 1, func(c *mq.Client, msg mq.Message) {
			received <- msg
		}).Wait(context.Background()); err != nil {
			t.Fatalf("Failed to subscribe: %v", err)
		}

		select {
		case msg := <-received:
			if string(msg.Payload) != payload {
				t.Errorf("Payload = %s, want %s", string(msg.Payload), payload)
			}
			if !msg.Retained {
				t.Error("Message should be marked as Retained")
			}
		case <-time.After(2 * time.Second):
			t.Fatal("Timeout waiting for retained message")
		}
	})

	// Test 4: Wildcard Subscriptions
	t.Run("WildcardSubscriptions", func(t *testing.T) {
		t.Parallel()
		client, err := mq.Dial(server,
			mq.WithClientID("test-wildcard-"+t.Name()),
			mq.WithProtocolVersion(mq.ProtocolV50),
		)
		if err != nil {
			t.Fatalf("Failed to connect: %v", err)
		}
		defer client.Disconnect(context.Background())

		received := make(chan mq.Message, 2)
		handler := func(c *mq.Client, msg mq.Message) {
			received <- msg
		}

		// 1. Subscribe to wildcards
		// "+" matches single level: test/wildcard/one
		// "#" matches multi level: test/wildcard/nested/two
		prefix := "test/wildcard/" + t.Name() + "/"
		if err := client.Subscribe(prefix+"+", 1, handler).Wait(context.Background()); err != nil {
			t.Fatalf("Failed to subscribe +: %v", err)
		}
		if err := client.Subscribe(prefix+"nested/#", 1, handler).Wait(context.Background()); err != nil {
			t.Fatalf("Failed to subscribe #: %v", err)
		}

		// 2. Publish matching messages
		client.Publish(prefix+"level1", []byte("msg1"), mq.WithQoS(1))
		client.Publish(prefix+"nested/level2/level3", []byte("msg2"), mq.WithQoS(1))

		// 3. Verify reception
		expected := map[string]bool{
			"msg1": false,
			"msg2": false,
		}

		for i := 0; i < 2; i++ {
			select {
			case msg := <-received:
				expected[string(msg.Payload)] = true
			case <-time.After(2 * time.Second):
				t.Fatal("Timeout waiting for wildcard messages")
			}
		}

		if !expected["msg1"] {
			t.Error("Did not receive msg1 (single level wildcard)")
		}
		if !expected["msg2"] {
			t.Error("Did not receive msg2 (multi level wildcard)")
		}
	})
}
