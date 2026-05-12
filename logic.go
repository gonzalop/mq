package mq

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/gonzalop/mq/internal/packets"
)

// logicLoop is the single-threaded state machine that manages all client state.
// This avoids the need for mutexes on the pending and subscriptions maps.
func (c *Client) logicLoop() {
	defer c.wg.Done()
	defer c.activeLoops.Add(-1)

	retryTicker := time.NewTicker(5 * time.Second)
	defer retryTicker.Stop()

	for {
		select {
		case pkt := <-c.incoming:
			toSend := c.handleIncoming(pkt)
			c.sendPackets(toSend)

		case <-retryTicker.C:
			c.sessionLock.Lock()
			toResend := c.retryPending()
			toResend = append(toResend, c.processPublishQueueLocked()...)
			c.sessionLock.Unlock()
			c.sendPackets(toResend)

		case <-c.stop:
			c.opts.Logger.Debug("logicLoop stopped")
			c.sessionLock.Lock()
			for _, op := range c.pending {
				op.token.complete(ErrClientDisconnected)
			}
			// Complete tokens for queued publish requests
			for _, req := range c.publishQueue {
				req.token.complete(ErrClientDisconnected)
			}
			c.pending = nil
			c.pendingOrder = nil
			c.publishQueue = nil
			c.sessionLock.Unlock()
			return
		}
	}
}

// removePending removes a packet ID from both pending and pendingOrder.
// Assumes sessionLock is HELD.
func (c *Client) removePending(packetID uint16) {
	delete(c.pending, packetID)
	for i, id := range c.pendingOrder {
		if id == packetID {
			c.pendingOrder = append(c.pendingOrder[:i], c.pendingOrder[i+1:]...)
			break
		}
	}
}

// internalResetState resets session state (e.g. on clean session reconnect).
// It acquires the session lock.
func (c *Client) internalResetState() {
	c.sessionLock.Lock()
	defer c.sessionLock.Unlock()
	c.receivedQoS2 = make(map[uint16]struct{})
	c.inboundUnacked = make(map[uint16]struct{})
}

// sendPackets sends a slice of packets to the outgoing channel without holding the lock.
// It uses a non-blocking send to ensure the logicLoop never stalls.
// Retransmissions for QoS 1/2 will be handled by retryPending if they fail here.
func (c *Client) sendPackets(packets []packets.Packet) {
	for _, pkt := range packets {
		select {
		case c.outgoing <- pkt:
		case <-c.stop:
			return
		default:
			c.opts.Logger.Debug("outgoing channel full, dropping packet (will retry if QoS > 0)", "type", fmt.Sprintf("%T", pkt))
		}
	}
}

// handleIncoming processes incoming packets from the server.
// Returns a slice of packets that should be sent in response.
func (c *Client) handleIncoming(pkt packets.Packet) []packets.Packet {
	switch p := pkt.(type) {
	case *packets.PublishPacket:
		return c.handlePublish(p)

	case *packets.PubackPacket:
		return c.handleAck(p.PacketID, p.ReasonCode)

	case *packets.PubrecPacket:
		return c.handlePubrec(p)

	case *packets.PubrelPacket:
		return c.handlePubrel(p)

	case *packets.PubcompPacket:
		return c.handleAck(p.PacketID, p.ReasonCode)

	case *packets.SubackPacket:
		return c.handleSuback(p)

	case *packets.UnsubackPacket:
		return c.handleUnsuback(p)

	case *packets.PingrespPacket:
		// Keepalive response - signal writeLoop that PINGRESP was received
		select {
		case c.pingPendingCh <- struct{}{}:
		default:
			// Channel full, which means writeLoop hasn't processed the previous signal yet
		}

	case *packets.DisconnectPacket:
		c.handleDisconnectPacket(p)

	case *packets.AuthPacket:
		return c.handleAuth(p)
	}
	return nil
}

