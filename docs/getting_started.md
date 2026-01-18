# Getting Started & Usage Guide

This guide covers the core operations of the `mq` library: connecting, publishing, subscribing, and proper resource management.

## Quick Start

```go
package main

import (
    "context"
    "fmt"
    "log/slog"
    "os"
    "time"

    "github.com/gonzalop/mq"
)

func main() {
    // 1. Connect to server
    client, err := mq.Dial(
        "tcp://localhost:1883",
        mq.WithClientID("my-client"),
        mq.WithKeepAlive(60*time.Second),
    )
    if err != nil {
        slog.Error("Failed to connect", "error", err)
        os.Exit(1)
    }
    defer client.Disconnect(context.Background())

    // 2. Subscribe to a topic
    token := client.Subscribe("sensors/+/temperature", mq.AtLeastOnce, func(c *mq.Client, msg mq.Message) {
        fmt.Printf("Topic: %s, Payload: %s\n", msg.Topic, string(msg.Payload))
    })

    // Wait for subscription to complete
    if err := token.Wait(context.Background()); err != nil {
        slog.Error("Subscription failed", "error", err)
        os.Exit(1)
    }

    // 3. Publish a message
    pubToken := client.Publish("sensors/living-room/temperature", []byte("22.5"), mq.WithQoS(mq.AtLeastOnce))
    pubToken.Wait(context.Background())

    time.Sleep(2 * time.Second)
}
```

## Connecting

The `Dial` and `DialContext` functions establish a connection to the MQTT server. They return a `Client` instance that is ready for use.

```go
client, err := mq.Dial(server, options...)
// OR
client, err := mq.DialContext(ctx, server, options...)
```

### Supported URI Schemes
- `tcp://` or `mqtt://` - Unencrypted (default port 1883)
- `tls://`, `ssl://`, or `mqtts://` - Encrypted with TLS (default port 8883)

### Connection Options
- `WithAutoReconnect(bool)` - Enable/disable auto-reconnect (default: true).
- `WithCleanSession(bool)` - Set clean session flag (default: true).
- `WithClientID(id string)` - Set client identifier.
- `WithConnectTimeout(duration time.Duration)` - Set connection timeout (default: 30s).
- `WithCredentials(username, password string)` - Set authentication.
- `WithDefaultPublishHandler(handler)` - Set fallback handler for unexpected messages.
- `WithDialer(d ContextDialer)` - Set custom dialer (e.g. for WebSockets or proxy).
- `WithKeepAlive(duration time.Duration)` - Set MQTT keepalive interval (default: 60s).
- `WithLogger(logger)` - Set custom log/slog Logger.
- `WithMaxIncomingPacket(max int)` - Set maximum incoming packet size (default: 256MB).
- `WithMaxPacketSize(bytes int)` - Set maximum packet size sent in CONNECT properties (v5.0) and enforce limit locally.
- `WithMaxPayloadSize(bytes int)` - Set maximum outgoing payload size (default: 256MB).
- `WithMaxTopicLength(bytes int)` - Set maximum topic length (default: 65535).
- `WithOnConnect(func)` - Set callback for successful connection.
- `WithOnConnectionLost(func)` - Set callback for connection loss.
- `WithProtocolVersion(version uint8)` - Set MQTT protocol version (default: v5.0).
  - `mq.ProtocolV311` (4) - MQTT v3.1.1
  - `mq.ProtocolV50` (5) - MQTT v5.0
- `WithReceiveMaximum(max uint16)` - Set maximum concurrent unacknowledged messages (Flow Control) (v5.0).
- `WithRequestProblemInformation(bool)` - Request extended error details (v5.0).
- `WithRequestResponseInformation(bool)` - Request response topic info (v5.0).
- `WithSessionExpiryInterval(seconds)` - Set session expiration time (v5.0).
- `WithSessionStore(store)` - Set storage backend for persistence.
- `WithSubscription(topic, handler)` - Register persistent subscription.
- `WithTLS(config)` - Set TLS configuration.
- `WithTopicAliasMaximum(max)` - Set max topic aliases to accept (v5.0).
- `WithWill(topic, payload, qos, retained)` - Set Last Will and Testament.

### Example with Limits
```go
client, err := mq.Dial("tcp://localhost:1883",
    mq.WithClientID("my-client"),
    mq.WithProtocolVersion(mq.ProtocolV50),
    mq.WithMaxPayloadSize(1024*1024),  // Limit to 1MB payloads
    mq.WithReceiveMaximum(100),        // Limit concurrent incoming messages
)
```

