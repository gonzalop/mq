package mq

import (
	"context"
	"errors"
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
