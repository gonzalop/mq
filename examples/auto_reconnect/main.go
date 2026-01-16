//go:build ignore_test

package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
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

	fmt.Printf("Auto-Reconnect Example\n")
	fmt.Printf("======================\n\n")
	fmt.Printf("This example demonstrates automatic reconnection when the connection is lost.\n")
	fmt.Printf("Try stopping/restarting your MQTT server while this is running!\n\n")

	connectionCount := 0
	disconnectionCount := 0

	opts := []mq.Option{
		mq.WithClientID("auto-reconnect-example"),
		mq.WithAutoReconnect(true),
		mq.WithKeepAlive(10 * time.Second),
		mq.WithOnConnect(func(c *mq.Client) {
			connectionCount++
			fmt.Printf("\n✅ Connected (connection #%d) at %s\n", connectionCount, time.Now().Format("15:04:05"))

			// Resubscribe on each connection
			fmt.Println("   Subscribing to 'test/reconnect'...")
			token := c.Subscribe("test/reconnect", mq.AtLeastOnce, func(c *mq.Client, msg mq.Message) {
				fmt.Printf("   📨 Received: %s\n", string(msg.Payload))
			})
			if err := token.Wait(context.Background()); err != nil {
				log.Printf("   ⚠️  Subscribe failed: %v\n", err)
			} else {
				fmt.Println("   ✓ Subscribed")
			}
		}),
		mq.WithOnConnectionLost(func(c *mq.Client, err error) {
			disconnectionCount++
			fmt.Printf("\n❌ Connection lost (disconnection #%d) at %s: %v\n", disconnectionCount, time.Now().Format("15:04:05"), err)
			fmt.Println("   Will attempt to reconnect automatically...")
		}),
	}

	if username != "" {
		opts = append(opts, mq.WithCredentials(username, password))
	}

	client, err := mq.Dial(server, opts...)
	if err != nil {
		log.Fatalf("Failed to connect: %v", err)
	}
	defer client.Disconnect(context.Background())

	// Publish messages periodically
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	messageCount := 0

	// Handle Ctrl+C gracefully
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	fmt.Println("\nPublishing messages every 5 seconds...")
	fmt.Println("Press Ctrl+C to exit\n")

	for {
		select {
		case <-ticker.C:
			messageCount++
			payload := fmt.Sprintf("Message #%d at %s", messageCount, time.Now().Format("15:04:05"))

			token := client.Publish("test/reconnect", []byte(payload), mq.WithQoS(mq.AtLeastOnce))
			if err := token.Wait(context.Background()); err != nil {
				fmt.Printf("⚠️  Publish failed: %v (will retry on reconnect)\n", err)
			} else {
				fmt.Printf("📤 Published: %s\n", payload)
			}

		case <-sigChan:
			fmt.Println("\n\n📊 Statistics:")
			fmt.Printf("   Total connections: %d\n", connectionCount)
			fmt.Printf("   Total disconnections: %d\n", disconnectionCount)
			fmt.Printf("   Messages published: %d\n", messageCount)
			fmt.Println("\nDisconnecting gracefully...")
			return
		}
	}
}
