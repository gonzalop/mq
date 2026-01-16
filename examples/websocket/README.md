# WebSocket Example

Demonstrates MQTT over WebSockets for browser and proxy-friendly connections.

## Features Demonstrated

- MQTT over WebSocket (`ws://` and `wss://`)
- Custom WebSocket headers
- Proxy support
- Secure WebSocket connections

## Running the Example

```bash
go run main.go [server] [username] [password]
```

Examples:
```bash
# Unsecure WebSocket
go run main.go ws://localhost:8080

# Secure WebSocket (WSS)
go run main.go wss://test.mosquitto.org:8081

# With custom path
go run main.go ws://localhost:8080/mqtt

# With authentication (command-line)
go run main.go ws://localhost:8080 myuser mypass

# With authentication (environment variables)
export MQTT_USERNAME=myuser
export MQTT_PASSWORD=mypass
go run main.go ws://localhost:8080
```

## What It Does

1. Connects to MQTT server via WebSocket
2. Subscribes to `websocket/test`
3. Publishes messages over WebSocket
4. Demonstrates that MQTT works identically over WebSocket

## Example Output

```
WebSocket Example
=================

Connecting to ws://localhost:8080...
Using WebSocket transport
✓ Connected via WebSocket!

Subscribing to 'websocket/test'...
✓ Subscribed

Publishing message over WebSocket...
✓ Published

📨 Received: This message traveled over WebSocket!

Test completed successfully! 🎉
```

## WebSocket vs TCP

| Feature | TCP (`tcp://`) | WebSocket (`ws://`) |
|---------|---------------|---------------------|
| **Firewall** | Often blocked | Usually allowed |
| **Proxies** | May not work | Works through HTTP proxies |
| **Browsers** | Not supported | Fully supported |
| **Overhead** | Lower | Slightly higher |
| **TLS** | `tls://` | `wss://` |

## Configuration

### Basic WebSocket
```go
client, err := mq.Dial("ws://localhost:8080")
```

### Secure WebSocket
```go
client, err := mq.Dial("wss://broker.example.com:443",
    mq.WithTLS(&tls.Config{...}))
```

### Custom WebSocket Path
```go
client, err := mq.Dial("ws://localhost:8080/mqtt")
```

### With HTTP Headers
```go
// Note: This requires modifying the Dial function to accept WebSocket options
// The library currently supports basic WebSocket connections
```

## Broker Configuration

### Mosquitto
```conf
# mosquitto.conf
listener 8080
protocol websockets

# For secure WebSocket
listener 8081
protocol websockets
cafile /path/to/ca.crt
certfile /path/to/server.crt
keyfile /path/to/server.key
```

### EMQX
```conf
# emqx.conf
listeners.ws.default {
  bind = "0.0.0.0:8083"
  max_connections = 1024000
}

listeners.wss.default {
  bind = "0.0.0.0:8084"
  ssl_options {
    certfile = "/path/to/cert.pem"
    keyfile = "/path/to/key.pem"
  }
}
```

## Use Cases

### Browser Applications
```javascript
// In browser (using paho-mqtt.js or similar)
const client = new Paho.MQTT.Client("ws://broker.example.com:8080", "clientId");
client.connect({
    onSuccess: () => {
        client.subscribe("sensors/+/temperature");
    }
});
```

### Behind Corporate Proxies
- WebSocket typically works through HTTP proxies
- Use `ws://` (port 80) or `wss://` (port 443)
- May require proxy authentication

### Mobile Applications
- Reduces battery drain (persistent connection)
- Works on restrictive networks
- Fallback when TCP is blocked

## Troubleshooting

### "websocket: bad handshake"
- Check server supports WebSocket
- Verify correct path (some servers use `/mqtt`)
- Check for proxy interference

### Connection drops frequently
- Enable WebSocket ping/pong (keepalive)
- Check for aggressive proxy timeouts
- Consider using `wss://` for better reliability

### "protocol not supported"
- Ensure server has WebSocket listener configured
- Check server logs for configuration errors

## Best Practices

- ✅ Use `wss://` in production for security
- ✅ Set appropriate keepalive intervals
- ✅ Handle reconnection (WebSocket can be flaky)
- ✅ Use standard ports (80/443) for better firewall traversal
- ❌ Don't use WebSocket if TCP works (lower overhead)
- ❌ Don't forget TLS certificates for `wss://`

## Performance Notes

- **Latency**: Slightly higher than raw TCP
- **Throughput**: ~5-10% overhead vs TCP
- **CPU**: Marginally higher due to framing
- **Memory**: Similar to TCP

For most applications, the overhead is negligible and the benefits (firewall/proxy traversal, browser support) outweigh the costs.
