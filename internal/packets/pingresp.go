package packets

import (
	"io"
)

// PingrespPacket represents an MQTT PINGRESP control packet.
type PingrespPacket struct{}

// Type returns the packet type.
func (p *PingrespPacket) Type() uint8 {
	return PINGRESP
}

// Encode serializes the PINGRESP packet to bytes.

// WriteTo writes the PINGRESP packet to the writer.
func (p *PingrespPacket) WriteTo(w io.Writer) (int64, error) {
	header := &FixedHeader{
		PacketType:      PINGRESP,
		Flags:           0,
		RemainingLength: 0,
	}

	_, err := header.WriteTo(w)
	return 0, err
}

// DecodePingresp decodes a PINGRESP packet (no payload).
func DecodePingresp(buf []byte) (*PingrespPacket, error) {
	return &PingrespPacket{}, nil
}
