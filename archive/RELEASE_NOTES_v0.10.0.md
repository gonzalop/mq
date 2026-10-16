# Release v0.10.0

This release is a broad reliability pass over the client. It fixes shared subscription routing, outgoing topic alias ordering, in-flight resend behavior, protocol-error handling that left the client silently dead, enhanced authentication against strict brokers, and several shutdown and persistence edge cases in `Disconnect`, `FileStore` and `AsyncStore`. It also removes the unused `SessionStore.ClearPendingPublishes` method (a small breaking change) and makes acknowledgments O(1).

---

## ⚠️ Breaking Changes

- **`SessionStore.ClearPendingPublishes` Removed**: The method is removed from the `SessionStore` interface and from `FileStore` and `AsyncStore`. The client never called it, and its documentation ("called when SessionPresent=false") was wrong: pending publishes are kept on purpose so they are resent. Custom stores that still define the method keep compiling; only code that calls it breaks.

---

## 🔄 Behavior Changes

- **In-Flight Packets Resent Once per Reconnect**: QoS 1/2 packets are no longer resent on a 10-second timer. They are resent exactly once, in their original order, when a connection is established (including the first connect after loading a persisted session). This complies with [MQTT-4.4.0-1], which forbids resending outside a reconnect, and stops duplicate publishes to slow brokers as well as the offline queue build-up that made `Publish` block.
- **Outgoing Queue Applies Backpressure**: Because pending packets are no longer retried on a timer, the internal dispatch now blocks on a full outgoing channel (until the client stops) instead of dropping the packet.
- **Re-authentication Callbacks Run in Their Own Goroutine**: During re-authentication, `Authenticator.HandleChallenge` and `Complete` no longer run in the packet processing loop, so a slow authenticator no longer delays `PUBLISH`, `PUBACK` and other packets. Authenticators that share state with other code must be safe for concurrent use. During the initial connection, `HandleChallenge` still runs synchronously as part of the handshake and counts against `ConnectTimeout`.
- **Requests After `Disconnect` Fail Cleanly**: `Publish` (QoS > 0), `Subscribe` and `Unsubscribe` called after `Disconnect` now return `ErrClientDisconnected` instead of panicking with `assignment to entry in nil map`.

---

## 🐛 Bug Fixes

### Subscriptions & Routing

- **Shared Subscriptions Reach Their Handler**: Handlers for `$share/<group>/<filter>` subscriptions were stored under the full filter, but brokers deliver those messages on the plain topic, so the handler never ran (the message went to the default handler, or was acked and dropped). Handlers are now matched on the filter without the `$share/<group>/` prefix, while the full filter is still sent to the broker on resubscribe and unsubscribe.
- **Interceptors for Restored Subscriptions**: Subscriptions restored from the session store replaced the wrapped `WithSubscription` handler with the unwrapped one, so handler interceptors (logging, tracing, recovery) silently stopped running for those topics.
- **`$`-Prefixed Topics and Root Wildcards**: Topics starting with `$` no longer match `+` or `#` at the root level ([MQTT-4.7.2-1]).
- **Unsubscribe Consistency**: Local handlers and trie entries are kept until the `UNSUBSCRIBE` packet is enqueued, so a cancelled request no longer loses the handler.

### Topic Aliases

- **Aliases Assigned in the Write Loop**: Outgoing topic aliases were assigned when `Publish` was called, so a later packet could reach the wire before the one defining its alias (concurrent publishers on one topic, flow-controlled QoS 1 followed by QoS 0, or a dropped defining packet), and the broker closed the connection. Each connection's write loop now owns its alias table and applies aliases just before writing, so the first packet on the wire for a topic always defines the alias.
- **Invalid Incoming Alias Discarded**: A `PUBLISH` with an unknown topic alias was delivered to the default handler with an empty topic and acked. It is now discarded.

### Connection Lifecycle

- **Protocol Errors No Longer Kill the Client**: Server protocol errors (malformed v5.0 packet, bad topic alias, too many `AUTH` rounds, Receive Maximum violations) used the user-facing disconnect path, so the client never reconnected and `OnConnectionLost` never ran. They now send a `DISCONNECT` with the reason code (v5.0), drop only the connection, and go through the normal `OnConnectionLost` and reconnect path.
- **`Disconnect` Stops an In-Progress Reconnect**: Calling `Disconnect` during a reconnect dial or handshake that then succeeded could still start the loops, fire `OnConnect`, and leave the TCP connection open until the broker timed it out (likely publishing the will). Stopping now cancels the reconnect context, closes the connection mid-handshake, and prevents finalization.
- **Stale Connection Isolation**: Read and write loops are scoped to their own connection, so lingering loops from a dropped socket can no longer tear down a newly reconnected session.
- **Faster Graceful Disconnect**: The fixed 100 ms sleep in disconnect is replaced by a flush signal from the write loop.
- **Deadlock Prevention**: Resubscription and alias-reset packets are no longer dispatched while holding `sessionLock`, and `Authenticator.HandleChallenge`/`Complete` run in a separate goroutine so slow user code cannot stall packet processing.
- **`Reauthenticate` Honors Its Context**: `Reauthenticate` no longer hangs on a full outgoing queue; it returns when the context is cancelled or the client stops.

