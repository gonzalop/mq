package mq

import (
	"context"
	"crypto/tls"
	"io"
	"log/slog"
	"net"
	"time"
)

// ContextDialer is an interface for custom network dialing logic.
// It matches the signature of net.Dialer.DialContext.
type ContextDialer interface {
	DialContext(ctx context.Context, network, addr string) (net.Conn, error)
}

// clientOptions holds configuration for the MQTT client.
type clientOptions struct {
	// MQTT server address (e.g., "tcp://localhost:1883")
	Server string

	// Client identifier
	ClientID string

	// Username for authentication (optional)
	Username string

	// Password for authentication (optional)
	Password string

	// Keep alive interval
	KeepAlive time.Duration

	// Clean session flag
	CleanSession bool

	// Auto-reconnect on connection loss
	AutoReconnect bool

	// Connection timeout
	ConnectTimeout time.Duration

	// TLS configuration (optional)
	TLSConfig *tls.Config

	// Logger for client events (optional, defaults to discarding logs)
	Logger *slog.Logger

	// Limits (0 = use MQTT spec defaults)
	MaxTopicLength    int // Maximum topic length (default: 65535)
	MaxPayloadSize    int // Maximum outgoing payload size (default: 256MB)
	MaxIncomingPacket int // Maximum incoming packet size (default: 256MB)

	// Will message (optional)
	will *willMessage

	// Lifecycle hooks (optional)
	OnConnect        func(*Client)
	OnConnectionLost func(*Client, error)
	OnServerRedirect func(serverURI string) // MQTT v5.0: Called when server provides redirection reference

	// Initial subscriptions (optional)
	InitialSubscriptions map[string]MessageHandler

	// Protocol Version (4 = v3.1.1, 5 = v5.0)
	ProtocolVersion uint8

	// MQTT v5.0 request flags
	RequestProblemInformation  bool
	RequestResponseInformation bool

	// MQTT v5.0 topic alias maximum (client → server)
	// Maximum number of topic aliases the client will use when publishing.
	// 0 = disabled (default). Server may override to a lower value.
	TopicAliasMaximum uint16

	// MQTT v5.0 receive maximum (client side flow control)
	// Maximum number of QoS 1 and QoS 2 publications the client is willing to process concurrently.
	// 0 = 65535 (default)
	ReceiveMaximum       uint16
	ReceiveMaximumPolicy LimitPolicy

	// MQTT v5.0 session expiry interval
	// How long the server should maintain session state after disconnect (in seconds).
	// Only used if SessionExpirySet is true.
	SessionExpiryInterval uint32
	SessionExpirySet      bool // Track if explicitly set vs default

	// MQTT v5.0 User Properties for CONNECT packet
	ConnectUserProperties map[string]string

	// Default publish handler (optional)
	// Called when a PUBLISH packet doesn't match any registered subscription.
	DefaultPublishHandler MessageHandler

	// Custom dialer (optional)
	// If set, this is used to establish the connection instead of net.Dialer.
	Dialer ContextDialer

	// Session store for persistence (optional)
	// If set, session state will be persisted across process restarts.
	SessionStore SessionStore

	// Authenticator for enhanced authentication (optional, MQTT v5.0 only)
	// If set, enables challenge/response authentication via AUTH packet flow.
	Authenticator Authenticator
}

const (
	// ProtocolV311 is MQTT version 3.1.1
	ProtocolV311 uint8 = 4
	// ProtocolV50 is MQTT version 5.0 (default)
	ProtocolV50 uint8 = 5
)

// willMessage represents the Last Will and Testament message.
type willMessage struct {
	Topic      string
	Payload    []byte
	QoS        uint8
	Retained   bool
	Properties *Properties
}

// Option is a functional option for configuring the client.
type Option func(*clientOptions)

// WithClientID sets the client identifier.
//
// The client ID uniquely identifies this client to the MQTT server.
//
// Empty client ID behavior (MQTT v3.1.1 spec):
//   - With CleanSession=true: Server will auto-generate a unique ID
//   - With CleanSession=false: Server will reject the connection (identifier rejected)
//
// For persistent sessions (CleanSession=false), you MUST provide a non-empty client ID.
func WithClientID(id string) Option {
	return func(o *clientOptions) {
		o.ClientID = id
	}
}

// WithCredentials sets the username and password for authentication.
func WithCredentials(username, password string) Option {
	return func(o *clientOptions) {
		o.Username = username
		o.Password = password
	}
}

