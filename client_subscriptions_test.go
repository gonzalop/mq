package mq

import (
	"context"
	"io"
	"log/slog"
	"maps"
	"strings"
	"testing"
	"time"

	"github.com/gonzalop/mq/internal/packets"
)

// MockSubscriptionSessionStore implements SessionStore for subscription testing.
type MockSubscriptionSessionStore struct {
	storedSubs map[string]*PersistedSubscription
}

func newMockSubscriptionStore() *MockSubscriptionSessionStore {
	return &MockSubscriptionSessionStore{
		storedSubs: make(map[string]*PersistedSubscription),
	}
}

func (m *MockSubscriptionSessionStore) SavePendingPublish(_ uint16, _ *PersistedPublish) error {
	return nil
}
func (m *MockSubscriptionSessionStore) DeletePendingPublish(_ uint16) error { return nil }
func (m *MockSubscriptionSessionStore) LoadPendingPublishes() (map[uint16]*PersistedPublish, error) {
	return nil, nil
}
func (m *MockSubscriptionSessionStore) ClearPendingPublishes() error { return nil }
func (m *MockSubscriptionSessionStore) SaveSubscription(topic string, sub *PersistedSubscription) error {
	if m.storedSubs == nil {
		m.storedSubs = make(map[string]*PersistedSubscription)
	}
	m.storedSubs[topic] = sub
	return nil
}
func (m *MockSubscriptionSessionStore) DeleteSubscription(topic string) error {
	delete(m.storedSubs, topic)
	return nil
}
func (m *MockSubscriptionSessionStore) LoadSubscriptions() (map[string]*PersistedSubscription, error) {
	result := make(map[string]*PersistedSubscription)
	maps.Copy(result, m.storedSubs)
	return result, nil
}
func (m *MockSubscriptionSessionStore) SaveReceivedQoS2(_ uint16) error   { return nil }
func (m *MockSubscriptionSessionStore) DeleteReceivedQoS2(_ uint16) error { return nil }
func (m *MockSubscriptionSessionStore) LoadReceivedQoS2() (map[uint16]struct{}, error) {
	return nil, nil
}
func (m *MockSubscriptionSessionStore) ClearReceivedQoS2() error { return nil }
func (m *MockSubscriptionSessionStore) Clear() error             { return nil }

func TestSubscribe(t *testing.T) {
	c := &Client{trie: newTopicTrie(),
		opts: &clientOptions{
			ProtocolVersion: ProtocolV50,
			Logger:          testLogger(),
		},
		subscriptions: make(map[string]subscriptionEntry),
		outgoing:      make(chan packets.Packet, 1),
		pending:       make(map[uint16]*pendingOp),
		stop:          make(chan struct{}),
		nextPacketID:  1,
	}
	c.connState.Store(&connectionState{caps: extractServerCapabilities(nil)})

	topic := "test/topic"
	handler := func(_ *Client, _ Message) {}

	// Test successful subscription request
	token := c.Subscribe(topic, 1, handler)

	select {
	case p := <-c.outgoing:
		req, ok := p.(*packets.SubscribePacket)
		if !ok {
			t.Errorf("Expected SubscribePacket, got %T", p)
		}
		if len(req.Topics) != 1 || req.Topics[0] != topic {
			t.Errorf("Request topic mismatch: %v", req.Topics)
		}
		// Verify pending op
		if op, ok := c.pending[req.PacketID]; !ok {
			t.Error("Pending op not found")
		} else if op.token != token {
			t.Error("Token mismatch")
		}
	case <-time.After(time.Second):
		t.Error("Timeout waiting for subscribe packet")
	}

	// Test invalid topic
	token = c.Subscribe("#/invalid", 1, handler)
	select {
	case <-token.Done():
		if token.Error() == nil {
			t.Error("Expected error for invalid topic")
		}
	case <-time.After(time.Second):
		t.Error("Timeout waiting for invalid topic token completion")
	}
}

