package mq

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/gonzalop/mq/internal/packets"
)

// IsConnected returns true if the client is currently connected to the server.
// This method is thread-safe.
func (c *Client) IsConnected() bool {
	return c.connected.Load()
}

// ProtocolVersion returns the active MQTT protocol version in use by the client
// (ProtocolV50 or ProtocolV311). When automatic protocol version negotiation is
// enabled (the default), this reflects the version negotiated during Dial.
func (c *Client) ProtocolVersion() uint8 {
	if c == nil || c.opts == nil {
		return 0
	}
	return c.opts.ProtocolVersion
}

// disconnectFlushTimeout caps how long Disconnect waits for the DISCONNECT
// packet to be written when ctx has no deadline.
const disconnectFlushTimeout = time.Second

// Disconnect gracefully disconnects from the server.
//
// It sends a DISCONNECT packet to the server, stops all background goroutines,
// and closes the network connection. The function blocks until all goroutines
// have exited or the context is cancelled.
//
// The DISCONNECT is written after any packets already queued for sending.
// If ctx has a deadline, Disconnect waits until that deadline for the
// DISCONNECT to be written before closing the connection. If ctx has no
// deadline (for example context.Background()), it waits at most 1 second, so
// a stalled connection cannot block it. If the connection is closed before
// the DISCONNECT is written, the server treats it as an unexpected drop and
// publishes the Last Will message, if one is set. When you publish a burst
// right before disconnecting over a slow link, pass a ctx with a longer
// deadline.
//
// If AutoReconnect is enabled, it will be disabled after calling Disconnect.
// To reconnect, create a new client with Dial.
//
// If the client is connected with MQTT v5.0, you can provide options such as
// WithReason to specify the reason code. These options are ignored when
// using MQTT v3.1.1.
//
// Example:
//
//	// Normal disconnect (v3.1.1 or v5.0)
//	client.Disconnect(context.Background())
//
//	// Disconnect with reason (v5.0 only)
//	client.Disconnect(context.Background(), mq.WithReason(mq.ReasonCodeDisconnectWithWill))
func (c *Client) Disconnect(ctx context.Context, opts ...DisconnectOption) error {
	options := &DisconnectOptions{
		ReasonCode: ReasonCodeNormalDisconnect,
	}
	for _, opt := range opts {
		opt(options)
	}
	return c.disconnectWithReason(ctx, uint8(options.ReasonCode), options.Properties, true)
}

// disconnectWithReason is an internal helper that sends a DISCONNECT packet
// with a specific reason code (MQTT v5.0).
// If block is true, it waits for all background loops to exit.
func (c *Client) disconnectWithReason(ctx context.Context, reasonCode uint8, props *Properties, block bool) error {
	c.opts.Logger.Debug("disconnecting from server", "reason_code", reasonCode)

	if err := validatePropertiesFieldLengths(props); err != nil {
		return fmt.Errorf("invalid disconnect properties: %w", err)
	}

	c.connLock.RLock()
	conn := c.conn
	connDone := c.connDone
	c.connLock.RUnlock()

	// A graceful DISCONNECT only makes sense while the connection is up: it is
	// what suppresses the will on the broker. When a reconnect is in progress
	// there is nothing to send, but the loops below must still be stopped.
	if c.connected.Swap(false) {
		disconnectPkt := &packets.DisconnectPacket{
			Version:    c.opts.ProtocolVersion,
			ReasonCode: reasonCode,
			Properties: toInternalProperties(props),
		}

		if conn != nil {
			// With a deadline on ctx, ctx alone limits how long we wait for
			// the DISCONNECT to be queued and flushed. Without one, the wait
			// is capped so Disconnect cannot hang on a stalled connection.
			var flushTimeout <-chan time.Time
			if _, ok := ctx.Deadline(); !ok {
				t := time.NewTimer(disconnectFlushTimeout)
				defer t.Stop()
				flushTimeout = t.C
			}

			if c.disconnectFlushed != nil {
				select {
				case <-c.disconnectFlushed:
				default:
				}
			}

			// Send DISCONNECT packet to outgoing and wait for writeLoop to flush it
			select {
			case c.outgoing <- disconnectPkt:
				if c.disconnectFlushed != nil {
					select {
					case <-c.disconnectFlushed:
					case <-connDone:
					case <-ctx.Done():
					case <-flushTimeout:
					}
				}
			case <-connDone:
			case <-ctx.Done():
			case <-flushTimeout:
			}
		} else if c.outgoing != nil {
			select {
			case c.outgoing <- disconnectPkt:
			default:
			}
		}
	}

	// Stop all goroutines. Idempotent and safe to call while a reconnect is in
	// progress (Disconnect may be invoked more than once). Clients built
	// without NewClient (tests) may have no stop channel: there is then no
	// loop to stop.
	if c.stop != nil {
		c.stopOnce.Do(func() { close(c.stop) })
	}

	// Close connection and signal connDone to unblock readLoop and writeLoop
	c.connLock.Lock()
	conn = c.conn
	c.conn = nil
	// A connect that finished after the Swap above may have set connected
	// again; clearing it under connLock pairs with finalizeConnection.
	c.connected.Store(false)
	if c.connDone != nil {
		close(c.connDone)
		c.connDone = nil
	}
	c.connLock.Unlock()
	if conn != nil {
		conn.Close()
	}

	if !block {
		return nil
	}

	// Wait for goroutines with timeout
	waitCtx, waitCancel := context.WithTimeout(ctx, 5*time.Second)
	defer waitCancel()

	done := make(chan struct{})
	go func() {
		c.wg.Wait()
		close(done)
	}()

	var err error
	select {
	case <-done:
		c.opts.Logger.Debug("disconnected successfully")
	case <-waitCtx.Done():
		if errors.Is(waitCtx.Err(), context.DeadlineExceeded) {
			err = fmt.Errorf("timeout waiting for goroutines to exit (%d still running)", c.activeLoops.Load())
		} else {
			err = waitCtx.Err()
		}
	}

	// Close the session store, bounded by the same deadline: AsyncStore.Close
	// waits for its queued writes, and a slow or stuck store must not make
	// Disconnect ignore ctx.
	if c.opts != nil && c.opts.SessionStore != nil {
		closed := make(chan error, 1)
		go func() { closed <- closeSessionStore(c.opts.SessionStore) }()
		select {
		case closeErr := <-closed:
			if closeErr != nil {
				c.opts.Logger.Warn("session store close failed", "error", closeErr)
			}
		case <-waitCtx.Done():
			if err == nil {
				err = fmt.Errorf("session store still flushing: %w", waitCtx.Err())
			}
		}
	}

	return err
}

