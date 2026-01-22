# Local Routing Example

This example demonstrates how to implement client-side message routing using `mq.WithDefaultPublishHandler` and `mq.MatchTopic`.

This pattern allows you to decouple message handling logic from the network subscription calls. It effectively creates a local router that dispatches messages based on topic filters, even if they arrive via a single broad subscription (like `#`).

## Usage

```bash
go run main.go
```

## How it works

1.  **Define Routes**: We create a map of topic filters (e.g., `system/alerts/#`) to handler functions.
2.  **Central Router**: We implement a single function that iterates through these routes and uses `mq.MatchTopic(filter, topic)` to find matches.
3.  **Default Handler**: We register this router using `mq.WithDefaultPublishHandler(router)`. This handler catches any message that doesn't have a specific callback attached to its subscription.
4.  **Subscribe**: We subscribe to `#` (or any other topic) passing `nil` as the handler. This ensures messages fall through to our default handler/router.

## Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `MQTT_SERVER` | Server URL | `tcp://localhost:1883` |
| `MQTT_USERNAME` | Username for authentication | (empty) |
| `MQTT_PASSWORD` | Password for authentication | (empty) |