// handlePublish processes an incoming PUBLISH packet.
func (c *Client) handlePublish(p *packets.PublishPacket) []packets.Packet {
	// 1. Process Topic Alias (MQTT v5.0)
	if err := c.processTopicAlias(p); err != nil {
		c.opts.Logger.Error("failed to process topic alias", "error", err)
		return nil
	}

	// 2. Enforce Receive Maximum (MQTT v5.0)
	if err := c.enforceReceiveMaximum(p); err != nil {
		c.opts.Logger.Error("failed to enforce receive maximum", "error", err)
		// Protocol error: server sent too many QoS 1/2 messages
		return []packets.Packet{
			&packets.DisconnectPacket{
				ReasonCode: uint8(ReasonCodeReceiveMaximumExceed),
			},
		}
	}

	// 3. Handle QoS 2 Duplicate Detection
	if p.QoS == 2 {
		if ack, isDup := c.handleQoS2Duplicate(p.PacketID); isDup {
			return []packets.Packet{ack}
		}
	}

	// 4. Find matching handlers
	handlers := c.matchHandlers(p.Topic)

	msg := Message{
		Topic:      p.Topic,
		Payload:    p.Payload,
		QoS:        QoS(p.QoS),
		Retained:   p.Retain,
		Duplicate:  p.Dup,
		Properties: toPublicProperties(p.Properties),
	}

	// 5. Dispatch to handlers and acknowledge
	return c.dispatchAndAcknowledge(p, msg, handlers)
}

// processTopicAlias handles MQTT v5.0 topic alias validation and resolution.
func (c *Client) processTopicAlias(p *packets.PublishPacket) error {
	if c.opts.ProtocolVersion < ProtocolV50 || p.Properties == nil || p.Properties.Presence&packets.PresTopicAlias == 0 {
		return nil
	}

	aliasID := p.Properties.TopicAlias

	// Validate alias ID
	if aliasID == 0 {
		c.opts.Logger.Error("server sent invalid topic alias 0")
		return c.disconnectWithReason(context.Background(), uint8(ReasonCodeTopicAliasInvalid), nil, false)
	}

	// Check if server violated our declared maximum
	if c.opts.TopicAliasMaximum > 0 && aliasID > c.opts.TopicAliasMaximum {
		c.opts.Logger.Error("server exceeded topic alias maximum", "alias", aliasID, "max", c.opts.TopicAliasMaximum)
		return c.disconnectWithReason(context.Background(), uint8(ReasonCodeTopicAliasInvalid), nil, false)
	}

	if p.Topic == "" {
		// Alias-only message - resolve to topic
		c.receivedAliasesLock.RLock()
		topic, exists := c.receivedAliases[aliasID]
		c.receivedAliasesLock.RUnlock()

		if !exists {
			c.opts.Logger.Error("server sent unknown topic alias", "alias", aliasID)
			return c.disconnectWithReason(context.Background(), uint8(ReasonCodeMalformedPacket), nil, false)
		}

		p.Topic = topic
		c.opts.Logger.Debug("resolved topic alias", "alias", aliasID, "topic", topic)
	} else {
		// Both topic and alias - register the mapping
		c.receivedAliasesLock.Lock()
		c.receivedAliases[aliasID] = p.Topic
		c.receivedAliasesLock.Unlock()
		c.opts.Logger.Debug("registered topic alias", "alias", aliasID, "topic", p.Topic)
	}

	return nil
}

// enforceReceiveMaximum checks if the incoming message exceeds the client's flow control limits.
func (c *Client) enforceReceiveMaximum(p *packets.PublishPacket) error {
	if c.opts.ProtocolVersion < ProtocolV50 || p.QoS == 0 {
		return nil
	}

	c.sessionLock.Lock()
	defer c.sessionLock.Unlock()

	if _, exists := c.inboundUnacked[p.PacketID]; !exists {
		// New message. Check if we have capacity.
		limit := c.opts.ReceiveMaximum
		if limit == 0 {
			limit = 65535
		}
		if len(c.inboundUnacked) >= int(limit) {
			if c.opts.ReceiveMaximumPolicy == LimitPolicyStrict {
				c.opts.Logger.Error("receive maximum exceeded", "limit", limit)
				// Returning error to trigger disconnect in caller
				return fmt.Errorf("receive maximum exceeded: %d", limit)
			}

			// Ignore policy: log warning once
			if !c.receiveMaxExceededLogged {
				c.opts.Logger.Warn("receive maximum exceeded, ignoring (server is misbehaving)", "limit", limit)
				c.receiveMaxExceededLogged = true
			}
		}
		c.inboundUnacked[p.PacketID] = struct{}{}
	}

	return nil
}

