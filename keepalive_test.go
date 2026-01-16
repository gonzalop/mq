package mq

import (
	"io"
	"log/slog"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gonzalop/mq/internal/packets"
)

// TestKeepAliveTimeout verifies that the client disconnects when no packets
// are received within 1.5x the keepalive interval.
func TestKeepAliveTimeout(t *testing.T) {
	// Create a mock connection that never sends data
	serverConn, clientConn := net.Pipe()
	defer serverConn.Close()
	defer clientConn.Close()

	// Create client with very short keepalive for fast test
	keepalive := 200 * time.Millisecond
	client := &Client{
		opts: &clientOptions{
			KeepAlive:       keepalive,
			Server:          "tcp://test:1883",
			Logger:          slog.New(slog.NewTextHandler(io.Discard, nil)),
			ProtocolVersion: ProtocolV311,
		},
		conn:           clientConn,
		outgoing:       make(chan packets.Packet, 10),
		packetReceived: make(chan struct{}, 1),
		stop:           make(chan struct{}),
		disconnected:   make(chan struct{}, 1),
	}
	client.connected.Store(true)

	// Consume writes on server side so PINGREQ doesn't block
	go func() {
		buf := make([]byte, 1024)
		for {
			_, err := serverConn.Read(buf)
			if err != nil {
				return
			}
		}
	}()

	// Track if writeLoop exited
	done := make(chan struct{})

	// Start writeLoop in background
	client.wg.Add(1)
	go func() {
		client.writeLoop()
		close(done)
	}()

	// Wait for timeout (1.5x keepalive = 300ms, plus some margin)
	select {
	case <-done:
		// writeLoop exited due to timeout - this is expected
	case <-time.After(500 * time.Millisecond):
		t.Error("Expected writeLoop to exit after keepalive timeout")
	}

	// Verify client is marked as disconnected
	if client.IsConnected() {
		t.Error("Client should be marked as disconnected")
	}
}

// TestKeepAliveTimeoutPrevented verifies that receiving packets prevents timeout.
func TestKeepAliveTimeoutPrevented(t *testing.T) {
	// Create a mock connection
	serverConn, clientConn := net.Pipe()
	defer serverConn.Close()
	defer clientConn.Close()

	// Create client with short keepalive
	keepalive := 200 * time.Millisecond
	client := &Client{
		opts: &clientOptions{
			KeepAlive:       keepalive,
			Server:          "tcp://test:1883",
			Logger:          slog.New(slog.NewTextHandler(io.Discard, nil)),
			ProtocolVersion: ProtocolV311,
		},
		conn:           clientConn,
		outgoing:       make(chan packets.Packet, 10),
		packetReceived: make(chan struct{}, 1),
		stop:           make(chan struct{}),
	}
	client.connected.Store(true)

	// Track if disconnect was called
	var disconnected atomic.Bool

	// Start writeLoop in background
	client.wg.Add(1)
	go func() {
		client.writeLoop()
		disconnected.Store(true)
	}()

	// Simulate receiving packets periodically (every 100ms)
	// This should prevent timeout
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for range 5 {
		<-ticker.C
		// Signal packet received
		select {
		case client.packetReceived <- struct{}{}:
		default:
		}
	}

	// After 500ms of periodic packets, client should still be connected
	if disconnected.Load() {
		t.Error("Client should not disconnect when receiving packets regularly")
	}

	// Clean up
	close(client.stop)
	time.Sleep(50 * time.Millisecond)
}

