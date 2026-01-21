package packets

import (
	"bytes"
	"testing"
)

func encodeToBytes(pkt Packet) []byte {
	var buf bytes.Buffer
	if _, err := pkt.WriteTo(&buf); err != nil {
		panic(err)
	}
	return buf.Bytes()
}

func TestConnectPacket(t *testing.T) {
	t.Parallel()
	pkt := &ConnectPacket{
		ProtocolName:  "MQTT",
		ProtocolLevel: 4,
		CleanSession:  true,
		KeepAlive:     60,
		ClientID:      "test-client",
		UsernameFlag:  true,
		Username:      "user",
		PasswordFlag:  true,
		Password:      "pass",
	}

	// Encode
	encoded := encodeToBytes(pkt)

	// Decode fixed header
	r := bytes.NewReader(encoded)
	header, err := DecodeFixedHeader(r)
	if err != nil {
		t.Fatalf("failed to decode fixed header: %v", err)
	}

	if header.PacketType != CONNECT {
		t.Errorf("packet type = %d, want %d", header.PacketType, CONNECT)
	}

	// Read remaining bytes
	remaining := make([]byte, header.RemainingLength)
	if _, err := r.Read(remaining); err != nil {
		t.Fatalf("failed to read remaining: %v", err)
	}

	// Decode packet
	decoded, err := DecodeConnect(remaining)
	if err != nil {
		t.Fatalf("failed to decode CONNECT: %v", err)
	}

	// Verify fields
	if decoded.ProtocolName != pkt.ProtocolName {
		t.Errorf("protocol name = %s, want %s", decoded.ProtocolName, pkt.ProtocolName)
	}
	if decoded.ProtocolLevel != pkt.ProtocolLevel {
		t.Errorf("protocol level = %d, want %d", decoded.ProtocolLevel, pkt.ProtocolLevel)
	}
	if decoded.CleanSession != pkt.CleanSession {
		t.Errorf("clean session = %v, want %v", decoded.CleanSession, pkt.CleanSession)
	}
	if decoded.KeepAlive != pkt.KeepAlive {
		t.Errorf("keep alive = %d, want %d", decoded.KeepAlive, pkt.KeepAlive)
	}
	if decoded.ClientID != pkt.ClientID {
		t.Errorf("client ID = %s, want %s", decoded.ClientID, pkt.ClientID)
	}
	if decoded.Username != pkt.Username {
		t.Errorf("username = %s, want %s", decoded.Username, pkt.Username)
	}
	if decoded.Password != pkt.Password {
		t.Errorf("password = %s, want %s", decoded.Password, pkt.Password)
	}
}

func TestConnectPacketWithWill(t *testing.T) {
	pkt := &ConnectPacket{
		ProtocolName:  "MQTT",
		ProtocolLevel: 4,
		CleanSession:  true,
		KeepAlive:     60,
		ClientID:      "test-client",
		WillFlag:      true,
		WillQoS:       1,
		WillRetain:    true,
		WillTopic:     "will/topic",
		WillMessage:   []byte("goodbye"),
	}

	encoded := encodeToBytes(pkt)
	r := bytes.NewReader(encoded)
	header, _ := DecodeFixedHeader(r)
	remaining := make([]byte, header.RemainingLength)
	_, _ = r.Read(remaining) // Safe to ignore: reading from in-memory buffer

	decoded, err := DecodeConnect(remaining)
	if err != nil {
		t.Fatalf("failed to decode: %v", err)
	}

	if !decoded.WillFlag {
		t.Error("will flag should be true")
	}
	if decoded.WillQoS != pkt.WillQoS {
		t.Errorf("will QoS = %d, want %d", decoded.WillQoS, pkt.WillQoS)
	}
	if !decoded.WillRetain {
		t.Error("will retain should be true")
	}
	if decoded.WillTopic != pkt.WillTopic {
		t.Errorf("will topic = %s, want %s", decoded.WillTopic, pkt.WillTopic)
	}
	if !bytes.Equal(decoded.WillMessage, pkt.WillMessage) {
		t.Errorf("will message = %v, want %v", decoded.WillMessage, pkt.WillMessage)
	}
}

func TestConnackPacket(t *testing.T) {
	pkt := &ConnackPacket{
		SessionPresent: true,
		ReturnCode:     ConnAccepted,
	}

	encoded := encodeToBytes(pkt)
	r := bytes.NewReader(encoded)
	header, _ := DecodeFixedHeader(r)
	remaining := make([]byte, header.RemainingLength)
	_, _ = r.Read(remaining) // Safe to ignore: reading from in-memory buffer

	decoded, err := DecodeConnack(remaining, 4)
	if err != nil {
		t.Fatalf("failed to decode: %v", err)
	}

	if decoded.SessionPresent != pkt.SessionPresent {
		t.Errorf("session present = %v, want %v", decoded.SessionPresent, pkt.SessionPresent)
	}
	if decoded.ReturnCode != pkt.ReturnCode {
		t.Errorf("return code = %d, want %d", decoded.ReturnCode, pkt.ReturnCode)
	}
}

