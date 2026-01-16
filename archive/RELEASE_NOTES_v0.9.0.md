# Release v0.9.0

This is the initial release of `mq`, a high-performance, thread-safe, and fully compliant MQTT v5.0 and v3.1.1 client for Go. Designed for throughput and reliability, it features a modern concurrency model, low memory footprint, and a user-friendly functional options API.

## 🚀 Key Features

### Protocol Support
- **Unified API**: Write code once using modern v5 concepts (Reason Codes, Properties); the library automatically degrades gracefully when connecting to v3.1.1 servers.
- **Feature Rich**: Implements all v5.0 features including User Properties, Reason Codes, Session Expiry, Topics Aliases, and Shared Subscriptions.
- **Automatic Handling**: Transparent handling of keepalives, reconnections, and protocol negotiation.

### Modern Concurrency Model
- **Shared-State Architecture**: Uses a highly optimized, mutex-protected state model instead of traditional actor/channel bottlenecks.
- **High Throughput**: Capable of processing high message rates in concurrent workloads, as verified by extensive benchmarks.
- **Low Latency**: Parallel publication path ensures minimal blocking for high-concurrency applications.
- **Async Callbacks**: All user hooks (`OnConnect`, `MessageHandler`, etc.) are executed asynchronously to prevent deadlocks and blocking the control loop.

### Robustness & Reliability
- **Auto-Reconnect**: Robust reconnection logic with exponential backoff to handle network instability.
- **Persistence**: Optional interface-based session storage for reliable message delivery across restarts. Includes a built-in `FileStore` implementation.
- **Resource Protection**: Strict limits on packet sizes, topic lengths, and topic aliases to mitigate memory exhaustion and DoS attacks.
- **Flow Control**: Respects `ReceiveMaximum` and server limits to ensure protocol compliance under load.
- **Fuzz Testing**: All packet parsing code is fuzz-tested to ensure robustness against malformed inputs.

### Connectivity
- **Custom Transports**: Support for alternative transports like **WebSockets** or **QUIC** via `WithDialer`, allowing full control over the connection layer.

## 🔒 Security

- **Enhanced Authentication**: Support for customizable authenticators, including a **SCRAM-SHA-256** implementation in the [examples](../examples/).
- **TLS Support**: Easy configuration for secure connections (`tls://`, `mqtts://`) via `WithTLS`.
- **Zero-Dependency**: Core library has zero external dependencies, minimizing the supply chain attack surface.
- **Security Policy**: See [SECURITY.md](../SECURITY.md) for vulnerability reporting and supported versions.

## 🛠️ Usage Experience

### Functional Options API
Configuration is handled via a clean, extensive functional options pattern:
```go
client, err := mq.Dial("tcp://localhost:1883",
    mq.WithClientID("my-app"),
    mq.WithCleanSession(false),
    mq.WithKeepAlive(30*time.Second),
    mq.WithOnConnect(func(c *mq.Client) {
        log.Println("Connected!")
    }),
)
```
### Comprehensive Documentation
- **Getting Started**: Step-by-step guide for connecting, publishing, and subscribing ([`docs/getting_started.md`](../docs/getting_started.md)).
- **Best Practices**: Production-grade configuration guide covering security, resource limits, and session management ([`docs/client_configuration_best_practices.md`](../docs/client_configuration_best_practices.md)).
- **Troubleshooting**: Solutions for common issues ([`docs/troubleshooting.md`](../docs/troubleshooting.md)).
- **Internals**: Detailed architectural docs ([`docs/internals/CONCURRENCY.md`](../docs/internals/CONCURRENCY.md)) explaining the threading model.
- **Examples**: A wide range of runnable examples in [`examples/`](../examples/) covering everything from "hello world" to complex SCRAM auth, WebSockets, TLS, and persistent sessions, along with throughput tests.

### Context & Select Support
Unlike older libraries that block indefinitely, all asynchronous operations return a `Token` that exposes a `Done()` channel. This allows usage with `context.Context` for timeouts or directly within `select` statements for complex control flow:
```go
token := client.Publish("data", payload, mq.WithQoS(1))

// Option 1: Block with Timeout
err := token.Wait(context.WithTimeout(ctx, 5*time.Second))

// Option 2: Non-blocking Select
select {
case <-token.Done():
    fmt.Println("Published!")
case <-shutdownChan:
    return
}
```

## ✅ Testing & Quality

- **Extensive Test Suite**: 61 test files with comprehensive unit tests, all run with race detector (`-race`) enabled.
- **Integration Tests**: 22 integration tests using real MQTT servers (Mosquitto, Mochi (found a bug, patched), nanomq (lacks behind the others in compliance)) via testcontainers. You can build local images of mochi and nanomq with the Dockerfile's in the [`extras/`](../extras/) folder.
- **Fuzz Testing**: Packet parsing code is continuously fuzz-tested for robustness.
- **Compliance**: Full MQTT v3.1.1 and v5.0 compliance verified through dedicated test suites.

## 📦 Installation

```bash
go get github.com/gonzalop/mq@v0.9.0
```
