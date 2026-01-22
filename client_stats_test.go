package mq

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/gonzalop/mq/internal/packets"
)

func TestGetStats(t *testing.T) {
	// Start a real TCP listener
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to listen: %v", err)
	}
	defer l.Close()

	// Channel to signal server done
	serverDone := make(chan struct{})

	// Mock server
	go func() {
		defer close(serverDone)
		conn, err := l.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		// Read CONNECT
		buf := make([]byte, 1024)
		n, err := conn.Read(buf)
		if err != nil || n < 2 {
			return
		}

		// Send CONNACK
		connack := &packets.ConnackPacket{
			ReturnCode: packets.ConnAccepted,
		}
		if _, err := connack.WriteTo(conn); err != nil {
			return
		}

		// Read loop
		for {
			if _, err := conn.Read(buf); err != nil {
				return
			}
		}
	}()

	// Connect client
	opts := []Option{
		WithClientID("test-stats-client"),
		WithKeepAlive(time.Second),
	}
	client, err := Dial("tcp://"+l.Addr().String(), opts...)
	if err != nil {
		t.Fatalf("Failed to dial: %v", err)
	}
	defer func() { _ = client.Disconnect(context.Background()) }()

	// Initial stats check
	stats := client.GetStats()
	if !stats.Connected {
		t.Error("Stats.Connected should be true")
	}
	if stats.PacketsSent < 1 {
		t.Errorf("Stats.PacketsSent should be >= 1, got %d", stats.PacketsSent)
	}
	if stats.PacketsReceived < 1 {
		t.Errorf("Stats.PacketsReceived should be >= 1, got %d", stats.PacketsReceived)
	}

	// Send a publish to increment stats
	client.Publish("test/stats", []byte("payload"), WithQoS(AtLeastOnce))

	// Give time for IO
	time.Sleep(100 * time.Millisecond)

	newStats := client.GetStats()
	if newStats.PacketsSent <= stats.PacketsSent {
		t.Errorf("PacketsSent did not increase: %d -> %d", stats.PacketsSent, newStats.PacketsSent)
	}
	if newStats.BytesSent <= stats.BytesSent {
		t.Errorf("BytesSent did not increase: %d -> %d", stats.BytesSent, newStats.BytesSent)
	}
}
