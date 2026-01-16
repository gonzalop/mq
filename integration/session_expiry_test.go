package mq_test

import (
	"context"
	"testing"
	"time"

	"github.com/gonzalop/mq"
)

func TestSessionExpiry(t *testing.T) {
	t.Parallel()
	server, cleanup := startMosquitto(t, "")
	defer cleanup()

	// Test A: Session Persistence (Expiry > Disconnect duration)
	t.Run("SessionPersistence", func(t *testing.T) {
		t.Parallel()
		clientID := "test-session-persistence-" + t.Name()
		topic := "test/session/persistence/" + t.Name()

		// 1. Connect with SessionExpiryInterval = 5s
		client1, err := mq.Dial(server,
			mq.WithClientID(clientID),
			mq.WithProtocolVersion(mq.ProtocolV50),
			mq.WithCleanSession(false),
			mq.WithSessionExpiryInterval(5),
		)
		if err != nil {
			t.Fatalf("Failed to connect client1: %v", err)
		}

		// 2. Subscribe
		if err := client1.Subscribe(topic, 1, nil).Wait(context.Background()); err != nil {
			t.Fatalf("Failed to subscribe: %v", err)
		}

		// 3. Disconnect
		client1.Disconnect(context.Background())

		// 4. Publish message while client is offline
		pubClient, err := mq.Dial(server, mq.WithClientID("publisher"))
		if err != nil {
			t.Fatalf("Failed to connect publisher: %v", err)
		}
		defer pubClient.Disconnect(context.Background())

		if err := pubClient.Publish(topic, []byte("persistent-msg"), mq.WithQoS(1)).Wait(context.Background()); err != nil {
			t.Fatalf("Failed to publish: %v", err)
		}

		// 5. Reconnect immediately (within 5s window)
		received := make(chan mq.Message, 1)
		client2, err := mq.Dial(server,
			mq.WithClientID(clientID),
			mq.WithProtocolVersion(mq.ProtocolV50),
			mq.WithCleanSession(false),
			mq.WithSessionExpiryInterval(5),
			mq.WithSubscription(topic, func(c *mq.Client, msg mq.Message) {
				received <- msg
			}),
		)
		if err != nil {
			t.Fatalf("Failed to reconnect: %v", err)
		}
		defer client2.Disconnect(context.Background())

		// 6. Verify Session Present
		// Note: The library doesn't expose SessionPresent flag directly in Client struct yet,
		// but receiving the message proves the session (and subscription) persisted.

		// 7. Verify message received
		select {
		case msg := <-received:
			if string(msg.Payload) != "persistent-msg" {
				t.Errorf("Payload = %s, want persistent-msg", string(msg.Payload))
			}
		case <-time.After(5 * time.Second):
			t.Fatal("Timeout waiting for persistent message")
		}
	})

	// Test B: Session Expiration (Expiry < Disconnect duration)
	t.Run("SessionExpiration", func(t *testing.T) {
		t.Parallel()
		clientID := "test-session-expiry-" + t.Name()
		topic := "test/session/expire/" + t.Name()

		// 1. Connect with SessionExpiryInterval = 2s
		client1, err := mq.Dial(server,
			mq.WithClientID(clientID),
			mq.WithProtocolVersion(mq.ProtocolV50),
			mq.WithCleanSession(false),
			mq.WithSessionExpiryInterval(2),
		)
		if err != nil {
			t.Fatalf("Failed to connect client1: %v", err)
		}

		// 2. Subscribe
		if err := client1.Subscribe(topic, 1, nil).Wait(context.Background()); err != nil {
			t.Fatalf("Failed to subscribe: %v", err)
		}

		// 3. Disconnect
		client1.Disconnect(context.Background())

		// 4. Wait for session to expire (wait > 2s)
		time.Sleep(4 * time.Second)

		// 5. Publish message (should be dropped by server as session is gone)
		pubClient, err := mq.Dial(server, mq.WithClientID("publisher-2"))
		if err != nil {
			t.Fatalf("Failed to connect publisher: %v", err)
		}
		defer pubClient.Disconnect(context.Background())

		if err := pubClient.Publish(topic, []byte("expired-msg"), mq.WithQoS(1)).Wait(context.Background()); err != nil {
			t.Fatalf("Failed to publish: %v", err)
		}

		// 6. Reconnect
		received := make(chan mq.Message, 1)
		client2, err := mq.Dial(server,
			mq.WithClientID(clientID),
			mq.WithProtocolVersion(mq.ProtocolV50),
			mq.WithCleanSession(false),
			mq.WithSessionExpiryInterval(2),
			mq.WithSubscription(topic, func(c *mq.Client, msg mq.Message) {
				received <- msg
			}),
		)
		if err != nil {
			t.Fatalf("Failed to reconnect: %v", err)
		}
		defer client2.Disconnect(context.Background())

		// 7. Verify message NOT received
		select {
		case msg := <-received:
			t.Errorf("Received message for expired session: %s", string(msg.Payload))
		case <-time.After(2 * time.Second):
			// Success - no message received
		}
	})

	// Test C: Orphaned Subscription (Fallback to Default Handler)
	t.Run("OrphanedSubscription", func(t *testing.T) {
		t.Parallel()
		clientID := "test-orphan-sub-" + t.Name()
		topic := "test/orphan/" + t.Name()

		// 1. Connect and create a persistent subscription MANUALLY (not via WithSubscription)
		client1, err := mq.Dial(server,
			mq.WithClientID(clientID),
			mq.WithCleanSession(false),
			mq.WithSessionExpiryInterval(60),
		)
		if err != nil {
			t.Fatalf("Failed to connect client1: %v", err)
		}

		if err := client1.Subscribe(topic, 1, nil).Wait(context.Background()); err != nil {
			t.Fatalf("Failed to subscribe: %v", err)
		}
		client1.Disconnect(context.Background())

		// 2. Publish message while offline
		pubClient, err := mq.Dial(server, mq.WithClientID("publisher-orphan"))
		if err != nil {
			t.Fatalf("Failed to connect publisher: %v", err)
		}
		defer pubClient.Disconnect(context.Background())

		if err := pubClient.Publish(topic, []byte("catch-me"), mq.WithQoS(1)).Wait(context.Background()); err != nil {
			t.Fatalf("Failed to publish: %v", err)
		}

		// 3. Reconnect WITHOUT WithSubscription, but WITH DefaultPublishHandler
		received := make(chan mq.Message, 1)
		client2, err := mq.Dial(server,
			mq.WithClientID(clientID),
			mq.WithCleanSession(false),
			mq.WithSessionExpiryInterval(60),
			mq.WithDefaultPublishHandler(func(c *mq.Client, msg mq.Message) {
				received <- msg
			}),
		)
		if err != nil {
			t.Fatalf("Failed to reconnect: %v", err)
		}
		defer client2.Disconnect(context.Background())

		// 4. Verify message falls back to default handler
		select {
		case msg := <-received:
			if string(msg.Payload) != "catch-me" {
				t.Errorf("Payload = %s, want catch-me", string(msg.Payload))
			}
		case <-time.After(5 * time.Second):
			t.Fatal("Timeout waiting for orphaned message")
		}
	})
}
