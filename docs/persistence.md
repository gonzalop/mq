# Persistence in `mq`

The `mq` library uses in-memory persistence by default, but also supports durable session persistence, allowing your application to survive restarts and network outages without losing messages or subscriptions.

## Overview

When a client connects with `CleanSession=false`, the server maintains state for that client (identified by its `Client ID`). This state includes:
- **Subscriptions**: The server remembers what topics the client is subscribed to.
- **QoS 1 & 2 Messages**: Messages sent to the client while it is offline are queued **by the server** and delivered upon reconnection.
- **Packet IDs**: To ensure exactly-once delivery (QoS 2).

On the **client side**, `mq` can also persist state to a storage backend (like the local filesystem) using a `SessionStore`. This ensures that outgoing messages pending acknowledgment are not lost if the client process crashes.

## Protocol Differences: MQTT 3.1.1 vs 5.0

While `mq` provides options like `WithCleanSession`, their behavior depends on the protocol version. It is critical to understand these differences to avoid losing session data during a migration to MQTT 5.0.

### MQTT 3.1.1: `CleanSession`
The `CleanSession` flag controls **both** the start and end of a session.
- **`true`**: Start fresh. Discard session on disconnect.
- **`false`**: Resume session if exists. **Keep session forever** (until server limits) on disconnect.

### MQTT 5.0: `CleanStart` + `SessionExpiryInterval`
In MQTT 5, `CleanSession` is renamed to `Clean Start` and only controls the **start**.
- **`true`**: Start fresh.
- **`false`**: Resume session if exists.

**Crucially**, the **end** of the session is controlled by `SessionExpiryInterval`.
- **Default (0)**: Session expires **immediately** on disconnect.
- **Value > 0**: Session persists for specific seconds.
- **0xFFFFFFFF**: Session persists indefinitely (like v3.1.1).

> [!NOTE]
> **Server Override**: The server can override your requested expiry interval (e.g., capping it to a maximum allowed value). The actual value granted is returned in the `CONNACK` packet. You can check `client.SessionExpiryInterval()` after connecting to see the negotiated value.

## Implementation: `SessionStore` Interface

The `SessionStore` interface defines how the library interacts with a storage backend. It includes methods for saving, loading, and deleting pending publishes, subscriptions, and QoS 2 state.

### High-Performance Persistence

To ensure the client's logic loop is never blocked by disk I/O, `mq` supports two key features:

1.  **Incremental Storage**: The default `FileStore` uses an incremental, directory-based format. Instead of rewriting a single large JSON file for every change (O(N)), it writes small, individual files for each packet or subscription (O(1)).
2.  **Asynchronous Persistence**: The `AsyncStore` wrapper allows any `SessionStore` implementation to perform write operations in a background goroutine. By utilizing an unbounded queue, it guarantees that persistence operations never block the client's internal logic loop, completely decoupling library performance from storage backend latency.

## Configuration

To enable persistence, you must:
1.  **Set a Client ID**: The server needs a stable ID to track your session.
2.  **Disable Clean Session**: Tell the server to keep state.
3.  **Configure a Session Store**: Tell the client where to save pending state.

```go
// 1. Create the base store (e.g., FileStore)
baseStore, _ := mq.NewFileStore("./data", "my-client-id")

// 2. Wrap it with AsyncStore for non-blocking I/O
store := mq.NewAsyncStore(baseStore, 1000) // Initial queue capacity of 1000 ops
defer store.Close()

// 3. Connect with the store
client, _ := mq.Dial("tcp://broker:1883",
    mq.WithClientID("my-client-id"),
    mq.WithCleanSession(false),
    mq.WithSessionStore(store),
    mq.WithSubscription("my/topic", myHandler),
)
```

## Built-in `FileStore`

The `FileStore` saves state to individual JSON files within a directory structure:

```text
data/
  my-client-id/
    pending/        # Pending QoS 1/2 publishes
      1.json
      2.json
    subscriptions/  # Active subscriptions (base64 encoded)
      bXkvdG9waWM=.json
    qos2/           # Received QoS 2 packet IDs
      1.json
```

This structure allows for extremely fast updates and removals, as only the affected file needs to be modified.

## "Client Alive" vs. "Client Restart"

It is crucial to understand the difference between a network reconnection and a process restart.

### Scenario 1: Client Alive (Network Reconnection)
The client process stays running, but the network connection drops and is re-established.
- **In-Memory State**: Preserved. The client knows all its subscriptions and their handlers.
- **Behavior**: The client reconnects. If `SessionPresent=true` (server kept state), we are good. If `SessionPresent=false` (server lost state), the client automatically re-subscribes to everything it knows about.
- **Handlers**: **Preserved**. Your message handlers continue to work.

### Scenario 2: Client Restart (Process Crash/Reboot)
The client process terminates and starts fresh.
- **In-Memory State**: Lost. Function pointers (handlers) are gone.
- **Persistence State**: Loaded from `SessionStore` (if configured). This includes pending messages and the *list* of subscriptions.
- **Behavior**:
    - **Pending Messages**: Restored and retransmitted.
    - **Subscriptions**: Restored from the store.
    - **Handlers**: **LOST**. The store saves the *topic* but cannot save the *Go function*.

> [!WARNING]
> **Zombie Subscriptions**: If you subscribed to `topic/foo` using `client.Subscribe` in a previous run, the client will restore this subscription on restart. However, because it doesn't know which function to call, messages for `topic/foo` might be **dropped**.
>
> To avoid this, use `WithSubscription` for permanent topics, or use `WithPersistence(false)` for temporary ones.

## Best Practices for Subscriptions

### 1. Static Subscriptions: Use `WithSubscription`
For subscriptions that should always exist, register them in `Dial`. This ensures the handler is **always re-attached** on startup, even after a crash.

```go
client, _ := mq.Dial(...,
    mq.WithSubscription("cmd/restart", handleRestart),
)
```

### 2. Ephemeral Subscriptions: Use `WithPersistence(false)`
For dynamic or temporary subscriptions, mark them as **ephemeral**. They will not be saved to the store and will not cause "zombie" subscriptions on restart.

```go
client.Subscribe(topic, mq.AtLeastOnce, handler,
    mq.WithPersistence(false),
)
```

### 3. Catch-All Handler
Set a default handler to log or process messages for restored subscriptions that have no specific handler attached.

```go
mq.WithDefaultPublishHandler(func(c *mq.Client, msg mq.Message) {
    slog.Warn("Received message with no handler", "topic", msg.Topic)
})
```

---

For other common MQTT issues, see [docs/troubleshooting.md](troubleshooting.md).
