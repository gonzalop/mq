# Error Handling Example

Demonstrates proper error handling patterns for MQTT operations.

## Features Demonstrated

- Checking for connection errors
- Handling publish failures
- Subscribe error handling
- Using `errors.Is` for MQTT v5.0 reason code introspection
- Timeout handling with contexts
- Graceful error recovery

## Running the Example

```bash
go run main.go [server]
```

Example:
```bash
go run main.go tcp://localhost:1883
```

## What It Does

1. **Connection Error Handling**: Shows how to handle connection failures
2. **Publish Errors**: Demonstrates checking publish token errors
3. **Subscribe Errors**: Shows subscription error handling
4. **MQTT v5 Reason Codes**: Uses `errors.Is` to check specific reason codes
5. **Context Timeouts**: Shows timeout handling with contexts
6. **Error Recovery**: Demonstrates recovering from errors

## Example Output

```
Error Handling Example
======================

1. Testing connection errors...
Attempting to connect to invalid server...
❌ Connection failed (as expected): dial tcp: lookup invalid-broker: no such host

2. Testing successful connection...
✓ Connected successfully

3. Testing publish with error checking...
Publishing message...
✓ Message published successfully

4. Testing subscribe errors...
Subscribing to valid topic...
✓ Subscribed successfully

5. Testing timeout handling...
Publishing with 100ms timeout...
✓ Publish completed within timeout

6. Testing MQTT v5 reason codes...
Attempting restricted subscription...
❌ Subscribe failed: Not authorized
   This is an MQTT error with reason code: 135

✓ All error handling tests completed!
```

## Error Handling Patterns

### Pattern 1: Immediate Error Check
```go
client, err := mq.Dial(server, options...)
if err != nil {
    log.Fatalf("Failed to connect: %v", err)
}
```

### Pattern 2: Token Wait with Context
```go
ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
defer cancel()

token := client.Publish(topic, payload, mq.WithQoS(1))
if err := token.Wait(ctx); err != nil {
    if errors.Is(err, context.DeadlineExceeded) {
        log.Println("Publish timed out")
    } else {
        log.Printf("Publish failed: %v", err)
    }
}
```

### Pattern 3: MQTT v5 Reason Code Introspection
```go
err := token.Wait(ctx)
if err != nil {
    if errors.Is(err, mq.ReasonCodeNotAuthorized) {
        log.Println("Not authorized - check credentials")
    } else if errors.Is(err, mq.ReasonCodeQuotaExceeded) {
        log.Println("Quota exceeded - slow down publishing")
    } else {
        log.Printf("Other error: %v", err)
    }
}
```

### Pattern 4: Async Error Handling
```go
token := client.Publish(topic, payload, mq.WithQoS(1))
go func() {
    if err := token.Wait(context.Background()); err != nil {
        log.Printf("Async publish failed: %v", err)
    }
}()
```

## Common MQTT v5 Reason Codes

| Code | Name | Meaning |
|------|------|---------|
| 0 | Success | Operation successful |
| 128 | Unspecified Error | Generic error |
| 129 | Malformed Packet | Invalid packet format |
| 130 | Protocol Error | Protocol violation |
| 131 | Implementation Error | Server implementation issue |
| 135 | Not Authorized | Authorization failed |
| 144 | Topic Name Invalid | Invalid topic format |
| 145 | Packet Too Large | Message exceeds size limit |
| 151 | Quota Exceeded | Rate limit exceeded |
| 153 | Payload Format Invalid | Invalid payload format |

## Best Practices

1. **Always check errors** from `Dial()` and `Wait()`
2. **Use contexts** for timeout control
3. **Log errors** with sufficient context
4. **Handle specific error types** when possible (MQTT v5 reason codes)
5. **Implement retry logic** for transient failures
6. **Monitor error rates** in production
7. **Test error paths** in your application
