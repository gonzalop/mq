# Interceptors & Middleware

The `mq` library supports **Interceptors**, a powerful middleware pattern that allows you to inject custom logic into the message flow. This is ideal for cross-cutting concerns like logging, metrics, tracing, and payload transformation.

## Types of Interceptors

There are two types of interceptors:

1.  **`HandlerInterceptor`**: Wraps incoming message handlers.
2.  **`PublishInterceptor`**: Wraps the outbound publishing process.

---

## 1. Handler Interceptor (Inbound)

A `HandlerInterceptor` wraps a `MessageHandler`. It is executed for every message received by the client before it reaches your specific subscription handler or the default handler.

### Example: Logging Every Incoming Message

```go
func LoggingInterceptor(next mq.MessageHandler) mq.MessageHandler {
    return func(client *mq.Client, msg mq.Message) {
        slog.Info("Received message", 
            "topic", msg.Topic, 
            "payload_len", len(msg.Payload),
            "qos", msg.QoS)
        
        // Call the next handler in the chain
        next(client, msg)
    }
}

// Apply it during Dial
client, _ := mq.Dial("tcp://localhost:1883",
    mq.WithHandlerInterceptor(LoggingInterceptor),
)
```

### Example: Global Payload Decryption

```go
func DecryptionInterceptor(next mq.MessageHandler) mq.MessageHandler {
    return func(client *mq.Client, msg mq.Message) {
        decrypted, err := decrypt(msg.Payload)
        if err == nil {
            msg.Payload = decrypted
        }
        next(client, msg)
    }
}
```

### Example: Panic Recovery

In accordance with Go's "fail-fast" philosophy, the library does not catch panics in user-provided callbacks. If you require process-level resilience, you can implement a recovery interceptor:

```go
recoveryInterceptor := func(next mq.MessageHandler) mq.MessageHandler {
    return func(c *mq.Client, m mq.Message) {
        defer func() {
            if r := recover(); r != nil {
                c.Options().Logger.Error("Recovered from panic in handler", 
                    "topic", m.Topic, "error", r)
            }
        }()
        next(c, m)
    }
}

// Apply to all subscriptions via Dial
client, _ := mq.Dial(server, 
    mq.WithHandlerInterceptor(recoveryInterceptor))
```

---

## 2. Publish Interceptor (Outbound)

A `PublishInterceptor` wraps the `Publish` function. It allows you to inspect or modify messages before they are queued for sending.

### Example: Distributed Tracing (OpenTelemetry)

```go
func TracingInterceptor(next mq.PublishFunc) mq.PublishFunc {
    return func(ctx context.Context, topic string, payload []byte, opts ...mq.PublishOption) mq.Token {
        // Create a new span
        ctx, span := tracer.Start(ctx, "mqtt.publish")
        defer span.End()

        // Inject trace context into MQTT v5 User Properties
        traceID := span.SpanContext().TraceID().String()
        opts = append(opts, mq.WithUserProperty("trace_id", traceID))

        return next(ctx, topic, payload, opts...)
    }
}

// Apply it during Dial
client, _ := mq.Dial("tcp://localhost:1883",
    mq.WithPublishInterceptor(TracingInterceptor),
)
```

---

## Chain of Execution

You can apply multiple interceptors. They are executed in the order they are provided to `mq.Dial`.

```go
client, _ := mq.Dial(uri,
    mq.WithPublishInterceptor(AuthTokenInjector), // Executed 1st
    mq.WithPublishInterceptor(MetricsCollector),   // Executed 2nd
)
```

1.  **Outbound**: The first interceptor in the list is the "outermost" wrapper.
2.  **Inbound**: The first interceptor in the list is the "outermost" wrapper.

## Best Practices

*   **Don't Block**: Interceptors run within the internal client loops (for outbound) or in the handler goroutine (for inbound). Avoid long-running blocking operations.
*   **Error Handling**: For outbound interceptors, you can return a failed token early without calling `next(...)`.
*   **Immutability**: While you can modify the `mq.Message` or `PublishOptions`, be careful not to break protocol expectations (e.g., changing QoS in a way that violates server limits).