### Authentication

- **Authentication Method in Handshake `AUTH` Replies**: Multi-step enhanced authentication (e.g. SCRAM) replies omitted the Authentication Method property, which MQTT 5.0 treats as a Protocol Error, so it failed against strict brokers.

### Persistence

- **Atomic `FileStore` Writes**: Entries are now written to a temporary file and renamed into place, so a crash mid-write no longer leaves a partial JSON file that was silently skipped (losing the pending message). Data is not fsynced; the docs now note that the most recent writes may be lost on power loss or an OS crash.
- **Long Subscription Filters in `FileStore`**: Subscription files were named by the base64 of the topic filter, so filters longer than ~190 bytes exceeded the file name limit and were never saved or restored. Files are now named by a SHA-256 hash and store the topic inside. Existing stores keep working: legacy files are still read, and are cleaned up when the subscription is saved or deleted.
- **`FileStore` Clears Recreate Their Directory**: Clearing stored QoS 2 IDs removed the directory without recreating it, so every later save failed until restart.
- **Stale QoS 2 IDs Cleared on Any Store**: When the broker reports no session, the client now calls `ClearReceivedQoS2` instead of `DeleteReceivedQoS2(0)` (which only `FileStore` treated as "clear all"). Custom stores previously kept stale IDs, causing new messages that reused them to be acked and dropped as duplicates.
- **`AsyncStore.Close` Waits for Queued Writes**: `Close` returned immediately, so "Disconnect, then exit" lost every queued save and delete. It now drains the queue and returns the first error reported by the wrapped store; `Disconnect` logs it.
- **`Disconnect` Bounded by Its Context**: Closing the session store now runs under the `Disconnect` context deadline, so a slow or stuck backend no longer blocks past it (it returns `session store still flushing` on timeout).

### Races & Leaks

- **CONNACK User Properties**: Stored in an immutable connection-state snapshot, fixing a data race with `ConnectionUserProperties`.
- **Rejected `PUBREC` Leak**: A broker rejecting a QoS 2 publish now releases the in-flight slot and removes the state from the session store.
- **Publish Rollback**: Packet ID state is rolled back if publish preparation fails, preventing half-initialized requests.
- **Keep-Alive Race on Reconnect**: The keep-alive duration is passed by value to the write loop.

---

## ⚡ Performance

- **O(1) Acknowledgments**: Removing an acknowledged packet no longer scans the in-flight order list. Pending operations carry a creation sequence number and are sorted once per reconnect for resending.
- Updated [`docs/performance_analysis.md`](../docs/performance_analysis.md).

---

## 🧹 Refactoring & Cleanup

- Simplified `resubscribeAll` by filtering topics the server forbids before grouping.
- Removed dead helpers (`readLoop`/`writeLoop` test-only wrappers and an unreachable default-handler fallback).
- Simplified fixed-header varint encoding with `binary.PutUvarint`.
- Fixed staticcheck findings.

---

## 📚 Documentation & Examples

- Documented `WithMinStableConnectionDuration`, `ConnectedAt`/`Uptime` and anti-flapping backoff across the getting started, configuration, observability and troubleshooting guides.
- The `auto_reconnect` example shows the uptime accessors and stability threshold; `local_routing` only sets credentials when a username is provided ([MQTT-3.1.2-22]).
- Updated `docs/internals/CONCURRENCY.md` and `docs/persistence.md` for the new behavior.

---

## 🧪 Testing

- New integration tests against a real broker for shared subscription delivery (one delivery per group member, plain subscribers still receive all) and for topic aliases with 10 concurrent publishers mixing QoS 0 and 1.
- Stabilized overlapping-subscription, lifecycle-hook and protocol-error tests.
- Added unit tests for disconnect and reconnect-cancellation paths, handshake `AUTH` methods, write-loop topic aliases, resend ordering, shared subscription routing, restored-subscription interceptors, `FileStore` atomic writes and long filters, `$`-topic wildcard matching, and persistence performance.

---

## ⚙️ Dependencies

- Upgraded `github.com/moby/moby/api` to `1.56.1` in `/integration` (#25).

---

## 📦 Installation

```bash
go get github.com/gonzalop/mq@v0.10.0
```