func TestPublishPacketQoS0(t *testing.T) {
	pkt := &PublishPacket{
		Topic:   "test/topic",
		QoS:     0,
		Retain:  false,
		Payload: []byte("hello world"),
	}

	encoded := encodeToBytes(pkt)
	r := bytes.NewReader(encoded)
	header, _ := DecodeFixedHeader(r)
	remaining := make([]byte, header.RemainingLength)
	_, _ = r.Read(remaining) // Safe to ignore: reading from in-memory buffer

	decoded, err := DecodePublish(remaining, &header, 4)
	if err != nil {
		t.Fatalf("failed to decode: %v", err)
	}

	if decoded.Topic != pkt.Topic {
		t.Errorf("topic = %s, want %s", decoded.Topic, pkt.Topic)
	}
	if decoded.QoS != pkt.QoS {
		t.Errorf("QoS = %d, want %d", decoded.QoS, pkt.QoS)
	}
	if !bytes.Equal(decoded.Payload, pkt.Payload) {
		t.Errorf("payload = %v, want %v", decoded.Payload, pkt.Payload)
	}
}

func TestPublishPacketQoS1(t *testing.T) {
	pkt := &PublishPacket{
		Topic:    "test/topic",
		QoS:      1,
		PacketID: 42,
		Retain:   true,
		Dup:      false,
		Payload:  []byte("hello"),
	}

	encoded := encodeToBytes(pkt)
	r := bytes.NewReader(encoded)
	header, _ := DecodeFixedHeader(r)
	remaining := make([]byte, header.RemainingLength)
	_, _ = r.Read(remaining) // Safe to ignore: reading from in-memory buffer

	decoded, err := DecodePublish(remaining, &header, 4)
	if err != nil {
		t.Fatalf("failed to decode: %v", err)
	}

	if decoded.PacketID != pkt.PacketID {
		t.Errorf("packet ID = %d, want %d", decoded.PacketID, pkt.PacketID)
	}
	if decoded.Retain != pkt.Retain {
		t.Errorf("retain = %v, want %v", decoded.Retain, pkt.Retain)
	}
}

func TestPubackPacket(t *testing.T) {
	pkt := &PubackPacket{PacketID: 123}

	encoded := encodeToBytes(pkt)
	r := bytes.NewReader(encoded)
	header, _ := DecodeFixedHeader(r)
	remaining := make([]byte, header.RemainingLength)
	_, _ = r.Read(remaining) // Safe to ignore: reading from in-memory buffer

	decoded, err := DecodePuback(remaining, 4)
	if err != nil {
		t.Fatalf("failed to decode: %v", err)
	}

	if decoded.PacketID != pkt.PacketID {
		t.Errorf("packet ID = %d, want %d", decoded.PacketID, pkt.PacketID)
	}
}

func TestSubscribePacket(t *testing.T) {
	pkt := &SubscribePacket{
		PacketID: 1,
		Topics:   []string{"topic/1", "topic/2"},
		QoS:      []uint8{0, 1},
	}

	encoded := encodeToBytes(pkt)
	r := bytes.NewReader(encoded)
	header, _ := DecodeFixedHeader(r)
	remaining := make([]byte, header.RemainingLength)
	_, _ = r.Read(remaining) // Safe to ignore: reading from in-memory buffer

	decoded, err := DecodeSubscribe(remaining, 4)
	if err != nil {
		t.Fatalf("failed to decode: %v", err)
	}

	if decoded.PacketID != pkt.PacketID {
		t.Errorf("packet ID = %d, want %d", decoded.PacketID, pkt.PacketID)
	}
	if len(decoded.Topics) != len(pkt.Topics) {
		t.Fatalf("topics length = %d, want %d", len(decoded.Topics), len(pkt.Topics))
	}
	for i := range pkt.Topics {
		if decoded.Topics[i] != pkt.Topics[i] {
			t.Errorf("topic[%d] = %s, want %s", i, decoded.Topics[i], pkt.Topics[i])
		}
		if decoded.QoS[i] != pkt.QoS[i] {
			t.Errorf("QoS[%d] = %d, want %d", i, decoded.QoS[i], pkt.QoS[i])
		}
	}
}

