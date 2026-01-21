package mq

import (
	"bytes"
	"testing"

	"github.com/gonzalop/mq/internal/packets"
)

// BenchmarkDecoding measures the cost of reading/decoding packets.
func BenchmarkDecoding_Publish_Small(b *testing.B) {
	pkt := &packets.PublishPacket{
		Topic:    "sensors/temperature",
		Payload:  []byte("25.5"),
		QoS:      1,
		PacketID: 10,
	}
	encoded := encodeToBytes(pkt)
	r := bytes.NewReader(encoded)

	for b.Loop() {
		r.Reset(encoded)
		_, err := packets.ReadPacket(r, 4, 0)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkDecoding_Publish_Large(b *testing.B) {
	payload := make([]byte, 2048) // 2KB, fits in 4KB pool
	pkt := &packets.PublishPacket{
		Topic:    "data/large",
		Payload:  payload,
		QoS:      1,
		PacketID: 10,
	}
	encoded := encodeToBytes(pkt)
	r := bytes.NewReader(encoded)

	for b.Loop() {
		r.Reset(encoded)
		_, err := packets.ReadPacket(r, 4, 0)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkClientThroughput measures end-to-end processing (mocked network).
// We use a pipe to clear the outgoing channel.
func BenchmarkClient_Publish_Throughput(b *testing.B) {
	// Setup client with mock stop channel and outgoing buffer
	c := &Client{
		opts:         defaultOptions("tcp://test:1883"),
		outgoing:     make(chan packets.Packet, 1000), // Larger buffer for bench
		stop:         make(chan struct{}),
		nextPacketID: 1,
		pending:      make(map[uint16]*pendingOp),
	}

	// Start a goroutine to drain outgoing (mock network write)
	go func() {
		for {
			select {
			case <-c.outgoing:
			case <-c.stop:
				return
			}
		}
	}()

	// Start logicLoop (needed to process publishReq)
	c.wg.Add(1)
	go c.logicLoop()

	payload := []byte("payload")

	for b.Loop() {
		// We call Publish but don't wait for token (QoS 0 fire and forget)
		c.Publish("bench/topic", payload, WithQoS(AtMostOnce))
	}

	// Cleanup
	close(c.stop)
	c.wg.Wait()
}

func encodeToBytes(pkt packets.Packet) []byte {
	var buf bytes.Buffer
	if _, err := pkt.WriteTo(&buf); err != nil {
		panic(err)
	}
	return buf.Bytes()
}
