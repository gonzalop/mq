package mq

import (
	"context"
	"testing"
	"time"

	"github.com/gonzalop/mq/internal/packets"
)

// newTestClient creates a Client for testing with all internal structures initialized.
func newTestClient(opts *clientOptions) *Client {
	if opts == nil {
		opts = defaultOptions("tcp://localhost:1883")
	}
	return &Client{
		opts:            opts,
		outgoing:        make(chan packets.Packet, opts.OutgoingQueueSize),
		incoming:        make(chan packets.Packet, opts.IncomingQueueSize),
		packetReceived:  make(chan struct{}, 1),
		pingPendingCh:   make(chan struct{}, 1),
		stop:            make(chan struct{}),
		pending:         make(map[uint16]*pendingOp),
		subscriptions:   make(map[string]subscriptionEntry),
		receivedQoS2:    make(map[uint16]struct{}),
		inboundUnacked:  make(map[uint16]struct{}),
		topicAliases:    make(map[string]uint16),
		receivedAliases: make(map[uint16]string),
		disconnected:    make(chan struct{}, 1),
		publishQueue:    []*publishRequest{},
	}
}

// TestQueueProcessingDeadlock verifies that the logicLoop does not deadlock
// when the outgoing channel is full and we attempt to process the publish queue.
func TestQueueProcessingDeadlock(t *testing.T) {
	// 1. Setup Client with a full outgoing channel
	opts := defaultOptions("tcp://localhost:1883")
	opts.ReceiveMaximum = 1
	opts.OutgoingQueueSize = 1

	c := newTestClient(opts)
	c.outgoing <- &packets.PingreqPacket{} // Fill it up immediately

	c.serverCaps = serverCapabilities{
		ReceiveMaximum: 1,
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

// TestQueuedTokensCompletedOnFullChannel verifies that tokens for queued messages
// are completed (with an error) when sendPublishLocked fails due to a full outgoing channel.
func TestQueuedTokensCompletedOnFullChannel(t *testing.T) {
	// 1. Setup Client with a full outgoing channel
	opts := defaultOptions("tcp://localhost:1883")
	opts.ReceiveMaximum = 1
	opts.OutgoingQueueSize = 1

	c := newTestClient(opts)
	c.outgoing <- &packets.PingreqPacket{} // Fill it up

	c.serverCaps = serverCapabilities{
		ReceiveMaximum: 1,
	}

	// 2. Add an in-flight message that we will ACK
	c.pending[1] = &pendingOp{
		token:  newToken(),
		qos:    1,
		packet: &packets.PublishPacket{PacketID: 1, QoS: 1},
	}
	c.inFlightCount = 1

	// 3. Add a queued message that we want to move to outgoing
	token := newToken()
	queuedReq := &publishRequest{
		packet: &packets.PublishPacket{Topic: "queued", QoS: 1, Payload: []byte("data")},
		token:  token,
	}
	c.publishQueue = append(c.publishQueue, queuedReq)

	// 4. Start logicLoop
	c.wg.Add(1)
	go c.logicLoop()

	// 5. Trigger the move from queue to outgoing
	// Send a PUBACK for packet 1. This decreases inFlightCount to 0 and calls processPublishQueue.
	c.incoming <- &packets.PubackPacket{PacketID: 1}

	// 6. Verify that the token for the queued message COMPLETES.
	// Without the fix, this will block forever because sendPublishLocked blocks on outgoing.
	select {
	case <-token.Done():
		if token.Error() == nil {
			t.Error("Expected error because outgoing channel was full, got nil")
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("DEADLOCK: Queued token never completed because sendPublishLocked blocked on full channel")
	}

	// Cleanup
	close(c.stop)
	c.wg.Wait()
}

// TestQueuedTokensCompletedOnShutdown verifies that tokens for messages still in the
// flow control queue are completed when the client is stopped.
func TestQueuedTokensCompletedOnShutdown(t *testing.T) {
	opts := defaultOptions("tcp://localhost:1883")
	c := newTestClient(opts)

	// Add a queued message
	token := newToken()
	c.publishQueue = append(c.publishQueue, &publishRequest{
		packet: &packets.PublishPacket{Topic: "queued", QoS: 1},
		token:  token,
	})

	// Start logicLoop and stop it
	c.wg.Add(1)
	go c.logicLoop()
	close(c.stop)

	// Token should complete
	select {
	case <-token.Done():
		if token.Error() == nil {
			t.Error("Expected error on shutdown, got nil")
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("DEADLOCK: Queued token never completed on shutdown")
	}
	c.wg.Wait()
}

// TestQoS0NonBlocking verifies that QoS 0 publishes do not block when the outgoing channel is full.
func TestQoS0NonBlocking(t *testing.T) {
	// 1. Setup Client with a small, full outgoing channel
	opts := defaultOptions("tcp://localhost:1883")
	opts.OutgoingQueueSize = 1
	c := newTestClient(opts)
	c.outgoing <- &packets.PingreqPacket{} // Fill it up

	// 2. Publish QoS 0
	// Without the fix, this would block forever here because it tries to send to 'outgoing'.
	token := c.Publish("qos0", []byte("payload"), WithQoS(0))

	// 3. Verify it completed immediately and is marked as dropped
	select {
	case <-token.Done():
		if err := token.Error(); err != nil {
			t.Errorf("Expected nil error for QoS 0 drop, got %v", err)
		}
		if !token.Dropped() {
			t.Error("Expected token.Dropped() to be true")
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("QoS 0 publish blocked on full outgoing channel")
	}
}

// TestCustomBufferSizes verifies that the client respects custom buffer size options.
func TestCustomBufferSizes(t *testing.T) {
	opts := defaultOptions("tcp://localhost:1883")
	WithOutgoingQueueSize(500)(opts)
	WithIncomingQueueSize(50)(opts)

	c := newTestClient(opts)

	if cap(c.outgoing) != 500 {
		t.Errorf("Expected outgoing capacity 500, got %d", cap(c.outgoing))
	}
	if cap(c.incoming) != 50 {
		t.Errorf("Expected incoming capacity 50, got %d", cap(c.incoming))
	}
}

// TestQoS0Blocking verifies that QoS 0 publishes block when the outgoing channel is full
// if the QoS0LimitPolicyBlock policy is set.
func TestQoS0Blocking(t *testing.T) {
	// 1. Setup Client with a small, full outgoing channel and Block policy
	opts := defaultOptions("tcp://localhost:1883")
	opts.QoS0Policy = QoS0LimitPolicyBlock
	opts.OutgoingQueueSize = 1
	c := newTestClient(opts)
	c.outgoing <- &packets.PingreqPacket{} // Fill it up

	// 2. Publish QoS 0 in a goroutine because it should block
	tokenCh := make(chan Token, 1)
	go func() {
		tokenCh <- c.Publish("qos0", []byte("payload"), WithQoS(0))
	}()

	// 3. Verify it's blocked (no token received yet)
	var token Token
	select {
	case <-tokenCh:
		t.Fatal("QoS 0 publish should have blocked on full outgoing channel")
	case <-time.After(100 * time.Millisecond):
		// Success, it's blocked
	}

	// 4. Drain the channel to unblock
	select {
	case <-c.outgoing:
		// Channel should now have space
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Failed to drain outgoing channel")
	}

	// 5. Verify it unblocks and completes
	select {
	case token = <-tokenCh:
		// Publish returned
		if err := token.Wait(context.Background()); err != nil {
			t.Errorf("Expected nil error, got %v", err)
		}
		if token.Dropped() {
			t.Error("Expected token.Dropped() to be false for Block policy")
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("QoS 0 publish remained blocked after channel drain")
	}
}
