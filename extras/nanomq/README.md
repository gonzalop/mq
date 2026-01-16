# NanoMQ for Integration Testing

This directory contains a Dockerfile and configuration to build a NanoMQ image for integration testing.

## Building the Image

To build the image, run the following command in this directory:

```bash
podman build -t minimal-nanomq .
```

*(Note: You can also use `docker` instead of `podman`.)*

## Running Integration Tests

To run the project's integration tests using the built image, set the `MQTT_SERVER_IMAGE` environment variable when running `make integration`:

```bash
MQTT_SERVER_IMAGE=localhost/minimal-nanomq:latest make integration
```

## Known Issues

Currently, several integration tests are expected to fail when using NanoMQ. These failures are likely due to server-side bugs or specific configuration requirements that haven't been fully identified yet. These same tests pass successfully with Mosquitto and the patched Mochi server.

Known failing tests include:
- `TestServerLimits` (MaximumPacketSize, ReceiveMaximum)
- `TestDefaultPublishHandlerIntegration`
- `TestLastWillWithDelay`
- `TestPersistenceIntegration`
- `TestSessionExpiry`
- `TestCleanSession`
- `TestKeepAliveDisabled`

On top of this, if you use nanomq with the throughput program under `examples/` and run it a few times, it leaks memory big time.
