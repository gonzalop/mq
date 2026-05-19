package mq

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/gonzalop/mq/internal/packets"
)

// readLoop continuously reads packets from the network.
func (c *Client) readLoop() {
	defer c.wg.Done()
	defer c.activeLoops.Add(-1)
	defer c.handleDisconnect()

	c.connLock.RLock()
	conn := c.conn
	c.connLock.RUnlock()

	if conn == nil {
		return
	}

	// Wrap connection in buffered reader to reduce syscalls
	cr := &countingReader{Reader: conn, c: c}
	br := bufio.NewReader(cr)

	for {
		pkt, err := packets.ReadPacket(br, c.opts.ProtocolVersion, c.opts.MaxIncomingPacket)
		if err != nil {
			var protoErr *packets.ProtocolError
			if errors.As(err, &protoErr) {
				c.opts.Logger.Error("protocol error, disconnecting", "error", err)
				if c.opts.ProtocolVersion >= ProtocolV50 {
					// Section 4.13: receiver SHOULD send a DISCONNECT with Reason Code 0x82 (Protocol Error)
					_ = c.disconnectWithReason(context.Background(), uint8(ReasonCodeProtocolError), nil, false)
				}
			} else {
				c.opts.Logger.Debug("read error, disconnecting", "error", err)
			}
			return
		}
		c.packetsReceived.Add(1)

		c.opts.Logger.Debug("received packet", "type", packets.PacketNames[pkt.Type()])

		select {
		case c.packetReceived <- struct{}{}:
		default:
		}

		select {
		case c.incoming <- pkt:
		case <-c.stop:
			c.opts.Logger.Debug("readLoop stopped")
			return
		}
	}
}

// writeLoop continuously writes packets to the network and handles keepalive.
func (c *Client) writeLoop() {
	defer c.wg.Done()
	defer c.activeLoops.Add(-1)

	var ticker *time.Ticker
	var tickerCh <-chan time.Time

	if c.opts.KeepAlive > 0 {
		// Ticker runs 4 times per keepalive interval for better resolution
		ticker = time.NewTicker(c.opts.KeepAlive / 4)
		defer ticker.Stop()
		tickerCh = ticker.C
	}

	c.connLock.RLock()
	conn := c.conn
	c.connLock.RUnlock()

	if conn == nil {
		c.opts.Logger.Debug("writeLoop started but not connected")
		return
	}

	cw := &countingWriter{Writer: conn, c: c}
	bw := bufio.NewWriter(cw)
	lastReceived := time.Now()
	lastSent := lastReceived

	for {
		select {
		case pkt := <-c.outgoing:
			c.opts.Logger.Debug("sending packet", "type", packets.PacketNames[pkt.Type()])
			if _, err := pkt.WriteTo(bw); err != nil {
				c.opts.Logger.Debug("write error, disconnecting", "error", err)
				c.handleDisconnect()
				return
			}
			c.packetsSent.Add(1)
			lastSent = time.Now()

			// Batching: try to drain channel to fill buffer
			count := len(c.outgoing)
			for range count {
				pkt := <-c.outgoing
				c.opts.Logger.Debug("sending packet (batch)", "type", packets.PacketNames[pkt.Type()])
				if _, err := pkt.WriteTo(bw); err != nil {
					c.opts.Logger.Debug("write error (batch), disconnecting", "error", err)
					c.handleDisconnect()
					return
				}
				c.packetsSent.Add(1)
				lastSent = time.Now()
			}

			// Flush after batch
			if err := bw.Flush(); err != nil {
				c.opts.Logger.Debug("flush error, disconnecting", "error", err)
				c.handleDisconnect()
				return
			}

		case <-c.packetReceived:
			// Update lastReceived timestamp when any packet arrives
			lastReceived = time.Now()

		case <-c.pingPendingCh:
			// PINGRESP received, clear pending flag
			c.pingPending = false

		case <-tickerCh:
			// Check if we've received anything recently (1.5x keepalive timeout)
			timeout := c.opts.KeepAlive + c.opts.KeepAlive/2 // 1.5x keepalive
			if time.Since(lastReceived) >= timeout {
				c.opts.Logger.Debug("keepalive timeout, no packets received",
					"timeout", timeout,
					"last_received", time.Since(lastReceived))
				c.handleDisconnect()
				return
			}

			// Send PINGREQ if we haven't sent anything for 3/4 of the keepalive interval
			// OR if we haven't received anything for 3/4 of the keepalive interval.
			// This ensures we actively probe the connection even when publishing regularly.
			// Only send if no PINGREQ is currently pending (waiting for PINGRESP).
			threshold := c.opts.KeepAlive - (c.opts.KeepAlive / 4)
			timeSinceSent := time.Since(lastSent)
			timeSinceReceived := time.Since(lastReceived)

			if !c.pingPending && (timeSinceSent >= threshold || timeSinceReceived >= threshold) {
				// Determine reason for PINGREQ
				reason := "no receive"
				if timeSinceSent >= threshold && timeSinceReceived >= threshold {
					reason = "no activity"
				} else if timeSinceSent >= threshold {
					reason = "no send"
				}
				c.opts.Logger.Debug("sending PINGREQ",
					"reason", reason,
					"time_since_sent", timeSinceSent,
					"time_since_received", timeSinceReceived)

				ping := &packets.PingreqPacket{}
				if _, err := ping.WriteTo(bw); err != nil {
					c.handleDisconnect()
					return
				}
				if err := bw.Flush(); err != nil {
					c.handleDisconnect()
					return
				}
				lastSent = time.Now()
				c.pingPending = true
			}

		case <-c.stop:
			c.opts.Logger.Debug("writeLoop stopped")
			return
		}
	}
}

