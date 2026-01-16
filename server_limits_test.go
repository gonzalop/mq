package mq

import (
	"strings"
	"testing"

	"github.com/gonzalop/mq/internal/packets"
)

func TestMaximumPacketSizeEnforcement(t *testing.T) {
	tests := []struct {
		name          string
		maxPacketSize uint32
		payloadSize   int
		wantError     bool
	}{
		{
			name:          "no limit set",
			maxPacketSize: 0,
			payloadSize:   10000,
			wantError:     false,
		},
		{
			name:          "under limit",
			maxPacketSize: 1024,
			payloadSize:   100,
			wantError:     false,
		},
		{
			name:          "at limit",
			maxPacketSize: 200,
			payloadSize:   150, // With headers, should be close to 200
			wantError:     false,
		},
		{
			name:          "exceeds limit",
			maxPacketSize: 100,
			payloadSize:   200, // With headers, will exceed 100
			wantError:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &Client{
				opts: &clientOptions{
					ProtocolVersion: ProtocolV50,
					Logger:          testLogger(),
				},
				serverCaps: serverCapabilities{
					MaximumPacketSize: tt.maxPacketSize,
				},
				pending:  make(map[uint16]*pendingOp),
				outgoing: make(chan packets.Packet, 10),
			}

			token := &token{done: make(chan struct{})}
			req := &publishRequest{
				packet: &packets.PublishPacket{
					Topic:   "test/topic",
					Payload: []byte(strings.Repeat("x", tt.payloadSize)),
					QoS:     0,
				},
				token: token,
			}

			// Process request
			c.internalPublish(req)

			// Check result
			select {
			case <-token.done:
				err := token.Error()
				if tt.wantError && err == nil {
					t.Error("expected error, got nil")
				}
				if !tt.wantError && err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if tt.wantError && err != nil && !strings.Contains(err.Error(), "exceeds server maximum") {
					t.Errorf("expected packet size error, got: %v", err)
				}
			default:
				if tt.wantError {
					t.Error("expected immediate error, token not completed")
				}
			}
		})
	}
}

func TestReceiveMaximumEnforcement(t *testing.T) {
	tests := []struct {
		name           string
		receiveMaximum uint16
		inFlightCount  int
		qos            uint8
		wantError      bool
		wantQueueLen   int
	}{
		{
			name:           "no limit set",
			receiveMaximum: 0,
			inFlightCount:  100,
			qos:            1,
			wantError:      false,
			wantQueueLen:   0,
		},
		{
			name:           "under limit",
			receiveMaximum: 10,
			inFlightCount:  5,
			qos:            1,
			wantError:      false,
			wantQueueLen:   0,
		},
		{
			name:           "at limit minus one",
			receiveMaximum: 10,
			inFlightCount:  9,
			qos:            1,
			wantError:      false,
			wantQueueLen:   0,
		},
		{
			name:           "at limit (should queue)",
			receiveMaximum: 10,
			inFlightCount:  10,
			qos:            1,
			wantError:      false,
			wantQueueLen:   1,
		},
		{
			name:           "exceeds limit (should queue)",
			receiveMaximum: 10,
			inFlightCount:  15,
			qos:            1,
			wantError:      false,
			wantQueueLen:   1,
		},
		{
			name:           "qos 0 ignores limit",
			receiveMaximum: 10,
			inFlightCount:  15,
			qos:            0,
			wantError:      false,
			wantQueueLen:   0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &Client{
				opts: &clientOptions{
					ProtocolVersion: ProtocolV50,
					Logger:          testLogger(),
				},
				serverCaps: serverCapabilities{
					ReceiveMaximum: tt.receiveMaximum,
					MaximumQoS:     2, // Default to allowing everything
				},
				pending:  make(map[uint16]*pendingOp),
				outgoing: make(chan packets.Packet, 100),
			}

			// Add in-flight messages to pending
			for i := 0; i < tt.inFlightCount; i++ {
				c.pending[uint16(i+1)] = &pendingOp{
					packet: &packets.PublishPacket{
						Topic: "test",
						QoS:   1,
					},
					qos: 1,
				}
				c.inFlightCount++
			}

			token := &token{done: make(chan struct{})}
			req := &publishRequest{
				packet: &packets.PublishPacket{
					Topic:   "test/topic",
					Payload: []byte("test"),
					QoS:     tt.qos,
				},
				token: token,
			}

			// Process request
			c.internalPublish(req)

			// Check result
			select {
			case <-token.done:
				err := token.Error()
				if tt.wantError && err == nil {
					t.Error("expected error, got nil")
				}
				if !tt.wantError && err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			default:
				if tt.wantError {
					t.Error("expected immediate error, token not completed")
				}
			}

			// Verify queue length
			if len(c.publishQueue) != tt.wantQueueLen {
				t.Errorf("publishQueue length = %d, want %d", len(c.publishQueue), tt.wantQueueLen)
			}
		})
	}
}
