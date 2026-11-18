package mq

import (
	"context"
	"errors"
	"net"
	"slices"
	"testing"
	"time"

	"github.com/gonzalop/mq/internal/packets"
)

func TestMqttError(t *testing.T) {
	t.Run("errors.Is", func(t *testing.T) {
		err := &MqttError{ReasonCode: ReasonCodeUnspecifiedError}
		if !errors.Is(err, ReasonCodeUnspecifiedError) {
			t.Error("errors.Is should return true for matching code")
		}
		if errors.Is(err, ReasonCodeMalformedPacket) {
			t.Error("errors.Is should return false for different code")
		}
		if errors.Is(errors.New("other"), ReasonCodeUnspecifiedError) {
			t.Error("errors.Is should return false for non-MqttError")
		}
	})

	t.Run("Error formatting", func(t *testing.T) {
		err := &MqttError{ReasonCode: ReasonCodeUnspecifiedError, Message: "failed"}
		expected := "mqtt error (0x80): failed"
		if err.Error() != expected {
			t.Errorf("Expected %q, got %q", expected, err.Error())
		}

		errNoMsg := &MqttError{ReasonCode: ReasonCodeMalformedPacket}
		expectedNoMsg := "mqtt error (0x81)"
		if errNoMsg.Error() != expectedNoMsg {
			t.Errorf("Expected %q, got %q", expectedNoMsg, errNoMsg.Error())
		}
	})
}

