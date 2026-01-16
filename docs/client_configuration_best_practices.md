# Client Configuration Best Practices

This guide provides recommendations for configuring the MQTT client for security, reliability, and performance.

## Security & Resource Limits

Efficient resource management is critical for preventing Denial of Service (DoS) and ensuring client stability across different hardware profiles.

### Incoming Packet Size

The library defaults to the MQTT specification maximum of **~256MB**. **This is usually too high for production environments.** A single malicious or malformed packet could exhaust your application's memory.

**Recommendations:**
- **Constrained/IoT:** 64KB - 1MB. Most sensor data is small.
- **General Purpose:** 1MB - 10MB.
- **Server/Bridge:** Use your expected maximum message size + 10%.

```go
// Example: Strict 1MB limit for an IoT device
client, err := mq.Dial(server,
    mq.WithMaxIncomingPacket(1024 * 1024),
)
```

### Topic Alias Maximum (MQTT v5.0)

Topic aliases reduce bandwidth by replacing long strings with numeric IDs. However, the client must store these mappings in memory for the duration of the session.

**Memory Calculation:**
`Aliases * (Avg Topic Length + Map Overhead) = RAM Usage`
*Example: 1000 aliases * (~100 bytes) ≈ 100KB.*

**Recommendations:**
- **Security:** Only enable if you trust the server. A malicious server can force the client to allocate memory for every alias you allow.
- **Embedded:** Keep at `0` (Disabled) or `< 50`.
- **IoT/Mobile:** `100 - 500`.
- **High-Throughput Servers:** `1000+` to maximize bandwidth savings on repetitive topics.

```go
// Example: Conservative IoT limit
client, err := mq.Dial(server,
    mq.WithProtocolVersion(mq.ProtocolV50),
    mq.WithTopicAliasMaximum(100),
)
```

### Receive Maximum (Flow Control - MQTT v5.0)

This setting protects your client from being overwhelmed by a flood of messages. It limits the number of unacknowledged QoS 1 and QoS 2 messages the server can send concurrently.

If your client processes messages slower than they arrive, this "in-flight" window will fill up, forcing the server to pause delivery until you acknowledge processing the current batch.

**Recommendations:**
- **Low Memory / Embedded:** `10 - 50`. Keeps the backlog small.
- **High Performance:** `1000+`. Allows higher throughput by keeping the pipeline full.
- **Default:** `65535` (Max). Good for most desktop/server apps but risky for constrained devices.

```go
// Example: Apply backpressure if we have > 50 unprocessed messages
client, err := mq.Dial(server,
    mq.WithProtocolVersion(mq.ProtocolV50),
    mq.WithReceiveMaximum(50),
)
```

For more details on resource-based vulnerabilities, see [SECURITY.md](../SECURITY.md).

---

## TLS Configuration

TLS is mandatory for securing your MQTT username, password, and payload over the wire.

### Standard TLS (Server Verification)
Always use TLS for production. The simplest secure configuration relies on your OS's root CA pool.

```go
// Connect to a public server secured by Let's Encrypt or similar
client, err := mq.Dial("tls://broker.example.com:8883",
    mq.WithTLS(nil), // nil uses default secure settings
)
```

### Mutual TLS (mTLS / Client Certificates)
For high-security or IoT environments where devices authenticate via certificates instead of passwords.

```go
func getMTLSConfig(caFile, certFile, keyFile string) (*tls.Config, error) {
    // 1. Load the CA certificate that signed the server's cert
    caCert, _ := os.ReadFile(caFile)
    caCertPool := x509.NewCertPool()
    caCertPool.AppendCertsFromPEM(caCert)

    // 2. Load client certificate and key
    cert, err := tls.LoadX509KeyPair(certFile, keyFile)
    if err != nil {
        return nil, err
    }

    return &tls.Config{
        RootCAs:      caCertPool,
        Certificates: []tls.Certificate{cert},
        MinVersion:   tls.VersionTLS12,
    }, nil
}

// Usage
tlsConfig, _ := getMTLSConfig("ca.pem", "client.crt", "client.key")
client, err := mq.Dial(server, mq.WithTLS(tlsConfig))
```

### ⚠️ Security Warning
**NEVER** use `InsecureSkipVerify: true` in production. It disables server certificate verification, making your connection vulnerable to Man-in-the-Middle (MitM) attacks. Use it **only** for local testing.

