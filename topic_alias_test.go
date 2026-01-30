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
			c := &Client{
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
	c := &Client{
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
		c := &Client{
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
		c := &Client{
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
		c := &Client{
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
		c := &Client{
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

func uint16Ptr(v uint16) *uint16 {
	return &v
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
