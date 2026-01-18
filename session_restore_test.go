package mq

import (
	"testing"
)

// MockSessionStoreForRestore implements SessionStore interface for testing restoration
type MockSessionStoreForRestore struct {
	pendingPublishes map[uint16]*PersistedPublish
}

func (m *MockSessionStoreForRestore) SavePendingPublish(packetID uint16, pub *PersistedPublish) error {
	return nil
}
func (m *MockSessionStoreForRestore) DeletePendingPublish(packetID uint16) error { return nil }
func (m *MockSessionStoreForRestore) LoadPendingPublishes() (map[uint16]*PersistedPublish, error) {
	// Return copy to avoid races in test
	result := make(map[uint16]*PersistedPublish)
	for k, v := range m.pendingPublishes {
		result[k] = v
	}
	return result, nil
}
func (m *MockSessionStoreForRestore) ClearPendingPublishes() error { return nil }
func (m *MockSessionStoreForRestore) SaveSubscription(topic string, sub *PersistedSubscription) error {
	return nil
}
func (m *MockSessionStoreForRestore) DeleteSubscription(topic string) error { return nil }
func (m *MockSessionStoreForRestore) LoadSubscriptions() (map[string]*PersistedSubscription, error) {
	return nil, nil
}
func (m *MockSessionStoreForRestore) SaveReceivedQoS2(packetID uint16) error         { return nil }
func (m *MockSessionStoreForRestore) DeleteReceivedQoS2(packetID uint16) error       { return nil }
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

	c := &Client{
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
