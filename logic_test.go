package mq

import (
	"context"
	"io"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gonzalop/mq/internal/packets"
)

// newTestClient creates a Client for testing with all internal structures initialized.
func newTestClient(opts *clientOptions) *Client {
	if opts == nil {
		opts = defaultOptions("tcp://localhost:1883")
	}
	c := &Client{
		trie:     newTopicTrie(),
		opts:     opts,
		outgoing: make(chan packets.Packet, opts.OutgoingQueueSize),
		incoming: make(chan packets.Packet, opts.IncomingQueueSize),

		packetReceived: make(chan struct{}, 1),
		pingPendingCh:  make(chan struct{}, 1),
		stop:           make(chan struct{}),
		pending:        make(map[uint16]*pendingOp),
		subscriptions:  make(map[string]subscriptionEntry),
		receivedQoS2:   make(map[uint16]struct{}),

		inboundUnacked:  make(map[uint16]struct{}),
		topicAliases:    make(map[string]uint16),
		receivedAliases: make(map[uint16]string),
		disconnected:    make(chan struct{}, 1),
		publishQueue:    []*publishRequest{},
	}
	c.connState.Store(&connectionState{
		caps: extractServerCapabilities(nil),
	})
	return c
}

func TestHandlePubcomp(t *testing.T) {
	// Setup client
	c := &Client{trie: newTopicTrie(),
		pending: make(map[uint16]*pendingOp),
		opts:    defaultOptions("tcp://localhost:1883"),
	}

	// Create a pending operation
	packetID := uint16(10)
	tkn := newToken()
	op := &pendingOp{
		packet:    &packets.PublishPacket{PacketID: packetID, QoS: 2},
		token:     tkn,
		qos:       2,
		timestamp: time.Now(),
	}
	c.pending[packetID] = op
	c.inFlightCount = 1

	// Create PUBCOMP packet
	pubcomp := &packets.PubcompPacket{
		PacketID: packetID,
	}

	// Call handleAck
	c.handleAck(pubcomp.PacketID, pubcomp.ReasonCode)

	// Verify operation is removed from pending
	if _, ok := c.pending[packetID]; ok {
		t.Error("pending operation should be removed")
	}

	// Verify token is completed
	select {
	case <-tkn.Done():
		if tkn.Error() != nil {
			t.Errorf("expected no error, got %v", tkn.Error())
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("token should be completed")
	}

	// Verify inFlightCount is decremented
	if c.inFlightCount != 0 {
		t.Errorf("inFlightCount should be 0, got %d", c.inFlightCount)
	}
}

func TestHandlePubcomp_V5_Error(t *testing.T) {
	// Setup client with V5 protocol
	opts := defaultOptions("tcp://localhost:1883")
	opts.ProtocolVersion = ProtocolV50
	c := &Client{trie: newTopicTrie(),
		pending: make(map[uint16]*pendingOp),
		opts:    opts,
	}

	// Create a pending operation
	packetID := uint16(11)
	tkn := newToken()
	op := &pendingOp{
		packet:    &packets.PublishPacket{PacketID: packetID, QoS: 2},
		token:     tkn,
		qos:       2,
		timestamp: time.Now(),
	}
	c.pending[packetID] = op
	c.inFlightCount = 1

	// Create PUBCOMP packet with error reason code
	pubcomp := &packets.PubcompPacket{
		PacketID:   packetID,
		ReasonCode: 0x92, // Packet identifier not found
	}

	// Call handleAck
	c.handleAck(pubcomp.PacketID, pubcomp.ReasonCode)

	// Verify operation is removed from pending
	if _, ok := c.pending[packetID]; ok {
		t.Error("pending operation should be removed")
	}

	// Verify token is completed with error
	select {
	case <-tkn.Done():
		err := tkn.Error()
		if err == nil {
			t.Error("expected error, got nil")
		} else if mqttErr, ok := err.(*MqttError); !ok || mqttErr.ReasonCode != ReasonCode(0x92) {
			t.Errorf("expected MqttError with code 0x92, got %v", err)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("token should be completed")
	}
}

// MockLogicSessionStore implements SessionStore for testing logic.go
type MockLogicSessionStore struct {
	deletePendingPublishCalled bool
	deletedPacketID            uint16
	deleteError                error

	saveReceivedQoS2Called bool
	savedPacketID          uint16
}

func (m *MockLogicSessionStore) SavePendingPublish(_ uint16, _ *PersistedPublish) error {
	return nil
}
func (m *MockLogicSessionStore) DeletePendingPublish(packetID uint16) error {
	m.deletePendingPublishCalled = true
	m.deletedPacketID = packetID
	return m.deleteError
}
func (m *MockLogicSessionStore) LoadPendingPublishes() (map[uint16]*PersistedPublish, error) {
	return nil, nil
}
func (m *MockLogicSessionStore) ClearPendingPublishes() error { return nil }
func (m *MockLogicSessionStore) SaveSubscription(_ string, _ *PersistedSubscription) error {
	return nil
}
func (m *MockLogicSessionStore) DeleteSubscription(_ string) error { return nil }
func (m *MockLogicSessionStore) LoadSubscriptions() (map[string]*PersistedSubscription, error) {
	return nil, nil
}
func (m *MockLogicSessionStore) SaveReceivedQoS2(packetID uint16) error {
	m.saveReceivedQoS2Called = true
	m.savedPacketID = packetID
	return nil
}
func (m *MockLogicSessionStore) DeleteReceivedQoS2(_ uint16) error { return nil }
func (m *MockLogicSessionStore) LoadReceivedQoS2() (map[uint16]struct{}, error) {
	return nil, nil
}
func (m *MockLogicSessionStore) ClearReceivedQoS2() error { return nil }
func (m *MockLogicSessionStore) Clear() error             { return nil }

func TestHandlePubcomp_WithSessionStore(t *testing.T) {
	// Setup client with session store
	store := &MockLogicSessionStore{}
	opts := defaultOptions("tcp://localhost:1883")
	opts.SessionStore = store

	c := &Client{trie: newTopicTrie(),
		pending: make(map[uint16]*pendingOp),
		opts:    opts,
	}

	// Create a pending operation
	packetID := uint16(12)
	tkn := newToken()
	op := &pendingOp{
		packet:    &packets.PublishPacket{PacketID: packetID, QoS: 2},
		token:     tkn,
		qos:       2,
		timestamp: time.Now(),
	}
	c.pending[packetID] = op
	c.inFlightCount = 1

	// Create PUBCOMP packet
	pubcomp := &packets.PubcompPacket{
		PacketID: packetID,
	}

	// Call handleAck
	c.handleAck(pubcomp.PacketID, pubcomp.ReasonCode)

	// Verify operation is removed from pending
	if _, ok := c.pending[packetID]; ok {
		t.Error("pending operation should be removed")
	}

	// Verify session store called
	if !store.deletePendingPublishCalled {
		t.Error("expected DeletePendingPublish to be called")
	}
	if store.deletedPacketID != packetID {
		t.Errorf("expected deleted packet ID %d, got %d", packetID, store.deletedPacketID)
	}
}

func TestHandlePubcomp_WithSessionStore_Error(t *testing.T) {
	// Setup client with session store that returns error
	store := &MockLogicSessionStore{
		deleteError: &MqttError{ReasonCode: ReasonCode(0x80)}, // Generic error
	}
	opts := defaultOptions("tcp://localhost:1883")
	opts.SessionStore = store
	// We can't easily check log output with default logger, but we can ensure it doesn't panic

	c := &Client{trie: newTopicTrie(),
		pending: make(map[uint16]*pendingOp),
		opts:    opts,
	}

	// Create a pending operation
	packetID := uint16(13)
	tkn := newToken()
	op := &pendingOp{
		packet:    &packets.PublishPacket{PacketID: packetID, QoS: 2},
		token:     tkn,
		qos:       2,
		timestamp: time.Now(),
	}
	c.pending[packetID] = op
	c.inFlightCount = 1

	// Create PUBCOMP packet
	pubcomp := &packets.PubcompPacket{
		PacketID: packetID,
	}

	// Call handleAck
	c.handleAck(pubcomp.PacketID, pubcomp.ReasonCode)

	// Verify operation is removed from pending (error in store shouldn't stop processing)
	if _, ok := c.pending[packetID]; ok {
		t.Error("pending operation should be removed even if store fails")
	}

	// Verify session store called
	if !store.deletePendingPublishCalled {
		t.Error("expected DeletePendingPublish to be called")
	}
}

func TestHandleQoS2Duplicate(t *testing.T) {
	c := &Client{
		trie:         newTopicTrie(),
		receivedQoS2: make(map[uint16]struct{}),
		outgoing:     make(chan packets.Packet, 10),
		stop:         make(chan struct{}),
		opts:         defaultOptions("tcp://localhost:1883"),
	}

	packetID := uint16(42)

	// 1. First time - should return false (not a duplicate)
	if _, isDup := c.handleQoS2Duplicate(packetID); isDup {
		t.Error("expected first call to return false")
	}

	if _, exists := c.receivedQoS2[packetID]; !exists {
		t.Error("expected packetID to be added to receivedQoS2")
	}

	// 2. Second time - should return true (is a duplicate)
	ack, isDup := c.handleQoS2Duplicate(packetID)
	if !isDup {
		t.Error("expected second call to return true")
	}

	// Should have returned a PUBREC
	if ack == nil {
		t.Fatal("expected PUBREC packet, got nil")
	}
	pubrec, ok := ack.(*packets.PubrecPacket)
	if !ok {
		t.Errorf("expected PUBREC packet, got %T", ack)
	} else if pubrec.PacketID != packetID {
		t.Errorf("expected PUBREC with packetID %d, got %d", packetID, pubrec.PacketID)
	}

	// 3. Test with session store
	store := &MockLogicSessionStore{}
	c.opts.SessionStore = store
	packetID2 := uint16(43)

	if _, isDup := c.handleQoS2Duplicate(packetID2); isDup {
		t.Error("expected first call for new packet to return false")
	}

	if !store.saveReceivedQoS2Called {
		t.Error("expected SaveReceivedQoS2 to be called on session store")
	}
	if store.savedPacketID != packetID2 {
		t.Errorf("expected saved packet ID %d, got %d", packetID2, store.savedPacketID)
	}
}

func TestHandlePublish_ConcurrencyLimit(t *testing.T) {
	concurrencyLimit := 2
	opts := defaultOptions("tcp://localhost:1883")
	opts.MaxHandlerConcurrency = concurrencyLimit

	c := &Client{trie: newTopicTrie(),
		opts:           opts,
		handlerSem:     make(chan struct{}, concurrencyLimit),
		stop:           make(chan struct{}),
		subscriptions:  make(map[string]subscriptionEntry),
		inboundUnacked: make(map[uint16]struct{}),
	}

	var activeHandlers atomic.Int32
	var maxHandlers atomic.Int32
	var totalProcessed atomic.Int32

	handler := func(_ *Client, _ Message) {
		active := activeHandlers.Add(1)
		for {
			currentMax := maxHandlers.Load()
			if active <= currentMax || maxHandlers.CompareAndSwap(currentMax, active) {
				break
			}
		}

		// Hold the handler for a bit to ensure concurrency
		time.Sleep(50 * time.Millisecond)

		activeHandlers.Add(-1)
		totalProcessed.Add(1)
	}

	c.defaultHandler = handler

	// Send 5 messages. With limit 2, it should only run 2 at a time.
	for i := range 5 {
		p := &packets.PublishPacket{
			Topic:    "test/topic",
			Payload:  []byte("data"),
			PacketID: uint16(i + 1),
		}
		// handlePublish is what we're testing - it spawns goroutines but blocks on sem
		c.handlePublish(p)
	}

	// Wait for all to finish (total 5)
	for totalProcessed.Load() < 5 {
		time.Sleep(10 * time.Millisecond)
	}

	if maxHandlers.Load() > int32(concurrencyLimit) {
		t.Errorf("max concurrent handlers was %d, expected limit %d", maxHandlers.Load(), concurrencyLimit)
	}
	if totalProcessed.Load() != 5 {
		t.Errorf("expected 5 messages processed, got %d", totalProcessed.Load())
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

	c.connState.Store(&connectionState{
		caps: serverCapabilities{
			ReceiveMaximum: 1,
		},
	})
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

		// Verify that the queued message is STILL in the queue (since it couldn't be sent)
		// We do this before closing stop to avoid logicLoop clearing the queue
		c.sessionLock.Lock()
		if len(c.publishQueue) != 1 {
			t.Errorf("expected 1 message in publishQueue, got %d", len(c.publishQueue))
		}
		c.sessionLock.Unlock()

		// Close stop channel to signal exit
		close(c.stop)

		// Wait for logicLoop to exit
		c.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		t.Log("Test passed: logicLoop exited cleanly (didn't block on full channel)")
	case <-time.After(200 * time.Millisecond):
		t.Fatal("Test timed out: logicLoop blocked despite non-blocking send refactor")
	}
}

// TestQueuedMessagesStayInQueueOnFullChannel verifies that messages remain in the
// publish queue if the outgoing channel is full, instead of being dropped or blocking.
func TestQueuedMessagesStayInQueueOnFullChannel(t *testing.T) {
	// 1. Setup Client with a full outgoing channel
	opts := defaultOptions("tcp://localhost:1883")
	opts.ReceiveMaximum = 1
	opts.OutgoingQueueSize = 1

	c := newTestClient(opts)
	c.outgoing <- &packets.PingreqPacket{} // Fill it up

	c.connState.Store(&connectionState{
		caps: serverCapabilities{
			ReceiveMaximum: 1,
		},
	})

	// 2. Add an in-flight message that we will ACK
	c.pending[1] = &pendingOp{
		token:  newToken(),
		qos:    1,
		packet: &packets.PublishPacket{PacketID: 1, QoS: 1},
	}
	c.inFlightCount = 1

	// 3. Add a queued message
	token := newToken()
	queuedReq := &publishRequest{
		packet: &packets.PublishPacket{Topic: "queued", QoS: 1, Payload: []byte("data")},
		token:  token,
	}
	c.publishQueue = append(c.publishQueue, queuedReq)

	// 4. Start logicLoop
	c.wg.Add(1)
	go c.logicLoop()

	// 5. Trigger queue processing
	c.incoming <- &packets.PubackPacket{PacketID: 1}

	// 6. Verify liveness
	done := make(chan struct{})
	go func() {
		time.Sleep(50 * time.Millisecond)

		// 7. Verify message is still in queue
		c.sessionLock.Lock()
		if len(c.publishQueue) != 1 {
			t.Errorf("expected message to remain in queue, but it was removed")
		}
		c.sessionLock.Unlock()

		close(c.stop)
		c.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Success
	case <-time.After(200 * time.Millisecond):
		t.Fatal("logicLoop blocked while processing queue with full channel")
	}
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
	token := c.Publish(context.Background(), "qos0", []byte("payload"), WithQoS(0))

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
		tokenCh <- c.Publish(context.Background(), "qos0", []byte("payload"), WithQoS(0))
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

// TestSemaphoreStallPrevention verifies that the logicLoop does not stall
// when the message handler semaphore is full.
func TestSemaphoreStallPrevention(t *testing.T) {
	// 1. Setup Client with 1 concurrent handler limit
	opts := defaultOptions("tcp://localhost:1883")
	WithMaxHandlerConcurrency(1)(opts)
	c := newTestClient(opts)
	c.handlerSem = make(chan struct{}, 1)

	// 2. Start logicLoop
	c.wg.Add(1)
	go c.logicLoop()
	defer func() {
		close(c.stop)
		c.wg.Wait()
	}()

	// 3. Block the first handler
	handler1Started := make(chan struct{})
	blockHandler1 := make(chan struct{})

	h1 := func(_ *Client, _ Message) {
		close(handler1Started)
		<-blockHandler1
	}

	c.sessionLock.Lock()
	c.addSubscriptionLocked("topic/1", subscriptionEntry{handler: h1})
	c.sessionLock.Unlock()

	// Send first message
	c.incoming <- &packets.PublishPacket{Topic: "topic/1", QoS: 0, Payload: []byte("1")}

	// Wait for handler 1 to start
	select {
	case <-handler1Started:
		// h1 is now holding the only semaphore slot
	case <-time.After(200 * time.Millisecond):
		t.Fatal("Handler 1 never started")
	}

	// 4. Send second message (should block its handler, but NOT logicLoop)
	handler2Started := make(chan struct{})
	h2 := func(_ *Client, _ Message) {
		close(handler2Started)
	}
	c.addSubscriptionLocked("topic/2", subscriptionEntry{handler: h2})

	c.incoming <- &packets.PublishPacket{Topic: "topic/2", QoS: 0, Payload: []byte("2")}
	// 5. Verify logicLoop is still alive by sending an ACK and waiting for completion
	token := newToken()
	c.sessionLock.Lock()
	c.pending[100] = &pendingOp{token: token, qos: 1}
	c.sessionLock.Unlock()

	c.incoming <- &packets.PubackPacket{PacketID: 100}

	select {
	case <-token.Done():
		// Logic loop processed the PUBACK! Success.
	case <-time.After(200 * time.Millisecond):
		t.Fatal("logicLoop stalled while waiting for handler semaphore")
	}

	// 6. Unblock everything
	close(blockHandler1)

	select {
	case <-handler2Started:
		// Handler 2 eventually ran
	case <-time.After(200 * time.Millisecond):
		t.Fatal("Handler 2 never ran after semaphore freed")
	}
}

// TestDisconnectWaitGroup verifies that Disconnect waits correctly for goroutines to exit.
func TestDisconnectWaitGroup(t *testing.T) {
	opts := defaultOptions("tcp://localhost:1883")
	c := newTestClient(opts)

	// Simulate a running loop
	c.wg.Add(1)
	loopStarted := make(chan struct{})
	loopExited := make(chan struct{})
	go func() {
		close(loopStarted)
		<-c.stop
		time.Sleep(50 * time.Millisecond) // Simulate some work during shutdown
		c.wg.Done()
		close(loopExited)
	}()

	<-loopStarted
	c.connected.Store(true)

	// Call disconnectWithReason(block=true)
	start := time.Now()
	err := c.disconnectWithReason(context.Background(), 0, nil, true)
	elapsed := time.Since(start)

	if err != nil {
		t.Errorf("Disconnect failed: %v", err)
	}

	select {
	case <-loopExited:
		// Success
	default:
		t.Error("Disconnect returned before loop actually exited")
	}

	if elapsed < 50*time.Millisecond {
		t.Errorf("Disconnect returned too early (%v), didn't wait for work", elapsed)
	}
}

// FuzzPacketSequence generates sequences of valid MQTT packets to test the Client state machine.
func FuzzPacketSequence(f *testing.F) {
	// Seed with valid packet type sequences (using uint8 IDs)
	// 2 = CONNACK, 3 = PUBLISH, 4 = PUBACK, 5 = PUBREC, 6 = PUBREL, 7 = PUBCOMP, 9 = SUBACK, 11 = UNSUBACK
	f.Add([]byte{2, 3, 4})    // CONNACK, then QoS 0 PUBLISH, then PUBACK
	f.Add([]byte{3, 5, 6, 7}) // QoS 2 flow

	f.Fuzz(func(t *testing.T, sequence []byte) {
		c := &Client{trie: newTopicTrie(),
			opts: &clientOptions{
				ProtocolVersion: ProtocolV50,
				Logger:          slog.New(slog.NewTextHandler(io.Discard, nil)),
			},
			incoming:       make(chan packets.Packet, 100),
			outgoing:       make(chan packets.Packet, 100),
			pending:        make(map[uint16]*pendingOp),
			subscriptions:  make(map[string]subscriptionEntry),
			receivedQoS2:   make(map[uint16]struct{}),
			inboundUnacked: make(map[uint16]struct{}),
		}

		for _, pType := range sequence {
			var pkt packets.Packet
			packetID := uint16(1) // Constant for simplicity in sequence

			switch pType % 16 {
			case packets.CONNACK:
				pkt = &packets.ConnackPacket{ReturnCode: 0}
			case packets.PUBLISH:
				pkt = &packets.PublishPacket{PacketID: packetID, QoS: 1, Topic: "test"}
			case packets.PUBACK:
				pkt = &packets.PubackPacket{PacketID: packetID}
			case packets.PUBREC:
				pkt = &packets.PubrecPacket{PacketID: packetID}
			case packets.PUBREL:
				pkt = &packets.PubrelPacket{PacketID: packetID}
			case packets.PUBCOMP:
				pkt = &packets.PubcompPacket{PacketID: packetID}
			case packets.SUBACK:
				pkt = &packets.SubackPacket{PacketID: packetID, ReturnCodes: []uint8{0}}
			case packets.UNSUBACK:
				pkt = &packets.UnsubackPacket{PacketID: packetID}
			case packets.PINGRESP:
				pkt = &packets.PingrespPacket{}
			case packets.DISCONNECT:
				pkt = &packets.DisconnectPacket{}
			default:
				continue
			}

			// Simulate packet arrival
			// We call handleIncoming directly to avoid needing logicLoop goroutine
			func() {
				defer func() {
					if r := recover(); r != nil {
						t.Errorf("Recovered from panic during packet %d handling: %v", pType, r)
					}
				}()
				c.handleIncoming(pkt)
			}()
		}

		// Ensure we can still disconnect
		_ = c.Disconnect(context.Background())
	})
}
