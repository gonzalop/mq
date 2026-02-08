package mq

import (
	"testing"

	"github.com/gonzalop/mq/internal/packets"
)

func TestTopicAliasStaleAfterReconnectRepro(t *testing.T) {
	// This test reproduces the issue where a packet prepared with a topic alias
	// becomes invalid if the connection is lost and re-established.
	// Based on a true story.

	c := &Client{
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
	c := &Client{
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
