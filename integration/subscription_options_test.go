package mq_test

import (
	"context"
	"testing"
	"time"

	"github.com/gonzalop/mq"
)

func TestSubscriptionOptions_NoLocal(t *testing.T) {
	t.Parallel()
	// Verify that WithNoLocal(true) prevents receiving own messages

	server, cleanup := startMosquitto(t, "")
	defer cleanup()

	opts := []mq.Option{
		mq.WithClientID("client-no-local"),
		mq.WithCleanSession(true),
		mq.WithProtocolVersion(mq.ProtocolV50),
	}

	client, err := mq.Dial(server, opts...)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer client.Disconnect(context.Background())

	topic := "test/nolocal"
	received := make(chan string, 10)

	// Subscribe with NoLocal = true
	token := client.Subscribe(topic, mq.AtLeastOnce, func(c *mq.Client, msg mq.Message) {
		received <- string(msg.Payload)
	}, mq.WithNoLocal(true))

	if err := token.Wait(context.Background()); err != nil {
		t.Fatalf("Subscribe failed: %v", err)
	}

	// Publish to the topic
	pubToken := client.Publish(topic, []byte("own-message"), mq.WithQoS(1))
	if err := pubToken.Wait(context.Background()); err != nil {
		t.Fatalf("Publish failed: %v", err)
	}

	// Wait for potential delivery
	select {
	case msg := <-received:
		t.Fatalf("Received own message '%s' despite NoLocal=true", msg)
	case <-time.After(500 * time.Millisecond):
		// Success! No message received.
	}

	// Control Test: Subscribe to another topic WITHOUT NoLocal
	topicControl := "test/local"
	tokenControl := client.Subscribe(topicControl, mq.AtLeastOnce, func(c *mq.Client, msg mq.Message) {
		received <- string(msg.Payload)
	}, mq.WithNoLocal(false))

	if err := tokenControl.Wait(context.Background()); err != nil {
		t.Fatalf("Control Subscribe failed: %v", err)
	}

	client.Publish(topicControl, []byte("should-receive"), mq.WithQoS(1)).Wait(context.Background())

	select {
	case msg := <-received:
		if msg != "should-receive" {
			t.Fatalf("Received unexpected message: %s", msg)
		}
	case <-time.After(1 * time.Second):
		t.Fatalf("Did not receive control message (NoLocal=false broken?)")
	}
}
