package mq

import (
	"testing"
	"time"

	"github.com/gonzalop/mq/internal/packets"
)

func TestSubscribe(t *testing.T) {
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

	// Test successful subscription request
	token := c.Subscribe(topic, 1, handler)

	select {
	case p := <-c.outgoing:
		req, ok := p.(*packets.SubscribePacket)
		if !ok {
			t.Errorf("Expected SubscribePacket, got %T", p)
		}
		if len(req.Topics) != 1 || req.Topics[0] != topic {
			t.Errorf("Request topic mismatch: %v", req.Topics)
		}
		// Verify pending op
		if op, ok := c.pending[req.PacketID]; !ok {
			t.Error("Pending op not found")
		} else if op.token != token {
			t.Error("Token mismatch")
		}
	case <-time.After(time.Second):
		t.Error("Timeout waiting for subscribe packet")
	}

	// Test invalid topic (Subscribe should fail synchronously or return error token?
	// The validation in internalSubscribe checks topic validity)
	token = c.Subscribe("#/invalid", 1, handler)
	select {
	case <-token.Done():
		if token.Error() == nil {
			t.Error("Expected error for invalid topic")
		}
	case <-time.After(time.Second):
		t.Error("Timeout waiting for invalid topic token completion")
	}
}

func TestUnsubscribe(t *testing.T) {
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

	// Test successful unsubscribe request
	token := c.Unsubscribe(topic)

	select {
	case p := <-c.outgoing:
		req, ok := p.(*packets.UnsubscribePacket)
		if !ok {
			t.Errorf("Expected UnsubscribePacket, got %T", p)
		}
		if len(req.Topics) != 1 || req.Topics[0] != topic {
			t.Errorf("Request topic mismatch: %v", req.Topics)
		}
		// Verify pending op
		if op, ok := c.pending[req.PacketID]; !ok {
			t.Error("Pending op not found")
		} else if op.token != token {
			t.Error("Token mismatch")
		}
	case <-time.After(time.Second):
		t.Error("Timeout waiting for unsubscribe packet")
	}

}

func TestResubscribeAll(t *testing.T) {
	c := &Client{
		opts: &clientOptions{
			ProtocolVersion: ProtocolV50,
			Logger:          testLogger(),
		},
		subscriptions: map[string]subscriptionEntry{
			"topic1": {handler: nil, qos: 1},
		},
		outgoing:     make(chan packets.Packet, 1),
		pending:      make(map[uint16]*pendingOp),
		stop:         make(chan struct{}),
		nextPacketID: 1,
	}

	// resubscribeAll now runs and sends packets directly
	c.resubscribeAll()

	// Verify packet sent
	select {
	case p := <-c.outgoing:
		_, ok := p.(*packets.SubscribePacket)
		if !ok {
			t.Errorf("Expected SubscribePacket, got %T", p)
		}
	case <-time.After(time.Second):
		t.Error("Timeout waiting for resubscribe packet")
	}
}

func TestInternalSubscribe(t *testing.T) {
	c := &Client{
		opts: &clientOptions{
			ProtocolVersion: ProtocolV50,
			Logger:          testLogger(),
		},
		subscriptions: make(map[string]subscriptionEntry),
		pending:       make(map[uint16]*pendingOp),
		outgoing:      make(chan packets.Packet, 10),
		nextPacketID:  1,
	}

	topic := "test/topic"
	handler := func(c *Client, msg Message) {}

	pkt := &packets.SubscribePacket{
		Topics:  []string{topic},
		QoS:     []uint8{1},
		Version: ProtocolV50,
	}

	token := newToken()
	req := &subscribeRequest{
		packet:  pkt,
		handler: handler,
		token:   token,
	}

	// Execute internal method
	c.internalSubscribe(req)

	// Verify outgoing packet
	select {
	case p := <-c.outgoing:
		sent, ok := p.(*packets.SubscribePacket)
		if !ok {
			t.Errorf("Expected SubscribePacket, got %T", p)
		}
		// Verify pending op created with the sent PacketID
		if op, ok := c.pending[sent.PacketID]; !ok {
			t.Errorf("Pending op not created for PacketID %d", sent.PacketID)
		} else {
			if op.token != token {
				t.Error("Pending op token mismatch")
			}
		}
	case <-time.After(time.Second):
		t.Error("Timeout waiting for outgoing packet")
	}
}

func TestInternalUnsubscribe(t *testing.T) {
	c := &Client{
		opts: &clientOptions{
			ProtocolVersion: ProtocolV50,
			Logger:          testLogger(),
		},
		pending:      make(map[uint16]*pendingOp),
		outgoing:     make(chan packets.Packet, 10),
		nextPacketID: 10,
	}

	topics := []string{"test/topic"}
	pkt := &packets.UnsubscribePacket{
		Topics:  topics,
		Version: ProtocolV50,
	}

	token := newToken()
	req := &unsubscribeRequest{
		packet: pkt,
		topics: topics,
		token:  token,
	}

	// Execute internal method
	c.internalUnsubscribe(req)

	// Verify outgoing packet
	select {
	case p := <-c.outgoing:
		sent, ok := p.(*packets.UnsubscribePacket)
		if !ok {
			t.Errorf("Expected UnsubscribePacket, got %T", p)
		}
		// Verify pending op created with the sent PacketID
		if op, ok := c.pending[sent.PacketID]; !ok {
			t.Errorf("Pending op not created for PacketID %d", sent.PacketID)
		} else {
			if op.token != token {
				t.Error("Pending op token mismatch")
			}
		}
	case <-time.After(time.Second):
		t.Error("Timeout waiting for outgoing packet")
	}
}