// handleQoS2Duplicate checks if a QoS 2 message has already been received.
// Returns (ack, true) if it's a duplicate (processing should stop), or (nil, false) otherwise.
func (c *Client) handleQoS2Duplicate(packetID uint16) (packets.Packet, bool) {
	c.sessionLock.Lock()
	defer c.sessionLock.Unlock()

	if _, exists := c.receivedQoS2[packetID]; exists {
		// Duplicate QoS 2 message - send PUBREC but don't deliver again
		return &packets.PubrecPacket{PacketID: packetID}, true
	}
	c.receivedQoS2[packetID] = struct{}{}

	// Persist QoS 2 ID
	if c.opts.SessionStore != nil {
		if err := c.opts.SessionStore.SaveReceivedQoS2(packetID); err != nil {
			c.opts.Logger.Warn("failed to persist QoS2 ID", "packet_id", packetID, "error", err)
		}
	}
	return nil, false
}

// matchHandlers finds all handlers that match the given topic.
func (c *Client) matchHandlers(topic string) []MessageHandler {
	handlers := c.trie.match(topic)

	// Use default handler if no matches found
	if len(handlers) == 0 {
		if c.defaultHandler != nil {
			handlers = append(handlers, c.defaultHandler)
		} else if c.opts != nil && c.opts.DefaultPublishHandler != nil {
			handlers = append(handlers, c.opts.DefaultPublishHandler)
		}
	}
	return handlers
}

// dispatchAndAcknowledge calls the handlers and sends the appropriate MQTT acknowledgment.
// Returns a slice containing the acknowledgment packet if it should be sent immediately
// (i.e. no handlers), or nil if handlers are processing the message asynchronously.
func (c *Client) dispatchAndAcknowledge(p *packets.PublishPacket, msg Message, handlers []MessageHandler) []packets.Packet {
	if len(handlers) == 0 {
		ack := c.buildAckPacket(p)
		// Clean up state after building ack
		if p.QoS > 0 {
			c.sessionLock.Lock()
			delete(c.inboundUnacked, p.PacketID)
			c.sessionLock.Unlock()
		}
		return []packets.Packet{ack}
	}

	var wg sync.WaitGroup
	wg.Add(len(handlers))

	// Call handlers in separate goroutines
	for _, handler := range handlers {
		h := handler // Capture for goroutine

		go func() {
			defer wg.Done()
			// Acquire semaphore if configured
			if c.handlerSem != nil {
				select {
				case c.handlerSem <- struct{}{}:
					defer func() { <-c.handlerSem }()
				case <-c.stop:
					return
				}
			}

			// Create a context for the handler
			ctx, cancel := context.WithCancel(context.Background())
			if c.opts.HandlerTimeout > 0 {
				ctx, cancel = context.WithTimeout(context.Background(), c.opts.HandlerTimeout)
			}
			defer cancel()

			msg.Context = ctx
			h(c, msg)
		}()
	}

	// Wait for all handlers and then acknowledge
	go func() {
		wg.Wait()
		c.sendAck(p)
	}()

	return nil
}

func (c *Client) buildAckPacket(p *packets.PublishPacket) packets.Packet {
	switch p.QoS {
	case 1:
		return &packets.PubackPacket{PacketID: p.PacketID}
	case 2:
		return &packets.PubrecPacket{PacketID: p.PacketID}
	default:
		return nil
	}
}

