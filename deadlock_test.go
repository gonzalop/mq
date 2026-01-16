package mq

import (
	"testing"
	"time"

	"github.com/gonzalop/mq/internal/packets"
)

// TestQueueProcessingDeadlock verifies that the logicLoop does not deadlock
// when the outgoing channel is full and we attempt to process the publish queue.
func TestQueueProcessingDeadlock(t *testing.T) {
	// 1. Setup Client with a full outgoing channel
	// We use a small channel size for testing if we could, but struct has it hardcoded?
	// No, we can make the channel ourselves.

	outgoing := make(chan packets.Packet, 1)
	outgoing <- &packets.PingreqPacket{} // Fill it up immediately

	opts := defaultOptions("tcp://localhost:1883")
	opts.ReceiveMaximum = 1 // Limit flow control

	c := &Client{
		opts:          opts,
		outgoing:      outgoing,
		incoming:      make(chan packets.Packet, 10),
		stop:          make(chan struct{}),
		pending:       make(map[uint16]*pendingOp),
		subscriptions: make(map[string]subscriptionEntry),
		serverCaps: serverCapabilities{
			ReceiveMaximum: 1,
		},
		publishQueue:  []*publishRequest{},
		inFlightCount: 0,
	}
	// Note: We do NOT start writeLoop, so outgoing stays full.

	// 2. Setup State
	// We need 1 in-flight message that we will ACK
	c.pending[1] = &pendingOp{
		token:  newToken(),
		qos:    1,
		packet: &packets.PublishPacket{PacketID: 1, QoS: 1},
	}
	c.inFlightCount = 1

	// We need 1 queued message that wants to go out
	queuedReq := &publishRequest{
		packet: &packets.PublishPacket{Topic: "queued", QoS: 1, Payload: []byte("data")},
		token:  newToken(),
	}
	c.publishQueue = append(c.publishQueue, queuedReq)

	// 3. Start logicLoop
	c.wg.Add(1)
	go c.logicLoop()

	// 4. Trigger the deadlock
	// Send a PUBACK for packet 1.
	// This will decrease inFlightCount to 0.
	// logicLoop will call processPublishQueue.
	// processPublishQueue will see inFlightCount (0) < ReceiveMax (1).
	// It will try to send the queuedReq.
	// It calls sendPublishLocked -> c.outgoing <- pkt.
	// DEADLOCK EXPECTED HERE because outgoing is full.

	ack := &packets.PubackPacket{PacketID: 1}
	c.incoming <- ack

	// 5. Verify liveness
	// If deadlocked, logicLoop will never process the STOP signal.

	done := make(chan struct{})
	go func() {
		// Give it a tiny bit of time to process the ACK and get stuck
		time.Sleep(50 * time.Millisecond)

		// Close stop channel to signal exit
		close(c.stop)

		// Wait for logicLoop to exit
		c.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		t.Log("Test passed: logicLoop exited cleanly")
	case <-time.After(200 * time.Millisecond):
		t.Fatal("Test timed out: logicLoop is deadlocked trying to send to full outgoing channel")
	}
}
