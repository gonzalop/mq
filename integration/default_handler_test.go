package mq_test

import (
	"context"
	"testing"
	"time"

	"github.com/gonzalop/mq"
)

// TestDefaultPublishHandlerIntegration verifies that the DefaultPublishHandler
// correctly catches messages that do not match any active subscription.
// This simulates a scenario where a client reconnects (resuming session) but hasn't
// yet re-registered its subscription handlers, receiving queued offline messages.
func TestDefaultPublishHandlerIntegration(t *testing.T) {
	t.Parallel()
	server, cleanup := startMosquitto(t, "")
	defer cleanup()

	const (
		persistentTopic = "persistent/topic/data"
		offlinePayload  = "offline-data-123"
		clientID        = "persistent-client-1"
	)

	// 1. Initial Connect & Subscribe (Client A)
	// CleanSession=false to ensure server queues messages while we are offline
	clientA, err := mq.Dial(server,
		mq.WithClientID(clientID),
		mq.WithCleanSession(false),               // Persistent session
		mq.WithSessionExpiryInterval(0xFFFFFFFF), // Required for v5 persistence
	)
	if err != nil {
		t.Fatalf("Client A failed to connect: %v", err)
	}

	subscribeDone := make(chan struct{})
	token := clientA.Subscribe(persistentTopic, 1, func(c *mq.Client, msg mq.Message) {
		// Handler logic not relevant for this test, but must be present initially
		close(subscribeDone)
	})
	if err := token.Wait(context.Background()); err != nil {
		t.Fatalf("Client A failed to subscribe: %v", err)
	}

	// 2. Disconnect Client A
	// Allow some time for sync
	time.Sleep(100 * time.Millisecond)
	clientA.Disconnect(context.Background())
	// Ensure it's fully closed
	time.Sleep(100 * time.Millisecond)

	// 3. Publish Offline Message (Client B)
	clientB, err := mq.Dial(server, mq.WithClientID("client-b"))
	if err != nil {
		t.Fatalf("Client B failed to connect: %v", err)
	}
	defer clientB.Disconnect(context.Background())

	pToken := clientB.Publish(persistentTopic, []byte(offlinePayload), mq.WithQoS(1))
	if err := pToken.Wait(context.Background()); err != nil {
		t.Fatalf("Client B failed to publish: %v", err)
	}

	// 4. Reconnect Client A *WITHOUT* re-subscribing
	// The server should deliver the queued message and WithDefaultPublishHandler should
	// catch it.
	received := make(chan mq.Message, 1)

	defaultHandler := func(c *mq.Client, msg mq.Message) {
		received <- msg
	}

	// Reconnect
	clientA2, err := mq.Dial(server,
		mq.WithClientID(clientID),
		mq.WithCleanSession(false), // Resume session
		mq.WithSessionExpiryInterval(0xFFFFFFFF),
		mq.WithDefaultPublishHandler(defaultHandler),
	)
	if err != nil {
		t.Fatalf("Client A2 failed to reconnect: %v", err)
	}
	defer clientA2.Disconnect(context.Background())

	// 5. Verify receipt via Default Handler
	select {
	case msg := <-received:
		if string(msg.Payload) != offlinePayload {
			t.Errorf("Payload = %s, want %s", string(msg.Payload), offlinePayload)
		}
		if msg.Topic != persistentTopic {
			t.Errorf("Topic = %s, want %s", msg.Topic, persistentTopic)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Timeout: DefaultPublishHandler was not called with offline message")
	}
}
