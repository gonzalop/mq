package packets

import (
	"bytes"
	"io"
	"testing"
)

// genericWriter is a simple io.Writer that does NOT implement io.ByteWriter.
// This forces the fallback path in FixedHeader.WriteTo.
type genericWriter struct {
	w io.Writer
}

func (g *genericWriter) Write(p []byte) (n int, err error) {
	return g.w.Write(p)
}

func TestFixedHeader_WriteTo_Fallback(t *testing.T) {
	tests := []struct {
		name   string
		header FixedHeader
	}{
		{
			name: "Connect Header",
			header: FixedHeader{
				PacketType:      CONNECT,
				Flags:           0,
				RemainingLength: 10,
			},
		},
		{
			name: "Large Payload Header",
			header: FixedHeader{
				PacketType:      PUBLISH,
				Flags:           0x02,          // QoS 1
				RemainingLength: 128 * 128 * 2, // Large enough to use multiple bytes for varint
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			gw := &genericWriter{w: &buf}

			// Write using the fallback path
			n, err := tt.header.WriteTo(gw)
			if err != nil {
				t.Fatalf("WriteTo() error = %v", err)
			}

			// Verify correct number of bytes returned
			expectedBytes := 1 + encodedVarIntLen(tt.header.RemainingLength)
			if int(n) != expectedBytes {
				t.Errorf("WriteTo() returned %d bytes, want %d", n, expectedBytes)
			}

			// Verify content against the optimized path (which writes to bytes.Buffer directly)
			var expectedBuf bytes.Buffer
			_, _ = tt.header.WriteTo(&expectedBuf)

			if !bytes.Equal(buf.Bytes(), expectedBuf.Bytes()) {
				t.Errorf("Written bytes mismatch:\ngot  %x\nwant %x", buf.Bytes(), expectedBuf.Bytes())
			}
		})
	}
}

func encodedVarIntLen(x int) int {
	if x == 0 {
		return 1
	}
	count := 0
	for x > 0 {
		x /= 128
		count++
	}
	return count
}

func TestPacketWriteTo_NonByteWriterFallback(t *testing.T) {
	props := &Properties{ContentType: "text/plain", Presence: PresContentType}
	pkts := []Packet{
		&PubackPacket{PacketID: 1, ReasonCode: 0x10, Properties: props, Version: 5},
		&PubrecPacket{PacketID: 2, ReasonCode: 0x00, Properties: props, Version: 5},
		&PubrelPacket{PacketID: 3, ReasonCode: 0x92, Properties: props, Version: 5},
		&PubcompPacket{PacketID: 4, Version: 4},
		&ConnectPacket{ProtocolName: "MQTT", ProtocolLevel: 5, CleanSession: true, KeepAlive: 60, ClientID: "c1"},
		&ConnackPacket{SessionPresent: true, ReturnCode: 0, Properties: props},
		&SubscribePacket{PacketID: 5, Topics: []string{"a/b"}, QoS: []uint8{1}, NoLocal: []bool{true}, Version: 5},
		&DisconnectPacket{ReasonCode: 0x04, Properties: props, Version: 5},
		&AuthPacket{ReasonCode: AuthReasonContinue, Properties: props, Version: 5},
	}

	for _, pkt := range pkts {
		var want, got bytes.Buffer
		wn, err := pkt.WriteTo(&want)
		if err != nil {
			t.Fatalf("%T WriteTo(bytes.Buffer): %v", pkt, err)
		}
		gn, err := pkt.WriteTo(&genericWriter{w: &got})
		if err != nil {
			t.Fatalf("%T WriteTo(genericWriter): %v", pkt, err)
		}
		if gn != wn {
			t.Errorf("%T written = %d, want %d", pkt, gn, wn)
		}
		if !bytes.Equal(got.Bytes(), want.Bytes()) {
			t.Errorf("%T bytes mismatch:\ngot  %x\nwant %x", pkt, got.Bytes(), want.Bytes())
		}

		ver := uint8(5)
		if pa, ok := pkt.(*PubcompPacket); ok {
			ver = pa.Version
		}
		decoded, err := ReadPacket(bytes.NewReader(got.Bytes()), ver, 0)
		if err != nil {
			t.Fatalf("%T ReadPacket round-trip: %v", pkt, err)
		}
		if decoded.Type() != pkt.Type() {
			t.Errorf("%T decoded type = %d, want %d", pkt, decoded.Type(), pkt.Type())
		}
	}
}