---

## Session Management

Choosing the right session type ensures you don't miss messages during network interruptions.

### Transient vs. Persistent

| Type | Use Case | Behavior |
| :--- | :--- | :--- |
| **Transient** | Publishers, Command-only apps | Fresh state on every connect. Misses messages sent while offline. |
| **Persistent** | Sensors, Chat apps, Critical data | Queues messages while offline. Restores subscriptions on reconnect. |

### Configuration Guide

#### MQTT v3.1.1
Persistence is binary. If `CleanSession` is false, the session lasts forever (or until server limits).

```go
// Persistent Session (v3)
mq.WithCleanSession(false),
mq.WithClientID("unique-device-id"), // Required
```

#### MQTT v5.0 (Recommended)
You have fine-grained control. You can request to resume a session (`CleanStart=false`) but allow it to expire if the client stays offline too long.

```go
// "Resume my session, but discard it if I'm gone for > 1 hour"
mq.WithCleanSession(false),          // Maps to CleanStart=0
mq.WithSessionExpiryInterval(3600),  // 1 hour expiry
mq.WithClientID("unique-device-id"),
```

### Durable Storage (Client-Side)
By default, the client holds session state in memory. If the *application* restarts, it loses track of unacknowledged messages. To survive application restarts, use a session store.

The library provides a built-in `FileStore`, but you can implement the `SessionStore` interface to support other backends like Redis, BoltDB, or a relational database.

```go
// Save state to disk to survive reboots using built-in FileStore
store, _ := mq.NewFileStore("/var/lib/myapp/mqtt", "device-id")

client, err := mq.Dial(server,
    mq.WithSessionStore(store),
    mq.WithSessionExpiryInterval(3600),  // Ignored for MQTT 3.1.1, required for MQTT 5
    mq.WithCleanSession(false),
)
```

---

## Keep Alive Settings

The Keep Alive interval controls how often the client sends a PINGREQ to the server to keep the connection open. It's a trade-off between **detection speed** and **overhead/battery life**.

### Choosing an Interval

| Environment | Recommendation | Rationale |
| :--- | :--- | :--- |
| **Standard (Default)** | `60s` | Good balance for most LAN/WAN scenarios. |
| **Mobile / Battery** | `300s - 600s` | Minimizes radio wake-ups to save significant battery life. |
| **High Availability** | `10s - 30s` | Detects "half-open" (dead) connections quickly to trigger failover. |
| **Unreliable Cellular** | `30s - 60s` | Frequent pings help keep NAT bindings alive in strict firewalls. |

### Impact on "Last Will"
The server waits for roughly **1.5x the Keep Alive interval** before declaring a client dead.
*   *KeepAlive = 60s* → Server detects crash in ~90s.
*   *KeepAlive = 600s* → Server detects crash in ~15 minutes.

If your application relies on **Last Will & Testament (LWT)** to notify users of device failures, use a shorter interval (e.g., 10-30s).

### Internal Implementation & Timing

The library uses a proactive heartbeat mechanism to detect dead connections before the server does. It's important to understand the timing for production monitoring:

- **Ping Frequency:** The client sends a `PINGREQ` if no other traffic has been sent for **75% of the Keep Alive interval**. This proactive approach ensures we adhere to the MQTT spec requirement of sending a packet within the `1.0x` interval, while allowing a 25% safety buffer for network latency.
- **Failure Detection:** The client declares a connection "lost" if no packet (including `PINGRESP`) is received for **150% of the Keep Alive interval**. This matches the "grace period" that MQTT servers use to detect dead clients, providing a consistent failure model across the system.
- **Timing Resolution:** The internal timer checks state every `KeepAlive / 4`.

**What this means for you:**
Because of the resolution and the 1.5x timeout window, there is a **worst-case and best-case delay** for detecting a silent failure (like a physical cable cut):
- **Best case:** Detected almost exactly at `1.5x KeepAlive`.
- **Worst case:** Could take up to `1.5x KeepAlive + (KeepAlive / 4)` due to the polling resolution.

*Example:* With a 60s Keep Alive, the client will realize the connection is dead and start reconnecting within **90 to 105 seconds**.

---

## QoS Selection

MQTT provides three Quality of Service (QoS) levels to balance delivery reliability against performance and bandwidth.

