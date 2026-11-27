# Release v0.13.0

This release fixes inbound message delivery ordering so that messages on a given topic are delivered to handlers and acknowledged in arrival order (MQTT 4.6), while keeping different topics concurrent and internal client loops non-blocking.

---

## 🐛 Bug Fixes

- **Per-Topic In-Order Message Delivery & Acknowledgment (MQTT 4.6)**: Previously, each inbound message was dispatched in its own goroutine, allowing handlers on the same topic to observe messages out of order and QoS 1/2 acknowledgments to be sent in handler completion order. Deliveries now go through a per-topic FIFO queue with a single worker goroutine that exists only while the topic has pending work, ensuring handlers and their `PUBACK`/`PUBREC` packets follow arrival order. Overlapping subscriptions on the same topic still execute in parallel for a given message, different topics continue to run concurrently, and `logicLoop` never blocks on handlers.

---

## 📚 Documentation & Maintenance

- Updated [`docs/mqtt_3.1.1_compliance.md`](../docs/mqtt_3.1.1_compliance.md), [`docs/internals/CONCURRENCY.md`](../docs/internals/CONCURRENCY.md), and [`docs/client_configuration_best_practices.md`](../docs/client_configuration_best_practices.md) to document per-topic ordered delivery.
- Added unit tests in `ordering_test.go` covering per-topic delivery order, QoS 1 acknowledgment order, and cross-topic concurrency.
- Updated integration test module dependencies (`integration/go.mod`, `integration/go.sum`).

---

## 📦 Installation

```bash
go get github.com/gonzalop/mq@v0.13.0
```
