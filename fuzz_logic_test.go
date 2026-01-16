package mq

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/gonzalop/mq/internal/packets"
)

// FuzzPacketSequence generates sequences of valid MQTT packets to test the Client state machine.
func FuzzPacketSequence(f *testing.F) {
	// Seed with valid packet type sequences (using uint8 IDs)
	// 2 = CONNACK, 3 = PUBLISH, 4 = PUBACK, 5 = PUBREC, 6 = PUBREL, 7 = PUBCOMP, 9 = SUBACK, 11 = UNSUBACK
	f.Add([]byte{2, 3, 4})    // CONNACK, then QoS 0 PUBLISH, then PUBACK
	f.Add([]byte{3, 5, 6, 7}) // QoS 2 flow

	f.Fuzz(func(t *testing.T, sequence []byte) {
		c := &Client{
			opts: &clientOptions{
				ProtocolVersion: ProtocolV50,
				Logger:          slog.New(slog.NewTextHandler(io.Discard, nil)),
			},
			incoming:      make(chan packets.Packet, 100),
			outgoing:      make(chan packets.Packet, 100),
			pending:       make(map[uint16]*pendingOp),
			subscriptions: make(map[string]subscriptionEntry),
			receivedQoS2:  make(map[uint16]struct{}),
		}

		for _, pType := range sequence {
			var pkt packets.Packet
			packetID := uint16(1) // Constant for simplicity in sequence

			switch pType % 16 {
			case packets.CONNACK:
				pkt = &packets.ConnackPacket{ReturnCode: 0}
			case packets.PUBLISH:
				pkt = &packets.PublishPacket{PacketID: packetID, QoS: 1, Topic: "test"}
			case packets.PUBACK:
				pkt = &packets.PubackPacket{PacketID: packetID}
			case packets.PUBREC:
				pkt = &packets.PubrecPacket{PacketID: packetID}
			case packets.PUBREL:
				pkt = &packets.PubrelPacket{PacketID: packetID}
			case packets.PUBCOMP:
				pkt = &packets.PubcompPacket{PacketID: packetID}
			case packets.SUBACK:
				pkt = &packets.SubackPacket{PacketID: packetID, ReturnCodes: []uint8{0}}
			case packets.UNSUBACK:
				pkt = &packets.UnsubackPacket{PacketID: packetID}
			case packets.PINGRESP:
				pkt = &packets.PingrespPacket{}
			case packets.DISCONNECT:
				pkt = &packets.DisconnectPacket{}
			default:
				continue
			}

			// Simulate packet arrival
			// We call handleIncoming directly to avoid needing logicLoop goroutine
			func() {
				defer func() {
					if r := recover(); r != nil {
						t.Errorf("Recovered from panic during packet %d handling: %v", pType, r)
					}
				}()
				c.handleIncoming(pkt)
			}()
		}

		// Ensure we can still disconnect
		_ = c.Disconnect(context.Background())
	})
}