// WithKeepAlive sets the MQTT keep alive interval (default: 60s).
func WithKeepAlive(duration time.Duration) Option {
	return func(o *clientOptions) {
		o.KeepAlive = duration
	}
}

// WithCleanSession sets the clean session flag.
//
// When set to true (default), the server will discard any previous session state
// and subscriptions for this client ID. Each connection starts fresh.
//
// When set to false, the server maintains session state across disconnections:
//   - Subscriptions persist and are restored on reconnect
//   - QoS 1 and 2 messages sent while offline are queued for delivery
//   - The client MUST use a non-empty client ID (via WithClientID)
//   - The server will reject the connection if client ID is empty
//
// MQTT v3.1.1 vs v5.0 Differences:
//   - In v3.1.1: false ("Clean Session") means the session persists indefinitely after disconnect.
//   - In v5.0: false ("Clean Start") means the session RESUMES on connect, but EXPIRES immediately on disconnect unless WithSessionExpiryInterval is set.
//
// To achieve persistent sessions in MQTT v5.0, you must use both:
//
//	mq.WithCleanSession(false)
//	mq.WithSessionExpiryInterval(0xFFFFFFFF) // Persist indefinitely
//
// Use false for reliable message delivery across network interruptions.
// Use true for stateless clients or when you don't need message persistence.
//
// Example (persistent session):
//
//	client, err := mq.Dial("tcp://localhost:1883",
//	    mq.WithClientID("sensor-1"),        // Required for CleanSession=false
//	    mq.WithCleanSession(false))
func WithCleanSession(clean bool) Option {
	return func(o *clientOptions) {
		o.CleanSession = clean
	}
}

// WithAutoReconnect enables or disables automatic reconnection (default: true).
func WithAutoReconnect(enable bool) Option {
	return func(o *clientOptions) {
		o.AutoReconnect = enable
	}
}

// WithConnectTimeout sets the connection timeout (default: 30s).
func WithConnectTimeout(duration time.Duration) Option {
	return func(o *clientOptions) {
		o.ConnectTimeout = duration
	}
}

// WithTLS sets the TLS configuration for secure connections.
// Pass nil for default TLS settings, or provide a custom *tls.Config.
// The server URL should use "tls://", "ssl://", or "mqtts://" scheme, or this option
// will enable TLS for "tcp://" URLs as well.
func WithTLS(config *tls.Config) Option {
	return func(o *clientOptions) {
		o.TLSConfig = config
	}
}

// WithProtocolVersion sets the MQTT protocol version to use.
// Use ProtocolV50 (default) for MQTT v5.0 or ProtocolV311 for MQTT v3.1.1.
//
// Example for v3.1.1 server:
//
//	client, _ := mq.Dial("tcp://localhost:1883", mq.WithProtocolVersion(mq.ProtocolV311))
func WithProtocolVersion(version uint8) Option {
	return func(o *clientOptions) {
		o.ProtocolVersion = version
	}
}

// WithRequestProblemInformation requests that the server include detailed
// problem information (ReasonString and UserProperties) in error responses.
//
// Only applicable for MQTT v5.0 connections. When set to true, the server
// should include diagnostic information in CONNACK, PUBACK, PUBREC, PUBREL,
// PUBCOMP, SUBACK, UNSUBACK, and DISCONNECT packets when errors occur.
//
// This is useful for debugging and understanding server behavior, but may
// increase bandwidth usage. Most servers send problem information by default.
//
// This option is ignored when using MQTT v3.1.1.
//
// Example:
//
//	client, _ := mq.Dial("tcp://localhost:1883",
//	    mq.WithProtocolVersion(mq.ProtocolV50),
//	    mq.WithRequestProblemInformation(true))
func WithRequestProblemInformation(request bool) Option {
	return func(o *clientOptions) {
		o.RequestProblemInformation = request
	}
}

// WithRequestResponseInformation requests that the server provide response
// information in the CONNACK packet.
//
// Only applicable for MQTT v5.0 connections. When set to true, the server
// may include a ResponseInformation string that the client can use as the
// basis for creating response topics in request/response patterns.
//
// This is useful in multi-tenant or managed cloud environments where the
// server controls topic naming conventions.
//
// This option is ignored when using MQTT v3.1.1.
//
// Example:
//
//	client, _ := mq.Dial("tcp://localhost:1883",
//	    mq.WithProtocolVersion(mq.ProtocolV50),
//	    mq.WithRequestResponseInformation(true))
//
//	if respInfo := client.ResponseInformation(); respInfo != "" {
//	    // Use server's suggested prefix
//	    responseTopic := respInfo + "my-responses"
//	}
func WithRequestResponseInformation(request bool) Option {
	return func(o *clientOptions) {
		o.RequestResponseInformation = request
	}
}

