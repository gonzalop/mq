# Release v0.11.0

This release adds a few API conveniences (subscribe options on `WithSubscription`, `Client.ProtocolVersion()`, ordered and duplicate v5.0 user properties, `NewCompletedToken` for test doubles) and a configurable socket write timeout. It also includes a second audit pass with protocol compliance fixes, stricter field length validation, a `Disconnect` that respects its context when flushing, and fixes for several races and edge cases around reconnects and shutdown.

---

## 🚀 New Features & Enhancements

- **Subscribe Options in `WithSubscription`**: `WithSubscription(topic, handler, opts ...SubscribeOption)` now accepts the same options as `Subscribe` (`WithRetainAsPublished`, `WithNoLocal`, `WithRetainHandling`, `WithSubscriptionIdentifier`, ...). The new `WithSubscriptionQoS(qos)` sets the QoS, which defaults to `AtMostOnce` as before. The options are validated in `Dial`, which returns an error for invalid combinations (for example No Local on a shared subscription).
- **`Client.ProtocolVersion()`**: Returns the protocol version in use (`ProtocolV50` or `ProtocolV311`). With automatic negotiation (the default), this is the version negotiated during `Dial`.
- **Ordered and Duplicate User Properties**: MQTT v5.0 allows the same user property key more than once, and order can matter. `Properties.UserPropertyList` holds all pairs in wire order, and `Properties.AddUserProperty` appends a pair. `UserProperties` (the map) is still filled in, holding the last value for each key. `SetUserProperty` keeps "one value per key" semantics. Persisted publishes keep the list through the new `PublishProperties.UserPropertyList` field.
- **`NewCompletedToken`**: Returns a `Token` that has already completed with the given error (nil for success), so test doubles of code calling `Publish`, `Subscribe` or `Unsubscribe` no longer need their own `Token` implementation.
- **Socket Write Timeout (`WithWriteTimeout`)**: Socket writes now have a deadline (default 30s, `0` disables it). A stalled TCP connection used to hang the write loop indefinitely; it is now detected and goes through the normal `OnConnectionLost` and reconnect path. A stall is detected between `WriteTimeout/2` and `WriteTimeout` after it starts.

---

## 🔄 Behavior Changes

- **`WithUserProperty` Appends**: Calling `WithUserProperty` twice with the same key on a publish now sends both pairs instead of keeping only the last one.
- **`Disconnect` Waits Longer for the `DISCONNECT` Flush**: `Disconnect` used to wait at most 50 ms for the `DISCONNECT` packet to be written, ignoring its context. Behind a burst of publishes on a slow link the connection was closed first, and the broker published the Last Will. If the context has a deadline, that deadline alone now limits the wait. Without a deadline, the wait is capped at 1s so a stalled connection cannot hang `Disconnect`. See the godoc, [`docs/getting_started.md`](../docs/getting_started.md) and [`docs/client_configuration_best_practices.md`](../docs/client_configuration_best_practices.md).
- **Field Length Validation**: Strings and binary fields longer than 65535 bytes (client ID, username, password, will topic and payload, topics, content type, response topic, correlation data, user properties, authentication method and data, ...) are now rejected with an error by `Dial`, `Publish`, `Subscribe`, `Unsubscribe`, `Disconnect` and `Reauthenticate`. They used to reach the encoder and produce invalid packets or panic.
- **Requests Complete on Shutdown**: Pending requests are completed with `ErrClientDisconnected` when the client stops, and their state is cleaned up.
- **Queued Publishes with Expired Contexts Are Not Sent**: Publishes waiting for flow control whose context has expired are now completed with the context error and dropped, even while the broker's Receive Maximum is exhausted.

---

## 🐛 Bug Fixes

### Protocol Compliance

- **Incoming Topic Aliases**: Aliases are rejected when the client's Topic Alias Maximum is 0 [MQTT-3.3.2-11].
- **Empty `AUTH` Packets**: `AUTH` packets with no reason code or properties (meaning Success) are accepted [MQTT-3.15.2-1], including during the handshake, where they used to cause a nil dereference.
- **Variable Byte Integers**: Remaining Length and other varints longer than 4 bytes, or not minimally encoded, are rejected [MQTT-1.5.5-1].
- **Unknown Properties**: Unknown property IDs are rejected instead of being read as bytes.
- **No Reason String in `PUBLISH`**: Reason String is no longer sent in `PUBLISH` or will properties [MQTT-3.3.2].
- **Nothing Written After `DISCONNECT`**: The write loop stops batching once it writes a `DISCONNECT` [MQTT-3.14.4-1].
- **QoS 2 Receive Slot Held Until `PUBREL`**: The receive slot for an incoming QoS 2 message is kept until `PUBREL` arrives.
- **Authentication Failures Drop the Connection**: An authentication method mismatch or a failed challenge now drops the connection. Authenticator output longer than 65535 bytes returns an error from `CONNECT` and re-authentication instead of panicking in the encoder.
- **Packet Size Errors**: Errors for packets that exceed the server's Maximum Packet Size, including properties, now wrap `ErrPacketExceedsServerMax`.

