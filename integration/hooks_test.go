package mq_test

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gonzalop/mq"
)

// TestLifecycleHooks verify that OnConnect and OnConnectionLost
// hooks are called at the appropriate times.
func TestLifecycleHooks(t *testing.T) {
	t.Parallel()
	// Use dedicated server because we stop/start it
	server, cleanup := startMosquitto(t, "# Dedicated for hooks test")
	defer cleanup()

	// Extract port to reuse on restart
	parts := strings.Split(server, ":")
	port := parts[len(parts)-1]

	connectCh := make(chan struct{}, 10)
	disconnectCh := make(chan error, 10)

	var mu sync.Mutex
	var connectCount int

	client, err := mq.Dial(server,
		mq.WithClientID("test-hooks"),
		mq.WithAutoReconnect(true),
		mq.WithOnConnect(func(c *mq.Client) {
			mu.Lock()
			connectCount++
			count := connectCount
			mu.Unlock()
			t.Logf("OnConnect hook called (count=%d)", count)
			connectCh <- struct{}{}
		}),
		mq.WithOnConnectionLost(func(c *mq.Client, err error) {
			t.Logf("OnConnectionLost hook called: %v", err)
			disconnectCh <- err
		}),
	)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer client.Disconnect(context.Background())

	select {
	case <-connectCh:
		// Success
	case <-time.After(5 * time.Second):
		t.Fatal("Timeout waiting for initial OnConnect")
	}

	if !client.IsConnected() {
		t.Error("Expected IsConnected() to be true")
	}

	// Force disconnect
	t.Log("Stopping Mosquitto...")
	cleanup()

	select {
	case err := <-disconnectCh:
		if err == nil {
			t.Error("Expected error in OnConnectionLost, got nil")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Timeout waiting for OnConnectionLost")
	}

	if client.IsConnected() {
		t.Error("Expected IsConnected() to be false after connection lost")
	}

	t.Log("Restarting Mosquitto...")

	// Drain
loop:
	for {
		select {
		case <-connectCh:
		case <-disconnectCh:
		default:
			break loop
		}
	}

	server, cleanup = startMosquitto(t, "# Dedicated for hooks test restart", port)
	defer cleanup()

	select {
	case <-connectCh:
		// Success
	case <-time.After(20 * time.Second): // Increase timeout for backoff + reconnect
		t.Fatal("Timeout waiting for OnConnect after reconnection")
	}

	if !client.IsConnected() {
		t.Error("Expected IsConnected() to be true after reconnect")
	}

	t.Log("✅ Lifecycle hooks verified!")
}
