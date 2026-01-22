//go:build ignore_test

// This example demonstrates how to implement client-side routing for messages
// using mq.WithDefaultPublishHandler and mq.MatchTopic.
//
// This pattern is useful for:
//   - Centralized message dispatching
//   - "Snoop" logging (logging all messages that pass through)
//   - Handling messages for subscriptions managed externally or statically
//
// Usage:
//   go run main.go

package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/gonzalop/mq"
)

func main() {
	// 1. Define our custom routes map
	// These are filters we want to handle locally.
	// Note: We are NOT calling client.Subscribe() with these handlers directly.
	routes := map[string]mq.MessageHandler{
		"system/alerts/#": handleSystemAlert,
		"logs/error":      handleErrorLog,
	}

	// 2. Create the central router (DefaultPublishHandler)
	// This function receives ALL messages that don't match a specific
	// callback registered via client.Subscribe().
	router := func(c *mq.Client, msg mq.Message) {
		matched := false
		// Check the message topic against our route filters
		for filter, handler := range routes {
			if mq.MatchTopic(filter, msg.Topic) {
				handler(c, msg)
				matched = true
			}
		}

		if !matched {
			fmt.Printf("[UNMATCHED] Topic: %s, Payload: %s\n", msg.Topic, string(msg.Payload))
		}
	}

	// 3. Configure the client
	server := "tcp://localhost:1883"
	if v := os.Getenv("MQTT_SERVER"); v != "" {
		server = v
	}

	opts := []mq.Option{
		mq.WithClientID("router-example"),
		mq.WithDefaultPublishHandler(router), // Register our router
	}

	// Load credentials from environment
	if v := os.Getenv("MQTT_PASSWORD"); v != "" {
		opts = append(opts, mq.WithCredentials(os.Getenv("MQTT_USERNAME"), v))
	}

	client, err := mq.Dial(server, opts...)
	if err != nil {
		log.Fatalf("Failed to connect: %v", err)
	}
	defer client.Disconnect(context.Background())

	fmt.Println("Connected. Default handler (router) is active.")

	// 4. Subscribe to a wide wildcard (or specific topics) to pull messages in.
	// The DefaultPublishHandler is ONLY called if no specific handler is attached
	// to the subscription. Here we pass 'nil' as the handler for Subscribe.
	fmt.Println("Subscribing to '#' to capture traffic...")
	if err := client.Subscribe("#", mq.AtLeastOnce, nil).Wait(context.Background()); err != nil {
		log.Fatalf("Subscribe failed: %v", err)
	}

	// 5. Publish test messages
	// These messages will be received by our client (because of subscription to "#"),
	// and since we passed nil to Subscribe, they will fall through to the router.

	fmt.Println("\n--- Publishing Tests ---")

	// Should match "system/alerts/#"
	client.Publish("system/alerts/cpu", []byte("CPU > 90%"), mq.WithQoS(1)).Wait(context.Background())

	// Should match "logs/error"
	client.Publish("logs/error", []byte("Disk full"), mq.WithQoS(1)).Wait(context.Background())

	// Should be [UNMATCHED]
	client.Publish("users/chat", []byte("Hello world"), mq.WithQoS(1)).Wait(context.Background())

	fmt.Println("------------------------")
	fmt.Println("Waiting for messages... (Press Ctrl+C to quit)")

	// Wait for signal to quit
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	<-sig
}

func handleSystemAlert(c *mq.Client, msg mq.Message) {
	fmt.Printf("[ALERT]     %s: %s\n", msg.Topic, string(msg.Payload))
}

func handleErrorLog(c *mq.Client, msg mq.Message) {
	fmt.Printf("[ERROR]     %s\n", string(msg.Payload))
}
