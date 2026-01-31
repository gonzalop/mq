package mq

import (
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/gonzalop/mq/internal/packets"
)

func TestConnectUserProperties(t *testing.T) {
	// 1. Configure client with User Properties
	props := map[string]string{
		"region":  "us-east-1",
		"version": "1.0.0",
	}

	c := &Client{
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

	c := &Client{
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
	c := &Client{
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
