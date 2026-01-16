//go:build ignore_test

package main

import (
	"context"
	"flag"
	"fmt"
	"math/rand"
	"net/url"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/eclipse/paho.golang/autopaho"
	"github.com/eclipse/paho.golang/paho"
)

func main() {
	var (
		server   = flag.String("server", "tcp://localhost:1883", "MQTT server address")
		topic    = flag.String("topic", "bench/throughput/paho5", "Topic to publish to")
		msgCount = flag.Int("count", 100000, "Number of messages to publish")
		msgSize  = flag.Int("size", 1024, "Size of message payload in bytes")
		qos      = flag.Int("qos", 1, "QoS level (0, 1, or 2)")
		workers  = flag.Int("workers", 10, "Number of concurrent publisher workers")
	)
	flag.Parse()

	ctx := context.Background()
	u, err := url.Parse(*server)
	if err != nil {
		panic(err)
	}

	// 1. Subscriber
	var receivedCount atomic.Int64
	var lastReceived atomic.Int64
	lastReceived.Store(time.Now().UnixNano())

	doneCh := make(chan struct{})

	subCfg := autopaho.ClientConfig{
		BrokerUrls: []*url.URL{u},
		KeepAlive:  60,
		OnConnectionUp: func(cm *autopaho.ConnectionManager, connAck *paho.Connack) {
			// Subscribe on connection
			if _, err := cm.Subscribe(context.Background(), &paho.Subscribe{
				Subscriptions: []paho.SubscribeOptions{
					{Topic: *topic, QoS: byte(*qos)},
				},
			}); err != nil {
				fmt.Printf("Subscribe failed: %v\n", err)
			}
		},
		ClientConfig: paho.ClientConfig{
			ClientID: "bench-paho5-sub-" + randomString(5),
			Router: paho.NewSingleHandlerRouter(func(m *paho.Publish) {
				newVal := receivedCount.Add(1)
				lastReceived.Store(time.Now().UnixNano())
				if newVal == int64(*msgCount) {
					close(doneCh)
				}
			}),
		},
	}

	subCM, err := autopaho.NewConnection(ctx, subCfg)
	if err != nil {
		panic(err)
	}
	if err := subCM.AwaitConnection(ctx); err != nil {
		panic(err)
	}
	fmt.Printf("✅ Paho v5 Subscriber connected. Expecting %d messages...\n", *msgCount)

	// 2. Publisher
	pubCfg := autopaho.ClientConfig{
		BrokerUrls: []*url.URL{u},
		KeepAlive:  60,
		ClientConfig: paho.ClientConfig{
			ClientID: "bench-paho5-pub-" + randomString(5),
		},
	}

	pubCM, err := autopaho.NewConnection(ctx, pubCfg)
	if err != nil {
		panic(err)
	}
	if err := pubCM.AwaitConnection(ctx); err != nil {
		panic(err)
	}

	payload := make([]byte, *msgSize)
	rand.Read(payload)

	fmt.Printf("🚀 Starting Paho v5 publish of %d messages (%d bytes each) with QoS %d...\n", *msgCount, *msgSize, *qos)
	start := time.Now()

	var pubWg sync.WaitGroup
	msgsPerWorker := *msgCount / *workers

	for i := 0; i < *workers; i++ {
		pubWg.Add(1)
		go func() {
			defer pubWg.Done()
			for j := 0; j < msgsPerWorker; j++ {
				_, err := pubCM.Publish(context.Background(), &paho.Publish{
					Topic:   *topic,
					QoS:     byte(*qos),
					Payload: payload,
				})
				if err != nil {
					fmt.Printf("Publish error: %v\n", err)
				}
			}
		}()
	}

	pubWg.Wait()
	pubTime := time.Since(start)
	fmt.Printf("📤 Publish done in %v\n", pubTime)
	fmt.Printf("   Rate: %.2f msgs/sec\n", float64(*msgCount)/pubTime.Seconds())

	// Wait for subscriber
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	fmt.Println("⏳ Waiting for subscriber to finish or drain...")

	finalTime := time.Now()
	timeout := 2 * time.Second // Halt if no messages for 2s

Loop:
	for {
		select {
		case <-doneCh:
			finalTime = time.Now()
			break Loop
		case <-ticker.C:
			current := receivedCount.Load()
			if current == int64(*msgCount) {
				finalTime = time.Now()
				break Loop
			}

			// Check for idle timeout
			lr := lastReceived.Load()
			if time.Since(time.Unix(0, lr)) > timeout {
				fmt.Printf("⚠️ Subscriber idle for %v (likely dropped messages), stopping.\n", timeout)
				finalTime = time.Unix(0, lr)
				break Loop
			}

			if time.Since(start) > 60*time.Second {
				fmt.Println("❌ Global timeout reached")
				break Loop
			}
		}
	}

	totalTime := finalTime.Sub(start)
	count := receivedCount.Load()
	fmt.Printf("✅ Messages received: %d/%d in %v\n", count, *msgCount, totalTime)
	if totalTime > 0 {
		fmt.Printf("   End-to-End Rate: %.2f msgs/sec\n", float64(count)/totalTime.Seconds())
		fmt.Printf("   Throughput: %.2f MB/sec\n", float64(count*int64(*msgSize))/1024/1024/totalTime.Seconds())
	}
	printMemUsage()
}

func printMemUsage() {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	fmt.Printf("\n💾 Memory Usage:\n")
	fmt.Printf("   Alloc = %v MiB\n", bToMb(m.Alloc))
	fmt.Printf("   TotalAlloc = %v MiB\n", bToMb(m.TotalAlloc))
	fmt.Printf("   Sys = %v MiB\n", bToMb(m.Sys))
	fmt.Printf("   NumGC = %v\n", m.NumGC)
}

func bToMb(b uint64) uint64 {
	return b / 1024 / 1024
}

func randomString(n int) string {
	var letters = []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ")
	b := make([]rune, n)
	for i := range b {
		b[i] = letters[rand.Intn(len(letters))]
	}
	return string(b)
}
