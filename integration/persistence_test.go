package mq_test

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gonzalop/mq"
)

func TestPersistenceIntegration(t *testing.T) {
	t.Parallel()

	// 1. Session Resumed Case (Happy Path)
	// Server keeps state, Client keeps state.
	t.Run("ServerSessionResumed", func(t *testing.T) {
		t.Parallel()
		// Use dedicated server to avoid interference during parallel tests
		server, cleanup := startMosquitto(t, "# Dedicated for resumed test")
		defer cleanup()

		// Setup temp dir for store
		tmpDir, err := os.MkdirTemp("", "mq-test-resumed-*")
		if err != nil {
			t.Fatal(err)
		}
		defer os.RemoveAll(tmpDir)

		clientID := "client-resumed-" + strings.ReplaceAll(t.Name(), "/", "_")
		topic := "test/persistence/resumed/" + t.Name()

		// A. Initial Connection & Subscription
		store1, err := mq.NewFileStore(tmpDir, clientID)
		if err != nil {
			t.Fatalf("NewFileStore 1 failed: %v", err)
		}
		client1, err := mq.Dial(server,
			mq.WithClientID(clientID),
			mq.WithCleanSession(false),
			mq.WithSessionStore(store1),
			mq.WithSessionExpiryInterval(60), // Keep session on server
		)
		if err != nil {
			t.Fatalf("Connect 1 failed: %v", err)
		}

		// Subscribe with persistence enabled (default)
		if err := client1.Subscribe(topic, 1, nil).Wait(context.Background()); err != nil {
			t.Fatalf("Subscribe failed: %v", err)
		}

		// Disconnect
		client1.Disconnect(context.Background())

		// B. Publish while offline
		pubClient, _ := mq.Dial(server, mq.WithClientID("publisher-1-"+t.Name()))
		if err := pubClient.Publish(topic, []byte("msg-1"), mq.WithQoS(1)).Wait(context.Background()); err != nil {
			t.Fatalf("Publish failed: %v", err)
		}
		pubClient.Disconnect(context.Background())

		// C. Client Restart (New instance, same store/ID)
		received := make(chan mq.Message, 1)
		store2, err := mq.NewFileStore(tmpDir, clientID) // Load from disk
		if err != nil {
			t.Fatalf("NewFileStore 2 failed: %v", err)
		}
		client2, err := mq.Dial(server,
			mq.WithClientID(clientID),
			mq.WithCleanSession(false),
			mq.WithSessionStore(store2),
			mq.WithSessionExpiryInterval(60),
			mq.WithDefaultPublishHandler(func(c *mq.Client, msg mq.Message) {
				received <- msg
			}),
		)
		if err != nil {
			t.Fatalf("Connect 2 failed: %v", err)
		}
		defer client2.Disconnect(context.Background())

		// Verify message reception
		select {
		case msg := <-received:
			if string(msg.Payload) != "msg-1" {
				t.Errorf("Want msg-1, got %s", string(msg.Payload))
			}
		case <-time.After(5 * time.Second):
			t.Fatal("Timeout waiting for message in Resumed session")
		}
	})

	// 2. Server Session Lost Case (Client must resubscribe from disk)
	// Server restarts (loses state), Client restarts (loads state from disk) -> Client MUST re-send SUBSCRIBE
	t.Run("ServerSessionLost", func(t *testing.T) {
		t.Parallel()
		// Start server (Isolated)
		server, cleanup := startMosquitto(t, "persistence false\n# Dedicated for lost test")
		// We will NOT defer cleanup here because we want to kill it manually to simulate loss

		// Extract port to reuse on restart
		parts := strings.Split(server, ":")
		port := parts[len(parts)-1]

		// Setup temp dir for store
		tmpDir, err := os.MkdirTemp("", "mq-test-lost-*")
		if err != nil {
			cleanup()
			t.Fatal(err)
		}
		defer os.RemoveAll(tmpDir)

		clientID := "client-lost-" + strings.ReplaceAll(t.Name(), "/", "_")
		topic := "test/persistence/lost/" + t.Name()

		// A. Initial Connection & Subscription
		store1, err := mq.NewFileStore(tmpDir, clientID)
		if err != nil {
			t.Fatalf("NewFileStore 1 failed: %v", err)
		}
		client1, err := mq.Dial(server,
			mq.WithClientID(clientID),
			mq.WithCleanSession(false),
			mq.WithSessionStore(store1),
		)
		if err != nil {
			cleanup()
			t.Fatalf("Connect 1 failed: %v", err)
		}

		// Subscribe
		if err := client1.Subscribe(topic, 1, nil).Wait(context.Background()); err != nil {
			cleanup()
			t.Fatalf("Subscribe failed: %v", err)
		}

		client1.Disconnect(context.Background())

		// B. Restart Server (Simulate Crash/Loss of State)
		cleanup() // Kills the container

		server2, cleanup2 := startMosquitto(t, "persistence false\n# Dedicated for lost test restart", port)
		defer cleanup2()

		// C. Client Restart (New instance, same store) connecting to "New" server (empty state)
		received := make(chan mq.Message, 1)
		store2, err := mq.NewFileStore(tmpDir, clientID)
		if err != nil {
			t.Fatalf("NewFileStore 2 failed: %v", err)
		}

		client2, err := mq.Dial(server2, // Connect to the new server instance
			mq.WithClientID(clientID),
			mq.WithCleanSession(false),
			mq.WithSessionStore(store2), // Has subscription on disk
			mq.WithDefaultPublishHandler(func(c *mq.Client, msg mq.Message) {
				received <- msg
			}),
		)
		if err != nil {
			t.Fatalf("Connect 2 failed: %v", err)
		}
		defer client2.Disconnect(context.Background())

		// D. Verify Subscription Restoration
		// Since server was fresh, it knew nothing. Client should have detected SessionPresent: false
		// and re-sent SUBSCRIBE for 'topic' loaded from disk.

		// Publish from another connection
		pubClient, _ := mq.Dial(server2, mq.WithClientID("publisher-2-"+t.Name()))
		if err := pubClient.Publish(topic, []byte("msg-2"), mq.WithQoS(1)).Wait(context.Background()); err != nil {
			t.Fatalf("Publish failed: %v", err)
		}
		pubClient.Disconnect(context.Background())

		// Verify message reception
		select {
		case msg := <-received:
			if string(msg.Payload) != "msg-2" {
				t.Errorf("Want msg-2, got %s", string(msg.Payload))
			}
		case <-time.After(5 * time.Second):
			t.Fatal("Timeout waiting for message in Lost session (Resubscribe failed?)")
		}
	})

	// 3. Ephemeral Subscription Persistence Test (Integration)
	// Verify that WithPersistence(false) is NOT saved to disk and NOT restored.
	t.Run("EphemeralSubscription", func(t *testing.T) {
		t.Parallel()
		// Force fresh container to ensure no server-side state retention
		server, cleanup := startMosquitto(t, "persistence false\n# Dedicated for ephemeral test")
		defer cleanup()

		// Extract port to reuse on restart
		parts := strings.Split(server, ":")
		port := parts[len(parts)-1]

		tmpDir, err := os.MkdirTemp("", "mq-test-ephemeral-*")
		if err != nil {
			t.Fatal(err)
		}
		defer os.RemoveAll(tmpDir)

		clientID := "client-ephemeral-" + strings.ReplaceAll(t.Name(), "/", "_")
		topic := "test/ephemeral/" + t.Name()

		// A. Subscribe Ephemerally
		store1, err := mq.NewFileStore(tmpDir, clientID)
		if err != nil {
			t.Fatalf("NewFileStore 1 failed: %v", err)
		}
		client1, err := mq.Dial(server,
			mq.WithClientID(clientID),
			mq.WithCleanSession(false),
			mq.WithSessionStore(store1),
		)
		if err != nil {
			t.Fatalf("Connect 1 failed: %v", err)
		}

		// USE WithPersistence(false)
		if err := client1.Subscribe(topic, 1, nil, mq.WithPersistence(false)).Wait(context.Background()); err != nil {
			t.Fatalf("Subscribe failed: %v", err)
		}

		client1.Disconnect(context.Background())

		cleanup() // Kill server 1
		server2, cleanup2 := startMosquitto(t, "persistence false\n# Dedicated for ephemeral test restart", port)
		defer cleanup2()

		// B. Client Restart
		received := make(chan mq.Message, 1)
		store2, err := mq.NewFileStore(tmpDir, clientID)
		if err != nil {
			t.Fatalf("NewFileStore 2 failed: %v", err)
		}
		client2, err := mq.Dial(server2,
			mq.WithClientID(clientID),
			mq.WithCleanSession(false),
			mq.WithSessionStore(store2),
			mq.WithDefaultPublishHandler(func(c *mq.Client, msg mq.Message) {
				received <- msg
			}),
		)
		if err != nil {
			t.Fatalf("Connect 2 failed: %v", err)
		}
		defer client2.Disconnect(context.Background())

		// C. Publish and verify NOT received
		pubClient, _ := mq.Dial(server2, mq.WithClientID("publisher-3-"+t.Name()))
		pubClient.Publish(topic, []byte("should-not-receive"), mq.WithQoS(1)).Wait(context.Background())
		pubClient.Disconnect(context.Background())

		select {
		case <-received:
			t.Fatal("Received message for ephemeral subscription after restart!")
		case <-time.After(1 * time.Second):
			// Success
		}
	})
}
