package mq

import (
	"fmt"
	"io"
	"time"

	"github.com/gonzalop/mq/internal/packets"
)

// internalPublish processes a publish request synchronously with locking.
func (c *Client) internalPublish(req *publishRequest) {
	pkt := req.packet

	c.sessionLock.Lock()

	state := c.connState.Load()
	var caps serverCapabilities
	if state != nil {
		caps = state.caps
	} else {
		caps = extractServerCapabilities(nil)
	}

	// Validate packet size against server's maximum (fail-fast)
	if caps.MaximumPacketSize > 0 {
		n, _ := pkt.WriteTo(io.Discard)
		packetSize := uint32(n)

		if packetSize > caps.MaximumPacketSize {
			req.token.complete(fmt.Errorf("packet size %d bytes exceeds server maximum %d bytes",
				packetSize, caps.MaximumPacketSize))
			c.sessionLock.Unlock()
			return
		}
	}

	// Enforce RetainAvailable validation (fail-fast)
	if pkt.Retain && !caps.RetainAvailable {
		req.token.complete(ErrServerNoRetain)
		c.sessionLock.Unlock()
		return
	}

	// Enforce MaximumQoS validation (fail-fast)
	if pkt.QoS > caps.MaximumQoS {
		req.token.complete(fmt.Errorf("%w: requested QoS %d, server maximum is %d",
			ErrQoSExceedsServerMax, pkt.QoS, caps.MaximumQoS))
		c.sessionLock.Unlock()
		return
	}

	if pkt.QoS == 0 {
		c.sessionLock.Unlock()
		if c.opts.QoS0Policy == QoS0LimitPolicyBlock {
			select {
			case c.outgoing <- pkt:
				req.token.complete(nil)
			case <-c.stop:
				req.token.complete(ErrClientDisconnected)
			}
			return
		}

		// Default Drop behavior
		select {
		case c.outgoing <- pkt:
			req.token.complete(nil)
		case <-c.stop:
			req.token.complete(ErrClientDisconnected)
		default:
			// Channel full, drop QoS 0 message (at most once)
			req.token.dropped = true
			req.token.complete(nil)
		}
		return
	}

	// Flow control for QoS > 0
	if state != nil && state.caps.ReceiveMaximum > 0 {
		if c.inFlightCount >= int(state.caps.ReceiveMaximum) {
			c.publishQueue = append(c.publishQueue, req)
			c.sessionLock.Unlock()
			return
		}
	}

	pkt.PacketID = c.nextID()
	if pkt.PacketID == 0 {
		c.sessionLock.Unlock()
		req.token.complete(ErrNoPacketIDsAvailable)
		return
	}

	c.pending[pkt.PacketID] = &pendingOp{
		packet:    pkt,
		token:     req.token,
		qos:       pkt.QoS,
		timestamp: time.Now(),
	}
	c.pendingOrder = append(c.pendingOrder, pkt.PacketID)

	if pkt.QoS > 0 {
		c.inFlightCount++
	}

	if c.opts.SessionStore != nil && pkt.QoS > 0 {
		pub := c.convertToPersistedPublish(req)
		if err := c.opts.SessionStore.SavePendingPublish(pkt.PacketID, pub); err != nil {
			c.opts.Logger.Warn("failed to persist publish", "packet_id", pkt.PacketID, "error", err)
		}
	}

	c.sessionLock.Unlock()
	select {
	case c.outgoing <- pkt:
	case <-c.stop:
		req.token.complete(fmt.Errorf("client stopped"))
	}
}

// preparePublishLocked prepares a publish packet for sending while holding the lock.
// It assigns a packet ID, updates pending state, and performs persistence.
// Returns the prepared packet and true if successful, or nil/false if failed.
// Assumes sessionLock is HELD.
func (c *Client) preparePublishLocked(req *publishRequest) (*packets.PublishPacket, bool) {
	pkt := req.packet

	pkt.PacketID = c.nextID()
	if pkt.PacketID == 0 {
		req.token.complete(ErrNoPacketIDsAvailable)
		return nil, false
	}

	c.pending[pkt.PacketID] = &pendingOp{
		packet:    pkt,
		token:     req.token,
		qos:       pkt.QoS,
		timestamp: time.Now(),
	}
	c.pendingOrder = append(c.pendingOrder, pkt.PacketID)

	if pkt.QoS > 0 {
		c.inFlightCount++
	}

	if c.opts.SessionStore != nil && pkt.QoS > 0 {
		pub := c.convertToPersistedPublish(req)
		if err := c.opts.SessionStore.SavePendingPublish(pkt.PacketID, pub); err != nil {
			c.opts.Logger.Warn("failed to persist publish", "packet_id", pkt.PacketID, "error", err)
		}
	}

	return pkt, true
}

