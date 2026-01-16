package packets

import (
	"encoding/binary"
	"fmt"
	"io"
)

// UnsubackPacket represents an MQTT UNSUBACK control packet.
type UnsubackPacket struct {
	PacketID uint16

	// MQTT v5.0 fields
	ReasonCodes []uint8     // v5.0
	Properties  *Properties // v5.0
	Version     uint8       // 4 or 5
}

// Type returns the packet type.
func (p *UnsubackPacket) Type() uint8 {
	return UNSUBACK
}

// Encode serializes the UNSUBACK packet to bytes.

// WriteTo writes the UNSUBACK packet to the writer.
func (p *UnsubackPacket) WriteTo(w io.Writer) (int64, error) {
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

	variableHeaderLen := 2
	if p.Version >= 5 {
		variableHeaderLen += propsLen // PacketID + Props
	}

	// 2. Write Fixed Header
	remainingLength := variableHeaderLen
	// In v5.0, payload is Reason Codes
	if p.Version >= 5 {
		remainingLength += len(p.ReasonCodes)
	}

	header := &FixedHeader{
		PacketType:      UNSUBACK,
		Flags:           0,
		RemainingLength: remainingLength,
	}

	hN, err := header.WriteTo(w)
	total += hN
	if err != nil {
		return total, err
	}
	var n int

	// 3. Write Variable Header
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

		// 4. Write Payload (Reason Codes) - v5.0 only
		n, err = w.Write(p.ReasonCodes)
		total += int64(n)
		if err != nil {
			return total, err
		}
	}

	return total, nil
}

// DecodeUnsuback decodes an UNSUBACK packet from the buffer.
func DecodeUnsuback(buf []byte, version uint8) (*UnsubackPacket, error) {
	if len(buf) < 2 {
		return nil, fmt.Errorf("buffer too short for UNSUBACK packet")
	}

	pkt := &UnsubackPacket{
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

	// Reason Codes (Payload)
	if offset < len(buf) {
		pkt.ReasonCodes = make([]uint8, len(buf)-offset)
		copy(pkt.ReasonCodes, buf[offset:])
	}

	return pkt, nil
}
