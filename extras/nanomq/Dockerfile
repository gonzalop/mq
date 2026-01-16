# Build with: (podman|docker) build -t minimal-nanomq .
# Then you can: MQTT_SERVER_IMAGE=localhost/minimal-nanomq:latest make integration

# A few tests fail with nanomq and I don't know if it's me not figuring out the right
# configuration or nanomq missing the mark.
#
# The tests known to fail with nanomq (work with mochi-server and mosquitto-server):
# --- FAIL: TestServerLimits (0.28s)
#     --- FAIL: TestServerLimits/MaximumPacketSize (0.10s)
#     --- FAIL: TestServerLimits/ReceiveMaximum (0.10s)
# --- FAIL: TestDefaultPublishHandlerIntegration (5.54s)
# --- FAIL: TestLastWillWithDelay (5.64s)
# --- FAIL: TestPersistenceIntegration (0.00s)
#     --- FAIL: TestPersistenceIntegration/ServerSessionResumed (5.64s)
# --- FAIL: TestSessionExpiry (0.00s)
#     --- FAIL: TestSessionExpiry/SessionPersistence (5.31s)
#     --- FAIL: TestSessionExpiry/OrphanedSubscription (5.30s)
# --- FAIL: TestCleanSession (7.34s)
# --- FAIL: TestKeepAliveDisabled (10.04s)

FROM alpine:latest AS builder

# Install build dependencies
# build-base: gcc, g++, make, etc.
# cmake, ninja: build systems
# git: to clone the repo
# linux-headers: required for compiling some system-level code
RUN apk add --no-cache \
    git \
    cmake \
    ninja \
    build-base \
    linux-headers

WORKDIR /src
RUN git clone https://github.com/emqx/nanomq.git . \
    && git submodule update --init --recursive

# Create build directory
WORKDIR /src/build

# Configure the build
# -DCMAKE_BUILD_TYPE=Release: Optimizes for performance and size
# -DBUILD_CLIENT=OFF: Disables building CLI tools (sub/pub) for a smaller image.
#                     Remove this flag if you want the client tools included.
# -DNNG_ENABLE_TLS=OFF: Keeps it minimal. Set to ON if you need SSL/TLS (requires mbedtls-dev).
RUN cmake -G Ninja \
    -DCMAKE_BUILD_TYPE=Release \
    -DBUILD_CLIENT=OFF \
    -DNNG_ENABLE_TLS=OFF \
    ..

RUN ninja

FROM alpine:latest

WORKDIR /opt/nanomq

COPY --from=builder /src/build/nanomq/nanomq /usr/local/bin/nanomq

COPY nanomq.conf /etc/nanomq.conf

# We specify the config file explicitly to ensure it loads settings
# Go Testcontainers passes the --url argument with scheme, IP, and port.
ENTRYPOINT ["nanomq"]
CMD ["nanomq", "start", "--conf", "/etc/nanomq.conf"]
