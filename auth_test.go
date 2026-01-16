package mq

import (
	"context"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/gonzalop/mq/internal/packets"
)

// Simple token authenticator for testing
type tokenAuthenticator struct {
	token          string
	challengeCount int
}

func (t *tokenAuthenticator) Method() string {
	return "TOKEN"
}

func (t *tokenAuthenticator) InitialData() ([]byte, error) {
	return []byte(t.token), nil
}

func (t *tokenAuthenticator) HandleChallenge(data []byte, reasonCode uint8) ([]byte, error) {
	t.challengeCount++
	return []byte("challenge-response"), nil
}

func (t *tokenAuthenticator) Complete() error {
	return nil
}

func TestAuthenticatorInCONNECT(t *testing.T) {
	auth := &tokenAuthenticator{token: "test-token-123"}

	client := &Client{
		opts: &clientOptions{
			ProtocolVersion: ProtocolV50,
			Authenticator:   auth,
			Logger:          testLogger(),
		},
	}

	pkt := client.buildConnectPacket()

	// Verify authentication method is set
	if pkt.Properties == nil {
		t.Fatal("expected properties to be set")
	}

	if pkt.Properties.AuthenticationMethod != "TOKEN" {
		t.Errorf("expected method TOKEN, got %s", pkt.Properties.AuthenticationMethod)
	}

	// Verify authentication data is set
	if string(pkt.Properties.AuthenticationData) != "test-token-123" {
		t.Errorf("expected data 'test-token-123', got %s", pkt.Properties.AuthenticationData)
	}
}

func TestHandleAuth(t *testing.T) {
	auth := &tokenAuthenticator{token: "test-token"}

	client := &Client{
		opts: &clientOptions{
			ProtocolVersion: ProtocolV50,
			Authenticator:   auth,
			Logger:          testLogger(),
		},
		outgoing: make(chan packets.Packet, 1),
	}

	// Simulate AUTH challenge from server
	authPkt := &packets.AuthPacket{
		ReasonCode: packets.AuthReasonContinue,
		Properties: &packets.Properties{
			AuthenticationMethod: "TOKEN",
			AuthenticationData:   []byte("server-challenge"),
			Presence:             packets.PresAuthenticationMethod,
		},
		Version: 5,
	}

	client.handleAuth(authPkt)

	// Verify response was sent
	select {
	case resp := <-client.outgoing:
		authResp, ok := resp.(*packets.AuthPacket)
		if !ok {
			t.Fatalf("expected AuthPacket, got %T", resp)
		}

		if authResp.ReasonCode != packets.AuthReasonContinue {
			t.Errorf("expected reason code %d, got %d", packets.AuthReasonContinue, authResp.ReasonCode)
		}

		if string(authResp.Properties.AuthenticationData) != "challenge-response" {
			t.Errorf("expected 'challenge-response', got %s", authResp.Properties.AuthenticationData)
		}

	default:
		t.Error("expected AUTH response to be sent")
	}

	// Verify HandleChallenge was called
	if auth.challengeCount != 1 {
		t.Errorf("expected 1 challenge, got %d", auth.challengeCount)
	}
}

func TestReauthenticate(t *testing.T) {
	auth := &tokenAuthenticator{token: "refresh-token"}

	client := &Client{
		opts: &clientOptions{
			ProtocolVersion: ProtocolV50,
			Authenticator:   auth,
			Logger:          testLogger(),
		},
		outgoing: make(chan packets.Packet, 1),
	}
	client.connected.Store(true)

	err := client.Reauthenticate(context.TODO())
	if err != nil {
		t.Fatalf("Reauthenticate failed: %v", err)
	}

	// Verify AUTH packet was sent
	select {
	case pkt := <-client.outgoing:
		authPkt, ok := pkt.(*packets.AuthPacket)
		if !ok {
			t.Fatalf("expected AuthPacket, got %T", pkt)
		}

		if authPkt.ReasonCode != packets.AuthReasonReauthenticate {
			t.Errorf("expected reason code %d, got %d", packets.AuthReasonReauthenticate, authPkt.ReasonCode)
		}

		if authPkt.Properties.AuthenticationMethod != "TOKEN" {
			t.Errorf("expected method TOKEN, got %s", authPkt.Properties.AuthenticationMethod)
		}

	default:
		t.Error("expected AUTH packet to be sent")
	}
}