### Subscriptions & Routing

- **Each Handler Gets Its Own Message**: When several subscriptions matched one message, their handlers shared one `Message` and context, racing on `Context` and cancelling each other. Each handler now gets its own copy.
- **Rejected Subscriptions Are Forgotten**: Filters the broker rejects in `SUBACK` are removed from routing, the subscription map and the session store, so they are no longer resubscribed on every reconnect. The same applies to filters left without a return code when a broker sends fewer return codes than requested; the operation then fails.
- **Failed Unsubscribe Keeps the Subscription**: The stored subscription is kept when `UNSUBACK` reports a failure.
- **Resubscribe Bound to Its Connection**: Resubscription runs once per connection, after it is up, and stops if that connection drops. Packet IDs are freed if it runs out of them.

### Connection Lifecycle

- **`Disconnect` Racing the End of a Connect**: If `Disconnect` ran at the very end of a connect, the client could still mark itself connected, run `OnConnect` and start its loops with no connection, so `IsConnected` stayed true for good.
- **Old Packets vs. Alias Reset on Reconnect**: Packets read from the previous connection could still be queued when a reconnect cleared the topic alias map. An old alias-only `PUBLISH` was then resolved against the empty map and the client dropped the new connection as malformed. The client now handles everything from the old connection before resetting per-connection state.
- **Disabled Keep-Alive Kept Across Reconnects**: A keep-alive of 0 (disabled) is no longer lost on reconnect.
- **Sub-Millisecond Keep-Alive**: The keep-alive ticker interval is clamped to at least 1 ms, preventing a `time.NewTicker` panic.
- **Handshake Uses the Dialed Connection**: The handshake no longer re-reads `c.conn`, which `Disconnect` may have cleared.
- **No Connection Close Under `connLock`**: `Disconnect` and server-initiated disconnects close the socket after releasing the lock.
- **Unhandled QoS 0 Publishes**: No nil acknowledgment is queued for a QoS 0 message without a handler.

### Persistence

- **Nil QoS 2 Map from a Custom Store**: A `SessionStore` returning `(nil, nil)` from `LoadReceivedQoS2` caused a panic on the first incoming QoS 2 publish.
- **`FileStore` Directory Permissions**: Directory search permissions are now granted only to roles that can read files in the configured mode, so a `0600` file mode no longer yields group/other traversable directories.
- **`SUBACK` Store Writes Outside `sessionLock`**: Session store writes for `SUBACK` run after releasing the lock, and the token completes after them.

### Races & Leaks

- **`receiveMaxExceededLogged` Race**: The flag was reset without a lock on reconnect while the logic loop could be reading it.
- **Memory Retention in Queues**: Popped entries in the publish queue and in `AsyncStore`'s queue are cleared, so payloads and closures are not kept alive by the backing array.

---

## ⚡ Performance

- **Topic Matching Without Allocations**: Topic matching no longer uses `strings.Split`, removing a slice allocation per message.
- **Cheaper Write Deadlines**: The write deadline is only pushed forward once less than half of `WriteTimeout` is left, instead of being set and cleared on every write. Single-worker QoS 1/2 throughput is back to where it was before write deadlines were added.

---

## 📚 Documentation

- Documented `Disconnect` flush timing and its effect on the Last Will.
- Fixed the `MaxHandlerConcurrency` and `HandlerTimeout` docs.
- The persistence example in `docs/client_configuration_best_practices.md` wraps `FileStore` in `AsyncStore`.

---

## 🧪 Testing

- New integration tests against Mosquitto for rejected subscriptions (dynamic-security plugin, v5.0 and v3.1.1: token error and reason code, no resubscribe after reconnect, removal from the session store) and for `Disconnect` timing on a stalled link (Will sent at the 1s cap without a deadline, no Will when the deadline outlasts the stall).
- The integration test proxy can record `SUBSCRIBE` packets per connection and stall the client-to-broker direction.
- Replaced fixed sleeps with explicit barriers or polling in unit and integration tests, and added assertions for backoff reset after a stable connection.
- Added unit tests for field length limits, initial subscription options, user property ordering, `NewCompletedToken`, `ProtocolVersion` and write timeouts.

---

## 📦 Installation

```bash
go get github.com/gonzalop/mq@v0.11.0
```
