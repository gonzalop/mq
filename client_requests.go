package mq

import (
	"fmt"
	"io"

	"github.com/gonzalop/mq/internal/packets"
)

// internalPublish processes a publish request synchronously with locking.
func (c *Client) internalPublish(req *publishRequest) {
	if c.stopped() {
		req.token.complete(ErrClientDisconnected)
		return
	}

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
		if pkt.UseAlias {
			// Room for the Topic Alias property the write loop may add.
			packetSize += 4
		}

		if packetSize > caps.MaximumPacketSize {
			req.token.complete(fmt.Errorf("%w: packet size %d bytes exceeds server maximum %d bytes",
				ErrPacketExceedsServerMax, packetSize, caps.MaximumPacketSize))
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
			case <-req.ctx.Done():
				req.token.complete(req.ctx.Err())
			case <-c.stop:
				req.token.complete(ErrClientDisconnected)
			}
			return
		}

		// Default Drop behavior
		select {
		case c.outgoing <- pkt:
			req.token.complete(nil)
		case <-req.ctx.Done():
			req.token.complete(req.ctx.Err())
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

	op := &pendingOp{
		packet: pkt,
		token:  req.token,
		qos:    pkt.QoS,
	}
	op.sending.Store(true)
	c.addPendingLocked(pkt.PacketID, op)

	if pkt.QoS > 0 {
		c.inFlightCount++
	}

	c.savePendingPublishLocked(req)

	c.sessionLock.Unlock()
	// The publish is on disk before it can reach the network.
	c.flushStore()
	select {
	case c.outgoing <- pkt:
		op.sending.Store(false)
	case <-req.ctx.Done():
		c.sessionLock.Lock()
		c.removePending(pkt.PacketID)
		if pkt.QoS > 0 {
			c.inFlightCount--
		}
		c.deletePendingPublishLocked(pkt.PacketID)
		c.sessionLock.Unlock()
		c.flushStore()
		req.token.complete(req.ctx.Err())
	case <-c.stop:
		c.sessionLock.Lock()
		c.removePending(pkt.PacketID)
		if pkt.QoS > 0 {
			c.inFlightCount--
		}
		c.deletePendingPublishLocked(pkt.PacketID)
		c.sessionLock.Unlock()
		c.flushStore()
		req.token.complete(ErrClientDisconnected)
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

	c.addPendingLocked(pkt.PacketID, &pendingOp{
		packet: pkt,
		token:  req.token,
		qos:    pkt.QoS,
	})

	if pkt.QoS > 0 {
		c.inFlightCount++
	}

	c.savePendingPublishLocked(req)

	return pkt, true
}

// internalSubscribe processes a subscribe request synchronously with locking.
func (c *Client) internalSubscribe(req *subscribeRequest) {
	if c.stopped() {
		req.token.complete(ErrClientDisconnected)
		return
	}

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

	op := &pendingOp{
		packet: pkt,
		token:  req.token,
	}
	op.sending.Store(true)
	c.addPendingLocked(pkt.PacketID, op)

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

		if i < len(pkt.QoS) {
			subOpts.QoS = QoS(pkt.QoS[i])
		}

		c.addSubscriptionLocked(topic, subscriptionEntry{
			handler: c.wrapHandler(req.handler),
			options: subOpts,
		})
	}

	c.sessionLock.Unlock()
	select {
	case c.outgoing <- pkt:
		op.sending.Store(false)
	case <-req.ctx.Done():
		c.sessionLock.Lock()
		c.removePending(pkt.PacketID)
		for _, topic := range pkt.Topics {
			c.removeSubscriptionLocked(topic)
		}
		c.sessionLock.Unlock()
		req.token.complete(req.ctx.Err())
	case <-c.stop:
		c.sessionLock.Lock()
		c.removePending(pkt.PacketID)
		for _, topic := range pkt.Topics {
			c.removeSubscriptionLocked(topic)
		}
		c.sessionLock.Unlock()
		req.token.complete(ErrClientDisconnected)
	}
}

// internalUnsubscribe processes an unsubscribe request synchronously with locking.
func (c *Client) internalUnsubscribe(req *unsubscribeRequest) {
	if c.stopped() {
		req.token.complete(ErrClientDisconnected)
		return
	}

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

	op := &pendingOp{
		packet: pkt,
		token:  req.token,
	}
	op.sending.Store(true)
	c.addPendingLocked(pkt.PacketID, op)

	c.opts.Logger.Debug("unsubscribing", "packet_id", pkt.PacketID, "topics", req.topics)
	c.sessionLock.Unlock()

	select {
	case c.outgoing <- pkt:
		op.sending.Store(false)
		c.sessionLock.Lock()
		for _, topic := range req.topics {
			c.removeSubscriptionLocked(topic)
		}
		c.sessionLock.Unlock()
	case <-req.ctx.Done():
		c.sessionLock.Lock()
		c.removePending(pkt.PacketID)
		c.sessionLock.Unlock()
		req.token.complete(req.ctx.Err())
	case <-c.stop:
		c.sessionLock.Lock()
		c.removePending(pkt.PacketID)
		c.sessionLock.Unlock()
		req.token.complete(ErrClientDisconnected)
	}
}

// savePendingPublishLocked queues storing a QoS 1/2 publish.
// Assumes sessionLock is HELD.
func (c *Client) savePendingPublishLocked(req *publishRequest) {
	pkt := req.packet
	if c.opts.SessionStore == nil || pkt.QoS == 0 {
		return
	}
	pub := c.convertToPersistedPublish(req)
	c.queueStoreLocked(func(s SessionStore) error { return s.SavePendingPublish(pkt.PacketID, pub) },
		"failed to persist publish", "packet_id", pkt.PacketID)
}

// deletePendingPublishLocked queues removing a stored publish.
// Assumes sessionLock is HELD.
func (c *Client) deletePendingPublishLocked(packetID uint16) {
	c.queueStoreLocked(func(s SessionStore) error { return s.DeletePendingPublish(packetID) },
		"failed to delete pending publish", "packet_id", packetID)
}
