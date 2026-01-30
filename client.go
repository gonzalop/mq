package mq

import (
	"bufio"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/url"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gonzalop/mq/internal/packets"
)

// serverCapabilities holds MQTT v5.0 server capabilities received in CONNACK.
// These are used to validate client operations against server limits.
type serverCapabilities struct {
	// MaximumPacketSize is the maximum packet size the server will accept.
	// 0 means no limit specified by server.
	MaximumPacketSize uint32

	// ReceiveMaximum is the maximum number of QoS 1 and QoS 2 publications
	// the server is willing to process concurrently.
	// Default is 65535 if not specified.
	ReceiveMaximum uint16

	// TopicAliasMaximum is the maximum topic alias value the server accepts.
	// 0 means topic aliases are not supported.
	TopicAliasMaximum uint16

	// MaximumQoS is the maximum QoS level the server supports.
	// Can be 0, 1, or 2.
	MaximumQoS uint8

	// RetainAvailable indicates if the server supports retained messages.
	RetainAvailable bool

	// WildcardAvailable indicates if the server supports wildcard subscriptions.
	WildcardAvailable bool

	// SubscriptionIDAvailable indicates if the server supports subscription identifiers.
	SubscriptionIDAvailable bool

	// SharedSubscriptionAvailable indicates if the server supports shared subscriptions.
	SharedSubscriptionAvailable bool
}

type subscriptionEntry struct {
	handler MessageHandler
	options SubscribeOptions
	qos     uint8
}

// Client represents an MQTT client connection.
type Client struct {
	// Configuration
	opts *clientOptions

	// Connection
	conn     net.Conn
	connLock sync.RWMutex

	// Channels for goroutine communication
	outgoing       chan packets.Packet // Packets to send
	incoming       chan packets.Packet // Packets received
	packetReceived chan struct{}       // Signal when packet received (for keepalive)
	pingPendingCh  chan struct{}       // Signal when PINGRESP received
	stop           chan struct{}       // Shutdown signal
	pingPending    bool                // True if PINGREQ sent but no PINGRESP received yet

	// Session State Lock guards:
	// - pending
	// - subscriptions
	// - receivedQoS2
	// - inFlightCount
	// - publishQueue
	// - nextPacketID
	sessionLock sync.Mutex

	// Internal queues
	publishQueue []*publishRequest

	// State (managed by logicLoop to avoid races)
	nextPacketID  uint16
	pending       map[uint16]*pendingOp // Outgoing in-flight packets (PUBLISH QoS 1/2, SUBSCRIBE, UNSUBSCRIBE)
	subscriptions map[string]subscriptionEntry
	receivedQoS2  map[uint16]struct{} // Track received QoS 2 packet IDs to prevent duplicates
	inFlightCount int                 // Number of QoS 1 special & QoS 2 packets currently in flight (outgoing)

	// Lifecycle
	connected atomic.Bool
	wg        sync.WaitGroup

	// Server capabilities (MQTT v5.0)
	serverCaps serverCapabilities

	// assignedClientID is the client ID assigned by the server when the client
	// connects with an empty client ID. Only populated for MQTT v5.0 connections.
	assignedClientID string

	// serverKeepAlive is the keepalive interval (in seconds) that the server
	// wants the client to use. If set, this overrides the client's requested keepalive.
	// Only populated for MQTT v5.0 connections when server provides this property.
	serverKeepAlive uint16

	// requestedKeepAlive preserves the original user-requested keepalive value.
	// This is used to send the same request on reconnection, even if the server
	// overrode it in a previous connection.
	requestedKeepAlive time.Duration

	// responseInformation is a string provided by the server that the client can
	// use as the basis for creating response topics. Only populated for MQTT v5.0
	// connections when the server provides this property.
	responseInformation string

	// serverReference is a server URI that the client should use for reconnection.
	// This is used for server redirects, load balancing, or maintenance scenarios.
	// Only populated for MQTT v5.0 connections when the server provides this property.
	// The library does not automatically redirect; users must handle this manually.
	serverReference string

	// Topic alias management (MQTT v5.0, client → server only)
	topicAliases     map[string]uint16 // topic → alias ID
	nextAliasID      uint16            // next ID to assign (1-based)
	maxAliases       uint16            // server's limit from CONNACK
	topicAliasesLock sync.Mutex        // protect concurrent access

	// Receive-side topic aliases (MQTT v5.0, server → client)
	receivedAliases     map[uint16]string // alias ID → topic
	receivedAliasesLock sync.RWMutex      // protect concurrent access (read-heavy)

	// Session expiry interval (MQTT v5.0)
	requestedSessionExpiry uint32 // Original user request (preserved on reconnect)
	sessionExpiryInterval  uint32 // Actual value from server (may override request)

	// Stats (atomic)
	packetsSent     atomic.Uint64
	packetsReceived atomic.Uint64
	bytesSent       atomic.Uint64
	bytesReceived   atomic.Uint64
	reconnectCount  atomic.Uint64

	// For reconnection
	disconnected chan struct{}

	// Last disconnect reason (if any) received from server via DISCONNECT packet
	lastDisconnectReason error
}

