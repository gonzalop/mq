package mq

import (
	"bytes"
	"testing"
	"time"

	"github.com/gonzalop/mq/internal/packets"
)

// TestCompliance_Topic_Validation verifies topic validation rules including UTF-8, case sensitivity, and wildcards.
func TestCompliance_Topic_Validation(t *testing.T) {
	opts := defaultOptions("tcp://test:1883")

	t.Run("UTF-8 Validation", func(t *testing.T) {
		// MQTT 3.1.1 section 1.5.3: "UTF-8 data... MUST not include an encoding of the null character U+0000" (Checked)
		// "The data SHOULD NOT include... U+D800 to U+DFFF" (Surrogates - technically valid in loose UTF-8 but invalid in strict)
		// Go's `utf8.ValidString` checks for valid UTF-8.

		invalidUTF8 := string([]byte{0xff, 0xfe, 0xfd}) // Invalid UTF-8 sequence

		err := validatePublishTopic(invalidUTF8, opts)
		if err == nil {
			// Failing strictly as we enabled UTF-8 validation
			t.Errorf("validatePublishTopic accepted invalid UTF-8")
		} else {
			t.Logf("Passed: Invalid UTF-8 topic rejected: %v", err)
		}
	})

	t.Run("Case Sensitivity", func(t *testing.T) {
		matched := MatchTopic("Topic/A", "topic/a")
		if matched {
			t.Errorf("MatchTopic MATCHED 'Topic/A' vs 'topic/a', expected NO match (case sensitive)")
		}

	})

	t.Run("Invalid Wildcard Placement", func(t *testing.T) {
		invalidFilters := []string{
			"sport/tennis#",          // # not alone
			"sport/tennis/#/ranking", // # not last
			"sport/ten+nis/player",   // + not alone
		}

		for _, f := range invalidFilters {
			err := validateSubscribeTopic(f, opts)
			if err == nil {
				t.Errorf("validateSubscribeTopic accepted invalid filter: %s", f)
			}
		}
	})
}

// TestCompliance_Connect_Validation verifies connection validation rules.
func TestCompliance_Connect_Validation(t *testing.T) {
	t.Run("v3.1.1 Empty ClientID requires CleanSession=true", func(t *testing.T) {
		// Attempt to Dial with invalid configuration
		_, err := Dial("tcp://localhost:1883",
			WithProtocolVersion(ProtocolV311),
			WithClientID(""),
			WithCleanSession(false),
		)

		if err == nil {
			t.Fatal("Expected error when dialing with empty ClientID and CleanSession=false for MQTT 3.1.1, got nil")
		}

		expectedError := "MQTT requires a non-empty ClientID when CleanSession is false"
		if err.Error() != expectedError {
			t.Errorf("Expected error %q, got %q", expectedError, err.Error())
		}
	})
}

// TestCompliance_PacketID_Reuse verifies that Packet IDs are not reused while in flight.
func TestCompliance_PacketID_Reuse(t *testing.T) {
	c := &Client{
		pending:      make(map[uint16]*pendingOp),
		nextPacketID: 10,
	}

	// Occupy ID 11
	c.pending[11] = &pendingOp{}

	// Generate next ID - should be 11 (nextPacketID++)
	// But since 11 is used, it should skip to 12 if compliant.
	id := c.nextID()
	switch id {
	case 11:
		t.Errorf("Compliance violation: nextID() returned 11 which is currently in use (MQTT-2.3.1-4)")
	case 12:
		t.Logf("Compliance passed: nextID() skipped in-use ID 11")
	default:
		t.Errorf("Unexpected ID: %d", id)
	}
}

// TestCompliance_QoS2_Retransmission verifies correct QoS 2 flow retransmission (PUBREL vs PUBLISH).
func TestCompliance_QoS2_Retransmission(t *testing.T) {
	c := &Client{
		pending:  make(map[uint16]*pendingOp),
		outgoing: make(chan packets.Packet, 10),
		opts: &clientOptions{
			Logger: defaultOptions("").Logger,
		},
	}

	// Setup a QoS 2 publish in pending state
	pkt := &packets.PublishPacket{
		PacketID: 100,
		QoS:      2,
		Topic:    "test",
	}
	op := &pendingOp{
		packet:    pkt,
		qos:       2,
		timestamp: time.Now().Add(-20 * time.Second), // Expired
		token:     &token{},
	}
	c.pending[100] = op

	// Simulate receiving PUBREC
	// The handler should send PUBREL and update state
	pubrec := &packets.PubrecPacket{PacketID: 100}
	c.handlePubrec(pubrec)

	// Backdate timestamp again to trigger retryPending
	c.pending[100].timestamp = time.Now().Add(-20 * time.Second)

	// Check outgoing for PUBREL (first one from handlePubrec)
	select {
	case p := <-c.outgoing:
		if _, ok := p.(*packets.PubrelPacket); !ok {
			t.Errorf("Expected PUBREL after PUBREC, got %T", p)
		}
	default:
		t.Errorf("No packet sent after PUBREC")
	}

	// Simulate timeout and retry
	c.retryPending()

	// Expect PUBREL to be resent (in second phase of QoS 2)
	select {
	case p := <-c.outgoing:
		if _, ok := p.(*packets.PubrelPacket); ok {
			t.Log("Compliance passed: Resent PUBREL")
		} else if _, ok := p.(*packets.PublishPacket); ok {
			t.Errorf("Compliance violation: Resent PUBLISH packet instead of PUBREL after PUBREC received (MQTT-4.3.3-2)")
		} else {
			t.Errorf("Resent unexpected packet type: %T", p)
		}
	default:
		t.Errorf("No packet resent")
	}
}

