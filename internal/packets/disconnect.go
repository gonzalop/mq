package packets

import (
	"encoding/binary"
	"fmt"
	"io"
)

// DisconnectPacket represents an MQTT DISCONNECT control packet.
// DisconnectPacket represents an MQTT DISCONNECT control packet.
type DisconnectPacket struct {
	// MQTT v5.0 fields
	ReasonCode uint8       // v5.0
	Properties *Properties // v5.0
	Version    uint8       // 4 or 5
}

// Type returns the packet type.
func (p *DisconnectPacket) Type() uint8 {
	return DISCONNECT
}

// Encode serializes the DISCONNECT packet to bytes.

// WriteTo writes the DISCONNECT packet to the writer.
func (p *DisconnectPacket) WriteTo(w io.Writer) (int64, error) {
	var total int64

	// 1. Calculate Variable Header length
	var propsBytes []byte
	var propsLen int

	// MQTT v5.0
	if p.Version >= 5 {
		if p.ReasonCode != 0 || p.Properties != nil {
			propsBytes = encodeProperties(p.Properties)
			propsLen = len(propsBytes)
		}
	}

	variableHeaderLen := 0
	if p.Version >= 5 {
		if p.ReasonCode != 0 || p.Properties != nil {
			variableHeaderLen += 1 + propsLen // ReasonCode + Props
		}
	}

	// 2. Write Fixed Header
	header := &FixedHeader{
		PacketType:      DISCONNECT,
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

// DecodeDisconnect decodes a DISCONNECT packet.
func DecodeDisconnect(buf []byte, version uint8) (*DisconnectPacket, error) {
	pkt := &DisconnectPacket{
		Version: version,
	}

	// v5.0 fields
	if version >= 5 && len(buf) > 0 {
		pkt.ReasonCode = buf[0]
		if len(buf) > 1 {
			props, _, err := decodeProperties(buf[1:])
			if err != nil {
				return nil, fmt.Errorf("failed to decode properties: %w", err)
			}
			pkt.Properties = props
		}
	}

	return pkt, nil
}
