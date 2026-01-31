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

	// Check if it's a DisconnectError with the right code
	var dErr *mq.DisconnectError
	if errors.As(disconnectErr, &dErr) {
		if dErr.ReasonCode != mq.ReasonCodeSessionTakenOver {
			t.Errorf("expected ReasonCodeSessionTakenOver (0x8E), got code: 0x%02x", dErr.ReasonCode)
		}
		if dErr.ReasonString != "You are being replaced" {
			t.Errorf("expected reason string 'You are being replaced', got '%s'", dErr.ReasonString)
		}
	} else {
		t.Fatalf("expected *DisconnectError, got %T: %v", disconnectErr, disconnectErr)
	}
}

// TestDisconnectExtraProperties verifies that all DISCONNECT properties are propagated.
func TestDisconnectExtraProperties(t *testing.T) {
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

		// Send CONNACK
		connack := &packets.ConnackPacket{
			ReturnCode: packets.ConnAccepted,
			Properties: &packets.Properties{},
		}
		_, _ = conn.Write(encodeToBytes(connack))

		// Wait briefly then send DISCONNECT with ALL properties
		time.Sleep(100 * time.Millisecond)

		disconnect := &packets.DisconnectPacket{
			Version:    5,
			ReasonCode: uint8(mq.ReasonCodeServerMoved),
			Properties: &packets.Properties{
				ReasonString:          "Moving to new server",
				SessionExpiryInterval: 120,
				ServerReference:       "tcp://newalderaan:1883",
				UserProperties: []packets.UserProperty{
					{Key: "maintenance", Value: "true"},
				},
				Presence: packets.PresReasonString | packets.PresSessionExpiryInterval | packets.PresServerReference,
			},
		}
		_, _ = conn.Write(encodeToBytes(disconnect))
		time.Sleep(50 * time.Millisecond)
		conn.Close()
	}()

	// 2. Connect Client
	var disconnectErr error
	var wg sync.WaitGroup
	wg.Add(1)

	client, err := mq.Dial(
		"tcp://"+listener.Addr().String(),
		mq.WithClientID("test-props-client-2"),
		mq.WithProtocolVersion(mq.ProtocolV50),
		mq.WithAutoReconnect(false),
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
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for OnConnectionLost")
	}

	// 4. Verify Properties
	var dErr *mq.DisconnectError
	if errors.As(disconnectErr, &dErr) {
		if dErr.ReasonCode != mq.ReasonCodeServerMoved {
			t.Errorf("expected ReasonCodeServerMoved, got 0x%02x", dErr.ReasonCode)
		}
		if dErr.ReasonString != "Moving to new server" {
			t.Errorf("expected reason string 'Moving to new server', got '%s'", dErr.ReasonString)
		}
		if dErr.SessionExpiryInterval != 120 {
			t.Errorf("expected SessionExpiryInterval 120, got %d", dErr.SessionExpiryInterval)
		}
		if dErr.ServerReference != "tcp://newalderaan:1883" {
			t.Errorf("expected ServerReference 'tcp://newalderaan:1883', got '%s'", dErr.ServerReference)
		}
		if val, ok := dErr.UserProperties["maintenance"]; !ok || val != "true" {
			t.Errorf("expected UserProperty maintenance=true, got %v", dErr.UserProperties)
		}
	} else {
		t.Fatalf("expected *DisconnectError, got %T: %v", disconnectErr, disconnectErr)
	}
}
