# MQTT v5.0 Request/Response Example

Demonstrates the request/response messaging pattern using MQTT v5.0 properties.

## Features Demonstrated

- Request/response pattern with ResponseTopic
- Correlation data for matching requests and responses
- Timeout handling for requests
- Multiple concurrent requests
- Response routing

## Running the Example

```bash
go run main.go [server]
```

Example:
```bash
go run main.go tcp://localhost:1883
```

## What It Does

1. **Server Setup**: Creates a "server" client that handles requests
2. **Client Setup**: Creates a "client" that sends requests
3. **Request/Response**: Demonstrates synchronous request/response
4. **Concurrent Requests**: Shows handling multiple requests simultaneously
5. **Timeout Handling**: Demonstrates request timeouts

## Example Output

```
MQTT v5.0 Request/Response Example
===================================

Starting request/response server...
✓ Server listening on 'rpc/requests'

Starting client...
✓ Client connected

Test 1: Simple Request/Response
Sending request: {"method":"getStatus"}
📨 Server received request: {"method":"getStatus"}
📤 Server sending response: {"status":"ok"}
✅ Received response: {"status":"ok"}

Test 2: Request with Parameters
Sending request: {"method":"calculate","params":{"a":5,"b":3}}
📨 Server received request: {"method":"calculate","params":{"a":5,"b":3}}
📤 Server sending response: {"result":8}
✅ Received response: {"result":8}

Test 3: Multiple Concurrent Requests
Sending 5 concurrent requests...
✅ Response 1: {"id":1,"result":"ok"}
✅ Response 2: {"id":2,"result":"ok"}
✅ Response 3: {"id":3,"result":"ok"}
✅ Response 4: {"id":4,"result":"ok"}
✅ Response 5: {"id":5,"result":"ok"}

Test 4: Request Timeout
Sending request with 1s timeout...
⏱️  Request timed out (as expected)

✓ All tests completed!
```

## How It Works

### Server Side
```go
// Subscribe to request topic
client.Subscribe(context.Background(), "rpc/requests", mq.AtLeastOnce, func(c *mq.Client, msg mq.Message) {
    if msg.Properties == nil || msg.Properties.ResponseTopic == "" {
        return // Not a request
    }
    
    // Process request
    response := processRequest(msg.Payload)
    
    // Send response
    c.Publish(context.Background(), msg.Properties.ResponseTopic, response,
        mq.WithQoS(mq.AtLeastOnce),
        mq.WithCorrelationData(msg.Properties.CorrelationData))
})
```

### Client Side
```go
// Create unique response topic
responseTopic := fmt.Sprintf("rpc/responses/%s", clientID)

// Subscribe to responses
client.Subscribe(context.Background(), responseTopic, mq.AtLeastOnce, handleResponse)

// Send request
correlationID := generateUniqueID()
client.Publish(context.Background(), "rpc/requests", request,
    mq.WithQoS(mq.AtLeastOnce),
    mq.WithResponseTopic(responseTopic),
    mq.WithCorrelationData([]byte(correlationID)))

// Wait for response (with timeout)
select {
case response := <-responses[correlationID]:
    // Got response
case <-time.After(5 * time.Second):
    // Timeout
}
```

## Pattern Variations

### 1. Shared Response Topic
All clients use same response topic, rely on correlation data:
```go
responseTopic := "rpc/responses/shared"
```

### 2. Per-Client Response Topic
Each client has unique response topic:
```go
responseTopic := fmt.Sprintf("rpc/responses/%s", clientID)
```

### 3. Per-Request Response Topic
Each request gets unique topic (more overhead):
```go
responseTopic := fmt.Sprintf("rpc/responses/%s", requestID)
```

## Use Cases

- **RPC (Remote Procedure Call)**: Call functions on remote systems
- **Microservices**: Service-to-service communication
- **Command/Response**: Send commands, wait for acknowledgment
- **Query/Result**: Database-like queries over MQTT
- **IoT Control**: Send commands to devices, wait for confirmation

## Advantages Over HTTP

- ✅ Persistent connections (no connection overhead)
- ✅ Bidirectional (server can initiate)
- ✅ QoS guarantees
- ✅ Works through firewalls/NAT
- ✅ Lower latency for repeated requests

## Best Practices

- ✅ Always set a timeout for requests
- ✅ Use unique correlation IDs
- ✅ Clean up pending requests on timeout
- ✅ Use QoS 1 or 2 for reliability
- ✅ Consider using per-client response topics
- ❌ Don't use for high-throughput scenarios (consider streaming)
- ❌ Don't forget to unsubscribe from response topics when done

## Comparison with Other Patterns

| Pattern | Use Case | Pros | Cons |
|---------|----------|------|------|
| **Request/Response** | RPC, Commands | Simple, familiar | Blocking, latency |
| **Pub/Sub** | Events, Broadcasts | Decoupled, scalable | No response guarantee |
| **Streaming** | Real-time data | High throughput | Complex |
