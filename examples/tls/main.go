//go:build ignore_test

package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"log"
	"log/slog"
	"os"
	"time"

	"github.com/gonzalop/mq"
)

func main() {
	topic := "test/tls" // Change to the right topic if you're testing on AWS IoT test page.
	// Get server address from command line or use default
	server := "tls://localhost:8883"
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

	fmt.Printf("Connecting to MQTT server at %s (TLS)...\n", server)
	if username != "" {
		fmt.Printf("Using credentials: username=%s\n", username)
	}

	// Change the log level here if you wish
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))

	opts := []mq.Option{
		mq.WithClientID("basicPubSub"),
		mq.WithKeepAlive(60 * time.Second),
		mq.WithLogger(logger),
	}
	if username != "" {
		opts = append(opts, mq.WithCredentials(username, password))
	}

	// BEGIN - Real Cert
	// // Load the certificate and private key from files
	// cert, err := tls.LoadX509KeyPair("private/<CHANGEME>.cert.pem", "private/<CHANGEME>.private.key")
	// if err != nil {
	// 	fmt.Printf("Error loading key pair: %v\n", err)
	// 	return
	// }
	// opts = append(opts, mq.WithTLS(&tls.Config{
	//		Certificates: []tls.Certificate{cert},
	// }))
	//
	// END - Real Cert

	// BEGIN - Unsafe for Testing Only
	opts = append(opts, mq.WithTLS(&tls.Config{
		InsecureSkipVerify: true, // WARNING: invalidates TLS security. For testing locally ONLY. Do NOT use in production.
	}))
	// END - Unsafe for Testing Only

	// Connect to server
	client, err := mq.Dial(server, opts...)
	if err != nil {
		log.Fatalf("Failed to connect: %v", err)
	}
	defer client.Disconnect(context.Background())

	fmt.Println("✓ Connected successfully via TLS!")

	// Subscribe to test topic
	fmt.Printf("\nSubscribing to '%s'...\n", topic)
	received := make(chan mq.Message, 10)

	token := client.Subscribe(topic, 1, func(c *mq.Client, msg mq.Message) {
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

	// Publish test message
	fmt.Println("\nPublishing secure message...")
	pubToken := client.Publish(topic, []byte("Secure Hello via TLS!"), mq.WithQoS(1))
	if err := pubToken.Wait(context.Background()); err != nil {
		log.Printf("Publish failed: %v", err)
	} else {
		fmt.Println("✓ Message published and acknowledged")
	}

	// Wait for message
	fmt.Println("\nWaiting for message...")
	select {
	case msg := <-received:
		fmt.Printf("✓ Received: %s\n", string(msg.Payload))
	case <-time.After(5 * time.Second):
		fmt.Println("⚠ Timeout waiting for message")
	}

	fmt.Println("\nTLS test completed! 🔒")
}
