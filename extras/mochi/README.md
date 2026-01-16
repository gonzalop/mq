# Mochi MQTT Server for Integration Testing

This directory contains a Dockerfile and a patch to build a Mochi MQTT server image tailored for integration testing.

## Building the Image

To build the image, run the following command in this directory:

```bash
docker build -t minimal-mochi .
```

*(Note: You can also use `podman` instead of `docker`.)*

## Running Integration Tests

To run the project's integration tests using the built image, set the `MQTT_SERVER_IMAGE` environment variable when running `make integration`:

```bash
MQTT_SERVER_IMAGE=localhost/minimal-mochi:latest make integration
```

## Patching and Compatibility

We apply `server_config.patch` during the build process to:
1. Fix a server bug related to property handling.
2. Add support for `--max-packet-size` and `--max-receive` command-line flags, which are necessary for testing MQTT 5.0 limits.

We intend to submit these improvements upstream. With this patch, all integration tests are expected to pass when using Mochi.
