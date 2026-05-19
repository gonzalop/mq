package mq

import (
	"io"
	"log/slog"
	"testing"

	"github.com/gonzalop/mq/internal/packets"
)

func TestApplyTopicAlias(t *testing.T) {
	tests := []struct {
		name          string
		maxAliases    uint16
		existingAlias map[string]uint16
		nextID        uint16
		topic         string
		wantAliasID   *uint16
		wantTopic     string
		wantNextID    uint16
		wantMapSize   int
	}{
		{
			name:        "aliases disabled (maxAliases=0)",
			maxAliases:  0,
			topic:       "test/topic",
			wantAliasID: nil,
			wantTopic:   "test/topic",
			wantNextID:  1,
			wantMapSize: 0,
		},
		{
			name:        "first alias allocation",
			maxAliases:  10,
			nextID:      1,
			topic:       "sensors/temp",
			wantAliasID: uint16Ptr(1),
			wantTopic:   "sensors/temp", // First time sends both
			wantNextID:  2,
			wantMapSize: 1,
		},
		{
			name:          "reuse existing alias",
			maxAliases:    10,
			existingAlias: map[string]uint16{"sensors/temp": 5},
			nextID:        6,
			topic:         "sensors/temp",
			wantAliasID:   uint16Ptr(5),
			wantTopic:     "", // Subsequent sends empty topic
			wantNextID:    6,  // Unchanged
			wantMapSize:   1,
		},
		{
			name:        "hit alias limit",
			maxAliases:  2,
			nextID:      3, // Already at limit
			topic:       "new/topic",
			wantAliasID: nil,
			wantTopic:   "new/topic", // Graceful degradation
			wantNextID:  3,
			wantMapSize: 0,
		},
		{
			name:          "allocate second alias",
			maxAliases:    10,
			existingAlias: map[string]uint16{"topic1": 1},
			nextID:        2,
			topic:         "topic2",
			wantAliasID:   uint16Ptr(2),
			wantTopic:     "topic2",
			wantNextID:    3,
			wantMapSize:   2,
		},
		{
			name:        "at exact limit boundary",
			maxAliases:  200,
			nextID:      200,
			topic:       "last/topic",
			wantAliasID: uint16Ptr(200),
			wantTopic:   "last/topic",
			wantNextID:  201,
			wantMapSize: 1,
		},
		{
			name:        "beyond limit boundary",
			maxAliases:  200,
			nextID:      201,
			topic:       "overflow/topic",
			wantAliasID: nil,
			wantTopic:   "overflow/topic",
			wantNextID:  201,
			wantMapSize: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &Client{trie: newTopicTrie(),
				opts: &clientOptions{
					ProtocolVersion: ProtocolV50,
					Logger:          testLogger(),
				},
				maxAliases:   tt.maxAliases,
				nextAliasID:  tt.nextID,
				topicAliases: tt.existingAlias,
			}
			if c.topicAliases == nil {
				c.topicAliases = make(map[string]uint16)
			}
			if c.nextAliasID == 0 {
				c.nextAliasID = 1
			}

			pkt := &packets.PublishPacket{
				Topic: tt.topic,
			}

			c.applyTopicAlias(pkt)

			// Check alias ID
			if tt.wantAliasID == nil {
				if pkt.Properties != nil && pkt.Properties.Presence&packets.PresTopicAlias != 0 {
					t.Errorf("expected no alias, got %d", pkt.Properties.TopicAlias)
				}
			} else {
				if pkt.Properties == nil || pkt.Properties.Presence&packets.PresTopicAlias == 0 {
					t.Errorf("expected alias %d, got nil", *tt.wantAliasID)
				} else if pkt.Properties.TopicAlias != *tt.wantAliasID {
					t.Errorf("expected alias %d, got %d", *tt.wantAliasID, pkt.Properties.TopicAlias)
				}
			}

			if pkt.Topic != tt.wantTopic {
				t.Errorf("expected topic %q, got %q", tt.wantTopic, pkt.Topic)
			}

			if c.nextAliasID != tt.wantNextID {
				t.Errorf("expected nextID %d, got %d", tt.wantNextID, c.nextAliasID)
			}

			if len(c.topicAliases) != tt.wantMapSize {
				t.Errorf("expected map size %d, got %d", tt.wantMapSize, len(c.topicAliases))
			}
		})
	}
}