func (c *Client) sendAck(p *packets.PublishPacket) {
	ack := c.buildAckPacket(p)
	if ack == nil {
		return
	}

	select {
	case c.outgoing <- ack:
		if p.QoS == 1 {
			c.sessionLock.Lock()
			delete(c.inboundUnacked, p.PacketID)
			c.sessionLock.Unlock()
		}
	case <-c.stop:
	}
}

// handleAck processes a PUBACK or PUBCOMP packet.
func (c *Client) handleAck(packetID uint16, reasonCode uint8) []packets.Packet {
	c.sessionLock.Lock()
	defer c.sessionLock.Unlock()

	if op, ok := c.pending[packetID]; ok {
		var err error
		if c.opts.ProtocolVersion >= ProtocolV50 {
			op.token.reasonCode = ReasonCode(reasonCode)
			if reasonCode >= 0x80 {
				err = &MqttError{
					ReasonCode: ReasonCode(reasonCode),
				}
			}
		}
		op.token.complete(err)
		c.removePending(packetID)

		if c.opts.SessionStore != nil {
			if err := c.opts.SessionStore.DeletePendingPublish(packetID); err != nil {
				c.opts.Logger.Warn("failed to delete pending publish", "packet_id", packetID, "error", err)
			}
		}

		c.inFlightCount--
		return c.processPublishQueueLocked()
	}
	return nil
}

// handlePubrec processes a PUBREC packet (QoS 2, step 1).
// Returns a slice containing the PUBREL packet to be sent in response.
func (c *Client) handlePubrec(p *packets.PubrecPacket) []packets.Packet {
	c.sessionLock.Lock()
	defer c.sessionLock.Unlock()

	if op, ok := c.pending[p.PacketID]; ok {
		if c.opts.ProtocolVersion >= ProtocolV50 {
			op.token.reasonCode = ReasonCode(p.ReasonCode)
			if p.ReasonCode >= 0x80 {
				op.token.complete(&MqttError{ReasonCode: ReasonCode(p.ReasonCode)})
				c.removePending(p.PacketID)
				return c.processPublishQueueLocked()
			}
		}

		pubrel := &packets.PubrelPacket{PacketID: p.PacketID, Version: c.opts.ProtocolVersion}
		// Update pending operation to track PUBREL for retransmission
		op.packet = pubrel
		op.timestamp = time.Now()
		return []packets.Packet{pubrel}
	}
	return nil
}

// handlePubrel processes a PUBREL packet (QoS 2, step 2).
// Returns a slice containing the PUBCOMP packet to be sent in response.
func (c *Client) handlePubrel(p *packets.PubrelPacket) []packets.Packet {
	c.sessionLock.Lock()
	defer c.sessionLock.Unlock()

	delete(c.inboundUnacked, p.PacketID)
	delete(c.receivedQoS2, p.PacketID)

	if c.opts.SessionStore != nil {
		if err := c.opts.SessionStore.DeleteReceivedQoS2(p.PacketID); err != nil {
			c.opts.Logger.Warn("failed to delete QoS2 ID", "packet_id", p.PacketID, "error", err)
		}
	}

	return []packets.Packet{&packets.PubcompPacket{PacketID: p.PacketID}}
}

