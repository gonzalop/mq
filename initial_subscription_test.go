package mq

import (
	"testing"
)

// MockSessionStore implements SessionStore interface for testing
type MockSessionStore struct {
	storedSubs map[string]*SubscriptionInfo
}

func NewMockSessionStore() *MockSessionStore {
	return &MockSessionStore{
		storedSubs: make(map[string]*SubscriptionInfo),
	}
}

func (m *MockSessionStore) SavePendingPublish(packetID uint16, pub *PublishPacket) error { return nil }
func (m *MockSessionStore) DeletePendingPublish(packetID uint16) error                   { return nil }
func (m *MockSessionStore) LoadPendingPublishes() (map[uint16]*PublishPacket, error)     { return nil, nil }
func (m *MockSessionStore) ClearPendingPublishes() error                                 { return nil }
func (m *MockSessionStore) SaveSubscription(topic string, sub *SubscriptionInfo) error {
	m.storedSubs[topic] = sub
	return nil
}
func (m *MockSessionStore) DeleteSubscription(topic string) error {
	delete(m.storedSubs, topic)
	return nil
}
func (m *MockSessionStore) LoadSubscriptions() (map[string]*SubscriptionInfo, error) {
	// Return copy to avoid races in test
	result := make(map[string]*SubscriptionInfo)
	for k, v := range m.storedSubs {
		result[k] = v
	}
	return result, nil
}
func (m *MockSessionStore) SaveReceivedQoS2(packetID uint16) error         { return nil }
func (m *MockSessionStore) DeleteReceivedQoS2(packetID uint16) error       { return nil }
func (m *MockSessionStore) LoadReceivedQoS2() (map[uint16]struct{}, error) { return nil, nil }
func (m *MockSessionStore) ClearReceivedQoS2() error                       { return nil }
func (m *MockSessionStore) Clear() error                                   { return nil }

func TestInitialSubscriptionsPersistence(t *testing.T) {
	// Setup mock store (empty)
	store := NewMockSessionStore()

	// Topic to test
	topic := "initial/topic"

	// Create client options
	opts := defaultOptions("tcp://localhost:1883")
	opts.CleanSession = false
	opts.SessionStore = store
	opts.InitialSubscriptions = map[string]MessageHandler{
		topic: func(c *Client, msg Message) {},
	}

	// Initialize client (partial initialization just to test loadSessionState)
	c := &Client{
		opts:          opts,
		subscriptions: make(map[string]subscriptionEntry),
	}

	// Manually populate c.subscriptions as Dial would do before loadSessionState
	c.subscriptions[topic] = subscriptionEntry{
		handler: opts.InitialSubscriptions[topic],
		qos:     0,
	}

	// Run loadSessionState
	// This should merge store state with initial state, but currently (suspected) wipes initial state
	if err := c.loadSessionState(); err != nil {
		t.Fatalf("loadSessionState failed: %v", err)
	}

	// Check if initial subscription is preserved
	if _, ok := c.subscriptions[topic]; !ok {
		t.Errorf("Initial subscription %q was lost after loadSessionState with empty store!", topic)
	} else {
		t.Logf("Initial subscription %q preserved.", topic)
	}
}
