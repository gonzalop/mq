# Topic Aliases Example (MQTT v5.0)

Demonstrates MQTT v5.0 topic aliases for bandwidth optimization.

## Features Demonstrated

- Automatic topic alias management
- Bandwidth savings on repeated topics
- `WithAlias()` publish option
- Server capability negotiation

## What Are Topic Aliases?

Topic aliases allow clients to use a short integer (alias) instead of the full topic string after the first use. This saves bandwidth, especially for:
- Long topic names
- High-frequency publishing
- Constrained networks (IoT, mobile)

### Example
```
First publish:  topic="sensors/building-5/floor-3/room-42/temperature" + alias=1
Second publish: alias=1 (topic omitted, ~50 bytes saved!)
Third publish:  alias=1 (another ~50 bytes saved!)
```

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

1. Connects with MQTT v5.0 and topic alias support
2. Publishes multiple messages to the same long topic
3. First message: Sends full topic + assigns alias
4. Subsequent messages: Uses alias only (bandwidth saved)
5. Shows bandwidth savings

## Example Output

```
Topic Aliases Example (MQTT v5.0)
==================================

Connecting with topic alias support...
✓ Connected with MQTT v5.0

Publishing 10 messages to long topic...
Topic: sensors/building-5/floor-3/room-42/temperature

Message 1: Full topic sent (+ alias assigned)
Message 2: Using alias (saved ~50 bytes)
Message 3: Using alias (saved ~50 bytes)
Message 4: Using alias (saved ~50 bytes)
Message 5: Using alias (saved ~50 bytes)
Message 6: Using alias (saved ~50 bytes)
Message 7: Using alias (saved ~50 bytes)
Message 8: Using alias (saved ~50 bytes)
Message 9: Using alias (saved ~50 bytes)
Message 10: Using alias (saved ~50 bytes)

✓ All messages published

📊 Bandwidth Savings:
   Without aliases: ~520 bytes
   With aliases:    ~70 bytes
   Saved:           ~450 bytes (86%)

Test completed successfully! 🎉
```

## Configuration

```go
client, err := mq.Dial(server,
    mq.WithProtocolVersion(mq.ProtocolV50),
    mq.WithTopicAliasMaximum(10), // Support up to 10 aliases
)

// Explicitly request alias usage
client.Publish(topic, payload,
    mq.WithQoS(1),
    mq.WithAlias(), // Request topic alias
)
```

## How It Works

1. **First Publish**: Client sends full topic + alias number
2. **Server Stores**: Server maps alias → topic for this connection
3. **Subsequent Publishes**: Client sends only alias number
4. **Server Resolves**: Server looks up topic from alias
5. **Auto-Management**: Library handles alias assignment automatically

## Bandwidth Savings

| Topic Length | Messages | Without Aliases | With Aliases | Savings |
|--------------|----------|-----------------|--------------|---------|
| 50 bytes | 100 | 5,000 bytes | 350 bytes | 93% |
| 100 bytes | 100 | 10,000 bytes | 600 bytes | 94% |
| 200 bytes | 1000 | 200,000 bytes | 5,800 bytes | 97% |

## Limitations

- **Per-connection**: Aliases are valid only for the current connection
- **Maximum**: Server sets max aliases (typically 10-65535)
- **MQTT v5 Only**: Not available in MQTT v3.1.1
- **Direction**: Client-to-server only (not server-to-client in this library)

## Use Cases

- **IoT Sensors**: Devices with long hierarchical topics
- **High-Frequency Publishing**: Same topic published repeatedly
- **Constrained Networks**: Cellular, satellite, low-bandwidth links
- **Cost Optimization**: Reduce data transfer costs

## Best Practices

- ✅ Use for frequently published topics
- ✅ Enable when topic names are long (>20 characters)
- ✅ Monitor alias usage (don't exceed server's maximum)
- ❌ Don't use for topics published only once
- ❌ Don't manually manage aliases (library does it automatically)
