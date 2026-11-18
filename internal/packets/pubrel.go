package packets

import "io"

// PubrelPacket represents an MQTT PUBREL control packet (QoS 2, step 2).
type PubrelPacket struct {
	PacketID uint16

	// MQTT v5.0 fields
	ReasonCode uint8       // v5.0
	Properties *Properties // v5.0
	Version    uint8       // 4 or 5
}

// Type returns the packet type.
func (p *PubrelPacket) Type() uint8 {
	return PUBREL
}

// WriteTo writes the PUBREL packet to the writer.
func (p *PubrelPacket) WriteTo(w io.Writer) (int64, error) {
	// PUBREL has fixed header flags = 0x02 (bit 1 set).
	return writeAckPacket(w, PUBREL, 0x02, p.PacketID, p.Version, p.ReasonCode, p.Properties)
}

// DecodePubrel decodes a PUBREL packet from the buffer.
func DecodePubrel(buf []byte, version uint8) (*PubrelPacket, error) {
	packetID, reasonCode, props, err := decodeAckPacket(buf, version, "PUBREL")
	if err != nil {
		return nil, err
	}
	return &PubrelPacket{
		PacketID:   packetID,
		ReasonCode: reasonCode,
		Properties: props,
		Version:    version,
	}, nil
}
