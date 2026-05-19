package mq

import (
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
				PayloadFormat: uint8PtrForPayload(PayloadFormatBytes),
			},
			wantErr: false,
		},
		{
			name:    "Payload Format UTF-8 (1) - Valid",
			payload: []byte("Hello World"),
			props: &Properties{
				PayloadFormat: uint8PtrForPayload(PayloadFormatUTF8),
			},
			wantErr: false,
		},
		{
			name:    "Payload Format UTF-8 (1) - Invalid",
			payload: []byte{0xFF, 0xFE},
			props: &Properties{
				PayloadFormat: uint8PtrForPayload(PayloadFormatUTF8),
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

func uint8PtrForPayload(v uint8) *uint8 {
	return &v
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

// TestOrderedRetransmission verifies that pending packets are retransmitted
// in the same order they were originally sent.
func TestOrderedRetransmission(t *testing.T) {
	opts := defaultOptions("tcp://localhost:1883")
	c := newTestClient(opts)

	// 1. Manually populate pending packets in a specific order
	// We'll use very old timestamps to trigger immediate retransmission
	oldTime := time.Now().Add(-1 * time.Hour)

	packetIDs := []uint16{10, 5, 20, 1}
	for _, id := range packetIDs {
		c.sessionLock.Lock()
		c.pending[id] = &pendingOp{
			packet:    &packets.PublishPacket{PacketID: id, QoS: 1, Topic: "test"},
			timestamp: oldTime,
		}
		c.pendingOrder = append(c.pendingOrder, id)
		c.sessionLock.Unlock()
	}

	// 2. Run retryPending
	c.sessionLock.Lock()
	toResend := c.retryPending()
	c.sessionLock.Unlock()

	for _, p := range toResend {
		c.outgoing <- p
	}

	// 3. Verify order in outgoing channel
	for _, expectedID := range packetIDs {
		select {
		case pkt := <-c.outgoing:
			if p, ok := pkt.(*packets.PublishPacket); ok {
				if p.PacketID != expectedID {
					t.Errorf("Expected retransmission of %d, got %d", expectedID, p.PacketID)
				}
				if !p.Dup {
					t.Errorf("Expected DUP flag to be set for %d", expectedID)
				}
			} else {
				t.Errorf("Expected PUBLISH, got %T", pkt)
			}
		case <-time.After(100 * time.Millisecond):
			t.Errorf("Packet %d was never retransmitted", expectedID)
		}
	}
}

// TestRemovePending verifies that removePending correctly cleans up both
// the map and the order slice.
func TestRemovePending(t *testing.T) {
	c := &Client{trie: newTopicTrie(),
		pending:      make(map[uint16]*pendingOp),
		pendingOrder: []uint16{1, 2, 3, 4, 5},
	}
	for _, id := range c.pendingOrder {
		c.pending[id] = &pendingOp{}
	}

	// Remove middle element
	c.removePending(3)

	if _, exists := c.pending[3]; exists {
		t.Error("ID 3 still exists in pending map")
	}
	expectedOrder := []uint16{1, 2, 4, 5}
	if len(c.pendingOrder) != len(expectedOrder) {
		t.Errorf("Expected order length %d, got %d", len(expectedOrder), len(c.pendingOrder))
	}
	for i, id := range c.pendingOrder {
		if id != expectedOrder[i] {
			t.Errorf("At index %d, expected %d, got %d", i, expectedOrder[i], id)
		}
	}

	// Remove first element
	c.removePending(1)
	expectedOrder = []uint16{2, 4, 5}
	if len(c.pendingOrder) != len(expectedOrder) {
		t.Errorf("Expected order length %d, got %d", len(expectedOrder), len(c.pendingOrder))
	}

	// Remove last element
	c.removePending(5)
	expectedOrder = []uint16{2, 4}
	if len(c.pendingOrder) != len(expectedOrder) {
		t.Errorf("Expected order length %d, got %d", len(expectedOrder), len(c.pendingOrder))
	}
}
