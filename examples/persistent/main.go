//go:build ignore_test

package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/gonzalop/mq"
)

func main() {
	// Parse arguments (server, username, password)
	server := "tcp://localhost:1883"
	if len(os.Args) > 1 {
		server = os.Args[1]
	}

	username := os.Getenv("MQTT_USERNAME")
	password := os.Getenv("MQTT_PASSWORD")

	if len(os.Args) > 2 {
		username = os.Args[2]
	}
	if len(os.Args) > 3 {
		password = os.Args[3]
	}

	clientID := "persistent-client-example"
	topic := "data/persistent"

	addCreds := func(opts []mq.Option) []mq.Option {
		if username != "" {
			return append(opts, mq.WithCredentials(username, password))
		}
		return opts
	}

	// 1. Connect first to establish session
	fmt.Println("--- Phase 1: Establishing Session ---")
	opts1 := []mq.Option{
		mq.WithClientID(clientID),
		mq.WithCleanSession(false),               // Request persistent session
		mq.WithSessionExpiryInterval(0xFFFFFFFF), // Required for v5 persistence
	}
	client1, err := mq.Dial(server, addCreds(opts1)...)
	if err != nil {
		log.Fatalf("Failed to connect: %v", err)
	}

	// Subscribe (subscription will be persisted by server)
	token := client1.Subscribe(topic, mq.AtLeastOnce, func(c *mq.Client, msg mq.Message) {
		fmt.Printf("[Client 1] Received: %s\n", string(msg.Payload))
	})
	token.Wait(context.Background())
	fmt.Println("Subscribed to", topic)

	// Disconnect gracefully
	client1.Disconnect(context.Background())
	fmt.Println("Client 1 disconnected")

	// 2. Publish message while client is offline
	fmt.Println("\n--- Phase 2: Publishing while Offline ---")
	pubOpts := []mq.Option{mq.WithClientID("publisher")}
	publisher, err := mq.Dial(server, addCreds(pubOpts)...)
	if err != nil {
		log.Fatal(err)
	}
	defer publisher.Disconnect(context.Background())

	msg := "This message was sent while you were sleeping 💤"
	publisher.Publish(topic, []byte(msg), mq.WithQoS(mq.AtLeastOnce)).Wait(context.Background())
	fmt.Printf("Published: %q\n", msg)

	// 3. Reconnect and receive offline message
	fmt.Println("\n--- Phase 3: Reconnecting ---")

	received := make(chan struct{})

	// CRITICAL: We use WithSubscription to register the handler BEFORE connection.
	// If we used Subscribe() after connecting, we might miss messages delivered immediately
	// upon connection if they arrive before the Subscribe packet is processed.
	client2, err := mq.Dial(server,
		mq.WithClientID(clientID),  // Must match previous ID
		mq.WithCleanSession(false), // Resume session
		mq.WithSessionExpiryInterval(0xFFFFFFFF),

		// Pre-register handler for the persistent subscription
		mq.WithSubscription(topic, func(c *mq.Client, msg mq.Message) {
			fmt.Printf("[Client 2] Received offline message: %s\n", string(msg.Payload))
			close(received)
		}),
	)
	if err != nil {
		log.Fatalf("Failed to connect: %v", err)
	}
	defer client2.Disconnect(context.Background())

	// Wait for the offline message
	select {
	case <-received:
		fmt.Println("✓ Successfully received offline message!")
	case <-time.After(3 * time.Second):
		fmt.Println("❌ Timed out waiting for offline message")
	}
}
