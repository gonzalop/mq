package packets

import (
	"testing"
)

func TestSubscribeOptionsEncoding(t *testing.T) {
	// Test Case 1: All options enabled
	pkt := &SubscribePacket{
		PacketID:          1234,
		Version:           5,
		Topics:            []string{"test/topic"},
		QoS:               []uint8{1},
		NoLocal:           []bool{true},
		RetainAsPublished: []bool{true},
		RetainHandling:    []uint8{2}, // Do not send
	}

	encoded := encodeToBytes(pkt)
	decoded, err := DecodeSubscribe(encoded[2:], 5) // Skip fixed header (2 bytes)
	if err != nil {
		t.Fatalf("Failed to decode: %v", err)
	}

	if decoded.PacketID != 1234 {
		t.Errorf("PacketID = %d, want 1234", decoded.PacketID)
	}

	if len(decoded.Topics) != 1 || decoded.Topics[0] != "test/topic" {
		t.Errorf("Topics = %v, want ['test/topic']", decoded.Topics)
	}

	// Verify Options
	// QoS=1 (Bit 0=1)
	// NoLocal=true (Bit 2=1)
	// RetainAsPublished=true (Bit 3=1)
	// RetainHandling=2 (Bit 5=1)
	// Byte should be: 0010 1101 = 0x2D = 45

	if len(decoded.NoLocal) != 1 || !decoded.NoLocal[0] {
		t.Error("NoLocal = false, want true")
	}
	if len(decoded.RetainAsPublished) != 1 || !decoded.RetainAsPublished[0] {
		t.Error("RetainAsPublished = false, want true")
	}
	if len(decoded.RetainHandling) != 1 || decoded.RetainHandling[0] != 2 {
		t.Errorf("RetainHandling = %d, want 2", decoded.RetainHandling[0])
	}
}

func TestSubscribeOptionsDefaults(t *testing.T) {
	// Test Case 2: Defaults (everything off/zero)
	pkt := &SubscribePacket{
		PacketID: 5678,
		Version:  5,
		Topics:   []string{"test/defaults"},
		QoS:      []uint8{0},
	}

	encoded := encodeToBytes(pkt)
	decoded, err := DecodeSubscribe(encoded[2:], 5)
	if err != nil {
		t.Fatalf("Failed to decode: %v", err)
	}

	if len(decoded.NoLocal) != 1 || decoded.NoLocal[0] {
		t.Error("NoLocal = true, want false (default)")
	}
	if len(decoded.RetainAsPublished) != 1 || decoded.RetainAsPublished[0] {
		t.Error("RetainAsPublished = true, want false (default)")
	}
	if len(decoded.RetainHandling) != 1 || decoded.RetainHandling[0] != 0 {
		t.Errorf("RetainHandling = %d, want 0 (default)", decoded.RetainHandling[0])
	}
}

func TestSubscribeOptionsMultipleTopics(t *testing.T) {
	// Test Case 3: Multiple topics with mixed options
	pkt := &SubscribePacket{
		PacketID:          9999,
		Version:           5,
		Topics:            []string{"topic/1", "topic/2"},
		QoS:               []uint8{1, 2},
		NoLocal:           []bool{true, false},
		RetainAsPublished: []bool{false, true},
		RetainHandling:    []uint8{0, 1},
	}

	encoded := encodeToBytes(pkt)
	decoded, err := DecodeSubscribe(encoded[2:], 5)
	if err != nil {
		t.Fatalf("Failed to decode: %v", err)
	}

	if len(decoded.Topics) != 2 {
		t.Fatalf("Decoded topics count = %d, want 2", len(decoded.Topics))
	}

	// Unpack manual checks from byte stream for verification? No, trust decoder if logic is sound.
	// But let's verify values.

	// Topic 1
	if !decoded.NoLocal[0] {
		t.Error("Topic 1: NoLocal = false")
	}
	if decoded.RetainAsPublished[0] {
		t.Error("Topic 1: RetainAsPublished = true")
	}
	if decoded.RetainHandling[0] != 0 {
		t.Error("Topic 1: RetainHandling != 0")
	}

	// Topic 2
	if decoded.NoLocal[1] {
		t.Error("Topic 2: NoLocal = true")
	}
	if !decoded.RetainAsPublished[1] {
		t.Error("Topic 2: RetainAsPublished = false")
	}
	if decoded.RetainHandling[1] != 1 {
		t.Error("Topic 2: RetainHandling != 1")
	}
}

func TestSubscribeV3Compatibility(t *testing.T) {
	// Test Case 4: MQTT v3.1.1 should ignore options (only QoS)
	pkt := &SubscribePacket{
		PacketID: 1111,
		Version:  4, // v3.1.1
		Topics:   []string{"topic/3"},
		QoS:      []uint8{1},
		// These should be ignored
		NoLocal: []bool{true},
	}

	encoded := encodeToBytes(pkt)

	// Check byte directly: Should be QoS=1 (0x01), not 0x05 (NoLocal set)
	// Encode: Header(2) + PacketID(2) + TopicLen(2) + "topic/3"(7) + QoS(1) = 14 bytes
	lastByte := encoded[len(encoded)-1]
	if lastByte != 1 {
		t.Errorf("v3.1.1 encoded byte = 0x%02x, want 0x01", lastByte)
	}

	decoded, err := DecodeSubscribe(encoded[2:], 4)
	if err != nil {
		t.Fatalf("Failed to decode: %v", err)
	}

	// v3 decoder ignores extra fields? Actually DecodeSubscribe logic appends to slice if version >= 5
	if len(decoded.NoLocal) != 0 {
		t.Error("v3.1.1 decoded NoLocal slice should be empty")
	}
}