func TestUnsubscribe(t *testing.T) {
	c := &Client{trie: newTopicTrie(),
		opts: &clientOptions{
			ProtocolVersion: ProtocolV50,
			Logger:          testLogger(),
		},
		subscriptions: make(map[string]subscriptionEntry),
		outgoing:      make(chan packets.Packet, 1),
		pending:       make(map[uint16]*pendingOp),
		stop:          make(chan struct{}),
		nextPacketID:  1,
	}
	c.connState.Store(&connectionState{caps: extractServerCapabilities(nil)})

	topic := "test/topic"

	// Test successful unsubscribe request
	token := c.Unsubscribe(topic)

	select {
	case p := <-c.outgoing:
		req, ok := p.(*packets.UnsubscribePacket)
		if !ok {
			t.Errorf("Expected UnsubscribePacket, got %T", p)
		}
		if len(req.Topics) != 1 || req.Topics[0] != topic {
			t.Errorf("Request topic mismatch: %v", req.Topics)
		}
		// Verify pending op
		if op, ok := c.pending[req.PacketID]; !ok {
			t.Error("Pending op not found")
		} else if op.token != token {
			t.Error("Token mismatch")
		}
	case <-time.After(time.Second):
		t.Error("Timeout waiting for unsubscribe packet")
	}
}

func TestResubscribeAll(t *testing.T) {
	c := &Client{trie: newTopicTrie(),
		opts: &clientOptions{
			ProtocolVersion: ProtocolV50,
			Logger:          testLogger(),
		},
		subscriptions: map[string]subscriptionEntry{
			"topic1": {handler: nil, qos: 1},
		},
		outgoing:     make(chan packets.Packet, 1),
		pending:      make(map[uint16]*pendingOp),
		stop:         make(chan struct{}),
		nextPacketID: 1,
	}
	c.connState.Store(&connectionState{caps: extractServerCapabilities(nil)})

	c.resubscribeAll()

	select {
	case p := <-c.outgoing:
		_, ok := p.(*packets.SubscribePacket)
		if !ok {
			t.Errorf("Expected SubscribePacket, got %T", p)
		}
	case <-time.After(time.Second):
		t.Error("Timeout waiting for resubscribe packet")
	}
}

func TestInternalSubscribe(t *testing.T) {
	c := &Client{trie: newTopicTrie(),
		opts: &clientOptions{
			ProtocolVersion: ProtocolV50,
			Logger:          testLogger(),
		},
		subscriptions: make(map[string]subscriptionEntry),
		pending:       make(map[uint16]*pendingOp),
		outgoing:      make(chan packets.Packet, 10),
		nextPacketID:  1,
	}
	c.connState.Store(&connectionState{caps: extractServerCapabilities(nil)})

	topic := "test/topic"
	handler := func(_ *Client, _ Message) {}

	pkt := &packets.SubscribePacket{
		Topics:  []string{topic},
		QoS:     []uint8{1},
		Version: ProtocolV50,
	}

	token := newToken()
	req := &subscribeRequest{
		packet:  pkt,
		handler: handler,
		token:   token,
	}

	c.internalSubscribe(req)

	select {
	case p := <-c.outgoing:
		sent, ok := p.(*packets.SubscribePacket)
		if !ok {
			t.Errorf("Expected SubscribePacket, got %T", p)
		}
		if op, ok := c.pending[sent.PacketID]; !ok {
			t.Errorf("Pending op not created for PacketID %d", sent.PacketID)
		} else {
			if op.token != token {
				t.Error("Pending op token mismatch")
			}
		}
	case <-time.After(time.Second):
		t.Error("Timeout waiting for outgoing packet")
	}
}