// publishRequest represents a request to publish a message.
type publishRequest struct {
	packet *packets.PublishPacket
	token  *token
}

// subscribeRequest represents a request to subscribe to a topic.
type subscribeRequest struct {
	packet      *packets.SubscribePacket
	handler     MessageHandler
	token       *token
	persistence bool
}

// unsubscribeRequest represents a request to unsubscribe from topics.
type unsubscribeRequest struct {
	packet *packets.UnsubscribePacket
	topics []string
	token  *token
}

// pendingOp tracks an in-flight operation (publish, subscribe, etc.)
type pendingOp struct {
	packet    packets.Packet
	token     *token
	qos       uint8
	timestamp time.Time
}

// MessageHandler is called when a message is received on a subscribed topic.
type MessageHandler func(*Client, Message)

// DialContext establishes a connection to an MQTT server with a context and returns a Client.
//
// The context is used to control the initial connection establishment, including
// the network dial, TLS handshake, and MQTT CONNECT handshake. If the context
// is cancelled or expires before the handshake completes, DialContext returns an error.
//
// When using DialContext, the WithConnectTimeout option is ignored for the initial
// connection (as the provided context takes precedence), but it is still used
// for subsequent automatic reconnection attempts.
//
// Once the initial connection is established, the context's expiration has no
// effect on the ongoing connection or background maintenance.
//
// Example:
//
//	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
//	defer cancel()
//
//	client, err := mq.DialContext(ctx, "tcp://localhost:1883",
//	    mq.WithClientID("my-client"))
func DialContext(ctx context.Context, server string, opts ...Option) (*Client, error) {
	options := defaultOptions(server)
	for _, opt := range opts {
		opt(options)
	}

	if options.Logger != nil {
		options.Logger = options.Logger.With("lib", "mq")
	}

	c := &Client{
		opts:     options,
		outgoing: make(chan packets.Packet, 1000),
		incoming: make(chan packets.Packet, 100),

		packetReceived: make(chan struct{}, 1),
		pingPendingCh:  make(chan struct{}, 1),
		stop:           make(chan struct{}),
		pending:        make(map[uint16]*pendingOp),
		subscriptions:  make(map[string]subscriptionEntry),
		receivedQoS2:   make(map[uint16]struct{}),
		disconnected:   make(chan struct{}, 1),
	}

	for topic, handler := range options.InitialSubscriptions {
		c.subscriptions[topic] = subscriptionEntry{
			handler: handler,
			qos:     0,
		}
	}

	if !c.opts.CleanSession {
		if err := c.loadSessionState(); err != nil {
			c.opts.Logger.Warn("failed to load session state", "error", err)
		}
	}

	if err := c.connect(ctx); err != nil {
		return nil, err
	}

	c.wg.Add(1)
	go c.logicLoop()

	if options.AutoReconnect {
		c.wg.Add(1)
		go c.reconnectLoop()
	}

	return c, nil
}