| QoS Level | Guarantee | Overhead | Best For... |
| :--- | :--- | :--- | :--- |
| **0** (At Most Once) | Best effort, no ACK | Lowest | High-frequency telemetry (e.g., CPU stats, ambient temperature). |
| **1** (At Least Once) | Guaranteed delivery | Moderate | **Most use cases.** Commands, alerts, status changes. |
| **2** (Exactly Once) | Exactly one delivery | Highest | Critical non-idempotent actions (e.g., billing, toggle commands). |

### QoS 1: The Practical Choice
QoS 1 is the standard for most applications. It ensures the message reaches the server, but may result in **duplicate messages** if the network connection drops exactly during the acknowledgment phase.

**Important:** Your application logic must be **idempotent**. For example, if you receive a "Set Volume to 50" message twice, the result is the same. But if you receive "Increment Volume" twice, the final state will be incorrect.

**Handling Duplicates:** The `Message` struct passed to your handler includes a `Duplicate` flag. If `msg.Duplicate` is true, it indicates that the server has re-sent this message because it did not receive an acknowledgment for a previous attempt.

*Note: While `msg.Duplicate == true` is a strong hint, it is not a guarantee of a duplicate. Always use unique IDs in your payload for robust deduplication.*

### QoS 2: When Duplicates are Forbidden
QoS 2 involves a four-part handshake. It is significantly slower and heavier on bandwidth. Use it only if your application cannot handle duplicates and the logic cannot be made idempotent.

```go
// Example: Reliable alert (QoS 1)
client.Publish("alerts/fire", []byte("detected"), mq.WithQoS(mq.AtLeastOnce))

// Example: Non-critical sensor data (QoS 0)
client.Publish("sensors/temp", []byte("22.5"), mq.WithQoS(mq.AtMostOnce))
```

---

## Topic Design

Well-designed topics make your system easier to monitor, secure, and scale.

### Naming Conventions

- **Use Hierarchies:** Organize topics from general to specific.
  `[building]/[floor]/[room]/[device]/[sensor]` → `hq/f3/r301/ac/temp`
- **Avoid Leading Slashes:** Do not use `/home/sensor`. The leading slash creates an unnecessary empty level at the root and is considered poor practice. Use `home/sensor` instead.
- **Lowercase & Alphanumeric:** Use only alphanumeric characters, dashes (`-`), and underscores (`_`). Stick to lowercase to avoid accidental mismatches, as topics are **case-sensitive**.
- **No Spaces:** Topics with spaces are difficult to debug and integrate with other systems.

### Restricted & System Topics

- **$SYS Topics:** Topics starting with `$` are reserved for internal server statistics (e.g., `$SYS/broker/uptime`).
- **Permissions:** Most servers require administrative privileges to subscribe to `$SYS` topics. Similarly, ensure your application topics are restricted via Access Control Lists (ACLs) on the server to prevent unauthorized snooping.

### Performance & Wildcards

