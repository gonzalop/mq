package mq

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"testing"
	"time"

	"github.com/gonzalop/mq/internal/packets"
)

func TestConnectPacketLimits(t *testing.T) {
	tests := []struct {
		name                 string
		opts                 []Option
		wantReceiveMax       uint16
		wantMaxPacketSize    uint32
		wantReceiveMaxSet    bool
		wantMaxPacketSizeSet bool
	}{
		{
			name:                 "Defaults",
			opts:                 []Option{WithProtocolVersion(ProtocolV50)},
			wantReceiveMaxSet:    false,
			wantMaxPacketSizeSet: false,
		},
		{
			name: "With Receive Maximum",
			opts: []Option{
				WithProtocolVersion(ProtocolV50),
				WithReceiveMaximum(100, LimitPolicyIgnore),
			},
			wantReceiveMax:       100,
			wantReceiveMaxSet:    true,
			wantMaxPacketSizeSet: false,
		},
		{
			name: "With Max Packet Size",
			opts: []Option{
				WithProtocolVersion(ProtocolV50),
				WithMaxIncomingPacket(2048),
			},
			wantMaxPacketSize:    2048,
			wantReceiveMaxSet:    false,
			wantMaxPacketSizeSet: true,
		},
		{
			name: "With Both",
			opts: []Option{
				WithProtocolVersion(ProtocolV50),
				WithReceiveMaximum(50, LimitPolicyIgnore),
				WithMaxIncomingPacket(4096),
			},
			wantReceiveMax:       50,
			wantMaxPacketSize:    4096,
			wantReceiveMaxSet:    true,
			wantMaxPacketSizeSet: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup client options
			options := defaultOptions("tcp://localhost:1883")
			for _, opt := range tt.opts {
				opt(options)
			}

			c := &Client{trie: newTopicTrie(),
				opts:               options,
				requestedKeepAlive: options.KeepAlive,
			}

			pkt := c.buildConnectPacket()

			if pkt.Properties == nil {
				if tt.wantReceiveMaxSet || tt.wantMaxPacketSizeSet {
					t.Fatal("Properties is nil but expected properties to be set")
				}
				return
			}

			// Check Receive Maximum
			hasReceiveMax := pkt.Properties.Presence&packets.PresReceiveMaximum != 0
			if hasReceiveMax != tt.wantReceiveMaxSet {
				t.Errorf("ReceiveMaximum presence = %v, want %v", hasReceiveMax, tt.wantReceiveMaxSet)
			}
			if hasReceiveMax && pkt.Properties.ReceiveMaximum != tt.wantReceiveMax {
				t.Errorf("ReceiveMaximum = %d, want %d", pkt.Properties.ReceiveMaximum, tt.wantReceiveMax)
			}

			// Check Maximum Packet Size
			hasMaxPacketSize := pkt.Properties.Presence&packets.PresMaximumPacketSize != 0
			if hasMaxPacketSize != tt.wantMaxPacketSizeSet {
				t.Errorf("MaximumPacketSize presence = %v, want %v", hasMaxPacketSize, tt.wantMaxPacketSizeSet)
			}
			if hasMaxPacketSize && pkt.Properties.MaximumPacketSize != tt.wantMaxPacketSize {
				t.Errorf("MaximumPacketSize = %d, want %d", pkt.Properties.MaximumPacketSize, tt.wantMaxPacketSize)
			}
		})
	}
}

func TestConnectUserProperties(t *testing.T) {
	// 1. Configure client with User Properties
	props := map[string]string{
		"region":  "us-east-1",
		"version": "1.0.0",
	}

	c := &Client{trie: newTopicTrie(),
		opts: &clientOptions{
			ProtocolVersion:       ProtocolV50,
			KeepAlive:             60 * time.Second,
			ConnectUserProperties: props,
		},
	}
	c.requestedKeepAlive = 60 * time.Second

	// 2. Build the CONNECT packet
	pkt := c.buildConnectPacket()

	// 3. Verify Properties
	if pkt.Properties == nil {
		t.Fatal("Connect packet properties should not be nil")
	}

	if len(pkt.Properties.UserProperties) != 2 {
		t.Fatalf("Expected 2 user properties, got %d", len(pkt.Properties.UserProperties))
	}

	// Verify content (map iteration order is random, so check existence)
	foundRegion := false
	foundVersion := false

	for _, up := range pkt.Properties.UserProperties {
		if up.Key == "region" && up.Value == "us-east-1" {
			foundRegion = true
		}
		if up.Key == "version" && up.Value == "1.0.0" {
			foundVersion = true
		}
	}

	if !foundRegion {
		t.Error("User property 'region' not found or incorrect")
	}
	if !foundVersion {
		t.Error("User property 'version' not found or incorrect")
	}

	// 4. Verify Presence bit is NOT set (it doesn't exist for UserProperties)
	if pkt.Properties.Presence != 0 {
		t.Errorf("Expected Presence to be 0, got 0x%x", pkt.Properties.Presence)
	}
}

func TestConnectUserProperties_V311(t *testing.T) {
	// Verify properties are NOT sent in v3.1.1
	props := map[string]string{
		"region": "us-east-1",
	}

	c := &Client{trie: newTopicTrie(),
		opts: &clientOptions{
			ProtocolVersion:       ProtocolV311,
			KeepAlive:             60 * time.Second,
			ConnectUserProperties: props,
		},
	}
	c.requestedKeepAlive = 60 * time.Second

	pkt := c.buildConnectPacket()

	if pkt.Properties != nil {
		t.Error("Connect packet properties should be nil for v3.1.1")
	}
}

