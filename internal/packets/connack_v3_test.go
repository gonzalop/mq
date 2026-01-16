package packets

import (
	"testing"
)

func TestConnackV3Decoding(t *testing.T) {
	// Simulate a v3.1.1 CONNACK from Mosquitto
	// Format: [Session Present flags] [Return Code]
	buf := []byte{
		0x00, // No session present
		0x00, // Connection accepted
	}

	decoded, err := DecodeConnack(buf, 4) // v3.1.1
	if err != nil {
		t.Fatalf("failed to decode: %v", err)
	}

	if decoded.ReturnCode != ConnAccepted {
		t.Errorf("return code = %d, want %d", decoded.ReturnCode, ConnAccepted)
	}

	if decoded.SessionPresent {
		t.Error("session present should be false")
	}

	t.Logf("Successfully decoded v3.1.1 CONNACK")
}

func TestConnackV3WithRefusal(t *testing.T) {
	// Test with "unacceptable protocol version" error
	buf := []byte{
		0x00, // No session present
		0x01, // Unacceptable protocol version
	}

	decoded, err := DecodeConnack(buf, 4)
	if err != nil {
		t.Fatalf("failed to decode: %v", err)
	}

	if decoded.ReturnCode != ConnRefusedUnacceptableProtocol {
		t.Errorf("return code = %d, want %d (unacceptable protocol)",
			decoded.ReturnCode, ConnRefusedUnacceptableProtocol)
	}

	t.Logf("Return code: %d (unacceptable protocol version)", decoded.ReturnCode)
}