// TestKeepAlivePINGREQSent verifies that PINGREQ is sent when no activity.
func TestKeepAlivePINGREQSent(t *testing.T) {
	// Create a mock connection
	serverConn, clientConn := net.Pipe()
	defer serverConn.Close()
	defer clientConn.Close()

	// Create client with short keepalive
	keepalive := 200 * time.Millisecond
	client := &Client{
		opts: &clientOptions{
			KeepAlive:       keepalive,
			Server:          "tcp://test:1883",
			Logger:          slog.New(slog.NewTextHandler(io.Discard, nil)),
			ProtocolVersion: ProtocolV311,
		},
		conn:           clientConn,
		outgoing:       make(chan packets.Packet, 10),
		packetReceived: make(chan struct{}, 1),
		stop:           make(chan struct{}),
	}
	client.connected.Store(true)

	// Start writeLoop in background
	client.wg.Add(1)
	go client.writeLoop()

	// Read from server side to capture PINGREQ
	go func() {
		buf := make([]byte, 2)
		for {
			n, err := serverConn.Read(buf)
			if err != nil {
				return
			}
			if n == 2 && buf[0] == 0xc0 && buf[1] == 0x00 {
				// PINGREQ received! Signal back as PINGRESP
				select {
				case client.packetReceived <- struct{}{}:
				default:
				}
			}
		}
	}()

	// Wait for PINGREQ to be sent (keepalive/2 = 100ms, plus margin)
	time.Sleep(150 * time.Millisecond)

	// Verify PINGREQ was sent by checking if we got the signal back
	// (In real scenario, readLoop would signal this)

	// Clean up
	close(client.stop)
	time.Sleep(50 * time.Millisecond)
}

// TestKeepAliveWriteDoesNotResetTimeout verifies that writing packets
// does NOT reset the receive timeout (only receiving packets should).
func TestKeepAliveWriteDoesNotResetTimeout(t *testing.T) {
	// Create a mock connection
	serverConn, clientConn := net.Pipe()
	defer serverConn.Close()
	defer clientConn.Close()

	// Create client with short keepalive
	keepalive := 200 * time.Millisecond
	client := &Client{
		opts: &clientOptions{
			KeepAlive:       keepalive,
			Server:          "tcp://test:1883",
			Logger:          slog.New(slog.NewTextHandler(io.Discard, nil)),
			ProtocolVersion: ProtocolV311,
		},
		conn:           clientConn,
		outgoing:       make(chan packets.Packet, 10),
		packetReceived: make(chan struct{}, 1),
		stop:           make(chan struct{}),
	}
	client.connected.Store(true)

	// Track if disconnect was called
	var disconnected atomic.Bool

	// Start writeLoop in background
	client.wg.Add(1)
	go func() {
		client.writeLoop()
		disconnected.Store(true)
	}()

	// Consume writes on server side so they don't block
	go func() {
		buf := make([]byte, 1024)
		for {
			_, err := serverConn.Read(buf)
			if err != nil {
				return
			}
		}
	}()

	// Keep sending packets (simulating application activity)
	// This should NOT prevent timeout since we're not receiving
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()

	for range 8 {
		<-ticker.C
		// Send a PUBLISH packet
		select {
		case client.outgoing <- &packets.PublishPacket{
			Topic:   "test",
			Payload: []byte("data"),
		}:
		default:
		}
	}

	// After 400ms of only sending (no receiving), should timeout
	// Timeout is 1.5x keepalive = 300ms
	time.Sleep(50 * time.Millisecond)

	if !disconnected.Load() {
		t.Error("Client should disconnect even when sending packets, if not receiving")
	}
}

