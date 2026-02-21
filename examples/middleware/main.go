//go:build ignore_test

package main

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"os"
	"time"

	"github.com/gonzalop/mq"
)

func main() {
	// Get server address from command line or use default
	server := "tcp://localhost:1883"
	if len(os.Args) > 1 {
		server = os.Args[1]
	}

	// Get credentials from environment or command line
	username := os.Getenv("MQTT_USERNAME")
	password := os.Getenv("MQTT_PASSWORD")

	if len(os.Args) > 2 {
		username = os.Args[2]
	}
	if len(os.Args) > 3 {
		password = os.Args[3]
	}

	// A simple logger for demonstration
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	// 1. Define a Handler Interceptor (Incoming)
	// This one logs every message and how long it takes to process.
	loggingInterceptor := func(next mq.MessageHandler) mq.MessageHandler {
		return func(c *mq.Client, m mq.Message) {
			start := time.Now()
			logger.Info("Incoming message", "topic", m.Topic, "size", len(m.Payload))

			// Call the next handler in the chain
			next(c, m)

			logger.Info("Message processed", "topic", m.Topic, "duration", time.Since(start))
		}
	}

	// 2. Define a Publish Interceptor (Outgoing)
	// This one adds a custom "trace-id" User Property to every outgoing message
	// and logs the publish attempt.
	tracingInterceptor := func(next mq.PublishFunc) mq.PublishFunc {
		return func(topic string, payload []byte, opts ...mq.PublishOption) mq.Token {
			traceID := fmt.Sprintf("trace-%d", time.Now().UnixNano())

			// Append a new option to the existing ones
			newOpts := append(opts, mq.WithUserProperty("x-trace-id", traceID))

			logger.Info("Publishing message", "topic", topic, "trace_id", traceID)

			// Call the next function in the chain
			return next(topic, payload, newOpts...)
		}
	}

	// 3. Create client with interceptors
	ctx := context.Background()
	opts := []mq.Option{
		mq.WithClientID("middleware-example"),
		mq.WithHandlerInterceptor(loggingInterceptor),
		mq.WithPublishInterceptor(tracingInterceptor),
		mq.WithLogger(logger),
	}

	if username != "" {
		opts = append(opts, mq.WithCredentials(username, password))
	}

	fmt.Printf("Connecting to MQTT server at %s...\n", server)
	client, err := mq.Dial(server, opts...)
	if err != nil {
		log.Fatalf("Failed to connect: %v", err)
	}
	defer client.Disconnect(ctx)

	fmt.Println("✅ Connected successfully!")

	// 4. Subscribe
	// The loggingInterceptor will automatically wrap this handler.
	fmt.Println("Subscribing to 'example/middleware'...")
	subToken := client.Subscribe("example/middleware", mq.AtLeastOnce, func(c *mq.Client, msg mq.Message) {
		traceID := "none"
		if msg.Properties != nil {
			if tid, ok := msg.Properties.UserProperties["x-trace-id"]; ok {
				traceID = tid
			}
		}
		fmt.Printf("\n--- 📨 Handler Received: %s (TraceID: %s) ---\n\n", string(msg.Payload), traceID)
	})

	if err := subToken.Wait(ctx); err != nil {
		log.Fatalf("Subscription failed: %v", err)
	}

	// 5. Publish
	// The tracingInterceptor will automatically wrap this call.
	fmt.Println("Publishing test message...")
	token := client.Publish("example/middleware", []byte("Hello Middleware!"), mq.WithQoS(mq.AtLeastOnce))
	if err := token.Wait(ctx); err != nil {
		logger.Error("Publish failed", "error", err)
	}

	// Wait a bit to see the incoming message logs
	time.Sleep(1 * time.Second)
	fmt.Println("✅ Example completed!")
}
