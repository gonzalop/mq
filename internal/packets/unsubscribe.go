package packets

import (
	"encoding/binary"
	"fmt"
	"io"
)

// UnsubscribePacket represents an MQTT UNSUBSCRIBE control packet.
type UnsubscribePacket struct {
	PacketID uint16
	Topics   []string

	// MQTT v5.0 fields
	Properties *Properties // v5.0
	Version    uint8       // 4 or 5
}

// Type returns the packet type.
func (p *UnsubscribePacket) Type() uint8 {
	return UNSUBSCRIBE
}

// Encode serializes the UNSUBSCRIBE packet to bytes.

// WriteTo writes the UNSUBSCRIBE packet to the writer.
func (p *UnsubscribePacket) WriteTo(w io.Writer) (int64, error) {
	var total int64

	// 1. Calculate Variable Header length
	var packetIDBytes [2]byte
	var propsBytes []byte
	var propsLen int

	// MQTT v5.0 Properties
	if p.Version >= 5 {
		propsBytes = encodeProperties(p.Properties)
		propsLen = len(propsBytes)
	}

	variableHeaderLen := 2 + propsLen // PacketID + Props

	// 2. Calculate Payload Length
	var payloadLen int
	var topicBytesList [][]byte

	for _, topic := range p.Topics {
		tb := encodeString(topic)
		topicBytesList = append(topicBytesList, tb)
		payloadLen += len(tb)
	}

	// 3. Write Fixed Header
	// UNSUBSCRIBE has fixed header flags = 0x02 (bit 1 set)
	remainingLength := variableHeaderLen + payloadLen
	header := &FixedHeader{
		PacketType:      UNSUBSCRIBE,
		Flags:           0x02,
		RemainingLength: remainingLength,
	}

	hN, err := header.WriteTo(w)
	total += hN
	if err != nil {
		return total, err
	}
	var n int

	// 4. Write Variable Header
	// Packet ID
	binary.BigEndian.PutUint16(packetIDBytes[:], p.PacketID)
	n, err = w.Write(packetIDBytes[:])
	total += int64(n)
	if err != nil {
		return total, err
	}

	// Properties (v5.0)
	if p.Version >= 5 {
		n, err = w.Write(propsBytes)
		total += int64(n)
		if err != nil {
			return total, err
		}
	}

	// 5. Write Payload
	for _, tb := range topicBytesList {
		n, err = w.Write(tb)
		total += int64(n)
		if err != nil {
			return total, err
		}
	}

	return total, nil
}

// DecodeUnsubscribe decodes an UNSUBSCRIBE packet from the buffer.
func DecodeUnsubscribe(buf []byte, version uint8) (*UnsubscribePacket, error) {
	if len(buf) < 2 {
		return nil, fmt.Errorf("buffer too short for UNSUBSCRIBE packet")
	}

	pkt := &UnsubscribePacket{
		Version: version,
	}

	offset := 0

	// Packet ID
	pkt.PacketID = binary.BigEndian.Uint16(buf[offset : offset+2])
	offset += 2

	// v5.0 Properties
	if version >= 5 {
		if offset >= len(buf) {
			return nil, fmt.Errorf("buffer too short for properties length")
		}
		props, n, err := decodeProperties(buf[offset:])
		if err != nil {
			return nil, fmt.Errorf("failed to decode properties: %w", err)
		}
		pkt.Properties = props
		offset += n
	}

	// Topic filters
	for offset < len(buf) {
		topic, n, err := decodeString(buf[offset:])
		if err != nil {
			return nil, fmt.Errorf("failed to decode topic filter: %w", err)
		}
		offset += n

		pkt.Topics = append(pkt.Topics, topic)
	}

	return pkt, nil
}