func TestSubackPacket(t *testing.T) {
	pkt := &SubackPacket{
		PacketID:    1,
		ReturnCodes: []uint8{SubackQoS0, SubackQoS1, SubackFailure},
	}

	encoded := encodeToBytes(pkt)
	r := bytes.NewReader(encoded)
	header, _ := DecodeFixedHeader(r)
	remaining := make([]byte, header.RemainingLength)
	_, _ = r.Read(remaining) // Safe to ignore: reading from in-memory buffer

	decoded, err := DecodeSuback(remaining, 4)
	if err != nil {
		t.Fatalf("failed to decode: %v", err)
	}

	if decoded.PacketID != pkt.PacketID {
		t.Errorf("packet ID = %d, want %d", decoded.PacketID, pkt.PacketID)
	}
	if !bytes.Equal(decoded.ReturnCodes, pkt.ReturnCodes) {
		t.Errorf("return codes = %v, want %v", decoded.ReturnCodes, pkt.ReturnCodes)
	}
}

func TestUnsubscribePacket(t *testing.T) {
	pkt := &UnsubscribePacket{
		PacketID: 2,
		Topics:   []string{"topic/1", "topic/2"},
	}

	encoded := encodeToBytes(pkt)
	r := bytes.NewReader(encoded)
	header, _ := DecodeFixedHeader(r)
	remaining := make([]byte, header.RemainingLength)
	_, _ = r.Read(remaining) // Safe to ignore: reading from in-memory buffer

	decoded, err := DecodeUnsubscribe(remaining, 4)
	if err != nil {
		t.Fatalf("failed to decode: %v", err)
	}

	if decoded.PacketID != pkt.PacketID {
		t.Errorf("packet ID = %d, want %d", decoded.PacketID, pkt.PacketID)
	}
	if len(decoded.Topics) != len(pkt.Topics) {
		t.Fatalf("topics length = %d, want %d", len(decoded.Topics), len(pkt.Topics))
	}
	for i := range pkt.Topics {
		if decoded.Topics[i] != pkt.Topics[i] {
			t.Errorf("topic[%d] = %s, want %s", i, decoded.Topics[i], pkt.Topics[i])
		}
	}
}

func TestPingreqPacket(t *testing.T) {
	pkt := &PingreqPacket{}

	encoded := encodeToBytes(pkt)
	if len(encoded) != 2 {
		t.Errorf("encoded length = %d, want 2", len(encoded))
	}

	r := bytes.NewReader(encoded)
	header, _ := DecodeFixedHeader(r)

	if header.PacketType != PINGREQ {
		t.Errorf("packet type = %d, want %d", header.PacketType, PINGREQ)
	}
	if header.RemainingLength != 0 {
		t.Errorf("remaining length = %d, want 0", header.RemainingLength)
	}
}

func TestPingrespPacket(t *testing.T) {
	pkt := &PingrespPacket{}

	encoded := encodeToBytes(pkt)
	if len(encoded) != 2 {
		t.Errorf("encoded length = %d, want 2", len(encoded))
	}

	r := bytes.NewReader(encoded)
	header, _ := DecodeFixedHeader(r)

	if header.PacketType != PINGRESP {
		t.Errorf("packet type = %d, want %d", header.PacketType, PINGRESP)
	}
}

func TestDisconnectPacket(t *testing.T) {
	pkt := &DisconnectPacket{}

	encoded := encodeToBytes(pkt)
	if len(encoded) != 2 {
		t.Errorf("encoded length = %d, want 2", len(encoded))
	}

	r := bytes.NewReader(encoded)
	header, _ := DecodeFixedHeader(r)

	if header.PacketType != DISCONNECT {
		t.Errorf("packet type = %d, want %d", header.PacketType, DISCONNECT)
	}
}

func TestReadPacket(t *testing.T) {
	tests := []struct {
		name string
		pkt  Packet
	}{
		{"CONNACK", &ConnackPacket{SessionPresent: false, ReturnCode: 0}},
		{"PUBLISH QoS0", &PublishPacket{Topic: "test", QoS: 0, Payload: []byte("data")}},
		{"PUBLISH QoS1", &PublishPacket{Topic: "test", QoS: 1, PacketID: 1, Payload: []byte("data")}},
		{"PUBACK", &PubackPacket{PacketID: 42}},
		{"SUBACK", &SubackPacket{PacketID: 1, ReturnCodes: []uint8{0}}},
		{"PINGRESP", &PingrespPacket{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			encoded := encodeToBytes(tt.pkt)
			r := bytes.NewReader(encoded)

			decoded, err := ReadPacket(r, 4, 0)
			if err != nil {
				t.Fatalf("ReadPacket() error = %v", err)
			}

			if decoded.Type() != tt.pkt.Type() {
				t.Errorf("packet type = %d, want %d", decoded.Type(), tt.pkt.Type())
			}
		})
	}
}
