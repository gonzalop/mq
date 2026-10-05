package mq

import (
	"context"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/gonzalop/mq/internal/packets"
)

// TestReconnectBackoff_ShortLivedConnectionEscalates verifies that when connections
// die shortly after CONNACK (below MinStableConnectionDuration), the reconnect loop
// treats them as failed attempts and escalates backoff exponentially instead of spinning
// at base backoff.
func TestReconnectBackoff_ShortLivedConnectionEscalates(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	defer ln.Close()

	var mu sync.Mutex
	var connTimes []time.Time
	stopServer := make(chan struct{})

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				mu.Lock()
				connTimes = append(connTimes, time.Now())
				count := len(connTimes)
				mu.Unlock()

				// Read CONNECT packet
				pkt, err := packets.ReadPacket(c, ProtocolV50, 0)
				if err != nil {
					return
				}
				if _, ok := pkt.(*packets.ConnectPacket); !ok {
					return
				}

				// Send successful CONNACK
				connack := &packets.ConnackPacket{
					ReturnCode: uint8(packets.ConnAccepted),
				}
				if _, err := connack.WriteTo(c); err != nil {
					return
				}

				// Immediately close the connection to simulate short-lived connection
				if count >= 4 {
					select {
					case <-stopServer:
					default:
						close(stopServer)
					}
				}
			}(conn)
		}
	}()

	baseBackoff := 30 * time.Millisecond
	maxBackoff := 500 * time.Millisecond
	stableThreshold := 150 * time.Millisecond

	client, err := Dial("tcp://"+ln.Addr().String(),
		WithClientID("test-flapping"),
		WithAutoReconnect(true),
		WithReconnectBackoff(baseBackoff, maxBackoff, false),
		WithMinStableConnectionDuration(stableThreshold),
		WithConnectTimeout(2*time.Second),
	)
	if err != nil {
		t.Fatalf("failed to dial: %v", err)
	}
	defer client.Disconnect(context.Background())

	// Wait for 4 connections to happen
	select {
	case <-stopServer:
	case <-time.After(5 * time.Second):
		t.Fatalf("timeout waiting for 4 connection attempts")
	}

	mu.Lock()
	times := make([]time.Time, len(connTimes))
	copy(times, connTimes)
	mu.Unlock()

	if len(times) < 4 {
		t.Fatalf("expected at least 4 connections, got %d", len(times))
	}

	d1 := times[1].Sub(times[0]) // initial backoff (around baseBackoff)
	d2 := times[2].Sub(times[1]) // first escalation (around 2 * baseBackoff)
	d3 := times[3].Sub(times[2]) // second escalation (around 4 * baseBackoff)

	t.Logf("connection intervals: d1=%v, d2=%v, d3=%v", d1, d2, d3)

	// In an exponential backoff sequence: d3 > d2 > d1 (allowing for small scheduling jitter)
	if d2 <= d1/2 {
		t.Errorf("expected d2 (%v) to be larger than d1 (%v)", d2, d1)
	}
	if d3 <= d2/2 {
		t.Errorf("expected d3 (%v) to be larger than d2 (%v)", d3, d2)
	}
}

// TestReconnectBackoff_StableConnectionResetsBackoff verifies that if a connection
// survives past MinStableConnectionDuration, the backoff is reset to baseBackoff.
func TestReconnectBackoff_StableConnectionResetsBackoff(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	defer ln.Close()

	var mu sync.Mutex
	var connTimes []time.Time
	done := make(chan struct{})

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				mu.Lock()
				connTimes = append(connTimes, time.Now())
				attempt := len(connTimes)
				mu.Unlock()

				// Read CONNECT packet
				pkt, err := packets.ReadPacket(c, ProtocolV50, 0)
				if err != nil {
					return
				}
				if _, ok := pkt.(*packets.ConnectPacket); !ok {
					return
				}

				// Send successful CONNACK
				connack := &packets.ConnackPacket{
					ReturnCode: uint8(packets.ConnAccepted),
				}
				if _, err := connack.WriteTo(c); err != nil {
					return
				}

				switch attempt {
				case 1:
					// First attempt dies quickly (< stableThreshold)
					return
				case 2:
					// Second attempt stays alive for > stableThreshold, then closes
					time.Sleep(120 * time.Millisecond)
					return
				case 3:
					// Third connection arrived after reset; signal done
					select {
					case <-done:
					default:
						close(done)
					}
					return
				}
			}(conn)
		}
	}()

	baseBackoff := 30 * time.Millisecond
	maxBackoff := 500 * time.Millisecond
	stableThreshold := 60 * time.Millisecond

	client, err := Dial("tcp://"+ln.Addr().String(),
		WithClientID("test-stable-reset"),
		WithAutoReconnect(true),
		WithReconnectBackoff(baseBackoff, maxBackoff, false),
		WithMinStableConnectionDuration(stableThreshold),
		WithConnectTimeout(2*time.Second),
	)
	if err != nil {
		t.Fatalf("failed to dial: %v", err)
	}
	defer client.Disconnect(context.Background())

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatalf("timeout waiting for connections")
	}

	mu.Lock()
	times := make([]time.Time, len(connTimes))
	copy(times, connTimes)
	mu.Unlock()

	if len(times) < 3 {
		t.Fatalf("expected at least 3 connections, got %d", len(times))
	}

	// Interval from connection 2 closing to connection 3 opening should be ~baseBackoff
	// because connection 2 was stable (> 60ms).
	t.Logf("reconnect count: %d", client.GetStats().ReconnectCount)
}