// handleSuback processes a SUBACK packet.
func (c *Client) handleSuback(p *packets.SubackPacket) []packets.Packet {
	c.sessionLock.Lock()
	defer c.sessionLock.Unlock()

	if op, ok := c.pending[p.PacketID]; ok {
		// Check for subscription failures
		var err error
		for _, code := range p.ReturnCodes {
			if code >= 0x80 {
				if c.opts.ProtocolVersion >= ProtocolV50 {
					err = &MqttError{
						ReasonCode: ReasonCode(code),
						Parent:     ErrSubscriptionFailed,
					}
				} else {
					err = ErrSubscriptionFailed
				}
				break
			}
		}

		// Set reason code from the first return code (Subscribe operates on a single topic)
		if len(p.ReturnCodes) > 0 {
			op.token.reasonCode = ReasonCode(p.ReturnCodes[0])
		}

		// Save subscriptions if successful
		if c.opts.SessionStore != nil && err == nil { // Global error (e.g. timeout) check
			if subPkt, ok := op.packet.(*packets.SubscribePacket); ok {
				for i, topic := range subPkt.Topics {
					// Check individual result code
					success := false
					if i < len(p.ReturnCodes) && p.ReturnCodes[i] < 0x80 {
						success = true
					}

					if success {
						if entry, ok := c.subscriptions[topic]; ok {
							// Only persist if enabled (default is true)
							if entry.options.Persistence {
								sub := c.convertToPersistedSubscription(entry)
								if err := c.opts.SessionStore.SaveSubscription(topic, sub); err != nil {
									c.opts.Logger.Warn("failed to persist subscription", "topic", topic, "error", err)
								}
							}
						}
					}
				}
			}
		}

		op.token.complete(err)
		c.removePending(p.PacketID)
		return c.processPublishQueueLocked()
	}
	return nil
}

// handleUnsuback processes an UNSUBACK packet.
func (c *Client) handleUnsuback(p *packets.UnsubackPacket) []packets.Packet {
	c.sessionLock.Lock()
	defer c.sessionLock.Unlock()

	if op, ok := c.pending[p.PacketID]; ok {
		var err error
		if c.opts.ProtocolVersion >= ProtocolV50 {
			// Set reason code from the first reason code (Unsubscribe operates on a single topic)
			if len(p.ReasonCodes) > 0 {
				op.token.reasonCode = ReasonCode(p.ReasonCodes[0])
			}
			for _, code := range p.ReasonCodes {
				if code >= 0x80 {
					err = &MqttError{
						ReasonCode: ReasonCode(code),
					}
					break
				}
			}
		}
		op.token.complete(err)
		c.removePending(p.PacketID)

		// Delete subscriptions from store
		if c.opts.SessionStore != nil {
			if unsubPkt, ok := op.packet.(*packets.UnsubscribePacket); ok {
				for _, topic := range unsubPkt.Topics {
					if err := c.opts.SessionStore.DeleteSubscription(topic); err != nil {
						c.opts.Logger.Warn("failed to delete subscription", "topic", topic, "error", err)
					}
				}
			}
		}
		return c.processPublishQueueLocked()
	}
	return nil
}

// retryPending retransmits packets that haven't been acknowledged.
// Returns a slice of packets that should be retransmitted.
func (c *Client) retryPending() []packets.Packet {
	now := time.Now()
	var toResend []packets.Packet

	for _, packetID := range c.pendingOrder {
		op, ok := c.pending[packetID]
		if !ok {
			continue
		}

		if now.Sub(op.timestamp) > 10*time.Second {
			// Resend with DUP flag if it's a PUBLISH
			if pub, ok := op.packet.(*packets.PublishPacket); ok {
				pub.Dup = true
			}

			toResend = append(toResend, op.packet)
			op.timestamp = now
		}
	}
	return toResend
}

// nextID generates the next packet ID (1-65535, cycling).
// Returns 0 if all possible packet IDs are currently in use.
func (c *Client) nextID() uint16 {
	if len(c.pending) >= 65535 {
		return 0
	}

	for range 65535 {
		c.nextPacketID++
		if c.nextPacketID == 0 {
			c.nextPacketID = 1
		}
		if _, used := c.pending[c.nextPacketID]; !used {
			return c.nextPacketID
		}
	}
	return 0
}

