package mq

import "github.com/gonzalop/mq/internal/packets"

func (c *Client) processPublishQueueLocked() []packets.Packet {
	var toSend []packets.Packet
	if len(c.publishQueue) == 0 {
		return nil
	}

	state := c.connState.Load()
	if state == nil {
		return nil
	}

	limit := 0
	if state.caps.ReceiveMaximum > 0 {
		limit = int(state.caps.ReceiveMaximum)
	}

	// Process queue
	for len(c.publishQueue) > 0 {
		if limit > 0 && c.inFlightCount >= limit {
			break
		}

		// Peek from queue
		req := c.publishQueue[0]

		// Try to prepare
		pkt, ok := c.preparePublishLocked(req)
		if !ok {
			break
		}

		// We must check if the outgoing channel is full to prevent discarding
		// the message from the queue and putting it in pending state too early.
		// If capacity is 0 (unbuffered), we can't reliably check len vs cap without blocking,
		// but outgoing should always be buffered (default is non-zero).
		if cap(c.outgoing) > 0 && len(c.outgoing) == cap(c.outgoing) {
			c.removePending(pkt.PacketID)
			if pkt.QoS > 0 {
				c.inFlightCount--
			}
			break
		}

		// Success, remove from queue
		c.publishQueue = c.publishQueue[1:]
		toSend = append(toSend, pkt)
	}
	return toSend
}