- **Multi-level Wildcards (#):** Subscribing to `#` at a high level (e.g., `home/#`) on a busy server can overwhelm your client with thousands of messages. Be specific.
- **Topic Aliases (MQTT v5.0):** If your topic names are long and descriptive, use Topic Aliases to reduce bandwidth. The library handles the mapping automatically: the first call sends the full topic + alias ID, while subsequent calls (to the same topic string) send only the alias ID.

```go
// Example: The first call sends the full topic. Subsequent calls using the
// same topic string and mq.WithAlias() will automatically send only the
// numeric alias ID to save bandwidth.
const longTopic = "telemetry/v1/sensors/locations/california/san-francisco/building-4/room-202/temp"
client.Publish(longTopic, []byte("22.5"), mq.WithAlias())
```

---

## Connection Resilience

Networks are unreliable. Your client configuration should assume connections will drop.

### Automatic Reconnection
The client enables `WithAutoReconnect(true)` by default. It uses an **exponential backoff** strategy (starting at 1s, doubling up to 2m) to avoid hammering the server during outages.

**Best Practice:** Do not disable this unless you have a specific reason to implement your own recovery logic.

### Monitoring Connectivity
Use the lifecycle callbacks to update your application state or UI.

```go
client, err := mq.Dial(server,
    mq.WithOnConnect(func(c *mq.Client) {
        slog.Info("Connected")
    }),
    mq.WithOnConnectionLost(func(c *mq.Client, err error) {
        slog.Error("Connection lost", "error", err)
    }),
)
```

### Last Will & Testament (LWT)
LWT is the standard way to detect when a device has gone offline ungracefully (crash, power loss, network cut). The server publishes this message automatically when the connection breaks.

**Crucial Detail:** The LWT is **NOT** sent if you call `client.Disconnect()` gracefully. It is only sent on unexpected failures.

```go
// Set a retained "Offline" message as the Will
client, err := mq.Dial(server,
    mq.WithWill("status/device-123", []byte("offline"), 1, true),
    mq.WithOnConnect(func(c *mq.Client) {
        // Immediately publish "Online" when we connect
        c.Publish("status/device-123", []byte("online"), mq.WithRetain(true))
    }),
)
```

---

## Performance Tuning

The `mq` library is optimized for high throughput and low latency, but your usage patterns determine the final performance.

### Internal Optimizations
- **Zero-Allocation Hot Paths:** The topic matching and packet encoding paths are designed to generate zero garbage.
- **Buffer Pooling:** The library uses `sync.Pool` for reading packets. It is optimized for standard MQTT packets (**< 4KB**).
- **Packet Limits:** Staying under 4KB for payloads keeps you in the "fast path" of the internal allocator.

### User Optimizations
1.  **Concurrency:** The `Client` is thread-safe. You can call `Publish` from multiple goroutines simultaneously.
2.  **QoS Impact:**
    *   **QoS 0** is essentially "line speed" (limited only by TCP).
    *   **QoS 1** requires round-trip latency (PUBACK).
    *   **QoS 2** requires two round-trips.
3.  **Asynchronous Publishing:** `Publish()` returns a `Token`. For maximum throughput, do not `Wait()` on every token immediately. Launch them and wait in batches or use the `Done()` channel with `select` for timeouts.

```go
token := client.Publish("data", payload, mq.WithQoS(mq.AtLeastOnce))

// Wait for acknowledgment with a timeout using select
select {
case <-token.Done():
    if err := token.Error(); err != nil {
        slog.Error("Publish failed", "error", err)
    }
case <-time.After(5 * time.Second):
    slog.Warn("Timed out waiting for ACK")
}
```

---

## Deployment Scenarios (Copy & Paste Profiles)

### 1. Low-Resource / Embedded Device
Optimized for minimal RAM usage. Disables optional buffers and uses small packet limits.

```go
client, err := mq.Dial("tcp://broker.local:1883",
    mq.WithClientID("tiny-device-01"),
    mq.WithMaxIncomingPacket(8192),    // Limit to 8KB
    mq.WithMaxTopicLength(64),         // Short topics only
    mq.WithTopicAliasMaximum(0),       // Disable alias memory map
    mq.WithReceiveMaximum(20),         // Small in-flight window
    mq.WithCleanSession(true),         // Stateless
    mq.WithKeepAlive(300 * time.Second), // Save radio/power
)
```

### 2. Reliable Field Device (IoT)
Optimized for connectivity persistence and data integrity.

```go
client, err := mq.Dial("tls://broker.cloud.com:8883",
    mq.WithClientID("sensor-field-A1"),
    mq.WithTLS(nil),                   // Standard TLS
    mq.WithCleanSession(false),        // Persistent Session
    mq.WithSessionExpiryInterval(3600),// 1 Hour persistence (v5)
    mq.WithKeepAlive(60 * time.Second),// Standard heartbeat
    mq.WithWill("status/sensor-field-A1", []byte("offline"), 1, true),
    mq.WithSessionStore(myFileStore),  // Durable disk storage
)
```

### 3. High-Performance Backend / Bridge
Optimized for processing massive throughput.

```go
client, err := mq.Dial("tcp://internal-broker:1883",
    mq.WithClientID("backend-service-worker-01"),
    mq.WithTopicAliasMaximum(5000),      // Maximize bandwidth savings
    mq.WithReceiveMaximum(2000),         // Allow high concurrency
    mq.WithMaxIncomingPacket(10 * 1024 * 1024), // 10MB limit
    mq.WithDefaultPublishHandler(myLogHandler),
    mq.WithKeepAlive(10 * time.Second),  // Fast failure detection
)
```

---

## Migration Notes

### From Eclipse Paho
If you are coming from the `paho.mqtt.golang` library, here are the main idiomatic differences:

- **Construction:** Instead of `NewClient()` followed by `Connect()`, use `mq.Dial()`. It establishes the connection and returns a ready client.
- **Context Awareness:** Our `Token.Wait()` requires a `context.Context`. This allows you to handle timeouts and cancellations properly.
- **Error Handling:** Errors are first-class citizens. Check the `Token.Error()` or the return of `Token.Wait()`.

### Protocol Version Migration (v3.1.1 to v5.0)

Upgrading to MQTT v5.0 is as simple as adding `mq.WithProtocolVersion(mq.ProtocolV50)`.

**Graceful Degradation:**
The library is designed to be **v5-first**. You can use v5-specific options (like `WithUserProperty` or `WithReason`) in your code even when connecting to a v3.1.1 server. If the client negotiates a v3.1.1 connection, **v5-only options are safely ignored** and will not be sent, preventing protocol errors.

**Session Persistence Changes:**
Remember that in v3.1.1, `CleanSession(false)` means the session lasts "forever". In v5.0, you must explicitly set `WithSessionExpiryInterval` to a value greater than 0, otherwise the session expires the moment you disconnect.

## Troubleshooting & Common Pitfalls

If you encounter unexpected behavior (e.g., connection flapping, "Zombie" messages, or slow consumers), please refer to our dedicated **[Troubleshooting Guide](troubleshooting.md)**.

---

## Monitoring & Testing

Ensuring your client is healthy in production requires proper visibility and rigorous testing.

### Structured Logging
The library uses Go's standard `log/slog` for structured logging. By default, it discards all logs. In production, you should provide a logger to help diagnose connectivity issues.

```go
logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))

client, err := mq.Dial(server,
    mq.WithLogger(logger),
)
```

### Metrics to Track
At a minimum, we recommend tracking the following indicators:
- **`mqtt_connect_total`**: Count of successful connections.
- **`mqtt_connection_lost_total`**: Count of unexpected disconnections.
- **`mqtt_messages_published_total`**: Total messages attempted/sent.
- **`mqtt_messages_received_total`**: Total messages received by your handlers.

**Implementation Tip:**
You can easily implement these using the library's lifecycle hooks and handlers:

```go
var (
    connectTotal      atomic.Uint64
    connectionLostTotal atomic.Uint64
    msgReceivedTotal  atomic.Uint64
    msgPublishedTotal atomic.Uint64
)

client, err := mq.Dial(server,
    mq.WithOnConnect(func(c *mq.Client) {
        connectTotal.Add(1)
    }),
    // This hook only fires on UNEXPECTED disconnections
    mq.WithOnConnectionLost(func(c *mq.Client, err error) {
        connectionLostTotal.Add(1)
    }),
)
if err != nil {
    slog.Error("Failed to connect", "error", err)
    os.Exit(1)
}

// 1. Track Published Messages
token := client.Publish("data", payload, mq.WithQoS(1))
if err := token.Wait(context.Background()); err != nil {
    slog.Error("Publish failed", "error", err)
} else {
    msgPublishedTotal.Add(1)
}

// 2. Track Received Messages
subToken := client.Subscribe("sensors/+", mq.AtLeastOnce, func(c *mq.Client, msg mq.Message) {
    msgReceivedTotal.Add(1)
    // process message...
})
if err := subToken.Wait(context.Background()); err != nil {
    slog.Error("Subscription failed", "error", err)
}

// 3. Graceful Disconnect (does not trigger OnConnectionLost)
if err := client.Disconnect(context.Background()); err != nil {
    slog.Error("Graceful disconnect failed", "error", err)
}
```

### Testing Recommendations

- **Don't Mock the Library:** The `mq` API is designed to be simple. Instead of creating complex mocks of the `Client` interface, we recommend using a real server for integration tests.
- **Use a Test Server:** Run a local **Mosquitto** instance or use a container engine (**Podman or Docker**) to run `eclipse-mosquitto` for your test suite.
- **Chaos Testing:** Manually stop and start your test server while your client is running to verify that your **Auto-Reconnect** and **Last Will & Testament** logic works correctly under fire.
- **Context Timeouts:** Always use `context.WithTimeout` when calling `token.Wait()` in tests to avoid deadlocks if a server fails to respond.
