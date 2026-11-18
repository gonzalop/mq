package packets

import "io"

// PubcompPacket represents an MQTT PUBCOMP control packet (QoS 2, step 3).
type PubcompPacket struct {
	PacketID uint16

	// MQTT v5.0 fields
	ReasonCode uint8       // v5.0
	Properties *Properties // v5.0
	Version    uint8       // 4 or 5
}

// Type returns the packet type.
func (p *PubcompPacket) Type() uint8 {
	return PUBCOMP
}

// WriteTo writes the PUBCOMP packet to the writer.
func (p *PubcompPacket) WriteTo(w io.Writer) (int64, error) {
	return writeAckPacket(w, PUBCOMP, 0, p.PacketID, p.Version, p.ReasonCode, p.Properties)
}

// DecodePubcomp decodes a PUBCOMP packet from the buffer.
func DecodePubcomp(buf []byte, version uint8) (*PubcompPacket, error) {
	packetID, reasonCode, props, err := decodeAckPacket(buf, version, "PUBCOMP")
	if err != nil {
		return nil, err
	}
	return &PubcompPacket{
		PacketID:   packetID,
		ReasonCode: reasonCode,
		Properties: props,
		Version:    version,
	}, nil
}
