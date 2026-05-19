# Persistence with FileStore

This example demonstrates how to use the built-in `FileStore` and `AsyncStore` to provide durable client-side persistence that survives application restarts.

## How it works

1.  **`FileStore`**: Stores pending QoS 1/2 messages and subscription filters on the local filesystem.
2.  **`AsyncStore`**: Wraps the `FileStore` to ensure that disk I/O (which can be slow) is handled in a background goroutine, preventing it from slowing down the MQTT message processing.
3.  **`WithSubscription`**: Pre-registers handlers so that when the client restarts and loads subscriptions from the disk store, it knows which function to call for incoming messages.

## Running the example

1.  Start an MQTT broker (e.g., Mosquitto) on `localhost:1883`.
2.  Run the example:
    ```bash
    go run main.go
    ```
3.  Publish a QoS 1 message to `sensors/data` while the client is running.
4.  Stop the client (`Ctrl+C`).
5.  Publish another QoS 1 message while the client is offline.
6.  Restart the client. It will automatically reconnect, resume its session, and receive the offline message.
