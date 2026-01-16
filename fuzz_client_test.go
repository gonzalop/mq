package mq

import (
	"bytes"
	"io"
	"log/slog"
	"testing"

	"github.com/gonzalop/mq/internal/packets"
)

// FuzzClientHandleIncoming fuzzes the critical path of packet processing:
// 1. Packet Decoding (via packets.ReadPacket)
// 2. Packet Handling logic (Client.handleIncoming)
//
// This ensures that:
// - The parser doesn't crash on garbage inputs
// - The handler doesn't crash on unexpected packet structures (nil properties, etc.)
// - State machines (like QoS 2) don't deadlock or panic
func FuzzClientHandleIncoming(f *testing.F) {
	// Seed with various packet types
	f.Add([]byte{0x20, 0x02, 0x00, 0x00})       // CONNACK success
	f.Add([]byte{0x30, 0x03, 0x00, 0x01, 'a'})  // PUBLISH QoS 0
	f.Add([]byte{0x40, 0x02, 0x00, 0x01})       // PUBACK
	f.Add([]byte{0x90, 0x03, 0x00, 0x01, 0x00}) // SUBACK
	f.Add([]byte{0xb0, 0x00})                   // UNSUBACK
	f.Add([]byte{0xd0, 0x00})                   // PINGRESP
	f.Add([]byte{0x50, 0x02, 0x00, 0x01})       // PUBREC
	f.Add([]byte{0x60, 0x02, 0x00, 0x01})       // PUBREL
	f.Add([]byte{0x70, 0x02, 0x00, 0x01})       // PUBCOMP
	f.Add([]byte{0xe0, 0x00})                   // DISCONNECT v3
	f.Add([]byte{0xe0, 0x02, 0x00, 0x00})       // DISCONNECT v5 with reason

	f.Fuzz(func(t *testing.T, data []byte) {
		// 1. Setup a dummy client
		c := &Client{
			opts: &clientOptions{
				ProtocolVersion: ProtocolV50, // Test v5.0 to exercise property logic
				Logger:          slog.New(slog.NewTextHandler(io.Discard, nil)),
			},
			incoming:        make(chan packets.Packet, 1),
			outgoing:        make(chan packets.Packet, 10), // Buffer outgoing to prevent block
			pending:         make(map[uint16]*pendingOp),
			subscriptions:   make(map[string]subscriptionEntry),
			receivedQoS2:    make(map[uint16]struct{}),
			topicAliases:    make(map[string]uint16),
			receivedAliases: make(map[uint16]string),
		}

		// 2. Decode packet from fuzz data
		r := bytes.NewReader(data)
		pkt, err := packets.ReadPacket(r, 5, 0)
		if err != nil {
			return // Invalid packet, parser rejected it safely
		}

		// 3. Process the packet
		// We call handleIncoming directly. In a real client, this runs in logicLoop.
		// This should NEVER panic.
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("panic processing packet %T: %v", pkt, r)
				}
			}()
			c.handleIncoming(pkt)
		}()
	})
}