func TestMqttError_v5_v3_Compatibility(t *testing.T) {
	setupClient := func(version ProtocolVersion) *Client {
		return &Client{trie: newTopicTrie(),
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
		c.handleAck(puback.PacketID, puback.ReasonCode)

		err := tok.Error()
		if err == nil {
			t.Fatal("Expected error for v5 PUBACK with reason code 0x80, got nil")
		}
		if !errors.Is(err, ReasonCodeUnspecifiedError) {
			t.Errorf("Expected ReasonCodeUnspecifiedError, got %v", err)
		}
	})

	t.Run("handlePuback MQTT v3 ignored reason", func(t *testing.T) {
		c := setupClient(ProtocolV311)
		tok := newToken()
		c.pending[1] = &pendingOp{token: tok}

		// In v3, the byte at offset 2 is not a reason code (packet is shorter)
		// But our internal struct might have it. Let's ensure logic uses version.
		puback := &packets.PubackPacket{PacketID: 1, ReasonCode: 0x80, Version: 4}
		c.handleAck(puback.PacketID, puback.ReasonCode)

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
		if !errors.Is(err, ReasonCodeUnspecifiedError) {
			t.Errorf("Expected ReasonCodeUnspecifiedError, got %v", err)
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
		if errors.Is(err, ReasonCodeUnspecifiedError) {
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
		if !errors.Is(err, ReasonCodeUnspecifiedError) {
			t.Errorf("Expected ReasonCodeUnspecifiedError, got %v", err)
		}
	})

	t.Run("MqttError with ReasonString", func(t *testing.T) {
		// This simulates the logic in client_connection.go for CONNACK
		err := &MqttError{
			ReasonCode: ReasonCodeUnspecifiedError,
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

	t.Run("DisconnectError UserPropertyList", func(t *testing.T) {
		c := setupClient(ProtocolV50)
		c.handleDisconnectPacket(&packets.DisconnectPacket{
			Version:    5,
			ReasonCode: uint8(ReasonCodeServerMoved),
			Properties: &packets.Properties{
				ServerReference: "mqtt://other:1883",
				Presence:        packets.PresServerReference,
				UserProperties: []packets.UserProperty{
					{Key: "route", Value: "a"},
					{Key: "note", Value: "ok"},
					{Key: "route", Value: "b"},
				},
			},
		})

		wantList := []UserProperty{
			{Key: "route", Value: "a"},
			{Key: "note", Value: "ok"},
			{Key: "route", Value: "b"},
		}
		dErr, ok := c.lastDisconnectReason.(*DisconnectError)
		if !ok {
			t.Fatalf("expected *DisconnectError, got %T: %v", c.lastDisconnectReason, c.lastDisconnectReason)
		}
		if !slices.Equal(dErr.UserPropertyList, wantList) {
			t.Fatalf("UserPropertyList = %v, want %v", dErr.UserPropertyList, wantList)
		}
		if dErr.UserProperties["route"] != "b" || dErr.UserProperties["note"] != "ok" {
			t.Fatalf("UserProperties = %v, unexpected values", dErr.UserProperties)
		}
	})
}

type pipeDialer struct {
	conn net.Conn
}

func (d pipeDialer) DialContext(_ context.Context, _, _ string) (net.Conn, error) {
	return d.conn, nil
}

func TestRejectedConnackProperties(t *testing.T) {
	t.Run("validateConnack preserves ServerReference, UserProperties, and calls OnServerRedirect", func(t *testing.T) {
		redirected := make(chan string, 1)
		opts := defaultOptions("tcp://localhost:1883")
		opts.ProtocolVersion = ProtocolV50
		opts.OnServerRedirect = func(serverURI string) {
			redirected <- serverURI
		}

		c := &Client{trie: newTopicTrie(), opts: opts}
		clientConn, serverConn := net.Pipe()
		defer serverConn.Close()

		connack := &packets.ConnackPacket{
			ReturnCode: uint8(ReasonCodeUseAnotherServer),
			Properties: &packets.Properties{
				Presence:        packets.PresReasonString | packets.PresServerReference,
				ReasonString:    "use regional broker",
				ServerReference: "mqtts://eu-west.example.com:8883",
				UserProperties: []packets.UserProperty{
					{Key: "region", Value: "eu-west"},
					{Key: "region", Value: "eu-central"},
					{Key: "hint", Value: "tls"},
				},
			},
		}

		err := c.validateConnack(clientConn, connack)
		var mErr *MqttError
		if !errors.As(err, &mErr) {
			t.Fatalf("expected *MqttError, got %T: %v", err, err)
		}
		if !errors.Is(err, ReasonCodeUseAnotherServer) {
			t.Errorf("errors.Is(err, ReasonCodeUseAnotherServer) = false, err = %v", err)
		}
		if !errors.Is(err, ErrConnectionRefused) {
			t.Errorf("errors.Is(err, ErrConnectionRefused) = false, err = %v", err)
		}
		if mErr.Message != "use regional broker" {
			t.Errorf("Message = %q, want %q", mErr.Message, "use regional broker")
		}
		if mErr.ServerReference != "mqtts://eu-west.example.com:8883" {
			t.Errorf("ServerReference = %q, want %q", mErr.ServerReference, "mqtts://eu-west.example.com:8883")
		}
		if c.ServerReference() != "mqtts://eu-west.example.com:8883" {
			t.Errorf("client.ServerReference() = %q, want %q", c.ServerReference(), "mqtts://eu-west.example.com:8883")
		}

		wantList := []UserProperty{
			{Key: "region", Value: "eu-west"},
			{Key: "region", Value: "eu-central"},
			{Key: "hint", Value: "tls"},
		}
		if !slices.Equal(mErr.UserPropertyList, wantList) {
			t.Errorf("UserPropertyList = %v, want %v", mErr.UserPropertyList, wantList)
		}
		if mErr.UserProperties["region"] != "eu-central" || mErr.UserProperties["hint"] != "tls" {
			t.Errorf("UserProperties = %v, unexpected map contents", mErr.UserProperties)
		}

		select {
		case gotURI := <-redirected:
			if gotURI != "mqtts://eu-west.example.com:8883" {
				t.Errorf("OnServerRedirect got %q, want %q", gotURI, "mqtts://eu-west.example.com:8883")
			}
		case <-time.After(time.Second):
			t.Fatal("timed out waiting for OnServerRedirect callback")
		}
	})

	t.Run("validateConnack leaves properties empty when absent", func(t *testing.T) {
		opts := defaultOptions("tcp://localhost:1883")
		opts.ProtocolVersion = ProtocolV50
		called := make(chan string, 1)
		opts.OnServerRedirect = func(serverURI string) {
			called <- serverURI
		}

		c := &Client{trie: newTopicTrie(), opts: opts}
		clientConn, serverConn := net.Pipe()
		defer serverConn.Close()

		err := c.validateConnack(clientConn, &packets.ConnackPacket{
			ReturnCode: uint8(ReasonCodeNotAuthorized),
		})
		var mErr *MqttError
		if !errors.As(err, &mErr) {
			t.Fatalf("expected *MqttError, got %T: %v", err, err)
		}
		if mErr.ServerReference != "" {
			t.Errorf("ServerReference = %q, want empty", mErr.ServerReference)
		}
		if mErr.UserProperties != nil || mErr.UserPropertyList != nil {
			t.Errorf("expected nil UserProperties/UserPropertyList, got %v / %v", mErr.UserProperties, mErr.UserPropertyList)
		}
		select {
		case uri := <-called:
			t.Errorf("unexpected OnServerRedirect call with %q", uri)
		case <-time.After(20 * time.Millisecond):
		}
	})

	t.Run("DialContext returns ServerReference and UserProperties on ServerMoved CONNACK", func(t *testing.T) {
		clientConn, serverConn := net.Pipe()
		defer serverConn.Close()

		serverErr := make(chan error, 1)
		go func() {
			pkt, err := packets.ReadPacket(serverConn, 5, 0)
			if err != nil {
				serverErr <- err
				return
			}
			if _, ok := pkt.(*packets.ConnectPacket); !ok {
				serverErr <- errors.New("expected CONNECT packet")
				return
			}
			connack := &packets.ConnackPacket{
				ReturnCode: uint8(ReasonCodeServerMoved),
				Properties: &packets.Properties{
					Presence:        packets.PresReasonString | packets.PresServerReference,
					ReasonString:    "moved permanently",
					ServerReference: "tcp://new-broker.example.com:1883",
					UserProperties: []packets.UserProperty{
						{Key: "route", Value: "primary"},
						{Key: "route", Value: "secondary"},
					},
				},
			}
			_, err = connack.WriteTo(serverConn)
			serverErr <- err
		}()

		redirected := make(chan string, 1)
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		client, err := DialContext(ctx, "tcp://old-broker.example.com:1883",
			WithProtocolVersion(ProtocolV50),
			WithDialer(pipeDialer{conn: clientConn}),
			WithOnServerRedirect(func(serverURI string) {
				redirected <- serverURI
			}),
		)
		if client != nil {
			t.Fatalf("expected nil client on rejected CONNACK, got %v", client)
		}
		if sErr := <-serverErr; sErr != nil {
			t.Fatalf("mock server error: %v", sErr)
		}

		var mErr *MqttError
		if !errors.As(err, &mErr) {
			t.Fatalf("expected *MqttError, got %T: %v", err, err)
		}
		if !errors.Is(err, ReasonCodeServerMoved) {
			t.Errorf("errors.Is(err, ReasonCodeServerMoved) = false, err = %v", err)
		}
		if mErr.Message != "moved permanently" {
			t.Errorf("Message = %q, want %q", mErr.Message, "moved permanently")
		}
		if mErr.ServerReference != "tcp://new-broker.example.com:1883" {
			t.Errorf("ServerReference = %q, want %q", mErr.ServerReference, "tcp://new-broker.example.com:1883")
		}
		wantList := []UserProperty{
			{Key: "route", Value: "primary"},
			{Key: "route", Value: "secondary"},
		}
		if !slices.Equal(mErr.UserPropertyList, wantList) {
			t.Errorf("UserPropertyList = %v, want %v", mErr.UserPropertyList, wantList)
		}
		if mErr.UserProperties["route"] != "secondary" {
			t.Errorf("UserProperties[route] = %q, want %q", mErr.UserProperties["route"], "secondary")
		}

		select {
		case gotURI := <-redirected:
			if gotURI != "tcp://new-broker.example.com:1883" {
				t.Errorf("OnServerRedirect got %q, want %q", gotURI, "tcp://new-broker.example.com:1883")
			}
		case <-time.After(time.Second):
			t.Fatal("timed out waiting for OnServerRedirect callback")
		}
	})
}
