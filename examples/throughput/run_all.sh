#!/bin/bash

# default: # -server tcp://localhost:1883 -count 100000 -size 1024 -workers 10 -qos 1"
# Use a non-local MQTT server when possible.

# Paho 3 is included for reference, but it's not really comparing apples to apples since
# it doesn't encode properties, topic aliases, flow control,...
echo paho_v3
(cd paho_v3; go run main.go $@)

echo paho_v5
(cd paho_v5; go run main.go $@)

echo mq
go run main.go $@

