# MQTT 3.1.1 Compliance Report

This document outlines the support for MQTT 3.1.1 features in the `mq` library, based on the [OASIS MQTT v3.1.1 Standard](http://docs.oasis-open.org/mqtt/mqtt/v3.1.1/os/mqtt-v3.1.1-os.html).

## 1. Connection Management (CONNECT / CONNACK)

| Feature | Spec Ref | Status | Implementation Details |
| :--- | :--- | :--- | :--- |
| **TCP/IP Connection** | 4.2 | ✅ Supported | Uses `net.Dial` or custom `Dialer`. |
| **TLS/SSL Connection** | 4.2 | ✅ Supported | Uses `crypto/tls`. |
| **Clean Session** | 3.1.2.4 | ✅ Supported | `WithCleanSession(true/false)`. Handles session state persistence. |
| **Keep Alive** | 3.1.2.10 | ✅ Supported | `WithKeepAlive()`. Implements PINGREQ/PINGRESP loop. |
| **Will Message** | 3.1.2.5 | ✅ Supported | `WithWill()`. Supports Topic, Message, QoS, Retain. |
| **Username/Password** | 3.1.2.8 | ✅ Supported | `WithCredentials()`. |
| **Client ID** | 3.1.3.1 | ✅ Supported | `WithClientID()`. Validation ensures non-empty ID if CleanSession=false. |

## 2. Message Publishing (PUBLISH / PUBACK / PUBREC / PUBREL / PUBCOMP)

| Feature | Spec Ref | Status | Implementation Details |
| :--- | :--- | :--- | :--- |
| **QoS 0 (At most once)** | 4.3.1 | ✅ Supported | Fire-and-forget implementation. |
| **QoS 1 (At least once)** | 4.3.2 | ✅ Supported | Retries with DUP flag until PUBACK received. |
| **QoS 2 (Exactly once)** | 4.3.3 | ✅ Supported | Full 4-step handshake (PUBLISH -> PUBREC -> PUBREL -> PUBCOMP). |
| **Retain Flag** | 3.3.1.3 | ✅ Supported | `WithRetain()`. |
| **Duplicate Flag (DUP)** | 3.3.1.2 | ✅ Supported | Sets DUP=1 on retries. |
| **Packet Ordering** | 4.6 | ✅ Supported | `logicLoop` processes packets sequentially. |

## 3. Subscription (SUBSCRIBE / SUBACK / UNSUBSCRIBE / UNSUBACK)

| Feature | Spec Ref | Status | Implementation Details |
| :--- | :--- | :--- | :--- |
| **Topic Filters** | 4.7 | ✅ Supported | Validates `+` and `#` wildcards correctly. |
| **Multiple Topics** | 3.8.3 | ✅ Supported | `Subscribe` sends single topic, `resubscribeAll` batches topics. |
| **Return Codes** | 3.9.3 | ✅ Supported | Checks SUBACK return codes for success/failure. |
| **Unsubscribe** | 3.10 | ✅ Supported | `Unsubscribe` removes handler and sends packet. |

## 4. Operational Behavior

| Feature | Spec Ref | Status | Implementation Details |
| :--- | :--- | :--- | :--- |
| **Payload Size** | - | ✅ Supported | Respects `WithMaxIncomingPacket` and spec limit (256MB). |
| **Topic Matching** | 4.7 | ✅ Supported | Efficient string-based matching (no regex). |
| **Session Persistence** | 4.1 | ✅ Supported | `WithSessionStore` allows persistent storage of session state. |

