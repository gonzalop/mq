package mq

import (
	"testing"

	"github.com/gonzalop/mq/internal/packets"
)

func TestConnectPacketLimits(t *testing.T) {
	tests := []struct {
		name                 string
		opts                 []Option
		wantReceiveMax       uint16
		wantMaxPacketSize    uint32
		wantReceiveMaxSet    bool
		wantMaxPacketSizeSet bool
	}{
		{
			name:                 "Defaults",
			opts:                 []Option{WithProtocolVersion(ProtocolV50)},
			wantReceiveMaxSet:    false,
			wantMaxPacketSizeSet: false,
		},
		{
			name: "With Receive Maximum",
			opts: []Option{
				WithProtocolVersion(ProtocolV50),
				WithReceiveMaximum(100),
			},
			wantReceiveMax:       100,
			wantReceiveMaxSet:    true,
			wantMaxPacketSizeSet: false,
		},
		{
			name: "With Max Packet Size",
			opts: []Option{
				WithProtocolVersion(ProtocolV50),
				WithMaxIncomingPacket(2048),
			},
			wantMaxPacketSize:    2048,
			wantReceiveMaxSet:    false,
			wantMaxPacketSizeSet: true,
		},
		{
			name: "With Both",
			opts: []Option{
				WithProtocolVersion(ProtocolV50),
				WithReceiveMaximum(50),
				WithMaxIncomingPacket(4096),
			},
			wantReceiveMax:       50,
			wantMaxPacketSize:    4096,
			wantReceiveMaxSet:    true,
			wantMaxPacketSizeSet: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup client options
			options := defaultOptions("tcp://localhost:1883")
			for _, opt := range tt.opts {
				opt(options)
			}

			c := &Client{
				opts:               options,
				requestedKeepAlive: options.KeepAlive,
			}

			pkt := c.buildConnectPacket()

			if pkt.Properties == nil {
				if tt.wantReceiveMaxSet || tt.wantMaxPacketSizeSet {
					t.Fatal("Properties is nil but expected properties to be set")
				}
				return
			}

			// Check Receive Maximum
			hasReceiveMax := pkt.Properties.Presence&packets.PresReceiveMaximum != 0
			if hasReceiveMax != tt.wantReceiveMaxSet {
				t.Errorf("ReceiveMaximum presence = %v, want %v", hasReceiveMax, tt.wantReceiveMaxSet)
			}
			if hasReceiveMax && pkt.Properties.ReceiveMaximum != tt.wantReceiveMax {
				t.Errorf("ReceiveMaximum = %d, want %d", pkt.Properties.ReceiveMaximum, tt.wantReceiveMax)
			}

			// Check Maximum Packet Size
			hasMaxPacketSize := pkt.Properties.Presence&packets.PresMaximumPacketSize != 0
			if hasMaxPacketSize != tt.wantMaxPacketSizeSet {
				t.Errorf("MaximumPacketSize presence = %v, want %v", hasMaxPacketSize, tt.wantMaxPacketSizeSet)
			}
			if hasMaxPacketSize && pkt.Properties.MaximumPacketSize != tt.wantMaxPacketSize {
				t.Errorf("MaximumPacketSize = %d, want %d", pkt.Properties.MaximumPacketSize, tt.wantMaxPacketSize)
			}
		})
	}
}
