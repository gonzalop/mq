# MQTT 5.0 Compliance Report

This document outlines the support for MQTT 5.0 features in the `mq` library, based on the [OASIS MQTT v5.0 Standard](https://docs.oasis-open.org/mqtt/mqtt/v5.0/os/mqtt-v5.0-os.html).

## 1. Connection & Session (CONNECT / CONNACK / DISCONNECT)

| Feature | Spec Ref | Status | Implementation Details |
| :--- | :--- | :--- | :--- |
| **Session Expiry** | 3.1.2.23 | ✅ Supported | `WithSessionExpiryInterval`. Persists state if > 0. Can be updated on Disconnect (v5). |
| **Clean Start** | 3.1.2.4 | ✅ Supported | Mapped to `WithCleanSession` (false = CleanStart=0). |
| **Reason Codes** | 3.2.2.2 | ✅ Supported | Full support for reason codes in all ACK packets and DISCONNECT. |
| **Server Redirection** | 3.2.2.3 | ✅ Supported | `WithOnServerRedirect` callback provides the reference URI. |
| **User Properties** | 3.1.2.28 | ✅ Supported | `WithConnectUserProperties` (send) and `ConnectionUserProperties()` (receive). |
| **Assigned Client ID** | 3.1.3.1 | ✅ Supported | Updates internal ID if server assigns one. |
| **Server Keep Alive** | 3.2.2.3 | ✅ Supported | Respects server's override of Keep Alive interval. |
| **Capabilities** | 3.2.2.3 | ✅ Supported | Extracts `ReceiveMaximum`, `MaximumPacketSize`, etc. |
| **Enhanced Auth** | 4.12 | ✅ Supported | `WithAuthenticator` enables AUTH packet exchange. |

## 2. Enhanced Publishing (PUBLISH)

| Feature | Spec Ref | Status | Implementation Details |
| :--- | :--- | :--- | :--- |
| **Message Expiry** | 3.3.2.3.3 | ✅ Supported | `WithMessageExpiry`. |
| **Topic Aliases** | 3.3.2.3.4 | ✅ Supported | Send & Receive. Auto-negotiates limits via `TopicAliasMaximum`. |
| **Payload Format** | 3.3.2.3.2 | ✅ Supported | `WithPayloadFormat`. |
| **Content Type** | 3.3.2.3.5 | ✅ Supported | `WithContentType`. |
| **Response Topic** | 3.3.2.3.6 | ✅ Supported | `WithResponseTopic` / `WithCorrelationData`. |
| **User Properties** | 3.3.2.3.7 | ✅ Supported | `WithUserProperty`. |
| **Flow Control** | 4.9 | ✅ Supported | Respects `ReceiveMaximum` from server capabilities. |

## 3. Enhanced Subscription (SUBSCRIBE)

| Feature | Spec Ref | Status | Implementation Details |
| :--- | :--- | :--- | :--- |
| **No Local** | 3.8.3.1 | ✅ Supported | `WithNoLocal`. |
| **Retain As Published** | 3.8.3.1 | ✅ Supported | `WithRetainAsPublished`. |
| **Retain Handling** | 3.8.3.1 | ✅ Supported | `WithRetainHandling`. |
| **Shared Subscriptions** | 4.8.2 | ✅ Supported | Via string format (e.g., `$share/group/topic`). |
| **Subscription ID** | 3.8.2.1 | ✅ Supported | `WithSubscriptionIdentifier` option and `Message.Properties` access. |

## 4. Error Handling

| Feature | Spec Ref | Status | Implementation Details |
| :--- | :--- | :--- | :--- |
| **Reason Strings** | 3.14.2.2 | ✅ Supported | Extracted from properties and added to error messages. |
| **Request Problem Info**| 3.1.2.25 | ✅ Supported | `WithRequestProblemInformation`. |