func TestReauthenticateErrors(t *testing.T) {
	tests := []struct {
		name        string
		version     uint8
		auth        Authenticator
		connected   bool
		expectError string
	}{
		{
			name:        "MQTT v3.1.1",
			version:     ProtocolV311,
			auth:        &tokenAuthenticator{},
			connected:   true,
			expectError: "requires MQTT v5.0",
		},
		{
			name:        "no authenticator",
			version:     ProtocolV50,
			auth:        nil,
			connected:   true,
			expectError: "no authenticator configured",
		},
		{
			name:        "not connected",
			version:     ProtocolV50,
			auth:        &tokenAuthenticator{},
			connected:   false,
			expectError: "not connected",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &Client{
				opts: &clientOptions{
					ProtocolVersion: tt.version,
					Authenticator:   tt.auth,
					Logger:          testLogger(),
				},
				outgoing: make(chan packets.Packet, 1),
			}
			client.connected.Store(tt.connected)

			err := client.Reauthenticate(context.TODO())
			if err == nil {
				t.Fatal("expected error, got nil")
			}

			if !contains(err.Error(), tt.expectError) {
				t.Errorf("expected error containing %q, got %q", tt.expectError, err.Error())
			}
		})
	}
}

// PingPongAuthenticator implements a simple challenge-response mechanism
// for testing the MQTT v5.0 Enhanced Authentication flow.
type PingPongAuthenticator struct{}

func (a *PingPongAuthenticator) Method() string {
	return "PING-PONG"
}

func (a *PingPongAuthenticator) InitialData() ([]byte, error) {
	// Client sends no initial data, just the method
	return nil, nil
}

func (a *PingPongAuthenticator) HandleChallenge(data []byte, code uint8) ([]byte, error) {
	if string(data) == "PING" {
		return []byte("PONG"), nil
	}
	return nil, fmt.Errorf("unexpected challenge: %s", string(data))
}

func (a *PingPongAuthenticator) Complete() error {
	return nil
}

func TestEnhancedAuthenticationFlow(t *testing.T) {
	// 1. Create a pipe to simulate network connection
	clientConn, serverConn := net.Pipe()

	// 2. Start Mock Server
	errCh := make(chan error, 1)
	go func() {
		defer serverConn.Close()

		// A. Read CONNECT
		pkt, err := packets.ReadPacket(serverConn, 5, 1024*1024)
		if err != nil {
			errCh <- fmt.Errorf("server read CONNECT: %w", err)
			return
		}
		connect, ok := pkt.(*packets.ConnectPacket)
		if !ok {
			errCh <- fmt.Errorf("expected CONNECT, got %T", pkt)
			return
		}

		// Verify Auth Method
		if connect.Properties == nil || connect.Properties.AuthenticationMethod != "PING-PONG" {
			errCh <- fmt.Errorf("expected AuthMethod 'PING-PONG', got properties: %+v", connect.Properties)
			return
		}

		// B. Send AUTH (Challenge)
		authChallenge := &packets.AuthPacket{
			Version:    5,
			ReasonCode: packets.AuthReasonContinue, // 0x18
			Properties: &packets.Properties{
				AuthenticationMethod: "PING-PONG",
				AuthenticationData:   []byte("PING"),
				Presence:             packets.PresAuthenticationMethod,
			},
		}
		if _, err := authChallenge.WriteTo(serverConn); err != nil {
			errCh <- fmt.Errorf("server write AUTH challenge: %w", err)
			return
		}

		// C. Read AUTH (Response)
		pkt, err = packets.ReadPacket(serverConn, 5, 1024*1024)
		if err != nil {
			errCh <- fmt.Errorf("server read AUTH response: %w", err)
			return
		}
		authResp, ok := pkt.(*packets.AuthPacket)
		if !ok {
			errCh <- fmt.Errorf("expected AUTH response, got %T", pkt)
			return
		}

		// Verify Response
		if authResp.ReasonCode != packets.AuthReasonContinue {
			errCh <- fmt.Errorf("expected AUTH ReasonCode 0x18, got 0x%02x", authResp.ReasonCode)
			return
		}
		if string(authResp.Properties.AuthenticationData) != "PONG" {
			errCh <- fmt.Errorf("expected AuthData 'PONG', got %q", authResp.Properties.AuthenticationData)
			return
		}

		// D. Send CONNACK (Success)
		connack := &packets.ConnackPacket{
			ReturnCode: packets.ConnAccepted,
			Properties: &packets.Properties{}, // Empty properties
		}
		if _, err := connack.WriteTo(serverConn); err != nil {
			errCh <- fmt.Errorf("server write CONNACK: %w", err)
			return
		}

		errCh <- nil
	}()

	// 3. Start Client
	// Use a custom dialer that returns our pipe connection
	dialer := DialFunc(func(ctx context.Context, network, addr string) (net.Conn, error) {
		return clientConn, nil
	})

	client, err := Dial("tcp://mock-server:1883",
		WithClientID("auth-client"),
		WithProtocolVersion(ProtocolV50),
		WithAuthenticator(&PingPongAuthenticator{}),
		WithDialer(dialer),
		WithConnectTimeout(2*time.Second),
	)

	// Check server error first
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("Mock Server failed: %v", err)
		}
	default:
	}

	if err != nil {
		t.Fatalf("Client Dial failed: %v", err)
	}
	defer func() { _ = client.Disconnect(context.Background()) }()

	// Verify connected state
	if !client.IsConnected() {
		t.Error("Client should be connected")
	}
}
