package mq_test

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/gonzalop/mq"
	"github.com/gonzalop/mq/internal/packets"
)

// TestDisconnectWithProperties verifies that the client sends the specified properties
// in the DISCONNECT packet.
func TestDisconnectWithProperties(t *testing.T) {
	// 1. Setup a mock server
	listener, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	defer listener.Close()

	done := make(chan struct{})

	go func() {
		defer close(done)
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

		// Read packet - Expect DISCONNECT
		pkt, err := packets.ReadPacket(conn, 5, 0)
		if err != nil {
			t.Errorf("failed to read disconnect packet: %v", err)
			return
		}

		disconnect, ok := pkt.(*packets.DisconnectPacket)
		if !ok {
			t.Errorf("expected DISCONNECT packet, got %T", pkt)
			return
		}

		if disconnect.ReasonCode != 0x04 { // DisconnectWithWill
			t.Errorf("expected reason code 0x04, got 0x%02x", disconnect.ReasonCode)
		}

		if disconnect.Properties == nil {
			t.Error("expected properties, got nil")
			return
		}

		if disconnect.Properties.SessionExpiryInterval != 300 {
			t.Errorf("expected SessionExpiryInterval 300, got %d", disconnect.Properties.SessionExpiryInterval)
		}

		if disconnect.Properties.ReasonString != "Taking a break" {
			t.Errorf("expected ReasonString 'Taking a break', got '%s'", disconnect.Properties.ReasonString)
		}

		foundUserProp := false
		for _, up := range disconnect.Properties.UserProperties {
			if up.Key == "my-key" && up.Value == "my-value" {
				foundUserProp = true
				break
			}
		}
		if !foundUserProp {
			t.Errorf("expected UserProperty 'my-key'='my-value', not found")
		}
	}()

	// 2. Connect Client
	client, err := mq.Dial(
		"tcp://"+listener.Addr().String(),
		mq.WithClientID("test-props-client"),
		mq.WithProtocolVersion(mq.ProtocolV50),
	)
	if err != nil {
		t.Fatalf("failed to dial: %v", err)
	}

	// 3. Disconnect with Properties
	props := mq.NewProperties()
	expiry := uint32(300)
	props.SessionExpiryInterval = &expiry
	props.ReasonString = "Taking a break"
	props.SetUserProperty("my-key", "my-value")

	err = client.Disconnect(
		context.Background(),
		mq.WithReason(0x04), // DisconnectWithWill
		mq.WithDisconnectProperties(props),
	)
	if err != nil {
		t.Fatalf("failed to disconnect: %v", err)
	}

	// Wait for server verification
	select {
	case <-done:
		// Success
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for server verification")
	}
}
