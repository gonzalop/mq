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

	// Build connection options
	opts := []mq.Option{
		mq.WithClientID("v5-properties-example"),
	}
	if username != "" {
		opts = append(opts, mq.WithCredentials(username, password))
	}

	// Connect with MQTT v5.0
	client, err := mq.Dial(server, opts...)
	if err != nil {
		log.Fatalf("Failed to connect: %v", err)
	}
	defer client.Disconnect(context.Background())

	fmt.Println("✅ Connected to server with MQTT v5.0")

	// Example 1: Publish with Content Type
	fmt.Println("\n📤 Example 1: Publishing with Content Type")
	err = client.Publish("sensors/temperature",
		[]byte(`{"value": 22.5, "unit": "celsius"}`),
		mq.WithQoS(1),
		mq.WithContentType("application/json"),
	).Wait(context.Background())
	if err != nil {
		log.Fatalf("Failed to publish: %v", err)
	}
	fmt.Println("   Published JSON data with content type")

	// Example 2: Publish with User Properties
	fmt.Println("\n📤 Example 2: Publishing with User Properties")
	err = client.Publish("sensors/humidity",
		[]byte("65.2"),
		mq.WithQoS(1),
		mq.WithUserProperty("sensor-id", "hum-01"),
		mq.WithUserProperty("location", "warehouse-a"),
		mq.WithUserProperty("firmware", "v2.1.0"),
	).Wait(context.Background())
	if err != nil {
		log.Fatalf("Failed to publish: %v", err)
	}
	fmt.Println("   Published with custom metadata")

	// Example 3: Publish with Message Expiry
	fmt.Println("\n📤 Example 3: Publishing with Message Expiry")
	err = client.Publish("events/alert",
		[]byte("Temperature threshold exceeded"),
		mq.WithQoS(1),
		mq.WithMessageExpiry(60), // Expires in 60 seconds
		mq.WithContentType("text/plain"),
	).Wait(context.Background())
	if err != nil {
		log.Fatalf("Failed to publish: %v", err)
	}
	fmt.Println("   Published alert with 60s expiry")

	// Example 4: Subscribe and receive properties
	fmt.Println("\n📥 Example 4: Subscribing to receive properties")
	received := make(chan bool, 1)

	token := client.Subscribe("sensors/#", 1, func(c *mq.Client, msg mq.Message) {
		fmt.Printf("\n   📨 Received message:\n")
		fmt.Printf("      Topic: %s\n", msg.Topic)
		fmt.Printf("      Payload: %s\n", string(msg.Payload))
		fmt.Printf("      QoS: %d\n", msg.QoS)

		if msg.Properties != nil {
			fmt.Printf("      Properties:\n")
			if msg.Properties.ContentType != "" {
				fmt.Printf("        - Content-Type: %s\n", msg.Properties.ContentType)
			}
			if msg.Properties.MessageExpiry != nil {
				fmt.Printf("        - Message Expiry: %d seconds\n", *msg.Properties.MessageExpiry)
			}
			if len(msg.Properties.UserProperties) > 0 {
				fmt.Printf("        - User Properties:\n")
				for key, value := range msg.Properties.UserProperties {
					fmt.Printf("          * %s: %s\n", key, value)
				}
			}
		} else {
			fmt.Printf("      Properties: none\n")
		}

		received <- true
	})

	if err := token.Wait(context.Background()); err != nil {
		log.Fatalf("Failed to subscribe: %v", err)
	}
	fmt.Println("   Subscribed to sensors/#")

	// Publish a test message
	fmt.Println("\n📤 Publishing test message...")
	err = client.Publish("sensors/test",
		[]byte("test data"),
		mq.WithQoS(1),
		mq.WithContentType("text/plain"),
		mq.WithUserProperty("test", "true"),
	).Wait(context.Background())
	if err != nil {
		log.Fatalf("Failed to publish: %v", err)
	}

	// Wait for message
	select {
	case <-received:
		fmt.Println("\n✅ Successfully received message with properties!")
	case <-time.After(5 * time.Second):
		fmt.Println("\n⚠️  Timeout waiting for message")
	}

	// Example 5: Using Properties struct
	fmt.Println("\n📤 Example 5: Using Properties struct")
	props := mq.NewProperties()
	props.ContentType = "application/json"
	props.SetUserProperty("version", "1.0")
	props.SetUserProperty("schema", "sensor-v1")
	expiry := uint32(300)
	props.MessageExpiry = &expiry

	err = client.Publish("data/structured",
		[]byte(`{"sensor": "temp-01", "value": 23.1}`),
		mq.WithQoS(1),
		mq.WithProperties(props),
	).Wait(context.Background())
	if err != nil {
		log.Fatalf("Failed to publish: %v", err)
	}
	fmt.Println("   Published with Properties struct")

	fmt.Println("\n✅ All examples completed successfully!")
}