// handleDisconnect handles connection loss.
func (c *Client) handleDisconnect() {
	if !c.connected.Swap(false) {
		return // Already disconnected
	}

	c.connLock.Lock()
	if c.conn != nil {
		c.conn.Close()
		c.conn = nil
	}
	// Check if we have a specific disconnect reason from the server
	reason := fmt.Errorf("connection lost")
	if c.lastDisconnectReason != nil {
		reason = c.lastDisconnectReason
		c.lastDisconnectReason = nil // Clear it after use
	}
	c.connLock.Unlock()

	if c.opts.OnConnectionLost != nil {
		go func() {
			c.opts.OnConnectionLost(c, reason)
		}()
	}

	// Signal reconnect loop
	select {
	case c.disconnected <- struct{}{}:
	default:
	}
}

// reconnectLoop handles automatic reconnection.
func (c *Client) reconnectLoop() {
	defer c.wg.Done()
	defer c.activeLoops.Add(-1)

	backoff := c.opts.ReconnectBackoff
	maxBackoff := c.opts.MaxReconnectBackoff
	if backoff == 0 {
		backoff = time.Second
	}
	if maxBackoff == 0 {
		maxBackoff = 2 * time.Minute
	}

	for {
		select {
		case <-c.disconnected:
			// Wait before reconnecting with interruptible sleep
			select {
			case <-time.After(backoff):
			case <-c.stop:
				return
			}

			c.reconnectCount.Add(1)

			// Attempt to reconnect
			ctx, cancel := context.WithTimeout(context.Background(), c.opts.ConnectTimeout)
			err := c.connect(ctx)
			cancel()

			if err != nil {
				// Exponential backoff
				backoff = min(backoff*2, maxBackoff)

				// Signal disconnected again to retry
				select {
				case c.disconnected <- struct{}{}:
				default:
				}
				continue
			}

			backoff = c.opts.ReconnectBackoff
			if backoff == 0 {
				backoff = time.Second
			}

			if c.opts.CleanSession {
				c.internalResetState()
			}

			c.resubscribeAll()

		case <-c.stop:
			c.opts.Logger.Debug("reconnectLoop stopped")
			return
		}
	}
}

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
