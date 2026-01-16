package packets

import (
	"bytes"
	"strings"
	"testing"
)

func TestEncodeString(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []byte
	}{
		{
			name:     "empty string",
			input:    "",
			expected: []byte{0, 0},
		},
		{
			name:     "simple string",
			input:    "foo",
			expected: []byte{0, 3, 'f', 'o', 'o'},
		},
		{
			name:     "UTF-8 string",
			input:    "héllö",
			expected: []byte{0, 7, 'h', 0xc3, 0xa9, 'l', 'l', 0xc3, 0xb6}, // 2 bytes length + 7 bytes utf-8
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := encodeString(tt.input)
			if !bytes.Equal(got, tt.expected) {
				t.Errorf("encodeString() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestAppendString(t *testing.T) {
	dst := []byte{0xAA} // pre-existing data
	input := "bar"
	expected := []byte{0xAA, 0, 3, 'b', 'a', 'r'}

	got := appendString(dst, input)
	if !bytes.Equal(got, expected) {
		t.Errorf("appendString() = %v, want %v", got, expected)
	}
}

func TestEncodeBinary(t *testing.T) {
	tests := []struct {
		name     string
		input    []byte
		expected []byte
	}{
		{
			name:     "empty",
			input:    []byte{},
			expected: []byte{0, 0},
		},
		{
			name:     "data",
			input:    []byte{1, 2, 3},
			expected: []byte{0, 3, 1, 2, 3},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := encodeBinary(tt.input)
			if !bytes.Equal(got, tt.expected) {
				t.Errorf("encodeBinary() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestAppendBinary(t *testing.T) {
	dst := []byte{0xFF}
	input := []byte{0x01, 0x02}
	expected := []byte{0xFF, 0, 2, 0x01, 0x02}

	got := appendBinary(dst, input)
	if !bytes.Equal(got, expected) {
		t.Errorf("appendBinary() = %v, want %v", got, expected)
	}
}

func TestDecodeString(t *testing.T) {
	tests := []struct {
		name        string
		input       []byte
		want        string
		wantBytes   int
		expectError bool
		errorSubstr string // Expected substring in error string, if expectError is true
	}{
		{
			name:        "valid string",
			input:       []byte{0, 3, 'b', 'a', 'z'},
			want:        "baz",
			wantBytes:   5,
			expectError: false,
		},
		{
			name:        "valid UTF-8",
			input:       []byte{0, 2, 0xc3, 0xb6}, // 'ö'
			want:        "ö",
			wantBytes:   4,
			expectError: false,
		},
		{
			name:        "buffer too short for length",
			input:       []byte{0},
			want:        "",
			wantBytes:   0,
			expectError: true,
			errorSubstr: "buffer too short",
		},
		{
			name:        "buffer too short for data",
			input:       []byte{0, 5, 'a', 'b'},
			want:        "",
			wantBytes:   0,
			expectError: true,
			errorSubstr: "buffer too short",
		},
		{
			name:        "invalid UTF-8",
			input:       []byte{0, 1, 0xFF}, // Invalid UTF-8 byte
			want:        "",
			wantBytes:   0,
			expectError: true,
			errorSubstr: "invalid UTF-8",
		},
		{
			name:        "null character",
			input:       []byte{0, 5, 'h', 'e', 0x00, 'l', 'o'},
			want:        "",
			wantBytes:   0,
			expectError: true,
			errorSubstr: "null byte",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, n, err := decodeString(tt.input)
			if (err != nil) != tt.expectError {
				t.Errorf("decodeString() error = %v, expectError %v", err, tt.expectError)
				return
			}
			if tt.expectError && err != nil && tt.errorSubstr != "" {
				if !strings.Contains(err.Error(), tt.errorSubstr) {
					t.Errorf("decodeString() error = %q, want substring %q", err.Error(), tt.errorSubstr)
				}
			}
			if got != tt.want {
				t.Errorf("decodeString() = %v, want %v", got, tt.want)
			}
			if n != tt.wantBytes {
				t.Errorf("decodeString() bytes consumed = %v, want %v", n, tt.wantBytes)
			}
		})
	}
}

func TestDecodeBinary(t *testing.T) {
	tests := []struct {
		name        string
		input       []byte
		want        []byte
		wantBytes   int
		expectError bool
	}{
		{
			name:        "valid data",
			input:       []byte{0, 2, 0xCA, 0xFE},
			want:        []byte{0xCA, 0xFE},
			wantBytes:   4,
			expectError: false,
		},
		{
			name:        "buffer too short for length",
			input:       []byte{0},
			want:        nil,
			wantBytes:   0,
			expectError: true,
		},
		{
			name:        "buffer too short for data",
			input:       []byte{0, 3, 0x01},
			want:        nil,
			wantBytes:   0,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, n, err := decodeBinary(tt.input)
			if (err != nil) != tt.expectError {
				t.Errorf("decodeBinary() error = %v, expectError %v", err, tt.expectError)
				return
			}
			if !bytes.Equal(got, tt.want) {
				t.Errorf("decodeBinary() = %v, want %v", got, tt.want)
			}
			if n != tt.wantBytes {
				t.Errorf("decodeBinary() bytes consumed = %v, want %v", n, tt.wantBytes)
			}
		})
	}
}
