package mq

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gonzalop/mq/internal/packets"
	"github.com/gonzalop/mq/internal/trie"
)

// topicTrie is a type alias for the generic trie in internal/trie.
// This allows existing code and tests to work with minimal changes.
type topicTrie = trie.TopicTrie[MessageHandler]

func newTopicTrie() *topicTrie {
	return trie.New[MessageHandler]()
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
	pendingOrder  []uint16              // Order of pending packets for retransmission
	subscriptions map[string]subscriptionEntry
	trie          *topicTrie          // Efficient topic matching
	receivedQoS2  map[uint16]struct{} // Track received QoS 2 packet IDs to prevent duplicates
	inFlightCount int                 // Number of QoS 1 special & QoS 2 packets currently in flight (outgoing)

	// Lifecycle
	connected   atomic.Bool
	wg          sync.WaitGroup
	activeLoops atomic.Int32

	// connState holds the server capabilities and connection properties (MQTT v5.0).
	// It is accessed atomically to prevent race conditions during reconnection.
	connState atomic.Pointer[connectionState]

	// requestedKeepAlive preserves the original user-requested keepalive value.
	// This is used to send the same request on reconnection, even if the server
	// overrode it in a previous connection.
	requestedKeepAlive time.Duration

	// Topic alias management (MQTT v5.0, client → server only)
	topicAliases     map[string]uint16 // topic → alias ID
	nextAliasID      uint16            // next ID to assign (1-based)
	maxAliases       uint16            // server's limit from CONNACK
	topicAliasesLock sync.Mutex        // protect concurrent access

	// Flow control (MQTT v5.0, server → client)
	inboundUnacked           map[uint16]struct{} // Packet IDs of received QoS 1/2 messages not yet acked
	receiveMaxExceededLogged bool                // Warn once per connection

	// Receive-side topic aliases (MQTT v5.0, server → client)
	receivedAliases     map[uint16]string // alias ID → topic
	receivedAliasesLock sync.RWMutex      // protect concurrent access (read-heavy)

	// Concurrency control for message handlers
	handlerSem chan struct{}

	// authExchangeCount tracks the number of AUTH packet exchanges
	// to prevent infinite authentication loops.
	authExchangeCount atomic.Uint32

	// Session expiry interval (MQTT v5.0)
	requestedSessionExpiry uint32 // Original user request (preserved on reconnect)

	// User Properties received in CONNACK (MQTT v5.0)
	connackUserProperties map[string]string

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

	// The wrapped publish function (including interceptors)
	publish PublishFunc

	// The wrapped default message handler (including interceptors)
	defaultHandler MessageHandler
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
		trie:     newTopicTrie(),
		opts:     options,
		outgoing: make(chan packets.Packet, options.OutgoingQueueSize),
		incoming: make(chan packets.Packet, options.IncomingQueueSize),

		packetReceived:  make(chan struct{}, 1),
		pingPendingCh:   make(chan struct{}, 1),
		stop:            make(chan struct{}),
		pending:         make(map[uint16]*pendingOp),
		subscriptions:   make(map[string]subscriptionEntry),
		receivedAliases: make(map[uint16]string),
		receivedQoS2:    make(map[uint16]struct{}),
		inboundUnacked:  make(map[uint16]struct{}),
		disconnected:    make(chan struct{}, 1),
	}
	c.connState.Store(&connectionState{
		caps: extractServerCapabilities(nil),
	})

	if options.MaxHandlerConcurrency > 0 {
		c.handlerSem = make(chan struct{}, options.MaxHandlerConcurrency)
	}

	c.publish = applyPublishInterceptors(c.basePublish, options.PublishInterceptors)
	c.defaultHandler = c.wrapHandler(options.DefaultPublishHandler)

	for topic, handler := range options.InitialSubscriptions {
		wrapped := c.wrapHandler(handler)
		c.addSubscriptionLocked(topic, subscriptionEntry{
			handler: wrapped,
			qos:     0,
		})
	}

	if !c.opts.CleanSession {
		if err := c.loadSessionState(); err != nil {
			c.opts.Logger.Warn("failed to load session state", "error", err)
		}
	} else if c.opts.SessionStore != nil {
		if err := c.opts.SessionStore.Clear(); err != nil {
			c.opts.Logger.Warn("failed to clear session store", "error", err)
		}
	}

	if err := c.connect(ctx); err != nil {
		// Version negotiation: if v5.0 fails with "unacceptable protocol", try v3.1.1
		if c.opts.AutoProtocolVersion && c.opts.ProtocolVersion == ProtocolV50 {
			isProtoError := false
			if errors.Is(err, ErrUnacceptableProtocolVersion) {
				isProtoError = true
			} else if mqErr, ok := err.(*MqttError); ok && mqErr.ReasonCode == 0x84 {
				// 0x84 is MQTT v5.0 "Unsupported Protocol Version"
				isProtoError = true
			} else if mqErr, ok := err.(*MqttError); ok && mqErr.ReasonCode == ReasonCode(packets.ConnRefusedUnacceptableProtocol) {
				// Some servers might return 0x01 even in v5.0-like responses
				isProtoError = true
			}

			if isProtoError {
				c.opts.Logger.Debug("v5.0 connection refused with unacceptable protocol, falling back to v3.1.1")
				c.opts.ProtocolVersion = ProtocolV311
				if err := c.connect(ctx); err != nil {
					return nil, err
				}
			} else {
				return nil, err
			}
		} else {
			return nil, err
		}
	}

	c.wg.Add(1)
	c.activeLoops.Add(1)
	go c.logicLoop()

	if options.AutoReconnect {
		c.wg.Add(1)
		c.activeLoops.Add(1)
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
	if c.opts.ClientID == "" && !c.opts.CleanSession &&
		!(c.opts.ProtocolVersion >= ProtocolV50 && c.opts.SessionExpirySet && c.opts.SessionExpiryInterval > 0) {
		return fmt.Errorf("MQTT requires a non-empty ClientID when CleanSession is false")
	}

	c.prepareConnectionState()

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

	// 1. Send CONNECT
	if err := c.sendConnectPacket(cw); err != nil {
		conn.Close()
		return err
	}

	// 2. Handshake (CONNACK / AUTH)
	connack, err := c.performHandshake(ctx, cr, cw)
	if err != nil {
		return err
	}

	// 3. Validate CONNACK
	if err := c.validateConnack(conn, connack); err != nil {
		return err
	}

	// 4. Initialize Session
	c.finalizeConnection(connack)

	return nil
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
