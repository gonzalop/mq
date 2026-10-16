package mq

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"net"
	"time"

	"github.com/gonzalop/mq/internal/packets"
)

// readLoop continuously reads packets from the network.
func (c *Client) readLoop() {
	c.connLock.RLock()
	conn := c.conn
	c.connLock.RUnlock()
	c.runReadLoop(conn)
}

func (c *Client) runReadLoop(conn net.Conn) {
	defer c.wg.Done()
	defer c.activeLoops.Add(-1)
	defer c.handleDisconnect(conn)

	if conn == nil {
		return
	}

	// Wrap connection in buffered reader to reduce syscalls
	cr := &countingReader{Reader: conn, c: c}
	br := bufio.NewReader(cr)

	for {
		pkt, err := packets.ReadPacket(br, c.opts.ProtocolVersion, c.opts.MaxIncomingPacket)
		if err != nil {
			if _, ok := errors.AsType[*packets.ProtocolError](err); ok {
				c.opts.Logger.Error("protocol error, disconnecting", "error", err)
				// Section 4.13: receiver SHOULD send a DISCONNECT with Reason Code 0x82 (Protocol Error)
				c.dropConnection(ReasonCodeProtocolError)
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
	c.connLock.RLock()
	conn := c.conn
	connDone := c.connDone
	c.connLock.RUnlock()
	if connDone == nil {
		connDone = make(chan struct{})
	}
	c.runWriteLoop(conn, connDone, c.opts.KeepAlive)
}

func (c *Client) runWriteLoop(conn net.Conn, connDone <-chan struct{}, keepAlive time.Duration) {
	defer c.wg.Done()
	defer c.activeLoops.Add(-1)

	if conn == nil {
		c.opts.Logger.Debug("writeLoop started but not connected")
		return
	}

	var ticker *time.Ticker
	var tickerCh <-chan time.Time

	if keepAlive > 0 {
		// Ticker runs 4 times per keepalive interval for better resolution
		ticker = time.NewTicker(keepAlive / 4)
		defer ticker.Stop()
		tickerCh = ticker.C
	}

	cw := &countingWriter{Writer: conn, c: c}
	bw := bufio.NewWriter(cw)
	aliases := newTopicAliases(c.outgoingAliasLimit())
	lastReceived := time.Now()
	lastSent := lastReceived

	// Reset ping state and drain any stale PINGRESP signal from previous connection
	c.pingPending.Store(false)
	select {
	case <-c.pingPendingCh:
	default:
	}

	for {
		select {
		case pkt := <-c.outgoing:
			if pkt == nil {
				continue // This can happen in quick disconnect/reconnect
			}
			if a, ok := pkt.(*abortPacket); ok {
				if c.writeAbort(a, conn, bw) {
					return
				}
				continue
			}
			hasDisconnect := false
			if _, ok := pkt.(*packets.DisconnectPacket); ok {
				hasDisconnect = true
			}
			c.opts.Logger.Debug("sending packet", "type", packets.PacketNames[pkt.Type()])
			if _, err := aliases.prepare(pkt).WriteTo(bw); err != nil {
				c.opts.Logger.Debug("write error, disconnecting", "error", err)
				c.handleDisconnect(conn)
				return
			}
			c.packetsSent.Add(1)
			lastSent = time.Now()

			// Batching: try to drain channel to fill buffer
			count := len(c.outgoing)
			for range count {
				pkt := <-c.outgoing
				if pkt == nil {
					continue // This can happen in quick disconnect/reconnect
				}
				if a, ok := pkt.(*abortPacket); ok {
					if c.writeAbort(a, conn, bw) {
						return
					}
					continue
				}
				if _, ok := pkt.(*packets.DisconnectPacket); ok {
					hasDisconnect = true
				}
				c.opts.Logger.Debug("sending packet (batch)", "type", packets.PacketNames[pkt.Type()])
				if _, err := aliases.prepare(pkt).WriteTo(bw); err != nil {
					c.opts.Logger.Debug("write error (batch), disconnecting", "error", err)
					c.handleDisconnect(conn)
					return
				}
				c.packetsSent.Add(1)
				lastSent = time.Now()
			}

			// Flush after batch
			if err := bw.Flush(); err != nil {
				c.opts.Logger.Debug("flush error, disconnecting", "error", err)
				c.handleDisconnect(conn)
				return
			}

			if hasDisconnect {
				if c.disconnectFlushed != nil {
					select {
					case c.disconnectFlushed <- struct{}{}:
					default:
					}
				}
				return
			}

		case <-c.packetReceived:
			// Update lastReceived timestamp when any packet arrives
			lastReceived = time.Now()

		case <-c.pingPendingCh:
			// PINGRESP received, clear pending flag
			c.pingPending.Store(false)

		case <-tickerCh:
			// Check if we've received anything recently (1.5x keepalive timeout)
			timeout := keepAlive + keepAlive/2 // 1.5x keepalive
			if time.Since(lastReceived) >= timeout {
				c.opts.Logger.Debug("keepalive timeout, no packets received",
					"timeout", timeout,
					"last_received", time.Since(lastReceived))
				c.handleDisconnect(conn)
				return
			}

			// Send PINGREQ if we haven't sent anything for 3/4 of the keepalive interval
			// OR if we haven't received anything for 3/4 of the keepalive interval.
			// This ensures we actively probe the connection even when publishing regularly.
			// Only send if no PINGREQ is currently pending (waiting for PINGRESP).
			threshold := keepAlive - (keepAlive / 4)
			timeSinceSent := time.Since(lastSent)
			timeSinceReceived := time.Since(lastReceived)

			if !c.pingPending.Load() && (timeSinceSent >= threshold || timeSinceReceived >= threshold) {
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
					c.handleDisconnect(conn)
					return
				}
				if err := bw.Flush(); err != nil {
					c.handleDisconnect(conn)
					return
				}
				lastSent = time.Now()
				c.pingPending.Store(true)
			}

		case <-connDone:
			c.opts.Logger.Debug("writeLoop stopped: connection closed")
			return

		case <-c.stop:
			c.opts.Logger.Debug("writeLoop stopped")
			// Flush any pending packets (such as DisconnectPacket) before exiting
			for {
				select {
				case pkt := <-c.outgoing:
					if pkt == nil {
						continue
					}
					if _, ok := pkt.(*abortPacket); ok {
						continue // the client is stopping: no reason to send
					}
					if _, err := aliases.prepare(pkt).WriteTo(bw); err != nil {
						return
					}
					c.packetsSent.Add(1)
					if _, ok := pkt.(*packets.DisconnectPacket); ok {
						_ = bw.Flush()
						if c.disconnectFlushed != nil {
							select {
							case c.disconnectFlushed <- struct{}{}:
							default:
							}
						}
						return
					}
				default:
					_ = bw.Flush()
					return
				}
			}
		}
	}
}

// abortPacket is a DISCONNECT queued by dropConnection. It carries the
// connection it was meant for, so a write loop of a later connection skips it
// instead of closing the new connection.
type abortPacket struct {
	*packets.DisconnectPacket
	conn net.Conn
}

// writeAbort sends an abortPacket taken from outgoing and closes the
// connection. It reports whether the write loop must exit; an abort queued for
// an earlier connection is dropped.
func (c *Client) writeAbort(a *abortPacket, conn net.Conn, bw *bufio.Writer) bool {
	if a.conn != conn {
		return false
	}
	if _, err := a.WriteTo(bw); err == nil {
		_ = bw.Flush()
	}
	c.handleDisconnect(conn)
	return true
}

// dropConnection closes the current connection after a protocol violation by
// the server. On MQTT v5.0 it first tries to send a DISCONNECT with reasonCode.
// Unlike Disconnect it does not stop the client: OnConnectionLost runs and the
// reconnect loop takes over.
func (c *Client) dropConnection(reasonCode ReasonCode) {
	c.connLock.RLock()
	conn := c.conn
	connDone := c.connDone
	c.connLock.RUnlock()
	if conn == nil {
		return
	}

	if c.opts.ProtocolVersion >= ProtocolV50 {
		pkt := &abortPacket{
			DisconnectPacket: &packets.DisconnectPacket{
				Version:    c.opts.ProtocolVersion,
				ReasonCode: uint8(reasonCode),
			},
			conn: conn,
		}
		// Best effort: the write loop sends it and closes the connection.
		// Give it a moment, then close the connection ourselves.
		timeout := time.After(50 * time.Millisecond)
		select {
		case c.outgoing <- pkt:
			select {
			case <-connDone:
			case <-timeout:
			}
		case <-connDone:
		case <-timeout:
		}
	}
	c.handleDisconnect(conn)
}

// handleDisconnect handles connection loss for the given connection.
func (c *Client) handleDisconnect(conn net.Conn) {
	c.connLock.Lock()
	if c.conn != conn || conn == nil {
		c.connLock.Unlock()
		return // Stale or already disconnected
	}

	c.conn = nil
	if c.connDone != nil {
		close(c.connDone)
		c.connDone = nil
	}
	conn.Close()
	c.pingPending.Store(false)
	select {
	case <-c.pingPendingCh:
	default:
	}
	// Check if we have a specific disconnect reason from the server
	reason := fmt.Errorf("connection lost")
	if c.lastDisconnectReason != nil {
		reason = c.lastDisconnectReason
		c.lastDisconnectReason = nil // Clear it after use
	}
	c.connLock.Unlock()

	if !c.connected.Swap(false) {
		return // Already disconnected
	}

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

	baseBackoff := c.opts.ReconnectBackoff
	maxBackoff := c.opts.MaxReconnectBackoff
	if baseBackoff <= 0 {
		baseBackoff = time.Second
	}
	if maxBackoff <= 0 {
		maxBackoff = 2 * time.Minute
	}

	minStable := c.opts.MinStableConnectionDuration
	if minStable == 0 {
		minStable = max(2*baseBackoff, 2*time.Second)
	}

	backoff := baseBackoff

	for {
		select {
		case <-c.disconnected:
			// If this disconnection followed an established connection, check its uptime.
			if connAt := c.connectedAt.Swap(0); connAt > 0 {
				uptime := time.Since(time.Unix(0, connAt))
				if minStable > 0 && uptime < minStable {
					// Connection died prematurely (flapping): escalate backoff.
					backoff = min(backoff*2, maxBackoff)
					c.opts.Logger.Debug("connection severed below stable threshold, escalating backoff",
						"uptime", uptime, "threshold", minStable, "backoff", backoff)
				} else {
					// Connection was stable: reset backoff to base.
					backoff = baseBackoff
				}
			}

			sleepDuration := backoff
			if c.opts.EnableJitter && backoff > 0 {
				sleepDuration = time.Duration(rand.N(int64(backoff)))
			}

			// Wait before reconnecting with interruptible sleep
			select {
			case <-time.After(sleepDuration):
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

			// finalizeConnection (inside connect above) is responsible for
			// setting c.connectedAt, resetting clean session state and
			// resubscribing: it does it directly for clean sessions and through
			// checkSessionPresent for persistent ones.
			//
			// Note: backoff is NOT reset here. It will only reset if the newly
			// established connection remains alive for at least minStable duration.

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

		if cap(c.outgoing) > 0 && len(c.outgoing) == cap(c.outgoing) {
			break
		}

		// Pop from queue
		req := c.publishQueue[0]
		c.publishQueue = c.publishQueue[1:]

		// Try to prepare
		pkt, ok := c.preparePublishLocked(req)
		if !ok {
			// preparePublishLocked already completed req.token with ErrNoPacketIDsAvailable
			break
		}

		toSend = append(toSend, pkt)
	}
	return toSend
}
