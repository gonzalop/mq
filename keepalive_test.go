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
	keepalive := 60 * time.Millisecond
	client := &Client{trie: newTopicTrie(),
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
		client.runWriteLoop(client.conn, make(chan struct{}), client.opts.KeepAlive)
		close(done)
	}()

	// Wait for timeout (1.5x keepalive = 90ms, plus margin)
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
	keepalive := 60 * time.Millisecond
	client := &Client{trie: newTopicTrie(),
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

	// Consume writes on server side so PINGREQ doesn't block on net.Pipe
	go func() {
		buf := make([]byte, 1024)
		for {
			if _, err := serverConn.Read(buf); err != nil {
				return
			}
		}
	}()

	// Start writeLoop in background
	client.wg.Add(1)
	go func() {
		client.runWriteLoop(client.conn, make(chan struct{}), client.opts.KeepAlive)
		disconnected.Store(true)
	}()

	// Simulate receiving packets periodically (every 30ms)
	// This should prevent timeout (1.5x keepalive = 90ms)
	ticker := time.NewTicker(30 * time.Millisecond)
	defer ticker.Stop()

	for range 5 {
		<-ticker.C
		// Signal packet received
		select {
		case client.packetReceived <- struct{}{}:
		default:
		}
	}

	// After 150ms of periodic packets, client should still be connected
	if disconnected.Load() {
		t.Error("Client should not disconnect when receiving packets regularly")
	}

	// Clean up
	close(client.stop)
	_ = clientConn.Close()
	client.wg.Wait()
}

// TestKeepAlivePINGREQSent verifies that PINGREQ is sent when no activity.
func TestKeepAlivePINGREQSent(t *testing.T) {
	// Create a mock connection
	serverConn, clientConn := net.Pipe()
	defer serverConn.Close()
	defer clientConn.Close()

	// Create client with short keepalive
	keepalive := 60 * time.Millisecond
	client := &Client{trie: newTopicTrie(),
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
	go client.runWriteLoop(client.conn, make(chan struct{}), client.opts.KeepAlive)

	// Read from server side to capture PINGREQ
	pingSeen := make(chan struct{})
	go func() {
		buf := make([]byte, 2)
		for {
			n, err := serverConn.Read(buf)
			if err != nil {
				return
			}
			if n == 2 && buf[0] == 0xc0 && buf[1] == 0x00 {
				close(pingSeen)
				return
			}
		}
	}()

	// PINGREQ is due after keepalive/2 = 30ms of no outgoing traffic.
	select {
	case <-pingSeen:
	case <-time.After(2 * time.Second):
		t.Fatal("PINGREQ was not sent")
	}

	// Clean up
	close(client.stop)
	_ = clientConn.Close()
	client.wg.Wait()
}

// TestKeepAliveWriteDoesNotResetTimeout verifies that writing packets
// does NOT reset the receive timeout (only receiving packets should).
func TestKeepAliveWriteDoesNotResetTimeout(t *testing.T) {
	// Create a mock connection
	serverConn, clientConn := net.Pipe()
	defer serverConn.Close()
	defer clientConn.Close()

	// Create client with short keepalive
	keepalive := 60 * time.Millisecond
	client := &Client{trie: newTopicTrie(),
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
	done := make(chan struct{})

	// Start writeLoop in background
	client.wg.Add(1)
	go func() {
		client.runWriteLoop(client.conn, make(chan struct{}), client.opts.KeepAlive)
		close(done)
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
	ticker := time.NewTicker(15 * time.Millisecond)
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

	// Timeout is 1.5x keepalive = 90ms
	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
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
	keepalive := 120 * time.Millisecond
	client := &Client{trie: newTopicTrie(),
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
					// Signal packet received to client (in real usage readLoop does this on PINGRESP)
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
	go client.runWriteLoop(client.conn, make(chan struct{}), client.opts.KeepAlive)

	// Simulate continuous QoS 0 publishing (every 30ms)
	// This is faster than the PINGREQ threshold (3/4 * 120ms = 90ms)
	publishTicker := time.NewTicker(30 * time.Millisecond)
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
	// With keepalive=120ms, threshold=90ms, we should see PINGREQ
	// even though we're publishing every 30ms
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
	_ = clientConn.Close()
	client.wg.Wait()
}

// TestKeepAliveZeroDisabled verifies that keepalive=0 disables the mechanism.
func TestKeepAliveZeroDisabled(t *testing.T) {
	// Create a mock connection
	serverConn, clientConn := net.Pipe()
	defer serverConn.Close()
	defer clientConn.Close()

	// Create client with keepalive disabled
	client := &Client{trie: newTopicTrie(),
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
		client.runWriteLoop(client.conn, make(chan struct{}), client.opts.KeepAlive)
		disconnected.Store(true)
	}()

	// Wait a while - should NOT timeout
	time.Sleep(100 * time.Millisecond)

	if disconnected.Load() {
		t.Error("Client should not timeout when keepalive is disabled (0)")
	}

	// Clean up
	close(client.stop)
	client.wg.Wait()
}

// TestKeepAlivePingPendingResetOnReconnect verifies that pingPending is cleared
// and pingPendingCh is drained when resetting client state or starting writeLoop.
func TestKeepAlivePingPendingResetOnReconnect(t *testing.T) {
	client := &Client{
		pingPendingCh: make(chan struct{}, 1),
	}
	client.pingPending.Store(true)
	client.pingPendingCh <- struct{}{}

	client.internalResetState()

	if client.pingPending.Load() {
		t.Error("expected pingPending to be false after internalResetState")
	}
	select {
	case <-client.pingPendingCh:
		t.Error("expected pingPendingCh to be empty after internalResetState")
	default:
	}
}

// TestKeepAlive_SubMillisecondDoesNotPanic verifies that runWriteLoop does not panic
// when keepalive is smaller than 4 nanoseconds (preventing NewTicker(0)).
func TestKeepAlive_SubMillisecondDoesNotPanic(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer serverConn.Close()
	defer clientConn.Close()

	client := &Client{
		trie: newTopicTrie(),
		opts: &clientOptions{
			KeepAlive:       1 * time.Nanosecond,
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

	connDone := make(chan struct{})
	close(connDone) // Stop writeLoop immediately after startup

	client.wg.Add(1)
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("runWriteLoop panicked on sub-millisecond keepalive: %v", r)
		}
	}()

	client.runWriteLoop(client.conn, connDone, client.opts.KeepAlive)
}

// TestWriteLoop_WriteTimeoutDisconnects verifies that when socket writes stall and time out,
// runWriteLoop detects the timeout, triggers disconnect, and exits.
func TestWriteLoop_WriteTimeoutDisconnects(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer serverConn.Close()
	defer clientConn.Close()

	client := &Client{
		trie: newTopicTrie(),
		opts: &clientOptions{
			KeepAlive:       0,
			WriteTimeout:    50 * time.Millisecond,
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

	// Do NOT read from serverConn so that writes to the pipe block and trigger WriteTimeout
	done := make(chan struct{})
	client.wg.Add(1)
	go func() {
		client.runWriteLoop(client.conn, make(chan struct{}), 0)
		close(done)
	}()

	// Send a publish packet to trigger a write
	client.outgoing <- &packets.PublishPacket{Topic: "test", QoS: 0, Payload: []byte("hello")}

	select {
	case <-done:
		// Success: writeLoop exited after write timeout
		if client.connected.Load() {
			t.Error("client should be disconnected after write timeout")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("runWriteLoop hung and did not respect WriteTimeout")
	}
}