// closeSessionStore closes store if it has a Close method.
func closeSessionStore(store SessionStore) error {
	switch closer := store.(type) {
	case interface{ Close() error }:
		return closer.Close()
	case interface{ Close() }:
		closer.Close()
	}
	return nil
}

// AssignedClientID returns the client ID assigned by the server.
//
// When connecting with an empty client ID, the server may assign a unique
// identifier and return it in the CONNACK packet. This method returns that
// assigned ID if one was provided.
//
// This is only populated for MQTT v5.0 connections where the client connected
// with an empty client ID and the server assigned one. For v3.1.1 connections
// or when the client provided their own ID, this returns an empty string.
//
// The assigned ID can be useful for:
//   - Logging and debugging
//   - Reconnection with the same session
//   - Understanding what ID the server chose
//
// Example:
//
//	client, _ := mq.Dial("tcp://localhost:1883",
//	    mq.WithClientID(""),  // Empty - let server assign
//	    mq.WithProtocolVersion(mq.ProtocolV50))
//
//	assignedID := client.AssignedClientID()
//	if assignedID != "" {
//	    fmt.Printf("Server assigned ID: %s\n", assignedID)
//	}
func (c *Client) AssignedClientID() string {
	if state := c.connState.Load(); state != nil {
		return state.assignedClientID
	}
	return ""
}

// ServerKeepAlive returns the keepalive interval (in seconds) that the server
// wants the client to use.
//
// In MQTT v5.0, the server can override the client's requested keepalive interval
// by sending a Server Keep Alive property in the CONNACK packet. When this happens,
// the client MUST use the server's value instead of its requested value.
//
// This method returns the server's keepalive value if one was provided, or 0 if:
//   - The connection is using MQTT v3.1.1 (property not supported)
//   - The server did not override the keepalive (accepted client's request)
//   - Not yet connected
//
// The library automatically adjusts its internal keepalive timer when the server
// overrides the value, so users typically don't need to take action. This method
// is primarily useful for logging and debugging.
//
// Example:
//
//	client, _ := mq.Dial("tcp://localhost:1883",
//	    mq.WithKeepAlive(60*time.Second),
//	    mq.WithProtocolVersion(mq.ProtocolV50))
//
//	if serverKA := client.ServerKeepAlive(); serverKA > 0 {
//	    fmt.Printf("Server overrode keepalive to %d seconds\n", serverKA)
//	}
func (c *Client) ServerKeepAlive() uint16 {
	if state := c.connState.Load(); state != nil {
		return state.serverKeepAlive
	}
	return 0
}

