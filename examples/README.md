# Examples

This directory contains examples demonstrating various features of the `mq` library.

## Basic Usage
- **[Simple](./simple)**: A basic example of connecting, subscribing, and publishing.
- **[Wildcards](./wildcards)**: Demonstrates the use of MQTT wildcards (`+` and `#`) in subscriptions.
- **[TLS](./tls)**: Connecting to an MQTT broker using TLS/SSL encryption.
- **[Websocket](./websocket)**: Connecting via WebSockets using a custom dialer.

## Reliability and Persistence
- **[Auto Reconnect](./auto_reconnect)**: Demonstrates the built-in automatic reconnection with exponential backoff.
- **[Persistent Session (Protocol)](./persistent)**: Demonstrates MQTT-level persistent sessions (`CleanSession=false`) where the broker queues messages while the client is offline.
- **[Durable Persistence (FileStore)](./persistence_filestore)**: Demonstrates using a local `SessionStore` (`FileStore`) to persist state on disk, allowing the client to survive process restarts without losing track of subscriptions or pending messages.
- **[Last Will and Testament (LWT)](./lwt)**: Configuring a "will" message to be sent by the broker if the client disconnects unexpectedly.

## MQTT v5.0 Features
- **[User Properties](./v5_properties)**: Using MQTT v5.0 User Properties for custom metadata.
- **[Request/Response](./v5_request_response)**: Implementing the Request/Response pattern using `ResponseTopic` and `CorrelationData`.
- **[Topic Aliases](./topic_aliases)**: Reducing bandwidth by using numeric aliases for long topic names.

## Advanced Patterns
- **[Middleware/Interceptors](./middleware)**: Using interceptors for cross-cutting concerns like logging, metrics, or auth.
- **[SCRAM Auth](./scram_auth)**: Implementing custom SASL-SCRAM authentication for MQTT v5.0.
- **[Error Handling](./errors)**: Demonstrates how to handle MQTT v5.0 Reason Codes and library errors.
- **[Local Routing](./local_routing)**: Advanced routing patterns.

## Benchmarking
- **[Throughput](./throughput)**: A tool for measuring message throughput and comparing performance with other libraries.
