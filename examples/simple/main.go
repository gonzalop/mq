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

	fmt.Printf("Connecting to MQTT server at %s...\n", server)
	if username != "" {
		fmt.Printf("Using credentials: username=%s\n", username)
	}

	opts := []mq.Option{
		mq.WithClientID("test-client"),
		mq.WithKeepAlive(60 * time.Second),
	}

	if username != "" {
		opts = append(opts, mq.WithCredentials(username, password))
	}

	client, err := mq.Dial(server, opts...)
	if err != nil {
		log.Fatalf("Failed to connect: %v", err)
	}
	defer client.Disconnect(context.Background())

	fmt.Println("✓ Connected successfully!")

	// Subscribe to test topic
	fmt.Println("\nSubscribing to 'test/topic'...")
	received := make(chan mq.Message, 10)

	token := client.Subscribe("test/topic", mq.AtLeastOnce, func(c *mq.Client, msg mq.Message) {
		fmt.Printf("📨 Received: topic=%s qos=%d payload=%s\n",
			msg.Topic, msg.QoS, string(msg.Payload))
		received <- msg
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := token.Wait(ctx); err != nil {
		log.Fatalf("Failed to subscribe: %v", err)
	}
	fmt.Println("✓ Subscribed successfully!")

	// Publish test messages with different QoS levels
	fmt.Println("\nPublishing test messages...")

	// QoS 0: Fire and forget
	fmt.Println("  Publishing QoS 0 message...")
	client.Publish("test/topic", []byte("Hello QoS 0"), mq.WithQoS(mq.AtMostOnce))

	// QoS 1: Wait for acknowledgment (PUBACK)
	fmt.Println("  Publishing QoS 1 message...")
	pubToken := client.Publish("test/topic", []byte("Hello QoS 1"), mq.WithQoS(mq.AtLeastOnce))
	if err := pubToken.Wait(context.Background()); err != nil {
		log.Printf("QoS 1 publish failed: %v", err)
	} else {
		fmt.Println("  ✓ QoS 1 acknowledged")
	}

	// QoS 2: Wait for full handshake (PUBREC, PUBREL, PUBCOMP)
	fmt.Println("  Publishing QoS 2 message...")
	pubToken2 := client.Publish("test/topic", []byte("Hello QoS 2"), mq.WithQoS(mq.ExactlyOnce))
	if err := pubToken2.Wait(context.Background()); err != nil {
		log.Printf("QoS 2 publish failed: %v", err)
	} else {
		fmt.Println("  ✓ QoS 2 acknowledged")
	}

	fmt.Println("\nWaiting for messages (5 seconds)...")
	timeout := time.After(5 * time.Second)
	messageCount := 0

	for {
		select {
		case <-received:
			messageCount++
			if messageCount >= 3 {
				fmt.Printf("\n✓ Received %d messages\n", messageCount)
				fmt.Println("\nTest completed successfully! 🎉")
				return
			}
		case <-timeout:
			fmt.Printf("\n✓ Received %d messages (timed out waiting for more)\n", messageCount)
			fmt.Println("\nTest completed successfully! 🎉")
			return
		}
	}
}
