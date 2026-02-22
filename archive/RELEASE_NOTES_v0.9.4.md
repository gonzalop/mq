# Release v0.9.4

This release introduces a powerful middleware pattern for observability, improves protocol robustness with automatic version negotiation, and provides more control over client-side buffering and reliability policies. It also includes significant performance and stability enhancements.

## 🚀 New Features

### Middleware/Interceptor Pattern
- **`WithHandlerInterceptor` & `WithPublishInterceptor`**: New functional options to add cross-cutting logic to inbound and outbound message flows.
- **Use Cases**: Ideal for logging, metrics, tracing, or modifying messages on the fly. Interceptors can be chained and are executed in the order they are added.
- **Example**: See the new [middleware example](https://github.com/gonzalop/mq/tree/v0.9.4/examples/middleware) for a demonstration of logging and tracing.
```go
// Add an interceptor to log all incoming messages
client, _ := mq.Dial(uri,
    mq.WithHandlerInterceptor(func(next mq.MessageHandler) mq.MessageHandler {
        return func(c *mq.Client, m mq.Message) {
            log.Printf("Received message on topic %s", m.Topic)
            next(c, m) // Call the original handler
        }
    }),
)
```

### Automatic Protocol Negotiation
- **`WithAutoProtocolVersion`**: The client now defaults to `true`, enabling it to transparently negotiate the MQTT protocol version.
- **How it Works**: It first attempts to connect with MQTT v5.0. If the server rejects this, it automatically falls back and retries with MQTT v3.1.1.

### Configurable Buffers & QoS 0 Reliability
- **`WithOutgoingQueueSize` & `WithIncomingQueueSize`**: New options to tune the size of internal packet buffers, allowing for better control over memory usage and throughput.
- **`WithQoS0LimitPolicy`**: Defines behavior for QoS 0 messages when the outgoing buffer is full:
    - `QoS0LimitPolicyDrop` (Default): Drops the message to prevent blocking the publisher. The operation's `Token` will indicate it was dropped via `token.Dropped()`.
    - `QoS0LimitPolicyBlock`: Blocks the publisher until space is available.

## ⚡ Performance
- **Optimized Packet Serialization**: Outgoing packet serialization now uses a `sync.Pool` to reduce allocations and memory pressure, improving throughput under high-concurrency workloads.

## 🩹 Bug Fixes
- **Shutdown Deadlock**: Resolved a potential deadlock condition that could occur during client shutdown while publish operations were in-flight.
- **Flow Control Deadlock**: Fixed a deadlock related to the publish flow control mechanism.

## 📦 Installation

```bash
go get github.com/gonzalop/mq@v0.9.4
```
