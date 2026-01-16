//go:build ignore_test

package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"sync"
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

	fmt.Printf("Wildcard Subscriptions Example\n")
	fmt.Printf("===============================\n\n")
	fmt.Printf("This example demonstrates MQTT wildcard subscriptions:\n")
	fmt.Printf("  + (plus)  = single-level wildcard\n")
	fmt.Printf("  # (hash)  = multi-level wildcard\n\n")

	opts := []mq.Option{
		mq.WithClientID("wildcard-example"),
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

	fmt.Println("✓ Connected successfully!\n")

	var mu sync.Mutex
	messagesByFilter := make(map[string][]string)

	// Subscribe to various wildcard patterns
	subscriptions := []struct {
		filter      string
		description string
	}{
		{"sensors/+/temperature", "Single-level wildcard: matches any sensor's temperature"},
		{"sensors/bedroom/#", "Multi-level wildcard: matches all bedroom sensor topics"},
		{"home/+/+/status", "Multiple single-level: matches home/{room}/{device}/status"},
		{"#", "Match everything (use with caution!)"},
	}

	for _, sub := range subscriptions {
		fmt.Printf("📡 Subscribing to: %s\n", sub.filter)
		fmt.Printf("   %s\n", sub.description)

		filter := sub.filter // Capture for closure
		token := client.Subscribe(filter, mq.AtLeastOnce, func(c *mq.Client, msg mq.Message) {
			mu.Lock()
			messagesByFilter[filter] = append(messagesByFilter[filter], msg.Topic)
			mu.Unlock()
			fmt.Printf("   📨 [%s] received from: %s\n", filter, msg.Topic)
		})

		if err := token.Wait(context.Background()); err != nil {
			log.Fatalf("Failed to subscribe to %s: %v", sub.filter, err)
		}
	}

	fmt.Println("\n✓ All subscriptions active!\n")

	// Publish test messages to various topics
	testMessages := []struct {
		topic   string
		payload string
	}{
		{"sensors/living-room/temperature", "22.5°C"},
		{"sensors/bedroom/temperature", "20.1°C"},
		{"sensors/bedroom/humidity", "65%"},
		{"sensors/bedroom/light/brightness", "80%"},
		{"home/living-room/tv/status", "on"},
		{"home/bedroom/lamp/status", "off"},
		{"system/status", "ok"},
	}

	fmt.Println("📤 Publishing test messages...\n")
	for _, msg := range testMessages {
		fmt.Printf("Publishing to: %s\n", msg.topic)
		token := client.Publish(msg.topic, []byte(msg.payload), mq.WithQoS(mq.AtLeastOnce))
		if err := token.Wait(context.Background()); err != nil {
			log.Printf("Failed to publish to %s: %v", msg.topic, err)
		}
		time.Sleep(100 * time.Millisecond) // Small delay for readability
	}

	// Wait for messages to be received
	time.Sleep(1 * time.Second)

	// Print summary
	fmt.Println("\n" + repeat("=", 60))
	fmt.Println("📊 Summary of Messages Received by Each Filter:")
	fmt.Println(repeat("=", 60))

	mu.Lock()
	for _, sub := range subscriptions {
		topics := messagesByFilter[sub.filter]
		fmt.Printf("\n%s (%d messages):\n", sub.filter, len(topics))
		if len(topics) == 0 {
			fmt.Println("  (no messages)")
		} else {
			for _, topic := range topics {
				fmt.Printf("  ✓ %s\n", topic)
			}
		}
	}
	mu.Unlock()

	fmt.Println("\n" + repeat("=", 60))
	fmt.Println("\n💡 Key Observations:")
	fmt.Println("  • 'sensors/+/temperature' matched both living-room and bedroom")
	fmt.Println("  • 'sensors/bedroom/#' matched all bedroom sensor topics")
	fmt.Println("  • 'home/+/+/status' matched room/device/status pattern")
	fmt.Println("  • '#' matched ALL published topics")

	// Keep running until Ctrl+C
	fmt.Println("\nPress Ctrl+C to exit")
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	<-sigChan

	fmt.Println("\nDisconnecting...")
}

// Helper function to repeat a string
func repeat(s string, count int) string {
	result := ""
	for i := 0; i < count; i++ {
		result += s
	}
	return result
}
