package mq

import (
	"testing"
	"time"

	"github.com/gonzalop/mq/internal/packets"
)

func TestAsyncCallbacks(t *testing.T) {
	// specific test to ensure user callbacks don't block logicLoop
	c := &Client{
		opts: &clientOptions{
			Logger:          testLogger(),
			ProtocolVersion: ProtocolV50,
		},
		subscriptions: make(map[string]subscriptionEntry),
		outgoing:      make(chan packets.Packet, 10),
		pending:       make(map[uint16]*pendingOp),
		stop:          make(chan struct{}),
	}

	callbackStart := make(chan struct{})
	callbackDone := make(chan struct{})

	// Subscribe with a slow callback
	handler := func(client *Client, msg Message) {
		close(callbackStart)
		time.Sleep(100 * time.Millisecond) // Simulate slow work
		close(callbackDone)
	}

	c.subscriptions["test/topic"] = subscriptionEntry{
		handler: handler,
		qos:     0,
	}

	// Simulate incoming PUBLISH
	pkt := &packets.PublishPacket{
		Topic:   "test/topic",
		Payload: []byte("payload"),
		QoS:     0,
	}

	// We need to call handleIncoming directly or mock the channels?
	// handleIncoming is private. logicLoop is private.
	// But we can test handleIncoming if we are in package mq.

	start := time.Now()

	// This should return immediately because callback is goroutine'd
	c.handleIncoming(pkt)

	duration := time.Since(start)

	if duration > 50*time.Millisecond {
		t.Errorf("handleIncoming took too long (%v), callback blocked?", duration)
	}

	select {
	case <-callbackStart:
		// good
	case <-time.After(50 * time.Millisecond):
		t.Error("Callback wasn't started")
	}

	select {
	case <-callbackDone:
		// good
	case <-time.After(200 * time.Millisecond):
		t.Error("Callback didn't finish")
	}
}
