package mq

import (
	"context"
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
			c.sessionLock.Lock()
			c.handleIncoming(pkt)
			c.sessionLock.Unlock()

		case <-retryTicker.C:
			c.sessionLock.Lock()
			c.retryPending()
			c.processPublishQueue()
			c.sessionLock.Unlock()

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

// handleIncoming processes incoming packets from the server.
func (c *Client) handleIncoming(pkt packets.Packet) {
	switch p := pkt.(type) {
	case *packets.PublishPacket:
		c.handlePublish(p)

	case *packets.PubackPacket:
		c.handleAck(p.PacketID, p.ReasonCode)

	case *packets.PubrecPacket:
		c.handlePubrec(p)

	case *packets.PubrelPacket:
		c.handlePubrel(p)

	case *packets.PubcompPacket:
		c.handleAck(p.PacketID, p.ReasonCode)

	case *packets.SubackPacket:
		c.handleSuback(p)

	case *packets.UnsubackPacket:
		c.handleUnsuback(p)

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
		c.handleAuth(p)
	}
}

// handlePublish processes an incoming PUBLISH packet.
func (c *Client) handlePublish(p *packets.PublishPacket) {
	// 1. Process Topic Alias (MQTT v5.0)
	if err := c.processTopicAlias(p); err != nil {
		c.opts.Logger.Error("failed to process topic alias", "error", err)
		return
	}

	// 2. Enforce Receive Maximum (MQTT v5.0)
	if err := c.enforceReceiveMaximum(p); err != nil {
		c.opts.Logger.Error("failed to enforce receive maximum", "error", err)
		return
	}

	// 3. Handle QoS 2 Duplicate Detection
	if p.QoS == 2 && c.handleQoS2Duplicate(p.PacketID) {
		return
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
	c.dispatchAndAcknowledge(p, msg, handlers)
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

	if _, exists := c.inboundUnacked[p.PacketID]; !exists {
		// New message. Check if we have capacity.
		limit := c.opts.ReceiveMaximum
		if limit == 0 {
			limit = 65535
		}
		if len(c.inboundUnacked) >= int(limit) {
			if c.opts.ReceiveMaximumPolicy == LimitPolicyStrict {
				c.opts.Logger.Error("receive maximum exceeded", "limit", limit)
				return c.disconnectWithReason(context.Background(), uint8(ReasonCodeReceiveMaximumExceed), nil, false)
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
// Returns true if it's a duplicate (processing should stop).
func (c *Client) handleQoS2Duplicate(packetID uint16) bool {
	if _, exists := c.receivedQoS2[packetID]; exists {
		// Duplicate QoS 2 message - send PUBREC but don't deliver again
		select {
		case c.outgoing <- &packets.PubrecPacket{PacketID: packetID}:
		case <-c.stop:
		default:
		}
		return true
	}
	c.receivedQoS2[packetID] = struct{}{}

	// Persist QoS 2 ID
	if c.opts.SessionStore != nil {
		if err := c.opts.SessionStore.SaveReceivedQoS2(packetID); err != nil {
			c.opts.Logger.Warn("failed to persist QoS2 ID", "packet_id", packetID, "error", err)
		}
	}
	return false
}

// matchHandlers finds all handlers that match the given topic.
func (c *Client) matchHandlers(topic string) []MessageHandler {
	var handlers []MessageHandler
	for filter, entry := range c.subscriptions {
		if MatchTopic(filter, topic) {
			if entry.handler != nil {
				handlers = append(handlers, entry.handler)
			}
		}
	}

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
func (c *Client) dispatchAndAcknowledge(p *packets.PublishPacket, msg Message, handlers []MessageHandler) {
	if len(handlers) == 0 {
		c.sendAck(p)
		return
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

			defer c.recoverPanic("MessageHandler")

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
}

func (c *Client) sendAck(p *packets.PublishPacket) {
	switch p.QoS {
	case 1:
		select {
		case c.outgoing <- &packets.PubackPacket{PacketID: p.PacketID}:
			c.sessionLock.Lock()
			delete(c.inboundUnacked, p.PacketID)
			c.sessionLock.Unlock()
		case <-c.stop:
		default:
			// If we can't send PUBACK right now, it will be retried (or handled)
			// when we have capacity.
		}
	case 2:
		select {
		case c.outgoing <- &packets.PubrecPacket{PacketID: p.PacketID}:
		case <-c.stop:
		default:
		}
	}
}

// handleAck processes a PUBACK or PUBCOMP packet.
func (c *Client) handleAck(packetID uint16, reasonCode uint8) {
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
		c.processPublishQueue()
	}
}

// handlePubrec processes a PUBREC packet (QoS 2, step 1).
func (c *Client) handlePubrec(p *packets.PubrecPacket) {
	if op, ok := c.pending[p.PacketID]; ok {
		if c.opts.ProtocolVersion >= ProtocolV50 {
			op.token.reasonCode = ReasonCode(p.ReasonCode)
			if p.ReasonCode >= 0x80 {
				op.token.complete(&MqttError{ReasonCode: ReasonCode(p.ReasonCode)})
				c.removePending(p.PacketID)
				c.processPublishQueue()
				return
			}
		}

		pubrel := &packets.PubrelPacket{PacketID: p.PacketID, Version: c.opts.ProtocolVersion}
		select {
		case c.outgoing <- pubrel:
			// Update pending operation to track PUBREL for retransmission
			op.packet = pubrel
			op.timestamp = time.Now()
		case <-c.stop:
		default:
		}
	}
}

// handlePubrel processes a PUBREL packet (QoS 2, step 2).
func (c *Client) handlePubrel(p *packets.PubrelPacket) {
	select {
	case c.outgoing <- &packets.PubcompPacket{PacketID: p.PacketID}:
		delete(c.inboundUnacked, p.PacketID)
	case <-c.stop:
	default:
	}

	delete(c.receivedQoS2, p.PacketID)

	if c.opts.SessionStore != nil {
		if err := c.opts.SessionStore.DeleteReceivedQoS2(p.PacketID); err != nil {
			c.opts.Logger.Warn("failed to delete QoS2 ID", "packet_id", p.PacketID, "error", err)
		}
	}
}

// handleSuback processes a SUBACK packet.
func (c *Client) handleSuback(p *packets.SubackPacket) {
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
	}
}

// handleUnsuback processes an UNSUBACK packet.
func (c *Client) handleUnsuback(p *packets.UnsubackPacket) {
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
	}
}

// retryPending retransmits packets that haven't been acknowledged.
func (c *Client) retryPending() {
	now := time.Now()

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

			select {
			case c.outgoing <- op.packet:
				op.timestamp = now
			case <-c.stop:
				return
			default:
				// Outgoing queue is full, skip retransmission for now
				// to avoid blocking the logicLoop.
				return
			}
		}
	}
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
