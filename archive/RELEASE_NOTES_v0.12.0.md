# Release v0.12.0

This release prepares the public API for `v1.0.0` stability (Go 1.27+ with `encoding/json/v2`, typed enums across options and interfaces, and unexported internal option structs), completes ordered and duplicate v5.0 `UserPropertyList` support across all MQTT packets, and exposes `ServerReference` and user properties on rejected `CONNACK` errors. It also includes a substantial reliability, protocol-validation, and performance pass covering reconnect synchronization, ordered lock-free session store writes, UTF-8 and packet-size validation, zero-allocation packet sizing, and a streamlined CI/Makefile setup.

---

## ⚠️ Breaking Changes

- **Go 1.27+ Required**: `go.mod` now requires Go `1.27` (`FileStore` migrated from `encoding/json` to `encoding/json/v2`).
- **Typed Enums Across Public APIs**:
  - Added typed enums `ProtocolVersion`, `PayloadFormat`, and `RetainHandling` with named constants (`ProtocolV311`/`ProtocolV50`, `PayloadFormatBytes`/`PayloadFormatUTF8`, `RetainHandlingSend`/`RetainHandlingSendIfNew`/`RetainHandlingDoNotSend`), replacing untyped `uint8` parameters and fields in `WithProtocolVersion`, `Client.ProtocolVersion()`, `WithPayloadFormat`, `Properties.PayloadFormat`, `PublishProperties.PayloadFormat`, `WithRetainHandling`, and `PersistedSubscriptionOptions.RetainHandling`.
  - `QoS` is now used in `WithWill`, `ServerCapabilities.MaximumQoS`, `PersistedPublish.QoS`, and `PersistedSubscription.QoS` instead of `uint8`.
  - `Authenticator.HandleChallenge(challengeData []byte, reasonCode ReasonCode)` now accepts `ReasonCode` instead of `uint8`.
- **Unexported Option Structs & Removed Unused Field**: `PublishOptions`, `SubscribeOptions`, `UnsubscribeOptions`, and `DisconnectOptions` are now unexported (the functional option types `PublishOption`, `SubscribeOption`, `UnsubscribeOption`, and `DisconnectOption` remain exported). Removed the unused `SubscriptionIdentifier` field from `PublishProperties`.
- **`Dial` Fails on `SessionStore.Load` Errors**: `Dial` and `DialContext` now return the wrapped error when `SessionStore.Load` fails instead of logging a warning and connecting with a partially restored session. `FileStore` continues to skip individual corrupt or unreadable entry files and only returns an error when a directory cannot be read.

---

## 🚀 New Features & Enhancements

- **Ordered & Duplicate `UserPropertyList` Across All Packets**:
  - `CONNECT`: Added `WithConnectUserProperty(key, value string)` to append user properties in call order while allowing duplicate keys.
  - `CONNACK`: Added `Client.ConnectionUserPropertyList() []UserProperty` to retrieve server connection user properties in wire order with duplicates preserved.
  - `DISCONNECT`: Added `DisconnectError.UserPropertyList`.
  - `SUBSCRIBE` / `UNSUBSCRIBE` & Persistence: Added `UserPropertyList` to `PersistedSubscriptionOptions`, updated `resubscribeAll` grouping to key off ordered user-property lists, and updated `WithSubscribeUserProperty` and `WithUnsubscribeUserProperty` to preserve call order and duplicate keys.
- **`ServerReference` & User Properties on Rejected `CONNACK`**: `MqttError` now includes `ServerReference`, `UserProperties`, and `UserPropertyList`, populated from rejected MQTT v5.0 `CONNACK` packets. When a rejected `CONNACK` provides a `ServerReference`, `Client.ServerReference()` is updated and `WithOnServerRedirect` is invoked asynchronously.

---

## 🔄 Behavior Changes

- **`WithSubscribeUserProperty` and `WithUnsubscribeUserProperty` Append**: Calling `WithSubscribeUserProperty` or `WithUnsubscribeUserProperty` multiple times with the same key now sends all pairs in order instead of overwriting earlier values.
- **Default v5.0 `MaximumPacketSize` Advertised on `CONNECT`**: `CONNECT` packets now always include `MaximumPacketSize` in MQTT v5.0 using the effective incoming packet limit (`DefaultMaxPacketSize` = 1 MiB when `MaxIncomingPacket` is 0, capped at `MaxAllowedPacketSize`).
- **Rejected `UNSUBSCRIBE` Restores Removed Subscriptions**: Local subscriptions are removed before sending `UNSUBSCRIBE`, and are restored on broker rejection (`UNSUBACK`) or cancellation/shutdown before enqueueing unless superseded by a newer `Subscribe` or `Unsubscribe`. On v5.0, `UNSUBACK` packets whose reason-code count does not match the number of topics fail the operation.

---

## 🐛 Bug Fixes

### Protocol Compliance & Validation

- **MQTT UTF-8, Null Byte & Topic Validation**: Client ID, username, content type, response topic, reason string, user property keys/values, and authentication method are validated as valid UTF-8 with no null bytes (`[MQTT-1.5.4-1]`, `[MQTT-1.5.4-2]`). `ResponseTopic` rejects `+` and `#` wildcards, `WithWill` validates the will topic as a publish topic, and publish/subscribe topic length limits are capped at 65,535 bytes.
- **Total Packet Size Check**: `ReadPacket` now validates the total packet size (fixed header + remaining length) against `EffectiveMaxPacketSize`, and `MaxAllowedPacketSize` includes the 5-byte fixed header (`1 + 4 + 268435455`).
- **Empty v5.0 `UNSUBACK` Rejected**: `DecodeUnsuback` rejects MQTT v5.0 `UNSUBACK` packets with no reason codes.
- **Resent `PUBLISH` Version Normalized on Fallback**: When protocol negotiation falls back from v5.0 to v3.1.1, resent QoS 1/2 `PUBLISH` packets normalize their `Version` and clear v5.0 properties and `UseAlias`.

