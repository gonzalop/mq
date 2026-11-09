package mq

import (
	"context"
	"maps"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gonzalop/mq/internal/packets"
)

// MockSessionStoreForRestore implements SessionStore interface for testing restoration
type MockSessionStoreForRestore struct {
	pendingPublishes map[uint16]*PersistedPublish
}

func (m *MockSessionStoreForRestore) SavePendingPublish(_ uint16, _ *PersistedPublish) error {
	return nil
}
func (m *MockSessionStoreForRestore) DeletePendingPublish(_ uint16) error { return nil }
func (m *MockSessionStoreForRestore) LoadPendingPublishes() (map[uint16]*PersistedPublish, error) {
	// Return copy to avoid races in test
	result := make(map[uint16]*PersistedPublish)
	maps.Copy(result, m.pendingPublishes)
	return result, nil
}
func (m *MockSessionStoreForRestore) SaveSubscription(_ string, _ *PersistedSubscription) error {
	return nil
}
func (m *MockSessionStoreForRestore) DeleteSubscription(_ string) error { return nil }
func (m *MockSessionStoreForRestore) LoadSubscriptions() (map[string]*PersistedSubscription, error) {
	return nil, nil
}
func (m *MockSessionStoreForRestore) SaveReceivedQoS2(_ uint16) error                { return nil }
func (m *MockSessionStoreForRestore) DeleteReceivedQoS2(_ uint16) error              { return nil }
func (m *MockSessionStoreForRestore) LoadReceivedQoS2() (map[uint16]struct{}, error) { return nil, nil }
func (m *MockSessionStoreForRestore) ClearReceivedQoS2() error                       { return nil }
func (m *MockSessionStoreForRestore) Clear() error                                   { return nil }

func TestLoadSessionState_InFlightCount(t *testing.T) {
	// Create mock store with specific pending publishes
	store := &MockSessionStoreForRestore{
		pendingPublishes: map[uint16]*PersistedPublish{
			1: {Topic: "t1", QoS: 0, Payload: []byte("q0")}, // Should NOT count (QoS 0 is typically not persisted, but if it were, it shouldn't count towards inFlight)
			2: {Topic: "t2", QoS: 1, Payload: []byte("q1")}, // Should count
			3: {Topic: "t3", QoS: 2, Payload: []byte("q2")}, // Should count
			4: {Topic: "t4", QoS: 1, Payload: []byte("q1")}, // Should count
		},
	}

	// Create client using defaultOptions to ensure proper initialization
	opts := defaultOptions("tcp://localhost:1883")
	opts.SessionStore = store
	// The default logger is io.Discard, which is fine for tests

	c := &Client{trie: newTopicTrie(),
		opts: opts,
	}

	// Perform loading
	if err := c.loadSessionState(); err != nil {
		t.Fatalf("loadSessionState failed: %v", err)
	}

	// Verify inFlightCount
	// Expected: 3 (PacketIDs 2, 3, 4)
	expectedInFlight := 3
	if c.inFlightCount != expectedInFlight {
		t.Errorf("inFlightCount = %d, want %d", c.inFlightCount, expectedInFlight)
	}

	// Also verify that the pending map is populated correctly
	if len(c.pending) != 4 {
		t.Errorf("pending map size = %d, want 4", len(c.pending))
	}

	// Verify Packet ID 1 (QoS 0) is present in map but didn't increment count
	if _, ok := c.pending[1]; !ok {
		t.Error("Packet ID 1 missing from pending map")
	}

	// Extra check: Verify that if we add another item manually, it increments
	c.inFlightCount++
	if c.inFlightCount != 4 {
		t.Errorf("inFlightCount didn't increment correctly, got %d", c.inFlightCount)
	}
}

type clearTrackingSessionStore struct {
	MockSessionStoreForRestore
	clearCalled bool
}

func (c *clearTrackingSessionStore) Clear() error {
	c.clearCalled = true
	return nil
}

func TestClearSessionStoreOnCleanSession(t *testing.T) {
	mockStore := &clearTrackingSessionStore{}

	// Attempt connection (will fail since there is no broker running, but Clear should run first)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	_, _ = DialContext(ctx, "tcp://localhost:1883",
		WithSessionStore(mockStore),
		WithCleanSession(true),
	)

	if !mockStore.clearCalled {
		t.Error("expected session store Clear() to be called on CleanSession=true")
	}
}

