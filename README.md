# MQTT Client Library for Go


[![Go Reference](https://pkg.go.dev/badge/github.com/gonzalop/mq.svg)](https://pkg.go.dev/github.com/gonzalop/mq)
[![Tests](https://github.com/gonzalop/mq/workflows/Tests/badge.svg)](https://github.com/gonzalop/mq/actions)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

A lightweight, idiomatic MQTT client library for Go with full support for v3.1.1 and v5.0 with a unified API, built using only the standard library.

## Supported Features
- **MQTT v3.1.1 & v5.0**: Full support for both protocol versions
  - **Unified API**: Write modern v5-style code (Properties, Reason Codes) that automatically degrades on v3 servers.
  - **Auto-Negotiation**: Automatically falls back to v3.1.1 if v5.0 is not supported by the server.
- **High Performance**:
  - **Radix Tree Routing**: O(K) topic matching for high-subscription environments.
  - **Non-blocking I/O**: Core state machine uses a non-blocking logic loop to maximize throughput.
- **Persistence**: Durable Session Persistence (CleanSession=false) with incremental disk storage and optional asynchronous background worker (see [docs/persistence.md](docs/persistence.md)).
- **Auto-Reconnect**: Built-in exponential backoff (see [Examples](./examples))
- **Transport**: TCP and TLS directly, WebSockets via `WithDialer` (see [Examples](./examples))
- **Middleware/Interceptors**: Intercept inbound/outbound messages for logging, metrics, or tracing.
- **Context Awareness**: `context.Context` support for cancellation/timeouts
- **MQTT v5.0 Features**:
  - **Message Properties**: Content Type, User Properties, Request/Response, Message Expiry
  - **Connection Config**: Session Expiry, Request Problem/Response Info, User Properties
  - **Bandwidth**: Topic Aliases (Client & Server)
  - **Flow Control**: Receive Maximum, Max Packet Size
  - **Subscription**: NoLocal, RetainAsPublished, RetainHandling, Shared Subscriptions

For code demonstrations of these features, see the **[Examples Index](examples/README.md)**.

## Performance

The library is designed for high-concurrency environments:
- **Throughput**: Up to **3x faster** than Paho v5 in high-concurrency scenarios, with peak rates exceeding **1.3M msg/s**.
- **Radix Tree Routing**: O(K) topic matching for high-subscription environments.
- **Non-blocking I/O**: Core state machine uses a non-blocking logic loop to maximize throughput.
- **Efficiency**: **10x lower memory allocation** and significantly reduced GC overhead compared to alternative libraries.

For a detailed comparative analysis, see the **[Performance Analysis Report](docs/PERFORMANCE_ANALYSIS.md)**.

## Safety and Resilience

In accordance with Go's "fail-fast" philosophy, the library does not catch panics in user-provided callbacks. If a handler panics, the entire process will terminate to prevent running in an inconsistent state.

### Panic Recovery via Middleware

If you require process-level resilience, you can implement a recovery interceptor:

```go
recoveryInterceptor := func(next mq.MessageHandler) mq.MessageHandler {
    return func(c *mq.Client, m mq.Message) {
        defer func() {
            if r := recover(); r != nil {
                c.Options().Logger.Error("Recovered from panic in handler", 
                    "topic", m.Topic, "error", r)
            }
        }()
        next(c, m)
    }
}

client.Subscribe("sensors/#", mq.AtMostOnce, handler, 
    mq.WithHandlerInterceptor(recoveryInterceptor))
```

## Installation

```bash
go get github.com/gonzalop/mq
```

## Quick Start

```go
package main

import (
    "context"
    "fmt"
    "log/slog"
    "os"
    "time"

    "github.com/gonzalop/mq"
)

func main() {
    // Connect to server
    client, err := mq.Dial(
        "tcp://localhost:1883",
        mq.WithClientID("my-client"),
        mq.WithKeepAlive(60*time.Second),
    )
    if err != nil {
        slog.Error("Failed to connect", "error", err)
        os.Exit(1)
    }
    defer client.Disconnect(context.Background())

    // Subscribe to a topic
    client.Subscribe("sensors/+/temperature", mq.AtLeastOnce, func(c *mq.Client, msg mq.Message) {
        fmt.Printf("Topic: %s, Payload: %s\n", msg.Topic, string(msg.Payload))
    })

    // Publish a message
    token := client.Publish("sensors/living-room/temperature", []byte("22.5"), mq.WithQoS(mq.AtLeastOnce))
    
    // Wait for acknowledgment
    if err := token.Wait(context.Background()); err != nil {
        fmt.Printf("Publish failed: %v\n", err)
    }
}
```

## Documentation
- [Getting Started](docs/getting_started.md)
- [Examples Index](examples/README.md)
- [Client Configuration Best Practices](docs/client_configuration_best_practices.md)
- [Auth Patterns](docs/auth.md)
- [Interceptors](docs/interceptors.md)
- [Persistence](docs/persistence.md)
- [Troubleshooting](docs/troubleshooting.md)
- [Performance Analysis](docs/PERFORMANCE_ANALYSIS.md)
- [MQTT 5.0 Compliance](docs/MQTT_5.0_Compliance.md)
- [MQTT 3.1.1 Compliance](docs/MQTT_3.1.1_Compliance.md)

## License
This software is under the MIT License.
See [LICENSE](LICENSE) file for details.