// Dial establishes a connection to an MQTT server and returns a Client.
//
// It is a wrapper around DialContext that uses the configured connection
// timeout (see WithConnectTimeout) to control the initial handshake.
//
// The server parameter specifies the server address with scheme and port.
// Supported schemes:
//   - tcp://  or mqtt://  - Unencrypted connection (default port 1883)
//   - tls://, ssl://, or mqtts:// - TLS encrypted connection (default port 8883)
//
// Options can be provided to configure the client behavior. Common options include
// WithClientID, WithCredentials, WithKeepAlive, WithTLS, and WithAutoReconnect.
//
// The function performs the MQTT handshake and starts background goroutines for
// reading, writing, and managing the connection. If AutoReconnect is enabled
// (default: true), the client will automatically reconnect on connection loss.
//
// Example (basic connection):
//
//	client, err := mq.Dial("tcp://localhost:1883",
//	    mq.WithClientID("my-client"))
//	if err != nil {
//	    log.Fatal(err)
//	}
//	defer client.Disconnect(context.Background())
//
// Example (with authentication):
//
//	client, err := mq.Dial("tcp://server:1883",
//	    mq.WithClientID("secure-client"),
//	    mq.WithCredentials("username", "password"))
//
// Example (TLS connection):
//
//	client, err := mq.Dial("tls://server:8883",
//	    mq.WithClientID("tls-client"),
//	    mq.WithTLS(&tls.Config{
//	        InsecureSkipVerify: false,
//	    }))
//
// Example (all options):
//
//	client, err := mq.Dial("tcp://server:1883",
//	    mq.WithClientID("full-client"),
//	    mq.WithCredentials("user", "pass"),
//	    mq.WithKeepAlive(60*time.Second),
//	    mq.WithCleanSession(true),
//	    mq.WithAutoReconnect(true),
//	    mq.WithConnectTimeout(30*time.Second),
//	    mq.WithWill("status/offline", []byte("disconnected"), 1, true))
func Dial(server string, opts ...Option) (*Client, error) {
	// Parse options purely to get the ConnectTimeout
	options := defaultOptions(server)
	for _, opt := range opts {
		opt(options)
	}

	ctx, cancel := context.WithTimeout(context.Background(), options.ConnectTimeout)
	defer cancel()

	return DialContext(ctx, server, opts...)
}