// WithTopicAliasMaximum sets the maximum number of topic aliases the client
// will accept from the server when receiving PUBLISH messages.
//
// Only applicable for MQTT v5.0. This value is sent in the CONNECT packet
// to tell the server how many aliases it can send to the client.
//
// The server will also send its own TopicAliasMaximum in CONNACK, which tells
// the client how many aliases it can send to the server when publishing.
// These are independent values - one for each direction.
//
// Topic aliases allow short numeric IDs to be used instead of full topic names,
// reducing bandwidth usage for frequently published topics.
//
// Values:
//   - 0: Topic aliases disabled (default)
//   - 1-65535: Maximum number of aliases to accept from server
//
// This option is ignored when using MQTT v3.1.1.
//
// Example:
//
//	client, _ := mq.Dial("tcp://localhost:1883",
//	    mq.WithProtocolVersion(mq.ProtocolV50),
//	    mq.WithTopicAliasMaximum(100))  // Accept up to 100 aliases from server
//
//	// Publishing with alias (uses server's limit from CONNACK)
//	client.Publish("sensors/building-a/floor-3/room-42/temperature", data,
//	    mq.WithAlias())
//
//	// Receiving with alias (uses our declared limit of 100)
//	client.Subscribe("sensors/#", mq.AtLeastOnce, func(c *mq.Client, msg mq.Message) {
//	    // Topic is automatically resolved from alias
//	    fmt.Printf("Topic: %s\n", msg.Topic)
//	})
func WithTopicAliasMaximum(max uint16) Option {
	return func(o *clientOptions) {
		o.TopicAliasMaximum = max
	}
}

// LimitPolicy determines how the client enforces limits (like ReceiveMaximum).
type LimitPolicy int

const (
	// LimitPolicyIgnore logs a warning once per connection but continues processing.
	LimitPolicyIgnore LimitPolicy = iota

	// LimitPolicyStrict sends a DISCONNECT with Reason Code 0x93 (Receive Maximum exceeded)
	// when the limit is reached.
	//
	// Note: Auto-reconnect should be disabled or carefully managed when using this policy,
	// as a misbehaving server could cause an infinite loop of connect -> overflow -> disconnect.
	LimitPolicyStrict
)

// WithReceiveMaximum sets the maximum number of unacknowledged QoS 1 and QoS 2
// messages the client is willing to process concurrently.
//
// Only applicable for MQTT v5.0. This value is sent in the CONNECT packet.
// The default value is 65535 (maximum allowed by spec).
//
// This allows the client to limit the number of messages it has to manage
// buffering for. If the client cannot process messages fast enough, it can
// lower this value to apply backpressure to the server.
//
// The policy argument determines behavior when the limit is exceeded:
//   - LimitPolicyIgnore (recommended): Log a warning once and continue processing.
//     This protects the client from disconnecting due to server bugs, while still
//     processing messages (potentially unbounded).
//   - LimitPolicyStrict: Disconnect with Reason Code 0x93.
//     Use this if strict flow control compliance is required.
//
// This option is ignored when using MQTT v3.1.1.
func WithReceiveMaximum(max uint16, policy LimitPolicy) Option {
	return func(o *clientOptions) {
		o.ReceiveMaximum = max
		o.ReceiveMaximumPolicy = policy
	}
}

