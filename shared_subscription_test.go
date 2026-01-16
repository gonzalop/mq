package mq

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/gonzalop/mq/internal/packets"
)

func TestSharedSubscriptionNoLocalValidation(t *testing.T) {
	// Setup a dummy client
	c := &Client{
		opts:          defaultOptions("tcp://localhost:1883"),
		subscriptions: make(map[string]subscriptionEntry),
		pending:       make(map[uint16]*pendingOp),
	}
	// Simple logger
	c.opts.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))

	handler := func(c *Client, msg Message) {}

	tests := []struct {
		name      string
		topic     string
		noLocal   bool
		wantError bool
	}{
		{
			name:      "shared subscription with NoLocal",
			topic:     "$share/group1/topic",
			noLocal:   true,
			wantError: true,
		},
		{
			name:      "shared subscription without NoLocal",
			topic:     "$share/group1/topic",
			noLocal:   false,
			wantError: false,
		},
		{
			name:      "normal subscription with NoLocal",
			topic:     "normal/topic",
			noLocal:   true,
			wantError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {

			// For the purposes of this test, we only care about the initial validation errors
			// returned by Subscribe, not the actual sending mechanism.
			// However, Subscribe sends to 'outgoing' channel. We need a channel to prevent blocking.
			c.outgoing = make(chan packets.Packet, 10)

			// Use raw integer 1 for QoS 1
			token := c.Subscribe(tt.topic, 1, handler, WithNoLocal(tt.noLocal))

			// If we expect an error, it should be in the token immediately because the validation failed
			// synchronously before sending to the channel.
			err := token.Error()

			// Wait is not needed if the error is immediate, but let's check properly.
			// If validation passes, the token is technically "in progress" until the mock loop handles it.
			// But we only care if it *failed* validation.

			if tt.wantError {
				if err == nil {
					// Try waiting briefly just in case (though validation is sync)
					ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
					defer cancel()
					err = token.Wait(ctx)
				}

				if err == nil {
					t.Errorf("expected error for %s, got nil", tt.name)
				} else if !strings.Contains(err.Error(), "protocol error") {
					t.Errorf("expected protocol error, got: %v", err)
				}
			} else {
				// If we don't expect an error, the validation should have passed.
				// The token might still be pending (since no loop processing), but verify
				// we didn't get the VALIDATION error.
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}