// connect establishes the TCP connection and performs MQTT handshake.
func (c *Client) connect(ctx context.Context) error {
	c.opts.Logger.Debug("connecting to MQTT server", "server", c.opts.Server)

	// Validate configuration for MQTT compliance
	// MQTT 3.1.1: Empty ClientID requires CleanSession=true
	// MQTT 5.0: Empty ClientID with CleanStart=false is allowed if SessionExpiryInterval > 0
	//           (server will assign a ClientID)
	if c.opts.ClientID == "" && !c.opts.CleanSession {
		// For MQTT 5.0, allow empty ClientID if SessionExpiryInterval is set
		if c.opts.ProtocolVersion >= ProtocolV50 && c.opts.SessionExpirySet && c.opts.SessionExpiryInterval > 0 {
			// Valid: Server will assign a ClientID
		} else {
			return fmt.Errorf("MQTT requires a non-empty ClientID when CleanSession is false")
		}
	}

	if c.requestedKeepAlive == 0 {
		c.requestedKeepAlive = c.opts.KeepAlive
	}

	if c.requestedSessionExpiry == 0 && c.opts.SessionExpirySet {
		c.requestedSessionExpiry = c.opts.SessionExpiryInterval
	}

	c.topicAliasesLock.Lock()
	c.topicAliases = make(map[string]uint16)
	c.nextAliasID = 1
	c.maxAliases = 0
	c.topicAliasesLock.Unlock()

	c.receivedAliasesLock.Lock()
	c.receivedAliases = make(map[uint16]string)
	c.receivedAliasesLock.Unlock()

	conn, err := c.dialServer(ctx)
	if err != nil {
		return err
	}

	c.connLock.Lock()
	c.conn = conn
	c.lastDisconnectReason = nil
	c.connLock.Unlock()

	cr := &countingReader{Reader: conn, c: c}
	cw := &countingWriter{Writer: conn, c: c}

	connectPkt := c.buildConnectPacket()
	if _, err := connectPkt.WriteTo(cw); err != nil {
		conn.Close()
		return fmt.Errorf("failed to send CONNECT: %w", err)
	}
	c.packetsSent.Add(1)

	// Handshake (CONNACK / AUTH)
	connack, err := c.performHandshake(ctx, cr, cw)
	if err != nil {
		return err
	}

	if connack.ReturnCode != packets.ConnAccepted {
		conn.Close()

		if c.opts.ProtocolVersion >= ProtocolV50 {
			err := &MqttError{
				ReasonCode: ReasonCode(connack.ReturnCode),
				Parent:     ErrConnectionRefused,
			}
			if connack.Properties != nil && connack.Properties.Presence&packets.PresReasonString != 0 {
				err.Message = connack.Properties.ReasonString
			}
			return err
		}

		switch connack.ReturnCode {
		case packets.ConnRefusedUnacceptableProtocol:
			return ErrUnacceptableProtocolVersion
		case packets.ConnRefusedIdentifierRejected:
			return ErrIdentifierRejected
		case packets.ConnRefusedServerUnavailable:
			return ErrServerUnavailable
		case packets.ConnRefusedBadUsernameOrPassword:
			return ErrBadUsernameOrPassword
		case packets.ConnRefusedNotAuthorized:
			return ErrNotAuthorized
		default:
			return fmt.Errorf("%w: code %d", ErrConnectionRefused, connack.ReturnCode)
		}
	}

	// Reset keepalive to requested value before processing server override
	// If server doesn't send ServerKeepAlive, we should use the requested value
	c.opts.KeepAlive = c.requestedKeepAlive

	c.processConnackProperties(connack)

	if !c.opts.CleanSession {
		if err := c.checkSessionPresent(connack.SessionPresent); err != nil {
			c.opts.Logger.Warn("failed to check session present", "error", err)
		}
	}

	c.opts.Logger.Debug("connection established", "server", c.opts.Server)

	c.connected.Store(true)

	if c.opts.Authenticator != nil {
		if err := c.opts.Authenticator.Complete(); err != nil {
			c.opts.Logger.Warn("authenticator complete failed", "error", err)
		}
	}

	if c.opts.OnConnect != nil {
		go c.opts.OnConnect(c)
	}

	c.wg.Add(2)
	go c.readLoop()
	go c.writeLoop()

	c.opts.Logger.Debug("client started", "client_id", c.opts.ClientID)
	return nil
}

