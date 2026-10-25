package mq_test

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gonzalop/mq"
)

// TestAutoReconnect verifies that the client automatically reconnects
// and resubscribes when the connection is lost.
func TestAutoReconnect(t *testing.T) {
	t.Parallel()
	// Use dedicated server because we stop/start it
	server, cleanup := startMosquitto(t, "# Dedicated for reconnect test")
	defer cleanup()

	// Extract port to reuse on restart
	parts := strings.Split(server, ":")
	port := parts[len(parts)-1]

	client, err := mq.Dial(server,
		mq.WithClientID("test-reconnect"),
		mq.WithAutoReconnect(true),
		mq.WithReconnectBackoff(50*time.Millisecond, 1*time.Second, false))
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer client.Disconnect(context.Background())

	received := make(chan mq.Message, 10)
	var mu sync.Mutex
	var msgCount int

	token := client.Subscribe(context.Background(), "test/reconnect", mq.AtLeastOnce, func(_ *mq.Client, msg mq.Message) {
		mu.Lock()
		msgCount++
		mu.Unlock()
		received <- msg
	})
	if err := token.Wait(context.Background()); err != nil {
		t.Fatalf("Failed to subscribe: %v", err)
	}

	pubToken := client.Publish(context.Background(), "test/reconnect", []byte("before disconnect"), mq.WithQoS(mq.AtLeastOnce))
	if err := pubToken.Wait(context.Background()); err != nil {
		t.Fatalf("Failed to publish: %v", err)
	}

	select {
	case msg := <-received:
		if string(msg.Payload) != "before disconnect" {
			t.Errorf("Expected 'before disconnect', got %s", string(msg.Payload))
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Timeout waiting for first message")
	}

	// Force disconnect by stopping and restarting Mosquitto
	t.Log("Stopping Mosquitto to simulate connection loss...")
	cleanup()

	time.Sleep(2 * time.Second)

	// Restart Mosquitto on SAME PORT
	t.Log("Restarting Mosquitto...")
	server, cleanup = startMosquitto(t, "# Dedicated for reconnect test restart", port)
	defer cleanup()

	t.Log("Waiting for auto-reconnect...")
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if client.IsConnected() {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !client.IsConnected() {
		t.Fatal("Timeout waiting for auto-reconnect")
	}

	// Publish from a different client to test if the subscription was
	// restored. Resubscription completes in the background with no public
	// signal, so retry until a message gets through.
	publisher, err := mq.Dial(server, mq.WithClientID("test-publisher"))
	if err != nil {
		t.Fatalf("Failed to connect publisher: %v", err)
	}
	defer publisher.Disconnect(context.Background())

	delivered := false
	for deadline := time.Now().Add(10 * time.Second); !delivered && time.Now().Before(deadline); {
		pubToken2 := publisher.Publish(context.Background(), "test/reconnect", []byte("after reconnect"), mq.WithQoS(mq.AtLeastOnce))
		if err := pubToken2.Wait(context.Background()); err != nil {
			t.Fatalf("Failed to publish after reconnect: %v", err)
		}
		select {
		case msg := <-received:
			if string(msg.Payload) != "after reconnect" {
				t.Errorf("Expected 'after reconnect', got %s", string(msg.Payload))
			}
			delivered = true
		case <-time.After(500 * time.Millisecond):
		}
	}
	if !delivered {
		t.Fatal("Timeout waiting for message after reconnect - auto-reconnect or resubscribe failed")
	}
	t.Log("✅ Auto-reconnect and resubscribe successful!")

	// An earlier attempt delivered after its 500ms window would add an extra
	// message, so only a lower bound is meaningful here.
	mu.Lock()
	if msgCount < 2 {
		t.Errorf("Expected at least 2 messages, got %d", msgCount)
	}
	mu.Unlock()
}
