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
		mq.WithAutoReconnect(true))
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer client.Disconnect(context.Background())

	received := make(chan mq.Message, 10)
	var mu sync.Mutex
	var msgCount int

	token := client.Subscribe("test/reconnect", mq.AtLeastOnce, func(c *mq.Client, msg mq.Message) {
		mu.Lock()
		msgCount++
		mu.Unlock()
		received <- msg
	})
	if err := token.Wait(context.Background()); err != nil {
		t.Fatalf("Failed to subscribe: %v", err)
	}

	pubToken := client.Publish("test/reconnect", []byte("before disconnect"), mq.WithQoS(mq.AtLeastOnce))
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
	time.Sleep(5 * time.Second)

	// Publish a message from a different client to test if subscription was restored
	publisher, err := mq.Dial(server, mq.WithClientID("test-publisher"))
	if err != nil {
		t.Fatalf("Failed to connect publisher: %v", err)
	}
	defer publisher.Disconnect(context.Background())

	pubToken2 := publisher.Publish("test/reconnect", []byte("after reconnect"), mq.WithQoS(mq.AtLeastOnce))
	if err := pubToken2.Wait(context.Background()); err != nil {
		t.Fatalf("Failed to publish after reconnect: %v", err)
	}

	select {
	case msg := <-received:
		if string(msg.Payload) != "after reconnect" {
			t.Errorf("Expected 'after reconnect', got %s", string(msg.Payload))
		}
		t.Log("✅ Auto-reconnect and resubscribe successful!")
	case <-time.After(5 * time.Second):
		t.Fatal("Timeout waiting for message after reconnect - auto-reconnect or resubscribe failed")
	}

	mu.Lock()
	if msgCount != 2 {
		t.Errorf("Expected 2 messages, got %d", msgCount)
	}
	mu.Unlock()
}