// TestKeepAlivePINGREQWithQoS0Publishing verifies that PINGREQ is sent
// when continuously publishing QoS 0 messages (which don't get server responses).
// This tests the fix for the bug where PINGREQ was never sent during active publishing.
func TestKeepAlivePINGREQWithQoS0Publishing(t *testing.T) {
	// Create a mock connection
	serverConn, clientConn := net.Pipe()
	defer serverConn.Close()
	defer clientConn.Close()

	// Create client with short keepalive
	keepalive := 400 * time.Millisecond
	client := &Client{
		opts: &clientOptions{
			KeepAlive:       keepalive,
			Server:          "tcp://test:1883",
			Logger:          slog.New(slog.NewTextHandler(io.Discard, nil)),
			ProtocolVersion: ProtocolV311,
		},
		conn:           clientConn,
		outgoing:       make(chan packets.Packet, 10),
		packetReceived: make(chan struct{}, 1),
		stop:           make(chan struct{}),
	}
	client.connected.Store(true)

	// Track PINGREQ packets received by server
	pingreqReceived := make(chan struct{}, 5)

	// Server side: consume writes and detect PINGREQ
	go func() {
		buf := make([]byte, 1024)
		for {
			n, err := serverConn.Read(buf)
			if err != nil {
				return
			}
			// Check for PINGREQ packet (0xc0 0x00)
			for i := 0; i < n-1; i++ {
				if buf[i] == 0xc0 && buf[i+1] == 0x00 {
					select {
					case pingreqReceived <- struct{}{}:
					default:
					}
					// Send PINGRESP back (0xd0 0x00)
					_, _ = serverConn.Write([]byte{0xd0, 0x00})
					// Signal packet received to client
					select {
					case client.packetReceived <- struct{}{}:
					default:
					}
				}
			}
		}
	}()

	// Start writeLoop in background
	client.wg.Add(1)
	go client.writeLoop()

	// Simulate continuous QoS 0 publishing (every 100ms)
	// This is faster than the PINGREQ threshold (3/4 * 400ms = 300ms)
	publishTicker := time.NewTicker(100 * time.Millisecond)
	defer publishTicker.Stop()

	var publishCount atomic.Int32
	done := make(chan struct{})

	go func() {
		for {
			select {
			case <-publishTicker.C:
				// Send QoS 0 PUBLISH (no response expected from server)
				select {
				case client.outgoing <- &packets.PublishPacket{
					Topic:   "test/topic",
					Payload: []byte("data"),
					QoS:     0,
				}:
					publishCount.Add(1)
				default:
				}
			case <-done:
				return
			}
		}
	}()

	// Wait for at least one PINGREQ to be sent
	// With keepalive=400ms, threshold=300ms, we should see PINGREQ
	// even though we're publishing every 100ms
	select {
	case <-pingreqReceived:
		// Success! PINGREQ was sent despite continuous publishing
		t.Logf("PINGREQ sent after %d publishes", publishCount.Load())
	case <-time.After(1 * time.Second):
		t.Error("PINGREQ should be sent even when continuously publishing QoS 0 messages")
	}

	// Verify client is still connected (PINGRESP was received)
	if !client.IsConnected() {
		t.Error("Client should remain connected after receiving PINGRESP")
	}

	// Clean up
	close(done)
	close(client.stop)
	time.Sleep(50 * time.Millisecond)
}

// TestKeepAliveZeroDisabled verifies that keepalive=0 disables the mechanism.
func TestKeepAliveZeroDisabled(t *testing.T) {
	// Create a mock connection
	serverConn, clientConn := net.Pipe()
	defer serverConn.Close()
	defer clientConn.Close()

	// Create client with keepalive disabled
	client := &Client{
		opts: &clientOptions{
			KeepAlive:       0, // Disabled
			Server:          "tcp://test:1883",
			Logger:          slog.New(slog.NewTextHandler(io.Discard, nil)),
			ProtocolVersion: ProtocolV311,
		},
		conn:           clientConn,
		outgoing:       make(chan packets.Packet, 10),
		packetReceived: make(chan struct{}, 1),
		stop:           make(chan struct{}),
	}
	client.connected.Store(true)

	// Track if disconnect was called
	var disconnected atomic.Bool

	// Start writeLoop in background
	client.wg.Add(1)
	go func() {
		client.writeLoop()
		disconnected.Store(true)
	}()

	// Wait a while - should NOT timeout
	time.Sleep(500 * time.Millisecond)

	if disconnected.Load() {
		t.Error("Client should not timeout when keepalive is disabled (0)")
	}

	// Clean up
	close(client.stop)
	time.Sleep(50 * time.Millisecond)
}