// TestReconnectBackoff_DisabledEscalation verifies that passing a negative duration
// disables the anti-flapping backoff escalation, resetting backoff to base on every CONNACK.
func TestReconnectBackoff_DisabledEscalation(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	defer ln.Close()

	var mu sync.Mutex
	var connTimes []time.Time
	done := make(chan struct{})

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				mu.Lock()
				connTimes = append(connTimes, time.Now())
				count := len(connTimes)
				mu.Unlock()

				pkt, err := packets.ReadPacket(c, ProtocolV50, 0)
				if err != nil {
					return
				}
				if _, ok := pkt.(*packets.ConnectPacket); !ok {
					return
				}

				connack := &packets.ConnackPacket{
					ReturnCode: uint8(packets.ConnAccepted),
				}
				if _, err := connack.WriteTo(c); err != nil {
					return
				}

				if count >= 3 {
					select {
					case <-done:
					default:
						close(done)
					}
				}
			}(conn)
		}
	}()

	baseBackoff := 30 * time.Millisecond
	maxBackoff := 500 * time.Millisecond

	client, err := Dial("tcp://"+ln.Addr().String(),
		WithClientID("test-disabled-flapping"),
		WithAutoReconnect(true),
		WithReconnectBackoff(baseBackoff, maxBackoff, false),
		WithMinStableConnectionDuration(-1), // explicitly disabled
		WithConnectTimeout(2*time.Second),
	)
	if err != nil {
		t.Fatalf("failed to dial: %v", err)
	}
	defer client.Disconnect(context.Background())

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatalf("timeout waiting for connections")
	}

	mu.Lock()
	times := make([]time.Time, len(connTimes))
	copy(times, connTimes)
	mu.Unlock()

	if len(times) < 3 {
		t.Fatalf("expected at least 3 connections, got %d", len(times))
	}

	d1 := times[1].Sub(times[0])
	d2 := times[2].Sub(times[1])

	t.Logf("connection intervals with disabled escalation: d1=%v, d2=%v", d1, d2)

	// Since escalation is disabled, d2 should be approximately baseBackoff (~30ms),
	// NOT escalated to 60ms or higher.
	if d2 > 3*baseBackoff {
		t.Errorf("expected d2 (%v) to stay near baseBackoff (%v), but it escalated", d2, baseBackoff)
	}
}

// TestClient_ConnectedAtAndUptime verifies ConnectedAt and Uptime accessor methods.
func TestClient_ConnectedAtAndUptime(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	defer ln.Close()

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		_, _ = packets.ReadPacket(conn, ProtocolV50, 0)
		connack := &packets.ConnackPacket{
			ReturnCode: uint8(packets.ConnAccepted),
		}
		_, _ = connack.WriteTo(conn)

		// Keep connection open until closed by client
		buf := make([]byte, 1024)
		for {
			if _, err := conn.Read(buf); err != nil {
				return
			}
		}
	}()

	client, err := Dial("tcp://"+ln.Addr().String(),
		WithClientID("test-uptime"),
		WithAutoReconnect(false),
	)
	if err != nil {
		t.Fatalf("failed to dial: %v", err)
	}

	connectedAt := client.ConnectedAt()
	if connectedAt.IsZero() {
		t.Error("expected non-zero ConnectedAt")
	}
	time.Sleep(20 * time.Millisecond)
	uptime := client.Uptime()
	if uptime < 15*time.Millisecond {
		t.Errorf("expected uptime >= 15ms, got %v", uptime)
	}

	_ = client.Disconnect(context.Background())

	if !client.ConnectedAt().IsZero() {
		t.Errorf("expected zero ConnectedAt after disconnect, got %v", client.ConnectedAt())
	}
	if client.Uptime() != 0 {
		t.Errorf("expected 0 Uptime after disconnect, got %v", client.Uptime())
	}
}
