# Wildcard Subscriptions Example

This example demonstrates MQTT wildcard subscriptions using `+` (single-level) and `#` (multi-level) wildcards.

## Wildcard Types

### Single-Level Wildcard (`+`)
Matches exactly one topic level.

Examples:
- `sensors/+/temperature` matches:
  - ✅ `sensors/bedroom/temperature`
  - ✅ `sensors/kitchen/temperature`
  - ❌ `sensors/bedroom/living-room/temperature` (too many levels)

### Multi-Level Wildcard (`#`)
Matches zero or more topic levels (must be last character).

Examples:
- `sensors/bedroom/#` matches:
  - ✅ `sensors/bedroom/temperature`
  - ✅ `sensors/bedroom/humidity`
  - ✅ `sensors/bedroom/light/brightness`
  - ❌ `sensors/kitchen/temperature` (different room)

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

1. Subscribes to multiple wildcard patterns
2. Publishes test messages to various topics
3. Shows which subscriptions matched which topics
4. Displays a summary of matches

## Example Output

```
Wildcard Subscriptions Example
===============================

✓ Connected successfully!

📡 Subscribing to: sensors/+/temperature
   Single-level wildcard: matches any sensor's temperature
📡 Subscribing to: sensors/bedroom/#
   Multi-level wildcard: matches all bedroom sensor topics
📡 Subscribing to: home/+/+/status
   Multiple single-level: matches home/{room}/{device}/status
📡 Subscribing to: #
   Match everything (use with caution!)

✓ All subscriptions active!

📤 Publishing test messages...

Publishing to: sensors/living-room/temperature
   📨 [sensors/+/temperature] received from: sensors/living-room/temperature
   📨 [#] received from: sensors/living-room/temperature

Publishing to: sensors/bedroom/temperature
   📨 [sensors/+/temperature] received from: sensors/bedroom/temperature
   📨 [sensors/bedroom/#] received from: sensors/bedroom/temperature
   📨 [#] received from: sensors/bedroom/temperature

============================================================
📊 Summary of Messages Received by Each Filter:
============================================================

sensors/+/temperature (2 messages):
  ✓ sensors/living-room/temperature
  ✓ sensors/bedroom/temperature

sensors/bedroom/# (3 messages):
  ✓ sensors/bedroom/temperature
  ✓ sensors/bedroom/humidity
  ✓ sensors/bedroom/light/brightness
```

## Use Cases

- **IoT Sensors**: Subscribe to all sensors of a type (`sensors/+/temperature`)
- **Room Monitoring**: Subscribe to all devices in a room (`home/bedroom/#`)
- **Device Status**: Subscribe to status of all devices (`devices/+/status`)
- **Debugging**: Temporarily subscribe to everything (`#`)