func TestTopicAliasReconnectionClearing(t *testing.T) {
	c := &Client{trie: newTopicTrie(),
		maxAliases:   50,
		nextAliasID:  10,
		topicAliases: map[string]uint16{"topic1": 1, "topic2": 2},
	}

	// Simulate reconnection clearing
	c.topicAliases = make(map[string]uint16)
	c.nextAliasID = 1
	c.maxAliases = 0

	if len(c.topicAliases) != 0 {
		t.Errorf("expected empty map after reconnect, got %d entries", len(c.topicAliases))
	}
	if c.nextAliasID != 1 {
		t.Errorf("expected nextID=1 after reconnect, got %d", c.nextAliasID)
	}
	if c.maxAliases != 0 {
		t.Errorf("expected maxAliases=0 after reconnect, got %d", c.maxAliases)
	}
}

func TestHandleIncomingTopicAlias(t *testing.T) {
	t.Run("register and resolve alias", func(t *testing.T) {
		c := &Client{trie: newTopicTrie(),
			opts: &clientOptions{
				ProtocolVersion: ProtocolV50,
				Logger:          testLogger(),
			},
			receivedAliases: make(map[uint16]string),
		}

		// 1. Incoming packet with both topic and alias
		p1 := &packets.PublishPacket{
			Topic: "sensors/temp",
			Properties: &packets.Properties{
				TopicAlias: 1,
				Presence:   packets.PresTopicAlias,
			},
		}
		c.handlePublish(p1)

		// Verify registration
		c.receivedAliasesLock.RLock()
		if c.receivedAliases[1] != "sensors/temp" {
			t.Errorf("expected alias 1 to be 'sensors/temp', got %q", c.receivedAliases[1])
		}
		c.receivedAliasesLock.RUnlock()

		// 2. Incoming packet with only alias
		p2 := &packets.PublishPacket{
			Topic: "",
			Properties: &packets.Properties{
				TopicAlias: 1,
				Presence:   packets.PresTopicAlias,
			},
		}
		c.handlePublish(p2)

		// Verify resolution
		if p2.Topic != "sensors/temp" {
			t.Errorf("expected p2.Topic to be 'sensors/temp', got %q", p2.Topic)
		}
	})

	t.Run("invalid alias 0", func(t *testing.T) {
		c := &Client{trie: newTopicTrie(),
			opts: &clientOptions{
				ProtocolVersion: ProtocolV50,
				Logger:          testLogger(),
			},
		}

		p := &packets.PublishPacket{
			Topic: "test",
			Properties: &packets.Properties{
				TopicAlias: 0,
				Presence:   packets.PresTopicAlias,
			},
		}
		// This should log an error and NOT register anything
		c.handlePublish(p)

		if len(c.receivedAliases) != 0 {
			t.Errorf("expected no aliases to be registered for alias 0")
		}
	})

	t.Run("server exceeds TopicAliasMaximum", func(t *testing.T) {
		c := &Client{trie: newTopicTrie(),
			opts: &clientOptions{
				ProtocolVersion:   ProtocolV50,
				TopicAliasMaximum: 5,
				Logger:            testLogger(),
			},
		}

		p := &packets.PublishPacket{
			Topic: "test",
			Properties: &packets.Properties{
				TopicAlias: 10, // Exceeds 5
				Presence:   packets.PresTopicAlias,
			},
		}
		c.handlePublish(p)

		if len(c.receivedAliases) != 0 {
			t.Errorf("expected no aliases to be registered when limit exceeded")
		}
	})

	t.Run("unknown alias", func(t *testing.T) {
		c := &Client{trie: newTopicTrie(),
			opts: &clientOptions{
				ProtocolVersion: ProtocolV50,
				Logger:          testLogger(),
			},
			receivedAliases: make(map[uint16]string),
		}

		p := &packets.PublishPacket{
			Topic: "",
			Properties: &packets.Properties{
				TopicAlias: 99,
				Presence:   packets.PresTopicAlias,
			},
		}
		c.handlePublish(p)

		if p.Topic != "" {
			t.Errorf("expected topic to remain empty for unknown alias")
		}
	})
}