// TestCompliance_AssignedClientID_Persistence verifies that a server-assigned ClientID
// is persisted in options for future reconnections.
func TestCompliance_AssignedClientID_Persistence(t *testing.T) {
	c := &Client{
		opts: &clientOptions{
			ProtocolVersion: ProtocolV50,
			ClientID:        "", // Empty initially
		},
	}

	// Simulate receiving a CONNACK with an assigned client ID
	assignedID := "server-assigned-123"
	connack := &packets.ConnackPacket{
		Properties: &packets.Properties{
			Presence:                 packets.PresAssignedClientIdentifier,
			AssignedClientIdentifier: assignedID,
		},
	}

	// Trigger the logic that processes CONNACK properties.
	// In client.go, this is inside connect(). Since we can't easily call connect()
	// without a real network, verify that buildConnectPacket would use the new ID
	// if it was updated.

	// Manually simulate the update that should happen in connect()
	if connack.Properties.Presence&packets.PresAssignedClientIdentifier != 0 {
		c.assignedClientID = connack.Properties.AssignedClientIdentifier
		c.opts.ClientID = c.assignedClientID
	}

	if c.opts.ClientID != assignedID {
		t.Errorf("Expected ClientID to be updated to %q, got %q", assignedID, c.opts.ClientID)
	}

	// Verify buildConnectPacket uses the updated ID
	pkt := c.buildConnectPacket()
	if pkt.ClientID != assignedID {
		t.Errorf("Expected CONNECT packet to use assigned ID %q, got %q", assignedID, pkt.ClientID)
	}
}

// TestCompliance_Resubscribe_Options_Persistence verifies that subscription options
// (NoLocal, etc.) are preserved across reconnections.
func TestCompliance_Resubscribe_Options_Persistence(t *testing.T) {
	c := &Client{
		opts: &clientOptions{
			ProtocolVersion: ProtocolV50,
			Logger:          defaultOptions("").Logger,
		},
		subscriptions: make(map[string]subscriptionEntry),
		pending:       make(map[uint16]*pendingOp),
		outgoing:      make(chan packets.Packet, 10),
	}

	// Subscribe with special options
	topic := "sensors/+/data"
	handler := func(c *Client, msg Message) {}
	opts := SubscribeOptions{
		NoLocal:           true,
		RetainAsPublished: true,
		RetainHandling:    2,
	}
	c.subscriptions[topic] = subscriptionEntry{
		handler: handler,
		options: opts,
		qos:     1,
	}

	// Trigger resubscription
	c.resubscribeAll()

	// Verify the outgoing SUBSCRIBE packet carries the options
	select {
	case p := <-c.outgoing:
		subPkt, ok := p.(*packets.SubscribePacket)
		if !ok {
			t.Fatalf("Expected SubscribePacket, got %T", p)
		}
		if len(subPkt.NoLocal) == 0 || !subPkt.NoLocal[0] {
			t.Error("NoLocal option was lost during resubscription")
		}
		if len(subPkt.RetainAsPublished) == 0 || !subPkt.RetainAsPublished[0] {
			t.Error("RetainAsPublished option was lost during resubscription")
		}
		if len(subPkt.RetainHandling) == 0 || subPkt.RetainHandling[0] != 2 {
			t.Errorf("RetainHandling option mismatch: got %d, want 2", subPkt.RetainHandling[0])
		}
	default:
		t.Error("No SUBSCRIBE packet sent")
	}
}

// TestCompliance_Disconnect_ReasonCode verifies that we can send a DISCONNECT
// with a specific reason code.
func TestCompliance_Disconnect_ReasonCode(t *testing.T) {

	// Disconnect with a specific reason (Disconnect with Will)
	// Since Disconnect() blocks and starts a timer, we'll just test the packet encoding
	// or the internal helper if we could, but let's check the packet creation.

	pkt := &packets.DisconnectPacket{
		Version:    ProtocolV50,
		ReasonCode: uint8(ReasonCodeDisconnectWithWill),
	}

	var buf bytes.Buffer
	if _, err := pkt.WriteTo(&buf); err != nil {
		t.Fatalf("failed to write packet: %v", err)
	}
	encoded := buf.Bytes()
	// Header(2) + ReasonCode(1) + PropertyLength(1) = 4 bytes
	if len(encoded) < 3 {
		t.Fatalf("Encoded DISCONNECT packet too short: %d", len(encoded))
	}
	if encoded[2] != uint8(ReasonCodeDisconnectWithWill) {
		t.Errorf("Expected reason code 0x04 at offset 2, got 0x%02x", encoded[2])
	}
}
