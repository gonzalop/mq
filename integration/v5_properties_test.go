package mq_test

import (
	"context"
	"testing"
	"time"

	"github.com/gonzalop/mq"
)

func TestV5Properties(t *testing.T) {
	t.Parallel()
	server, cleanup := startMosquitto(t, "")
	defer cleanup()

	// Connect with MQTT v5.0
	client, err := mq.Dial(server,
		mq.WithClientID("test-v5-properties"),
		mq.WithProtocolVersion(mq.ProtocolV50),
	)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer client.Disconnect(context.Background())

	// Test 1: Publish and receive with ContentType
	t.Run("ContentType", func(t *testing.T) {
		received := make(chan mq.Message, 1)

		token := client.Subscribe("test/properties/contenttype", 1, func(c *mq.Client, msg mq.Message) {
			received <- msg
		})

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if err := token.Wait(ctx); err != nil {
			t.Fatalf("Failed to subscribe: %v", err)
		}

		// Publish with content type
		err := client.Publish("test/properties/contenttype",
			[]byte(`{"value": 42}`),
			mq.WithQoS(1),
			mq.WithContentType("application/json"),
		).Wait(ctx)

		if err != nil {
			t.Fatalf("Failed to publish: %v", err)
		}

		// Wait for message
		select {
		case msg := <-received:
			if msg.Properties == nil {
				t.Fatal("Properties is nil")
			}
			if msg.Properties.ContentType != "application/json" {
				t.Errorf("ContentType = %v, want application/json", msg.Properties.ContentType)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("Timeout waiting for message")
		}
	})

	// Test 2: User Properties
	t.Run("UserProperties", func(t *testing.T) {
		received := make(chan mq.Message, 1)

		token := client.Subscribe("test/properties/user", 1, func(c *mq.Client, msg mq.Message) {
			received <- msg
		})

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if err := token.Wait(ctx); err != nil {
			t.Fatalf("Failed to subscribe: %v", err)
		}

		// Publish with user properties
		err := client.Publish("test/properties/user",
			[]byte("test"),
			mq.WithQoS(1),
			mq.WithUserProperty("key1", "value1"),
			mq.WithUserProperty("key2", "value2"),
		).Wait(ctx)

		if err != nil {
			t.Fatalf("Failed to publish: %v", err)
		}

		// Wait for message
		select {
		case msg := <-received:
			if msg.Properties == nil {
				t.Fatal("Properties is nil")
			}
			if len(msg.Properties.UserProperties) != 2 {
				t.Errorf("UserProperties length = %d, want 2", len(msg.Properties.UserProperties))
			}
			if msg.Properties.UserProperties["key1"] != "value1" {
				t.Errorf("UserProperties[key1] = %v, want value1", msg.Properties.UserProperties["key1"])
			}
			if msg.Properties.UserProperties["key2"] != "value2" {
				t.Errorf("UserProperties[key2] = %v, want value2", msg.Properties.UserProperties["key2"])
			}
		case <-time.After(5 * time.Second):
			t.Fatal("Timeout waiting for message")
		}
	})

	// Test 3: Request-Response Pattern
	t.Run("RequestResponse", func(t *testing.T) {
		responseTopic := "test/responses"
		responses := make(chan mq.Message, 1)

		// Subscribe to response topic
		token := client.Subscribe(responseTopic, 1, func(c *mq.Client, msg mq.Message) {
			responses <- msg
		})

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if err := token.Wait(ctx); err != nil {
			t.Fatalf("Failed to subscribe to responses: %v", err)
		}

		// Subscribe to request topic and auto-respond
		requestTopic := "test/requests"
		token = client.Subscribe(requestTopic, 1, func(c *mq.Client, msg mq.Message) {
			if msg.Properties == nil || msg.Properties.ResponseTopic == "" {
				return
			}

			// Send response
			c.Publish(msg.Properties.ResponseTopic,
				[]byte("response-data"),
				mq.WithQoS(1),
				mq.WithCorrelationData(msg.Properties.CorrelationData),
			)
		})

		if err := token.Wait(ctx); err != nil {
			t.Fatalf("Failed to subscribe to requests: %v", err)
		}

		// Send request
		correlationData := []byte("req-123")
		err := client.Publish(requestTopic,
			[]byte("request-data"),
			mq.WithQoS(1),
			mq.WithResponseTopic(responseTopic),
			mq.WithCorrelationData(correlationData),
		).Wait(ctx)

		if err != nil {
			t.Fatalf("Failed to publish request: %v", err)
		}

		// Wait for response
		select {
		case msg := <-responses:
			if msg.Properties == nil {
				t.Fatal("Response properties is nil")
			}
			if string(msg.Properties.CorrelationData) != string(correlationData) {
				t.Errorf("CorrelationData = %v, want %v", msg.Properties.CorrelationData, correlationData)
			}
			if string(msg.Payload) != "response-data" {
				t.Errorf("Payload = %v, want response-data", string(msg.Payload))
			}
		case <-time.After(5 * time.Second):
			t.Fatal("Timeout waiting for response")
		}
	})

	// Test 4: Multiple Properties
	t.Run("MultipleProperties", func(t *testing.T) {
		received := make(chan mq.Message, 1)

		token := client.Subscribe("test/properties/multiple", 1, func(c *mq.Client, msg mq.Message) {
			received <- msg
		})

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if err := token.Wait(ctx); err != nil {
			t.Fatalf("Failed to subscribe: %v", err)
		}

		// Publish with multiple properties
		err := client.Publish("test/properties/multiple",
			[]byte("test data"),
			mq.WithQoS(1),
			mq.WithContentType("text/plain"),
			mq.WithUserProperty("source", "test"),
			mq.WithUserProperty("version", "1.0"),
			mq.WithMessageExpiry(300),
			mq.WithPayloadFormat(1),
		).Wait(ctx)

		if err != nil {
			t.Fatalf("Failed to publish: %v", err)
		}

		// Wait for message
		select {
		case msg := <-received:
			if msg.Properties == nil {
				t.Fatal("Properties is nil")
			}
			if msg.Properties.ContentType != "text/plain" {
				t.Errorf("ContentType = %v, want text/plain", msg.Properties.ContentType)
			}
			if len(msg.Properties.UserProperties) != 2 {
				t.Errorf("UserProperties length = %d, want 2", len(msg.Properties.UserProperties))
			}
			if msg.Properties.MessageExpiry == nil {
				t.Error("MessageExpiry is nil")
			} else if *msg.Properties.MessageExpiry == 0 {
				// Note: Mosquitto may adjust the expiry based on time in transit
				t.Logf("MessageExpiry = %d (adjusted by server)", *msg.Properties.MessageExpiry)
			}
			if msg.Properties.PayloadFormat == nil {
				t.Error("PayloadFormat is nil")
			} else if *msg.Properties.PayloadFormat != 1 {
				t.Errorf("PayloadFormat = %d, want 1", *msg.Properties.PayloadFormat)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("Timeout waiting for message")
		}
	})

	// Test 5: Properties struct
	t.Run("PropertiesStruct", func(t *testing.T) {
		received := make(chan mq.Message, 1)

		token := client.Subscribe("test/properties/struct", 1, func(c *mq.Client, msg mq.Message) {
			received <- msg
		})

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if err := token.Wait(ctx); err != nil {
			t.Fatalf("Failed to subscribe: %v", err)
		}

		// Create properties struct
		props := mq.NewProperties()
		props.ContentType = "application/json"
		props.SetUserProperty("test", "value")
		expiry := uint32(600)
		props.MessageExpiry = &expiry

		// Publish with properties struct
		err := client.Publish("test/properties/struct",
			[]byte(`{"test": true}`),
			mq.WithQoS(1),
			mq.WithProperties(props),
		).Wait(ctx)

		if err != nil {
			t.Fatalf("Failed to publish: %v", err)
		}

		// Wait for message
		select {
		case msg := <-received:
			if msg.Properties == nil {
				t.Fatal("Properties is nil")
			}
			if msg.Properties.ContentType != "application/json" {
				t.Errorf("ContentType = %v, want application/json", msg.Properties.ContentType)
			}
			if msg.Properties.GetUserProperty("test") != "value" {
				t.Errorf("UserProperty[test] = %v, want value", msg.Properties.GetUserProperty("test"))
			}
		case <-time.After(5 * time.Second):
			t.Fatal("Timeout waiting for message")
		}
	})
}

func TestV3NoProperties(t *testing.T) {
	t.Parallel()
	server, cleanup := startMosquitto(t, "")
	defer cleanup()

	// Connect with MQTT v3.1.1
	client, err := mq.Dial(server,
		mq.WithClientID("test-v3-no-properties"),
		mq.WithProtocolVersion(mq.ProtocolV311),
	)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer client.Disconnect(context.Background())

	received := make(chan mq.Message, 1)

	token := client.Subscribe("test/v3/noprops", 1, func(c *mq.Client, msg mq.Message) {
		received <- msg
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := token.Wait(ctx); err != nil {
		t.Fatalf("Failed to subscribe: %v", err)
	}

	// Publish with properties (should be ignored for v3.1.1)
	err = client.Publish("test/v3/noprops",
		[]byte("test"),
		mq.WithQoS(1),
		mq.WithContentType("text/plain"),
		mq.WithUserProperty("key", "value"),
	).Wait(ctx)

	if err != nil {
		t.Fatalf("Failed to publish: %v", err)
	}

	// Wait for message
	select {
	case msg := <-received:
		// Properties should be nil for v3.1.1
		if msg.Properties != nil {
			t.Errorf("Properties = %v, want nil for v3.1.1", msg.Properties)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Timeout waiting for message")
	}
}
