package mq

import (
	"context"
	"testing"
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
