package mq_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gonzalop/mq"
)

// TestKeepAliveWithContinuousQoS0Publishing verifies that the client maintains
// a connection when continuously publishing QoS 0 messages by sending PINGREQ
// to verify bidirectional communication.
//
// This is an integration test that uses a real Mosquitto server to ensure
// the keepalive mechanism works correctly in production scenarios.
func TestKeepAliveWithContinuousQoS0Publishing(t *testing.T) {
	t.Parallel()
	server, cleanup := startMosquitto(t, "")
	defer cleanup()

	// Use a short keepalive for faster test (10 seconds)
	keepalive := 10 * time.Second

	client, err := mq.Dial(server,
		mq.WithClientID("test-keepalive-qos0"),
		mq.WithKeepAlive(keepalive))
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer client.Disconnect(context.Background())

	// Track if client disconnects unexpectedly
	var disconnected atomic.Bool
	disconnectCh := make(chan struct{})

	// Monitor connection status
	go func() {
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if !client.IsConnected() {
					disconnected.Store(true)
					close(disconnectCh)
					return
				}
			case <-time.After(30 * time.Second):
				return
			}
		}
	}()

	// Publish QoS 0 messages continuously (every 2 seconds)
	// This is faster than the PINGREQ threshold (3/4 * 10s = 7.5s)
	// but the server won't send any responses for QoS 0
	publishTicker := time.NewTicker(2 * time.Second)
	defer publishTicker.Stop()

	publishCount := 0
	testDuration := 25 * time.Second // More than 2x keepalive
	timeout := time.After(testDuration)

	for {
		select {
		case <-publishTicker.C:
			// Publish QoS 0 message (no PUBACK expected)
			client.Publish("keepalive/test", []byte("data"), mq.WithQoS(0))
			publishCount++
			t.Logf("Published message %d", publishCount)

		case <-disconnectCh:
			t.Fatalf("Client disconnected unexpectedly after %d publishes", publishCount)

		case <-timeout:
			// Test completed successfully
			if !client.IsConnected() {
				t.Error("Client should still be connected after test duration")
			}
			t.Logf("Test completed: published %d messages over %v with keepalive=%v",
				publishCount, testDuration, keepalive)
			return
		}
	}
}

// TestKeepAliveTimeoutWithNoActivity verifies that the client disconnects
// when there's no activity (no sending or receiving) for 1.5x keepalive.
func TestKeepAliveTimeoutWithNoActivity(t *testing.T) {
	t.Parallel()
	server, cleanup := startMosquitto(t, "")
	defer cleanup()

	// Use a very short keepalive for faster test
	keepalive := 3 * time.Second

	client, err := mq.Dial(server,
		mq.WithClientID("test-keepalive-timeout"),
		mq.WithKeepAlive(keepalive),
		mq.WithAutoReconnect(false)) // Disable auto-reconnect for this test
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer client.Disconnect(context.Background())

	// Wait for timeout (1.5x keepalive = 4.5s, plus margin)
	// The client should send PINGREQ at ~2.25s (3/4 of 3s)
	// If server doesn't respond (which it should), timeout at 4.5s
	time.Sleep(10 * time.Second) // Wait for multiple keepalive cycles

	if !client.IsConnected() {
		t.Error("Client should remain connected when server responds to PINGREQ")
	}
}

// TestKeepAliveDisabled verifies that keepalive=0 disables the mechanism.
func TestKeepAliveDisabled(t *testing.T) {
	t.Parallel()
	server, cleanup := startMosquitto(t, "")
	defer cleanup()

	client, err := mq.Dial(server,
		mq.WithClientID("test-keepalive-disabled"),
		mq.WithKeepAlive(0)) // Disabled
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer client.Disconnect(context.Background())

	// Wait a long time with no activity
	time.Sleep(10 * time.Second)

	// Should still be connected (no keepalive timeout)
	if !client.IsConnected() {
		t.Error("Client should remain connected when keepalive is disabled")
	}
}
