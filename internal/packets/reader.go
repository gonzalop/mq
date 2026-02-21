package packets

import (
	"fmt"
	"io"
)

// PacketDecoder is a function that decodes a packet from remaining bytes and header.
// It accepts the protocol version (4 for v3.1.1, 5 for v5.0).
type PacketDecoder func(remaining []byte, header *FixedHeader, version uint8) (Packet, error)

// packetDecoders maps packet types to their decoder functions.
var packetDecoders = map[uint8]PacketDecoder{
	CONNECT: func(remaining []byte, _ *FixedHeader, _ uint8) (Packet, error) { return DecodeConnect(remaining) },
	CONNACK: func(remaining []byte, _ *FixedHeader, v uint8) (Packet, error) { return DecodeConnack(remaining, v) },
	PUBLISH: func(remaining []byte, header *FixedHeader, v uint8) (Packet, error) {
		return DecodePublish(remaining, header, v)
	},
	PUBACK:    func(remaining []byte, _ *FixedHeader, v uint8) (Packet, error) { return DecodePuback(remaining, v) },
	PUBREC:    func(remaining []byte, _ *FixedHeader, v uint8) (Packet, error) { return DecodePubrec(remaining, v) },
	PUBREL:    func(remaining []byte, _ *FixedHeader, v uint8) (Packet, error) { return DecodePubrel(remaining, v) },
	PUBCOMP:   func(remaining []byte, _ *FixedHeader, v uint8) (Packet, error) { return DecodePubcomp(remaining, v) },
	SUBSCRIBE: func(remaining []byte, _ *FixedHeader, v uint8) (Packet, error) { return DecodeSubscribe(remaining, v) },
	SUBACK:    func(remaining []byte, _ *FixedHeader, v uint8) (Packet, error) { return DecodeSuback(remaining, v) },
	UNSUBSCRIBE: func(remaining []byte, _ *FixedHeader, v uint8) (Packet, error) {
		return DecodeUnsubscribe(remaining, v)
	},
	UNSUBACK:   func(remaining []byte, _ *FixedHeader, v uint8) (Packet, error) { return DecodeUnsuback(remaining, v) },
	PINGREQ:    func(remaining []byte, _ *FixedHeader, _ uint8) (Packet, error) { return DecodePingreq(remaining) },
	PINGRESP:   func(remaining []byte, _ *FixedHeader, _ uint8) (Packet, error) { return DecodePingresp(remaining) },
	DISCONNECT: func(remaining []byte, _ *FixedHeader, v uint8) (Packet, error) { return DecodeDisconnect(remaining, v) },
	AUTH:       func(remaining []byte, _ *FixedHeader, v uint8) (Packet, error) { return DecodeAuth(remaining, v) },
}

// ReadPacket reads a complete MQTT packet from the reader.
// It accepts the protocol version to correctly decode version-specific fields (like Properties).
// The maxIncomingPacket parameter sets the maximum allowed packet size. If 0 or exceeding the
// MQTT spec maximum (268435455 bytes), the spec maximum is used.
func ReadPacket(r io.Reader, version uint8, maxIncomingPacket int) (Packet, error) {
	header, err := DecodeFixedHeader(r)
	if err != nil {
		return nil, fmt.Errorf("failed to decode fixed header: %w", err)
	}

	// Validate packet size
	// MQTT spec maximum: 268435455 bytes (256MB - 1 byte)
	const mqttSpecMax = 268435455
	maxPacketSize := maxIncomingPacket
	if maxPacketSize <= 0 || maxPacketSize > mqttSpecMax {
		maxPacketSize = mqttSpecMax
	}
	if header.RemainingLength > maxPacketSize {
		return nil, fmt.Errorf("packet size %d exceeds maximum %d", header.RemainingLength, maxPacketSize)
	}

	var remaining []byte
	var bufPtr *[]byte

	if header.RemainingLength > 0 {
		bufPtr = GetBuffer(header.RemainingLength)
		remaining = (*bufPtr)[:header.RemainingLength]

		if _, err := io.ReadFull(r, remaining); err != nil {
			PutBuffer(bufPtr)
			return nil, fmt.Errorf("failed to read packet body: %w", err)
		}
	}

	decoder, ok := packetDecoders[header.PacketType]
	if !ok {
		if bufPtr != nil {
			PutBuffer(bufPtr)
		}
		return nil, fmt.Errorf("unknown packet type: %d", header.PacketType)
	}

	pkt, err := decoder(remaining, &header, version)

	if bufPtr != nil {
		PutBuffer(bufPtr)
	}

	return pkt, err
}