// internalSubscribe processes a subscribe request synchronously with locking.
func (c *Client) internalSubscribe(req *subscribeRequest) {
	pkt := req.packet

	c.sessionLock.Lock()

	// Capability checks for QoS, wildcards, etc. are handled in Subscribe() pre-flight.
	// We still check packet size here because it depends on the final serialized form.
	state := c.connState.Load()
	if state != nil && state.caps.MaximumPacketSize > 0 {
		n, _ := pkt.WriteTo(io.Discard)
		packetSize := uint32(n)
		if packetSize > state.caps.MaximumPacketSize {
			req.token.complete(fmt.Errorf("%w: packet size %d bytes exceeds server maximum %d bytes",
				ErrPacketExceedsServerMax, packetSize, state.caps.MaximumPacketSize))
			c.sessionLock.Unlock()
			return
		}
	}

	pkt.PacketID = c.nextID()
	if pkt.PacketID == 0 {
		c.sessionLock.Unlock()
		req.token.complete(ErrNoPacketIDsAvailable)
		return
	}

	c.pending[pkt.PacketID] = &pendingOp{
		packet:    pkt,
		token:     req.token,
		timestamp: time.Now(),
	}
	c.pendingOrder = append(c.pendingOrder, pkt.PacketID)

	// Register before receiving SUBACK to avoid racing
	// with the server since it might sent messages right away
	// before we get a SUBACK.
	for i, topic := range pkt.Topics {
		var subOpts SubscribeOptions
		subOpts.Persistence = req.persistence

		if pkt.Version >= 5 {
			if i < len(pkt.NoLocal) {
				subOpts.NoLocal = pkt.NoLocal[i]
			}
			if i < len(pkt.RetainAsPublished) {
				subOpts.RetainAsPublished = pkt.RetainAsPublished[i]
			}
			if i < len(pkt.RetainHandling) {
				subOpts.RetainHandling = pkt.RetainHandling[i]
			}

			if pkt.Properties != nil {
				if len(pkt.Properties.SubscriptionIdentifier) > 0 {
					subOpts.SubscriptionID = pkt.Properties.SubscriptionIdentifier[0]
				}
				if len(pkt.Properties.UserProperties) > 0 {
					subOpts.UserProperties = make(map[string]string)
					for _, up := range pkt.Properties.UserProperties {
						subOpts.UserProperties[up.Key] = up.Value
					}
				}
			}
		}

		qos := uint8(0)
		if i < len(pkt.QoS) {
			qos = pkt.QoS[i]
		}

		c.addSubscriptionLocked(topic, subscriptionEntry{
			handler: c.wrapHandler(req.handler),
			options: subOpts,
			qos:     qos,
		})
	}

	c.sessionLock.Unlock()
	select {
	case c.outgoing <- pkt:
	case <-c.stop:
		req.token.complete(fmt.Errorf("client stopped"))
	}
}

// internalUnsubscribe processes an unsubscribe request synchronously with locking.
func (c *Client) internalUnsubscribe(req *unsubscribeRequest) {
	pkt := req.packet

	c.sessionLock.Lock()

	// Validate packet size against server's maximum
	state := c.connState.Load()
	if state != nil && state.caps.MaximumPacketSize > 0 {
		n, _ := pkt.WriteTo(io.Discard)
		packetSize := uint32(n)
		if packetSize > state.caps.MaximumPacketSize {
			req.token.complete(fmt.Errorf("%w: packet size %d bytes exceeds server maximum %d bytes",
				ErrPacketExceedsServerMax, packetSize, state.caps.MaximumPacketSize))
			c.sessionLock.Unlock()
			return
		}
	}

	pkt.PacketID = c.nextID()
	if pkt.PacketID == 0 {
		c.sessionLock.Unlock()
		req.token.complete(ErrNoPacketIDsAvailable)
		return
	}

	c.pending[pkt.PacketID] = &pendingOp{
		packet:    pkt,
		token:     req.token,
		timestamp: time.Now(),
	}
	c.pendingOrder = append(c.pendingOrder, pkt.PacketID)

	c.opts.Logger.Debug("unsubscribing", "packet_id", pkt.PacketID, "topics", req.topics)

	for _, topic := range req.topics {
		c.removeSubscriptionLocked(topic)
	}

	c.sessionLock.Unlock()
	select {
	case c.outgoing <- pkt:
	case <-c.stop:
		req.token.complete(fmt.Errorf("client stopped"))
	}
}
