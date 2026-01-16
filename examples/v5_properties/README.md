# MQTT v5.0 Properties Example

Demonstrates MQTT v5.0 packet properties including user properties, content type, message expiry, and more.

## Features Demonstrated

- User properties (custom key-value metadata)
- Content type specification
- Message expiry intervals
- Payload format indicator
- Response topic and correlation data
- Properties struct usage

## Running the Example

```bash
go run main.go [server]
```

Example:
```bash
go run main.go tcp://localhost:1883
```

## What It Does

1. Connects with MQTT v5.0
2. Demonstrates various property types:
   - Content Type
   - User Properties
   - Message Expiry
   - Payload Format
   - Request/Response pattern
3. Shows how to read properties from received messages

## Example Output

```
MQTT v5.0 Properties Example
============================

✓ Connected with MQTT v5.0

Test 1: Content Type
Publishing JSON with content-type...
✓ Published
📨 Received with ContentType: application/json

Test 2: User Properties
Publishing with custom metadata...
✓ Published
📨 Received with user properties:
   source: sensor-42
   location: building-5
   priority: high

Test 3: Message Expiry
Publishing with 60s expiry...
✓ Published
📨 Received (will expire in ~60s if not delivered)

Test 4: Request/Response Pattern
Sending request...
✓ Request sent
📨 Response received with correlation ID: req-12345

✓ All property tests completed!
```

## Property Types

### Content Type
Indicates the MIME type of the payload:
```go
client.Publish(topic, jsonData,
    mq.WithContentType("application/json"))
```

### User Properties
Custom key-value metadata:
```go
client.Publish(topic, data,
    mq.WithUserProperty("source", "sensor-1"),
    mq.WithUserProperty("location", "warehouse"),
    mq.WithUserProperty("priority", "high"))
```

### Message Expiry
Time in seconds until message expires:
```go
client.Publish(topic, data,
    mq.WithMessageExpiry(300)) // 5 minutes
```

### Payload Format
Indicates if payload is UTF-8 text:
```go
client.Publish(topic, textData,
    mq.WithPayloadFormat(1)) // 1 = UTF-8 text, 0 = binary
```

### Request/Response Pattern
For request/response messaging:
```go
// Request
client.Publish("requests/topic", request,
    mq.WithResponseTopic("responses/topic"),
    mq.WithCorrelationData([]byte("request-id-123")))

// Response handler checks correlation data
func handler(c *mq.Client, msg mq.Message) {
    if msg.Properties != nil {
        correlationID := msg.Properties.CorrelationData
        // Match with original request
    }
}
```

## Reading Properties

```go
func messageHandler(c *mq.Client, msg mq.Message) {
    if msg.Properties == nil {
        return // MQTT v3.1.1 or no properties
    }
    
    // Content Type
    if msg.Properties.ContentType != "" {
        fmt.Printf("Content-Type: %s\n", msg.Properties.ContentType)
    }
    
    // User Properties
    for key, value := range msg.Properties.UserProperties {
        fmt.Printf("%s: %s\n", key, value)
    }
    
    // Message Expiry
    if msg.Properties.MessageExpiry != nil {
        fmt.Printf("Expires in: %d seconds\n", *msg.Properties.MessageExpiry)
    }
    
    // Response Topic
    if msg.Properties.ResponseTopic != "" {
        // Send response to this topic
        c.Publish(msg.Properties.ResponseTopic, response,
            mq.WithCorrelationData(msg.Properties.CorrelationData))
    }
}
```

## Use Cases

### Content Type
- Indicate JSON, XML, Protocol Buffers, etc.
- Allow subscribers to parse correctly
- Enable content negotiation

### User Properties
- Add metadata without changing payload
- Routing information
- Debugging/tracing data
- Application-specific flags

### Message Expiry
- Time-sensitive alerts
- Temporary offers
- Real-time data that becomes stale

### Payload Format
- Help subscribers decode text vs binary
- Enable automatic UTF-8 validation

### Request/Response
- RPC-style messaging
- Command/response patterns
- Correlation of requests and responses

## Best Practices

- ✅ Use Content-Type for structured data
- ✅ Add User Properties for metadata
- ✅ Set Message Expiry for time-sensitive data
- ✅ Use Response Topic for request/response patterns
- ❌ Don't overuse User Properties (adds overhead)
- ❌ Don't rely on Message Expiry for critical timing
