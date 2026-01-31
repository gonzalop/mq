package packets

import (
	"bytes"
	"testing"
)

func TestConnectPacketV5(t *testing.T) {
	t.Parallel()
	props := &Properties{
		SessionExpiryInterval: 3600,
		UserProperties: []UserProperty{
			{Key: "client", Value: "test"},
		},
		Presence: PresSessionExpiryInterval,
	}

	willProps := &Properties{
		ContentType: "text/plain",
		Presence:    PresContentType,
	}

	pkt := &ConnectPacket{
		ProtocolName:   "MQTT",
		ProtocolLevel:  5,
		ClientID:       "v5-client",
		Properties:     props,
		WillProperties: willProps,
		WillFlag:       true,
		WillTopic:      "will",
		WillMessage:    []byte("bye"),
	}

	encoded := encodeToBytes(pkt)

	// Decode with v5
	r := bytes.NewReader(encoded)
	header, _ := DecodeFixedHeader(r)
	remaining := make([]byte, header.RemainingLength)
	_, _ = r.Read(remaining)

	decoded, err := DecodeConnect(remaining)
	if err != nil {
		t.Fatalf("failed to decode: %v", err)
	}

	// Verify properties
	if !compareProperties(decoded.Properties, props) {
		t.Errorf("properties mismatch")
	}
	if !compareProperties(decoded.WillProperties, willProps) {
		t.Errorf("will properties mismatch")
	}
}

func TestConnackPacketV5(t *testing.T) {
	props := &Properties{
		AssignedClientIdentifier: "assigned-id",
		Presence:                 PresAssignedClientIdentifier,
	}
	pkt := &ConnackPacket{
		SessionPresent: true,
		ReturnCode:     0, // Success
		Properties:     props,
	}

	encoded := encodeToBytes(pkt)

	// Decode
	r := bytes.NewReader(encoded)
	header, _ := DecodeFixedHeader(r)
	remaining := make([]byte, header.RemainingLength)
	_, _ = r.Read(remaining)

	decoded, err := DecodeConnack(remaining, 5) // Pass version 5
	if err != nil {
		t.Fatalf("failed to decode: %v", err)
	}

	if !compareProperties(decoded.Properties, props) {
		t.Errorf("properties mismatch")
	}
}

func TestPublishPacketV5(t *testing.T) {
	props := &Properties{
		ContentType: "application/json",
		Presence:    PresContentType,
	}
	pkt := &PublishPacket{
		Topic:      "topic/v5",
		QoS:        1,
		PacketID:   10,
		Payload:    []byte("payload"),
		Properties: props,
		Version:    5,
	}

	encoded := encodeToBytes(pkt)

	r := bytes.NewReader(encoded)
	header, _ := DecodeFixedHeader(r)
	remaining := make([]byte, header.RemainingLength)
	_, _ = r.Read(remaining)

	decoded, err := DecodePublish(remaining, &header, 5)
	if err != nil {
		t.Fatalf("failed to decode: %v", err)
	}

	if !compareProperties(decoded.Properties, props) {
		t.Errorf("properties mismatch")
	}
	if string(decoded.Payload) != "payload" {
		t.Errorf("payload mismatch")
	}
}

func TestPubackPacketV5(t *testing.T) {
	props := &Properties{
		ReasonString: "ok",
		Presence:     PresReasonString,
	}
	pkt := &PubackPacket{
		PacketID:   20,
		ReasonCode: 0,
		Properties: props,
		Version:    5,
	}

	encoded := encodeToBytes(pkt)

	r := bytes.NewReader(encoded)
	header, _ := DecodeFixedHeader(r)
	remaining := make([]byte, header.RemainingLength)
	_, _ = r.Read(remaining)

	decoded, err := DecodePuback(remaining, 5)
	if err != nil {
		t.Fatalf("failed to decode: %v", err)
	}

	if decoded.ReasonCode != pkt.ReasonCode {
		t.Errorf("reason code mismatch")
	}
	if !compareProperties(decoded.Properties, props) {
		t.Errorf("properties mismatch")
	}
}

