package packets

import (
	"io"
)

// PingreqPacket represents an MQTT PINGREQ control packet.
type PingreqPacket struct{}

// Type returns the packet type.
func (p *PingreqPacket) Type() uint8 {
	return PINGREQ
}

// Encode serializes the PINGREQ packet to bytes.

// WriteTo writes the PINGREQ packet to the writer.
func (p *PingreqPacket) WriteTo(w io.Writer) (int64, error) {
	header := &FixedHeader{
		PacketType:      PINGREQ,
		Flags:           0,
		RemainingLength: 0,
	}

	_, err := header.WriteTo(w)
	return 0, err
}

// DecodePingreq decodes a PINGREQ packet (no payload).
func DecodePingreq(buf []byte) (*PingreqPacket, error) {
	return &PingreqPacket{}, nil
}
