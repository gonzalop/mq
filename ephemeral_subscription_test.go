package mq

import (
	"testing"

	"github.com/gonzalop/mq/internal/packets"
)

// MockPersistenceStore implements SessionStore interface for testing ephemeral subs
type MockPersistenceStore struct {
	SavedSubs map[string]*PersistedSubscription
}

func (m *MockPersistenceStore) SavePendingPublish(packetID uint16, pub *PersistedPublish) error {
	return nil
}
func (m *MockPersistenceStore) DeletePendingPublish(packetID uint16) error { return nil }
func (m *MockPersistenceStore) LoadPendingPublishes() (map[uint16]*PersistedPublish, error) {
	return nil, nil
}
func (m *MockPersistenceStore) ClearPendingPublishes() error { return nil }
func (m *MockPersistenceStore) SaveSubscription(topic string, sub *PersistedSubscription) error {
	if m.SavedSubs == nil {
		m.SavedSubs = make(map[string]*PersistedSubscription)
	}
	m.SavedSubs[topic] = sub
	return nil
}
func (m *MockPersistenceStore) DeleteSubscription(topic string) error {
	delete(m.SavedSubs, topic)
	return nil
}
func (m *MockPersistenceStore) LoadSubscriptions() (map[string]*PersistedSubscription, error) {
	return nil, nil
}
func (m *MockPersistenceStore) SaveReceivedQoS2(packetID uint16) error         { return nil }
func (m *MockPersistenceStore) DeleteReceivedQoS2(packetID uint16) error       { return nil }
func (m *MockPersistenceStore) LoadReceivedQoS2() (map[uint16]struct{}, error) { return nil, nil }
func (m *MockPersistenceStore) ClearReceivedQoS2() error                       { return nil }
func (m *MockPersistenceStore) Clear() error                                   { return nil }

func TestEphemeralSubscription(t *testing.T) {
	store := &MockPersistenceStore{}

	// Setup client with mock store
	c := &Client{
		opts:          defaultOptions("tcp://test:1883"),
		subscriptions: make(map[string]subscriptionEntry),
		pending:       make(map[uint16]*pendingOp),
		outgoing:      make(chan packets.Packet, 10),
		stop:          make(chan struct{}),
	}
	c.opts.SessionStore = store

	// 1. Subscribe with Persistence=false (Ephemeral)
	ephemeralTopic := "topic/ephemeral"
	reqEphemeral := &subscribeRequest{
		packet: &packets.SubscribePacket{
			Topics:   []string{ephemeralTopic},
			PacketID: 1,
		},
		persistence: false, // Explicitly false via option
		token:       newToken(),
	}

	// Register locally via logic loop handler simulation
	c.internalSubscribe(reqEphemeral)

	// Simulate SUBACK for ephemeral
	subackEphemeral := &packets.SubackPacket{
		PacketID:    1,
		ReturnCodes: []uint8{0},
	}

	// Create pending op for suback handling
	c.pending = make(map[uint16]*pendingOp)
	c.pending[1] = &pendingOp{
		packet: reqEphemeral.packet,
		token:  reqEphemeral.token,
	}

	c.handleSuback(subackEphemeral)

	if _, ok := store.SavedSubs[ephemeralTopic]; ok {
		t.Errorf("Ephemeral topic %q was saved to store, but should not have been", ephemeralTopic)
	}

	// 2. Subscribe with Persistence=true (Default)
	persistentTopic := "topic/persistent"
	reqPersistent := &subscribeRequest{
		packet: &packets.SubscribePacket{
			Topics:   []string{persistentTopic},
			PacketID: 2,
		},
		persistence: true, // Default true
		token:       newToken(),
	}

	// Register locally
	c.internalSubscribe(reqPersistent)

	// Simulate SUBACK for persistent
	subackPersistent := &packets.SubackPacket{
		PacketID:    2,
		ReturnCodes: []uint8{0},
	}

	c.pending[2] = &pendingOp{
		packet: reqPersistent.packet,
		token:  reqPersistent.token,
	}

	c.handleSuback(subackPersistent)

	if _, ok := store.SavedSubs[persistentTopic]; !ok {
		t.Errorf("Persistent topic %q was NOT saved to store", persistentTopic)
	}
}