func TestSubscribePacketV5(t *testing.T) {
	props := &Properties{SubscriptionIdentifier: []int{1}}
	pkt := &SubscribePacket{
		PacketID:   30,
		Topics:     []string{"topic"},
		QoS:        []uint8{1},
		Properties: props,
		Version:    5,
	}

	encoded := encodeToBytes(pkt)

	r := bytes.NewReader(encoded)
	header, _ := DecodeFixedHeader(r)
	remaining := make([]byte, header.RemainingLength)
	_, _ = r.Read(remaining)

	decoded, err := DecodeSubscribe(remaining, 5)
	if err != nil {
		t.Fatalf("failed to decode: %v", err)
	}

	if !compareProperties(decoded.Properties, props) {
		t.Errorf("properties mismatch")
	}
}

func TestSubackPacketV5(t *testing.T) {
	props := &Properties{
		ReasonString: "granted",
		Presence:     PresReasonString,
	}
	pkt := &SubackPacket{
		PacketID:    30,
		ReturnCodes: []uint8{1},
		Properties:  props,
		Version:     5,
	}

	encoded := encodeToBytes(pkt)

	r := bytes.NewReader(encoded)
	header, _ := DecodeFixedHeader(r)
	remaining := make([]byte, header.RemainingLength)
	_, _ = r.Read(remaining)

	decoded, err := DecodeSuback(remaining, 5)
	if err != nil {
		t.Fatalf("failed to decode: %v", err)
	}

	if !compareProperties(decoded.Properties, props) {
		t.Errorf("properties mismatch")
	}
}

func TestUnsubscribePacketV5(t *testing.T) {
	props := &Properties{UserProperties: []UserProperty{{Key: "k", Value: "v"}}}
	pkt := &UnsubscribePacket{
		PacketID:   40,
		Topics:     []string{"topic"},
		Properties: props,
		Version:    5,
	}

	encoded := encodeToBytes(pkt)

	r := bytes.NewReader(encoded)
	header, _ := DecodeFixedHeader(r)
	remaining := make([]byte, header.RemainingLength)
	_, _ = r.Read(remaining)

	decoded, err := DecodeUnsubscribe(remaining, 5)
	if err != nil {
		t.Fatalf("failed to decode: %v", err)
	}

	if !compareProperties(decoded.Properties, props) {
		t.Errorf("properties mismatch")
	}
}

func TestUnsubackPacketV5(t *testing.T) {
	props := &Properties{
		ReasonString: "removed",
		Presence:     PresReasonString,
	}
	pkt := &UnsubackPacket{
		PacketID:    40,
		ReasonCodes: []uint8{0},
		Properties:  props,
		Version:     5,
	}

	encoded := encodeToBytes(pkt)

	r := bytes.NewReader(encoded)
	header, _ := DecodeFixedHeader(r)
	remaining := make([]byte, header.RemainingLength)
	_, _ = r.Read(remaining)

	decoded, err := DecodeUnsuback(remaining, 5)
	if err != nil {
		t.Fatalf("failed to decode: %v", err)
	}

	if !compareProperties(decoded.Properties, props) {
		t.Errorf("properties mismatch")
	}
}

func TestDisconnectPacketV5(t *testing.T) {
	props := &Properties{
		ReasonString: "shutdown",
		Presence:     PresReasonString,
	}
	pkt := &DisconnectPacket{
		ReasonCode: 0,
		Properties: props,
		Version:    5,
	}

	encoded := encodeToBytes(pkt)

	r := bytes.NewReader(encoded)
	header, _ := DecodeFixedHeader(r)
	remaining := make([]byte, header.RemainingLength)
	_, _ = r.Read(remaining)

	decoded, err := DecodeDisconnect(remaining, 5)
	if err != nil {
		t.Fatalf("failed to decode: %v", err)
	}

	if decoded.ReasonCode != pkt.ReasonCode {
		t.Errorf("reason code mismatch")
	}
	if !compareProperties(decoded.Properties, props) {
		t.Errorf("properties mismatch")
	}
}

