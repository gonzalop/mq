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
		mq.WithClientID("v5-request-response-example"),
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

	// Set up response handler
	responseTopic := "responses/temperature"
	responses := make(map[string]chan string)

	token := client.Subscribe(responseTopic, 1, func(c *mq.Client, msg mq.Message) {
		if msg.Properties != nil && len(msg.Properties.CorrelationData) > 0 {
			correlationID := string(msg.Properties.CorrelationData)
			fmt.Printf("📨 Received response for request %s\n", correlationID)

			if ch, ok := responses[correlationID]; ok {
				ch <- string(msg.Payload)
			}
		}
	})

	if err := token.Wait(context.Background()); err != nil {
		log.Fatalf("Failed to subscribe: %v", err)
	}
	fmt.Printf("📥 Subscribed to response topic: %s\n\n", responseTopic)

	// Set up request handler (simulates a server)
	requestTopic := "requests/temperature"
	token = client.Subscribe(requestTopic, 1, func(c *mq.Client, msg mq.Message) {
		fmt.Printf("📨 Received request on %s\n", msg.Topic)

		// Extract request properties
		var replyTopic string
		var correlationData []byte

		if msg.Properties != nil {
			replyTopic = msg.Properties.ResponseTopic
			correlationData = msg.Properties.CorrelationData
		}

		if replyTopic == "" {
			fmt.Println("   ⚠️  No response topic specified, ignoring request")
			return
		}

		// Simulate processing
		fmt.Printf("   Processing request: %s\n", string(msg.Payload))
		time.Sleep(100 * time.Millisecond)

		// Send response
		response := `{"temperature": 22.5, "unit": "celsius", "timestamp": "2024-01-07T16:00:00Z"}`
		err := c.Publish(replyTopic,
			[]byte(response),
			mq.WithQoS(1),
			mq.WithContentType("application/json"),
			mq.WithCorrelationData(correlationData),
		).Wait(context.Background())

		if err != nil {
			fmt.Printf("   ❌ Failed to send response: %v\n", err)
		} else {
			fmt.Printf("   ✅ Sent response to %s\n", replyTopic)
		}
	})

	if err := token.Wait(context.Background()); err != nil {
		log.Fatalf("Failed to subscribe: %v", err)
	}
	fmt.Printf("📥 Subscribed to request topic: %s\n\n", requestTopic)

	// Send a request-response
	fmt.Println("📤 Sending request-response pattern example")
	correlationID := fmt.Sprintf("req-%d", time.Now().Unix())
	responseChan := make(chan string, 1)
	responses[correlationID] = responseChan

	err = client.Publish(requestTopic,
		[]byte("get-temperature"),
		mq.WithQoS(1),
		mq.WithResponseTopic(responseTopic),
		mq.WithCorrelationData([]byte(correlationID)),
		mq.WithContentType("text/plain"),
	).Wait(context.Background())

	if err != nil {
		log.Fatalf("Failed to publish request: %v", err)
	}
	fmt.Printf("   📤 Sent request with correlation ID: %s\n", correlationID)

	// Wait for response
	select {
	case response := <-responseChan:
		fmt.Printf("   ✅ Received response: %s\n", response)
	case <-time.After(5 * time.Second):
		fmt.Println("   ⚠️  Timeout waiting for response")
	}

	// Send multiple requests
	fmt.Println("\n📤 Sending multiple concurrent requests")
	for i := 1; i <= 3; i++ {
		correlationID := fmt.Sprintf("req-batch-%d", i)
		responseChan := make(chan string, 1)
		responses[correlationID] = responseChan

		err = client.Publish(requestTopic,
			[]byte(fmt.Sprintf("get-temperature-sensor-%d", i)),
			mq.WithQoS(1),
			mq.WithResponseTopic(responseTopic),
			mq.WithCorrelationData([]byte(correlationID)),
		).Wait(context.Background())

		if err != nil {
			log.Printf("Failed to publish request %d: %v", i, err)
			continue
		}
		fmt.Printf("   📤 Sent request %d\n", i)

		// Wait for response in background
		go func(id string, ch chan string) {
			select {
			case response := <-ch:
				fmt.Printf("   ✅ Response for %s: %s\n", id, response)
			case <-time.After(5 * time.Second):
				fmt.Printf("   ⚠️  Timeout for %s\n", id)
			}
		}(correlationID, responseChan)
	}

	// Wait for all responses
	time.Sleep(2 * time.Second)

	fmt.Println("\n✅ Request-response pattern example completed!")
}
