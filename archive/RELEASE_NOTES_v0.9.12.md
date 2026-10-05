# Release v0.9.12

This release introduces anti-flapping backoff escalation to prevent tight reconnect loops during short-lived connection drops, adds the `WithMinStableConnectionDuration` option to tune stability thresholds, and provides new `ConnectedAt()` and `Uptime()` client accessors for connection monitoring.

---

## 🐛 Bug Fixes & Stability Improvements

- **Anti-Flapping Reconnect Backoff Escalation**: Fixed an issue where the reconnection loop immediately reset backoff to the base interval upon receiving `CONNACK`. In scenarios where a connection was severed shortly after establishing (e.g., duplicate client ID takeovers, broker kicks, or intermittent network flaps), the client would reconnect aggressively at the base backoff indefinitely. The reconnection loop now tracks connection uptime and escalates backoff exponentially if a connection dies before reaching the stability threshold. Backoff is only reset to the base interval after a connection remains stable past the threshold (Fixes [#24](https://github.com/gonzalop/mq/issues/24)).

---

## 🚀 New Features & Enhancements

- **Configurable Connection Stability Threshold (`WithMinStableConnectionDuration`)**: Added `WithMinStableConnectionDuration(duration time.Duration)` to configure the minimum uptime required for a connection to be considered stable before resetting reconnection backoff. Defaults to 5 seconds (or `max(2 * baseBackoff, 2s)`). Escalation can be disabled by passing a negative duration (e.g., `-1`).
- **Connection Timing Accessors (`ConnectedAt` & `Uptime`)**:
  - `client.ConnectedAt() time.Time`: Returns the timestamp when the current connection was established, or zero `time.Time` if not connected.
  - `client.Uptime() time.Duration`: Returns the duration for which the current connection has been active, or `0` if not connected.

---

## 🧪 Testing

- **Reconnect Backoff & Flapping Tests**: Added comprehensive test coverage in [`reconnect_backoff_test.go`](file:///home/gonzalo/go/src/github.com/gonzalop/mq/reconnect_backoff_test.go) verifying backoff escalation on flapping connections, backoff reset on stable connections, disabling escalation behavior via negative durations, and `ConnectedAt`/`Uptime` tracking.

---

## 📦 Installation

```bash
go get github.com/gonzalop/mq@v0.9.12
```
