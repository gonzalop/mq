package packets

import (
	"bytes"
	"testing"
)

// FuzzDecodeProperties fuzzes MQTT v5.0 properties decoding
func FuzzDecodeProperties(f *testing.F) {
	// Seed with valid property examples
	f.Add([]byte{0x00}) // Empty properties (length 0)

	// PayloadFormatIndicator (0x01) = 1
	f.Add([]byte{0x02, 0x01, 0x01})

	// ContentType (0x03) = "text/plain"
	f.Add([]byte{0x0d, 0x03, 0x00, 0x0a, 't', 'e', 'x', 't', '/', 'p', 'l', 'a', 'i', 'n'})

	// UserProperty (0x26) = "key" -> "value"
	f.Add([]byte{0x10, 0x26, 0x00, 0x03, 'k', 'e', 'y', 0x00, 0x05, 'v', 'a', 'l', 'u', 'e'})

	// Multiple properties
	f.Add([]byte{
		0x06,       // Length
		0x01, 0x01, // PayloadFormatIndicator = 1
		0x24, 0x02, // MaximumQoS = 2
	})

	f.Fuzz(func(t *testing.T, data []byte) {
		_, _, _ = decodeProperties(data)
	})
}

// FuzzVarIntBuffer fuzzes variable integer decoding from buffer
func FuzzVarIntBuffer(f *testing.F) {
	// Seed with valid varint examples
	f.Add([]byte{0x00})
	f.Add([]byte{0x7f})
	f.Add([]byte{0x80, 0x01})
	f.Add([]byte{0xff, 0x7f})
	f.Add([]byte{0x80, 0x80, 0x80, 0x01})
	f.Add([]byte{0xff, 0xff, 0xff, 0x7f}) // Max value

	f.Fuzz(func(t *testing.T, data []byte) {
		_, _, _ = decodeVarIntBuf(data)
	})
}

// FuzzEncodeDecodeProperties tests round-trip property encoding/decoding
func FuzzEncodeDecodeProperties(f *testing.F) {
	f.Add(uint8(1), uint32(3600), "application/json")
	f.Add(uint8(0), uint32(0), "")
	f.Add(uint8(1), uint32(60), "text/plain")

	f.Fuzz(func(t *testing.T, formatIndicator uint8, expiryInterval uint32, contentType string) {
		// Build a Properties struct
		props := &Properties{}

		if formatIndicator <= 1 {
			props.PayloadFormatIndicator = formatIndicator
			props.Presence |= PresPayloadFormatIndicator
		}

		if expiryInterval > 0 && expiryInterval <= 268435455 {
			props.MessageExpiryInterval = expiryInterval
			props.Presence |= PresMessageExpiryInterval
		}

		if len(contentType) > 0 && len(contentType) <= 65535 {
			props.ContentType = contentType
			props.Presence |= PresContentType
		}

		// Encode
		encoded := encodeProperties(props)

		// Decode
		decoded, n, err := decodeProperties(encoded)
		if err != nil {
			t.Fatalf("decode failed: %v", err)
		}

		if n != len(encoded) {
			t.Fatalf("decoded length mismatch: got %d, want %d", n, len(encoded))
		}

		// Verify round-trip for set fields
		if props.Presence&PresPayloadFormatIndicator != 0 {
			if decoded.Presence&PresPayloadFormatIndicator == 0 {
				t.Fatal("PayloadFormatIndicator lost in round-trip")
			}
			if decoded.PayloadFormatIndicator != props.PayloadFormatIndicator {
				t.Fatalf("PayloadFormatIndicator mismatch: got %d, want %d",
					decoded.PayloadFormatIndicator, props.PayloadFormatIndicator)
			}
		}
	})
}

// FuzzDecodeConnack fuzzes CONNACK packet decoding with v5 properties
func FuzzDecodeConnack(f *testing.F) {
	// v3.1.1 CONNACK
	f.Add([]byte{0x00, 0x00}, uint8(4))
	f.Add([]byte{0x01, 0x00}, uint8(4))

	// v5.0 CONNACK with properties
	f.Add([]byte{0x00, 0x00, 0x00}, uint8(5))                               // Empty properties
	f.Add([]byte{0x00, 0x00, 0x05, 0x11, 0x00, 0x00, 0x0e, 0x10}, uint8(5)) // SessionExpiryInterval

	f.Fuzz(func(t *testing.T, data []byte, version uint8) {
		if version != 4 && version != 5 {
			return
		}
		_, _ = DecodeConnack(data, version)
	})
}

// FuzzPacketReaderV5 fuzzes packet reading with v5 protocol
func FuzzPacketReaderV5(f *testing.F) {
	// Seed with v5 packets
	f.Add([]byte{0x20, 0x03, 0x00, 0x00, 0x00}) // CONNACK v5 with empty properties
	f.Add([]byte{0x30, 0x00})                   // PUBLISH QoS 0
	f.Add([]byte{0xe0, 0x00})                   // DISCONNECT v3

	f.Fuzz(func(t *testing.T, data []byte) {
		r := bytes.NewReader(data)
		_, _ = ReadPacket(r, 5, 0)
	})
}