func TestResubscribeBatching(t *testing.T) {
	tests := []struct {
		name            string
		numTopics       int
		expectedBatches int
	}{
		{"no subscriptions", 0, 0},
		{"single topic", 1, 1},
		{"exactly one batch", 100, 1},
		{"two batches", 150, 2},
		{"five batches", 500, 5},
		{"partial last batch", 250, 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &Client{trie: newTopicTrie(),
				subscriptions: make(map[string]subscriptionEntry),
				pending:       make(map[uint16]*pendingOp),
				outgoing:      make(chan packets.Packet, 100),
				opts:          defaultOptions("tcp://test:1883"),
			}
			c.connState.Store(&connectionState{caps: extractServerCapabilities(nil)})

			for i := 0; i < tt.numTopics; i++ {
				topic := "test/topic/" + string(rune('a'+i%26)) + "/" + string(rune('0'+i/26))
				c.subscriptions[topic] = subscriptionEntry{handler: func(*Client, Message) {}, qos: 1}
			}

			c.resubscribeAll()

			actualBatches := len(c.outgoing)
			if actualBatches != tt.expectedBatches {
				t.Errorf("expected %d batches, got %d", tt.expectedBatches, actualBatches)
			}

			totalTopics := 0
			for i := range actualBatches {
				pkt := <-c.outgoing
				subPkt, ok := pkt.(*packets.SubscribePacket)
				if !ok {
					t.Fatalf("expected SubscribePacket, got %T", pkt)
				}

				batchSize := len(subPkt.Topics)
				if i < actualBatches-1 {
					if batchSize != 100 {
						t.Errorf("batch %d: expected 100 topics, got %d", i+1, batchSize)
					}
				} else {
					expectedLast := tt.numTopics % 100
					if expectedLast == 0 && tt.numTopics > 0 {
						expectedLast = 100
					}
					if batchSize != expectedLast {
						t.Errorf("last batch: expected %d topics, got %d", expectedLast, batchSize)
					}
				}
				totalTopics += batchSize
			}

			if totalTopics != tt.numTopics {
				t.Errorf("total topics mismatch: expected %d, got %d", tt.numTopics, totalTopics)
			}
		})
	}
}

func TestSubscribeWithUserProperties(t *testing.T) {
	c := &Client{trie: newTopicTrie(),
		opts: &clientOptions{
			ProtocolVersion: ProtocolV50,
			Logger:          testLogger(),
		},
		subscriptions: make(map[string]subscriptionEntry),
		outgoing:      make(chan packets.Packet, 1),
		pending:       make(map[uint16]*pendingOp),
		stop:          make(chan struct{}),
		nextPacketID:  1,
	}
	c.connState.Store(&connectionState{caps: extractServerCapabilities(nil)})

	topic := "test/topic"
	handler := func(_ *Client, _ Message) {}

	_ = c.Subscribe(topic, 1, handler,
		WithSubscribeUserProperty("key1", "value1"),
		WithSubscribeUserProperty("key2", "value2"),
	)

	select {
	case p := <-c.outgoing:
		req, ok := p.(*packets.SubscribePacket)
		if !ok {
			t.Fatalf("Expected SubscribePacket, got %T", p)
		}

		if req.Properties == nil {
			t.Fatal("Expected Properties in SubscribePacket")
		}

		props := make(map[string]string)
		for _, up := range req.Properties.UserProperties {
			props[up.Key] = up.Value
		}

		if props["key1"] != "value1" {
			t.Errorf("Expected key1=value1, got %s", props["key1"])
		}
		if props["key2"] != "value2" {
			t.Errorf("Expected key2=value2, got %s", props["key2"])
		}

	case <-time.After(time.Second):
		t.Error("Timeout waiting for subscribe packet")
	}
}

func TestEphemeralSubscription(t *testing.T) {
	store := newMockSubscriptionStore()
	c := &Client{trie: newTopicTrie(),
		opts:          defaultOptions("tcp://test:1883"),
		subscriptions: make(map[string]subscriptionEntry),
		pending:       make(map[uint16]*pendingOp),
		outgoing:      make(chan packets.Packet, 10),
		stop:          make(chan struct{}),
	}
	c.opts.SessionStore = store

	ephemeralTopic := "topic/ephemeral"
	reqEphemeral := &subscribeRequest{
		packet: &packets.SubscribePacket{
			Topics:   []string{ephemeralTopic},
			PacketID: 1,
		},
		persistence: false,
		token:       newToken(),
	}

	c.internalSubscribe(reqEphemeral)
	c.pending[1] = &pendingOp{packet: reqEphemeral.packet, token: reqEphemeral.token}
	c.handleSuback(&packets.SubackPacket{PacketID: 1, ReturnCodes: []uint8{0}})

	if _, ok := store.storedSubs[ephemeralTopic]; ok {
		t.Errorf("Ephemeral topic %q was saved to store", ephemeralTopic)
	}

	persistentTopic := "topic/persistent"
	reqPersistent := &subscribeRequest{
		packet: &packets.SubscribePacket{
			Topics:   []string{persistentTopic},
			PacketID: 2,
		},
		persistence: true,
		token:       newToken(),
	}

	c.internalSubscribe(reqPersistent)
	c.pending[2] = &pendingOp{packet: reqPersistent.packet, token: reqPersistent.token}
	c.handleSuback(&packets.SubackPacket{PacketID: 2, ReturnCodes: []uint8{0}})

	if _, ok := store.storedSubs[persistentTopic]; !ok {
		t.Errorf("Persistent topic %q was NOT saved to store", persistentTopic)
	}
}

