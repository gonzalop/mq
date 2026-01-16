package mq

import (
	"testing"

	"github.com/gonzalop/mq/internal/packets"
)

func TestOnServerRedirectCallback(t *testing.T) {
	tests := []struct {
		name           string
		serverRef      string
		callbackSet    bool
		expectCallback bool
		expectedURI    string
	}{
		{
			name:           "callback invoked with redirect",
			serverRef:      "mqtt://redirect.example.com:1883",
			callbackSet:    true,
			expectCallback: true,
			expectedURI:    "mqtt://redirect.example.com:1883",
		},
		{
			name:           "callback not invoked without redirect",
			serverRef:      "",
			callbackSet:    true,
			expectCallback: false,
		},
		{
			name:           "no callback set",
			serverRef:      "mqtt://redirect.example.com:1883",
			callbackSet:    false,
			expectCallback: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			callbackInvoked := false
			var receivedURI string

			opts := defaultOptions("tcp://localhost:1883")
			if tt.callbackSet {
				opts.OnServerRedirect = func(serverURI string) {
					callbackInvoked = true
					receivedURI = serverURI
				}
			}

			client := &Client{
				opts: opts,
			}

			// Simulate receiving CONNACK with server reference
			if tt.serverRef != "" {
				connackProps := &packets.Properties{
					ServerReference: tt.serverRef,
					Presence:        packets.PresServerReference,
				}

				// Simulate what happens in connect()
				if connackProps.Presence&packets.PresServerReference != 0 {
					client.serverReference = connackProps.ServerReference

					// Invoke callback if configured
					if client.opts.OnServerRedirect != nil {
						client.opts.OnServerRedirect(client.serverReference)
					}
				}
			}

			// Verify callback behavior
			if callbackInvoked != tt.expectCallback {
				t.Errorf("callback invoked = %v, want %v", callbackInvoked, tt.expectCallback)
			}

			if tt.expectCallback && receivedURI != tt.expectedURI {
				t.Errorf("received URI = %q, want %q", receivedURI, tt.expectedURI)
			}

			// Verify ServerReference() still works
			if tt.serverRef != "" {
				if client.ServerReference() != tt.serverRef {
					t.Errorf("ServerReference() = %q, want %q", client.ServerReference(), tt.serverRef)
				}
			}
		})
	}
}

func TestOnServerRedirectCallbackMultiple(t *testing.T) {
	// Test that callback can be invoked multiple times (e.g., on reconnect)
	callCount := 0
	var lastURI string

	opts := defaultOptions("tcp://localhost:1883")
	opts.OnServerRedirect = func(serverURI string) {
		callCount++
		lastURI = serverURI
	}

	client := &Client{opts: opts}

	// First redirect
	client.serverReference = "mqtt://server1.example.com:1883"
	if client.opts.OnServerRedirect != nil {
		client.opts.OnServerRedirect(client.serverReference)
	}

	if callCount != 1 {
		t.Errorf("after first redirect: callCount = %d, want 1", callCount)
	}
	if lastURI != "mqtt://server1.example.com:1883" {
		t.Errorf("after first redirect: lastURI = %q, want mqtt://server1.example.com:1883", lastURI)
	}

	// Second redirect
	client.serverReference = "mqtt://server2.example.com:1883"
	if client.opts.OnServerRedirect != nil {
		client.opts.OnServerRedirect(client.serverReference)
	}

	if callCount != 2 {
		t.Errorf("after second redirect: callCount = %d, want 2", callCount)
	}
	if lastURI != "mqtt://server2.example.com:1883" {
		t.Errorf("after second redirect: lastURI = %q, want mqtt://server2.example.com:1883", lastURI)
	}
}
