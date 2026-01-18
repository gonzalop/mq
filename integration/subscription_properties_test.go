package mq_test

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gonzalop/mq"
)

func TestSubscriptionProperties_Integration(t *testing.T) {
	t.Parallel()
	server, cleanup := startMosquitto(t, "")
	defer cleanup()

	// Connect with MQTT v5.0
	client, err := mq.Dial(server,
		mq.WithClientID("test-sub-properties"),
		mq.WithProtocolVersion(mq.ProtocolV50),
	)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer client.Disconnect(context.Background())

	topic := "test/sub/properties"
	received := make(chan mq.Message, 1)

	// Subscribe with Subscription Identifier and User Properties
	subID := 42

token := client.Subscribe(topic, mq.AtLeastOnce, func(c *mq.Client, msg mq.Message) {
		received <- msg
	}, 
		mq.WithSubscriptionIdentifier(subID),
		mq.WithSubscribeUserProperty("test-key", "test-value"),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := token.Wait(ctx); err != nil {
		t.Fatalf("Subscribe failed: %v", err)
	}

	// Publish a message to the topic
	pubToken := client.Publish(topic, []byte("hello"), mq.WithQoS(1))
	if err := pubToken.Wait(ctx); err != nil {
		t.Fatalf("Publish failed: %v", err)
	}

	// Verify that we received the message with the Subscription Identifier
	select {
	case msg := <-received:
		if msg.Properties == nil {
			t.Fatal("Properties in received message is nil")
		}
		
		foundID := false
		for _, id := range msg.Properties.SubscriptionIdentifier {
			if id == subID {
				foundID = true
				break
			}
		}
		
		if !foundID {
			t.Errorf("Subscription Identifier %d not found in received message, got %v", subID, msg.Properties.SubscriptionIdentifier)
		}
		
	case <-time.After(2 * time.Second):
		t.Fatal("Timeout waiting for message")
	}
}

func TestSubscriptionProperties_Persistence(t *testing.T) {
	t.Parallel()
	
	// Start server (Isolated)
	server, cleanup := startMosquitto(t, "persistence false\n# Dedicated for sub-props persistence test")
	
	// Extract port to reuse on restart
	parts := strings.Split(server, ":")
	port := parts[len(parts)-1]

	// Setup temp dir for store
	tmpDir, err := os.MkdirTemp("", "mq-test-subprops-persistence-*")
	if err != nil {
		cleanup()
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	clientID := "client-subprops-" + strings.ReplaceAll(t.Name(), "/", "_")
	topic := "test/subprops/persistence/" + t.Name()
	subID := 1234

	// 1. Initial Connection & Subscription with Properties
	store1, err := mq.NewFileStore(tmpDir, clientID)
	if err != nil {
		t.Fatalf("NewFileStore 1 failed: %v", err)
	}
	client1, err := mq.Dial(server,
		mq.WithClientID(clientID),
		mq.WithCleanSession(false),
		mq.WithSessionStore(store1),
		mq.WithProtocolVersion(mq.ProtocolV50),
	)
	if err != nil {
		cleanup()
		t.Fatalf("Connect 1 failed: %v", err)
	}

	// Subscribe with ID and User Properties
	subToken := client1.Subscribe(topic, 1, nil, 
		mq.WithSubscriptionIdentifier(subID),
		mq.WithSubscribeUserProperty("persist-key", "persist-value"),
	)
	if err := subToken.Wait(context.Background()); err != nil {
		cleanup()
		t.Fatalf("Subscribe failed: %v", err)
	}

	client1.Disconnect(context.Background())

	// 2. Restart Server (Simulate Loss of State)
	cleanup() // Kills the container

	server2, cleanup2 := startMosquitto(t, "persistence false\n# Dedicated for sub-props persistence restart", port)
	defer cleanup2()

	// 3. Client Restart (New instance, same store)
	received := make(chan mq.Message, 1)
	store2, err := mq.NewFileStore(tmpDir, clientID)
	if err != nil {
		t.Fatalf("NewFileStore 2 failed: %v", err)
	}

	client2, err := mq.Dial(server2,
		mq.WithClientID(clientID),
		mq.WithCleanSession(false),
		mq.WithSessionStore(store2),
		mq.WithProtocolVersion(mq.ProtocolV50),
		mq.WithDefaultPublishHandler(func(c *mq.Client, msg mq.Message) {
			received <- msg
		}),
	)
	if err != nil {
		t.Fatalf("Connect 2 failed: %v", err)
	}
	defer client2.Disconnect(context.Background())

	// 4. Verify Subscription Restoration (including Properties)
	// We verify restoration by checking if the Subscription ID is present in received messages.
	// If the client restored the subscription but lost the ID, the received message wouldn't have it.

	// Publish from another connection
	pubClient, _ := mq.Dial(server2, mq.WithClientID("publisher-"+t.Name()))
	if err := pubClient.Publish(topic, []byte("msg-persisted"), mq.WithQoS(1)).Wait(context.Background()); err != nil {
		t.Fatalf("Publish failed: %v", err)
	}
	pubClient.Disconnect(context.Background())

	// Verify message reception with ID
	select {
	case msg := <-received:
		if msg.Properties == nil {
			t.Fatal("Properties in received message is nil after restoration")
		}
		
		foundID := false
		for _, id := range msg.Properties.SubscriptionIdentifier {
			if id == subID {
				foundID = true
				break
			}
		}
		
		if !foundID {
			t.Errorf("Subscription Identifier %d not restored from disk! Got %v", subID, msg.Properties.SubscriptionIdentifier)
		}
		
	case <-time.After(5 * time.Second):
		t.Fatal("Timeout waiting for message after restoration")
	}
}