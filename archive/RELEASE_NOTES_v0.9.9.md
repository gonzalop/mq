# Release v0.9.9

This release resolves a critical keepalive ping state bug during auto-reconnection, ensures ReceiveMaximum quota warning flags are reset per connection lifecycle, and updates integration dependencies.

---

## 🐛 Bug Fixes & Keepalive Reliability

- **Keepalive Ping State Reset on Reconnect**: Fixed an issue where `c.pingPending` remained `true` on the long-lived `Client` struct across automatic reconnections if a disconnect occurred while a `PINGREQ` was inflight. Because `writeLoop` checks `!c.pingPending` before sending a `PINGREQ`, a stuck `pingPending = true` state prevented the client from ever sending another `PINGREQ` on subsequent connections. On quiet connections publishing only QoS 0 messages (which produce no inbound ACKs), `lastReceived` was never updated, causing every reconnection to continuously time out after 1.5x keepalive in an endless loop. `pingPending` is now reset to `false` and `pingPendingCh` is drained in `writeLoop`, `handleDisconnect`, and `internalResetState`.
- **Quota Warning Reset**: `c.receiveMaxExceededLogged` is now reset to `false` in `prepareConnectionState()`, ensuring warnings for exceeding the `ReceiveMaximum` quota are logged per connection rather than suppressed after the initial occurrence.

---

## 🛡️ Integration & Dependencies

- **Integration Dependencies**: Upgraded `github.com/moby/moby/api` to `v1.55.0` in `/integration`.
- **Integration Suite Verification**: Verified the full integration test suite against containerized `eclipse-mosquitto:2` brokers via Podman/Docker.

---

## 📦 Installation

```bash
go get github.com/gonzalop/mq@v0.9.9
```