// dialServer establishes a TCP, TLS, or custom connection to the MQTT server.
func (c *Client) dialServer(ctx context.Context) (net.Conn, error) {
	// If a custom dialer is provided, trust it to handle the scheme and address.
	// Pass the raw server string as the address to allow flexibility (e.g. WebSocket paths).
	if c.opts.Dialer != nil {
		network := "tcp"
		if u, err := url.Parse(c.opts.Server); err == nil && u.Scheme != "" {
			network = u.Scheme
		}

		conn, err := c.opts.Dialer.DialContext(ctx, network, c.opts.Server)
		if err != nil {
			return nil, fmt.Errorf("custom dialer failed: %w", err)
		}
		return conn, nil
	}

	u, err := url.Parse(c.opts.Server)
	if err != nil {
		return nil, fmt.Errorf("invalid server URL: %w", err)
	}

	if u.Port() == "" {
		switch u.Scheme {
		case "tls", "ssl", "mqtts":
			u.Host = net.JoinHostPort(u.Host, "8883")
		case "tcp", "mqtt", "":
			u.Host = net.JoinHostPort(u.Host, "1883")
		}
	}

	useTLS := u.Scheme == "tls" || u.Scheme == "ssl" || u.Scheme == "mqtts" || c.opts.TLSConfig != nil
	if !useTLS && u.Scheme != "tcp" && u.Scheme != "mqtt" {
		return nil, fmt.Errorf("unsupported scheme: %s (supported: tcp, mqtt, tls, ssl, mqtts)", u.Scheme)
	}

	var conn net.Conn
	if useTLS {
		tlsConfig := c.opts.TLSConfig
		if tlsConfig == nil {
			tlsConfig = &tls.Config{}
		}
		dialer := &tls.Dialer{
			NetDialer: &net.Dialer{},
			Config:    tlsConfig,
		}
		conn, err = dialer.DialContext(ctx, "tcp", u.Host)
	} else {
		var d net.Dialer
		conn, err = d.DialContext(ctx, "tcp", u.Host)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to connect to server: %w", err)
	}

	return conn, nil
}

// buildConnectPacket creates a CONNECT packet with the client's configuration.
func (c *Client) buildConnectPacket() *packets.ConnectPacket {
	// Use the original requested keepalive, not the potentially server-overridden value
	keepalive := c.requestedKeepAlive
	if keepalive == 0 {
		keepalive = c.opts.KeepAlive
	}

	pkt := &packets.ConnectPacket{
		ProtocolName:  "MQTT",
		ProtocolLevel: c.opts.ProtocolVersion,
		CleanSession:  c.opts.CleanSession,
		KeepAlive:     uint16(keepalive.Seconds()),
		ClientID:      c.opts.ClientID,
	}

	// Add MQTT v5.0 properties if using v5.0
	if c.opts.ProtocolVersion >= ProtocolV50 {
		pkt.Properties = &packets.Properties{}

		if c.opts.RequestProblemInformation {
			pkt.Properties.RequestProblemInformation = 1
			pkt.Properties.Presence |= packets.PresRequestProblemInformation
		}

		if c.opts.RequestResponseInformation {
			pkt.Properties.RequestResponseInformation = 1
			pkt.Properties.Presence |= packets.PresRequestResponseInformation
		}

		if c.opts.TopicAliasMaximum > 0 {
			pkt.Properties.TopicAliasMaximum = c.opts.TopicAliasMaximum
			pkt.Properties.Presence |= packets.PresTopicAliasMaximum
		}

		if c.opts.SessionExpirySet {
			pkt.Properties.SessionExpiryInterval = c.opts.SessionExpiryInterval
			pkt.Properties.Presence |= packets.PresSessionExpiryInterval
		}

		if c.opts.ReceiveMaximum > 0 {
			pkt.Properties.ReceiveMaximum = c.opts.ReceiveMaximum
			pkt.Properties.Presence |= packets.PresReceiveMaximum
		}

		if c.opts.MaxIncomingPacket > 0 {
			pkt.Properties.MaximumPacketSize = uint32(c.opts.MaxIncomingPacket)
			pkt.Properties.Presence |= packets.PresMaximumPacketSize
		}

		if c.opts.Authenticator != nil {
			pkt.Properties.AuthenticationMethod = c.opts.Authenticator.Method()
			pkt.Properties.Presence |= packets.PresAuthenticationMethod

			initialData, err := c.opts.Authenticator.InitialData()
			if err != nil {
				c.opts.Logger.Error("failed to get initial auth data", "error", err)
			} else if len(initialData) > 0 {
				pkt.Properties.AuthenticationData = initialData
			}
		}
	}

	if c.opts.Username != "" {
		pkt.UsernameFlag = true
		pkt.Username = c.opts.Username
	}
	if c.opts.Password != "" {
		pkt.PasswordFlag = true
		pkt.Password = c.opts.Password
	}

	if c.opts.will != nil {
		pkt.WillFlag = true
		pkt.WillTopic = c.opts.will.Topic
		pkt.WillMessage = c.opts.will.Payload
		pkt.WillQoS = c.opts.will.QoS
		pkt.WillRetain = c.opts.will.Retained

		if c.opts.will.Properties != nil {
			pkt.WillProperties = toInternalProperties(c.opts.will.Properties)
		}
	}

	return pkt
}

