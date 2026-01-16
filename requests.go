package mq

import (
	"fmt"
	"io"
	"time"
)

// internalPublish processes a publish request synchronously with locking.
func (c *Client) internalPublish(req *publishRequest) {
	pkt := req.packet

	c.sessionLock.Lock()

	// Validate packet size against server's maximum (fail-fast)
	if c.serverCaps.MaximumPacketSize > 0 {
		n, _ := pkt.WriteTo(io.Discard)
		packetSize := uint32(n)

		if packetSize > c.serverCaps.MaximumPacketSize {
			req.token.complete(fmt.Errorf("packet size %d bytes exceeds server maximum %d bytes",
				packetSize, c.serverCaps.MaximumPacketSize))
			c.sessionLock.Unlock()
			return
		}
	}

	// Enforce RetainAvailable validation (fail-fast)
	if pkt.Retain && !c.serverCaps.RetainAvailable {
		req.token.complete(fmt.Errorf("server does not support retained messages"))
		c.sessionLock.Unlock()
		return
	}

	// Enforce MaximumQoS validation (fail-fast)
	if pkt.QoS > c.serverCaps.MaximumQoS {
		req.token.complete(fmt.Errorf("qos %d exceeds server maximum %d",
			pkt.QoS, c.serverCaps.MaximumQoS))
		c.sessionLock.Unlock()
		return
	}

	if pkt.QoS == 0 {
		c.sessionLock.Unlock()
		select {
		case c.outgoing <- pkt:
			req.token.complete(nil)
		case <-c.stop:
			req.token.complete(fmt.Errorf("client stopped"))
		}
		return
	}

	// Flow control for QoS > 0
	if c.serverCaps.ReceiveMaximum > 0 {
		if c.inFlightCount >= int(c.serverCaps.ReceiveMaximum) {
			c.publishQueue = append(c.publishQueue, req)
			c.sessionLock.Unlock()
			return
		}
	}

	pkt.PacketID = c.nextID()

	c.pending[pkt.PacketID] = &pendingOp{
		packet:    pkt,
		token:     req.token,
		qos:       pkt.QoS,
		timestamp: time.Now(),
	}

	if pkt.QoS > 0 {
		c.inFlightCount++
	}

	if c.opts.SessionStore != nil && pkt.QoS > 0 {
		pub := c.convertToPublishPacket(req)
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

// helper for sending - assumes lock is HELD
// Returns true if sent, false if queue full or stopped
func (c *Client) sendPublishLocked(req *publishRequest) bool {
	pkt := req.packet

	pkt.PacketID = c.nextID()

	c.pending[pkt.PacketID] = &pendingOp{
		packet:    pkt,
		token:     req.token,
		qos:       pkt.QoS,
		timestamp: time.Now(),
	}

	select {
	case c.outgoing <- pkt:
		if pkt.QoS > 0 {
			c.inFlightCount++
		}

		if c.opts.SessionStore != nil && pkt.QoS > 0 {
			pub := c.convertToPublishPacket(req)
			if err := c.opts.SessionStore.SavePendingPublish(pkt.PacketID, pub); err != nil {
				c.opts.Logger.Warn("failed to persist publish", "packet_id", pkt.PacketID, "error", err)
			}
		}
		return true

	case <-c.stop:
		// Client stopped, treat as "not sent" but also won't be retried successfully
		return false

	default:
		// Channel full, back off
		// Remove from pending since we failed to send
		delete(c.pending, pkt.PacketID)
		return false
	}
}

// internalSubscribe processes a subscribe request synchronously with locking.
func (c *Client) internalSubscribe(req *subscribeRequest) {
	pkt := req.packet

	c.sessionLock.Lock()

	pkt.PacketID = c.nextID()

	c.pending[pkt.PacketID] = &pendingOp{
		packet:    pkt,
		token:     req.token,
		timestamp: time.Now(),
	}

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
		}

		qos := uint8(0)
		if i < len(pkt.QoS) {
			qos = pkt.QoS[i]
		}

		c.subscriptions[topic] = subscriptionEntry{
			handler: req.handler,
			options: subOpts,
			qos:     qos,
		}
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

	pkt.PacketID = c.nextID()

	c.pending[pkt.PacketID] = &pendingOp{
		packet:    pkt,
		token:     req.token,
		timestamp: time.Now(),
	}

	for _, topic := range req.topics {
		delete(c.subscriptions, topic)
	}

	c.sessionLock.Unlock()
	select {
	case c.outgoing <- pkt:
	case <-c.stop:
		req.token.complete(fmt.Errorf("client stopped"))
	}
}
