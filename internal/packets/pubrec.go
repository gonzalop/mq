package packets

import "io"

// PubrecPacket represents an MQTT PUBREC control packet (QoS 2, step 1).
type PubrecPacket struct {
	PacketID uint16

	// MQTT v5.0 fields
	ReasonCode uint8       // v5.0
	Properties *Properties // v5.0
	Version    uint8       // 4 or 5
}

// Type returns the packet type.
func (p *PubrecPacket) Type() uint8 {
	return PUBREC
}

// WriteTo writes the PUBREC packet to the writer.
func (p *PubrecPacket) WriteTo(w io.Writer) (int64, error) {
	return writeAckPacket(w, PUBREC, 0, p.PacketID, p.Version, p.ReasonCode, p.Properties)
}

// DecodePubrec decodes a PUBREC packet from the buffer.
func DecodePubrec(buf []byte, version uint8) (*PubrecPacket, error) {
	packetID, reasonCode, props, err := decodeAckPacket(buf, version, "PUBREC")
	if err != nil {
		return nil, err
	}
	return &PubrecPacket{
		PacketID:   packetID,
		ReasonCode: reasonCode,
		Properties: props,
		Version:    version,
	}, nil
}