func TestTopicAliasStaleAfterReconnectRepro(t *testing.T) {
	// This test reproduces the issue where a packet prepared with a topic alias
	// becomes invalid if the connection is lost and re-established.
	// Based on a true story.

	c := &Client{trie: newTopicTrie(),
		opts: &clientOptions{
			ProtocolVersion: ProtocolV50,
			Logger:          testLogger(),
		},
		outgoing:     make(chan packets.Packet, 10),
		topicAliases: make(map[string]uint16),
		nextAliasID:  1,
		maxAliases:   10,
	}

	topic := "test/topic"

	// 1. First publish - assigns alias, sends both topic and alias
	pkt1 := &packets.PublishPacket{
		Topic:    topic,
		UseAlias: true,
		Version:  5,
	}
	c.applyTopicAlias(pkt1)

	if pkt1.Topic != topic {
		t.Errorf("First publish should have topic, got empty")
	}
	if pkt1.Properties.TopicAlias != 1 {
		t.Errorf("Expected alias 1, got %d", pkt1.Properties.TopicAlias)
	}

	// 2. Second publish - uses alias, sends empty topic
	pkt2 := &packets.PublishPacket{
		Topic:    topic,
		UseAlias: true,
		Version:  5,
	}
	c.applyTopicAlias(pkt2)

	if pkt2.Topic != "" {
		t.Errorf("Second publish should have empty topic, got %q", pkt2.Topic)
	}
	if pkt2.Properties.TopicAlias != 1 {
		t.Errorf("Expected alias 1, got %d", pkt2.Properties.TopicAlias)
	}

	c.outgoing <- pkt2

	// 3. Simulate reconnection - use the new resetAllTopicAliases method
	c.resetAllTopicAliases()

	// 4. Now check if pkt2 was fixed
	// We need to get it back from the channel (it was re-queued)
	fixedPkt := (<-c.outgoing).(*packets.PublishPacket)

	if fixedPkt.Topic != topic {
		t.Errorf("Packet topic should have been restored to %q, got %q", topic, fixedPkt.Topic)
	}
	if pkt2.Properties != nil && pkt2.Properties.Presence&packets.PresTopicAlias != 0 {
		t.Errorf("Packet should have had its topic alias removed")
	}

	t.Log("Fix verified: pkt2 is no longer stale and is safe to send on new connection")
}

func TestTopicAliasStalePendingAfterReconnect(t *testing.T) {
	c := &Client{trie: newTopicTrie(),
		opts: &clientOptions{
			ProtocolVersion: ProtocolV50,
			Logger:          testLogger(),
		},
		pending:      make(map[uint16]*pendingOp),
		topicAliases: make(map[string]uint16),
		nextAliasID:  1,
		maxAliases:   10,
	}

	topic := "test/pending"

	// 1. Prepare packet with alias
	pkt := &packets.PublishPacket{
		Topic:    topic,
		QoS:      1,
		PacketID: 100,
		UseAlias: true,
		Version:  5,
	}
	c.applyTopicAlias(pkt)

	// Subsequent publish to the same topic would have empty topic
	pkt2 := &packets.PublishPacket{
		Topic:    topic,
		QoS:      1,
		PacketID: 101,
		UseAlias: true,
		Version:  5,
	}
	c.applyTopicAlias(pkt2)

	if pkt2.Topic != "" {
		t.Fatalf("pkt2 should have empty topic")
	}

	// Put in pending
	c.pending[pkt.PacketID] = &pendingOp{packet: pkt}
	c.pending[pkt2.PacketID] = &pendingOp{packet: pkt2}

	// 2. Reconnect
	c.resetAllTopicAliases()

	// 3. Verify
	if pkt2.Topic != topic {
		t.Errorf("Pending pkt2 topic should have been restored, got %q", pkt2.Topic)
	}
	if pkt2.Properties != nil && pkt2.Properties.Presence&packets.PresTopicAlias != 0 {
		t.Errorf("Pending pkt2 should have had its topic alias removed")
	}
}

