# MQTT 5.0 Advanced Patterns

MQTT 5.0 is not just an update; it's a significant evolution of the protocol. This document covers advanced patterns made possible by v5.0 features in the `mq` library.

---

## 1. Request / Response

In MQTT 3.1.1, request/response required hardcoded topic conventions. MQTT 5.0 standardizes this using **Response Topic** and **Correlation Data**.

### The Requester

```go
corrID := uuid.New().Bytes()

client.Publish("request/service", payload,
    mq.WithResponseTopic("results/client-123"),
    mq.WithCorrelationData(corrID),
)

// Subscribe to the response topic
client.Subscribe("results/client-123", 1, func(c *mq.Client, msg mq.Message) {
    if bytes.Equal(msg.Properties.CorrelationData, corrID) {
        // This is the response to our specific request
    }
})
```

### The Responder

```go
client.Subscribe("request/service", 1, func(c *mq.Client, msg mq.Message) {
    if msg.Properties.ResponseTopic != "" {
        // Send response back to the requested topic
        c.Publish(msg.Properties.ResponseTopic, responsePayload,
            mq.WithCorrelationData(msg.Properties.CorrelationData),
        )
    }
})
```

---

## 2. Topic Aliases (Bandwidth Optimization)

Topic aliases allow you to replace a long topic string with a 2-byte integer after the first message. This significantly reduces header overhead for small payloads.

The `mq` library handles this **automatically**.

```go
client, _ := mq.Dial(uri,
    mq.WithTopicAliasMaximum(10), // Tell server we accept up to 10 aliases
)

// The library will automatically assign an alias to "very/long/topic/name/..."
// on the first publish and use the alias ID for subsequent publishes.
client.Publish("very/long/topic/name/...", payload)
```

---

## 3. User Properties (Metadata)

User Properties are UTF-8 key-value pairs that can be attached to almost any packet. They are the "HTTP Headers" of MQTT.

### Use Cases:
*   **Routing**: Adding a `region` or `version` tag.
*   **Audit**: Adding a `user_id` or `request_id`.
*   **Application Logic**: Indicating the serialization format (if not using the built-in `PayloadFormat`).

```go
client.Publish("logs", data,
    mq.WithUserProperty("priority", "high"),
    mq.WithUserProperty("component", "engine"),
)
```

---

## 4. Message Expiry

In v3.1.1, a message stayed in the broker until delivered (or the session expired). In v5.0, you can set a TTL (Time-To-Live) per message.

```go
// This message is useless if not delivered within 60 seconds
client.Publish("alerts/temporary", data,
    mq.WithMessageExpiry(60),
)
```

---

## 5. Delayed Wills

You can now delay the "Last Will and Testament" (LWT) message. If the client reconnects within the delay, the LWT is never sent. This prevents "flapping" notifications during brief network drops.

```go
mq.Dial(uri,
    mq.WithWill("status/offline", []byte("dead"), 1, false),
    mq.WithWillDelayInterval(30), // Delay LWT by 30 seconds
)
```

---

## 6. Flow Control

MQTT 5.0 allows the server and client to negotiate the number of "in-flight" QoS 1 and 2 messages.

*   **Server Limit**: The client automatically throttles itself to respect the server's `ReceiveMaximum`.
*   **Client Limit**: You can tell the server to slow down if your application is overwhelmed.

```go
mq.Dial(uri,
    mq.WithReceiveMaximum(5, mq.LimitPolicyStrict), // Only allow 5 in-flight incoming messages
)
```
