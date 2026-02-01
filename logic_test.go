package mq

import (
	"testing"
	"time"

	"github.com/gonzalop/mq/internal/packets"
)

func TestHandlePubcomp(t *testing.T) {
	// Setup client
	c := &Client{
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

	// Call handlePubcomp
	c.handlePubcomp(pubcomp)

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
	c := &Client{
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

	// Call handlePubcomp
	c.handlePubcomp(pubcomp)

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
}

func (m *MockLogicSessionStore) SavePendingPublish(packetID uint16, pub *PersistedPublish) error {
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
func (m *MockLogicSessionStore) SaveSubscription(topic string, sub *PersistedSubscription) error {
	return nil
}
func (m *MockLogicSessionStore) DeleteSubscription(topic string) error { return nil }
func (m *MockLogicSessionStore) LoadSubscriptions() (map[string]*PersistedSubscription, error) {
	return nil, nil
}
func (m *MockLogicSessionStore) SaveReceivedQoS2(packetID uint16) error   { return nil }
func (m *MockLogicSessionStore) DeleteReceivedQoS2(packetID uint16) error { return nil }
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

	c := &Client{
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

	// Call handlePubcomp
	c.handlePubcomp(pubcomp)

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

	c := &Client{
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

	// Call handlePubcomp
	c.handlePubcomp(pubcomp)

	// Verify operation is removed from pending (error in store shouldn't stop processing)
	if _, ok := c.pending[packetID]; ok {
		t.Error("pending operation should be removed even if store fails")
	}

	// Verify session store called
	if !store.deletePendingPublishCalled {
		t.Error("expected DeletePendingPublish to be called")
	}
}
