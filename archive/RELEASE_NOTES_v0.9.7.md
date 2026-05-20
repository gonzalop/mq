# Release v0.9.7

This release introduces major architectural modernizations, high-performance radix tree subscription routing, non-blocking asynchronous session persistence, and key reliability fixes for recovered connections.

---

## 🚀 Architectural Modernization

### Radix Tree Subscription Routing
- Migrated subscription routing from linear list matching to a high-performance custom radix tree (`internal/trie`). This significantly reduces message routing overhead and CPU consumption, especially for clients managing large subscription tables.

### Modular Code Restructuring
- Restructured and cleaned up the core client code, splitting large files (`client.go`, `options.go`) and standardizing file layout conventions (renaming to `snake_case` filenames).
- Isolated the logic queue into dedicated packages for cleaner execution flow.

### High-Performance Table-Driven Properties
- Rewrote the MQTT v5.0 property encoding and decoding pipelines to use high-performance table-driven lookups, reducing allocation overhead and speed during packet serialization.

---

## 💾 Durable Session Persistence

This release introduces robust persistence capabilities, enabling MQTT session states (pending QoS 1/2 publishes, subscriptions, and QoS 2 packet history) to survive client process crashes and restarts.

### Incremental FileStore
- **`FileStore`**: A directory-backed, incremental session persistence store. Unlike monolithic stores that rewrite the entire state on every update ($O(N)$), `FileStore` updates state incrementally with small, individual files per packet ($O(1)$).

### AsyncStore Wrapper (Non-Blocking)
- **`AsyncStore`**: A wrapper that performs disk-bound writes and deletes on a background goroutine.
- **Unbounded Queue Refactor**: Refactored `AsyncStore` to use a thread-safe, mutex-backed unbounded queue (`sync.Cond` and slice) instead of a buffered channel, guaranteeing that writing to disk will never block or stall the single-threaded client logic loop.

### Persistence Bug Fixes
- **Cronological Retransmission Order**: Fixed a bug where `pendingOrder` was not populated during session recovery, which broke QoS 1/2 retransmissions for restored clients.
- **Trie Subscription Overwrite**: Fixed a handler duplicate execution issue where subscribing to the same topic filter multiple times appended duplicate entries to the Radix tree.
- **Stateless Persistence Cleanup**: Automatically clears persistent session files on startup if the client dials with `CleanSession=true` to prevent disk storage leaks.

---

## 🛡️ Reliability & Safety

- Patched client memory leak vectors occurring during reconnect and session cycles.
- Guaranteed thread-safe, zero-block behavior when enqueueing control packets and persistence events.

---

## 🧪 Testing & Build Automation

- **Fuzz Testing**: Implemented extensive go-fuzz harnesses covering incoming message handlers, packet parsers, topic validators, and matchers (`FuzzClientHandleIncoming`, `FuzzPacketSequence`, etc.).
- **Build Automation**: Automated fuzz test discovery and execution inside the `Makefile` (`make fuzz`).
- Consolidated integration and unit tests, removing obsolete test configurations.

---

## 📦 Installation

```bash
go get github.com/gonzalop/mq@v0.9.7
```
