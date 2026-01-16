# Last Will and Testament (LWT) Example

This example demonstrates MQTT's Last Will and Testament feature, which allows the server to publish a message on behalf of a client if it disconnects unexpectedly.

## What is Last Will and Testament?

When a client connects to an MQTT server, it can specify a "will message" that the server will publish if:
- The client disconnects without sending a proper DISCONNECT packet
- The connection is lost due to network failure
- The client crashes or is killed
- The keepalive timeout expires

The will message is **NOT** published if the client disconnects gracefully.

## Use Cases

- **Device Status Monitoring**: Automatically mark devices as "offline" when they crash
- **Alarm Systems**: Alert when critical sensors lose connection
- **Presence Detection**: Track when users/devices go offline unexpectedly
- **Failover**: Trigger backup systems when primary systems fail

## Running the Example

```bash
go run main.go [server] [username] [password]
```

Examples:
```bash
# Local server
go run main.go

# Custom server
go run main.go tcp://broker.example.com:1883

# With authentication (command-line)
go run main.go tcp://localhost:1883 myuser mypass

# With authentication (environment variables)
export MQTT_USERNAME=myuser
export MQTT_PASSWORD=mypass
go run main.go tcp://localhost:1883
```

## What It Does

1. **Monitor Client**: Starts a client that watches for will messages
2. **Main Client**: Connects with a configured Last Will message
3. **Normal Operation**: Publishes sensor data to demonstrate normal operation
4. **Disconnect Options**:
   - **Graceful** (Ctrl+C): Publishes "offline" status manually, LWT NOT triggered
   - **Unexpected** (kill -9 or timeout): LWT IS triggered by server

## Testing LWT

### Option 1: Graceful Disconnect (No LWT)
```bash
go run main.go
# Press Ctrl+C
# Result: No will message published
```

### Option 2: Unexpected Disconnect (LWT Triggered)
```bash
go run main.go &
PID=$!
sleep 5
kill -9 $PID  # Force kill
# Result: Will message IS published
```

### Option 3: Auto-simulation (Wait 15 seconds)
```bash
go run main.go
# Wait 15 seconds
# Result: Simulated unexpected disconnect
```

## Example Output

```
Last Will and Testament (LWT) Example
======================================

1️⃣  Starting monitor client...
   ✓ Monitor client ready

2️⃣  Starting client with Last Will configured...
   Will Topic: devices/sensor-1/will
   Will Message: {"status":"offline","reason":"unexpected_disconnect"}
   Will QoS: 1
   Will Retained: true

   ✓ Client connected with LWT configured

3️⃣  Publishing 'online' status...
   📊 Status: online

4️⃣  Simulating normal operation (publishing sensor data)...
   📡 Published sensor reading #1
   📡 Published sensor reading #2
   📡 Published sensor reading #3

============================================================
Choose how to disconnect:
============================================================
1. Press Ctrl+C for GRACEFUL disconnect (will NOT trigger LWT)
2. Kill the process (kill -9) for UNEXPECTED disconnect (WILL trigger LWT)
3. Wait 15 seconds and we'll simulate unexpected disconnect
============================================================

5️⃣  Simulating unexpected disconnect...
   Forcing connection close without proper DISCONNECT packet...
   ⚠️  Connection forcefully closed!
   ⏳ Waiting for server to detect disconnect and publish LWT...

🪦 WILL MESSAGE RECEIVED: {"status":"offline","reason":"unexpected_disconnect"}
   (This means the client disconnected unexpectedly!)

   ✅ LWT was published
```

## Configuration

```go
client, err := mq.Dial(server,
    mq.WithWill(
        "devices/sensor-1/will",           // Topic
        []byte(`{"status":"offline"}`),    // Payload
        mq.AtLeastOnce,                    // QoS
        true,                               // Retained
    ),
)
```

## Important Notes

- **Retained Flag**: Setting the will message as retained ensures new subscribers immediately see the last known status
- **QoS Level**: Use QoS 1 or 2 for critical will messages to ensure delivery
- **Keepalive**: Shorter keepalive intervals mean faster detection of unexpected disconnects
- **Graceful Shutdown**: Always call `Disconnect()` when shutting down normally to avoid triggering the LWT
