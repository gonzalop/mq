# Release v0.9.3

This release focuses on enhancing MQTT v5.0 compliance by exposing more connection-level metadata and addressing a protocol edge case related to Topic Aliases during reconnections.

## 🚀 New Features

### Enhanced DISCONNECT Support
- **`DisconnectError`**: Introduced a specialized error type for server-initiated DISCONNECT packets. When a server closes the connection with an MQTT v5.0 DISCONNECT, the error passed to `OnConnectionLost` can now be inspected for:
  - **Reason Code & String**: Human-readable explanation from the server.
  - **Server Reference**: Redirection URI for load balancing or maintenance.
  - **Session Expiry**: Updated interval if the server changed it.
  - **User Properties**: Custom metadata (e.g., `maintenance: true` or `retry-after: 60`).

### Connection Metadata (User Properties)
- **`WithConnectUserProperties`**: New functional option to send custom key-value pairs in the MQTT v5.0 CONNECT packet.
- **`ConnectionUserProperties()`**: New client method to retrieve User Properties sent back by the server in the CONNACK packet, enabling richer application-level handshaking.

## 🩹 Bug Fixes

### Topic Alias Reliability
- **Connection Reset**: Fixed an [issue](https://github.com/gonzalop/mq/issues/1) where Topic Aliases were incorrectly preserved across reconnections for queued packets. Since MQTT v5.0 Topic Aliases are connection-scoped, using a stale alias on a new connection would result in a protocol error (0x94).

## ⚡ Performance & Documentation
- **Concurrency Docs**: Updated internal documentation regarding the shared-state architecture and mutex-protected model.
- **Mosquitto 2.1.1**: Updated the provided Dockerfile to Mosquitto v2.1.1 for local testing and integration.

## ✅ Testing & Quality
- **PUB* Coverage**: Added comprehensive unit and fuzz tests for all publish acknowledgement packets (`PUBACK`, `PUBREC`, `PUBREL`, `PUBCOMP`).
- **Regression Tests**: Added dedicated tests for Topic Alias behavior during network instability and server restarts.

## 📦 Installation

```bash
go get github.com/gonzalop/mq@v0.9.3
```
