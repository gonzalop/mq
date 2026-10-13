# MQTT Client Performance Analysis: `mq` Library

This document presents a comprehensive performance analysis of the `mq` MQTT client library compared to Eclipse Paho v3 and v5 implementations. The analysis includes recent tests conducted against **Mosquitto v2.1.2**, as well as prior data from Mosquitto 2.1.0, 2.1rc2, 2.0.22, Mochi, and VerneMQ.

All benchmarks were performed using the [throughput test](../examples/throughput/) example, which measures end-to-end performance by subscribing to a topic and then publishing messages of a specified payload size concurrently using a configurable number of workers.

**Test Environment**: Linux.

## Executive Summary

The `mq` library demonstrates **exceptional performance across all tested scenarios**, consistently outperforming both Paho v3 and v5 in critical metrics. In the latest tests against **Mosquitto v2.1.2**, `mq` achieved peak publish rates exceeding **1.01 million messages per second** and sustained end-to-end delivery rates of over **536,000 messages per second**.

- **🏆 Throughput**: Up to **3.7x faster** than Paho v3 and **2.5x faster** than Paho v5 in high-concurrency acknowledged scenarios.
- **✅ Reliability**: **100% message delivery** across all QoS 1 and QoS 2 tests, while Paho v5 failed in 35% of all benchmarks (17 out of 48 runs) and Paho v3 dropped messages under high load.
- **💾 Memory Efficiency**: Up to **7x to 12x lower memory allocation** compared to Paho v5 (e.g., 20 MiB vs 238 MiB in QoS 2; 81 MiB vs 576 MiB in QoS 1).
- **🔄 GC Overhead**: Significantly fewer garbage collection cycles (e.g., 9 vs 176 for Paho v5 in QoS 2; 52 vs 391 in QoS 1), ensuring predictable latencies and avoiding stop-the-world spikes.

## Performance Highlights

### QoS 0 (Fire & Forget)

The `mq` library dominates in raw throughput scenarios. The table below reflects performance observed in the **Mosquitto v2.1.2** tests (20 workers, 20B payload):

| Metric | mq | Paho v3 | Paho v5 |
|--------|----|---------|---------|
| **Max Publish Rate** | **1,000,014 msg/s** | 186,265 msg/s | 270,971 msg/s |
| **Max E2E Rate** | **536,466 msg/s** | 179,072 msg/s | 216,254 msg/s |
| **Message Delivery** | ✅ 100% | ✅ 100% | ✅ 100% |

*(Note: Peak QoS 0 publish rate for `mq` reached **1,018,011 msg/s** at 4 workers).*

**Key Advantage**: `mq` scales effectively with concurrency, saturating broker ingress at moderate worker counts (4-20) where other clients deliver a fraction of the rate.

### QoS 1 (At Least Once)

In scenarios requiring acknowledgments, `mq` maintains its performance edge across all payload sizes (data from Mosquitto v2.1.2, 50 workers):

| Configuration | mq | Paho v3 | Paho v5 |
|---------------|----|---------|---------|
| **20B Payload** | **130,987 msg/s** | 91,004 msg/s | 61,565 msg/s |
| **128B Payload** | **178,578 msg/s** | 93,047 msg/s | 70,441 msg/s |
| **1KB Payload** | **154,155 msg/s** | 100,225 msg/s | 64,956 msg/s |
| **10KB Payload** | **67,849 msg/s** (✅ 100%) | 51,644 msg/s (⚠️ 32,050/40,000) | 29,610 msg/s (⚠️ 28,824/40,000) |

**Key Advantage**: `mq` processes acknowledgment flows up to **2.5x faster** than Paho v5 and **1.9x faster** than Paho v3, while being the **only client** to achieve 100% message delivery with 10KB payloads at 50 workers.

### QoS 2 (Exactly Once)

Even with the complex 4-step transaction handshake, `mq` delivers superior throughput and efficiency (data from Mosquitto v2.1.2, 20 workers):

| Configuration | mq | Paho v3 | Paho v5 |
|---------------|----|---------|---------|
| **20B Payload** | **61,619 msg/s** | 55,460 msg/s | 34,684 msg/s |
| **128B Payload** | **85,259 msg/s** | 46,308 msg/s | 33,704 msg/s |
| **1KB Payload** | **63,630 msg/s** | 44,891 msg/s | 31,265 msg/s |
| **10KB Payload** | **51,611 msg/s** (✅ 100%) | 32,641 msg/s (✅ 100%) | 25,717 msg/s (⚠️ 14,240/15,000) |

## Resource Efficiency

Memory management is a standout feature of `mq`. Comparisons across workloads highlight its lean memory footprint:

