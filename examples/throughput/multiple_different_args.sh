#!/bin/bash

SERVER=${1:-127.0.0.1}

echo "Using ${SERVER}:1883 for testing."
echo "Tests will start in 3s"
sleep 3

#
_WORKERS="1 4 20 50"
_SIZES="20 128 1024 10240"

rm -fv results.txt
echo "Sending output to results.txt"
for WORKERS in ${_WORKERS}; do
    for SIZE in ${_SIZES}; do
        QOS=0
        COUNT=200000
        echo "Workers: ${WORKERS} Size: ${SIZE} QoS: ${QOS} Count: ${COUNT}"
        echo ./run_all.sh -server tcp://${SERVER}:1883 -count ${COUNT} -size ${SIZE} -workers ${WORKERS} -qos ${QOS} >> results.txt
        ./run_all.sh -server tcp://${SERVER}:1883 -count ${COUNT} -size ${SIZE} -workers ${WORKERS} -qos ${QOS} >> results.txt
    done
done

for WORKERS in ${_WORKERS}; do
    for SIZE in ${_SIZES}; do
        QOS=1
        COUNT=40000
        echo "Workers: ${WORKERS} Size: ${SIZE} QoS: ${QOS} Count: ${COUNT}"
        echo ./run_all.sh -server tcp://${SERVER}:1883 -count ${COUNT} -size ${SIZE} -workers ${WORKERS} -qos ${QOS} >> results.txt
        ./run_all.sh -server tcp://${SERVER}:1883 -count ${COUNT} -size ${SIZE} -workers ${WORKERS} -qos ${QOS} >> results.txt
    done
done

for WORKERS in ${_WORKERS}; do
    for SIZE in ${_SIZES}; do
        QOS=2
        COUNT=15000
        echo "Workers: ${WORKERS} Size: ${SIZE} QoS: ${QOS} Count: ${COUNT}"
        echo ./run_all.sh -server tcp://${SERVER}:1883 -count ${COUNT} -size ${SIZE} -workers ${WORKERS} -qos ${QOS} >> results.txt
        ./run_all.sh -server tcp://${SERVER}:1883 -count ${COUNT} -size ${SIZE} -workers ${WORKERS} -qos ${QOS} >> results.txt
    done
done
