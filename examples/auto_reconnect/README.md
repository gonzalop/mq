# Auto-Reconnect Example

This example demonstrates automatic reconnection when the MQTT connection is lost.

## Features Demonstrated

- Automatic reconnection with exponential backoff and anti-flapping escalation
- Connection lifecycle hooks (`OnConnect`, `OnConnectionLost`)
- Connection health and uptime tracking (`ConnectedAt`, `Uptime`)
- Automatic resubscription after reconnect
- Graceful handling of publish failures during disconnection

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

## Testing Reconnection

1. Start the example
2. Stop your MQTT server (e.g., `docker stop mosquitto`)
3. Observe the "Connection lost" message
4. Restart your server (e.g., `docker start mosquitto`)
5. Observe automatic reconnection and resubscription

The example will continue publishing messages every 5 seconds, demonstrating that the client handles disconnections gracefully.

## Output Example

```
Auto-Reconnect Example
======================

✅ Connected (connection #1) at 14:23:45
   Subscribing to 'test/reconnect'...
   ✓ Subscribed

📤 Published: Message #1 at 14:23:50 (uptime: 5s)
📨 Received: Message #1

❌ Connection lost (disconnection #1) at 14:23:55: read tcp: connection reset
   Will attempt to reconnect automatically...

✅ Connected (connection #2) at 14:24:02
   Subscribing to 'test/reconnect'...
   ✓ Subscribed

📤 Published: Message #2 at 14:24:05 (uptime: 3s)
```
