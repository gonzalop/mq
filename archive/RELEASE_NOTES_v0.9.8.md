# Release v0.9.8

This release introduces context-aware public APIs for Publish, Subscribe, and Unsubscribe, adds a comprehensive observability framework guide, introduces performance profiling for the topic trie, and updates Docker/Podman and Testcontainers dependencies alongside test and CI reliability improvements.

---

## ⚡ Context-Aware APIs (Breaking Change)

To enable cancellation, timeouts, and tracing propagation, public client operation signatures have been modernized.

- **Signature Updates**: The signatures of `Client.Publish`, `Client.Subscribe`, and `Client.Unsubscribe` now accept `context.Context` as the first parameter.
- **Cancellation & Cleanup**: Internal request queues and logic handlers now support `<-ctx.Done()`, allowing them to gracefully roll back and clean up pending state on context cancellation.
- **Integration Coverage**: All unit tests, integration tests, and example applications have been refactored to align with the new context-aware signatures.

---

## 📊 Observability & Performance Benchmarks

- **Observability Guide (`docs/observability.md`)**: A new guide providing detailed instructions on how to instrument structured logging, metrics collection, and distributed tracing via client interceptors without external heavy dependencies.
- **Trie Performance Profiling**: Added a performance benchmark suite (`internal/trie/trie_benchmark_test.go`) to measure `Insert`, `Match`, and `Remove` operations on the radix tree subscription router across 10, 100, and 1000 filter topologies.
- **Documentation Refactoring**: Updated all design guides, troubleshooting docs, and examples README files to reflect the context-aware API signatures.

---

## 🛡️ Testing, CI & Dependency Updates

- **Docker & Testcontainers Updates**: Upgraded `testcontainers-go` to `v0.43.0` and `github.com/docker/docker` to `v27.1.1+incompatible` to fix Go module layout conflicts.
- **Test Isolation**: Isolated `TestWildcardSubscriptions` with a unique topic namespace to eliminate race conditions and collisions when running concurrent tests on the shared broker.
- **CI / Build Automation**:
  - Replaced the default CodeQL autobuilder with a manual build workflow (`.github/workflows/codeql.yml`) that dynamically compiles all nested Go modules and examples (including those with the `ignore_test` build tag).
  - Expanded GitHub Actions Go testing matrix to support Go versions `1.24`, `1.25`, and `1.26`.
  - Bumped `codecov/codecov-action` from version 4 to 7.
- **Integration Test Reliability**: Resolved race conditions and flakiness in persistence and subscription properties integration tests.

---

## 🐛 Critical Concurrency & Safety Fixes

- **Memory Safety / Buffer Reuse**: Fixed potential correlation data and will message corruption by copying binary slice data during parsing (rather than retaining references to reused network packet buffers).
- **Radix Tree Match Thread Safety**: Resolved a data race by protecting `c.trie.Match` with `c.sessionLock` to ensure safe concurrent execution with dynamic subscribe/unsubscribe operations.
- **AsyncStore Queue Synchronization**: Introduced a queue-flushing mechanism (`flush`) to guarantee that synchronous `Load` and `Clear` methods in `AsyncStore` wait for pending background writes to finish, preventing filesystem races.
- **AsyncStore Resource Leak Prevention**: Standardized `AsyncStore.Close()` to implement `io.Closer` and updated `Client.Disconnect()` to automatically close the store and cleanly reclaim its background worker goroutines.

---

## 📦 Installation

```bash
go get github.com/gonzalop/mq@v0.9.8
```
