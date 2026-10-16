package mq

import (
	"context"
	"errors"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gonzalop/mq/internal/packets"
)

// TestDisconnectWhenNotConnectedStopsLoops checks that Disconnect still stops
// the client when it is called while disconnected (for example during an
// automatic reconnect). Before the fix it returned early without closing stop,
// so the reconnect loop kept running and calls waiting on a token never
// completed.
func TestDisconnectWhenNotConnectedStopsLoops(t *testing.T) {
	c := &Client{
		stop: make(chan struct{}),
		opts: defaultOptions(""),
	}
	if c.connected.Load() {
		t.Fatal("precondition: the client starts disconnected")
	}

	if err := c.disconnectWithReason(context.Background(), uint8(ReasonCodeNormalDisconnect), nil, true); err != nil {
		t.Fatalf("Disconnect returned error: %v", err)
	}

	select {
	case <-c.stop:
		// stopped as expected
	default:
		t.Fatal("stop was not closed: the background loops would keep running")
	}

	// Idempotent: a second call must not panic on a double close.
	if err := c.disconnectWithReason(context.Background(), uint8(ReasonCodeNormalDisconnect), nil, true); err != nil {
		t.Fatalf("second Disconnect returned error: %v", err)
	}
}

// Regression test for Finding 11: a graceful Disconnect should not take a fixed 100ms+ when the
// DISCONNECT can be written immediately.
func TestDisconnect_NoFixedSleep(t *testing.T) {
	opts := defaultOptions("tcp://x")
	c := &Client{opts: opts, outgoing: make(chan packets.Packet, 10), stop: make(chan struct{})}
	c.connected.Store(true)

	start := time.Now()
	_ = c.disconnectWithReason(context.Background(), 0, nil, false)
	if d := time.Since(start); d >= 100*time.Millisecond {
		t.Errorf("disconnect took %v; it sleeps a fixed 100ms", d)
	}
}

// TestRequestsAfterDisconnect checks that Publish, Subscribe and Unsubscribe
// after Disconnect fail with ErrClientDisconnected instead of panicking on the
// session maps the logic loop cleared when it stopped.
func TestRequestsAfterDisconnect(t *testing.T) {
	c := newTestClient(nil)
	c.wg.Add(1)
	go c.logicLoop()
	if err := c.Disconnect(context.Background()); err != nil {
		t.Fatalf("Disconnect: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	toks := map[string]Token{
		"publish":     c.Publish(ctx, "a/b", []byte("x"), WithQoS(1)),
		"subscribe":   c.Subscribe(ctx, "a/b", 1, func(*Client, Message) {}),
		"unsubscribe": c.Unsubscribe(ctx, "a/b"),
	}
	for name, tok := range toks {
		if err := tok.Wait(ctx); !errors.Is(err, ErrClientDisconnected) {
			t.Errorf("%s after Disconnect: got %v, want ErrClientDisconnected", name, err)
		}
	}
}

// TestProtocolErrorReconnects checks that a protocol violation by the server
// drops only the connection: the client sends DISCONNECT 0x82 (v5.0), reports
// the loss through OnConnectionLost and reconnects. It used to call the
// user-facing Disconnect, which stopped the client for good without telling
// anyone.
func TestProtocolErrorReconnects(t *testing.T) {
	var dials atomic.Int32
	gotReason := make(chan uint8, 1)

	dialer := DialFunc(func(context.Context, string, string) (net.Conn, error) {
		n := dials.Add(1)
		cli, srv := net.Pipe()
		go func() {
			defer srv.Close()
			if _, err := packets.ReadPacket(srv, 5, 0); err != nil {
				return
			}
			connack := &packets.ConnackPacket{ReturnCode: packets.ConnAccepted, Properties: &packets.Properties{}}
			if _, err := connack.WriteTo(srv); err != nil {
				return
			}
			if n == 1 {
				// Reserved packet type 0: a protocol error.
				if _, err := srv.Write([]byte{0x00, 0x00}); err != nil {
					return
				}
			}
			for {
				pkt, err := packets.ReadPacket(srv, 5, 0)
				if err != nil {
					return
				}
				if d, ok := pkt.(*packets.DisconnectPacket); ok && n == 1 {
					gotReason <- d.ReasonCode
				}
			}
		}()
		return cli, nil
	})

	lost := make(chan error, 1)
	c, err := Dial("tcp://mock:1883",
		WithClientID("proto-err"),
		WithProtocolVersion(ProtocolV50),
		WithDialer(dialer),
		WithReconnectBackoff(10*time.Millisecond, 10*time.Millisecond, false),
		WithOnConnectionLost(func(_ *Client, err error) { lost <- err }),
	)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer c.Disconnect(context.Background())

	select {
	case rc := <-gotReason:
		if rc != uint8(ReasonCodeProtocolError) {
			t.Errorf("DISCONNECT reason = 0x%02x, want 0x82", rc)
		}
	case <-time.After(2 * time.Second):
		t.Error("server never received DISCONNECT 0x82")
	}

	select {
	case <-lost:
	case <-time.After(2 * time.Second):
		t.Fatal("OnConnectionLost was not called")
	}

	deadline := time.Now().Add(2 * time.Second)
	for dials.Load() < 2 || !c.IsConnected() {
		if time.Now().After(deadline) {
			t.Fatalf("client did not reconnect (dials=%d, connected=%v)", dials.Load(), c.IsConnected())
		}
		time.Sleep(10 * time.Millisecond)
	}
	if c.stopped() {
		t.Error("client was stopped by a protocol error")
	}
}
