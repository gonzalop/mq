# Persistent Session Example

Demonstrates MQTT persistent sessions (CleanSession=false) where subscriptions and undelivered messages survive disconnections.

## Features Demonstrated

- Persistent sessions with `WithCleanSession(false)`
- Session state preservation across disconnections
- Message queuing while client is offline
- Subscription persistence
- QoS 1 message delivery guarantees

## Running the Example

```bash
go run main.go [server]
```

Example:
```bash
go run main.go tcp://localhost:1883
```

## What It Does

### Phase 1: Initial Connection
1. Connects with `CleanSession=false` and a fixed client ID
2. Subscribes to `persistent/test` with QoS 1
3. Publishes a test message to verify subscription
4. Disconnects (but session persists on server)

### Phase 2: Offline Message Publishing
5. A separate publisher client connects
6. Publishes messages while the main client is offline
7. Server queues these messages for the disconnected client

### Phase 3: Reconnection
8. Main client reconnects with same client ID
9. Receives all queued messages from when it was offline
10. Demonstrates that subscription was preserved

## Example Output

```
Persistent Session Example
===========================

Phase 1: Initial connection with persistent session
Connecting to tcp://localhost:1883...
✓ Connected with CleanSession=false

Subscribing to 'persistent/test'...
✓ Subscribed

Publishing test message...
📨 Received: This is a test message

Disconnecting (session will persist)...
✓ Disconnected

Phase 2: Publishing messages while client is offline
Starting publisher...
Publishing offline message 1...
Publishing offline message 2...
Publishing offline message 3...
✓ Published 3 messages while client was offline

Waiting 2 seconds...

Phase 3: Reconnecting to receive queued messages
Reconnecting with same client ID...
✓ Reconnected

Waiting for queued messages...
📨 Received offline message: Offline message 1
📨 Received offline message: Offline message 2
📨 Received offline message: Offline message 3

✓ Received all 3 queued messages!

Test completed successfully! 🎉
```

## Key Concepts

### Clean Session vs Persistent Session

| CleanSession=true | CleanSession=false |
|-------------------|-------------------|
| Fresh start each time | Session persists |
| No message queuing | Messages queued while offline |
| Subscriptions lost | Subscriptions preserved |
| Lower server memory | Higher server memory |

### Use Cases for Persistent Sessions

- **IoT Devices**: Ensure no messages are lost during brief disconnections
- **Mobile Apps**: Receive messages sent while app was in background
- **Critical Systems**: Guarantee message delivery even during network issues
- **Monitoring**: Don't miss alerts during temporary outages

## Important Notes

- **Client ID**: Must use the same client ID when reconnecting
- **QoS Level**: Only QoS 1 and 2 messages are queued (QoS 0 are discarded)
- **Server Limits**: Check server's `max_queued_messages` setting
- **Session Expiry**: MQTT v5.0 adds session expiry intervals (see `session_expiry_test.go`)