// handleDisconnectPacket processes a DISCONNECT packet from the server.
func (c *Client) handleDisconnectPacket(p *packets.DisconnectPacket) {
	reason := "Unknown"
	if name, ok := disconnectReasonCodeNames[ReasonCode(p.ReasonCode)]; ok {
		reason = name
	}

	attrs := []any{
		"reason_code", p.ReasonCode,
		"reason", reason,
	}

	if p.Properties != nil && p.Properties.Presence&packets.PresReasonString != 0 {
		attrs = append(attrs, "reason_string", p.Properties.ReasonString)
	}

	c.opts.Logger.Warn("received DISCONNECT from server", attrs...)

	err := &DisconnectError{
		ReasonCode: ReasonCode(p.ReasonCode),
	}

	if p.Properties != nil {
		if p.Properties.Presence&packets.PresReasonString != 0 {
			err.ReasonString = p.Properties.ReasonString
		}
		if p.Properties.Presence&packets.PresSessionExpiryInterval != 0 {
			err.SessionExpiryInterval = p.Properties.SessionExpiryInterval
		}
		if p.Properties.Presence&packets.PresServerReference != 0 {
			err.ServerReference = p.Properties.ServerReference
		}
		if len(p.Properties.UserProperties) > 0 {
			err.UserProperties = make(map[string]string, len(p.Properties.UserProperties))
			for _, up := range p.Properties.UserProperties {
				err.UserProperties[up.Key] = up.Value
			}
		}
	}

	// Store for handleDisconnect to pick up
	c.connLock.Lock()
	c.lastDisconnectReason = err
	c.connLock.Unlock()
}

// disconnectReasonCodeNames maps MQTT v5.0 reason codes to human-readable strings for DISCONNECT packets.
var disconnectReasonCodeNames = map[ReasonCode]string{
	ReasonCodeNormalDisconnect:        "Normal disconnect",
	ReasonCodeDisconnectWithWill:      "Disconnect with Will Message",
	ReasonCodeUnspecifiedError:        "Unspecified error",
	ReasonCodeMalformedPacket:         "Malformed Packet",
	ReasonCodeProtocolError:           "Protocol Error",
	ReasonCodeImplementationError:     "Implementation specific error",
	ReasonCodeUnsupportedProtocol:     "Unsupported Protocol Version",
	ReasonCodeClientIdentifierInvalid: "Client Identifier not valid",
	ReasonCodeBadUsernameOrPassword:   "Bad User Name or Password",
	ReasonCodeNotAuthorized:           "Not authorized",
	ReasonCodeServerMovedConnack:      "Server moved (CONNACK)",
	ReasonCodeServerBusy:              "Server busy",
	ReasonCodeBanned:                  "Banned",
	ReasonCodeServerShuttingDown:      "Server shutting down",
	ReasonCodeBadAuthenticationMethod: "Bad authentication method",
	ReasonCodeKeepAliveTimeout:        "Keep Alive timeout",
	ReasonCodeSessionTakenOver:        "Session taken over",
	ReasonCodeTopicAliasExceeded:      "Topic Alias exceeded",
	ReasonCodeTopicFilterInvalid:      "Topic Filter invalid",
	ReasonCodeTopicNameInvalid:        "Topic Name invalid",
	ReasonCodePacketIdentifierInUse:   "Packet identifier in use",
	ReasonCodeReceiveMaximumExceed:    "Receive Maximum exceeded",
	ReasonCodeTopicAliasInvalid:       "Topic Alias invalid",
	ReasonCodePacketTooLarge:          "Packet too large",
	ReasonCodeMessageRateTooHigh:      "Message rate too high",
	ReasonCodeQuotaExceeded:           "Quota exceeded",
	ReasonCodeAdministrativeAction:    "Administrative action",
	ReasonCodePayloadFormatInvalid:    "Payload format invalid",
	ReasonCodeRetainNotSupported:      "Retain not supported",
	ReasonCodeQoSNotSupported:         "QoS not supported",
	ReasonCodeUseAnotherServer:        "Use another server",
	ReasonCodeServerMoved:             "Server moved",
	ReasonCodeSharedSubNotSupported:   "Shared Subscriptions not supported",
	ReasonCodeConnectionRateExceed:    "Connection rate exceeded",
	ReasonCodeMaximumConnectTime:      "Maximum connect time",
	ReasonCodeSubscriptionIDNotSupp:   "Subscription Identifiers not supported",
	ReasonCodeWildcardSubNotSupp:      "Wildcard Subscriptions not supported",
}
