package packets

import (
	"encoding/binary"
	"fmt"
	"io"
)

// encodeVarInt encodes an integer as a Variable Byte Integer (1-4 bytes).
// This is used for the Remaining Length field in the Fixed Header.
// Algorithm from MQTT v3.1.1 spec section 2.2.3
func encodeVarInt(value int) []byte {
	// Small optimization: most varints are 1 byte
	if value < 128 && value >= 0 {
		return []byte{byte(value)}
	}
	return appendVarInt(make([]byte, 0, 4), value)
}

// appendVarInt appends the Variable Byte Integer encoding of value to dst.
// It returns the extended slice.
func appendVarInt(dst []byte, value int) []byte {
	if value < 0 || value > 268435455 { // Max value: 0xFF, 0xFF, 0xFF, 0x7F
		panic(fmt.Sprintf("value %d out of range for variable byte integer", value))
	}

	for {
		digit := byte(value % 128)
		value /= 128
		if value > 0 {
			digit |= 0x80
		}
		dst = append(dst, digit)
		if value == 0 {
			break
		}
	}
	return dst
}

// decodeVarInt reads a Variable Byte Integer from the reader.
// Returns the decoded value and any error encountered.
func decodeVarInt(r io.Reader) (int, error) {
	// Wrap io.Reader as io.ByteReader if needed
	br, ok := r.(io.ByteReader)
	if !ok {
		br = &byteReader{r: r}
	}

	val, err := binary.ReadUvarint(br)
	if err != nil {
		return 0, err
	}

	// MQTT v5.0 Spec: Variable Byte Integer MUST NOT exceed 268,435,455
	if val > 268435455 {
		return 0, fmt.Errorf("variable byte integer exceeds limit")
	}

	return int(val), nil
}

// byteReader wraps an io.Reader to implement io.ByteReader
type byteReader struct {
	r   io.Reader
	buf [1]byte
}

func (br *byteReader) ReadByte() (byte, error) {
	_, err := io.ReadFull(br.r, br.buf[:])
	return br.buf[0], err
}

// decodeVarIntBuf reads a Variable Byte Integer from a byte slice.
// Returns the decoded value, number of bytes read, and error.
func decodeVarIntBuf(buf []byte) (int, int, error) {
	val, n := binary.Uvarint(buf)
	if n == 0 {
		return 0, 0, fmt.Errorf("buffer too short for variable byte integer")
	}
	if n < 0 {
		return 0, 0, fmt.Errorf("malformed variable byte integer")
	}

	// MQTT v5.0 Spec: Variable Byte Integer MUST NOT exceed 4 bytes.
	// And value must be <= 268,435,455.
	if n > 4 || val > 268435455 {
		return 0, 0, fmt.Errorf("variable byte integer exceeds limit")
	}

	return int(val), n, nil
}