// WithSessionExpiryInterval sets how long the server should maintain session
// state after the client disconnects (in seconds).
//
// Only applicable for MQTT v5.0. For v3.1.1, use WithCleanSession instead.
//
// Values:
//
//   - 0: Session ends immediately on disconnect (can be explicitly set)
//
//   - 1-4294967294: Session persists for this many seconds
//
//   - 4294967295 (0xFFFFFFFF): Session never expires
//
// The server may override this value (e.g., to enforce a maximum limit).
// Use client.SessionExpiryInterval() after connecting to see the actual
// value negotiated with the server.
//
// Note: This can be combined with WithCleanSession(false) to resume a
// previous session while also controlling how long it persists.
//
// This option is ignored when using MQTT v3.1.1.
//
// Example (short-lived session for mobile app):
//
//	client, _ := mq.Dial("tcp://localhost:1883",
//	    mq.WithProtocolVersion(mq.ProtocolV50),
//	    mq.WithCleanSession(false),
//	    mq.WithSessionExpiryInterval(300))  // 5 minutes
//
// Example (long-lived session for IoT device):
//
//	client, _ := mq.Dial("tcp://localhost:1883",
//	    mq.WithProtocolVersion(mq.ProtocolV50),
//	    mq.WithCleanSession(false),
//	    mq.WithSessionExpiryInterval(86400))  // 24 hours
func WithSessionExpiryInterval(seconds uint32) Option {
	return func(o *clientOptions) {
		o.SessionExpiryInterval = seconds
		o.SessionExpirySet = true
	}
}

// WithConnectUserProperties sets the User Properties to be sent in the CONNECT packet.
//
// Only applicable for MQTT v5.0 connections. User Properties are key-value pairs
// that allow the client to send custom metadata to the server during the connection
// handshake.
//
// This option is ignored when using MQTT v3.1.1.
//
// Example:
//
//	client, _ := mq.Dial("tcp://localhost:1883",
//	    mq.WithProtocolVersion(mq.ProtocolV50),
//	    mq.WithConnectUserProperties(map[string]string{
//	        "region": "us-east-1",
//	        "app-version": "1.0.2",
//	    }))
func WithConnectUserProperties(props map[string]string) Option {
	return func(o *clientOptions) {
		if o.ConnectUserProperties == nil {
			o.ConnectUserProperties = make(map[string]string)
		}
		for k, v := range props {
			o.ConnectUserProperties[k] = v
		}
	}
}

// WithDefaultPublishHandler sets a fallback handler for incoming PUBLISH messages
// that do not match any registered subscription.
//
// This is useful for:
//   - Handling messages received during reconnection race conditions
//   - Handling persistent subscriptions restored without a registered handler (orphans)
//   - Debugging or logging unexpected messages
//   - Implementing a catch-all strategy
//
// If not set (default), messages matching no subscription are silently dropped
// (but still acknowledged to comply with the protocol).
//
// Example:
//
//	client, _ := mq.Dial(uri,
//	    mq.WithDefaultPublishHandler(func(c *mq.Client, msg mq.Message) {
//	        log.Printf("Unexpected message on %s: %s", msg.Topic, msg.Payload)
//	    }),
//	)
func WithDefaultPublishHandler(handler MessageHandler) Option {
	return func(o *clientOptions) {
		o.DefaultPublishHandler = handler
	}
}

// WithLogger sets a custom logger for the client.
// If not provided, the client will use a logger that discards all output.
// Use this to integrate with your application's logging system.
//
// Example:
//
//	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
//	    Level: slog.LevelDebug,
//	}))
//	client, _ := mq.Dial("tcp://localhost:1883", mq.WithLogger(logger))
func WithLogger(logger *slog.Logger) Option {
	return func(o *clientOptions) {
		o.Logger = logger
	}
}

// WithDialer sets a custom dialer for establishing the network connection.
// This enables support for alternative transports like WebSockets, Unix sockets,
// or proxying, without adding dependencies to the core library.
//
// If provided, the library will skip its standard scheme validation and
// delegate the connection creation entirely to the dialer.
//
// The dialer's DialContext method receives:
//   - ctx: The context provided to DialContext (or one created from WithConnectTimeout if using Dial)
//   - network: The scheme from the server URL (e.g. "ws", "tcp", "unix")
//   - addr: The original server string passed to Dial
//
// Example (WebSockets using nhooyr.io/websocket):
//
//	client, _ := mq.Dial("ws://server.example.com/mqtt",
//	    mq.WithDialer(mq.DialFunc(func(ctx context.Context, network, addr string) (net.Conn, error) {
//	        c, _, err := websocket.Dial(ctx, addr, &websocket.DialOptions{
//	            Subprotocols: []string{"mqtt"}, // Crucial for MQTT over WebSockets
//	        })
//	        if err != nil {
//	            return nil, err
//	        }
//	        return websocket.NetConn(ctx, c, websocket.MessageBinary), nil
//	    })))
func WithDialer(dialer ContextDialer) Option {
	return func(o *clientOptions) {
		o.Dialer = dialer
	}
}

