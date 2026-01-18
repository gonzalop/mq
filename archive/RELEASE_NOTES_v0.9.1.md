# Release v0.9.1

This release introduces context-aware dialing, expands MQTT v5.0 support for subscriptions, and refines the public API for persistence and error handling.

## 🚀 New Features

### Context Support
- **`DialContext`**: Added `DialContext`, enabling `context.Context` support for the initial connection handshake. This allows for precise control over connection timeouts and cancellation integration.

### MQTT v5.0 Compliance
- **Enhanced Unsubscribe**: `Unsubscribe` now accepts options, bringing it to parity with `Subscribe` and `Publish`.
- **User Properties**: Added full support for User Properties in both subscription (`WithSubscribeUserProperty`) and unsubscription (`WithUnsubscribeUserProperty`) packets.

### Utilities
- **`MatchTopic`**: The internal topic matching logic is now exported as `mq.MatchTopic`. This utility allows users to validate if a topic string matches a given wildcard filter according to MQTT rules.

## 🛠️ API Changes

### Persistence
- **Type Renaming**: Renamed persistence-related types to better reflect their purpose in `SessionStore` implementations:
  - `SubscriptionInfo` is now **`PersistedSubscription`**
  - `PublishPacket` (in the context of persistence) is now **`PersistedPublish`**

### Error Handling
- **Refactored Errors**: The error handling API has been refactored to provide clearer, more idiomatic error types.

## 📝 Documentation & Reliability
- **CI/CD**: Added a GitHub Actions workflow for automated testing.
- **Docs**: Fixed relative links and improved godoc comments.
