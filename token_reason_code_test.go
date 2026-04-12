package mq

import (
	"testing"
	"time"

	"github.com/gonzalop/mq/internal/packets"
)

func TestTokenReasonCode_Puback(t *testing.T) {
	setupV5Client := func() *Client {
		opts := defaultOptions("tcp://localhost:1883")
		opts.ProtocolVersion = ProtocolV50
		return &Client{
			pending: make(map[uint16]*pendingOp),
			opts:    opts,
		}
	}

	t.Run("success", func(t *testing.T) {
		c := setupV5Client()
		tok := newToken()
		c.pending[1] = &pendingOp{token: tok}

		c.handlePuback(&packets.PubackPacket{PacketID: 1, ReasonCode: 0x00, Version: 5})

		if tok.ReasonCode() != ReasonCodeSuccess {
			t.Errorf("expected ReasonCodeSuccess, got 0x%02X", tok.ReasonCode())
		}
		if tok.Error() != nil {
			t.Errorf("expected nil error, got %v", tok.Error())
		}
	})

	t.Run("no matching subscribers", func(t *testing.T) {
		c := setupV5Client()
		tok := newToken()
		c.pending[1] = &pendingOp{token: tok}

		c.handlePuback(&packets.PubackPacket{PacketID: 1, ReasonCode: 0x10, Version: 5})

		if tok.ReasonCode() != ReasonCodeNoMatchingSubscribers {
			t.Errorf("expected ReasonCodeNoMatchingSubscribers (0x10), got 0x%02X", tok.ReasonCode())
		}
		if tok.Error() != nil {
			t.Errorf("expected nil error for non-error reason code, got %v", tok.Error())
		}
	})

	t.Run("error reason code", func(t *testing.T) {
		c := setupV5Client()
		tok := newToken()
		c.pending[1] = &pendingOp{token: tok}

		c.handlePuback(&packets.PubackPacket{PacketID: 1, ReasonCode: 0x80, Version: 5})

		if tok.ReasonCode() != ReasonCodeUnspecifiedError {
			t.Errorf("expected ReasonCodeUnspecifiedError (0x80), got 0x%02X", tok.ReasonCode())
		}
		if tok.Error() == nil {
			t.Error("expected error for reason code >= 0x80")
		}
	})

	t.Run("v3 does not set reason code", func(t *testing.T) {
		opts := defaultOptions("tcp://localhost:1883")
		opts.ProtocolVersion = ProtocolV311
		c := &Client{
			pending: make(map[uint16]*pendingOp),
			opts:    opts,
		}
		tok := newToken()
		c.pending[1] = &pendingOp{token: tok}

		c.handlePuback(&packets.PubackPacket{PacketID: 1, ReasonCode: 0x10, Version: 4})

		if tok.ReasonCode() != ReasonCodeSuccess {
			t.Errorf("v3 should leave reason code as default (0x00), got 0x%02X", tok.ReasonCode())
		}
	})
}

func TestTokenReasonCode_Pubcomp(t *testing.T) {
	setupV5Client := func() *Client {
		opts := defaultOptions("tcp://localhost:1883")
		opts.ProtocolVersion = ProtocolV50
		return &Client{
			pending: make(map[uint16]*pendingOp),
			opts:    opts,
		}
	}

	t.Run("success", func(t *testing.T) {
		c := setupV5Client()
		tok := newToken()
		c.pending[1] = &pendingOp{token: tok, qos: 2}
		c.inFlightCount = 1

		c.handlePubcomp(&packets.PubcompPacket{PacketID: 1, ReasonCode: 0x00})

		if tok.ReasonCode() != ReasonCodeSuccess {
			t.Errorf("expected ReasonCodeSuccess, got 0x%02X", tok.ReasonCode())
		}
	})

	t.Run("error reason code", func(t *testing.T) {
		c := setupV5Client()
		tok := newToken()
		c.pending[1] = &pendingOp{token: tok, qos: 2}
		c.inFlightCount = 1

		c.handlePubcomp(&packets.PubcompPacket{PacketID: 1, ReasonCode: 0x92})

		if tok.ReasonCode() != ReasonCode(0x92) {
			t.Errorf("expected reason code 0x92, got 0x%02X", tok.ReasonCode())
		}
		if tok.Error() == nil {
			t.Error("expected error for reason code >= 0x80")
		}
	})
}

