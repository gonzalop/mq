package mq_test

import (
	"context"
	"errors"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/gonzalop/mq"
	"github.com/gonzalop/mq/internal/packets"
)

// TestDisconnectReasonPropagation verifies that the OnConnectionLost callback
// receives the specific reason code sent by the server in a DISCONNECT packet.
func TestDisconnectReasonPropagation(t *testing.T) {
	// 1. Setup a mock server
	listener, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	defer listener.Close()

	// Server goroutine
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		// Read CONNECT
		_, _ = packets.ReadPacket(conn, 5, 0)

		// Send CONNACK (Success)
		connack := &packets.ConnackPacket{
			ReturnCode: packets.ConnAccepted,
			Properties: &packets.Properties{},
		}
		_, _ = conn.Write(encodeToBytes(connack))

		// Wait briefly then send DISCONNECT with a specific reason
		time.Sleep(100 * time.Millisecond)

		disconnect := &packets.DisconnectPacket{
			Version:    5,
			ReasonCode: uint8(mq.ReasonCodeSessionTakenOver),
			Properties: &packets.Properties{
				ReasonString: "You are being replaced",
				Presence:     packets.PresReasonString,
			},
		}
		_, _ = conn.Write(encodeToBytes(disconnect))

		// Close connection after sending
		time.Sleep(50 * time.Millisecond)
		conn.Close()
	}()

	// 2. Connect Client
	var disconnectErr error
	var wg sync.WaitGroup
	wg.Add(1)

	client, err := mq.Dial(
		"tcp://"+listener.Addr().String(),
		mq.WithClientID("test-client"),
		mq.WithProtocolVersion(mq.ProtocolV50),
		mq.WithAutoReconnect(false), // Disable auto-reconnect to make checking error stable
		mq.WithOnConnectionLost(func(c *mq.Client, err error) {
			disconnectErr = err
			wg.Done()
		}),
	)
	if err != nil {
		t.Fatalf("failed to dial: %v", err)
	}
	defer func() {
		_ = client.Disconnect(context.Background())
	}()

	// 3. Wait for disconnect
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Success
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for OnConnectionLost")
	}

	// 4. Verify Error
	if disconnectErr == nil {
		t.Fatal("expected error in OnConnectionLost, got nil")
	}

	// Check if it's an MqttError with the right code
	if !errors.Is(disconnectErr, mq.ReasonCodeSessionTakenOver) {
		t.Errorf("expected ReasonCodeSessionTakenOver (0x8E), got error: %v", disconnectErr)
	}

	// Check if message is preserved
	var mErr *mq.MqttError
	if errors.As(disconnectErr, &mErr) {
		if mErr.Message != "You are being replaced" {
			t.Errorf("expected reason string 'You are being replaced', got '%s'", mErr.Message)
		}
	} else {
		t.Fatalf("expected *MqttError, got %T", disconnectErr)
	}
}
