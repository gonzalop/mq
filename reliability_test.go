package mq

import (
	"testing"
	"time"

	"github.com/gonzalop/mq/internal/packets"
)

// TestDeferredAcknowledgment verifies that acknowledgments are only sent
// after all handlers have completed.
func TestDeferredAcknowledgment(t *testing.T) {
	opts := defaultOptions("tcp://localhost:1883")
	c := newTestClient(opts)

	c.wg.Add(1)
	go c.logicLoop()
	defer func() {
		close(c.stop)
		c.wg.Wait()
	}()

	// 1. Setup a slow handler
	handlerStarted := make(chan struct{})
	handlerRelease := make(chan struct{})
	handlerDone := make(chan struct{})

	h := func(_ *Client, _ Message) {
		close(handlerStarted)
		<-handlerRelease
		close(handlerDone)
	}

	c.subscriptions["topic/1"] = subscriptionEntry{handler: h}

	// 2. Receive a QoS 1 message
	c.incoming <- &packets.PublishPacket{
		Topic:    "topic/1",
		QoS:      1,
		PacketID: 1234,
		Payload:  []byte("qos1"),
	}

	// 3. Wait for handler to start
	select {
	case <-handlerStarted:
		// Handler is running but not yet done
	case <-time.After(200 * time.Millisecond):
		t.Fatal("Handler never started")
	}

	// 4. Verify that NO PUBACK has been sent yet
	select {
	case pkt := <-c.outgoing:
		if _, ok := pkt.(*packets.PubackPacket); ok {
			t.Fatal("PUBACK sent before handler completed")
		}
	case <-time.After(50 * time.Millisecond):
		// Success: no PUBACK received
	}

	// 5. Release handler and verify PUBACK is sent
	close(handlerRelease)

	select {
	case <-handlerDone:
		// Handler finished
	case <-time.After(200 * time.Millisecond):
		t.Fatal("Handler never finished")
	}

	select {
	case pkt := <-c.outgoing:
		if p, ok := pkt.(*packets.PubackPacket); ok {
			if p.PacketID != 1234 {
				t.Errorf("Expected PUBACK for 1234, got %d", p.PacketID)
			}
		} else {
			t.Errorf("Expected PUBACK, got %T", pkt)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("PUBACK never sent after handler completed")
	}
}

// TestOrderedRetransmission verifies that pending packets are retransmitted
// in the same order they were originally sent.
func TestOrderedRetransmission(t *testing.T) {
	opts := defaultOptions("tcp://localhost:1883")
	c := newTestClient(opts)

	// 1. Manually populate pending packets in a specific order
	// We'll use very old timestamps to trigger immediate retransmission
	oldTime := time.Now().Add(-1 * time.Hour)

	packetIDs := []uint16{10, 5, 20, 1}
	for _, id := range packetIDs {
		c.sessionLock.Lock()
		c.pending[id] = &pendingOp{
			packet:    &packets.PublishPacket{PacketID: id, QoS: 1, Topic: "test"},
			timestamp: oldTime,
		}
		c.pendingOrder = append(c.pendingOrder, id)
		c.sessionLock.Unlock()
	}

	// 2. Run retryPending
	c.sessionLock.Lock()
	c.retryPending()
	c.sessionLock.Unlock()

	// 3. Verify order in outgoing channel
	for _, expectedID := range packetIDs {
		select {
		case pkt := <-c.outgoing:
			if p, ok := pkt.(*packets.PublishPacket); ok {
				if p.PacketID != expectedID {
					t.Errorf("Expected retransmission of %d, got %d", expectedID, p.PacketID)
				}
				if !p.Dup {
					t.Errorf("Expected DUP flag to be set for %d", expectedID)
				}
			} else {
				t.Errorf("Expected PUBLISH, got %T", pkt)
			}
		case <-time.After(100 * time.Millisecond):
			t.Errorf("Packet %d was never retransmitted", expectedID)
		}
	}
}

// TestRemovePending verifies that removePending correctly cleans up both
// the map and the order slice.
func TestRemovePending(t *testing.T) {
	c := &Client{
		pending:      make(map[uint16]*pendingOp),
		pendingOrder: []uint16{1, 2, 3, 4, 5},
	}
	for _, id := range c.pendingOrder {
		c.pending[id] = &pendingOp{}
	}

	// Remove middle element
	c.removePending(3)

	if _, exists := c.pending[3]; exists {
		t.Error("ID 3 still exists in pending map")
	}
	expectedOrder := []uint16{1, 2, 4, 5}
	if len(c.pendingOrder) != len(expectedOrder) {
		t.Errorf("Expected order length %d, got %d", len(expectedOrder), len(c.pendingOrder))
	}
	for i, id := range c.pendingOrder {
		if id != expectedOrder[i] {
			t.Errorf("At index %d, expected %d, got %d", i, expectedOrder[i], id)
		}
	}

	// Remove first element
	c.removePending(1)
	expectedOrder = []uint16{2, 4, 5}
	if len(c.pendingOrder) != len(expectedOrder) {
		t.Errorf("Expected order length %d, got %d", len(expectedOrder), len(c.pendingOrder))
	}

	// Remove last element
	c.removePending(5)
	expectedOrder = []uint16{2, 4}
	if len(c.pendingOrder) != len(expectedOrder) {
		t.Errorf("Expected order length %d, got %d", len(expectedOrder), len(c.pendingOrder))
	}
}
