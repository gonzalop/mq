# Throughput Benchmarks

This directory contains high-performance throughput benchmarks for comparing the `mq` library against Eclipse Paho (v3 and v5).

## Structure

- **`mq` (default)**: The benchmark implementation using this library (`mq`). Located in `main.go`.
- **`paho_v3/`**: Comparison implementation using `github.com/eclipse/paho.mqtt.golang` (MQTT v3.1.1).
- **`paho_v5/`**: Comparison implementation using `github.com/eclipse/paho.golang` (MQTT v5.0).
- **`run_all.sh`**: Runs the standard comparison suite.
- **`multiple_different_args.sh`**: Runs benchmarks across a wide matrix of message sizes, QoS levels, and worker counts.

## Usage

To run the `mq` benchmark:
```bash
go run main.go -size 1024 -count 100000 -workers 10
```

To run the full comparative suite:
```bash
./run_all.sh
```

To run the extended matrix of tests (sizes/qos/workers):
```bash
./multiple_different_args.sh
```

## Comparisons

These examples demonstrate the high-throughput capabilities and **memory efficiency** of `mq`'s shared-state concurrency model, particularly in high-concurrency publishing scenarios.
