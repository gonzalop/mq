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

// Disconnect gracefully disconnects from the server.
//
// It sends a DISCONNECT packet to the server, stops all background goroutines,
// and closes the network connection. The function blocks until all goroutines
// have exited or the context is cancelled.
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

	// Mark as disconnected first
	if !c.connected.Swap(false) {
		return nil // Already disconnected
	}

	// Send DISCONNECT packet
	disconnectPkt := &packets.DisconnectPacket{
		Version:    c.opts.ProtocolVersion,
		ReasonCode: reasonCode,
		Properties: toInternalProperties(props),
	}
	select {
	case c.outgoing <- disconnectPkt:
	case <-time.After(100 * time.Millisecond):
		// Timeout sending disconnect, continue anyway
	}

	// Give it a moment to send
	time.Sleep(100 * time.Millisecond)

	// Stop all goroutines
	close(c.stop)

	// Close connection to unblock readLoop
	c.connLock.Lock()
	if c.conn != nil {
		c.conn.Close()
		c.conn = nil
	}
	c.connLock.Unlock()

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

	select {
	case <-done:
		c.opts.Logger.Debug("disconnected successfully")
		return nil
	case <-waitCtx.Done():
		if errors.Is(waitCtx.Err(), context.DeadlineExceeded) {
			return fmt.Errorf("timeout waiting for goroutines to exit (%d still running)", c.activeLoops.Load())
		}
		return waitCtx.Err()
	}
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
//	    client.Publish("requests/data", payload,
//	        mq.WithResponseTopic(responseTopic))
//	}
func (c *Client) ResponseInformation() string {
	if state := c.connState.Load(); state != nil {
		return state.responseInfo
	}
	return ""
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