// TestResubscribeBatching tests that resubscribe correctly batches topics.
func TestResubscribeBatching(t *testing.T) {
	tests := []struct {
		name            string
		numTopics       int
		expectedBatches int
	}{
		{"no subscriptions", 0, 0},
		{"single topic", 1, 1},
		{"exactly one batch", 100, 1},
		{"two batches", 150, 2},
		{"five batches", 500, 5},
		{"partial last batch", 250, 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &Client{
				subscriptions: make(map[string]subscriptionEntry),
				pending:       make(map[uint16]*pendingOp),
				outgoing:      make(chan packets.Packet, 100),
				opts:          defaultOptions("tcp://test:1883"),
			}

			// Add test subscriptions
			for i := 0; i < tt.numTopics; i++ {
				topic := "test/topic/" + string(rune('a'+i%26)) + "/" + string(rune('0'+i/26))
				c.subscriptions[topic] = subscriptionEntry{handler: func(*Client, Message) {}, qos: 1}
			}

			// Handle the request directly
			c.resubscribeAll()

			// Check number of packets sent
			actualBatches := len(c.outgoing)
			if actualBatches != tt.expectedBatches {
				t.Errorf("expected %d batches, got %d", tt.expectedBatches, actualBatches)
			}

			// Verify each batch
			totalTopics := 0
			for i := range actualBatches {
				pkt := <-c.outgoing
				subPkt, ok := pkt.(*packets.SubscribePacket)
				if !ok {
					t.Fatalf("expected SubscribePacket, got %T", pkt)
				}

				// Check batch size
				batchSize := len(subPkt.Topics)
				if i < actualBatches-1 {
					// All batches except last should be full (100 topics)
					if batchSize != 100 {
						t.Errorf("batch %d: expected 100 topics, got %d", i+1, batchSize)
					}
				} else {
					// Last batch can be partial
					expectedLast := tt.numTopics % 100
					if expectedLast == 0 && tt.numTopics > 0 {
						expectedLast = 100
					}
					if batchSize != expectedLast {
						t.Errorf("last batch: expected %d topics, got %d", expectedLast, batchSize)
					}
				}

				// Check QoS levels
				if len(subPkt.QoS) != batchSize {
					t.Errorf("QoS array length mismatch: got %d, want %d", len(subPkt.QoS), batchSize)
				}
				for j, qos := range subPkt.QoS {
					if qos != 1 {
						t.Errorf("topic %d in batch %d: expected QoS 1, got %d", j, i+1, qos)
					}
				}

				totalTopics += batchSize
			}

			// Verify total topics match
			if totalTopics != tt.numTopics {
				t.Errorf("total topics mismatch: expected %d, got %d", tt.numTopics, totalTopics)
			}

			// Verify pending operations were created
			if len(c.pending) != tt.expectedBatches {
				t.Errorf("expected %d pending operations, got %d", tt.expectedBatches, len(c.pending))
			}
		})
	}
}

// TestResubscribePacketIDs tests that each batch gets a unique packet ID.
func TestResubscribePacketIDs(t *testing.T) {
	c := &Client{
		subscriptions: make(map[string]subscriptionEntry),
		pending:       make(map[uint16]*pendingOp),
		outgoing:      make(chan packets.Packet, 10),
		opts:          defaultOptions("tcp://test:1883"),
		nextPacketID:  0,
	}

	// Add 250 subscriptions (should create 3 batches)
	for i := range 250 {
		c.subscriptions["topic/"+string(rune('a'+i%26))+"/"+string(rune('0'+i/26))] = subscriptionEntry{handler: func(*Client, Message) {}, qos: 1}
	}

	c.resubscribeAll()

	// Collect packet IDs
	seenIDs := make(map[uint16]bool)
	for range 3 {
		pkt := <-c.outgoing
		subPkt := pkt.(*packets.SubscribePacket)

		if seenIDs[subPkt.PacketID] {
			t.Errorf("duplicate packet ID: %d", subPkt.PacketID)
		}
		seenIDs[subPkt.PacketID] = true

		if subPkt.PacketID == 0 {
			t.Error("packet ID should not be 0")
		}
	}

	if len(seenIDs) != 3 {
		t.Errorf("expected 3 unique packet IDs, got %d", len(seenIDs))
	}
}

// TestResubscribeTimestamp tests that pending operations have timestamps.
func TestResubscribeTimestamp(t *testing.T) {
	c := &Client{
		subscriptions: make(map[string]subscriptionEntry),
		pending:       make(map[uint16]*pendingOp),
		outgoing:      make(chan packets.Packet, 10),
		opts:          defaultOptions("tcp://test:1883"),
	}

	// Add some subscriptions
	for i := range 50 {
		c.subscriptions["topic/"+string(rune('a'+i))] = subscriptionEntry{handler: func(*Client, Message) {}, qos: 1}
	}

	before := time.Now()
	c.resubscribeAll()
	after := time.Now()

	// Check that pending operation has a timestamp
	if len(c.pending) != 1 {
		t.Fatalf("expected 1 pending operation, got %d", len(c.pending))
	}

	for _, op := range c.pending {
		if op.timestamp.Before(before) || op.timestamp.After(after) {
			t.Errorf("timestamp %v is outside expected range [%v, %v]", op.timestamp, before, after)
		}
		if op.qos != 1 {
			t.Errorf("expected QoS 1, got %d", op.qos)
		}
	}
}