func TestTokenReasonCode_Pubrec(t *testing.T) {
	setupV5Client := func() *Client {
		opts := defaultOptions("tcp://localhost:1883")
		opts.ProtocolVersion = ProtocolV50
		return &Client{
			pending:  make(map[uint16]*pendingOp),
			opts:     opts,
			outgoing: make(chan packets.Packet, 10),
		}
	}

	t.Run("success sets reason code without completing token", func(t *testing.T) {
		c := setupV5Client()
		tok := newToken()
		c.pending[1] = &pendingOp{
			token:     tok,
			qos:       2,
			packet:    &packets.PublishPacket{PacketID: 1, QoS: 2},
			timestamp: time.Now(),
		}

		c.handlePubrec(&packets.PubrecPacket{PacketID: 1, ReasonCode: 0x10, Version: 5})

		// Token should NOT be completed yet (QoS 2 flow continues with PUBREL/PUBCOMP)
		select {
		case <-tok.Done():
			t.Error("token should not be completed after PUBREC success")
		default:
		}

		// But reason code should be set
		if tok.reasonCode != ReasonCodeNoMatchingSubscribers {
			t.Errorf("expected ReasonCodeNoMatchingSubscribers (0x10), got 0x%02X", tok.reasonCode)
		}
	})

	t.Run("error completes token with reason code", func(t *testing.T) {
		c := setupV5Client()
		tok := newToken()
		c.pending[1] = &pendingOp{
			token:     tok,
			qos:       2,
			packet:    &packets.PublishPacket{PacketID: 1, QoS: 2},
			timestamp: time.Now(),
		}

		c.handlePubrec(&packets.PubrecPacket{PacketID: 1, ReasonCode: 0x80, Version: 5})

		select {
		case <-tok.Done():
			if tok.ReasonCode() != ReasonCodeUnspecifiedError {
				t.Errorf("expected 0x80, got 0x%02X", tok.ReasonCode())
			}
			if tok.Error() == nil {
				t.Error("expected error")
			}
		case <-time.After(100 * time.Millisecond):
			t.Error("token should be completed")
		}
	})
}

func TestTokenReasonCode_Suback(t *testing.T) {
	setupV5Client := func() *Client {
		opts := defaultOptions("tcp://localhost:1883")
		opts.ProtocolVersion = ProtocolV50
		return &Client{
			pending:       make(map[uint16]*pendingOp),
			subscriptions: make(map[string]subscriptionEntry),
			opts:          opts,
		}
	}

	t.Run("granted QoS 0", func(t *testing.T) {
		c := setupV5Client()
		tok := newToken()
		c.pending[1] = &pendingOp{token: tok, packet: &packets.SubscribePacket{Topics: []string{"t"}}}

		c.handleSuback(&packets.SubackPacket{PacketID: 1, ReturnCodes: []uint8{0x00}, Version: 5})

		if tok.ReasonCode() != ReasonCodeGrantedQoS0 {
			t.Errorf("expected ReasonCodeGrantedQoS0 (0x00), got 0x%02X", tok.ReasonCode())
		}
	})

	t.Run("granted QoS 1", func(t *testing.T) {
		c := setupV5Client()
		tok := newToken()
		c.pending[1] = &pendingOp{token: tok, packet: &packets.SubscribePacket{Topics: []string{"t"}}}

		c.handleSuback(&packets.SubackPacket{PacketID: 1, ReturnCodes: []uint8{0x01}, Version: 5})

		if tok.ReasonCode() != ReasonCodeGrantedQoS1 {
			t.Errorf("expected ReasonCodeGrantedQoS1 (0x01), got 0x%02X", tok.ReasonCode())
		}
		if tok.Error() != nil {
			t.Errorf("expected nil error, got %v", tok.Error())
		}
	})

	t.Run("granted QoS 2", func(t *testing.T) {
		c := setupV5Client()
		tok := newToken()
		c.pending[1] = &pendingOp{token: tok, packet: &packets.SubscribePacket{Topics: []string{"t"}}}

		c.handleSuback(&packets.SubackPacket{PacketID: 1, ReturnCodes: []uint8{0x02}, Version: 5})

		if tok.ReasonCode() != ReasonCodeGrantedQoS2 {
			t.Errorf("expected ReasonCodeGrantedQoS2 (0x02), got 0x%02X", tok.ReasonCode())
		}
	})

	t.Run("subscription failure", func(t *testing.T) {
		c := setupV5Client()
		tok := newToken()
		c.pending[1] = &pendingOp{token: tok, packet: &packets.SubscribePacket{Topics: []string{"t"}}}

		c.handleSuback(&packets.SubackPacket{PacketID: 1, ReturnCodes: []uint8{0x87}, Version: 5})

		if tok.ReasonCode() != ReasonCodeNotAuthorized {
			t.Errorf("expected ReasonCodeNotAuthorized (0x87), got 0x%02X", tok.ReasonCode())
		}
		if tok.Error() == nil {
			t.Error("expected error for reason code >= 0x80")
		}
	})

	t.Run("v3 granted QoS", func(t *testing.T) {
		opts := defaultOptions("tcp://localhost:1883")
		opts.ProtocolVersion = ProtocolV311
		c := &Client{
			pending:       make(map[uint16]*pendingOp),
			subscriptions: make(map[string]subscriptionEntry),
			opts:          opts,
		}
		tok := newToken()
		c.pending[1] = &pendingOp{token: tok, packet: &packets.SubscribePacket{Topics: []string{"t"}}}

		c.handleSuback(&packets.SubackPacket{PacketID: 1, ReturnCodes: []uint8{0x01}, Version: 4})

		// v3 SUBACK return codes (0x00-0x02) are set regardless of version
		if tok.ReasonCode() != ReasonCodeGrantedQoS1 {
			t.Errorf("expected 0x01 even for v3, got 0x%02X", tok.ReasonCode())
		}
	})
}

