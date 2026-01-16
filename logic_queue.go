package mq

func (c *Client) processPublishQueue() {
	if len(c.publishQueue) == 0 {
		return
	}

	// Check current in-flight count
	if c.serverCaps.ReceiveMaximum > 0 {
		// Process queue while we have capacity
		for len(c.publishQueue) > 0 && c.inFlightCount < int(c.serverCaps.ReceiveMaximum) {
			// Peek from queue
			req := c.publishQueue[0]

			// Try to send
			if !c.sendPublishLocked(req) {
				// Failed to send (queue full), stop processing
				return
			}

			// Success, remove from queue
			c.publishQueue = c.publishQueue[1:]
		}
	} else {
		// No limit? Flush everything.
		for len(c.publishQueue) > 0 {
			req := c.publishQueue[0]

			if !c.sendPublishLocked(req) {
				return
			}

			c.publishQueue = c.publishQueue[1:]
		}
	}
}
