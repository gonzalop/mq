package mq

import (
	"testing"

	"github.com/gonzalop/mq/internal/packets"
)

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

	// Message 3 (QoS 1) - Should trigger disconnect!
	c.connected.Store(true)

	pkts := c.handleIncoming(&packets.PublishPacket{Topic: "t", QoS: 1, PacketID: 3})

	// Check if any packet is a DISCONNECT
	disconnected := false
	for _, p := range pkts {
		if dp, ok := p.(*packets.DisconnectPacket); ok && dp.ReasonCode == uint8(ReasonCodeReceiveMaximumExceed) {
			disconnected = true
			c.connected.Store(false) // Simulate what logicLoop would do
			break
		}
	}

	if !disconnected || c.IsConnected() {
		t.Error("client should have disconnected due to receive maximum exceeded")
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

	c.connected.Store(true)

	// Msg 1 (QoS 2)
	c.sendPackets(c.handleIncoming(&packets.PublishPacket{Topic: "t", QoS: 2, PacketID: 10}))
	if len(c.inboundUnacked) != 1 {
		t.Errorf("expected 1 unacked, got %d", len(c.inboundUnacked))
	}

	// Msg 2 (QoS 2) - Should limit
	pkts := c.handleIncoming(&packets.PublishPacket{Topic: "t", QoS: 2, PacketID: 11})

	disconnected := false
	for _, p := range pkts {
		if dp, ok := p.(*packets.DisconnectPacket); ok && dp.ReasonCode == uint8(ReasonCodeReceiveMaximumExceed) {
			disconnected = true
			c.connected.Store(false)
			break
		}
	}

	if !disconnected || c.IsConnected() {
		t.Error("client should have disconnected due to receive maximum exceeded (QoS 2)")
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
}
