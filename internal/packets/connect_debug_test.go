package packets

import (
	"bytes"
	"testing"
)

func TestConnectPacketHexDump(t *testing.T) {
	// Simulate what the client sends
	pkt := &ConnectPacket{
		ProtocolName:  "MQTT",
		ProtocolLevel: 4, // v3.1.1 - THIS IS THE KEY
		CleanSession:  true,
		KeepAlive:     60,
		ClientID:      "test-client",
	}

	encoded := encodeToBytes(pkt)

	t.Logf("Full CONNECT packet (%d bytes):", len(encoded))
	t.Logf("Hex: %x", encoded)
	t.Logf("Breakdown:")

	r := bytes.NewReader(encoded)
	header, _ := DecodeFixedHeader(r)
	t.Logf("  Fixed Header: type=%d, remaining=%d", header.PacketType, header.RemainingLength)

	remaining := make([]byte, header.RemainingLength)
	_, _ = r.Read(remaining)

	t.Logf("  Variable Header + Payload (%d bytes): %x", len(remaining), remaining)

	// Manual decode to see structure
	offset := 0

	// Protocol name length
	nameLen := int(remaining[offset])<<8 | int(remaining[offset+1])
	offset += 2
	t.Logf("    Protocol name length: %d", nameLen)

	// Protocol name
	protocolName := string(remaining[offset : offset+nameLen])
	offset += nameLen
	t.Logf("    Protocol name: %s", protocolName)

	// Protocol level
	protocolLevel := remaining[offset]
	offset++
	t.Logf("    Protocol level: %d", protocolLevel)

	// Connect flags
	connectFlags := remaining[offset]
	offset++
	t.Logf("    Connect flags: 0x%02x", connectFlags)

	// Keep alive
	keepAlive := int(remaining[offset])<<8 | int(remaining[offset+1])
	offset += 2
	t.Logf("    Keep alive: %d", keepAlive)

	// Check if there are unexpected bytes here (properties for v3.1.1)
	t.Logf("    Offset after keep alive: %d", offset)
	t.Logf("    Remaining bytes before client ID: %d", len(remaining)-offset)

	// Client ID length
	if offset < len(remaining) {
		clientIDLen := int(remaining[offset])<<8 | int(remaining[offset+1])
		offset += 2
		t.Logf("    Client ID length: %d", clientIDLen)

		if offset+clientIDLen <= len(remaining) {
			clientID := string(remaining[offset : offset+clientIDLen])
			t.Logf("    Client ID: %s", clientID)
		}
	}
}
