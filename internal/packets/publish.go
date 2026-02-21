package packets

import (
	"encoding/binary"
	"fmt"
	"io"
)

// PublishPacket represents an MQTT PUBLISH control packet.
type PublishPacket struct {
	// Fixed header flags
	Dup    bool
	QoS    uint8
	Retain bool

	// Variable header
	Topic         string
	OriginalTopic string // Original topic if Topic is emptied for alias
	PacketID      uint16 // Only present if QoS > 0

	// Payload
	Payload []byte

	// MQTT v5.0 fields
	Properties *Properties
	Version    uint8 // 4 for v3.1.1, 5 for v5.0

	// UseAlias indicates whether to use topic alias for this publish.
	// This is set by WithAlias() and processed by the client.
	UseAlias bool
}

// Type returns the packet type.
func (p *PublishPacket) Type() uint8 {
	return PUBLISH
}

// Encode serializes the PUBLISH packet into dst.
func (p *PublishPacket) Encode(dst []byte) ([]byte, error) {
	// 1. Calculate variable header length
	var topicLen int = 2 + len(p.Topic)
	var propertyLen int

	if p.Version >= 5 {
		// Temporary properties encoding to get length.
		// Use a stack-allocated buffer for typical small properties.
		var propBuf [128]byte
		encodedProps := appendProperties(propBuf[:0], p.Properties)
		propertyLen = len(encodedProps)
	}

	variableHeaderLen := topicLen
	if p.QoS > 0 {
		variableHeaderLen += 2
	}
	if p.Version >= 5 {
		variableHeaderLen += propertyLen
	}

	// 2. Calculate remaining length
	remainingLength := variableHeaderLen + len(p.Payload)

	// 3. Write fixed header
	var flags uint8
	if p.Dup {
		flags |= 0x08
	}
	flags |= (p.QoS & 0x03) << 1
	if p.Retain {
		flags |= 0x01
	}

	header := FixedHeader{
		PacketType:      PUBLISH,
		Flags:           flags,
		RemainingLength: remainingLength,
	}

	dst = header.appendBytes(dst)

	// 4. Write variable header
	// Topic
	dst = appendString(dst, p.Topic)

	// Packet ID
	if p.QoS > 0 {
		dst = binary.BigEndian.AppendUint16(dst, p.PacketID)
	}

	// Properties
	if p.Version >= 5 {
		dst = appendProperties(dst, p.Properties)
	}

	// 5. Write payload
	dst = append(dst, p.Payload...)

	return dst, nil
}

// WriteTo writes the PUBLISH packet to the writer.
func (p *PublishPacket) WriteTo(w io.Writer) (int64, error) {
	bufPtr := GetBuffer(4096)
	defer PutBuffer(bufPtr)

	data, err := p.Encode((*bufPtr)[:0])
	if err != nil {
		return 0, err
	}
	n, err := w.Write(data)
	return int64(n), err
}

// DecodePublish decodes a PUBLISH packet from the buffer and fixed header.
func DecodePublish(buf []byte, fixedHeader *FixedHeader, version uint8) (*PublishPacket, error) {
	pkt := &PublishPacket{
		Version: version,
	}

	// Extract flags from fixed header
	pkt.Dup = (fixedHeader.Flags & 0x08) != 0
	pkt.QoS = (fixedHeader.Flags >> 1) & 0x03
	pkt.Retain = (fixedHeader.Flags & 0x01) != 0

	offset := 0

	// Topic name
	topic, n, err := decodeString(buf[offset:])
	if err != nil {
		return nil, fmt.Errorf("failed to decode topic: %w", err)
	}
	pkt.Topic = topic
	offset += n

	// Packet ID (only for QoS > 0)
	if pkt.QoS > 0 {
		if offset+2 > len(buf) {
			return nil, fmt.Errorf("buffer too short for packet ID")
		}
		pkt.PacketID = binary.BigEndian.Uint16(buf[offset : offset+2])
		offset += 2
	}

	// Properties (v5.0 only)
	if version >= 5 {
		props, nProps, err := decodeProperties(buf[offset:])
		if err != nil {
			return nil, fmt.Errorf("failed to decode properties: %w", err)
		}
		pkt.Properties = props
		offset += nProps
	}

	// Payload (rest of the buffer)
	pkt.Payload = make([]byte, len(buf)-offset)
	copy(pkt.Payload, buf[offset:])

	return pkt, nil
}
