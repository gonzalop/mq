package packets

import (
	"bytes"
	"testing"
)

func TestConnectPacketV3Encoding(t *testing.T) {
	pkt := &ConnectPacket{
		ProtocolName:  "MQTT",
		ProtocolLevel: 4, // v3.1.1
		CleanSession:  true,
		KeepAlive:     60,
		ClientID:      "test-client",
	}

	encoded := encodeToBytes(pkt)

	// Decode to verify
	r := bytes.NewReader(encoded)
	header, err := DecodeFixedHeader(r)
	if err != nil {
		t.Fatalf("failed to decode header: %v", err)
	}

	remaining := make([]byte, header.RemainingLength)
	_, _ = r.Read(remaining)

	decoded, err := DecodeConnect(remaining)
	if err != nil {
		t.Fatalf("failed to decode: %v", err)
	}

	if decoded.ProtocolLevel != 4 {
		t.Errorf("protocol level = %d, want 4", decoded.ProtocolLevel)
	}

	if decoded.ClientID != "test-client" {
		t.Errorf("client ID = %s, want test-client", decoded.ClientID)
	}

	t.Logf("Encoded CONNECT packet (%d bytes): %x", len(encoded), encoded)
}
