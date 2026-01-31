# Release v0.9.2

This release brings significant performance optimizations, enhanced protocol compliance with client-side flow control, and better observability into client operations.

## 🚀 New Features

### Client-Side Flow Control
- **Receive Maximum Enforcement**: The client now actively enforces the MQTT v5.0 `Receive Maximum` property to protect against message floods from the server.
- **Configurable Policy**: Introduced `LimitPolicy` to control how the client reacts when a server violates the negotiated limit:
  - **`LimitPolicyIgnore` (Default)**: Logs a warning but maintains the connection, ensuring robustness against servers with strict but slightly buggy counting logic.
  - **`LimitPolicyStrict`**: Immediately disconnects with a protocol error (Reason Code 0x93) if the server exceeds the limit.
  ```go
  opts := []mq.Option{
      mq.WithReceiveMaximum(10, mq.LimitPolicyStrict),
  }
  ```

### Observability
- **`GetStats`**: A new method `client.GetStats()` provides real-time visibility into the client's network usage and state.
  - Returns `ClientStats` containing:
    - Bytes read/written
    - Packets read/written
    - Reconnect count
    - Current connection state

## ⚡ Performance

### Optimized I/O
- **Reduced Syscalls**: The network layer now utilizes buffered I/O (`bufio.Reader`), reducing system calls during packet reading. This yields a **~35% performance improvement** in packet decoding for large payloads.
- **Memory Locality**: Refactored internal packet decoding to reduce pointer chasing and allocations.
- **Throughput**: Overall client publish throughput has increased by approximately **8%** in benchmark scenarios.

## 📝 Documentation & Examples

- **`DialContext`**: Clarified the precedence of `DialContext` over `WithConnectTimeout` for the initial handshake versus subsequent reconnections.
- **WebSockets**: Updated `WithDialer` documentation with a correct, production-ready example for establishing WebSocket connections, including the required MQTT subprotocol negotiation.
- **Mosquitto 2.1**: Added compatibility verification and performance benchmarks for Mosquitto v2.1.0.

## ✅ Testing

- **Fuzz Testing**: Expanded the fuzzing corpus to cover more edge cases in packet parsing.
- **Integration Tests**: Improved stability and timing for Last Will and Testament (LWT) integration tests.