**QoS 1 (50 Workers, 1KB Payload, 40,000 Messages):**
```
mq:        81 MiB TotalAlloc (52 GC cycles)
Paho v3:  210 MiB TotalAlloc (90 GC cycles)   [2.6x more than mq]
Paho v5:  576 MiB TotalAlloc (391 GC cycles)  [7.1x more than mq, 7.5x more GC cycles]
```

**QoS 2 (20 Workers, 128B Payload, 15,000 Messages):**
```
mq:        20 MiB TotalAlloc (9 GC cycles)
Paho v3:   52 MiB TotalAlloc (20 GC cycles)   [2.6x more than mq]
Paho v5:  238 MiB TotalAlloc (176 GC cycles)  [11.9x more than mq, 19.5x more GC cycles]
```

**QoS 0 (50 Workers, 20B Payload, 200,000 Messages):**
```
mq:       172 MiB TotalAlloc (149 GC cycles)
Paho v3:  283 MiB TotalAlloc (109 GC cycles)  [1.6x more than mq]
Paho v5: 1,137 MiB TotalAlloc (673 GC cycles) [6.6x more than mq, 4.5x more GC cycles]
```

**Impact**:
- **Paho v5** exerts heavy pressure on Go's runtime and GC (up to 19.5x more GC cycles), inducing latency jitter under load.
- **mq** maintains a compact, predictable heap profile suitable for high-density, low-latency microservices.

## Reliability & Stability

### Message Delivery Under Load

`mq` demonstrated consistent delivery reliability across all tested scenarios:

- **Mosquitto v2.1.2 Tests**:
    - **Paho v5**: Severe instability—failed in **17 out of 48 test runs** (35% failure rate), repeatedly entering "Subscriber idle for 2s" state and dropping messages across 128B, 1KB, and 10KB payloads and high concurrency.
    - **Paho v3**: Stable across small-payload benchmarks, but suffered packet loss at high concurrency and large payload (QoS 1, 10KB, 50 workers: dropped ~20% of messages).
    - **mq**: **100% reliable across all QoS 1 and QoS 2 tests** (32/32 tests with zero message loss). Under QoS 0 fire-and-forget, `mq`'s extreme publish speed (>500-650k msg/s) triggered broker buffer saturation in 4 stress scenarios, while achieving 100% delivery across the remaining 12 scenarios.

## Server Performance Notes: Mosquitto v2.1.2

Testing with `mq` pushed the Mosquitto v2.1.2 broker to its hardware saturation boundaries. Key observations from the broker perspective:

- **Peak Ingress Rate**: ~1.02 Million msgs/sec (QoS 0, 20B, 4 workers).
- **Peak End-to-End Egress**: ~536,000 msgs/sec (QoS 0, 20B, 20 workers).
- **Peak Data Bandwidth**: ~804 MB/sec (QoS 0, 10KB, 4 workers) and ~662 MB/sec (QoS 1, 10KB, 50 workers).
- **Concurrency Scaling**: Optimal ingress throughput was achieved with 4 to 20 workers. In QoS 1 and QoS 2, `mq` sustained high throughput even as concurrency grew to 50 workers (e.g. 178k msg/s at 128B QoS 1).
- **QoS Handshake Overhead**: Transitioning from QoS 0 to QoS 1 reduced broker delivery throughput by ~67% (peak E2E ~178k vs ~536k msg/s), while QoS 2 reduced throughput by ~84% (peak E2E ~85k msg/s) due to the multi-phase network round trips and state management.

## Detailed Test Reports

- **[Mosquitto 2.1.2 Server Tests](https://gist.github.com/gonzalop/89d2e39d2e5a7f17cfa433c169869ad2)**
- **[Mosquitto 2.1.0 Server Tests](https://gist.github.com/gonzalop/bcfea63b0a74745c89543824113b25c6)**
- **[Mosquitto 2.1rc2 Server Tests](https://gist.github.com/gonzalop/904177a8b4ee9156fe87fcb503b222fa)**
- **[Mosquitto 2.0.22 Server Tests](https://gist.github.com/gonzalop/a338dd8c1c1a85d5e9f42d5fd7644f11)**
- **[Mochi Server Tests](https://gist.github.com/gonzalop/18e2d2ab28c94d32866f1a0d1668c523)**
- **[VerneMQ Server Tests](https://gist.github.com/gonzalop/e8e8addef4464354f674d7bab98896cf)**

## Recommendations

**Use `mq` when you need**:
- **High Throughput**: Proven to handle >1.0 million msg/s publish and >536k msg/s end-to-end.
- **Reliability**: 100% delivered messages in QoS 1 and QoS 2 without client-side stalls or deadlocks.
- **Efficiency**: Drastically lower memory usage (up to 12x lower) and minimal GC overhead.

**Use Paho v3 when**:
- You require legacy MQTT v3.1.1 compatibility in existing codebases that cannot migrate.

**Avoid Paho v5**:
- Exhibits significant instability (35% test failure rate due to idle timeouts/message loss) and excessive memory/GC pressure under load.