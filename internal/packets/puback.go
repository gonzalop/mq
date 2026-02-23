package packets

import (
	"encoding/binary"
	"fmt"
	"io"
)

// PubackPacket represents an MQTT PUBACK control packet (QoS 1 acknowledgment).
type PubackPacket struct {
	PacketID uint16

	// MQTT v5.0 fields
	ReasonCode uint8       // v5.0
	Properties *Properties // v5.0
	Version    uint8       // 4 or 5
}

// Type returns the packet type.
func (p *PubackPacket) Type() uint8 {
	return PUBACK
}

// Encode serializes the PUBACK packet to bytes.

// WriteTo writes the PUBACK packet to the writer.
func (p *PubackPacket) WriteTo(w io.Writer) (int64, error) {
	var total int64

	// 1. Calculate Variable Header length
	var packetIDBytes [2]byte
	var propsBytes []byte
	var propsLen int

	// MQTT v5.0
	if p.Version >= 5 {
		if p.ReasonCode != 0 || p.Properties != nil {
			propsBytes = encodeProperties(p.Properties)
			propsLen = len(propsBytes)
		}
	}

	variableHeaderLen := 2
	if p.Version >= 5 {
		if p.ReasonCode != 0 || p.Properties != nil {
			variableHeaderLen += 1 + propsLen // ReasonCode + Props
		}
	}

	// 2. Write Fixed Header
	header := &FixedHeader{
		PacketType:      PUBACK,
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
	// Packet ID
	binary.BigEndian.PutUint16(packetIDBytes[:], p.PacketID)
	n, err = w.Write(packetIDBytes[:])
	total += int64(n)
	if err != nil {
		return total, err
	}

	// MQTT v5.0
	if p.Version >= 5 {
		if p.ReasonCode != 0 || p.Properties != nil {
			if err := binary.Write(w, binary.BigEndian, p.ReasonCode); err != nil {
				return total, err
			}
			total++

			n, err = w.Write(propsBytes)
			total += int64(n)
			if err != nil {
				return total, err
			}
		}
	}

	return total, nil
}

// DecodePuback decodes a PUBACK packet from the buffer.
func DecodePuback(buf []byte, version uint8) (*PubackPacket, error) {
	if len(buf) < 2 {
		return nil, fmt.Errorf("buffer too short for PUBACK packet")
	}

	pkt := &PubackPacket{
		Version: version,
	}

	pkt.PacketID = binary.BigEndian.Uint16(buf[0:2])

	// v5.0 fields
	if version >= 5 && len(buf) > 2 {
		pkt.ReasonCode = buf[2]
		if len(buf) > 3 {
			props, _, err := decodeProperties(buf[3:])
			if err != nil {
				return nil, fmt.Errorf("failed to decode properties: %w", err)
			}
			pkt.Properties = props
		}
	}

	return pkt, nil
}
