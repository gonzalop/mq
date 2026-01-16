package packets

import (
	"encoding/binary"
	"fmt"
	"io"
)

// ConnackPacket represents an MQTT CONNACK control packet.
type ConnackPacket struct {
	// Session present flag (v3.1.1+)
	SessionPresent bool

	// Return code
	ReturnCode uint8

	// MQTT v5.0 fields
	Properties *Properties
}

// Type returns the packet type.
func (p *ConnackPacket) Type() uint8 {
	return CONNACK
}

// Encode serializes the CONNACK packet to bytes.

// WriteTo writes the CONNACK packet to the writer.
func (p *ConnackPacket) WriteTo(w io.Writer) (int64, error) {
	var total int64

	// 1. Calculate Variable Header length
	var propsBytes []byte
	var propsLen int

	// MQTT v5.0 Properties
	if p.Properties != nil {
		propsBytes = encodeProperties(p.Properties)
		propsLen = len(propsBytes)
	}

	variableHeaderLen := 1 + 1 // Ack Flags + Return Code
	variableHeaderLen += propsLen

	// 2. Write Fixed Header
	header := &FixedHeader{
		PacketType:      CONNACK,
		Flags:           0,
		RemainingLength: variableHeaderLen,
	}

	hN, err := header.WriteTo(w)
	total += hN
	if err != nil {
		return total, err
	}
	var n int

	// 3. Write Variable Header
	// Connect acknowledge flags
	var ackFlags uint8
	if p.SessionPresent {
		ackFlags |= 0x01
	}
	if err := binary.Write(w, binary.BigEndian, ackFlags); err != nil {
		return total, err
	}
	total++

	// Return code
	if err := binary.Write(w, binary.BigEndian, p.ReturnCode); err != nil {
		return total, err
	}
	total++

	// Properties
	if p.Properties != nil {
		n, err = w.Write(propsBytes)
		total += int64(n)
		if err != nil {
			return total, err
		}
	}

	return total, nil
}

// DecodeConnack decodes a CONNACK packet from the buffer.
func DecodeConnack(buf []byte, version uint8) (*ConnackPacket, error) {
	if len(buf) < 2 {
		return nil, fmt.Errorf("buffer too short for CONNACK packet")
	}

	pkt := &ConnackPacket{}

	// Connect acknowledge flags
	ackFlags := buf[0]
	pkt.SessionPresent = (ackFlags & 0x01) != 0

	// Return code
	pkt.ReturnCode = buf[1]

	// Properties (v5.0 only)
	if version >= 5 && len(buf) > 2 {
		props, _, err := decodeProperties(buf[2:])
		if err != nil {
			return nil, fmt.Errorf("failed to decode properties: %w", err)
		}
		pkt.Properties = props
	}

	return pkt, nil
}
