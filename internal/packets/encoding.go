package packets

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// encodeString encodes a UTF-8 string with a 2-byte length prefix (MSB first).
// This is the standard MQTT string encoding format.
func encodeString(s string) []byte {
	return appendString(make([]byte, 0, 2+len(s)), s)
}

// appendString appends a length-prefixed string to dst.
func appendString(dst []byte, s string) []byte {
	length := uint16(len(s))
	dst = append(dst, byte(length>>8), byte(length))
	return append(dst, s...)
}

// encodeBinary encodes binary data with a 2-byte length prefix (MSB first).
func encodeBinary(data []byte) []byte {
	return appendBinary(make([]byte, 0, 2+len(data)), data)
}

// appendBinary appends length-prefixed binary data to dst.
func appendBinary(dst []byte, data []byte) []byte {
	length := uint16(len(data))
	dst = append(dst, byte(length>>8), byte(length))
	return append(dst, data...)
}

// decodeString decodes an MQTT UTF-8 string (2-byte length + data).
// Returns the string, number of bytes consumed, and any error.
func decodeString(buf []byte) (string, int, error) {
	if len(buf) < 2 {
		return "", 0, fmt.Errorf("buffer too short for string length")
	}

	length := int(buf[0])<<8 | int(buf[1])
	if len(buf) < 2+length {
		return "", 0, fmt.Errorf("buffer too short for string data: need %d, have %d", 2+length, len(buf))
	}
	ret := string(buf[2 : 2+length])
	if strings.Contains(ret, "\x00") {
		return "", 0, fmt.Errorf("topic contains null byte which is not allowed")
	}
	if !utf8.ValidString(ret) {
		return "", 0, fmt.Errorf("invalid UTF-8 string")
	}

	return ret, 2 + length, nil
}

// decodeBinary reads length-prefixed binary data from the buffer.
// Returns the data, number of bytes consumed, and any error.
func decodeBinary(buf []byte) ([]byte, int, error) {
	if len(buf) < 2 {
		return nil, 0, fmt.Errorf("buffer too short for binary length")
	}

	length := int(buf[0])<<8 | int(buf[1])
	if len(buf) < 2+length {
		return nil, 0, fmt.Errorf("buffer too short for binary data: need %d, have %d", 2+length, len(buf))
	}

	return buf[2 : 2+length], 2 + length, nil
}