// DialFunc is a helper to convert a function to the ContextDialer interface.
type DialFunc func(ctx context.Context, network, addr string) (net.Conn, error)

// DialContext implements ContextDialer.
func (f DialFunc) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	return f(ctx, network, addr)
}

// WithWill sets the Last Will and Testament (LWT) message.
//
// The LWT is a message that the MQTT server will automatically publish on behalf
// of the client if the client disconnects unexpectedly (e.g., network failure,
// crash, or power loss). It is NOT sent on graceful disconnects via Disconnect().
//
// This is commonly used to notify other clients that a device has gone offline.
//
// Parameters:
//   - topic: The topic to publish the will message to
//   - payload: The message content (e.g., "offline", "disconnected")
//   - qos: Quality of Service level (0, 1, or 2)
//   - retained: Whether the will message should be retained by the server
//
// The will message is sent by the server when:
//   - The client fails to send a PINGREQ within the keepalive period
//   - The network connection is lost without a proper DISCONNECT packet
//   - The client crashes or loses power
//
// The will message is NOT sent when:
//   - The client calls Disconnect() normally
//   - The connection is closed gracefully
//
// Example (status monitoring):
//
//	client, err := mq.Dial("tcp://localhost:1883",
//	    mq.WithClientID("sensor-1"),
//	    mq.WithWill("devices/sensor-1/status", []byte("offline"), 1, true))
//
// Other clients can subscribe to "devices/+/status" to monitor device connectivity.
// WithWill sets the Last Will and Testament message.
// The properties argument is optional and can be used to set Will Properties (MQTT v5.0).
func WithWill(topic string, payload []byte, qos uint8, retained bool, properties ...*Properties) Option {
	return func(o *clientOptions) {
		o.will = &willMessage{
			Topic:    topic,
			Payload:  payload,
			QoS:      qos,
			Retained: retained,
		}
		if len(properties) > 0 && properties[0] != nil {
			o.will.Properties = properties[0]
		}
	}
}

// WithOnConnect sets the handler to be called when the client connects.
// This is called for the initial connection and every successful reconnection.
//
// The handler is invoked asynchronously in a separate goroutine. This allows
// implementing complex setup logic (e.g., subscribing, publishing) without
// blocking the connection process or logic loop.
func WithOnConnect(onConnect func(*Client)) Option {
	return func(o *clientOptions) {
		o.OnConnect = onConnect
	}
}

// WithOnConnectionLost sets the handler to be called when the connection is lost.
// The error parameter provides the reason for disconnection.
//
// The handler is invoked asynchronously in a separate goroutine to ensure
// it does not block internal cleanup or reconnection attempts.
func WithOnConnectionLost(onConnectionLost func(*Client, error)) Option {
	return func(o *clientOptions) {
		o.OnConnectionLost = onConnectionLost
	}
}

// WithOnServerRedirect sets the handler to be called when the server provides
// a redirection reference (MQTT v5.0 only).
//
// The server can send a ServerReference in the CONNACK or DISCONNECT packet to
// suggest that the client connect to a different server. This is used for:
//   - Load balancing: Distribute clients across multiple servers
//   - Maintenance: Move clients before server shutdown
//   - Geographic routing: Direct clients to nearest server
//   - Failover: Redirect to backup server
//
// The handler receives the server URI as provided by the server. The client
// does NOT automatically redirect - the handler should decide whether to accept
// the redirect and manually reconnect if desired.
//
// The handler is invoked asynchronously in a separate goroutine to prevent
// blocking the processing of the CONNACK packet.
//
// The server reference is also available via the ServerReference() method for
// polling/checking later.
//
// This option is only relevant for MQTT v5.0 connections. For v3.1.1, the
// callback will never be invoked.
//
// Example:
//
//	client, err := mq.Dial("tcp://server1.example.com:1883",
//	    mq.WithOnServerRedirect(func(newServer string) {
//	        log.Printf("Server suggests redirect to: %s", newServer)
//	        // Application decides whether to reconnect
//	    }))
func WithOnServerRedirect(onServerRedirect func(serverURI string)) Option {
	return func(o *clientOptions) {
		o.OnServerRedirect = onServerRedirect
	}
}