// ServerReference returns the server reference URI provided by the server.
//
// In MQTT v5.0, the server can provide a ServerReference string in the CONNACK
// or DISCONNECT packet to tell the client to connect to a different server.
// This is used for:
//   - Load balancing: Distribute clients across multiple servers
//   - Maintenance: Move clients before server shutdown
//   - Geographic routing: Direct clients to nearest server
//   - Failover: Redirect to backup server
//
// IMPORTANT: The library does NOT automatically redirect to the referenced server.
// Users must check this value and manually reconnect if desired or use the
// WithOnServerRedirect option.
//
// This method returns the server's reference URI if one was provided, or an
// empty string if:
//   - The connection is using MQTT v3.1.1 (property not supported)
//   - The server did not provide a server reference
//   - Not yet connected
//
// Example usage with callback (recommended):
//
//	client, _ := mq.Dial("tcp://server-a.example.com:1883",
//	    mq.WithOnServerRedirect(func(newServer string) {
//	        log.Printf("Server suggests: %s", newServer)
//	        // Decide whether to reconnect
//	    }))
//
// Example usage with polling:
//
//	client, _ := mq.Dial("tcp://server-a.example.com:1883",
//	    mq.WithProtocolVersion(mq.ProtocolV50))
//
//	if ref := client.ServerReference(); ref != "" {
//	    fmt.Printf("Server suggests redirecting to: %s\n", ref)
//	    // Application decides whether to reconnect
//	    client.Disconnect(context.Background())
//	    newClient, _ := mq.Dial(ref, mq.WithProtocolVersion(mq.ProtocolV50))
//	}
func (c *Client) ServerReference() string {
	if state := c.connState.Load(); state != nil {
		return state.serverReference
	}
	return ""
}

// SessionExpiryInterval returns the session expiry interval (in seconds)
// that the server is using for this connection.
//
// For MQTT v5.0, this returns the actual value negotiated with the server.
// The server can override the client's requested value.
//
// For MQTT v3.1.1:
//   - If CleanSession is false (persistent), this returns 0xFFFFFFFF (MaxUint32),
//     reflecting that the session does not expire on disconnect.
//   - If CleanSession is true, this returns 0.
//
// Returns 0 if session expires immediately on disconnect or not yet connected.
//
// Example:
//
//	client, _ := mq.Dial("tcp://localhost:1883",
//	    mq.WithProtocolVersion(mq.ProtocolV50),
//	    mq.WithSessionExpiryInterval(3600))
//
//	actual := client.SessionExpiryInterval()
//	if actual != 3600 {
//	    fmt.Printf("Server overrode session expiry to %d seconds\n", actual)
//	}
func (c *Client) SessionExpiryInterval() uint32 {
	if c.opts.ProtocolVersion < ProtocolV50 && !c.opts.CleanSession {
		return 0xFFFFFFFF
	}
	if state := c.connState.Load(); state != nil {
		return state.sessionExpiry
	}
	return 0
}

// ResponseInformation returns the response information string provided by the server.
//
// In MQTT v5.0, the server can provide a ResponseInformation string in the CONNACK
// packet. This string can be used by the client as the basis for creating response
// topics in request/response messaging patterns.
//
// This is typically used in multi-tenant or managed cloud environments where the
// server wants to control topic naming conventions and ensure proper namespace
// isolation between clients.
//
// This method returns the server's response information if one was provided, or an
// empty string if:
//   - The connection is using MQTT v3.1.1 (property not supported)
//   - The server did not provide response information
//   - Not yet connected
//
// Example usage:
//
//	client, _ := mq.Dial("tcp://localhost:1883",
//	    mq.WithProtocolVersion(mq.ProtocolV50))
//
//	if respInfo := client.ResponseInformation(); respInfo != "" {
//	    // Use server's suggested prefix for response topics
//	    responseTopic := respInfo + "my-responses"
//	    client.Publish(context.Background(), "requests/data", payload,
//	        mq.WithResponseTopic(responseTopic))
//	}
func (c *Client) ResponseInformation() string {
	if state := c.connState.Load(); state != nil {
		return state.responseInfo
	}
	return ""
}

// ConnectedAt returns the time when the current connection was established,
// or a zero time if the client is not currently connected.
func (c *Client) ConnectedAt() time.Time {
	if !c.IsConnected() {
		return time.Time{}
	}
	ts := c.connectedAt.Load()
	if ts == 0 {
		return time.Time{}
	}
	return time.Unix(0, ts)
}

// Uptime returns the duration for which the current connection has been active,
// or 0 if the client is not currently connected.
func (c *Client) Uptime() time.Duration {
	if !c.IsConnected() {
		return 0
	}
	ts := c.connectedAt.Load()
	if ts == 0 {
		return 0
	}
	return time.Since(time.Unix(0, ts))
}

// ClientStats holds connection and throughput statistics.
type ClientStats struct {
	PacketsSent     uint64
	PacketsReceived uint64
	BytesSent       uint64
	BytesReceived   uint64
	ReconnectCount  uint64
	Connected       bool
}

// GetStats returns the current client statistics.
func (c *Client) GetStats() ClientStats {
	return ClientStats{
		PacketsSent:     c.packetsSent.Load(),
		PacketsReceived: c.packetsReceived.Load(),
		BytesSent:       c.bytesSent.Load(),
		BytesReceived:   c.bytesReceived.Load(),
		ReconnectCount:  c.reconnectCount.Load(),
		Connected:       c.IsConnected(),
	}
}
