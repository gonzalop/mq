package mq

import (
	"context"
	"testing"

	"github.com/gonzalop/mq/internal/packets"
)

func TestWithSubscriptionIdentifier(t *testing.T) {
	tests := []struct {
		name           string
		subscriptionID int
		wantError      bool
		errorContains  string
	}{
		{
			name:           "valid ID - minimum",
			subscriptionID: 1,
			wantError:      false,
		},
		{
			name:           "valid ID - typical",
			subscriptionID: 100,
			wantError:      false,
		},
		{
			name:           "valid ID - maximum",
			subscriptionID: 268435455,
			wantError:      false,
		},
		{
			name:           "zero ID - no identifier",
			subscriptionID: 0,
			wantError:      false,
		},
		{
			name:           "negative ID - invalid",
			subscriptionID: -1,
			wantError:      true,
			errorContains:  "must be in range 0-268435455",
		},
		{
			name:           "exceeds maximum - invalid",
			subscriptionID: 268435456,
			wantError:      true,
			errorContains:  "must be in range 0-268435455",
		},
		{
			name:           "large invalid ID",
			subscriptionID: 999999999,
			wantError:      true,
			errorContains:  "must be in range 0-268435455",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a mock client
			c := &Client{
				opts: &clientOptions{
					ProtocolVersion: ProtocolV50,
					Logger:          testLogger(),
				},
				outgoing:      make(chan packets.Packet, 1),
				pending:       make(map[uint16]*pendingOp),
				subscriptions: make(map[string]subscriptionEntry),
				stop:          make(chan struct{}),
			}

			// Subscribe with the test subscription ID
			// Note: internalSubscribe is called directly by Subscribe
			token := c.Subscribe("test/topic", AtLeastOnce,
				func(*Client, Message) {},
				WithSubscriptionIdentifier(tt.subscriptionID))

			if tt.wantError {
				// For validation errors, the token is completed immediately
				// Check the error without waiting
				select {
				case <-token.Done():
					err := token.Error()
					if err == nil {
						t.Errorf("expected error, got nil")
					} else if tt.errorContains != "" && !contains(err.Error(), tt.errorContains) {
						t.Errorf("expected error containing %q, got %q", tt.errorContains, err.Error())
					}
				default:
					t.Error("expected token to be completed with error immediately")
				}
			} else {
				// For valid IDs, we expect a timeout (no error from validation)
				// The subscribe request would be sent to the channel
				ctx, cancel := context.WithTimeout(context.Background(), 100)
				defer cancel()

				err := token.Wait(ctx)
				if err != nil && err != context.DeadlineExceeded {
					t.Errorf("unexpected error: %v", err)
				}

				// Verify the request was created with correct subscription ID
				select {
				case p := <-c.outgoing:
					req, ok := p.(*packets.SubscribePacket)
					if !ok {
						t.Fatalf("Expected SubscribePacket, got %T", p)
					}

					if tt.subscriptionID > 0 {
						if req.Properties == nil {
							t.Error("expected properties to be set for non-zero subscription ID")
						} else if len(req.Properties.SubscriptionIdentifier) != 1 {
							t.Errorf("expected 1 subscription identifier, got %d", len(req.Properties.SubscriptionIdentifier))
						} else if req.Properties.SubscriptionIdentifier[0] != tt.subscriptionID {
							t.Errorf("expected subscription ID %d, got %d", tt.subscriptionID, req.Properties.SubscriptionIdentifier[0])
						}
					} else {
						// For ID = 0, properties should not be set (or at least no SubID)
						if req.Properties != nil && len(req.Properties.SubscriptionIdentifier) > 0 {
							t.Error("expected no subscription identifier for ID = 0")
						}
					}
				default:
					t.Error("expected subscribe packet to be sent")
				}
			}
		})
	}
}

func TestSubscriptionIdentifierMQTTv311(t *testing.T) {
	// Subscription identifiers should be ignored for MQTT v3.1.1
	c := &Client{
		opts: &clientOptions{
			ProtocolVersion: ProtocolV311,
			Logger:          testLogger(),
		},
		outgoing:      make(chan packets.Packet, 1),
		pending:       make(map[uint16]*pendingOp),
		subscriptions: make(map[string]subscriptionEntry),
		stop:          make(chan struct{}),
	}

	token := c.Subscribe("test/topic", AtLeastOnce,
		func(*Client, Message) {},
		WithSubscriptionIdentifier(100))

	ctx, cancel := context.WithTimeout(context.Background(), 100)
	defer cancel()

	err := token.Wait(ctx)
	if err != nil && err != context.DeadlineExceeded {
		t.Errorf("unexpected error: %v", err)
	}

	// Verify the request was created without subscription ID properties
	select {
	case p := <-c.outgoing:
		req, ok := p.(*packets.SubscribePacket)
		if !ok {
			t.Fatalf("Expected SubscribePacket, got %T", p)
		}
		if req.Properties != nil && len(req.Properties.SubscriptionIdentifier) > 0 {
			t.Error("expected no subscription identifier for MQTT v3.1.1")
		}
	default:
		t.Error("expected subscribe request to be sent")
	}
}

