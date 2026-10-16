package mq

import (
	"context"
	"net"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gonzalop/mq/internal/packets"
)

func TestValidatePayloadFormat(t *testing.T) {
	tests := []struct {
		name    string
		payload []byte
		props   *Properties
		wantErr bool
	}{
		{
			name:    "No Properties",
			payload: []byte{0xFF, 0xFE}, // Invalid UTF-8
			props:   nil,
			wantErr: false,
		},
		{
			name:    "No Payload Format",
			payload: []byte{0xFF, 0xFE},
			props:   &Properties{},
			wantErr: false,
		},
		{
			name:    "Payload Format Bytes (0)",
			payload: []byte{0xFF, 0xFE},
			props: &Properties{
				PayloadFormat: new(PayloadFormatBytes),
			},
			wantErr: false,
		},
		{
			name:    "Payload Format UTF-8 (1) - Valid",
			payload: []byte("Hello World"),
			props: &Properties{
				PayloadFormat: new(PayloadFormatUTF8),
			},
			wantErr: false,
		},
		{
			name:    "Payload Format UTF-8 (1) - Invalid",
			payload: []byte{0xFF, 0xFE},
			props: &Properties{
				PayloadFormat: new(PayloadFormatUTF8),
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validatePayloadFormat(tt.payload, tt.props)
			if (err != nil) != tt.wantErr {
				t.Errorf("validatePayloadFormat() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestReceiveMaximum_LimitExceeded(t *testing.T) {
	// Create a client with ReceiveMaximum = 2
	c := &Client{trie: newTopicTrie(),
		opts: &clientOptions{
			ProtocolVersion:      ProtocolV50,
			ReceiveMaximum:       2,
			ReceiveMaximumPolicy: LimitPolicyStrict,
			Logger:               testLogger(),
		},
		outgoing:       make(chan packets.Packet), // Unbuffered, so writes will BLOCK
		stop:           make(chan struct{}),
		inboundUnacked: make(map[uint16]struct{}),
		receivedQoS2:   make(map[uint16]struct{}),
		subscriptions:  make(map[string]subscriptionEntry),
	}

	// Message 1 (QoS 1)
	// Add a handler so it doesn't get acked and removed immediately
	c.sessionLock.Lock()
	c.addSubscriptionLocked("t", subscriptionEntry{handler: func(_ *Client, _ Message) {}})
	c.sessionLock.Unlock()

	// c.outgoing is unbuffered, so sendPackets will drop the PUBACK.
	// This leaves PacketID 1 in inboundUnacked.
	c.sendPackets(c.handleIncoming(&packets.PublishPacket{Topic: "t", QoS: 1, PacketID: 1}))
	if len(c.inboundUnacked) != 1 {
		t.Errorf("expected 1 unacked, got %d", len(c.inboundUnacked))
	}

	// Message 2 (QoS 1)
	c.sendPackets(c.handleIncoming(&packets.PublishPacket{Topic: "t", QoS: 1, PacketID: 2}))
	if len(c.inboundUnacked) != 2 {
		t.Errorf("expected 2 unacked, got %d", len(c.inboundUnacked))
	}

	// Message 3 (QoS 1) - Should drop the connection!
	conn, peer := net.Pipe()
	defer peer.Close()
	c.conn = conn
	c.connected.Store(true)

	c.handleIncoming(&packets.PublishPacket{Topic: "t", QoS: 1, PacketID: 3})

	if c.IsConnected() || c.conn != nil {
		t.Error("client should have dropped the connection due to receive maximum exceeded")
	}
}

func TestReceiveMaximum_QoS2_Enforcement(t *testing.T) {
	c := &Client{trie: newTopicTrie(),
		opts: &clientOptions{
			ProtocolVersion:      ProtocolV50,
			ReceiveMaximum:       1,
			ReceiveMaximumPolicy: LimitPolicyStrict,
			Logger:               testLogger(),
		},
		outgoing:       make(chan packets.Packet), // Unbuffered, blocks
		stop:           make(chan struct{}),
		inboundUnacked: make(map[uint16]struct{}),
		receivedQoS2:   make(map[uint16]struct{}),
		subscriptions:  make(map[string]subscriptionEntry),
	}
	c.sessionLock.Lock()
	c.addSubscriptionLocked("t", subscriptionEntry{handler: func(_ *Client, _ Message) {}})
	c.sessionLock.Unlock()

	conn, peer := net.Pipe()
	defer peer.Close()
	c.conn = conn
	c.connected.Store(true)

	// Msg 1 (QoS 2)
	c.sendPackets(c.handleIncoming(&packets.PublishPacket{Topic: "t", QoS: 2, PacketID: 10}))
	if len(c.inboundUnacked) != 1 {
		t.Errorf("expected 1 unacked, got %d", len(c.inboundUnacked))
	}

	// Msg 2 (QoS 2) - Should drop the connection
	c.handleIncoming(&packets.PublishPacket{Topic: "t", QoS: 2, PacketID: 11})

	if c.IsConnected() || c.conn != nil {
		t.Error("client should have dropped the connection due to receive maximum exceeded (QoS 2)")
	}
}

func TestReceiveMaximum_QoS1_AckReleasesSlot(t *testing.T) {
	c := &Client{trie: newTopicTrie(),
		opts: &clientOptions{
			ProtocolVersion: ProtocolV50,
			ReceiveMaximum:  1,
			Logger:          testLogger(),
		},
		outgoing:       make(chan packets.Packet, 10), // Buffered!
		stop:           make(chan struct{}),
		inboundUnacked: make(map[uint16]struct{}),
		subscriptions:  make(map[string]subscriptionEntry),
	}

	// Msg 1 (QoS 1)
	// Channel has space, so PUBACK is "sent" (queued).
	// Logic loop removes it from tracking immediately.
	c.sendPackets(c.handleIncoming(&packets.PublishPacket{Topic: "t", QoS: 1, PacketID: 1}))

	if len(c.inboundUnacked) != 0 {
		t.Errorf("expected 0 unacked (acked immediately), got %d", len(c.inboundUnacked))
	}

	// Msg 2 (QoS 1) - Should be fine
	c.sendPackets(c.handleIncoming(&packets.PublishPacket{Topic: "t", QoS: 1, PacketID: 2}))

	if len(c.inboundUnacked) != 0 {
		t.Errorf("expected 0 unacked, got %d", len(c.inboundUnacked))
	}

	// Check outgoing channel has 2 PUBACKs
	if len(c.outgoing) != 2 {
		t.Errorf("expected 2 pending PUBACKs, got %d", len(c.outgoing))
	}
}

func TestReceiveMaximum_QoS2_Lifecycle(t *testing.T) {
	c := &Client{trie: newTopicTrie(),
		opts: &clientOptions{
			ProtocolVersion: ProtocolV50,
			ReceiveMaximum:  1,
			Logger:          testLogger(),
		},
		outgoing:       make(chan packets.Packet, 10), // Buffered
		stop:           make(chan struct{}),
		inboundUnacked: make(map[uint16]struct{}),
		receivedQoS2:   make(map[uint16]struct{}),
		subscriptions:  make(map[string]subscriptionEntry),
	}

	c.sessionLock.Lock()
	c.addSubscriptionLocked("t", subscriptionEntry{handler: func(_ *Client, _ Message) {}})
	c.sessionLock.Unlock()

	// 1. PUBLISH QoS 2
	// Sends PUBREC. Unlike QoS 1 ACK, QoS 2 flow is not done.
	// It should REMAIN in inboundUnacked until PUBCOMP.
	c.sendPackets(c.handleIncoming(&packets.PublishPacket{Topic: "t", QoS: 2, PacketID: 5}))

	if len(c.inboundUnacked) != 1 {
		t.Errorf("expected 1 unacked (QoS 2 incomplete), got %d", len(c.inboundUnacked))
	}

	// 2. Client receives PUBREL (step 2)
	// Sends PUBCOMP. This is where we release the slot.
	c.handlePubrel(&packets.PubrelPacket{PacketID: 5})

	if len(c.inboundUnacked) != 0 {
		t.Errorf("expected 0 unacked after PUBCOMP, got %d", len(c.inboundUnacked))
	}
}

func TestReceiveMaximum_SoftLimit(t *testing.T) {
	// Create a client with ReceiveMaximum = 1 and Default Policy (Ignore)
	c := &Client{trie: newTopicTrie(),
		opts: &clientOptions{
			ProtocolVersion:      ProtocolV50,
			ReceiveMaximum:       1,
			ReceiveMaximumPolicy: LimitPolicyIgnore,
			Logger:               testLogger(),
		},
		outgoing:       make(chan packets.Packet), // Unbuffered, blocks so messages stay unacked
		stop:           make(chan struct{}),
		inboundUnacked: make(map[uint16]struct{}),
		subscriptions:  make(map[string]subscriptionEntry),
	}
	c.connected.Store(true)

	c.sessionLock.Lock()
	c.addSubscriptionLocked("t", subscriptionEntry{handler: func(_ *Client, _ Message) {}})
	c.sessionLock.Unlock()

	// Msg 1 (QoS 1)
	c.sendPackets(c.handleIncoming(&packets.PublishPacket{Topic: "t", QoS: 1, PacketID: 1}))
	if len(c.inboundUnacked) != 1 {
		t.Errorf("expected 1 unacked, got %d", len(c.inboundUnacked))
	}

	// Msg 2 (QoS 1) - Should overflow but NOT disconnect
	c.sendPackets(c.handleIncoming(&packets.PublishPacket{Topic: "t", QoS: 1, PacketID: 2}))

	if !c.IsConnected() {
		t.Error("client should NOT have disconnected with SoftLimit policy")
	}

	// It should also track the second message even if it overflowed (spec doesn't say "don't process", says "disconnect")
	// If we ignore, we probably should track it so we can eventually ack it?
	// Logic says: "if !strict { log; } c.inboundUnacked[...] = struct{}{}"
	// So yes, it is tracked.
	if len(c.inboundUnacked) != 2 {
		t.Errorf("expected 2 unacked (overflow allowed), got %d", len(c.inboundUnacked))
	}

	// Check that we logged that warning?
	// receiveMaxExceededLogged should be true
	if !c.receiveMaxExceededLogged {
		t.Error("Expected receiveMaxExceededLogged to be true")
	}

	c.prepareConnectionState()
	if c.receiveMaxExceededLogged {
		t.Error("Expected receiveMaxExceededLogged to be reset to false after prepareConnectionState")
	}
}

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

	c.addSubscriptionLocked("topic/1", subscriptionEntry{handler: h})

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

// TestOrderedRetransmission verifies that on reconnect pending packets are
// resent once, in the order they were originally sent, as DUP copies, and that
// queued copies of them are dropped while untracked packets are kept.
func TestOrderedRetransmission(t *testing.T) {
	opts := defaultOptions("tcp://localhost:1883")
	c := newTestClient(opts)

	packetIDs := []uint16{10, 5, 20, 1}
	stored := make(map[uint16]*packets.PublishPacket)
	for _, id := range packetIDs {
		pkt := &packets.PublishPacket{PacketID: id, QoS: 1, Topic: "test"}
		stored[id] = pkt
		c.addPendingLocked(id, &pendingOp{packet: pkt})
	}

	// Still queued from the lost connection: a copy of an in-flight publish
	// (replaced by the resend) and a QoS 0 publish (kept, after the resend).
	qos0 := &packets.PublishPacket{QoS: 0, Topic: "qos0"}
	c.outgoing <- stored[5]
	c.outgoing <- qos0

	c.sessionLock.Lock()
	toSend := c.takeResendLocked()
	c.sessionLock.Unlock()

	if len(toSend) != len(packetIDs)+1 {
		t.Fatalf("got %d packets to send, want %d", len(toSend), len(packetIDs)+1)
	}
	for i, expectedID := range packetIDs {
		p, ok := toSend[i].(*packets.PublishPacket)
		if !ok {
			t.Fatalf("packet %d: expected PUBLISH, got %T", i, toSend[i])
		}
		if p.PacketID != expectedID {
			t.Errorf("packet %d: expected retransmission of %d, got %d", i, expectedID, p.PacketID)
		}
		if !p.Dup {
			t.Errorf("expected DUP flag to be set for %d", expectedID)
		}
		if p == stored[expectedID] || stored[expectedID].Dup {
			t.Errorf("stored packet %d was modified instead of copied", expectedID)
		}
	}
	if toSend[len(packetIDs)] != qos0 {
		t.Errorf("expected the queued QoS 0 publish last, got %#v", toSend[len(packetIDs)])
	}
	if len(c.outgoing) != 0 {
		t.Errorf("outgoing should be drained, has %d", len(c.outgoing))
	}
}

// TestResendOrderAcrossPacketIDWrap checks that the resend order follows the
// order operations were created in, not their packet IDs, which wrap around.
func TestResendOrderAcrossPacketIDWrap(t *testing.T) {
	c := newTestClient(defaultOptions("tcp://localhost:1883"))
	for _, id := range []uint16{65534, 65535, 1} {
		c.addPendingLocked(id, &pendingOp{packet: &packets.PublishPacket{PacketID: id, QoS: 1, Topic: "t"}})
	}
	// An acked operation leaves no trace.
	c.addPendingLocked(2, &pendingOp{packet: &packets.PublishPacket{PacketID: 2, QoS: 1, Topic: "t"}})
	c.removePending(2)

	toSend := c.takeResendLocked()
	var got []uint16
	for _, p := range toSend {
		got = append(got, p.(*packets.PublishPacket).PacketID)
	}
	if want := []uint16{65534, 65535, 1}; !slices.Equal(got, want) {
		t.Errorf("resend order = %v, want %v", got, want)
	}
}

type dummySessionStore struct {
	pending map[uint16]*PersistedPublish
}

func (d *dummySessionStore) LoadPendingPublishes() (map[uint16]*PersistedPublish, error) {
	return d.pending, nil
}
func (d *dummySessionStore) SavePendingPublish(uint16, *PersistedPublish) error    { return nil }
func (d *dummySessionStore) DeletePendingPublish(uint16) error                     { return nil }
func (d *dummySessionStore) SaveSubscription(string, *PersistedSubscription) error { return nil }
func (d *dummySessionStore) DeleteSubscription(string) error                       { return nil }
func (d *dummySessionStore) LoadSubscriptions() (map[string]*PersistedSubscription, error) {
	return nil, nil
}
func (d *dummySessionStore) SaveReceivedQoS2(uint16) error   { return nil }
func (d *dummySessionStore) DeleteReceivedQoS2(uint16) error { return nil }
func (d *dummySessionStore) LoadReceivedQoS2() (map[uint16]struct{}, error) {
	return nil, nil
}
func (d *dummySessionStore) ClearReceivedQoS2() error { return nil }
func (d *dummySessionStore) Clear() error             { return nil }

func TestPersistencePendingOrder(t *testing.T) {
	mockStore := &dummySessionStore{
		pending: map[uint16]*PersistedPublish{
			20: {Topic: "test", QoS: 1, Payload: []byte("p20")},
			5:  {Topic: "test", QoS: 2, Payload: []byte("p5")},
			10: {Topic: "test", QoS: 1, Payload: []byte("p10")},
			1:  {Topic: "test", QoS: 0, Payload: []byte("p1")}, // QoS 0 should not increment inFlightCount but should be restored
		},
	}

	opts := defaultOptions("tcp://localhost:1883")
	opts.SessionStore = mockStore
	c := newTestClient(opts)

	if err := c.loadSessionState(); err != nil {
		t.Fatalf("loadSessionState failed: %v", err)
	}

	// Verify in-flight count (only QoS 1 and 2 increment it)
	// From the mock: QoS 1 (20), QoS 2 (5), QoS 1 (10) -> count should be 3
	if c.inFlightCount != 3 {
		t.Errorf("Expected inFlightCount 3, got %d", c.inFlightCount)
	}

	// Restored publishes are resent in packet ID order
	expectedOrder := []uint16{1, 5, 10, 20}
	var got []uint16
	for _, p := range c.takeResendLocked() {
		got = append(got, p.(*packets.PublishPacket).PacketID)
	}
	if !slices.Equal(got, expectedOrder) {
		t.Errorf("resend order = %v, want %v", got, expectedOrder)
	}
}

// recSessionStore records which pending publishes are currently persisted.
type recSessionStore struct {
	dummySessionStore
	mu    sync.Mutex
	saved map[uint16]bool
}

func (r *recSessionStore) SavePendingPublish(id uint16, _ *PersistedPublish) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.saved[id] = true
	return nil
}

func (r *recSessionStore) DeletePendingPublish(id uint16) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.saved, id)
	return nil
}

// Finding 3 (part b): rolling back a queued publish because outgoing is full
// must not leave it in the session store.
func TestPublishQueue_FullOutgoingLeavesStoreClean(t *testing.T) {
	store := &recSessionStore{saved: map[uint16]bool{}}
	opts := defaultOptions("tcp://localhost:1883")
	opts.SessionStore = store
	opts.OutgoingQueueSize = 1
	c := newTestClient(opts)
	c.outgoing <- &packets.PingreqPacket{} // fill the channel
	c.publishQueue = []*publishRequest{{ctx: context.Background(), packet: &packets.PublishPacket{Topic: "t", QoS: 1}, token: newToken()}}

	c.processPublishQueueLocked()

	if len(store.saved) != 0 {
		t.Errorf("%d orphan pending publish(es) left in the store after rollback", len(store.saved))
	}
}

// Finding 3 (part a): when packet IDs are exhausted the token fails, so the
// request must leave the queue and must never be sent afterwards.
func TestPublishQueue_FailedPublishIsNotSentLater(t *testing.T) {
	opts := defaultOptions("tcp://localhost:1883")
	opts.OutgoingQueueSize = 10
	c := newTestClient(opts)
	for i := 1; i <= 65535; i++ {
		c.pending[uint16(i)] = &pendingOp{token: newToken()}
	}
	tok := newToken()
	c.publishQueue = []*publishRequest{{ctx: context.Background(), packet: &packets.PublishPacket{Topic: "t", QoS: 1}, token: tok}}

	c.processPublishQueueLocked()
	failed := tok.Error() != nil
	delete(c.pending, 7) // free one ID
	sent := c.processPublishQueueLocked()

	if failed && len(sent) > 0 {
		t.Errorf("token already failed (%v) but the publish was sent later anyway", tok.Error())
	}
}

// TestResendOnReconnect checks that an unacked QoS 1 publish is resent exactly
// once, with DUP set, after the connection drops, and that its token then
// completes on the new connection's PUBACK.
func TestResendOnReconnect(t *testing.T) {
	var dials atomic.Int32
	firstPublish := make(chan uint16, 1)
	resent := make(chan *packets.PublishPacket, 10)

	dialer := DialFunc(func(context.Context, string, string) (net.Conn, error) {
		n := dials.Add(1)
		cli, srv := net.Pipe()
		go func() {
			defer srv.Close()
			if _, err := packets.ReadPacket(srv, 4, 0); err != nil {
				return
			}
			connack := &packets.ConnackPacket{ReturnCode: packets.ConnAccepted, SessionPresent: n > 1}
			if _, err := connack.WriteTo(srv); err != nil {
				return
			}
			for {
				pkt, err := packets.ReadPacket(srv, 4, 0)
				if err != nil {
					return
				}
				pub, ok := pkt.(*packets.PublishPacket)
				if !ok {
					continue
				}
				if n == 1 {
					// Drop the connection without acking.
					firstPublish <- pub.PacketID
					return
				}
				resent <- pub
				ack := &packets.PubackPacket{PacketID: pub.PacketID}
				if _, err := ack.WriteTo(srv); err != nil {
					return
				}
			}
		}()
		return cli, nil
	})

	c, err := Dial("tcp://mock:1883",
		WithClientID("resend"),
		WithCleanSession(false),
		WithProtocolVersion(ProtocolV311),
		WithDialer(dialer),
		WithReconnectBackoff(10*time.Millisecond, 10*time.Millisecond, false),
	)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer c.Disconnect(context.Background())

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	tok := c.Publish(ctx, "a/b", []byte("x"), WithQoS(1))

	var id uint16
	select {
	case id = <-firstPublish:
	case <-ctx.Done():
		t.Fatal("first connection never received the PUBLISH")
	}

	if err := tok.Wait(ctx); err != nil {
		t.Fatalf("publish token: %v", err)
	}

	select {
	case pub := <-resent:
		if pub.PacketID != id || !pub.Dup {
			t.Errorf("resent PUBLISH: id=%d dup=%v, want id=%d dup=true", pub.PacketID, pub.Dup, id)
		}
	default:
		t.Fatal("token completed but no resent PUBLISH was seen")
	}

	// Exactly once: no further copies.
	select {
	case pub := <-resent:
		t.Errorf("PUBLISH resent more than once (id=%d dup=%v)", pub.PacketID, pub.Dup)
	case <-time.After(200 * time.Millisecond):
	}
}
