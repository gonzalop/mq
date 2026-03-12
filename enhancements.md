# MQTT Compliance Enhancements

This document outlines the updates made to the `mq` library to ensure strict compliance with MQTT 3.1.1 and MQTT 5.0 specifications.

## 1. Strict Packet Validation
- **Fixed Header Hardening**: Added a centralized `validateFlags` check that rejects malformed headers.
    - Rejects `PUBLISH` packets with invalid QoS 3.
    - Rejects `PUBLISH` QoS 0 packets with the `DUP` flag set (MQTT 3.1.1, section 3.3.1.2).
    - Enforces zero flags for `CONNECT`, `CONNACK`, `PUBACK`, `PUBREC`, `PUBCOMP`, `SUBACK`, `UNSUBACK`, `PINGREQ`, `PINGRESP`, `DISCONNECT`, and `AUTH`.
    - Enforces exact flag bits (bit 1 set) for `PUBREL`, `SUBSCRIBE`, and `UNSUBSCRIBE`.
- **Field Constraints**:
    - **CONNECT**: Validates that the reserved bit is 0 and ensures `WillQoS` and `WillRetain` are zero if the `WillFlag` is not set.
    - **SUBSCRIBE/UNSUBSCRIBE**: Now strictly enforces that these packets must contain at least one topic filter.
    - **CONNACK**: Validates that bits 7-1 of acknowledge flags are reserved and must be 0.

## 2. MQTT 5.0 Property Hardening
- **Duplicate Property Detection**: Implemented strict validation in `decodeProperties`. Per MQTT v5.0 spec, most properties must appear at most once. User Properties and Subscription Identifiers remain repeatable.
- **Improved Internal Tracking**: Added `PresCorrelationData` and `PresAuthenticationData` to the internal bitmask to allow precise duplicate detection and conversion consistency between public and internal formats.

## 3. Advanced Error Handling
- **Structured Errors**: Introduced `ProtocolError` and `MalformedPacketError` types.
- **V5.0 Disconnect Flow**: Updated the client `readLoop` to handle protocol violations gracefully. For MQTT v5.0 connections, the client now sends a `DISCONNECT` with reason code `0x82` (Protocol Error) before closing the socket, improving server-side diagnostics.

## 4. Resource Safety & Fail-Fast
- **Client-Side Limits**: Expanded `MaximumPacketSize` enforcement. The client now checks the size of `SUBSCRIBE` and `UNSUBSCRIBE` packets against the server's advertised limit (received in `CONNACK`) before transmission, mirroring the behavior for `PUBLISH`.

## 5. Verification & Testing
- **New Unit Tests**: `internal/packets/compliance_unit_test.go` adds 30+ tests for malformed flags, duplicate properties, and protocol edge cases.
- **Expanded Integration Tests**: `integration/compliance_extra_test.go` and updates to `integration/subscription_options_test.go` verify:
    - **Overlapping Subscriptions**: Multiple matching subscriptions result in all handlers being called once.
    - **QoS Downgrade**: Client correctly handles QoS levels downgraded by the server.
    - **Subscription Identifiers**: Correct delivery of MQTT v5.0 subscription IDs.
    - **Retain As Published**: Verification that the `RETAIN` flag is preserved when requested.
    - **Retain Handling**: Verification of server-side retained message delivery filtering.
    - **Fail-Fast Limits**: Verified that `SUBSCRIBE` requests exceeding server limits are rejected client-side.
- **Mock Protocol Testing**: Updated `compliance_test.go` with mock server scenarios to verify the client's response to protocol violations.
