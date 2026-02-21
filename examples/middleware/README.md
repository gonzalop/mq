# Middleware / Interceptor Example

This example demonstrates how to use the **Interceptor pattern** (also known as middleware) to add cross-cutting concerns to your MQTT client.

Interceptors allow you to wrap incoming message handlers or outgoing publish calls without modifying the business logic.

## Common Use Cases

1.  **Observability**: Automatic logging of every message with its size and topic.
2.  **Metrics**: Measuring message processing duration or counting published messages.
3.  **Tracing**: Injecting/extracting OpenTelemetry context (e.g., `trace-id`) via MQTT v5.0 User Properties.
4.  **Auditing**: Logging a record of every outbound message for compliance.
5.  **Payload Transformation**: Globally encrypting or decrypting message payloads.

## Key Concepts

-   **`HandlerInterceptor`**: Wraps incoming `MessageHandler` functions. It's applied to all subscriptions and the `DefaultPublishHandler`.
-   **`PublishInterceptor`**: Wraps the `Client.Publish` method. It's called whenever your code sends a message.

## Running the Example

Make sure you have an MQTT broker running on `localhost:1883`:

```bash
# Run the example
go run examples/middleware/main.go
```

## How it Works

In `main.go`, we define two interceptors:
-   A **Handler Interceptor** that logs the topic, size, and duration of every incoming message.
-   A **Publish Interceptor** that automatically injects a custom `x-trace-id` User Property into every outgoing message.

The client is then configured using:
```go
mq.WithHandlerInterceptor(loggingInterceptor),
mq.WithPublishInterceptor(tracingInterceptor),
```
