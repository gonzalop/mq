# Build with: (podman|docker) build -t minimal-mochi .
# Then you can: MQTT_SERVER_IMAGE=localhost/minimal-mochi:latest make integration

# There are 3 server limits tests that fail with mochi, but the patch fixes them.
# I'll remove the patch once it makes it upstream.

FROM golang:1.25-alpine AS builder

RUN apk add --no-cache \
    git \
    patch

WORKDIR /src

COPY server_config.patch /src/server_config.patch

RUN git clone https://github.com/mochi-mqtt/server.git && \
    cd server && \
    patch -p1 < /src/server_config.patch && \
    cd cmd && \
    go build -o mochi-server .

FROM alpine:latest

COPY --from=builder /src/server/cmd/mochi-server /usr/local/bin/mochi-server

ENTRYPOINT ["mochi-server"]
CMD ["mochi-server"]