type closeTrackingSessionStore struct {
	MockSessionStoreForRestore
	closeCalled bool
}

func (c *closeTrackingSessionStore) Close() error {
	c.closeCalled = true
	return nil
}

func TestSessionStoreAutoClose(t *testing.T) {
	mockStore := &closeTrackingSessionStore{}

	opts := defaultOptions("tcp://localhost:1883")
	opts.SessionStore = mockStore

	c := &Client{
		trie:     newTopicTrie(),
		opts:     opts,
		outgoing: make(chan packets.Packet, 1),
		stop:     make(chan struct{}),
	}

	c.connected.Store(true)

	// Disconnect should trigger the Close() method on SessionStore
	err := c.disconnectWithReason(context.Background(), uint8(ReasonCodeNormalDisconnect), nil, true)
	if err != nil {
		t.Fatalf("Disconnect failed: %v", err)
	}

	if !mockStore.closeCalled {
		t.Error("expected SessionStore Close() to be called automatically on Disconnect()")
	}
}

// qos2ClearRecorder records how the client clears received QoS 2 IDs.
type qos2ClearRecorder struct {
	MockSessionStoreForRestore
	cleared   bool
	deletedID []uint16
}

func (r *qos2ClearRecorder) ClearReceivedQoS2() error { r.cleared = true; return nil }
func (r *qos2ClearRecorder) DeleteReceivedQoS2(id uint16) error {
	r.deletedID = append(r.deletedID, id)
	return nil
}

// TestCheckSessionPresent_ClearsReceivedQoS2 checks that a CONNACK without a
// session clears the stored QoS 2 IDs through ClearReceivedQoS2. It used to
// call DeleteReceivedQoS2(0), which only FileStore treated as "clear all":
// other stores kept stale IDs, and new messages reusing them were dropped as
// duplicates.
func TestCheckSessionPresent_ClearsReceivedQoS2(t *testing.T) {
	store := &qos2ClearRecorder{}
	opts := defaultOptions("tcp://localhost:1883")
	opts.SessionStore = store
	c := newTestClient(opts)
	c.receivedQoS2[5] = struct{}{}

	if err := c.checkSessionPresent(false); err != nil {
		t.Fatalf("checkSessionPresent: %v", err)
	}

	if !store.cleared {
		t.Error("ClearReceivedQoS2 was not called")
	}
	if len(store.deletedID) != 0 {
		t.Errorf("DeleteReceivedQoS2 called with %v, want no calls", store.deletedID)
	}
	if len(c.receivedQoS2) != 0 {
		t.Errorf("in-memory QoS 2 IDs not cleared: %v", c.receivedQoS2)
	}
}

// TestLoadSessionState_RestoredHandlerIsWrapped checks that a subscription
// restored from the store and joined to its WithSubscription handler still
// runs through the handler interceptors.
func TestLoadSessionState_RestoredHandlerIsWrapped(t *testing.T) {
	store := newMockSubscriptionStore()
	store.storedSubs["a/b"] = &PersistedSubscription{QoS: 1}

	var intercepted, handled atomic.Int32
	opts := defaultOptions("tcp://localhost:1883")
	opts.SessionStore = store
	WithSubscription("a/b", func(*Client, Message) { handled.Add(1) })(opts)
	WithHandlerInterceptor(func(next MessageHandler) MessageHandler {
		return func(c *Client, m Message) {
			intercepted.Add(1)
			next(c, m)
		}
	})(opts)

	c := newTestClient(opts)
	if err := c.loadSessionState(); err != nil {
		t.Fatalf("loadSessionState: %v", err)
	}

	handlers := c.matchHandlers("a/b")
	if len(handlers) != 1 {
		t.Fatalf("got %d handlers for a/b, want 1", len(handlers))
	}
	handlers[0](c, Message{Topic: "a/b"})

	if handled.Load() != 1 || intercepted.Load() != 1 {
		t.Errorf("handled=%d intercepted=%d, want 1 and 1", handled.Load(), intercepted.Load())
	}
}