func TestInitialSubscriptionsPersistence(t *testing.T) {
	store := newMockSubscriptionStore()
	topic := "initial/topic"
	opts := defaultOptions("tcp://localhost:1883")
	opts.CleanSession = false
	opts.SessionStore = store
	opts.InitialSubscriptions = map[string]MessageHandler{
		topic: func(_ *Client, _ Message) {},
	}

	c := &Client{trie: newTopicTrie(),
		opts:          opts,
		subscriptions: make(map[string]subscriptionEntry),
	}
	c.subscriptions[topic] = subscriptionEntry{
		handler: opts.InitialSubscriptions[topic],
		qos:     0,
	}

	if err := c.loadSessionState(); err != nil {
		t.Fatalf("loadSessionState failed: %v", err)
	}

	if _, ok := c.subscriptions[topic]; !ok {
		t.Errorf("Initial subscription %q was lost", topic)
	}
}

func TestSharedSubscriptionNoLocalValidation(t *testing.T) {
	c := &Client{trie: newTopicTrie(),
		opts:          defaultOptions("tcp://localhost:1883"),
		subscriptions: make(map[string]subscriptionEntry),
		pending:       make(map[uint16]*pendingOp),
	}
	c.connState.Store(&connectionState{caps: extractServerCapabilities(nil)})
	c.opts.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))

	tests := []struct {
		name      string
		topic     string
		noLocal   bool
		wantError bool
	}{
		{"shared with NoLocal", "$share/group1/topic", true, true},
		{"shared without NoLocal", "$share/group1/topic", false, false},
		{"normal with NoLocal", "normal/topic", true, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c.outgoing = make(chan packets.Packet, 10)
			token := c.Subscribe(tt.topic, 1, func(*Client, Message) {}, WithNoLocal(tt.noLocal))
			err := token.Error()

			if tt.wantError {
				if err == nil {
					ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
					defer cancel()
					err = token.Wait(ctx)
				}
				if err == nil || !strings.Contains(err.Error(), "protocol error") {
					t.Errorf("expected protocol error for %s, got: %v", tt.name, err)
				}
			} else if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestWithSubscriptionIdentifier(t *testing.T) {
	tests := []struct {
		name           string
		subscriptionID int
		wantError      bool
		errorContains  string
	}{
		{"valid ID", 100, false, ""},
		{"negative ID", -1, true, "must be in range 0-268435455"},
		{"too large ID", 268435456, true, "must be in range 0-268435455"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &Client{trie: newTopicTrie(),
				opts: &clientOptions{
					ProtocolVersion: ProtocolV50,
					Logger:          testLogger(),
				},
				outgoing:      make(chan packets.Packet, 1),
				pending:       make(map[uint16]*pendingOp),
				subscriptions: make(map[string]subscriptionEntry),
				stop:          make(chan struct{}),
			}
			c.connState.Store(&connectionState{caps: extractServerCapabilities(nil)})

			token := c.Subscribe("test/topic", AtLeastOnce, func(*Client, Message) {}, WithSubscriptionIdentifier(tt.subscriptionID))

			if tt.wantError {
				select {
				case <-token.Done():
					err := token.Error()
					if err == nil || (tt.errorContains != "" && !strings.Contains(err.Error(), tt.errorContains)) {
						t.Errorf("expected error containing %q, got %v", tt.errorContains, err)
					}
				default:
					t.Error("expected immediate error")
				}
			} else {
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
				defer cancel()
				_ = token.Wait(ctx)

				select {
				case p := <-c.outgoing:
					req := p.(*packets.SubscribePacket)
					if tt.subscriptionID > 0 && (req.Properties == nil || req.Properties.SubscriptionIdentifier[0] != tt.subscriptionID) {
						t.Error("SubscriptionIdentifier not correctly set in packet")
					}
				default:
					t.Error("expected packet")
				}
			}
		})
	}
}
