package packets

import "io"

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

// WriteTo writes the PUBACK packet to the writer.
func (p *PubackPacket) WriteTo(w io.Writer) (int64, error) {
	return writeAckPacket(w, PUBACK, 0, p.PacketID, p.Version, p.ReasonCode, p.Properties)
}

// DecodePuback decodes a PUBACK packet from the buffer.
func DecodePuback(buf []byte, version uint8) (*PubackPacket, error) {
	packetID, reasonCode, props, err := decodeAckPacket(buf, version, "PUBACK")
	if err != nil {
		return nil, err
	}
	return &PubackPacket{
		PacketID:   packetID,
		ReasonCode: reasonCode,
		Properties: props,
		Version:    version,
	}, nil
}
