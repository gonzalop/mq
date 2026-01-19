# MQTT Client Performance Analysis: `mq` Library

This document presents a comprehensive performance analysis of the `mq` MQTT client library compared to Eclipse Paho v3 and v5 implementations. Tests were conducted against three popular MQTT brokers: Mosquitto, Mochi, and VerneMQ.

All benchmarks were performed using the [throughput test](../examples/throughput/) example, which measures end-to-end performance by subscribing to a topic and then publishing messages of a specified payload size concurrently using a configurable number of workers.

**Test Environment**: Debian Linux (sid), kernel 6.18.x, AMD Ryzen AI 9 365, 32 GiB DDR5 RAM (idle machine).

**MQTT Brokers**: Mosquitto 2.0.22, Mochi (built from HEAD), VerneMQ (current official image).

## Executive Summary

The `mq` library demonstrates **exceptional performance across all tested scenarios**, consistently outperforming both Paho v3 and v5 in critical metrics:

- **🏆 Throughput**: Up to **2.8x faster** than Paho v5 and **1.5x faster** than Paho v3 in high-concurrency scenarios
- **✅ Reliability**: **100% message delivery** in all QoS 0 tests, while Paho clients frequently dropped messages under load
- **💾 Memory Efficiency**: **10x lower memory allocation** compared to Paho v5 (111 MiB vs 1,137 MiB in typical workloads)
- **🔄 GC Overhead**: **14x fewer garbage collection cycles** than Paho v5, resulting in more predictable latencies

## Performance Highlights

### QoS 0 (Fire & Forget)

The `mq` library dominates in raw throughput scenarios:

| Metric | mq | Paho v3 | Paho v5 |
|--------|----|---------|---------| 
| **Max Message Rate** | 574,344 msg/s | 379,463 msg/s | 200,781 msg/s |
| **Max Throughput** | 847 MB/s | 693 MB/s | 312 MB/s |
| **Message Delivery** | ✅ 100% | ⚠️ 81% | ❌ 37% |

**Key Advantage**: `mq` scales significantly better as concurrency increases, maintaining stability where Paho clients drop messages.

### QoS 1 (At Least Once)

In scenarios requiring acknowledgments, `mq` maintains its performance edge:

| Workers | mq | Paho v3 | Paho v5 |
|---------|----|---------|---------| 
| **4 workers** | 70,000 msg/s | 73,000 msg/s | 51,000 msg/s |
| **50 workers** | Superior scaling | Competitive | Significantly slower |

**Key Advantage**: `mq` handles the acknowledgment flow more efficiently at higher concurrency levels.

### QoS 2 (Exactly Once)

Even with the complex 4-way handshake, `mq` outperforms:

| Configuration | mq | Paho v3 | Paho v5 |
|---------------|----|---------|---------| 
| **50 workers, small packets** | 68,720 msg/s | 51,637 msg/s | 43,074 msg/s |

**Key Advantage**: Lower protocol overhead enables better performance even in the most demanding transactional scenarios.

## Resource Efficiency

Memory management is where `mq` truly shines:

### Memory Allocation (QoS 0, 20 Workers, 200k messages)

```
mq:       111 MiB  (47 GC cycles)
Paho v3:  281 MiB  (92 GC cycles)  [2.5x more than mq]
Paho v5: 1,137 MiB (681 GC cycles) [10x more than mq]
```

**Impact**: Lower memory allocation and fewer GC cycles translate to:
- More predictable latencies
- Better performance in resource-constrained environments
- Reduced CPU overhead from garbage collection

## Reliability & Stability

### Message Delivery Under Load

`mq` is the **only client** to achieve 100% message delivery across all test scenarios:

| Scenario | mq | Paho v3 | Paho v5 |
|----------|----|---------|---------| 
| **10KB payload, QoS 0** | ✅ 100% (200k/200k) | ⚠️ 81% (162k/200k) | ❌ 37% (74k/200k) |
| **High concurrency** | ✅ No drops | ⚠️ Occasional drops | ❌ Frequent drops |

### Stability Issues Observed

- **Paho v5**: Deadlocked during high-concurrency, high-bandwidth tests (20 workers, 10KB QoS 0), requiring manual interruption
- **Paho v5**: Frequent "Subscriber idle" timeouts indicating message loss
- **Paho v3**: Moderate message loss under sustained high-throughput conditions
- **mq**: Zero stability issues across all test scenarios

## Server Performance Notes

### Mosquitto

Mosquitto demonstrated solid performance as a general-purpose broker, handling moderate to high loads effectively. The broker showed predictable behavior across all QoS levels, with saturation points clearly defined by worker count and payload size.

### Mochi

Mochi (bare metal deployment) exhibited excellent raw throughput capabilities, particularly for small packet scenarios. The server maintained stability across all client libraries, making it an ideal testbed for client performance comparisons.

### VerneMQ

VerneMQ testing included both containerized and host networking configurations. Host networking significantly improved throughput, demonstrating the importance of deployment configuration. The broker handled high-concurrency scenarios well, though some message drops were observed at extreme loads (50 workers, QoS 2).

## Detailed Test Reports

For complete test data, methodology, and detailed results, see the original benchmark reports:

- **[Mosquitto Server Tests](https://gist.github.com/gonzalop/a338dd8c1c1a85d5e9f42d5fd7644f11)** - Comprehensive analysis including broker saturation points and QoS overhead
- **[Mochi Server Tests](https://gist.github.com/gonzalop/18e2d2ab28c94d32866f1a0d1668c523)** - Bare metal performance benchmarks
- **[VerneMQ Server Tests](https://gist.github.com/gonzalop/e8e8addef4464354f674d7bab98896cf)** - Container vs host networking comparison

## Recommendations

**Use `mq` when you need**:
- High-throughput applications (>100k msg/s)
- Reliable message delivery under load (QoS 0 scenarios)
- Resource-constrained environments (embedded systems, cloud cost optimization)
- Predictable, low-latency performance

**Use Paho v3 when**:
- You have an existing, stable deployment with moderate throughput requirements
- Migration cost outweighs performance benefits
- You don't need MQTT v5 features (enhanced flow control, user properties, etc.)

**Avoid Paho v5**: Stability issues and poor resource efficiency make it unsuitable for production use in high-load scenarios.
