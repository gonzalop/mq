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

> [!CAUTION]
> **Migration Hazard**: If you simply switch to `ProtocolV50` and keep `WithCleanSession(false)`, your session will **resume** on connect but **vanish** on disconnect because the default expiry is 0.
>
> To match v3.1.1 persistence behavior in v5.0, you must explicitly set the expiry:
> ```go
> mq.Dial(...,
>     mq.WithProtocolVersion(mq.ProtocolV50),
>     mq.WithCleanSession(false),             // Resume on start
>     mq.WithSessionExpiryInterval(0xFFFFFFFF), // Persist indefinitely, the server might lower this
> )
> ```

## Configuration

To enable persistence, you must:
1.  **Set a Client ID**: The server needs a stable ID to track your session.
2.  **Disable Clean Session**: Tell the server to keep state.
3.  **Configure a Session Store**: Tell the client where to save pending state. The library includes a built-in `FileStore`, but you can implement the `SessionStore` interface to use any backend (e.g., Redis, PostgreSQL, or cloud storage).

```go
store, _ := mq.NewFileStore("./data", "my-client-id")

// WithSubscription ensures that "my/topic" is always subscribed to on connect,
// and crucially, that the handler is re-attached if the session is restored from disk.
client, _ := mq.Dial("tcp://broker:1883",
    mq.WithClientID("my-client-id"),
    mq.WithCleanSession(false),
    mq.WithSessionStore(store),
    mq.WithSubscription("my/topic", myHandler),
)
```

### Server-Assigned ClientID (MQTT 5.0)

In MQTT 5.0, you can connect with an **empty ClientID** and let the server assign one, even with persistent sessions:

```go
client, _ := mq.Dial("tcp://broker:1883",
    mq.WithClientID(""),                      // Empty - server will assign
    mq.WithProtocolVersion(mq.ProtocolV50),
    mq.WithCleanSession(false),               // Persistent session
    mq.WithSessionExpiryInterval(3600),       // Required to be >0 for getting an assigned ClientID
)

assignedID := client.AssignedClientID()
// The library automatically uses this ID for reconnections
```

> [!NOTE]
> The assigned ClientID is **automatically reused** on reconnection to resume the session. You don't need to manually track it.


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
> **Zombie Subscriptions**: If you subscribed to `topic/foo` using `client.Subscribe` in a previous run, the client will restore this subscription on restart. However, because it doesn't know which function to call, messages for `topic/foo` might be **dropped** (or handled by the `DefaultPublishHandler` if set).
>
> To avoid this, use `WithSubscription` for permanent topics (see below), or use `WithPersistence(false)` for temporary ones.

## Best Practices for Subscriptions

To address the "Lost Handler" problem, follow these rules:

### 1. Static Subscriptions: Use `WithSubscription`
For subscriptions that should always exist (e.g., "cmd/restart"), register them in `Dial`. These are re-attached every time the app starts before the server has a chance to send us anything, which solves the "lost handler" problem.

**Why not just use `Subscribe`?**
*   `Subscribe` works perfectly while the client is running.
*   **However**, if the client restarts, the persisted topic is restored but the **handler is lost**, potentially leading to a "zombie" subscription if no topic matches the subscription and there is no default handler.
*   `WithSubscription` ensures the handler is **always re-attached** on startup.

```go
client, _ := mq.Dial(...,
    mq.WithSubscription("cmd/restart", handleRestart),
)
// Even if the store is empty or wiped, this subscription is created.
// If the store has it, the handler is attached to the restored subscription.
```

### 2. Ephemeral Subscriptions: Use `WithPersistence(false)`
For dynamic or temporary subscriptions (e.g., a specific response topic for a single request), mark them as **ephemeral**. This prevents them from being saved to the store, so you don't end up with "zombie" subscriptions on restart.

```go
// Subscribe to a temporary topic
topic := "response/" + uuid.New()
client.Subscribe(topic, mq.AtLeastOnce, handler,
    mq.WithPersistence(false), // Do not save to the store
)
```

### 3. Catch-All Handler
Set a default handler to log or process messages for restored subscriptions that have no specific handler attached.

```go
mq.WithDefaultPublishHandler(func(c *mq.Client, msg mq.Message) {
    slog.Warn("Received message with no handler", "topic", msg.Topic, "payload_len", len(msg.Payload))
})
```

> [!NOTE]
> If a message is received for a restored subscription, no specific handler is found and no `DefaultPublishHandler` is set, the client will **acknowledge** the message to satisfy the spec but **drop** the payload, logging a warning.

## Example: File Store

The library provides a built-in `FileStore` that saves state to JSON files on disk.

```go
package main

import (
    "log/slog"
    "os"

    "github.com/gonzalop/mq"
)

func main() {
    // 1. Create Store
    clientID := "client-1"
    store, err := mq.NewFileStore("/var/lib/myapp/mqtt-store", clientID)
    if err != nil {
        slog.Error("Failed to create store", "error", err)
        os.Exit(1)
    }

    // 2. Connect
    client, err := mq.Dial("tcp://localhost:1883",
        mq.WithClientID(clientID),
        mq.WithCleanSession(false),
        mq.WithSessionStore(store),

        // Permanent Subscription
        mq.WithSubscription("app/config", func(c *mq.Client, msg mq.Message) {
            // Handle message from server
        }),
    )

    // ...
}
```

## Troubleshooting: The "Poison Pill" Loop

When using persistence (`CleanSession=false`+`SessionExpiryInterval`>0), be aware of the "Poison Pill" scenario:

1.  Client publishes a message (or subscribes) that violates server policy (e.g., payload too large, invalid topic).
2.  Server detects the violation and **closes the connection** (as per MQTT spec).
3.  Client detects disconnect and **auto-reconnects**.
4.  Client restores session and **immediately re-transmits** the pending "poison" message.
5.  Server disconnects again. **Loop continues indefinitely.**

### Mitigation

1.  **Match Limits**: Ensure client-side limits match the server to prevent invalid packets from ever being queued.
    ```go
    mq.Dial(...,
        mq.WithMaxPayloadSize(serverMaxBytes),
        mq.WithMaxTopicLength(serverMaxChars),
    )
    ```
2.  **Recovery**: If stuck in a loop, connect once with a clean session to wipe the bad state.
    ```go
    // Emergency wiper
    client, _ := mq.Dial(...,
        mq.WithClientID("same-client-id"),
        mq.WithCleanSession(true), // Wipes previous state on server and client (if using FileStore)
    )
    client.Disconnect(context.Background())
    ```

## QoS 2 Duplicate Detection

If you **manually delete** the `SessionStore` files without a clean disconnect, the client loses its record of received QoS 2 packet IDs. When it reconnects and the server resends those messages, the client will treat them as new, causing **duplicates**.

**Prevention**: Always call `client.Disconnect()` before deleting store files, or ensure your application logic is idempotent.

---

For other common MQTT issues (Client ID thrashing, zombie retained messages, slow subscribers, connection-scoped behaviors), see [docs/troubleshooting.md](troubleshooting.md).
