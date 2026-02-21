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

// Encode serializes the PUBACK packet into dst.
func (p *PubackPacket) Encode(dst []byte) ([]byte, error) {
	// 1. Calculate Variable Header length
	var propsLen int
	if p.Version >= 5 {
		if p.ReasonCode != 0 || p.Properties != nil {
			var propBuf [128]byte
			encodedProps := appendProperties(propBuf[:0], p.Properties)
			propsLen = len(encodedProps)
		}
	}

	variableHeaderLen := 2
	if p.Version >= 5 {
		if p.ReasonCode != 0 || p.Properties != nil {
			variableHeaderLen += 1 + propsLen // ReasonCode + Props
		}
	}

	// 2. Write Fixed Header
	header := FixedHeader{
		PacketType:      PUBACK,
		Flags:           0,
		RemainingLength: variableHeaderLen,
	}
	dst = header.appendBytes(dst)

	// 3. Write Variable Header
	// Packet ID
	dst = binary.BigEndian.AppendUint16(dst, p.PacketID)

	// MQTT v5.0
	if p.Version >= 5 {
		if p.ReasonCode != 0 || p.Properties != nil {
			dst = append(dst, p.ReasonCode)
			dst = appendProperties(dst, p.Properties)
		}
	}

	return dst, nil
}

// WriteTo writes the PUBACK packet to the writer.
func (p *PubackPacket) WriteTo(w io.Writer) (int64, error) {
	bufPtr := GetBuffer(4096)
	defer PutBuffer(bufPtr)

	data, err := p.Encode((*bufPtr)[:0])
	if err != nil {
		return 0, err
	}
	n, err := w.Write(data)
	return int64(n), err
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
