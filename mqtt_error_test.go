package mq

import (
	"errors"
	"testing"

	"github.com/gonzalop/mq/internal/packets"
)

func TestMqttError(t *testing.T) {
	t.Run("IsReasonCode", func(t *testing.T) {
		err := &MqttError{ReasonCode: 0x80}
		if !IsReasonCode(err, 0x80) {
			t.Error("IsReasonCode should return true for matching code")
		}
		if IsReasonCode(err, 0x81) {
			t.Error("IsReasonCode should return false for different code")
		}
		if IsReasonCode(errors.New("other"), 0x80) {
			t.Error("IsReasonCode should return false for non-MqttError")
		}
	})

	t.Run("Error formatting", func(t *testing.T) {
		err := &MqttError{ReasonCode: 0x80, Message: "failed"}
		expected := "mqtt error (0x80): failed"
		if err.Error() != expected {
			t.Errorf("Expected %q, got %q", expected, err.Error())
		}

		errNoMsg := &MqttError{ReasonCode: 0x81}
		expectedNoMsg := "mqtt error (0x81)"
		if errNoMsg.Error() != expectedNoMsg {
			t.Errorf("Expected %q, got %q", expectedNoMsg, errNoMsg.Error())
		}
	})
}

func TestMqttError_v5_v3_Compatibility(t *testing.T) {
	setupClient := func(version uint8) *Client {
		return &Client{
			opts: &clientOptions{
				ProtocolVersion: version,
				Logger:          defaultOptions("").Logger,
			},
			pending:  make(map[uint16]*pendingOp),
			outgoing: make(chan packets.Packet, 10),
		}
	}

	t.Run("handlePuback MQTT v5 error", func(t *testing.T) {
		c := setupClient(ProtocolV50)
		tok := newToken()
		c.pending[1] = &pendingOp{token: tok}

		puback := &packets.PubackPacket{PacketID: 1, ReasonCode: 0x80, Version: 5}
		c.handlePuback(puback)

		err := tok.Error()
		if err == nil {
			t.Fatal("Expected error for v5 PUBACK with reason code 0x80, got nil")
		}
		if !IsReasonCode(err, 0x80) {
			t.Errorf("Expected MqttError with reason code 0x80, got %v", err)
		}
	})

	t.Run("handlePuback MQTT v3 ignored reason", func(t *testing.T) {
		c := setupClient(ProtocolV311)
		tok := newToken()
		c.pending[1] = &pendingOp{token: tok}

		// In v3, the byte at offset 2 is not a reason code (packet is shorter)
		// But our internal struct might have it. Let's ensure logic uses version.
		puback := &packets.PubackPacket{PacketID: 1, ReasonCode: 0x80, Version: 4}
		c.handlePuback(puback)

		if err := tok.Error(); err != nil {
			t.Errorf("Expected nil error for v3 PUBACK, got %v", err)
		}
	})

	t.Run("handleSuback MQTT v5 error", func(t *testing.T) {
		c := setupClient(ProtocolV50)
		tok := newToken()
		c.pending[1] = &pendingOp{token: tok}

		suback := &packets.SubackPacket{
			PacketID:    1,
			ReturnCodes: []uint8{0x80},
			Version:     5,
		}
		c.handleSuback(suback)

		err := tok.Error()
		if err == nil {
			t.Fatal("Expected error for v5 SUBACK with 0x80")
		}
		if !IsReasonCode(err, 0x80) {
			t.Errorf("Expected MqttError 0x80, got %v", err)
		}
		if !errors.Is(err, ErrSubscriptionFailed) {
			t.Errorf("Expected error to wrap ErrSubscriptionFailed, got %v", err)
		}
	})

	t.Run("handleSuback MQTT v3 generic error", func(t *testing.T) {
		c := setupClient(ProtocolV311)
		tok := newToken()
		c.pending[1] = &pendingOp{token: tok}

		suback := &packets.SubackPacket{
			PacketID:    1,
			ReturnCodes: []uint8{0x80}, // 0x80 is Failure in v3 too
			Version:     4,
		}
		c.handleSuback(suback)

		err := tok.Error()
		if err == nil {
			t.Fatal("Expected error")
		}
		if IsReasonCode(err, 0x80) {
			t.Error("Should NOT be MqttError for v3.1.1")
		}
		if err != ErrSubscriptionFailed {
			t.Errorf("Expected ErrSubscriptionFailed, got %v", err)
		}
	})

	t.Run("handleUnsuback MQTT v5 error", func(t *testing.T) {
		c := setupClient(ProtocolV50)
		tok := newToken()
		c.pending[1] = &pendingOp{token: tok}

		unsuback := &packets.UnsubackPacket{
			PacketID:    1,
			ReasonCodes: []uint8{0x80},
			Version:     5,
		}
		c.handleUnsuback(unsuback)

		err := tok.Error()
		if err == nil {
			t.Fatal("Expected error")
		}
		if !IsReasonCode(err, 0x80) {
			t.Errorf("Expected MqttError 0x80, got %v", err)
		}
	})

	t.Run("MqttError with ReasonString", func(t *testing.T) {
		// This simulates the logic in client.go for CONNACK
		err := &MqttError{
			ReasonCode: 0x80,
			Message:    "server busy",
			Parent:     ErrConnectionRefused,
		}

		if err.Error() != "mqtt error (0x80): server busy" {
			t.Errorf("Unexpected error message: %v", err.Error())
		}
		if !errors.Is(err, ErrConnectionRefused) {
			t.Error("Should wrap ErrConnectionRefused")
		}
	})
}
