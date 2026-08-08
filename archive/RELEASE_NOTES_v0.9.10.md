# Release v0.9.10

This release introduces new MQTT v5.0 features for disconnect session expiry and Will delay intervals, adds configurable exponential backoff with full jitter for automatic reconnections, fixes a potential panic during rapid disconnect/reconnect cycles, and updates CI workflows.

---

## 🚀 New Features & Enhancements

- **MQTT v5.0 Disconnect Session Expiry**: Added `WithDisconnectSessionExpiry(interval uint32)` to set the `SessionExpiryInterval` on DISCONNECT packets, allowing clients to update or expire session state upon disconnection.
- **MQTT v5.0 Will Delay Interval**: Added `WithWillDelayInterval(seconds uint32)` to set the `WillDelayInterval` property on Last Will and Testament (LWT) messages.
- **Configurable Reconnect Backoff & Full Jitter**: Expanded `WithReconnectBackoff(initial, maximum time.Duration, jitter bool)` to support custom initial backoff, maximum backoff ceiling, and randomized Full Jitter (`rand.N`) to prevent thundering herd spikes during broker outages.

---

## 🐛 Bug Fixes & Stability

- **Fast Disconnect/Reconnect Nil Packet Handling**: Fixed a panic in `writeLoop` caused by `nil` packets sent to the `outgoing` channel during rapid disconnect and reconnect sequences.

---

## ⚙️ CI & Build Updates

- Updated `actions/setup-go` action to version 7 in GitHub Workflows.
- Updated Go testing matrix to test against Go 1.25 and 1.26.
- Removed unused CodeQL workflow.

---

## 📦 Installation

```bash
go get github.com/gonzalop/mq@v0.9.10
```
