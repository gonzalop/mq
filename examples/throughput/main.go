//go:build ignore_test

package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"math/rand"
	"os"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gonzalop/mq"
)

func main() {
	var (
		server   = flag.String("server", "tcp://localhost:1883", "MQTT server address")
		topic    = flag.String("topic", "bench/throughput", "Topic to publish to")
		msgCount = flag.Int("count", 100000, "Number of messages to publish")
		msgSize  = flag.Int("size", 1024, "Size of message payload in bytes")
		qos      = flag.Int("qos", 1, "QoS level (0, 1, or 2)")
		workers  = flag.Int("workers", 10, "Number of concurrent publisher workers")
	)
	flag.Parse()

	// Logger setup
	opts := &slog.HandlerOptions{Level: slog.LevelInfo}
	logger := slog.New(slog.NewTextHandler(os.Stdout, opts))

	ctx := context.Background()

	// 1. Start Subscriber
	// mq.Dial connects synchronously
	subClient, err := mq.Dial(
		*server,
		mq.WithClientID("bench-sub-"+randomString(5)),
		mq.WithLogger(logger.With("client", "sub")),
	)
	if err != nil {
		panic(err)
	}
	defer subClient.Disconnect(context.Background())

	var receivedCount atomic.Int64
	var lastReceived atomic.Int64
	lastReceived.Store(time.Now().UnixNano())

	doneCh := make(chan struct{})

	// Subscribe
	token := subClient.Subscribe(*topic, mq.QoS(*qos), func(c *mq.Client, m mq.Message) {
		newVal := receivedCount.Add(1)
		lastReceived.Store(time.Now().UnixNano())
		if newVal == int64(*msgCount) {
			close(doneCh)
		}
	})
	if err := token.Wait(ctx); err != nil {
		panic(err)
	}
	fmt.Printf("✅ Subscriber connected and ready. Expecting %d messages...\n", *msgCount)

	// 2. Start Publisher(s)
	pubClient, err := mq.Dial(
		*server,
		mq.WithClientID("bench-pub-"+randomString(5)),
		mq.WithProtocolVersion(mq.ProtocolV50),
		mq.WithLogger(logger.With("client", "pub")),
	)
	if err != nil {
		panic(err)
	}
	defer pubClient.Disconnect(context.Background())

	payload := make([]byte, *msgSize)
	rand.Read(payload) // Random payload

	fmt.Printf("🚀 Starting publish of %d messages (%d bytes each) with QoS %d...\n", *msgCount, *msgSize, *qos)
	start := time.Now()

	var pubWg sync.WaitGroup
	msgsPerWorker := *msgCount / *workers

	for i := 0; i < *workers; i++ {
		pubWg.Add(1)
		go func() {
			defer pubWg.Done()
			for j := 0; j < msgsPerWorker; j++ {
				token := pubClient.Publish(*topic, payload, mq.WithQoS(mq.QoS(*qos)))
				if err := token.Wait(ctx); err != nil {
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
				finalTime = time.Unix(0, lr) // Use last receive time for accuracy
				break Loop
			}

			// Safety global timeout
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
