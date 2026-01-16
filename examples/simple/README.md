# Simple Example

A basic MQTT client example demonstrating connection, subscription, and publishing with different QoS levels.

## Features Demonstrated

- Connecting to an MQTT server
- Authentication with username/password
- Subscribing to topics
- Publishing messages with QoS 0, 1, and 2
- Receiving and handling messages
- Context-based timeouts

## Running the Example

```bash
go run main.go [server] [username] [password]
```

Examples:
```bash
# Local server without authentication
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

1. Connects to the MQTT server
2. Subscribes to `test/topic`
3. Publishes three messages with different QoS levels:
   - QoS 0: Fire and forget
   - QoS 1: At least once delivery (waits for PUBACK)
   - QoS 2: Exactly once delivery (waits for PUBREC/PUBREL/PUBCOMP)
4. Receives and displays the messages
5. Disconnects gracefully

## Example Output

```
Connecting to MQTT server at tcp://localhost:1883...
✓ Connected successfully!

Subscribing to 'test/topic'...
✓ Subscribed successfully!

Publishing test messages...
  Publishing QoS 0 message...
  Publishing QoS 1 message...
  ✓ QoS 1 acknowledged
  Publishing QoS 2 message...
  ✓ QoS 2 acknowledged

Waiting for messages (5 seconds)...
📨 Received: topic=test/topic qos=0 payload=Hello QoS 0
📨 Received: topic=test/topic qos=1 payload=Hello QoS 1
📨 Received: topic=test/topic qos=2 payload=Hello QoS 2

✓ Received 3 messages

Test completed successfully! 🎉
```

## QoS Levels Explained

- **QoS 0 (AtMostOnce)**: Message delivered at most once, no acknowledgment
- **QoS 1 (AtLeastOnce)**: Message delivered at least once, requires PUBACK
- **QoS 2 (ExactlyOnce)**: Message delivered exactly once, full handshake (PUBREC/PUBREL/PUBCOMP)