func TestConnackUserProperties(t *testing.T) {
	// Simulate receiving a CONNACK with User Properties
	connack := &packets.ConnackPacket{
		ReturnCode: 0,
		Properties: &packets.Properties{
			UserProperties: []packets.UserProperty{
				{Key: "server-region", Value: "eu-west-1"},
				{Key: "maintenance", Value: "false"},
			},
		},
	}

	// Initialize client with minimal options and a discard logger
	c := &Client{trie: newTopicTrie(),
		opts: &clientOptions{
			ProtocolVersion: ProtocolV50,
			Logger:          slog.New(slog.NewTextHandler(io.Discard, nil)),
		},
	}

	// Trigger processing
	c.processConnackProperties(connack)

	// Verify properties were stored
	props := c.ConnectionUserProperties()
	if props == nil {
		t.Fatal("ConnectionUserProperties should not be nil")
	}

	if len(props) != 2 {
		t.Fatalf("Expected 2 user properties, got %d", len(props))
	}

	if props["server-region"] != "eu-west-1" {
		t.Errorf("Expected server-region=eu-west-1, got %s", props["server-region"])
	}

	if props["maintenance"] != "false" {
		t.Errorf("Expected maintenance=false, got %s", props["maintenance"])
	}

	// Verify V3.1.1 ignores them
	c.opts.ProtocolVersion = ProtocolV311
	c.connackUserProperties = nil // Reset
	c.processConnackProperties(connack)

	if c.ConnectionUserProperties() != nil {
		t.Error("ConnectionUserProperties should be nil for v3.1.1")
	}
}

func TestDialContext_Cancellation(t *testing.T) {
	// 1. Create a context that is already cancelled
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// 2. Attempt to dial (should fail immediately)
	client, err := DialContext(ctx, "tcp://localhost:1883")
	if err == nil {
		_ = client.Disconnect(context.Background())
		t.Fatal("Expected error for cancelled context, got nil")
	}

	if err != context.Canceled {
		// It might be a wrapped error or net error depending on where it failed
		t.Logf("Got expected error: %v", err)
	}
}

func TestDialContext_Timeout(t *testing.T) {
	// 1. Create a context with a very short timeout
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Nanosecond)
	defer cancel()

	// 2. Attempt to dial (should fail due to timeout)
	// We use a non-existent server to ensure it doesn't accidentally connect fast
	client, err := DialContext(ctx, "tcp://192.0.2.1:1883")
	if err == nil {
		_ = client.Disconnect(context.Background())
		t.Fatal("Expected error for timed out context, got nil")
	}
}

func TestProtocolNegotiation(t *testing.T) {
	// Start a mock server that refuses v5.0 and accepts v3.1.1
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	addr := ln.Addr().String()

	go func() {
		// First attempt: client sends v5.0 CONNECT
		conn1, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn1.Close()

		// Read CONNECT
		pkt, err := packets.ReadPacket(conn1, ProtocolV50, 0)
		if err != nil {
			return
		}
		cpkt, ok := pkt.(*packets.ConnectPacket)
		if !ok || cpkt.ProtocolLevel != ProtocolV50 {
			return
		}

		// Refuse with Unacceptable Protocol Version (v3.1.1 style return code)
		connack1 := &packets.ConnackPacket{
			ReturnCode: uint8(packets.ConnRefusedUnacceptableProtocol),
		}
		_, _ = connack1.WriteTo(conn1)
		conn1.Close()

		// Second attempt: client should send v3.1.1 CONNECT
		conn2, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn2.Close()

		pkt, err = packets.ReadPacket(conn2, ProtocolV311, 0)
		if err != nil {
			return
		}
		cpkt, ok = pkt.(*packets.ConnectPacket)
		if !ok || cpkt.ProtocolLevel != ProtocolV311 {
			fmt.Printf("Expected v3.1.1, got %d\n", cpkt.ProtocolLevel)
			return
		}

		// Accept connection
		connack2 := &packets.ConnackPacket{
			ReturnCode: uint8(packets.ConnAccepted),
		}
		_, _ = connack2.WriteTo(conn2)
	}()

	client, err := Dial("tcp://"+addr,
		WithClientID("negotiator"),
		WithConnectTimeout(2*time.Second),
		WithAutoReconnect(false),
	)

	if err != nil {
		t.Fatalf("Dial failed: %v", err)
	}
	defer func() { _ = client.Disconnect(context.Background()) }()

	if client.opts.ProtocolVersion != ProtocolV311 {
		t.Errorf("expected protocol version %d, got %d", ProtocolV311, client.opts.ProtocolVersion)
	}
}

func TestWithWillDelayInterval(t *testing.T) {
	options := defaultOptions("tcp://localhost:1883")
	WithProtocolVersion(ProtocolV50)(options)
	WithWill("status", []byte("offline"), 1, false)(options)
	WithWillDelayInterval(120)(options)

	c := &Client{
		trie:               newTopicTrie(),
		opts:               options,
		requestedKeepAlive: options.KeepAlive,
	}

	pkt := c.buildConnectPacket()
	if pkt.WillProperties == nil {
		t.Fatal("expected WillProperties to be non-nil")
	}
	if pkt.WillProperties.WillDelayInterval != 120 {
		t.Errorf("WillDelayInterval = %d, want 120", pkt.WillProperties.WillDelayInterval)
	}
}
