# MQTT Client Library for Go


[![Go Reference](https://pkg.go.dev/badge/github.com/gonzalop/mq.svg)](https://pkg.go.dev/github.com/gonzalop/mq)
[![Tests](https://github.com/gonzalop/mq/workflows/Tests/badge.svg)](https://github.com/gonzalop/mq/actions)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

A lightweight, idiomatic MQTT client library for Go with full support for v3.1.1 and v5.0 with a unified API, built using only the standard library.

## Supported Features
- ✅ **MQTT v3.1.1 & v5.0**: Full support for both protocol versions
  - **Unified API**: Write modern v5-style code (Properties, Reason Codes) that automatically degrades on v3 servers.
- ✅ **Auto-Reconnect**: Built-in exponential backoff (see [examples/auto_reconnect](./examples/auto_reconnect))
- ✅ **Persistence**: Optional Durable Session Persistence (CleanSession=false) (see [docs/persistence.md](docs/persistence.md))
- ✅ **Transport**: TCP and TLS directly, WebSockets via `WithDialer` (see [examples/websocket](./examples/websocket))
- ✅ **Optimized**: High throughput, low memory footprint
- ✅ **Thread-Safe**: Safe for concurrent use
- ✅ **Context Awareness**: `context.Context` support for cancellation/timeouts
- ✅ **MQTT v5.0 Features**:
  - **Message Properties**: Content Type, User Properties (see [examples/v5_properties](./examples/v5_properties)), Request/Response (see [examples/v5_request_response](./examples/v5_request_response)), Message Expiry
  - **Connection Config**: Session Expiry, Request Problem/Response Info
  - **Bandwidth**: Topic Aliases (Client & Server) (see [examples/topic_aliases](./examples/topic_aliases))
  - **Flow Control**: Receive Maximum, Max Packet Size
  - **Subscription**: NoLocal, RetainAsPublished, RetainHandling, Shared Subscriptions

All features are tested with an extensive unit and integration test suite.

## Performance

The `mq` library is built for high-performance scenarios, consistently outperforming other Go MQTT clients in throughput and resource efficiency. Key highlights from our benchmarks include:
- **High Throughput**: Up to 2.8x faster than other popular implementations.
- **Memory Efficient**: 10x lower memory allocation and 14x fewer GC cycles.
- **Reliable**: 100% message delivery under extreme load where other clients may drop messages.

For a detailed comparative analysis, see the **[Performance Analysis Report](docs/PERFORMANCE_ANALYSIS.md)**.

**NOTE**: new changes after these analyses made `mq` even faster.

## Installation

```bash
go get github.com/gonzalop/mq
```

## Quick Start

For a detailed walkthrough of connecting, publishing, and subscribing, see the **[Getting Started & Usage Guide](docs/getting_started.md)**.

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
    token := client.Subscribe("sensors/+/temperature", mq.AtLeastOnce, func(c *mq.Client, msg mq.Message) {
        fmt.Printf("Topic: %s, Payload: %s\n", msg.Topic, string(msg.Payload))
    })

    // Wait for subscription
    if err := token.Wait(context.Background()); err != nil {
        slog.Error("Subscription failed", "error", err)
        os.Exit(1)
    }

    // Publish a message
    pubToken := client.Publish("sensors/living-room/temperature", []byte("22.5"), mq.WithQoS(mq.AtLeastOnce))
    pubToken.Wait(context.Background())

    time.Sleep(2 * time.Second)
}
```

## 📚 Documentation

- **[Getting Started](docs/getting_started.md)**: Detailed guide on Connecting, Publishing, Subscribing, and Options.
- **[Best Practices](./docs/client_configuration_best_practices.md)**: Production-grade configuration guide (Security, Resource Limits, Session Management). A **MUST**-read.
- **[Troubleshooting](./docs/troubleshooting.md)**: Solutions for common issues like client ID thrashing, zombie messages, and flow control.
- **[Persistence](./docs/persistence.md)**: Detailed guide on configuring durable sessions across restarts.
- **[Internals](./docs/internals/CONCURRENCY.md)**: Deep dive into the library's concurrency model.
- **Compliance**: [MQTT 3.1.1](./docs/MQTT_3.1.1_Compliance.md) and [MQTT 5.0](./docs/MQTT_5.0_Compliance.md) compliance reports.


## Examples

See the [examples](./examples) directory for various use cases:
- [`auto_reconnect/`](./examples/auto_reconnect) - Automatic reconnection demonstration
- [`errors/`](./examples/errors) - Proper error handling
- [`lwt/`](./examples/lwt) - Last Will and Testament usage
- [`persistent/`](./examples/persistent) - Persistent sessions and offline messaging
- [`scram_auth/`](./examples/scram_auth) - SCRAM-SHA-256 Authentication (MQTT v5.0)
- [`simple/`](./examples/simple) - Basic publish/subscribe
- [`throughput/`](./examples/throughput) - High-performance throughput benchmarks. See an example of its output and analysis [here](https://gist.github.com/gonzalop/18e2d2ab28c94d32866f1a0d1668c523).
- [`tls/`](./examples/tls) - Secure connection with optional client certs
- [`topic_aliases/`](./examples/topic_aliases) - Bandwidth optimization using Topic Aliases
- [`v5_properties/`](./examples/v5_properties) - MQTT v5.0 properties (ContentType, UserProperties, etc.)
- [`v5_request_response/`](./examples/v5_request_response) - Request/response pattern using v5.0
- [`websocket/`](./examples/websocket) - Connecting via WebSockets (BYOC pattern)
- [`wildcards/`](./examples/wildcards) - Using wildcard subscriptions

## Contributing

Contributions are welcome! Please see [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines on development, testing, and submitting pull requests.

## Acknowledgements

This library was developed with the assistance of [Antigravity](https://antigravity.google), powered by [Gemini](https://deepmind.google/technologies/gemini/) and [Claude](https://www.anthropic.com/claude).

## License

This software is under the MIT License.
See [LICENSE](LICENSE) file for details.
