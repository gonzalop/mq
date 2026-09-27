# Release v0.9.11

This release addresses critical edge cases in client disconnection and reconnection lifecycles, ensures initial subscriptions are reliably dispatched on first connect for clean sessions, resolves a keepalive concurrency race, enforces MQTT specification compliance for credential configuration, modernizes internal implementations with Go standard library additions, and updates the toolchain to Go 1.26.

---

## 🐛 Bug Fixes & Concurrency Improvements

- **Graceful Client Stop During Reconnection**: Fixed an issue where invoking `Disconnect()` while the client was disconnected or actively attempting an automatic reconnect would return early without stopping background client loops. The shutdown process now unconditionally closes the stop channel guarded by a `sync.Once` (ensuring idempotency) and unblocks pending completion tokens, preventing the client from reconnecting indefinitely in the background after disconnection.
- **Initial Subscriptions on First Connect**: Fixed an issue where topic subscriptions registered upfront via `WithSubscription()` with `CleanSession = true` (the default) were only dispatched during reconnection cycles and not on the initial connection. Subscriptions are now dispatched immediately upon connection finalization.
- **Clean Session State Reset Race Prevention**: Reordered clean session initialization in `finalizeConnection` to execute before starting background read and write loops, eliminating race conditions where inbound packets or keepalive pings on newly established connections could be inadvertently wiped.
- **Atomic Keepalive Ping State**: Converted `c.pingPending` from a plain boolean to `atomic.Bool`. This resolves a data race between the background `writeLoop` setting the flag and `handleDisconnect` / `internalResetState` clearing it, preventing stale reads from skipping or duplicating keepalive probes.
- **Credential Validation Compliance ([MQTT-3.1.2-22])**: Enforced the MQTT specification requirement that the password flag must be 0 when the username flag is 0. Configuring a password without a username (`WithCredentials("", "password")`) now fails early during connection validation with a clear error (`a password requires a username ([MQTT-3.1.2-22])`) rather than sending a malformed `CONNECT` packet rejected by the broker.

---

## 🧹 Code Modernization & Refactoring

- **Go Standard Library Modernization**: Replaced manual slice and map allocations and copies across packet serialization, deserialization, and client negotiation with standard library `maps.Clone` and `bytes.Clone`. Replaced manual loop-and-slice filtering in `removePending` with `slices.DeleteFunc`.
- **Source Consolidation**: Consolidated fragmented single-option and subscription files (`options_auth.go`, `options_tls.go`, `client_subscriptions.go`) into unified `options.go` and `subscribe.go` source files to improve project maintainability and navigation.
- **Make Target for Code Modernization**: Added a `fix` target to the Makefile to automate code modernizations across the repository using `go fix ./...`.

---

## ⚙️ CI, Toolchain & Dependencies

- **Go 1.26 Toolchain**: Updated `go.mod` across all root, integration, and example modules to Go 1.26, and updated documentation prerequisites accordingly.
- **CI Test Matrix**: Updated GitHub Actions CI workflow to test against Go 1.26 and Go 1.27.
- **Integration Test Suite Hardening**:
  - Stabilized reconnection and compliance integration tests by using shorter reconnect backoffs and polling `client.IsConnected()` with deadlines instead of arbitrary sleeps.
  - Added end-to-end integration tests verifying initial subscription delivery on clean session connections against a containerized Mosquitto broker (MQTT v3.1.1 and v5.0).
- **Dependency Upgrades**:
  - Upgraded `github.com/testcontainers/testcontainers-go` to `v0.44.0` in `/integration`.
  - Upgraded `github.com/moby/moby/api` to `1.56.0` in `/integration`.
  - Upgraded `github.com/moby/go-archive` to `0.3.0` in `/integration`.

---

## 📦 Installation

```bash
go get github.com/gonzalop/mq@v0.9.11
```
