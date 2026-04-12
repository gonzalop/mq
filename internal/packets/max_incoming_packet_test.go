package packets

import (
	"bytes"
	"strings"
	"testing"
)

// TestMaxIncomingPacketEnforcement verifies that the maxIncomingPacket parameter
// is properly enforced by ReadPacket.
func TestMaxIncomingPacketEnforcement(t *testing.T) {
	tests := []struct {
		name              string
		maxIncomingPacket int
		packetSize        int
		wantError         bool
	}{
		{
			name:              "default limit (0) allows medium packets",
			maxIncomingPacket: 0,
			packetSize:        512 * 1024, // 512KB (under default 1MB)
			wantError:         false,
		},
		{
			name:              "packet within custom limit",
			maxIncomingPacket: 2048,
			packetSize:        1024,
			wantError:         false,
		},
		{
			name:              "packet exceeds custom limit",
			maxIncomingPacket: 1024,
			packetSize:        2048,
			wantError:         true,
		},
		{
			name:              "small packet well within limit",
			maxIncomingPacket: 2048,
			packetSize:        512,
			wantError:         false,
		},
		{
			name:              "negative limit uses conservative default",
			maxIncomingPacket: -1,
			packetSize:        512 * 1024, // 512KB
			wantError:         false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a PUBLISH packet with the specified payload size
			payload := []byte(strings.Repeat("x", tt.packetSize))
			pkt := &PublishPacket{
				Topic:   "test/topic",
				Payload: payload,
				QoS:     0,
			}

			// Encode the packet
			encoded := encodeToBytes(pkt)

			// Try to read it back with the specified limit
			r := bytes.NewReader(encoded)
			_, err := ReadPacket(r, 4, tt.maxIncomingPacket)

			if tt.wantError && err == nil {
				t.Error("expected error, got nil")
			}
			if !tt.wantError && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			if tt.wantError && err != nil && !strings.Contains(err.Error(), "exceeds maximum") {
				t.Errorf("expected 'exceeds maximum' error, got: %v", err)
			}
		})
	}
}

// TestMaxIncomingPacketSpecMaximum verifies that very large packets are rejected.
func TestMaxIncomingPacketSpecMaximum(t *testing.T) {
	// Create a PUBLISH packet that exceeds a reasonable custom limit
	// to verify that the limit is enforced
	payload := make([]byte, 10*1024*1024) // 10MB payload
	pkt := &PublishPacket{
		Topic:   "test/topic",
		Payload: payload,
		QoS:     0,
	}

	encoded := encodeToBytes(pkt)
	r := bytes.NewReader(encoded)

	// Try to read with a 1MB limit - should reject
	_, err := ReadPacket(r, 4, 1024*1024)
	if err == nil {
		t.Error("expected error for packet exceeding 1MB limit, got nil")
	}
	if err != nil && !strings.Contains(err.Error(), "exceeds maximum") {
		t.Errorf("expected 'exceeds maximum' error, got: %v", err)
	}

	// Try again with large limit (20MB) - should accept
	r = bytes.NewReader(encoded)
	_, err = ReadPacket(r, 4, 20*1024*1024)
	if err != nil {
		t.Errorf("unexpected error with 20MB limit: %v", err)
	}

	// Try again with default limit (0) - should reject 10MB payload
	r = bytes.NewReader(encoded)
	_, err = ReadPacket(r, 4, 0)
	if err == nil {
		t.Error("expected error with default 1MB limit for 10MB packet, got nil")
	}
}
