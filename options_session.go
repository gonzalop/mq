package mq

// willMessage represents the Last Will and Testament message.
type willMessage struct {
	Topic      string
	Payload    []byte
	QoS        uint8
	Retained   bool
	Properties *Properties
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