func TestSubscriptionIdentifierInPacket(t *testing.T) {
	// Test that subscription ID is correctly encoded in the packet
	pkt := &packets.SubscribePacket{
		PacketID: 1,
		Topics:   []string{"test/topic"},
		QoS:      []uint8{1},
		Version:  5,
		Properties: &packets.Properties{
			SubscriptionIdentifier: []int{42},
		},
	}

	encoded := encodeToBytes(pkt)

	// Decode and verify
	decoded, err := packets.DecodeSubscribe(encoded[2:], 5) // Skip fixed header
	if err != nil {
		t.Fatalf("failed to decode: %v", err)
	}

	if decoded.Properties == nil {
		t.Fatal("expected properties to be set")
	}

	if len(decoded.Properties.SubscriptionIdentifier) != 1 {
		t.Fatalf("expected 1 subscription identifier, got %d", len(decoded.Properties.SubscriptionIdentifier))
	}

	if decoded.Properties.SubscriptionIdentifier[0] != 42 {
		t.Errorf("expected subscription ID 42, got %d", decoded.Properties.SubscriptionIdentifier[0])
	}
}

// Helper function to check if a string contains a substring
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && len(substr) > 0 && findSubstring(s, substr)))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func TestResubscribeBatchWithIDs(t *testing.T) {
	// This test verifies that during resubscription, subscriptions with
	// different identifiers are split into separate packets.
	c := &Client{
		opts: &clientOptions{
			ProtocolVersion: ProtocolV50,
			Logger:          testLogger(),
		},
		subscriptions: make(map[string]subscriptionEntry),
		outgoing:      make(chan packets.Packet, 10),
		pending:       make(map[uint16]*pendingOp),
	}

	// Add subscriptions with different IDs
	c.subscriptions["topic/1"] = subscriptionEntry{
		qos: 1,
		options: SubscribeOptions{
			SubscriptionID: 101,
		},
	}
	c.subscriptions["topic/2"] = subscriptionEntry{
		qos: 1,
		options: SubscribeOptions{
			SubscriptionID: 102,
		},
	}
	c.subscriptions["topic/3"] = subscriptionEntry{
		qos: 1,
		options: SubscribeOptions{
			SubscriptionID: 101, // Same as first
		}}

	// Trigger resubscription
	c.resubscribeAll()

	// We expect 2 packets:
	// 1. One with ID 101 (topics topic/1 and topic/3)
	// 2. One with ID 102 (topic topic/2)
	// (order doesn't strictly matter)

	packetsCount := 0
	foundID101 := false
	foundID102 := false

LOOP:
	for {
		select {
		case p := <-c.outgoing:
			packetsCount++
			sub, ok := p.(*packets.SubscribePacket)
			if !ok {
				t.Errorf("expected SubscribePacket, got %T", p)
				continue
			}

			if sub.Properties == nil || len(sub.Properties.SubscriptionIdentifier) != 1 {
				t.Fatalf("expected exactly 1 subscription identifier per packet, got props: %v", sub.Properties)
			}

			id := sub.Properties.SubscriptionIdentifier[0]
			switch id {
			case 101:
				foundID101 = true
				if len(sub.Topics) != 2 {
					t.Errorf("expected 2 topics for ID 101, got %d", len(sub.Topics))
				}
			case 102:
				foundID102 = true
				if len(sub.Topics) != 1 {
					t.Errorf("expected 1 topic for ID 102, got %d", len(sub.Topics))
				}
			default:
				t.Errorf("unexpected subscription ID: %d", id)
			}
		default:
			break LOOP
		}
	}

	if packetsCount != 2 {
		t.Errorf("expected 2 packets, got %d", packetsCount)
	}
	if !foundID101 {
		t.Error("did not find packet with ID 101")
	}
	if !foundID102 {
		t.Error("did not find packet with ID 102")
	}
}
