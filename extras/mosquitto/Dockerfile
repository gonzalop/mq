# Build with: (podman|docker) build -t minimal-mosquitto .
# Then you can: MQTT_SERVER_IMAGE=localhost/minimal-mosquitto:latest make integration

FROM alpine:latest AS builder

RUN apk add --no-cache \
    git \
    ca-certificates \
    openssl-dev \
    c-ares-dev \
    libmicrohttpd-dev \
    sqlite-dev \
    cjson-dev \
    libedit-dev \
    gcc \
    g++ \
    libc-dev \
    make

WORKDIR /src

RUN git clone https://github.com/eclipse-mosquitto/mosquitto.git

RUN mkdir -p /install && cd mosquitto && git checkout v2.1.0 && make clean && make -j10 WITH_DOCS=no DESTDIR=/install install

FROM alpine:latest

RUN apk add --no-cache \
    ca-certificates \
    openssl \
    c-ares \
    libmicrohttpd \
    sqlite \
    cjson \
    libedit

COPY --from=builder /install /
RUN mkdir -p /mosquitto/config /mosquitto/data /mosquitto/log
COPY mosquitto.conf /mosquitto/config/mosquitto.conf
WORKDIR /mosquitto
VOLUME ["/mosquitto/data", "/mosquitto/log"]

CMD ["/usr/local/sbin/mosquitto", "-c", "/mosquitto/config/mosquitto.conf"]