// checkingStore records pending publishes and fails the test when a call
// overlaps another one or runs while sessionLock is held.
type checkingStore struct {
	SessionStore // other methods are not used
	t            *testing.T
	c            *Client
	busy         atomic.Bool
	mu           sync.Mutex
	pending      map[uint16]bool
	qos2         map[uint16]bool
}

func (s *checkingStore) check() func() {
	if !s.busy.CompareAndSwap(false, true) {
		s.t.Error("store calls overlap")
	}
	locked := make(chan struct{})
	go func() {
		s.c.sessionLock.Lock()
		s.c.sessionLock.Unlock()
		close(locked)
	}()
	select {
	case <-locked:
	case <-time.After(time.Second):
		s.t.Error("store called with sessionLock held")
	}
	return func() { s.busy.Store(false) }
}

func (s *checkingStore) has(id uint16) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.pending[id]
}

func (s *checkingStore) SavePendingPublish(id uint16, _ *PersistedPublish) error {
	defer s.check()()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pending[id] = true
	return nil
}

func (s *checkingStore) DeletePendingPublish(id uint16) error {
	defer s.check()()
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.pending, id)
	return nil
}

// TestSessionStoreWritesSerializedOutsideLock publishes from several
// goroutines while the logic loop handles the PUBACKs, and checks that store
// calls never overlap, never run under sessionLock, persist each publish
// before it reaches outgoing, and keep Save and Delete in order.
func TestSessionStoreWritesSerializedOutsideLock(t *testing.T) {
	opts := defaultOptions("tcp://x")
	opts.Logger = testLogger()
	c := newTestClient(opts)
	c.connState.Store(&connectionState{caps: extractServerCapabilities(nil)})
	store := &checkingStore{t: t, c: c, pending: map[uint16]bool{}}
	c.opts.SessionStore = store

	c.wg.Add(1)
	go c.logicLoop()
	defer func() { close(c.stop); c.wg.Wait() }()

	const publishers, each = 4, 25
	for range publishers {
		go func() {
			for range each {
				c.internalPublish(&publishRequest{ctx: context.Background(), packet: &packets.PublishPacket{Topic: "t", QoS: 1}, token: newToken()})
			}
		}()
	}

	// Act as the write loop and the broker: every publish is acked.
	for range publishers * each {
		var pub *packets.PublishPacket
		select {
		case p := <-c.outgoing:
			pub = p.(*packets.PublishPacket)
		case <-time.After(5 * time.Second):
			t.Fatal("publish never reached outgoing")
		}
		if !store.has(pub.PacketID) {
			t.Errorf("publish %d reached outgoing before it was stored", pub.PacketID)
		}
		c.incoming <- &packets.PubackPacket{PacketID: pub.PacketID}
	}

	deadline := time.Now().Add(5 * time.Second)
	for {
		c.flushStore()
		store.mu.Lock()
		n := len(store.pending)
		store.mu.Unlock()
		if n == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("%d publishes still stored after all were acked", n)
		}
		time.Sleep(time.Millisecond)
	}
}

func (s *checkingStore) SaveReceivedQoS2(id uint16) error {
	defer s.check()()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.qos2[id] = true
	return nil
}

// TestQoS2IDStoredBeforeHandlerRuns checks that a received QoS 2 packet ID is
// on disk before any handler sees the message: after a crash the stored ID is
// what stops the broker's resend from being delivered a second time.
func TestQoS2IDStoredBeforeHandlerRuns(t *testing.T) {
	c := newTestClient(defaultOptions("tcp://x"))
	store := &checkingStore{t: t, c: c, qos2: map[uint16]bool{}}
	c.opts.SessionStore = store

	stored := make(chan bool, 1)
	c.addSubscriptionLocked("t", subscriptionEntry{handler: func(*Client, Message) {
		store.mu.Lock()
		stored <- store.qos2[9]
		store.mu.Unlock()
	}})
	c.handleIncoming(&packets.PublishPacket{Topic: "t", QoS: 2, PacketID: 9})

	select {
	case ok := <-stored:
		if !ok {
			t.Error("handler ran before the QoS 2 packet ID was stored")
		}
	case <-time.After(time.Second):
		t.Fatal("handler never ran")
	}
}