func TestAuthPacketV5(t *testing.T) {
	props := &Properties{
		AuthenticationMethod: "SCRAM-SHA-256",
		AuthenticationData:   []byte("client-first-message"),
		Presence:             PresAuthenticationMethod,
	}
	pkt := &AuthPacket{
		ReasonCode: AuthReasonContinue,
		Properties: props,
		Version:    5,
	}

	encoded := encodeToBytes(pkt)

	r := bytes.NewReader(encoded)
	header, _ := DecodeFixedHeader(r)
	remaining := make([]byte, header.RemainingLength)
	_, _ = r.Read(remaining)

	decoded, err := DecodeAuth(remaining, 5)
	if err != nil {
		t.Fatalf("failed to decode: %v", err)
	}

	if decoded.ReasonCode != pkt.ReasonCode {
		t.Errorf("reason code mismatch: got 0x%x, want 0x%x", decoded.ReasonCode, pkt.ReasonCode)
	}
	if !compareProperties(decoded.Properties, props) {
		t.Errorf("properties mismatch")
	}

	// Verify ReadPacket also works (integration check for the fix)
	r = bytes.NewReader(encoded)
	readPkt, err := ReadPacket(r, 5, 0)
	if err != nil {
		t.Fatalf("ReadPacket failed: %v", err)
	}
	if readPkt.Type() != AUTH {
		t.Errorf("ReadPacket returned type %d, want %d", readPkt.Type(), AUTH)
	}
}

func TestPubcompPacketV5(t *testing.T) {
	props := &Properties{
		ReasonString: "all done",
		Presence:     PresReasonString,
	}
	pkt := &PubcompPacket{
		PacketID:   50,
		ReasonCode: 0,
		Properties: props,
		Version:    5,
	}

	encoded := encodeToBytes(pkt)

	r := bytes.NewReader(encoded)
	header, _ := DecodeFixedHeader(r)
	remaining := make([]byte, header.RemainingLength)
	_, _ = r.Read(remaining)

	decoded, err := DecodePubcomp(remaining, 5)
	if err != nil {
		t.Fatalf("failed to decode: %v", err)
	}

	if decoded.ReasonCode != pkt.ReasonCode {
		t.Errorf("reason code mismatch")
	}
	if !compareProperties(decoded.Properties, props) {
		t.Errorf("properties mismatch")
	}
}

func TestPubrecPacketV5(t *testing.T) {
	props := &Properties{
		ReasonString: "received",
		Presence:     PresReasonString,
	}
	pkt := &PubrecPacket{
		PacketID:   60,
		ReasonCode: 0,
		Properties: props,
		Version:    5,
	}

	encoded := encodeToBytes(pkt)

	r := bytes.NewReader(encoded)
	header, _ := DecodeFixedHeader(r)
	remaining := make([]byte, header.RemainingLength)
	_, _ = r.Read(remaining)

	decoded, err := DecodePubrec(remaining, 5)
	if err != nil {
		t.Fatalf("failed to decode: %v", err)
	}

	if decoded.ReasonCode != pkt.ReasonCode {
		t.Errorf("reason code mismatch")
	}
	if !compareProperties(decoded.Properties, props) {
		t.Errorf("properties mismatch")
	}
}

func TestPubrelPacketV5(t *testing.T) {
	props := &Properties{
		ReasonString: "released",
		Presence:     PresReasonString,
	}
	pkt := &PubrelPacket{
		PacketID:   70,
		ReasonCode: 0,
		Properties: props,
		Version:    5,
	}

	encoded := encodeToBytes(pkt)

	r := bytes.NewReader(encoded)
	header, _ := DecodeFixedHeader(r)
	remaining := make([]byte, header.RemainingLength)
	_, _ = r.Read(remaining)

	decoded, err := DecodePubrel(remaining, 5)
	if err != nil {
		t.Fatalf("failed to decode: %v", err)
	}

	if decoded.ReasonCode != pkt.ReasonCode {
		t.Errorf("reason code mismatch")
	}
	if !compareProperties(decoded.Properties, props) {
		t.Errorf("properties mismatch")
	}
}
