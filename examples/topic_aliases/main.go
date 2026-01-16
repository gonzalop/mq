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

	// 1. Connect with Topic Alias support
	// We must tell the server how many aliases WE can accept (WithTopicAliasMaximum).
	// The library automatically handles the server's limit for us.
	fmt.Println("Connecting with Topic Alias support...")

	opts := []mq.Option{
		mq.WithClientID("alias-example"),
		mq.WithTopicAliasMaximum(100), // Verification: We can map up to 100 incoming topics
	}

	if username != "" {
		opts = append(opts, mq.WithCredentials(username, password))
	}

	client, err := mq.Dial(server, opts...)
	if err != nil {
		log.Fatalf("Failed to connect: %v", err)
	}
	defer client.Disconnect(context.Background())

	fmt.Println("✓ Connected! Negotiating aliases...")

	// check what the server allows
	caps := client.ServerCapabilities()
	fmt.Printf("   Server Topic Alias Max: %d\n", caps.TopicAliasMaximum)
	if caps.TopicAliasMaximum == 0 {
		fmt.Println("⚠️  Warning: Server does not support Topic Aliases. Example will fallback to standard publishing.")
	}

	// 2. Publish using Aliases
	// Ideally used for periodic data on long topics (e.g., sensors)
	topic := "sensors/warehouse-a/aisle-4/shelf-2/temperature"
	fmt.Printf("\nPublishing to long topic: %s\n", topic)

	for i := 1; i <= 5; i++ {
		payload := fmt.Sprintf("23.%d", i)

		// Use mq.WithAlias() to request alias usage.
		// - 1st Call: Library sends [Topic: "...", Alias: 1, Payload: ...]
		// - 2nd Call: Library sends [Topic: "",    Alias: 1, Payload: ...] (Savings!)
		token := client.Publish(topic, []byte(payload), mq.WithQoS(1), mq.WithAlias())

		if err := token.Wait(context.Background()); err != nil {
			log.Fatalf("Publish failed: %v", err)
		}

		fmt.Printf("   Message %d sent (optimized)\n", i)
		time.Sleep(500 * time.Millisecond)
	}

	fmt.Println("\n✓ Finished publishing. Bandwidth saved!")
}
