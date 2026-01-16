package packets

import (
	"encoding/binary"
	"fmt"
	"io"
)

// SubackPacket represents an MQTT SUBACK control packet.
type SubackPacket struct {
	PacketID    uint16
	ReturnCodes []uint8

	// MQTT v5.0 fields
	Properties *Properties // v5.0
	Version    uint8       // 4 or 5
}

// Type returns the packet type.
func (p *SubackPacket) Type() uint8 {
	return SUBACK
}

// Encode serializes the SUBACK packet to bytes.

// WriteTo writes the SUBACK packet to the writer.
func (p *SubackPacket) WriteTo(w io.Writer) (int64, error) {
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

	// 2. Write Fixed Header
	remainingLength := variableHeaderLen + len(p.ReturnCodes)
	header := &FixedHeader{
		PacketType:      SUBACK,
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
	}

	// 4. Write Payload (Return Codes)
	n, err = w.Write(p.ReturnCodes)
	total += int64(n)
	if err != nil {
		return total, err
	}

	return total, nil
}

// DecodeSuback decodes a SUBACK packet from the buffer.
func DecodeSuback(buf []byte, version uint8) (*SubackPacket, error) {
	if len(buf) < 2 {
		return nil, fmt.Errorf("buffer too short for SUBACK packet")
	}

	pkt := &SubackPacket{
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

	// Return codes (rest of the buffer)
	if offset < len(buf) {
		pkt.ReturnCodes = make([]uint8, len(buf)-offset)
		copy(pkt.ReturnCodes, buf[offset:])
	}

	return pkt, nil
}
