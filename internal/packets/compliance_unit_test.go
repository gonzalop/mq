package packets

import (
	"bytes"
	"testing"
)

func TestCompliance_ValidateFlags(t *testing.T) {
	tests := []struct {
		name       string
		packetType uint8
		flags      uint8
		wantErr    bool
	}{
		{"CONNECT-valid", CONNECT, 0, false},
		{"CONNECT-invalid", CONNECT, 1, true},
		{"CONNACK-valid", CONNACK, 0, false},
		{"CONNACK-invalid", CONNACK, 1, true},
		{"PUBLISH-valid-QoS0", PUBLISH, 0x00, false},
		{"PUBLISH-valid-QoS1", PUBLISH, 0x02, false},
		{"PUBLISH-valid-QoS2", PUBLISH, 0x04, false},
		{"PUBLISH-invalid-QoS3", PUBLISH, 0x06, true},
		{"PUBLISH-invalid-QoS0-DUP", PUBLISH, 0x08, true},
		{"PUBACK-valid", PUBACK, 0, false},
		{"PUBACK-invalid", PUBACK, 1, true},
		{"PUBREC-valid", PUBREC, 0, false},
		{"PUBREC-invalid", PUBREC, 1, true},
		{"PUBREL-valid", PUBREL, 2, false},
		{"PUBREL-invalid-0", PUBREL, 0, true},
		{"PUBREL-invalid-1", PUBREL, 1, true},
		{"PUBCOMP-valid", PUBCOMP, 0, false},
		{"PUBCOMP-invalid", PUBCOMP, 1, true},
		{"SUBSCRIBE-valid", SUBSCRIBE, 2, false},
		{"SUBSCRIBE-invalid", SUBSCRIBE, 0, true},
		{"SUBACK-valid", SUBACK, 0, false},
		{"SUBACK-invalid", SUBACK, 1, true},
		{"UNSUBSCRIBE-valid", UNSUBSCRIBE, 2, false},
		{"UNSUBSCRIBE-invalid", UNSUBSCRIBE, 0, true},
		{"UNSUBACK-valid", UNSUBACK, 0, false},
		{"UNSUBACK-invalid", UNSUBACK, 1, true},
		{"PINGREQ-valid", PINGREQ, 0, false},
		{"PINGREQ-invalid", PINGREQ, 1, true},
		{"PINGRESP-valid", PINGRESP, 0, false},
		{"PINGRESP-invalid", PINGRESP, 1, true},
		{"DISCONNECT-valid", DISCONNECT, 0, false},
		{"DISCONNECT-invalid", DISCONNECT, 1, true},
		{"AUTH-valid", AUTH, 0, false},
		{"AUTH-invalid", AUTH, 1, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateFlags(tt.packetType, tt.flags)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateFlags() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestReadPacket_InvalidFlags(t *testing.T) {
	// PINGREQ with flags = 1 (invalid)
	data := []byte{0xC1, 0x00}
	r := bytes.NewReader(data)
	_, err := ReadPacket(r, 4, 0)
	if err == nil {
		t.Error("ReadPacket accepted PINGREQ with invalid flags")
	}
}

func TestCompliance_DecodeConnect_ReservedBit(t *testing.T) {
	// Protocol Name: MQTT, Level: 4, Flags: 0x03 (Reserved bit 0 set), KeepAlive: 60, ClientID: "test"
	data := []byte{
		0, 4, 'M', 'Q', 'T', 'T',
		4,
		0x03, // Flags: CleanSession=1, Reserved=1
		0, 60,
		0, 4, 't', 'e', 's', 't',
	}

	_, err := DecodeConnect(data)
	if err == nil {
		t.Error("DecodeConnect accepted CONNECT with reserved bit set")
	}
}

func TestCompliance_DecodeConnect_WillFlags(t *testing.T) {
	// WillFlag=0, but WillQoS=1
	data := []byte{
		0, 4, 'M', 'Q', 'T', 'T',
		4,
		0x08, // Flags: WillFlag=0, WillQoS=1
		0, 60,
		0, 4, 't', 'e', 's', 't',
	}

	_, err := DecodeConnect(data)
	if err == nil {
		t.Error("DecodeConnect accepted CONNECT with WillFlag=0 and WillQoS=1")
	}
}

func TestCompliance_DecodeSubscribe_NoTopics(t *testing.T) {
	// Packet ID: 1, No topics
	data := []byte{0, 1}

	_, err := DecodeSubscribe(data, 4)
	if err == nil {
		t.Error("DecodeSubscribe accepted SUBSCRIBE with no topics")
	}
}

func TestCompliance_DecodeUnsubscribe_NoTopics(t *testing.T) {
	// Packet ID: 1, No topics
	data := []byte{0, 1}

	_, err := DecodeUnsubscribe(data, 4)
	if err == nil {
		t.Error("DecodeUnsubscribe accepted UNSUBSCRIBE with no topics")
	}
}

func TestCompliance_DecodeSubscribe_ReservedBits(t *testing.T) {
	// Packet ID: 1, Topic: "a", Options: 0x04 (Reserved bit 2 set in v3.1.1)
	data := []byte{
		0, 1,
		0, 1, 'a',
		0x04,
	}

	_, err := DecodeSubscribe(data, 4)
	if err == nil {
		t.Error("DecodeSubscribe accepted v3.1.1 SUBSCRIBE with reserved bit set")
	}
}

func TestCompliance_DuplicateProperties(t *testing.T) {
	// PropContentType (0x03) repeated twice
	data := []byte{
		7,                      // Properties length (1 ID + 2 len + 1 "a" = 4) * 2 = 8? No, 1+2+1=4. Two of them = 8.
		PropContentType, 0, 1, 'a',
		PropContentType, 0, 1, 'b',
	}

	// Adjust properties length
	data[0] = byte(len(data) - 1)

	_, _, err := decodeProperties(data)
	if err == nil {
		t.Error("decodeProperties accepted duplicate ContentType property")
	} else {
		t.Logf("Passed: Duplicate property rejected: %v", err)
	}
}
