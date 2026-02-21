package mq

import (
	"context"
	"time"

	"github.com/gonzalop/mq/internal/packets"
)

// logicLoop is the single-threaded state machine that manages all client state.
// This avoids the need for mutexes on the pending and subscriptions maps.
func (c *Client) logicLoop() {
	defer c.wg.Done()

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
			c.publishQueue = nil
			c.sessionLock.Unlock()
			return
		}
	}
}

// internalResetState resets session state (e.g. on clean session reconnect).
// It acquires the session lock.
func (c *Client) internalResetState() {
	c.sessionLock.Lock()
	defer c.sessionLock.Unlock()
	c.receivedQoS2 = make(map[uint16]struct{})
}

// handleIncoming processes incoming packets from the server.
func (c *Client) handleIncoming(pkt packets.Packet) {
	switch p := pkt.(type) {
	case *packets.PublishPacket:
		c.handlePublish(p)

	case *packets.PubackPacket:
		c.handlePuback(p)

	case *packets.PubrecPacket:
		c.handlePubrec(p)

	case *packets.PubrelPacket:
		c.handlePubrel(p)

	case *packets.PubcompPacket:
		c.handlePubcomp(p)

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
	// Handle topic alias if present (MQTT v5.0 only)
	if c.opts.ProtocolVersion >= ProtocolV50 && p.Properties != nil && p.Properties.Presence&packets.PresTopicAlias != 0 {
		aliasID := p.Properties.TopicAlias

		// Validate alias ID
		if aliasID == 0 {
			c.opts.Logger.Error("server sent invalid topic alias 0")
			// Protocol error - disconnect
			if c.opts.ProtocolVersion >= ProtocolV50 {
				_ = c.disconnectWithReason(context.Background(), uint8(ReasonCodeTopicAliasInvalid), nil)
			} else {
				_ = c.Disconnect(context.Background())
			}
			return
		}

		// Check if server violated our declared maximum
		if c.opts.TopicAliasMaximum > 0 && aliasID > c.opts.TopicAliasMaximum {
			c.opts.Logger.Error("server exceeded topic alias maximum",
				"alias", aliasID,
				"max", c.opts.TopicAliasMaximum)
			// Protocol error - disconnect
			if c.opts.ProtocolVersion >= ProtocolV50 {
				_ = c.disconnectWithReason(context.Background(), uint8(ReasonCodeTopicAliasInvalid), nil)
			} else {
				_ = c.Disconnect(context.Background())
			}
			return
		}

		if p.Topic == "" {
			// Alias-only message - resolve to topic
			c.receivedAliasesLock.RLock()
			topic, exists := c.receivedAliases[aliasID]
			c.receivedAliasesLock.RUnlock()

			if !exists {
				c.opts.Logger.Error("server sent unknown topic alias", "alias", aliasID)
				// Protocol error - disconnect
				if c.opts.ProtocolVersion >= ProtocolV50 {
					if err := c.disconnectWithReason(context.Background(), uint8(ReasonCodeMalformedPacket), nil); err != nil {
						c.opts.Logger.Error("failed to disconnect client", "error", err)
					}
				} else {
					_ = c.Disconnect(context.Background())
				}
				return
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
	}

	// Check receive maximum (MQTT v5.0) for QoS 1 and 2
	if c.opts.ProtocolVersion >= ProtocolV50 && p.QoS > 0 {
		if _, exists := c.inboundUnacked[p.PacketID]; !exists {
			// New message. Check if we have capacity.
			limit := c.opts.ReceiveMaximum
			if limit == 0 {
				limit = 65535
			}
			if len(c.inboundUnacked) >= int(limit) {
				if c.opts.ReceiveMaximumPolicy == LimitPolicyStrict {
					c.opts.Logger.Error("receive maximum exceeded", "limit", limit)
					_ = c.disconnectWithReason(context.Background(), uint8(ReasonCodeReceiveMaximumExceed), nil)
					return
				}

				// Ignore policy: log warning once
				if !c.receiveMaxExceededLogged {
					c.opts.Logger.Warn("receive maximum exceeded, ignoring (server is misbehaving)", "limit", limit)
					c.receiveMaxExceededLogged = true
				}
			}
			c.inboundUnacked[p.PacketID] = struct{}{}
		}
	}

	// For QoS 2, check if we've already received this packet
	if p.QoS == 2 {
		if _, exists := c.receivedQoS2[p.PacketID]; exists {
			// Duplicate QoS 2 message - send PUBREC but don't deliver again
			select {
			case c.outgoing <- &packets.PubrecPacket{PacketID: p.PacketID}:
			case <-c.stop:
			default:
			}
			return
		}
		c.receivedQoS2[p.PacketID] = struct{}{}

		// Persist QoS 2 ID
		if c.opts.SessionStore != nil {
			if err := c.opts.SessionStore.SaveReceivedQoS2(p.PacketID); err != nil {
				c.opts.Logger.Warn("failed to persist QoS2 ID", "packet_id", p.PacketID, "error", err)
			}
		}
	}

	// Find matching handlers
	var handlers []MessageHandler
	for filter, entry := range c.subscriptions {
		if MatchTopic(filter, p.Topic) {
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

	msg := Message{
		Topic:      p.Topic,
		Payload:    p.Payload,
		QoS:        QoS(p.QoS),
		Retained:   p.Retain,
		Duplicate:  p.Dup,
		Properties: toPublicProperties(p.Properties),
	}

	// Call handlers in separate goroutines (don't block logicLoop)
	for _, handler := range handlers {
		h := handler // Capture for goroutine
		go h(c, msg)
	}

	switch p.QoS {
	case 1:
		select {
		case c.outgoing <- &packets.PubackPacket{PacketID: p.PacketID}:
			// Successfully queued PUBACK, remove from tracking
			delete(c.inboundUnacked, p.PacketID)
		case <-c.stop:
		default:
			// If we can't send PUBACK right now, it stays in in-flight
			// and will be retried (or handled) when we have capacity.
		}
	case 2:
		select {
		case c.outgoing <- &packets.PubrecPacket{PacketID: p.PacketID}:
		case <-c.stop:
		default:
		}
	}
}

// handlePuback processes a PUBACK packet (QoS 1 acknowledgment).
func (c *Client) handlePuback(p *packets.PubackPacket) {
	if op, ok := c.pending[p.PacketID]; ok {
		var err error
		if c.opts.ProtocolVersion >= ProtocolV50 && p.ReasonCode >= 0x80 {
			err = &MqttError{
				ReasonCode: ReasonCode(p.ReasonCode),
			}
		}
		op.token.complete(err)
		delete(c.pending, p.PacketID)

		if c.opts.SessionStore != nil {
			if err := c.opts.SessionStore.DeletePendingPublish(p.PacketID); err != nil {
				c.opts.Logger.Warn("failed to delete pending publish", "packet_id", p.PacketID, "error", err)
			}
		}

		c.inFlightCount--
		c.processPublishQueue()
	}
}

// handlePubrec processes a PUBREC packet (QoS 2, step 1).
func (c *Client) handlePubrec(p *packets.PubrecPacket) {
	if op, ok := c.pending[p.PacketID]; ok {
		// MQTT v5.0: check for error reason codes
		if c.opts.ProtocolVersion >= ProtocolV50 && p.ReasonCode >= 0x80 {
			op.token.complete(&MqttError{ReasonCode: ReasonCode(p.ReasonCode)})
			delete(c.pending, p.PacketID)
			c.processPublishQueue()
			return
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

// handlePubcomp processes a PUBCOMP packet (QoS 2, step 3).
func (c *Client) handlePubcomp(p *packets.PubcompPacket) {
	if op, ok := c.pending[p.PacketID]; ok {
		var err error
		if c.opts.ProtocolVersion >= ProtocolV50 && p.ReasonCode >= 0x80 {
			err = &MqttError{
				ReasonCode: ReasonCode(p.ReasonCode),
			}
		}
		op.token.complete(err)
		delete(c.pending, p.PacketID)

		if c.opts.SessionStore != nil {
			if err := c.opts.SessionStore.DeletePendingPublish(p.PacketID); err != nil {
				c.opts.Logger.Warn("failed to delete pending publish", "packet_id", p.PacketID, "error", err)
			}
		}

		c.inFlightCount--
		c.processPublishQueue()
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
		delete(c.pending, p.PacketID)
	}
}

// handleUnsuback processes an UNSUBACK packet.
func (c *Client) handleUnsuback(p *packets.UnsubackPacket) {
	if op, ok := c.pending[p.PacketID]; ok {
		var err error
		if c.opts.ProtocolVersion >= ProtocolV50 {
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
		delete(c.pending, p.PacketID)

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

	for _, op := range c.pending {
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
func (c *Client) nextID() uint16 {
	for range 65535 {
		c.nextPacketID++
		if c.nextPacketID == 0 {
			c.nextPacketID = 1
		}
		if _, used := c.pending[c.nextPacketID]; !used {
			return c.nextPacketID
		}
	}
	// This should only happen if we have 65535 pending packets.
	// In that case, we return the next ID anyway as a fallback,
	// though it will cause a collision.
	return c.nextPacketID
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
	ReasonCodeNormalDisconnect:      "Normal disconnect",
	ReasonCodeDisconnectWithWill:    "Disconnect with Will Message",
	ReasonCodeUnspecifiedError:      "Unspecified error",
	ReasonCodeMalformedPacket:       "Malformed Packet",
	ReasonCodeProtocolError:         "Protocol Error",
	ReasonCodeImplementationError:   "Implementation specific error",
	ReasonCodeNotAuthorized:         "Not authorized",
	ReasonCodeServerBusy:            "Server busy",
	ReasonCodeServerShuttingDown:    "Server shutting down",
	ReasonCodeKeepAliveTimeout:      "Keep Alive timeout",
	ReasonCodeSessionTakenOver:      "Session taken over",
	ReasonCodeTopicFilterInvalid:    "Topic Filter invalid",
	ReasonCodeTopicNameInvalid:      "Topic Name invalid",
	ReasonCodeReceiveMaximumExceed:  "Receive Maximum exceeded",
	ReasonCodeTopicAliasInvalid:     "Topic Alias invalid",
	ReasonCodePacketTooLarge:        "Packet too large",
	ReasonCodeMessageRateTooHigh:    "Message rate too high",
	ReasonCodeQuotaExceeded:         "Quota exceeded",
	ReasonCodeAdministrativeAction:  "Administrative action",
	ReasonCodePayloadFormatInvalid:  "Payload format invalid",
	ReasonCodeRetainNotSupported:    "Retain not supported",
	ReasonCodeQoSNotSupported:       "QoS not supported",
	ReasonCodeUseAnotherServer:      "Use another server",
	ReasonCodeServerMoved:           "Server moved",
	ReasonCodeSharedSubNotSupported: "Shared Subscriptions not supported",
	ReasonCodeConnectionRateExceed:  "Connection rate exceeded",
	ReasonCodeMaximumConnectTime:    "Maximum connect time",
	ReasonCodeSubscriptionIDNotSupp: "Subscription Identifiers not supported",
	ReasonCodeWildcardSubNotSupp:    "Wildcard Subscriptions not supported",
}
