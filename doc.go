// Package mq provides a lightweight, idiomatic MQTT v5.0 and v3.1.1 client library for Go.
//
// The library is designed with zero external dependencies (only Go standard library)
// and provides a clean, functional options-based API for connecting to MQTT servers,
// publishing messages, and subscribing to topics.
//
// # Features
//
//   - Full MQTT v5.0 and v3.1.1 support
//   - (v5.0) User Properties & Packet Properties
//   - (v5.0) Topic Aliases (auto-managed)
//   - (v5.0) Request/Response pattern support
//   - (v5.0) Session & Message Expiry
//   - (v5.0) Shared Subscriptions
//   - (v5.0) Reason Codes & Enhanced Error Handling
//   - TLS/SSL encrypted connections
//   - Automatic reconnection with exponential backoff
//   - Clean, idiomatic Go API with functional options
//   - Context-based cancellation and timeouts
//   - Zero external dependencies (main library)
//
// # Unified API Philosophy
//
// The library exposes a single, unified API that embraces modern MQTT v5.0 concepts
// (Properties, Reason Codes, Session Expiry). When connecting to an MQTT v3.1.1 server,
// these v5-specific features are handled gracefully—they are simply ignored during
// packet encoding. This allows you to write code once using modern idioms while
// maintaining compatibility with older servers.
//
// # Quick Start
//
// Connect to a server and publish a message:
//
//	client, err := mq.Dial("tcp://localhost:1883",
//	    mq.WithClientID("my-client"),
//	    mq.WithProtocolVersion(mq.ProtocolV50)) // Use MQTT v5.0
//	if err != nil {
//	    log.Fatal(err)
//	}
//	defer client.Disconnect(context.Background())
//
//	token := client.Publish("sensors/temperature", []byte("22.5"), mq.WithQoS(1))
//	err = token.Wait(context.Background())  // 'select' also supported, see further down
//
// Subscribe to a topic:
//
//	client.Subscribe("sensors/+/temperature", mq.AtLeastOnce,
//	    func(c *mq.Client, msg mq.Message) {
//	        fmt.Printf("%s: %s\n", msg.Topic, string(msg.Payload))
//	    })
//
// # Connection Options
//
// The Dial and DialContext functions accept various options to configure the client:
//
//   - WithProtocolVersion(v) - Set MQTT version (ProtocolV50 or ProtocolV311)
//   - WithClientID(id) - Set the MQTT client identifier
//   - WithCredentials(user, pass) - Set username and password
//   - WithKeepAlive(duration) - Set keepalive interval (default: 60s)
//   - WithCleanSession(bool) - Set clean start/session flag
//   - WithSessionExpiryInterval(secs) - Set session expiry (v5.0)
//   - WithAutoReconnect(bool) - Enable auto-reconnect (default: true)
//   - WithTLS(config) - Enable TLS encryption
//   - WithWill(topic, payload, qos, retained) - Set Last Will and Testament
//
// # TLS Connections
//
// The library supports TLS/SSL encrypted connections:
//
//	client, err := mq.Dial("tls://server:8883",
//	    mq.WithClientID("secure-client"),
//	    mq.WithTLS(&tls.Config{
//	        InsecureSkipVerify: false,
//	    }))
//
// Supported URL schemes: tcp://, mqtt://, tls://, ssl://, mqtts://
//
// # Quality of Service
//
// The library supports all three MQTT QoS levels:
//
//   - QoS 0 (mq.AtMostOnce): At most once delivery (fire and forget)
//   - QoS 1 (mq.AtLeastOnce): At least once delivery (acknowledged)
//   - QoS 2 (mq.ExactlyOnce): Exactly once delivery (assured)
//
// Example:
//
//	// Using named constants (recommended)
//	client.Publish("topic", []byte("data"), mq.WithQoS(mq.AtLeastOnce))
//
//	// Using numeric values
//	client.Publish("topic", []byte("data"), mq.WithQoS(1))
//
// # Wildcard Subscriptions
//
// MQTT supports two wildcard characters in topic filters:
//
//   - '+' matches a single level (e.g., "sensors/+/temperature")
//   - '#' matches multiple levels (e.g., "sensors/#")
//
// Example:
//
//	// Subscribe to all temperature sensors
//	client.Subscribe("sensors/+/temperature", mq.AtLeastOnce, handler)
//
//	// Subscribe to all sensor data
//	client.Subscribe("sensors/#", mq.AtMostOnce, handler)
//
// # MQTT v5.0 Properties
//
// MQTT v5.0 introduces "Properties" that can be attached to packets. This
// library provides a clean API for using common properties:
//
//	client.Publish("sensors/temp", payload,
//	    mq.WithContentType("application/json"),
//	    mq.WithUserProperty("sensor-id", "temp-01"),
//	    mq.WithMessageExpiry(3600)) // Expire in 1 hour
//
// Supported properties include:
//   - ContentType: Specifies the MIME type of the payload
//   - MessageExpiry: How long the message should be kept by the server
//   - UserProperties: Custom key-value pairs (metadata)
//   - ResponseTopic & CorrelationData: For request/response patterns
//
// # Topic Aliases
//
// Topic Aliases (v5.0) allow reducing bandwidth by using a short numeric ID
// instead of the full topic string for repeated publications.
//
//	// Enable topic alias for this publication
//	client.Publish("very/long/topic/name/for/bandwidth/saving", data,
//	    mq.WithAlias())
//
// The library automatically manages alias assignment and mapping.
//
// # Subscription Options (v5.0)
//
// MQTT v5.0 adds options to control subscription behavior:
//
//   - WithNoLocal: Don't receive messages you published yourself
//   - WithRetainAsPublished: Keep the original retain flag from the publisher
//   - WithRetainHandling: Control when the server sends retained messages
//   - WithSubscriptionIdentifier: Set a numeric identifier for the subscription
//   - WithSubscribeUserProperty: Add custom metadata to the subscription
//
// Example:
//
//	client.Subscribe("chat/room", mq.AtLeastOnce, handler,
//	    mq.WithNoLocal(true))
//
// # Client-side Session Persistence
//
// The library supports pluggable session persistence to save pending messages (QoS 1 & 2)
// and subscriptions across restarts.
//
//	store, _ := mq.NewFileStore("/path/to/persist", "client-id")
//	client, _ := mq.Dial(server,
//	    mq.WithClientID("client-id"),
//	    mq.WithCleanSession(false),
//	    mq.WithSessionStore(store),
//	    // persistent subscription
//	    mq.WithSubscription("topic", handler),
//	)
//
// # Error Handling
//
// Operations return a Token that can be used for both blocking and non-blocking
// error handling. In MQTT v5.0, errors often include Reason Codes.
//
//	// Blocking with timeout
//	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
//	defer cancel()
//	if err := token.Wait(ctx); err != nil {
//	    // Check for specific reason codes (v5.0)
//		// You can also use mq.ReasonCodeUnspecifiedError for 0x80
//	    if mq.IsReasonCode(err, 0x80) { // e.g. Unspecified Error
//	        log.Printf("Server rejected operation: %v", err)
//	    }
//	}
//
//	// Non-blocking with select
//	select {
//	case <-token.Done():
//	    if err := token.Error(); err != nil {
//	        log.Printf("Failed: %v", err)
//	    }
//	case <-time.After(5 * time.Second):
//	    log.Println("Timeout")
//	}
//
//	// Connection can be closed with a specific reason code and properties (MQTT v5.0):
//
//	expiry := uint32(3600)
//	client.Disconnect(ctx,
//	    mq.WithReason(mq.ReasonCodeNormalDisconnect),
//	    mq.WithDisconnectProperties(&mq.Properties{
//	        SessionExpiryInterval: &expiry,
//	        ReasonString:          "Shutting down",
//	    }),
//	)
//
// The client handles reconnection automatically unless configured otherwise.
package mq
