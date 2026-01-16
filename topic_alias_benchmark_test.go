package mq

import (
	"io"
	"log/slog"
	"testing"

	"github.com/gonzalop/mq/internal/packets"
)

// BenchmarkTopicAlias_FirstPublish benchmarks the first publish with alias
// (sends full topic + assigns alias ID).
func BenchmarkTopicAlias_FirstPublish(b *testing.B) {
	c := &Client{
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

// BenchmarkTopicAlias_SubsequentPublish benchmarks subsequent publishes with alias
// (sends only alias ID, empty topic).
func BenchmarkTopicAlias_SubsequentPublish(b *testing.B) {
	c := &Client{
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

// BenchmarkTopicAlias_Disabled benchmarks the overhead when aliases are disabled.
// This should be very fast (just an early return) to show minimal overhead.
func BenchmarkTopicAlias_Disabled(b *testing.B) {
	c := &Client{
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

// BenchmarkTopicAlias_Encoding_WithAlias benchmarks encoding a packet with alias.
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

// BenchmarkTopicAlias_Encoding_WithoutAlias benchmarks encoding a packet without alias.
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
