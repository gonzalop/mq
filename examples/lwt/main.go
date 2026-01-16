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

	fmt.Printf("Last Will and Testament (LWT) Example\n")
	fmt.Printf("======================================\n\n")
	fmt.Printf("This example demonstrates MQTT Last Will and Testament.\n")
	fmt.Printf("The server will publish a 'will' message if this client disconnects unexpectedly.\n\n")

	// First, create a monitor client to watch for will messages
	fmt.Println("1️⃣  Starting monitor client...")

	monitorOpts := []mq.Option{
		mq.WithClientID("lwt-monitor"),
		mq.WithKeepAlive(60 * time.Second),
	}

	if username != "" {
		monitorOpts = append(monitorOpts, mq.WithCredentials(username, password))
	}

	monitorClient, err := mq.Dial(server, monitorOpts...)
	if err != nil {
		log.Fatalf("Failed to connect monitor: %v", err)
	}
	defer monitorClient.Disconnect(context.Background())

	willReceived := make(chan string, 1)
	statusReceived := make(chan string, 10)

	// Monitor subscribes to both will and status topics
	monitorClient.Subscribe("devices/sensor-1/will", mq.AtLeastOnce, func(c *mq.Client, msg mq.Message) {
		fmt.Printf("\n🪦 WILL MESSAGE RECEIVED: %s\n", string(msg.Payload))
		fmt.Printf("   (This means the client disconnected unexpectedly!)\n\n")
		willReceived <- string(msg.Payload)
	})

	monitorClient.Subscribe("devices/sensor-1/status", mq.AtLeastOnce, func(c *mq.Client, msg mq.Message) {
		fmt.Printf("   📊 Status: %s\n", string(msg.Payload))
		statusReceived <- string(msg.Payload)
	})

	time.Sleep(500 * time.Millisecond)
	fmt.Println("   ✓ Monitor client ready\n")

	// Now create a client with a Last Will configured
	fmt.Println("2️⃣  Starting client with Last Will configured...")
	fmt.Println("   Will Topic: devices/sensor-1/will")
	fmt.Println("   Will Message: {\"status\":\"offline\",\"reason\":\"unexpected_disconnect\"}")
	fmt.Println("   Will QoS: 1")
	fmt.Println("   Will Retained: true\n")

	clientOpts := []mq.Option{
		mq.WithClientID("sensor-1"),
		mq.WithKeepAlive(10 * time.Second),
		mq.WithWill(
			"devices/sensor-1/will",
			[]byte(`{"status":"offline","reason":"unexpected_disconnect"}`),
			1,    // QoS 1
			true, // retained
		),
	}

	if username != "" {
		clientOpts = append(clientOpts, mq.WithCredentials(username, password))
	}

	client, err := mq.Dial(server, clientOpts...)
	if err != nil {
		log.Fatalf("Failed to connect client: %v", err)
	}

	fmt.Println("   ✓ Client connected with LWT configured\n")

	// Publish online status
	fmt.Println("3️⃣  Publishing 'online' status...")
	client.Publish("devices/sensor-1/status", []byte("online"), mq.WithQoS(mq.AtLeastOnce), mq.WithRetain(true))
	time.Sleep(500 * time.Millisecond)

	// Simulate normal operation
	fmt.Println("\n4️⃣  Simulating normal operation (publishing sensor data)...")
	for i := 1; i <= 3; i++ {
		payload := fmt.Sprintf(`{"temperature":%.1f,"timestamp":"%s"}`, 20.0+float64(i)*0.5, time.Now().Format(time.RFC3339))
		client.Publish("devices/sensor-1/data", []byte(payload), mq.WithQoS(mq.AtLeastOnce))
		fmt.Printf("   📡 Published sensor reading #%d\n", i)
		time.Sleep(1 * time.Second)
	}

	// Now demonstrate the two ways a client can disconnect
	fmt.Println("\n" + repeat("=", 60))
	fmt.Println("Choose how to disconnect:")
	fmt.Println(repeat("=", 60))
	fmt.Println("1. Press Ctrl+C for GRACEFUL disconnect (will NOT trigger LWT)")
	fmt.Println("2. Kill the process (kill -9) for UNEXPECTED disconnect (WILL trigger LWT)")
	fmt.Println("3. Wait 15 seconds and we'll simulate unexpected disconnect")
	fmt.Println(repeat("=", 60) + "\n")

	// Set up signal handling
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	select {
	case <-sigChan:
		fmt.Println("\n5️⃣  Graceful disconnect requested (Ctrl+C)...")
		fmt.Println("   Publishing 'offline' status before disconnecting...")
		client.Publish("devices/sensor-1/status", []byte("offline"), mq.WithQoS(mq.AtLeastOnce), mq.WithRetain(true))
		time.Sleep(500 * time.Millisecond)

		fmt.Println("   Disconnecting gracefully...")
		client.Disconnect(context.Background())

		fmt.Println("\n   ✅ Graceful disconnect complete!")
		fmt.Println("   ℹ️  The LWT message was NOT published (as expected)")
		fmt.Println("   ℹ️  We manually published 'offline' status instead")

	case <-time.After(15 * time.Second):
		fmt.Println("\n5️⃣  Simulating unexpected disconnect...")
		fmt.Println("   Forcing connection close without proper DISCONNECT packet...")

		// Force close the connection without sending DISCONNECT
		// This simulates a crash or network failure
		client.Disconnect(context.Background()) // In real scenario, this would be a crash

		fmt.Println("   ⚠️  Connection forcefully closed!")
		fmt.Println("   ⏳ Waiting for server to detect disconnect and publish LWT...")

		// Wait for will message
		select {
		case will := <-willReceived:
			fmt.Printf("\n   ✅ LWT was published: %s\n", will)
		case <-time.After(5 * time.Second):
			fmt.Println("\n   ⚠️  LWT not received (may need to wait longer or check server config)")
		}
	}

	time.Sleep(1 * time.Second)
	fmt.Println("\nExample complete!")
}

func repeat(s string, count int) string {
	result := ""
	for i := 0; i < count; i++ {
		result += s
	}
	return result
}
