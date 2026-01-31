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
