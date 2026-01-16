package packets

import (
	"fmt"
	"io"
)

// FixedHeader represents the fixed header present in all MQTT control packets.
// Format: [PacketType + Flags (1 byte)][Remaining Length (1-4 bytes)]
type FixedHeader struct {
	PacketType      uint8
	Flags           uint8
	RemainingLength int
}

// WriteTo writes the fixed header to the writer.
func (h *FixedHeader) WriteTo(w io.Writer) (int64, error) {
	firstByte := (h.PacketType << 4) | (h.Flags & 0x0F)

	// Optimization: If writer supports WriteByte, use it to avoid slice allocation
	if bw, ok := w.(io.ByteWriter); ok {
		var totalBytesWritten int64 = 0

		if err := bw.WriteByte(firstByte); err != nil {
			return totalBytesWritten, err
		}
		totalBytesWritten++

		x := h.RemainingLength

		// Encode varint byte by byte
		for {
			b := byte(x % 128)
			x /= 128
			if x > 0 {
				b |= 128
			}
			if err := bw.WriteByte(b); err != nil {
				return totalBytesWritten, err
			}
			totalBytesWritten++

			if x == 0 {
				break
			}
		}
		return totalBytesWritten, nil
	}

	// Fallback for non-ByteWriter
	// Create a small buffer for the header: 1 byte type+flags + max 4 bytes length
	var buf [5]byte
	buf[0] = firstByte

	// Encode remaining length directly
	// Similar to encodeVarInt but writing to our stack buffer
	// Adapted from varint.go logic to avoid allocating slice
	x := h.RemainingLength
	n := 1 // Start at buf[1]

	for {
		b := byte(x % 128)
		x /= 128
		if x > 0 {
			b |= 128
		}
		buf[n] = b
		n++

		if x == 0 {
			break
		}
	}

	nw, err := w.Write(buf[:n])
	return int64(nw), err
}

// DecodeFixedHeader reads and decodes a fixed header from the reader.
func DecodeFixedHeader(r io.Reader) (*FixedHeader, error) {
	var buf [1]byte

	if _, err := io.ReadFull(r, buf[:]); err != nil {
		return nil, err
	}

	firstByte := buf[0]
	packetType := firstByte >> 4
	flags := firstByte & 0x0F

	remainingLength, err := decodeVarInt(r)
	if err != nil {
		return nil, fmt.Errorf("failed to decode remaining length: %w", err)
	}

	return &FixedHeader{
		PacketType:      packetType,
		Flags:           flags,
		RemainingLength: remainingLength,
	}, nil
}
