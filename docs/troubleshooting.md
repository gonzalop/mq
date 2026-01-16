# Troubleshooting Common MQTT Issues

This guide covers common pitfalls and edge cases when working with MQTT clients.

## Common Pitfalls

### Client ID Thrashing

**The Problem**: Two instances of your application (e.g., prod and staging, or two replicas) accidentally share the same `ClientID`.

**What Happens**: The server kicks Client A to let Client B connect. Client A sees the disconnect, auto-reconnects, kicking Client B. They fight forever, consuming massive bandwidth and CPU, while appearing "unstable."

**How to Detect**:
- **MQTT 3.1.1**: Generic `io.EOF` or "connection reset" errors in logs.
- **MQTT 5.0**: May receive `DISCONNECT` with Reason Code `0x8E` (Session Taken Over), but often the race condition results in an abrupt TCP reset before the packet arrives.
  - If the `DISCONNECT` packet is received, your `OnConnectionLost` callback will receive an `*MqttError` with the reason code and optional reason string from the server.

**Fix**: Always generate a unique ClientID per instance:
```go
import "github.com/google/uuid"

clientID := fmt.Sprintf("myapp-%s", uuid.New().String())
client, _ := mq.Dial("tcp://broker:1883",
    mq.WithClientID(clientID),
    mq.WithOnConnectionLost(func(c *mq.Client, err error) {
        if mq.IsReasonCode(err, mq.ReasonCodeSessionTakenOver) {
            slog.Error("Another client is using our ClientID!")
        }
    }),
)
```

---

### Zombie Retained Messages

**The Problem**: You test with `Publish("status", "ON", Retain=true)`. Even months later, any new subscriber to `status` immediately gets "ON", even if your publisher has been dead for weeks.

**What Happens**: New clients start up and think systems are online when they aren't.

**Fix**: Send an **empty payload** with `Retain=true` to delete the retained message:
```go
// Delete retained message
client.Publish("status", []byte(""), mq.WithRetain(true))
```

---

### Slow Subscriber (Backpressure)

**The Problem**: Your subscription handler takes 500ms to process a message (e.g., writing to DB), but messages arrive every 10ms.

**What Happens**: The client's internal memory buffer fills up. Depending on implementation, you either crash (OOM) or start dropping packets.

**Fix**: Process messages **asynchronously** so the MQTT client loop isn't blocked:
```go
msgQueue := make(chan mq.Message, 1000)

client.Subscribe("data/#", mq.AtLeastOnce, func(c *mq.Client, msg mq.Message) {
    select {
    case msgQueue <- msg:
        // Queued successfully
    default:
        slog.Warn("Message queue full, dropping message")
    }
})

// Separate worker goroutine
go func() {
    for msg := range msgQueue {
        processMessage(msg) // Slow DB write
    }
}()
```

---

## Connection-Scoped Behaviors

These MQTT features are **reset on every reconnection** and are not persisted:

### Topic Aliases

Topic aliases (both client→server and server→client) are **connection-scoped**:
- Aliases established before a disconnect are **not** reused after reconnect.
- The library handles this automatically by resetting and re-establishing aliases as needed.
- **Impact**: First few publishes after reconnection may have slightly higher bandwidth usage while aliases are re-established.

### Subscription Identifiers (MQTT 5.0)

Subscription Identifiers are **connection-scoped** per the MQTT 5.0 spec:
- They are **not persisted** by the library.
- If your application logic relies on Subscription Identifiers for message routing, you must re-establish them after reconnecting.

### Will Message Delay (MQTT 5.0)

If you use `WithWillDelay()`, the Will Message is **not published immediately** on disconnect:
- If the client reconnects within the delay window, the Will is **cancelled**.
- This can be confusing when testing Last Will and Testament with quick reconnects.
- **Tip**: Use a delay of 0 (default) for testing, or wait longer than the delay before reconnecting.

---

## Flow Control

### Receive Maximum

If the server's `ReceiveMaximum` is lower than the number of pending QoS 1/2 messages you're trying to send, the client will **throttle** publishing to respect the server's limit.

**What This Looks Like**: "Slow" publishing after reconnection when you have many queued messages.

**This is Correct Behavior**: The client is respecting the server's flow control limits to prevent overwhelming it.

**Mitigation**:
- Reduce the number of pending messages in your queue.
- Increase the server's `ReceiveMaximum` if you control its configuration.

---

## Persistence-Specific Issues

For issues specific to persistent sessions (e.g., Poison Pill loops, QoS 2 duplicates), see [docs/persistence.md](persistence.md#troubleshooting-the-poison-pill-loop).
