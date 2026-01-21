package mq

import (
	"testing"
	"time"

	"github.com/gonzalop/mq/internal/packets"
)

func TestSubscribeWithUserProperties(t *testing.T) {
	c := &Client{
		opts: &clientOptions{
			ProtocolVersion: ProtocolV50,
			Logger:          testLogger(),
		},
		subscriptions: make(map[string]subscriptionEntry),
		outgoing:      make(chan packets.Packet, 1),
		pending:       make(map[uint16]*pendingOp),
		stop:          make(chan struct{}),
		nextPacketID:  1,
	}

	topic := "test/topic"
	handler := func(c *Client, msg Message) {}

	// Test subscription with User Properties
	token := c.Subscribe(topic, 1, handler,
		WithSubscribeUserProperty("key1", "value1"),
		WithSubscribeUserProperty("key2", "value2"),
	)

	select {
	case p := <-c.outgoing:
		req, ok := p.(*packets.SubscribePacket)
		if !ok {
			t.Fatalf("Expected SubscribePacket, got %T", p)
		}

		if req.Properties == nil {
			t.Fatal("Expected Properties in SubscribePacket")
		}

		if len(req.Properties.UserProperties) != 2 {
			t.Errorf("Expected 2 User Properties, got %d", len(req.Properties.UserProperties))
		}

		// Map for validation
		props := make(map[string]string)
		for _, up := range req.Properties.UserProperties {
			props[up.Key] = up.Value
		}

		if props["key1"] != "value1" {
			t.Errorf("Expected key1=value1, got %s", props["key1"])
		}
		if props["key2"] != "value2" {
			t.Errorf("Expected key2=value2, got %s", props["key2"])
		}

		// Verify pending op has correct opts
		op := c.pending[req.PacketID]
		if op == nil {
			t.Fatal("Pending op not found")
		}
		if op.token != token {
			t.Error("Token mismatch")
		}

	case <-time.After(time.Second):
		t.Error("Timeout waiting for subscribe packet")
	}
}

func TestResubscribeWithUserPropertiesGrouping(t *testing.T) {
	c := &Client{
		subscriptions: make(map[string]subscriptionEntry),
		pending:       make(map[uint16]*pendingOp),
		outgoing:      make(chan packets.Packet, 10),
		opts: &clientOptions{
			ProtocolVersion: ProtocolV50,
			Logger:          testLogger(),
		},
		nextPacketID: 1,
	}

	// 1. Subscription with User Props A
	c.subscriptions["topic/A"] = subscriptionEntry{
		handler: nil,
		qos:     1,
		options: SubscribeOptions{
			UserProperties: map[string]string{"group": "A"},
		},
	}

	// 2. Subscription with User Props B
	c.subscriptions["topic/B"] = subscriptionEntry{
		handler: nil,
		qos:     1,
		options: SubscribeOptions{
			UserProperties: map[string]string{"group": "B"},
		},
	}

	// 3. Subscription with User Props A (same as 1)
	c.subscriptions["topic/A2"] = subscriptionEntry{
		handler: nil,
		qos:     1,
		options: SubscribeOptions{
			UserProperties: map[string]string{"group": "A"},
		},
	}

	c.resubscribeAll()

	// Should produce 2 packets (Group A and Group B)
	// Order is not guaranteed, so we check both

	seenA := false
	seenB := false

	for range 2 {
		select {
		case p := <-c.outgoing:
			req, ok := p.(*packets.SubscribePacket)
			if !ok {
				t.Fatalf("Expected SubscribePacket, got %T", p)
			}

			if req.Properties == nil || len(req.Properties.UserProperties) == 0 {
				t.Error("Packet missing User Properties")
				continue
			}

			group := req.Properties.UserProperties[0].Value

			if group == "A" {
				if seenA {
					t.Error("Duplicate packet for group A")
				}
				seenA = true
				if len(req.Topics) != 2 {
					t.Errorf("Group A should have 2 topics, got %d", len(req.Topics))
				}
			} else if group == "B" {
				if seenB {
					t.Error("Duplicate packet for group B")
				}
				seenB = true
				if len(req.Topics) != 1 {
					t.Errorf("Group B should have 1 topic, got %d", len(req.Topics))
				}
			} else {
				t.Errorf("Unknown group value: %s", group)
			}

		case <-time.After(time.Second):
			t.Error("Timeout waiting for resubscribe packets")
		}
	}

	if !seenA || !seenB {
		t.Error("Did not see packets for both groups")
	}
}