### Connection Lifecycle & Reconnects

- **Server `DISCONNECT` Handled Inline**: `runReadLoop` now handles an incoming `DISCONNECT` packet directly before exiting instead of queueing it onto `incoming`.
- **Initial `Authenticator.InitialData()` Errors Propagated**: Errors from `Authenticator.InitialData()` now fail `CONNECT` instead of only being logged.
- **`CONNECT` Send Failure Clears `c.conn`**: Failed `sendConnectPacket` calls clear `c.conn` under `connLock` before closing the socket.
- **Wait for Write Loop & `resubscribeAll` on Reconnect/Shutdown**: `drainPreviousConnection` now waits for both the previous write loop and `resubscribeAll` in addition to the read loop, and `finalizeConnection` registers loop counts before releasing `connLock` so a concurrent `Disconnect` cannot call `wg.Wait` early. `resubscribeAll` also checks `connDone` and `c.stop` before sending each batch.
- **Full `outgoing` Queue Cannot Stall Reconnection**: `sendPackets` no longer blocks on a full `outgoing` channel while `drainPreviousConnection` is draining for a reconnect.
- **No Duplicate `SUBSCRIBE`s on Reconnect**: `resubscribeAll` skips topics that already have a pending `SUBSCRIBE` in `c.pending`, avoiding duplicate packets alongside `takeResendLocked`.
- **In-Flight Caller Packets Not Resent Twice**: `takeResendLocked` skips pending operations whose caller is still handing the packet to `outgoing`.
- **Stale Session Acks Dropped**: When the broker starts a fresh session (`SessionPresent=false`), `sessionEpoch` is incremented and acknowledgments from earlier epochs are dropped in the write loop.

### Subscriptions, Persistence & Concurrency

- **Cancelled `Subscribe` Restores Replaced Subscription**: Rolling back a `Subscribe` cancelled before enqueueing now restores any subscription it replaced (when still owned by that request) instead of removing the active subscription.
- **Ordered Store Writes Outside `sessionLock`**: Session store writes are queued under `sessionLock` and flushed in order outside the lock; incoming QoS 2 IDs are flushed to disk before running message handlers.
- **`AsyncStore` Negative Buffer Size Clamped**: `NewAsyncStore` clamps negative `bufferSize` values to `0`.
- **Logging Outside `sessionLock`**: `handleSuback`, `enforceReceiveMaximum`, `resubscribeAll`, and `internalUnsubscribe` emit `slog` records after releasing `sessionLock`.
- **Single Handler Context Creation**: With `HandlerTimeout` configured, only the timeout context is created per handler invocation.

---

## ⚡ Performance

- **Lock-Free Packet Sizing**: Added `EncodedSize()` to `PublishPacket`, `SubscribePacket`, `UnsubscribePacket`, and `Properties` so `internalPublish` checks `MaximumPacketSize` without acquiring `sessionLock` or writing to `io.Discard`.
- **Fewer Goroutines for QoS 1/2 Acks**: Single-handler QoS 1/2 deliveries execute the handler and send the ack in one goroutine without an extra `WaitGroup` waiter goroutine, and QoS 0 deliveries skip `WaitGroup` setup.
- **Shared QoS Ack Codecs & Zero-Alloc Byte Writes**: Consolidated `PUBACK`, `PUBREC`, `PUBREL`, and `PUBCOMP` codecs in `internal/packets/ack.go` and replaced single-byte `binary.Write` calls with `writeByte`.
- **`AsyncStore` Double-Buffered Queue**: `AsyncStore.run` swaps between two buffers to process queued writes in batches without steady-state reallocations.

---

## 📚 Documentation

- Updated godoc comments, [`README.md`](../README.md), [`CONTRIBUTING.md`](../CONTRIBUTING.md), and guides under [`docs/`](../docs/) for the `v1.0`-ready typed APIs, Go 1.27+ requirement, semantic versioning commitment, and refreshed benchmark numbers.

---

## 🧪 Testing, Build & CI

- **Expanded CI & Standardized `Makefile`**: CI ([`.github/workflows/test.yaml`](../.github/workflows/test.yaml)) now targets Go 1.27 and runs `gofmt`, `make lint` (`go vet`, `revive`, `staticcheck`), `make vuln` (`govulncheck`), example builds, short fuzz smoke runs, unit tests, and integration tests. Updated the Mosquitto integration test container to `2.1.4` and updated example/integration dependencies.
- **Hardened Tests & Examples**: Added unit tests for `UserPropertyList` across all packets, `MqttError` redirect/property population, UTF-8/null-byte/will-topic/packet-size validation, `EncodedSize`, `SessionStore.Load` failures, `UNSUBACK` restore/mismatch handling, `AsyncStore` close/capacity edge cases, and `HandlerTimeout` cancellation. Fixed data races in `examples/auto_reconnect` and `examples/v5_request_response`, tightened integration test assertions (`ReceiveMaximum`, `NoLocal`, `AssignedClientID`), and shortened fixed test waits.

---

## 📦 Installation

```bash
go get github.com/gonzalop/mq@v0.12.0
```
