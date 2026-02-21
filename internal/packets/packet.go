package packets

import "io"

// Packet is the interface that all MQTT control packets must implement.
type Packet interface {
	// Type returns the MQTT control packet type.
	Type() uint8

	// WriteTo writes the packet to the writer.
	// It returns the number of bytes written and any error encountered.
	WriteTo(w io.Writer) (int64, error)
}

// Encoder is an optional interface that packets can implement to provide
// more efficient, allocation-free serialization.
type Encoder interface {
	// Encode appends the serialized packet bytes to dst and returns the resulting slice.
	Encode(dst []byte) ([]byte, error)
}
