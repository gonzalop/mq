# Integration Tests

This directory contains integration tests that require external dependencies (Docker, testcontainers).

## Why a Separate Module?

The integration tests are in a separate Go module to keep the main `mq` library dependency-free. This means:

- ✅ The main library has **zero external dependencies** (only Go standard library)
- ✅ Users who import `github.com/gonzalop/mq` don't pull in testcontainers and its dependencies
- ✅ Integration tests can use whatever dependencies they need without polluting the main module

## Running Integration Tests

### Prerequisites

- Docker or Podman must be installed and running
- Go 1.24 or later

### Run Tests

The easiest way to run the integration tests is using the project's Makefile from the root directory. This handles container engine detection (Docker vs Podman) and environment setup automatically.

```bash
# From the project root
make integration
```

If you prefer to run them manually:

```bash
cd integration
# For Docker
TESTCONTAINERS_RYUK_DISABLED=true go test -v -timeout=5m

# For rootless Podman (adjust socket path as needed)
DOCKER_HOST=unix:///run/user/$(id -u)/podman/podman.sock TESTCONTAINERS_RYUK_DISABLED=true go test -v -timeout=5m
```

### Testing with Other Servers (Mochi, NanoMQ, Mosquitto)

By default, tests run against `eclipse-mosquitto:2`. You can test against other servers like **Mochi**, **NanoMQ**, or a custom **Mosquitto** version by setting the `MQTT_SERVER_IMAGE` environment variable.

The [`../extras/`](../extras/) folder contains Dockerfiles for building custom images suitable for testcontainers:

```bash
# Build Mochi image
cd ../extras/mochi && docker build -t localhost/minimal-mochi:latest .

# Build NanoMQ image
cd ../extras/nanomq && docker build -t localhost/minimal-nanomq:latest .

# Build custom Mosquitto image (e.g. v2.1rc2)
cd ../extras/mosquitto && docker build -t localhost/minimal-mosquitto:latest .

# Run tests against Mochi (all tests should pass with our patch)
MQTT_SERVER_IMAGE=localhost/minimal-mochi:latest make integration

# Run tests against NanoMQ (some tests may fail due to compliance gaps on the server side)
MQTT_SERVER_IMAGE=localhost/minimal-nanomq:latest make integration

# Run tests against custom Mosquitto
MQTT_SERVER_IMAGE=localhost/minimal-mosquitto:latest make integration
```

*Note: Some tests requiring specific Mosquitto configurations (like `persistence false`) may skip or behave differently if the server image doesn't support `mosquitto.conf` injection. The test setup automatically detects NanoMQ and adjusts startup parameters.*

*Note: The following are the integration tests that fail with current nanomq built with `../extras/nanomq/Dockerfile`.
I am not sure if I am configuring it wrong or nanomq just doesn't support the feature or doesn't follow the spec.

$ MQTT\_SERVER\_IMAGE=localhost/minimal-nanomq:latest make integration 2>&1 | grep FAIL
--- FAIL: TestServerLimits (0.28s)
    --- FAIL: TestServerLimits/MaximumPacketSize (0.10s)
    --- FAIL: TestServerLimits/ReceiveMaximum (0.10s)
--- FAIL: TestDefaultPublishHandlerIntegration (5.56s)
--- FAIL: TestLastWillWithDelay (5.66s)
--- FAIL: TestPersistenceIntegration (0.00s)
    --- FAIL: TestPersistenceIntegration/ServerSessionResumed (5.59s)
--- FAIL: TestSessionExpiry (0.00s)
    --- FAIL: TestSessionExpiry/SessionPersistence (5.30s)
    --- FAIL: TestSessionExpiry/OrphanedSubscription (5.30s)
--- FAIL: TestCleanSession (7.36s)
--- FAIL: TestKeepAliveDisabled (10.00s)
*

### What's Tested

The integration tests use testcontainers to spin up real MQTT servers and test:

1. **Basic Publish/Subscribe** - QoS 0, 1, 2
2. **Wildcard Subscriptions** - `+` and `#` wildcards
3. **Retained Messages** - Message persistence
4. **Multiple Subscribers** - Broadcast to multiple clients
5. **High Throughput** - 1000 messages/sec
6. **Unsubscribe** - Proper cleanup
7. **Clean Session & Persistence**
   - Persistent vs clean sessions
   - Offline message recovery
8. **Connection Recovery**
   - Auto Reconnect
   - Last Will & Testament (LWT) with Delay
   - Lifecycle Hooks (OnConnect, OnConnectionUp, etc.)
9. **MQTT 5.0 Features**
   - Topic Aliases
   - Subscription Options (NoLocal, RetainAsPublished, etc.)
   - Session Expiry
   - User Properties
   - Server Limits
   - Assigned Client ID
   - Reason Codes
10. **Protocol Isolation & Compliance**
    - V3.1.1 strict isolation
    - Compliance scenarios
    - Error handling
11. **Client Limits**
    - Max Topic Length validation
    - Max Payload Size validation
    - Max Incoming Packet enforcement

## CI/CD

In CI pipelines, you can run integration tests separately:

```bash
# Run unit tests (main module)
go test ./...

# Run integration tests (separate module)
cd integration && go test -v
```

## 🚀 Architecture & Performance

Tests use **Smart Container Reuse** and **Dynamic Ports** for reliability:
- **Dynamic Port Mapping**: Tests bind to random available ports on the host instead of hardcoded 1883/1884. This prevents conflicts with local servers or parallel test runs.
- **Shared Server**: A single server instance handles most tests to save startup time.
- **Isolated Instances**: Tests needing custom config (e.g., persistence settings, restarts) get a dedicated container.
- **Automatic Cleanup**: All containers are automatically removed after execution or if interrupted (Ctrl+C).

## Adding New Integration Tests

Simply use the `startMosquitto(t, config)` helper:
- Pass `""` for config to use the **shared** server.
- Pass a config string to automatically spin up an **isolated** container.