func TestTokenReasonCode_Unsuback(t *testing.T) {
	setupV5Client := func() *Client {
		opts := defaultOptions("tcp://localhost:1883")
		opts.ProtocolVersion = ProtocolV50
		return &Client{
			pending: make(map[uint16]*pendingOp),
			opts:    opts,
		}
	}

	t.Run("success", func(t *testing.T) {
		c := setupV5Client()
		tok := newToken()
		c.pending[1] = &pendingOp{
			token:  tok,
			packet: &packets.UnsubscribePacket{Topics: []string{"t"}},
		}

		c.handleUnsuback(&packets.UnsubackPacket{PacketID: 1, ReasonCodes: []uint8{0x00}, Version: 5})

		if tok.ReasonCode() != ReasonCodeSuccess {
			t.Errorf("expected ReasonCodeSuccess, got 0x%02X", tok.ReasonCode())
		}
	})

	t.Run("no subscription existed", func(t *testing.T) {
		c := setupV5Client()
		tok := newToken()
		c.pending[1] = &pendingOp{
			token:  tok,
			packet: &packets.UnsubscribePacket{Topics: []string{"t"}},
		}

		c.handleUnsuback(&packets.UnsubackPacket{PacketID: 1, ReasonCodes: []uint8{0x11}, Version: 5})

		if tok.ReasonCode() != ReasonCodeNoSubscriptionExisted {
			t.Errorf("expected ReasonCodeNoSubscriptionExisted (0x11), got 0x%02X", tok.ReasonCode())
		}
		if tok.Error() != nil {
			t.Errorf("expected nil error for non-error reason code, got %v", tok.Error())
		}
	})

	t.Run("error reason code", func(t *testing.T) {
		c := setupV5Client()
		tok := newToken()
		c.pending[1] = &pendingOp{
			token:  tok,
			packet: &packets.UnsubscribePacket{Topics: []string{"t"}},
		}

		c.handleUnsuback(&packets.UnsubackPacket{PacketID: 1, ReasonCodes: []uint8{0x80}, Version: 5})

		if tok.ReasonCode() != ReasonCodeUnspecifiedError {
			t.Errorf("expected 0x80, got 0x%02X", tok.ReasonCode())
		}
		if tok.Error() == nil {
			t.Error("expected error")
		}
	})
}

func TestTokenReasonCode_Default(t *testing.T) {
	tok := newToken()
	if tok.ReasonCode() != ReasonCodeSuccess {
		t.Errorf("new token should have default reason code 0x00, got 0x%02X", tok.ReasonCode())
	}
}