// WithAuthenticator sets the authenticator for enhanced authentication (MQTT v5.0).
//
// Enhanced authentication allows challenge/response authentication mechanisms
// such as SCRAM, OAuth, Kerberos, or custom methods. The authenticator handles
// the authentication exchange via the AUTH packet flow.
//
// If set, the client will:
//  1. Send AuthenticationMethod + InitialData in CONNECT
//  2. Handle AUTH challenges from the server via HandleChallenge
//  3. Complete authentication when CONNACK is received
//
// This option is only relevant for MQTT v5.0. For v3.1.1, use WithCredentials
// for simple username/password authentication.
//
// Example:
//
//	auth := &MyScramAuthenticator{username: "user", password: "pass"}
//	client, err := mq.Dial("tcp://localhost:1883",
//	    mq.WithProtocolVersion(mq.ProtocolV50),
//	    mq.WithAuthenticator(auth))
func WithAuthenticator(auth Authenticator) Option {
	return func(o *clientOptions) {
		o.Authenticator = auth
	}
}

// DisconnectOptions holds configuration for a disconnection.
type DisconnectOptions struct {
	ReasonCode ReasonCode
	Properties *Properties
}

// DisconnectOption is a functional option for configuring a disconnection.
type DisconnectOption func(*DisconnectOptions)

// WithReason sets the reason code for the DISCONNECT packet (MQTT v5.0).
// Common codes include:
//   - 0x00: Normal Disconnect (default)
//   - 0x04: Disconnect with Will Message
//
// This option is ignored when using MQTT v3.1.1.
func WithReason(code ReasonCode) DisconnectOption {
	return func(o *DisconnectOptions) {
		o.ReasonCode = code
	}
}

// WithDisconnectProperties sets the MQTT v5.0 properties for the DISCONNECT packet.
//
// This can be used to set:
//   - Session Expiry Interval (to update expiry on disconnect)
//   - Reason String (diagnostic information)
//   - User Properties (custom metadata)
//
// This option is ignored when using MQTT v3.1.1.
func WithDisconnectProperties(props *Properties) DisconnectOption {
	return func(o *DisconnectOptions) {
		o.Properties = props
	}
}

// WithSubscription defines a subscription that the client should maintain.
//
// This serves two purposes:
//  1. Registers the MessageHandler locally before connection (preventing race conditions).
//  2. Automatically subscribes to the topic on connection/reconnection if needed.
//
// For persistent sessions (CleanSession=false):
//   - If SessionPresent=true: The server has the subscription; we just register the handler locally.
//   - If SessionPresent=false: The client will automatically resubscribe to this topic.
//
// For clean sessions (CleanSession=true):
//   - The client will automatically subscribe to this topic on every connection.
func WithSubscription(topic string, handler MessageHandler) Option {
	return func(o *clientOptions) {
		if o.InitialSubscriptions == nil {
			o.InitialSubscriptions = make(map[string]MessageHandler)
		}
		o.InitialSubscriptions[topic] = handler
	}
}

// WithSessionStore sets a custom session store for persistence.
//
// If set, session state (pending publishes, subscriptions, received QoS 2 IDs)
// will be persisted across process restarts. This enables the client to resume
// unacknowledged messages and subscriptions after a crash or reboot.
//
// The store is only loaded when the process starts (not on network reconnects).
// During normal reconnections, the in-memory state is used directly.
//
// Example with file-based storage:
//
//	store, err := mq.NewFileStore("/var/lib/mqtt", "sensor-1")
//	if err != nil {
//	    log.Fatal(err)
//	}
//
//	client, err := mq.Dial("tcp://localhost:1883",
//	    mq.WithClientID("sensor-1"),
//	    mq.WithCleanSession(false),
//	    mq.WithSessionStore(store))
func WithSessionStore(store SessionStore) Option {
	return func(o *clientOptions) {
		o.SessionStore = store
	}
}

// defaultOptions returns the default client options.
func defaultOptions(server string) *clientOptions {
	return &clientOptions{
		Server:          server,
		ClientID:        "",
		KeepAlive:       60 * time.Second,
		CleanSession:    true,
		ProtocolVersion: ProtocolV50,
		AutoReconnect:   true,
		ConnectTimeout:  30 * time.Second,
		Logger:          slog.New(slog.NewTextHandler(io.Discard, nil)),

		// Use MQTT spec defaults (0 = use defaults in validation functions)
		MaxTopicLength:    0,
		MaxPayloadSize:    0,
		MaxIncomingPacket: 0,
	}
}