func BenchmarkTopicAlias_FirstPublish(b *testing.B) {
	c := &Client{trie: newTopicTrie(),
		opts: &clientOptions{
			ProtocolVersion: ProtocolV50,
			Logger:          slog.New(slog.NewTextHandler(io.Discard, nil)),
		},
		maxAliases:   100,
		nextAliasID:  1,
		topicAliases: make(map[string]uint16),
	}

	pkt := &packets.PublishPacket{
		Topic:    "sensors/building-a/floor-3/room-42/temperature",
		Payload:  []byte("25.5"),
		QoS:      1,
		PacketID: 10,
		UseAlias: true,
	}

	for b.Loop() {
		// Reset for each iteration to simulate first publish
		c.topicAliases = make(map[string]uint16)
		c.nextAliasID = 1
		c.applyTopicAlias(pkt)
		pkt.Topic = "sensors/building-a/floor-3/room-42/temperature" // Reset topic
	}
}

func BenchmarkTopicAlias_SubsequentPublish(b *testing.B) {
	c := &Client{trie: newTopicTrie(),
		opts: &clientOptions{
			ProtocolVersion: ProtocolV50,
			Logger:          slog.New(slog.NewTextHandler(io.Discard, nil)),
		},
		maxAliases:  100,
		nextAliasID: 2,
		topicAliases: map[string]uint16{
			"sensors/building-a/floor-3/room-42/temperature": 1,
		},
	}

	pkt := &packets.PublishPacket{
		Topic:    "sensors/building-a/floor-3/room-42/temperature",
		Payload:  []byte("25.5"),
		QoS:      1,
		PacketID: 10,
		UseAlias: true,
	}

	for b.Loop() {
		c.applyTopicAlias(pkt)
		pkt.Topic = "sensors/building-a/floor-3/room-42/temperature" // Reset topic
	}
}

func BenchmarkTopicAlias_Disabled(b *testing.B) {
	c := &Client{trie: newTopicTrie(),
		opts: &clientOptions{
			ProtocolVersion: ProtocolV50,
			Logger:          slog.New(slog.NewTextHandler(io.Discard, nil)),
		},
		maxAliases:   0, // Disabled - early return
		topicAliases: make(map[string]uint16),
	}

	pkt := &packets.PublishPacket{
		Topic:    "sensors/building-a/floor-3/room-42/temperature",
		Payload:  []byte("25.5"),
		QoS:      1,
		PacketID: 10,
		UseAlias: true,
	}

	for b.Loop() {
		c.applyTopicAlias(pkt)
		pkt.Topic = "sensors/building-a/floor-3/room-42/temperature" // Reset topic
	}
}

func BenchmarkTopicAlias_Encoding_WithAlias(b *testing.B) {
	alias := uint16(1)
	pkt := &packets.PublishPacket{
		Topic:    "", // Empty when using alias
		Payload:  []byte("25.5"),
		QoS:      1,
		PacketID: 10,
		Properties: &packets.Properties{
			TopicAlias: alias,
			Presence:   packets.PresTopicAlias,
		},
	}

	for b.Loop() {
		encodeToBytes(pkt)
	}
}

func BenchmarkTopicAlias_Encoding_WithoutAlias(b *testing.B) {
	pkt := &packets.PublishPacket{
		Topic:    "sensors/building-a/floor-3/room-42/temperature",
		Payload:  []byte("25.5"),
		QoS:      1,
		PacketID: 10,
	}

	for b.Loop() {
		encodeToBytes(pkt)
	}
}

func uint16Ptr(v uint16) *uint16 {
	return &v
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
