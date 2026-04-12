# Release v0.9.6

This release strengthens protocol compliance, hardens security defaults, and introduces a new API for inspecting MQTT v5.0 reason codes on operation tokens.

## 🚀 New Features

### Token Reason Codes
- **`Token.ReasonCode()`**: New method on the `Token` interface that exposes the MQTT v5.0 reason code from server acknowledgments. This allows inspection of non-error status codes that were previously invisible to users:
    - `ReasonCodeNoMatchingSubscribers` (0x10) — message accepted, but no one is subscribed to the topic.
    - `ReasonCodeNoSubscriptionExisted` (0x11) — unsubscription succeeded, but the subscription didn't exist.
    - `ReasonCodeGrantedQoS1` / `ReasonCodeGrantedQoS2` (0x01/0x02) — subscription accepted at a QoS level that may differ from what was requested.
- Returns `ReasonCodeSuccess` (0x00) by default, for MQTT v3.1.1 connections, or when the server omits a reason code.
```go
token := client.Publish("sensors/temp", data, mq.WithQoS(1))
token.Wait(ctx)
if token.ReasonCode() == mq.ReasonCodeNoMatchingSubscribers {
    log.Println("Warning: no subscribers on this topic")
}
```

### Complete MQTT v5.0 Reason Code Constants
- Added all missing reason codes to the public API, including both non-error (< 0x80) and error (≥ 0x80) codes. Users can now match against any standard MQTT v5.0 reason code using `errors.Is` or direct comparison.

## 🔒 Security Hardening

### Tighter Resource Defaults
- **Reduced default limits**: `MaxIncomingPacket`, `MaxPayloadSize`, and `MaxTopicLength` now default to safer, more restrictive values to limit exposure to oversized or malicious packets.
- **`WithMaxHandlerConcurrency`**: New option to cap the number of simultaneously running message handler goroutines, preventing resource exhaustion from a burst of incoming messages.
- **`WithMaxAuthExchanges`**: New option to limit the number of AUTH packet round-trips during enhanced authentication, preventing infinite authentication loops.

### TLS Example Improvements
- The TLS example now uses secure defaults (`InsecureSkipVerify: false`) and requires an explicit `-insecure` flag to disable certificate verification.

## 📋 Protocol Compliance

### Stricter Packet Validation
- **Fixed header flag validation**: The packet reader now validates that fixed header flags match the MQTT specification for each packet type (e.g., SUBSCRIBE must have flags `0x02`).
- **Field constraint enforcement**: CONNECT, SUBSCRIBE, and UNSUBSCRIBE packets are now validated for required field constraints (e.g., non-empty topic lists, valid protocol names).
- **Duplicate property detection**: MQTT v5.0 property decoding now rejects packets with duplicate properties, as required by the specification.
- **Maximum Packet Size enforcement**: The client now validates outgoing SUBSCRIBE and UNSUBSCRIBE packets against the server's declared Maximum Packet Size before sending, failing fast with a clear error.
- **Protocol error disconnection**: On detecting a protocol error, the client now sends a DISCONNECT packet with reason code 0x82 (Protocol Error) before closing the connection, as recommended by the specification.

## ✅ Testing & Quality

- Added comprehensive compliance unit tests for fixed header validation, field constraints, and property decoding.
- Added integration tests for subscription options (NoLocal, RetainHandling) and Maximum Packet Size enforcement.
- Added 17 unit tests for `Token.ReasonCode()` covering all handler paths (PUBACK, PUBREC, PUBCOMP, SUBACK, UNSUBACK).

## 📦 Dependency Updates

- `go.opentelemetry.io/otel/sdk` to **v1.43.0** (integration tests)
- Various dependency updates across integration tests and examples.

## 📦 Installation

```bash
go get github.com/gonzalop/mq@v0.9.6
```
