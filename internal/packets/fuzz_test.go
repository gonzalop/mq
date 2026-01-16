package packets

import (
	"bytes"
	"testing"
)

// FuzzReadPacket fuzzes the packet reader to find crashes or panics
func FuzzReadPacket(f *testing.F) {
	// Seed with valid packet examples
	f.Add([]byte{0x10, 0x00})             // CONNECT with 0 length
	f.Add([]byte{0x20, 0x02, 0x00, 0x00}) // CONNACK
	f.Add([]byte{0x30, 0x00})             // PUBLISH QoS 0 with 0 length
	f.Add([]byte{0xc0, 0x00})             // PINGREQ
	f.Add([]byte{0xd0, 0x00})             // PINGRESP
	f.Add([]byte{0xe0, 0x00})             // DISCONNECT

	f.Fuzz(func(t *testing.T, data []byte) {
		// Just try to read - should never panic
		r := bytes.NewReader(data)
		_, _ = ReadPacket(r, 4, 0)
	})
}

// FuzzDecodeFixedHeader fuzzes the fixed header decoder
func FuzzDecodeFixedHeader(f *testing.F) {
	// Seed with various header patterns
	f.Add([]byte{0x10, 0x00})
	f.Add([]byte{0x30, 0x7f})
	f.Add([]byte{0x30, 0x80, 0x01})
	f.Add([]byte{0x30, 0xff, 0xff, 0xff, 0x7f})

	f.Fuzz(func(t *testing.T, data []byte) {
		r := bytes.NewReader(data)
		_, _ = DecodeFixedHeader(r)
	})
}

// FuzzDecodeVarInt fuzzes variable integer decoding
func FuzzDecodeVarInt(f *testing.F) {
	// Seed with valid varint examples
	f.Add([]byte{0x00})
	f.Add([]byte{0x7f})
	f.Add([]byte{0x80, 0x01})
	f.Add([]byte{0xff, 0x7f})
	f.Add([]byte{0x80, 0x80, 0x80, 0x01})

	f.Fuzz(func(t *testing.T, data []byte) {
		r := bytes.NewReader(data)
		_, _ = decodeVarInt(r)
	})
}

// FuzzDecodeString fuzzes MQTT string decoding
func FuzzDecodeString(f *testing.F) {
	// Seed with valid string examples
	f.Add([]byte{0x00, 0x00}) // Empty string
	f.Add([]byte{0x00, 0x04, 'M', 'Q', 'T', 'T'})
	f.Add([]byte{0x00, 0x05, 'h', 'e', 'l', 'l', 'o'})

	f.Fuzz(func(t *testing.T, data []byte) {
		_, _, _ = decodeString(data)
	})
}

// FuzzDecodeConnect fuzzes CONNECT packet decoding
func FuzzDecodeConnect(f *testing.F) {
	// Seed with a minimal valid CONNECT packet payload
	validConnect := []byte{
		0x00, 0x04, 'M', 'Q', 'T', 'T', // Protocol name
		0x04,       // Protocol level
		0x02,       // Connect flags (clean session)
		0x00, 0x3c, // Keep alive (60 seconds)
		0x00, 0x04, 't', 'e', 's', 't', // Client ID
	}
	f.Add(validConnect)

	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = DecodeConnect(data)
	})
}

// FuzzDecodePublish fuzzes PUBLISH packet decoding
func FuzzDecodePublish(f *testing.F) {
	// Seed with valid PUBLISH payloads
	f.Add([]byte{0x00, 0x04, 't', 'e', 's', 't', 'h', 'i'})                       // QoS 0
	f.Add([]byte{0x00, 0x04, 't', 'e', 's', 't', 0x00, 0x01, 'd', 'a', 't', 'a'}) // QoS 1

	f.Fuzz(func(t *testing.T, data []byte) {
		header := &FixedHeader{
			PacketType:      PUBLISH,
			Flags:           0,
			RemainingLength: len(data),
		}
		_, _ = DecodePublish(data, header, 4)
	})
}
