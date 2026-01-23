# MQTT Client Performance Analysis: `mq` Library

This document presents a comprehensive performance analysis of the `mq` MQTT client library compared to Eclipse Paho v3 and v5 implementations. The analysis includes recent tests conducted against **Mosquitto v2.1rc2**, as well as previous data from Mosquitto 2.0.22, Mochi, and VerneMQ.

All benchmarks were performed using the [throughput test](../examples/throughput/) example, which measures end-to-end performance by subscribing to a topic and then publishing messages of a specified payload size concurrently using a configurable number of workers.

**Test Environment**: Linux.

## Executive Summary

The `mq` library demonstrates **exceptional performance across all tested scenarios**, consistently outperforming both Paho v3 and v5 in critical metrics. In the latest tests against **Mosquitto v2.1rc2**, `mq` achieved peak message rates exceeding **1.2 million messages per second**.

- **🏆 Throughput**: Up to **3x faster** than Paho v5 and **4x faster** than Paho v3 in high-concurrency scenarios (Mosquitto v2.1rc2).
- **✅ Reliability**: **100% message delivery** in all QoS 0 tests, while Paho clients frequently dropped messages or timed out under high load.
- **💾 Memory Efficiency**: **10x lower memory allocation** compared to Paho v5 (109 MiB vs 1,137 MiB in 50-worker small-packet workloads).
- **🔄 GC Overhead**: Significantly fewer garbage collection cycles (e.g., 53 vs 703 for Paho v5), resulting in more predictable latencies.

## Performance Highlights

### QoS 0 (Fire & Forget)

The `mq` library dominates in raw throughput scenarios. The table below reflects the maximum performance observed in the latest Mosquitto v2.1rc2 tests:

| Metric | mq | Paho v3 | Paho v5 |
|--------|----|---------|---------|
| **Max Message Rate** | **1,291,438 msg/s** | 314,750 msg/s | 412,978 msg/s |
| **Max Throughput** | **839 MB/s** | 713 MB/s | ~232 MB/s (unstable) |
| **Message Delivery** | ✅ 100% | ✅ 100% | ⚠️ Loss/Timeout |

**Key Advantage**: `mq` scales effectively with concurrency, reaching peak rates at moderate worker counts (4-20) where other clients begin to saturate or fail.

### QoS 1 (At Least Once)

In scenarios requiring acknowledgments, `mq` maintains its performance edge (data from Mosquitto v2.1rc2):

| Workers | mq | Paho v3 | Paho v5 |
|---------|----|---------|---------|
| **4 workers (20B)** | 118,077 msg/s | 82,812 msg/s | 64,331 msg/s |
| **50 workers (20B)** | **129,816 msg/s** | 84,120 msg/s | 56,626 msg/s |
| **50 workers (1KB)** | **140,752 msg/s** | 77,245 msg/s | 60,093 msg/s |

**Key Advantage**: `mq` handles the acknowledgment flow roughly **2x faster** than Paho v5 at high concurrency.

### QoS 2 (Exactly Once)

Even with the complex 4-way handshake, `mq` outperforms competitors:

| Configuration | mq | Paho v3 | Paho v5 |
|---------------|----|---------|---------|
| **50 workers, 20B** | **61,110 msg/s** | 42,606 msg/s | 29,403 msg/s |
| **50 workers, 1KB** | **58,261 msg/s** | 40,894 msg/s | 32,825 msg/s |

## Resource Efficiency

Memory management is a standout feature of `mq`. In the latest tests (Mosquitto v2.1rc2, 50 workers, 20B payload, 200k messages, QoS 0):

```
mq:       109 MiB TotalAlloc (53 GC cycles)
Paho v3:  282 MiB TotalAlloc (97 GC cycles)  [2.6x more than mq]
Paho v5: 1,137 MiB TotalAlloc (703 GC cycles) [10.4x more than mq]
```

**Impact**:
- **Paho v5** exerts massive pressure on the Garbage Collector (703 cycles vs 53 for `mq`).
- **mq** maintains a lean footprint, ideal for high-throughput or resource-constrained environments.

## Reliability & Stability

### Message Delivery Under Load

`mq` is the **only client** to consistently achieve stability across all test scenarios in the new suite:

- **Mosquitto v2.1rc2 Tests**:
    - **Paho v5**: Frequently entered "Subscriber idle" state (likely dropped messages or deadlock) with large payloads (10KB) or high concurrency (50 workers).
    - **Paho v3**: Generally stable but slower.
    - **mq**: Zero stability issues; recovered quickly even in edge cases.

## Server Performance Notes

### Mosquitto v2.1rc2 (New)
The latest release candidate of Mosquitto proved to be highly performant, enabling `mq` to reach unprecedented message rates (1.29M msg/s). It handled high connection churn and throughput well, though Paho v5 struggled to maintain stability against it under maximum load.

### Mosquitto 2.0.22
Demonstrated solid performance as a general-purpose broker.

### Mochi & VerneMQ
Mochi showed excellent raw throughput for small packets. VerneMQ benefited significantly from host networking configurations.

## Detailed Test Reports

- **[Mosquitto 2.1rc2 Server Tests](https://gist.github.com/gonzalop/904177a8b4ee9156fe87fcb503b222fa)**
- **[Mosquitto 2.0.22 Server Tests](https://gist.github.com/gonzalop/a338dd8c1c1a85d5e9f42d5fd7644f11)**
- **[Mochi Server Tests](https://gist.github.com/gonzalop/18e2d2ab28c94d32866f1a0d1668c523)**
- **[VerneMQ Server Tests](https://gist.github.com/gonzalop/e8e8addef4464354f674d7bab98896cf)**

## Recommendations

**Use `mq` when you need**:
- **Extreme Throughput**: Proven to handle >1 million msg/s.
- **Reliability**: Consistently delivers messages without timeouts under load.
- **Efficiency**: Drastically lower memory and CPU footprint.

**Use Paho v3 when**:
- You require a legacy implementation for compatibility reasons.

**Avoid Paho v5**:
- Demonstrated significant instability and resource inefficiency in high-performance benchmarks.
