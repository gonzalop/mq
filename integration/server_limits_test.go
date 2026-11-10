package mq_test

import (
	"context"
	"testing"
	"time"

	"github.com/gonzalop/mq"
)

// TestServerLimits verifies that the client respects server-imposed limits.
func TestServerLimits(t *testing.T) {
	t.Parallel()
	// Configure Mosquitto with strict limits.
	// We don't specify listener here, startMosquitto will add it (port 1884).
	config := `
allow_anonymous true
max_packet_size 100
max_inflight_messages 2
`
	server, cleanup := startMosquitto(t, config)
	t.Cleanup(cleanup)

	// Test A: Maximum Packet Size
	t.Run("MaximumPacketSize", func(t *testing.T) {
		t.Parallel()
		client, err := mq.Dial(server,
			mq.WithClientID("test-limits-packet-"+t.Name()),
			mq.WithProtocolVersion(mq.ProtocolV50),
		)
		if err != nil {
			t.Fatalf("Failed to connect: %v", err)
		}
		defer client.Disconnect(context.Background())

		// Verify client learned the limit
		caps := client.ServerCapabilities()
		if caps.MaximumPacketSize != 100 {
			t.Errorf("Expected MaxPacketSize 100, got %d", caps.MaximumPacketSize)
		}

		// Try to publish a payload that exceeds the limit
		// Packet overhead is ~15-20 bytes, so 150 byte payload definitely exceeds 100
		payload := make([]byte, 150)
		token := client.Publish(context.Background(), "test/large", payload, mq.WithQoS(1))

		// Should fail immediately (client-side enforcement)
		if err := token.Wait(context.Background()); err == nil {
			t.Error("Expected error for large packet, got nil")
		} else {
			t.Logf("Got expected error: %v", err)
		}
	})

	// Test B: Receive Maximum (Flow Control)
	t.Run("ReceiveMaximum", func(t *testing.T) {
		t.Parallel()
		// The proxy lets the test see what is on the wire while it keeps the
		// broker from acknowledging anything.
		proxy := newTapProxy(t, server, mq.ProtocolV50)
		client, err := mq.Dial(proxy.server(),
			mq.WithClientID("test-limits-flow-"+t.Name()),
			mq.WithProtocolVersion(mq.ProtocolV50),
		)
		if err != nil {
			t.Fatalf("Failed to connect: %v", err)
		}
		defer client.Disconnect(context.Background())

		// Verify client learned the limit
		caps := client.ServerCapabilities()
		if caps.ReceiveMaximum != 2 {
			t.Errorf("Expected ReceiveMaximum 2, got %d", caps.ReceiveMaximum)
		}

		// We will receive acknowledgments here
		// We don't really need to subscribe, just publish QoS 1

		const numMsgs = 5
		done := make(chan error, numMsgs)

		// Publish messages rapidly in parallel
		// The client should throttle sending to respect ReceiveMaximum=2
		start := time.Now()
		proxy.withhold()

		for i := 0; i < numMsgs; i++ {
			go func(_ int) {
				token := client.Publish(context.Background(), "test/flow", []byte("data"), mq.WithQoS(1))
				err := token.Wait(context.Background())
				done <- err
			}(i)
		}

		// With no PUBACK coming back, exactly 2 publishes may be on the wire.
		deadline := time.Now().Add(5 * time.Second)
		for proxy.qosPublishes.Load() < 2 {
			if time.Now().After(deadline) {
				proxy.release()
				t.Fatalf("only %d publishes reached the wire, want 2", proxy.qosPublishes.Load())
			}
			time.Sleep(10 * time.Millisecond)
		}
		time.Sleep(300 * time.Millisecond) // time for a client that ignores the limit to send more
		inFlight := proxy.qosPublishes.Load()
		proxy.release()
		if inFlight != 2 {
			t.Errorf("%d unacknowledged publishes on the wire, want 2 (ReceiveMaximum)", inFlight)
		}

		// Wait for all to complete
		for i := 0; i < numMsgs; i++ {
			select {
			case err := <-done:
				if err != nil {
					t.Errorf("Publish failed: %v", err)
				}
			case <-time.After(5 * time.Second):
				t.Fatalf("Timeout waiting for publish %d", i)
			}
		}

		elapsed := time.Since(start)
		t.Logf("Published %d messages in %v", numMsgs, elapsed)

	})
}
