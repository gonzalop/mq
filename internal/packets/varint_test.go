package packets

import (
	"bytes"
	"testing"
)

func TestEncodeVarInt(t *testing.T) {
	tests := []struct {
		name     string
		value    int
		expected []byte
	}{
		{"zero", 0, []byte{0x00}},
		{"127", 127, []byte{0x7F}},
		{"128", 128, []byte{0x80, 0x01}},
		{"16383", 16383, []byte{0xFF, 0x7F}},
		{"16384", 16384, []byte{0x80, 0x80, 0x01}},
		{"2097151", 2097151, []byte{0xFF, 0xFF, 0x7F}},
		{"2097152", 2097152, []byte{0x80, 0x80, 0x80, 0x01}},
		{"268435455", 268435455, []byte{0xFF, 0xFF, 0xFF, 0x7F}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := encodeVarInt(tt.value)
			if !bytes.Equal(result, tt.expected) {
				t.Errorf("encodeVarInt(%d) = %v, want %v", tt.value, result, tt.expected)
			}
		})
	}
}

func TestDecodeVarInt(t *testing.T) {
	tests := []struct {
		name     string
		input    []byte
		expected int
		wantErr  bool
	}{
		{"zero", []byte{0x00}, 0, false},
		{"127", []byte{0x7F}, 127, false},
		{"128", []byte{0x80, 0x01}, 128, false},
		{"16383", []byte{0xFF, 0x7F}, 16383, false},
		{"16384", []byte{0x80, 0x80, 0x01}, 16384, false},
		{"2097151", []byte{0xFF, 0xFF, 0x7F}, 2097151, false},
		{"2097152", []byte{0x80, 0x80, 0x80, 0x01}, 2097152, false},
		{"268435455", []byte{0xFF, 0xFF, 0xFF, 0x7F}, 268435455, false},
		{"too long", []byte{0xFF, 0xFF, 0xFF, 0xFF, 0x7F}, 0, true},
		{"incomplete", []byte{0x80}, 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := bytes.NewReader(tt.input)
			result, err := decodeVarInt(r)

			if tt.wantErr {
				if err == nil {
					t.Errorf("decodeVarInt() expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("decodeVarInt() unexpected error: %v", err)
				return
			}

			if result != tt.expected {
				t.Errorf("decodeVarInt() = %d, want %d", result, tt.expected)
			}
		})
	}
}

func TestVarIntRoundTrip(t *testing.T) {
	values := []int{0, 1, 127, 128, 16383, 16384, 2097151, 2097152, 268435455}

	for _, val := range values {
		t.Run(string(rune(val)), func(t *testing.T) {
			encoded := encodeVarInt(val)
			r := bytes.NewReader(encoded)
			decoded, err := decodeVarInt(r)

			if err != nil {
				t.Errorf("round trip failed: %v", err)
			}

			if decoded != val {
				t.Errorf("round trip: got %d, want %d", decoded, val)
			}
		})
	}
}