// readLoop continuously reads packets from the network.
func (c *Client) readLoop() {
	defer c.wg.Done()
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
			c.opts.Logger.Debug("read error, disconnecting", "error", err)
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
		go c.opts.OnConnectionLost(c, reason)
	}

	// Signal reconnect loop
	select {
	case c.disconnected <- struct{}{}:
	default:
	}
}

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
	return c.disconnectWithReason(ctx, uint8(options.ReasonCode), options.Properties)
}

// disconnectWithReason is an internal helper that sends a DISCONNECT packet
// with a specific reason code (MQTT v5.0).
func (c *Client) disconnectWithReason(ctx context.Context, reasonCode uint8, props *Properties) error {
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

	// Wait for goroutines with timeout
	done := make(chan struct{})
	go func() {
		c.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		c.opts.Logger.Debug("disconnected successfully")
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(5 * time.Second):
		return fmt.Errorf("timeout waiting for goroutines to exit")
	}
}

// reconnectLoop handles automatic reconnection.
func (c *Client) reconnectLoop() {
	defer c.wg.Done()

	backoff := time.Second
	maxBackoff := 2 * time.Minute

	for {
		select {
		case <-c.disconnected:
			// Wait before reconnecting
			time.Sleep(backoff)

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

			backoff = time.Second

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
	return c.assignedClientID
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
	return c.serverKeepAlive
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
// WthOnServerRedirect option.
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
	return c.serverReference
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
	return c.sessionExpiryInterval
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
	return c.responseInformation
}

// extractServerCapabilities extracts server capabilities from CONNACK properties.
func extractServerCapabilities(props *packets.Properties) serverCapabilities {
	caps := serverCapabilities{
		// Set defaults per MQTT v5.0 spec
		ReceiveMaximum:              65535, // Default if not specified
		MaximumQoS:                  2,     // Default if not specified (supports 0, 1, 2)
		RetainAvailable:             true,  // Default if not specified
		WildcardAvailable:           true,  // Default if not specified
		SubscriptionIDAvailable:     true,  // Default if not specified
		SharedSubscriptionAvailable: true,  // Default if not specified
	}

	if props == nil {
		return caps
	}

	// Extract capabilities from properties
	if props.Presence&packets.PresMaximumPacketSize != 0 {
		caps.MaximumPacketSize = props.MaximumPacketSize
	}

	if props.Presence&packets.PresReceiveMaximum != 0 {
		caps.ReceiveMaximum = props.ReceiveMaximum
	}

	if props.Presence&packets.PresTopicAliasMaximum != 0 {
		caps.TopicAliasMaximum = props.TopicAliasMaximum
	}

	if props.Presence&packets.PresMaximumQoS != 0 {
		caps.MaximumQoS = props.MaximumQoS
	}

	if props.Presence&packets.PresRetainAvailable != 0 {
		caps.RetainAvailable = props.RetainAvailable
	}

	if props.Presence&packets.PresWildcardSubscriptionAvailable != 0 {
		caps.WildcardAvailable = props.WildcardSubscriptionAvailable
	}

	if props.Presence&packets.PresSubscriptionIdentifierAvailable != 0 {
		caps.SubscriptionIDAvailable = props.SubscriptionIdentifierAvailable
	}

	if props.Presence&packets.PresSharedSubscriptionAvailable != 0 {
		caps.SharedSubscriptionAvailable = props.SharedSubscriptionAvailable
	}

	return caps
}

// ServerCapabilities represents the capabilities and limits advertised by the MQTT server.
// These are only available when using MQTT v5.0.
type ServerCapabilities struct {
	// MaximumPacketSize is the maximum packet size the server will accept.
	// 0 means no limit was specified by the server.
	MaximumPacketSize uint32

	// ReceiveMaximum is the maximum number of QoS 1 and QoS 2 publications
	// the server is willing to process concurrently.
	ReceiveMaximum uint16

	// TopicAliasMaximum is the maximum topic alias value the server accepts.
	// 0 means topic aliases are not supported by the server.
	TopicAliasMaximum uint16

	// MaximumQoS is the maximum QoS level the server supports (0, 1, or 2).
	MaximumQoS uint8

	// RetainAvailable indicates if the server supports retained messages.
	RetainAvailable bool

	// WildcardAvailable indicates if the server supports wildcard subscriptions.
	WildcardAvailable bool

	// SubscriptionIDAvailable indicates if the server supports subscription identifiers.
	SubscriptionIDAvailable bool

	// SharedSubscriptionAvailable indicates if the server supports shared subscriptions.
	SharedSubscriptionAvailable bool
}

// ServerCapabilities returns the server capabilities received in the CONNACK packet.
// This is only populated for MQTT v5.0 connections.
// For v3.1.1 connections, default values are returned.
func (c *Client) ServerCapabilities() ServerCapabilities {
	return ServerCapabilities{
		MaximumPacketSize:           c.serverCaps.MaximumPacketSize,
		ReceiveMaximum:              c.serverCaps.ReceiveMaximum,
		TopicAliasMaximum:           c.serverCaps.TopicAliasMaximum,
		MaximumQoS:                  c.serverCaps.MaximumQoS,
		RetainAvailable:             c.serverCaps.RetainAvailable,
		WildcardAvailable:           c.serverCaps.WildcardAvailable,
		SubscriptionIDAvailable:     c.serverCaps.SubscriptionIDAvailable,
		SharedSubscriptionAvailable: c.serverCaps.SharedSubscriptionAvailable,
	}
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

func (c *Client) performHandshake(ctx context.Context, r io.Reader, w io.Writer) (*packets.ConnackPacket, error) {
	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(c.opts.ConnectTimeout)
	}

	c.connLock.RLock()
	conn := c.conn
	c.connLock.RUnlock()
	_ = conn.SetReadDeadline(deadline)
	defer func() { _ = conn.SetReadDeadline(time.Time{}) }()

	for {
		pkt, err := packets.ReadPacket(r, c.opts.ProtocolVersion, c.opts.MaxIncomingPacket)
		if err != nil {
			conn.Close()
			return nil, fmt.Errorf("failed to read packet: %w", err)
		}
		c.packetsReceived.Add(1)

		switch p := pkt.(type) {
		case *packets.ConnackPacket:
			return p, nil

		case *packets.AuthPacket:
			if c.opts.ProtocolVersion < ProtocolV50 {
				conn.Close()
				return nil, fmt.Errorf("received AUTH packet in v3.1.1")
			}
			if c.opts.Authenticator == nil {
				conn.Close()
				return nil, fmt.Errorf("received AUTH packet but no authenticator configured")
			}

			respData, err := c.opts.Authenticator.HandleChallenge(p.Properties.AuthenticationData, p.ReasonCode)
			if err != nil {
				conn.Close()
				return nil, fmt.Errorf("authentication failed: %w", err)
			}

			// Send AUTH response
			authResp := &packets.AuthPacket{
				Version:    ProtocolV50,
				ReasonCode: packets.AuthReasonContinue, // 0x18
				Properties: &packets.Properties{
					AuthenticationMethod: c.opts.Authenticator.Method(),
					AuthenticationData:   respData,
				},
			}

			if _, err := authResp.WriteTo(w); err != nil {
				conn.Close()
				return nil, fmt.Errorf("failed to send AUTH response: %w", err)
			}
			c.packetsSent.Add(1)

		default:
			conn.Close()
			return nil, fmt.Errorf("expected CONNACK or AUTH, got packet type %d", pkt.Type())
		}
	}
}

func (c *Client) processConnackProperties(connack *packets.ConnackPacket) {
	if c.opts.ProtocolVersion >= ProtocolV50 && connack.Properties != nil {
		c.serverCaps = extractServerCapabilities(connack.Properties)
		c.opts.Logger.Debug("received server capabilities",
			"max_packet_size", c.serverCaps.MaximumPacketSize,
			"receive_maximum", c.serverCaps.ReceiveMaximum,
			"max_qos", c.serverCaps.MaximumQoS,
			"retain_available", c.serverCaps.RetainAvailable)

		if connack.Properties.Presence&packets.PresAssignedClientIdentifier != 0 {
			c.assignedClientID = connack.Properties.AssignedClientIdentifier
			c.opts.ClientID = c.assignedClientID
			c.opts.Logger.Debug("server assigned client ID", "client_id", c.assignedClientID)
		}

		if connack.Properties.Presence&packets.PresResponseInformation != 0 {
			c.responseInformation = connack.Properties.ResponseInformation
			c.opts.Logger.Debug("server provided response information", "response_info", c.responseInformation)
		}

		if connack.Properties.Presence&packets.PresServerReference != 0 {
			c.serverReference = connack.Properties.ServerReference
			c.opts.Logger.Debug("server provided redirect reference", "server_reference", c.serverReference)

			if c.opts.OnServerRedirect != nil {
				go c.opts.OnServerRedirect(c.serverReference)
			}
		}

		if c.opts.TopicAliasMaximum > 0 && connack.Properties.Presence&packets.PresTopicAliasMaximum != 0 {
			serverLimit := connack.Properties.TopicAliasMaximum
			if serverLimit > 0 {
				c.maxAliases = min(serverLimit, c.opts.TopicAliasMaximum)
				c.topicAliases = make(map[string]uint16)
				c.nextAliasID = 1
				c.opts.Logger.Debug("topic aliases enabled",
					"client_accepts", c.opts.TopicAliasMaximum,
					"server_accepts", serverLimit,
					"using", c.maxAliases)
			}
		}

		if connack.Properties.Presence&packets.PresServerKeepAlive != 0 {
			c.serverKeepAlive = connack.Properties.ServerKeepAlive
			c.opts.KeepAlive = time.Duration(c.serverKeepAlive) * time.Second
			c.opts.Logger.Debug("server overrode keepalive",
				"requested", uint16(c.requestedKeepAlive.Seconds()),
				"server_keepalive", c.serverKeepAlive)
		} else {
			c.serverKeepAlive = 0
			c.opts.Logger.Debug("server accepted keepalive",
				"keepalive", uint16(c.requestedKeepAlive.Seconds()))
		}

		if connack.Properties.Presence&packets.PresSessionExpiryInterval != 0 {
			c.sessionExpiryInterval = connack.Properties.SessionExpiryInterval
			c.opts.Logger.Debug("server set session expiry",
				"requested", c.requestedSessionExpiry,
				"actual", c.sessionExpiryInterval)
		} else if c.opts.SessionExpirySet {
			c.sessionExpiryInterval = c.requestedSessionExpiry
			c.opts.Logger.Debug("server accepted session expiry",
				"interval", c.sessionExpiryInterval)
		}
	} else {
		// Use default capabilities for older protocols or if no properties sent
		c.serverCaps = extractServerCapabilities(nil)
	}
}

type countingReader struct {
	io.Reader
	c *Client
}

func (r *countingReader) Read(p []byte) (int, error) {
	n, err := r.Reader.Read(p)
	if n > 0 {
		r.c.bytesReceived.Add(uint64(n))
	}
	return n, err
}

type countingWriter struct {
	io.Writer
	c *Client
}

func (w *countingWriter) Write(p []byte) (int, error) {
	n, err := w.Writer.Write(p)
	if n > 0 {
		w.c.bytesSent.Add(uint64(n))
	}
	return n, err
}
