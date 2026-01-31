# MQTT Client Performance Analysis: `mq` Library

This document presents a comprehensive performance analysis of the `mq` MQTT client library compared to Eclipse Paho v3 and v5 implementations. The analysis includes recent tests conducted against **Mosquitto v2.1.0**, as well as data from Mosquitto 2.1rc2, 2.0.22, Mochi, and VerneMQ.

All benchmarks were performed using the [throughput test](../examples/throughput/) example, which measures end-to-end performance by subscribing to a topic and then publishing messages of a specified payload size concurrently using a configurable number of workers.

**Test Environment**: Linux.

## Executive Summary

The `mq` library demonstrates **exceptional performance across all tested scenarios**, consistently outperforming both Paho v3 and v5 in critical metrics. In the latest tests against **Mosquitto v2.1.0**, `mq` achieved peak publish rates exceeding **1.3 million messages per second** and sustained end-to-end delivery rates of over **710,000 messages per second**.

- **🏆 Throughput**: Up to **3x faster** than Paho v5 and **4x faster** than Paho v3 in high-concurrency scenarios.
- **✅ Reliability**: **100% message delivery** in all tests, while Paho clients frequently dropped messages or stalled under high load.
- **💾 Memory Efficiency**: **10x lower memory allocation** compared to Paho v5 (108 MiB vs 1,137 MiB in 50-worker small-packet workloads).
- **🔄 GC Overhead**: Significantly fewer garbage collection cycles (e.g., 56 vs 723 for Paho v5), resulting in more predictable latencies and smoother performance.

## Performance Highlights

### QoS 0 (Fire & Forget)

The `mq` library dominates in raw throughput scenarios. The table below reflects the maximum performance observed in the **Mosquitto v2.1.0** tests (20 workers, 20B payload):

| Metric | mq | Paho v3 | Paho v5 |
|--------|----|---------|---------|
| **Max Publish Rate** | **1,365,680 msg/s** | 175,555 msg/s | 201,842 msg/s |
| **Max E2E Rate** | **710,948 msg/s** | 168,934 msg/s | 199,997 msg/s |
| **Message Delivery** | ✅ 100% | ✅ 100% | ⚠️ Loss/Timeout |

**Key Advantage**: `mq` scales effectively with concurrency, reaching peak rates at moderate worker counts (4-20) where other clients begin to saturate or fail.

### QoS 1 (At Least Once)

In scenarios requiring acknowledgments, `mq` maintains its performance edge (data from Mosquitto v2.1.0, 50 workers):

| Configuration | mq | Paho v3 | Paho v5 |
|---------------|----|---------|---------|
| **20B Payload** | **150,162 msg/s** | 83,572 msg/s | 61,526 msg/s |
| **1KB Payload** | **127,312 msg/s** | 77,585 msg/s | 58,327 msg/s |

**Key Advantage**: `mq` handles the acknowledgment flow roughly **2x faster** than Paho v5 at high concurrency.

### QoS 2 (Exactly Once)

Even with the complex 4-way handshake, `mq` outperforms competitors (data from Mosquitto v2.1.0, 20 workers):

| Configuration | mq | Paho v3 | Paho v5 |
|---------------|----|---------|---------|
| **20B Payload** | **95,840 msg/s** | 42,401 msg/s | 36,922 msg/s |
| **1KB Payload** | **63,682 msg/s** | 41,737 msg/s | 29,774 msg/s |

## Resource Efficiency

Memory management is a standout feature of `mq`. In the latest tests (Mosquitto v2.1.0, 50 workers, 20B payload, 200k messages, QoS 0):

```
mq:       108 MiB TotalAlloc (56 GC cycles)
Paho v3:  282 MiB TotalAlloc (98 GC cycles)   [2.6x more than mq]
Paho v5: 1,137 MiB TotalAlloc (723 GC cycles) [10.5x more than mq]
```

**Impact**:
- **Paho v5** exerts massive pressure on the Garbage Collector (723 cycles vs 56 for `mq`).
- **mq** maintains a lean footprint, ideal for high-throughput or resource-constrained environments.

## Reliability & Stability

### Message Delivery Under Load

`mq` is the **only client** to consistently achieve stability across all test scenarios:

- **Mosquitto v2.1.0 Tests**:
    - **Paho v5**: Frequently entered "Subscriber idle" state (likely dropped messages or deadlock) with large payloads (10KB) or high concurrency (50 workers).
    - **Paho v3**: Generally stable but significantly slower.
    - **mq**: Zero stability issues; 100% delivery rate.

## Server Performance Notes: Mosquitto v2.1.0

The latest release of Mosquitto proved to be highly performant, enabling `mq` to reach unprecedented message rates. Key observations from the server perspective:

- **Peak Ingress**: ~1.36 Million msgs/sec (QoS 0, 20B, 20 workers).
- **Peak End-to-End**: ~710,000 msgs/sec (observed saturation point for routing and egress).
- **Scaling**: Optimal performance is reached between 4 and 20 workers; exceeding 20 active publishers introduces context-switching overhead.
- **QoS Overhead**: Transitioning from QoS 0 to QoS 1/2 reduces broker capacity by 74-86% due to handshaking complexity.

## Detailed Test Reports

- **[Mosquitto 2.1.0 Server Tests](https://gist.github.com/gonzalop/bcfea63b0a74745c89543824113b25c6)**
- **[Mosquitto 2.1rc2 Server Tests](https://gist.github.com/gonzalop/904177a8b4ee9156fe87fcb503b222fa)**
- **[Mosquitto 2.0.22 Server Tests](https://gist.github.com/gonzalop/a338dd8c1c1a85d5e9f42d5fd7644f11)**
- **[Mochi Server Tests](https://gist.github.com/gonzalop/18e2d2ab28c94d32866f1a0d1668c523)**
- **[VerneMQ Server Tests](https://gist.github.com/gonzalop/e8e8addef4464354f674d7bab98896cf)**

## Recommendations

**Use `mq` when you need**:
- **Extreme Throughput**: Proven to handle >1.3 million msg/s (publish) and >700k msg/s (E2E).
- **Reliability**: Consistently delivers messages without timeouts under load.
- **Efficiency**: Drastically lower memory and CPU footprint.

**Use Paho v3 when**:
- You require a legacy implementation for compatibility reasons.

**Avoid Paho v5**:
- Demonstrated significant instability and resource inefficiency in high-performance benchmarks.