## Publishing

```go
token := client.Publish(topic, payload, options...)
```

### Options
- `WithQoS(qos uint8)` - Set QoS level (0, 1, or 2). Default is 0.
- `WithRetain(bool)` - Set retain flag. Default is false.

### MQTT v5.0 Options
- `WithAlias()` - Enable topic alias optimization (Client-side).
- `WithContentType(contentType string)` - Set MIME content type.
- `WithCorrelationData(data []byte)` - Set correlation data for matching requests/responses.
- `WithMessageExpiry(seconds uint32)` - Set message expiry interval.
- `WithPayloadFormat(format uint8)` - Set payload format (0=bytes, 1=UTF-8).
- `WithProperties(props *mq.Properties)` - Set multiple properties at once.
- `WithResponseTopic(topic string)` - Set response topic for request/response pattern.
- `WithUserProperty(key, value string)` - Add custom user property.

### Example with Properties
```go
client.Publish("sensors/temp", []byte("22.5"),
    mq.WithQoS(1),
    mq.WithContentType("text/plain"),
    mq.WithUserProperty("sensor-id", "temp-01"),
    mq.WithMessageExpiry(300),
)
```

## Subscribing

```go
token := client.Subscribe(topic, qos, handler, options...)
```

The handler receives messages:
```go
func(c *mq.Client, msg mq.Message) {
    // Process message
}
```

### Options
- `WithPersistence(bool)` - Set persistence (default: true). If false, subscription is ephemeral.
- `WithNoLocal(bool)` - Prevent receiving own messages (v5.0).
- `WithRetainAsPublished(bool)` - Keep Retain flag when forwarding (v5.0).
- `WithRetainHandling(uint8)` - Control when to receive retained messages (0=Always, 1=IfNew, 2=Never) (v5.0).
- `WithSubscriptionIdentifier(id int)` - Set numeric identifier for this subscription (v5.0).
- `WithSubscribeUserProperty(key, value string)` - Add user property (v5.0).

### Wildcard Support
- `+` - Single-level wildcard (e.g., `sensors/+/temperature`)
- `#` - Multi-level wildcard (e.g., `sensors/#`)

### Examples
```go
// Don't receive own messages (NoLocal)
client.Subscribe("chat/room", 1, handler, mq.WithNoLocal(true))

// Control when to receive retained messages (RetainHandling: 2=Never)
client.Subscribe("status/+", 1, handler, mq.WithNoLocal(false), mq.WithRetainHandling(2))
```

## Unsubscribing

```go
token := client.Unsubscribe("sensors/temperature")
token.Wait(context.Background())
```

## Disconnecting

```go
// Basic disconnect
err := client.Disconnect(ctx)

// MQTT v5.0 Disconnect with Reason Code and Properties
err := client.Disconnect(ctx,
    mq.WithReason(mq.ReasonCodeNormalDisconnect),
    mq.WithDisconnectProperties(&mq.Properties{
        ReasonString: "Application shutting down",
    }),
)
```

## Token Interface

All async operations return a `Token` that supports `context.Context` and `select`.

```go
type Token interface {
    Wait(ctx context.Context) error   // Block until complete
    Done() <-chan struct{}            // Channel for select statements
    Error() error                     // Get error after completion
}
```

### Example with Select
```go
token := client.Publish("topic", []byte("data"), mq.WithQoS(mq.AtLeastOnce))

select {
case <-token.Done():
    if err := token.Error(); err != nil {
        slog.Error("Publish failed", "error", err)
    }
case <-time.After(5 * time.Second):
    slog.Warn("Timeout")
}
```

## QoS Levels

The library supports all three MQTT QoS levels:
- **QoS 0** (`mq.AtMostOnce`): Fire and forget.
- **QoS 1** (`mq.AtLeastOnce`): Acknowledged delivery. (Recommended)
- **QoS 2** (`mq.ExactlyOnce`): Assured delivery.

```go
client.Subscribe("sensors/temp", mq.AtLeastOnce, handler)
```

## Protocol Compatibility

- **Defaults to MQTT v5.0**: To connect to a v3.1.1 server, use `mq.WithProtocolVersion(mq.ProtocolV311)`.
- **Graceful Degradation**: v5.0 features (Reason Codes, Properties) are safely ignored when connected to a v3.1.1 server.
