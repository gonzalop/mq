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
	// 1. Get credentials from environment or defaults
	username := os.Getenv("MQTT_USERNAME")
	if username == "" {
		username = "user"
	}
	password := os.Getenv("MQTT_PASSWORD")
	if password == "" {
		password = "password123"
	}

	// 2. Configure the SCRAM authenticator
	// The implementation is in scram_authenticator.go
	auth := &ScramAuthenticator{
		username: username,
		password: password,
	}

	// 2. Configure options
	opts := []mq.Option{
		mq.WithAuthenticator(auth),
		mq.WithClientID("scram-client-example"),
		mq.WithCleanSession(true),
	}

	// 3. Connect (Assuming server supports SCRAM-SHA-256)
	// Note: Most public servers don't support SCRAM by default.
	// This example assumes a server configured for SCRAM.
	fmt.Println("Connecting with SCRAM-SHA-256...")
	client, err := mq.Dial("tcp://localhost:1883", opts...)
	if err != nil {
		log.Printf("Connection failed: %v", err)
		log.Println("(Note: Ensure your server is configured for SCRAM-SHA-256)")
		return
	}
	defer client.Disconnect(context.Background())

	fmt.Println("✅ Connected successfully using SCRAM-SHA-256! (Ctrl+C to exit)")

	// 4. Wait for signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan
}
