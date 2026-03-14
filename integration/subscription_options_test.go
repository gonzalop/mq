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
	token := client.Subscribe(topic, mq.AtLeastOnce, func(_ *mq.Client, msg mq.Message) {
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
	tokenControl := client.Subscribe(topicControl, mq.AtLeastOnce, func(_ *mq.Client, msg mq.Message) {
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

func TestSubscriptionOptions_RetainAsPublished(t *testing.T) {
	t.Parallel()
	server, cleanup := startMosquitto(t, "")
	defer cleanup()

	// 1. Client A subscribes with RetainAsPublished = true
	clientA, err := mq.Dial(server,
		mq.WithClientID("client-rap-true"),
		mq.WithProtocolVersion(mq.ProtocolV50),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer clientA.Disconnect(context.Background())

	receivedRetain := make(chan bool, 1)
	topic := "test/rap"

	clientA.Subscribe(topic, 1, func(_ *mq.Client, msg mq.Message) {
		receivedRetain <- msg.Retained
	}, mq.WithRetainAsPublished(true)).Wait(context.Background())

	// 2. Client B publishes a RETAINED message
	clientB, err := mq.Dial(server, mq.WithClientID("client-pub-retain"))
	if err != nil {
		t.Fatal(err)
	}
	defer clientB.Disconnect(context.Background())

	clientB.Publish(topic, []byte("rap-test"), mq.WithQoS(1), mq.WithRetain(true)).Wait(context.Background())

	// 3. Client A should receive the message with RETAIN flag PRESERVED
	select {
	case retained := <-receivedRetain:
		if !retained {
			t.Errorf("Expected RETAIN flag to be preserved (RetainAsPublished=true)")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Timeout waiting for message")
	}
}

func TestSubscriptionOptions_RetainHandling(t *testing.T) {
	t.Parallel()
	server, cleanup := startMosquitto(t, "")
	defer cleanup()

	topic := "test/retain-handling/" + t.Name()

	// 1. Publish a retained message first
	pub, err := mq.Dial(server, mq.WithClientID("retain-handler-pub"))
	if err != nil {
		t.Fatal(err)
	}
	pub.Publish(topic, []byte("pre-existing"), mq.WithRetain(true), mq.WithQoS(1)).Wait(context.Background())
	pub.Disconnect(context.Background())

	// 2. Client subscribes with RetainHandling = 2 (Do not send)
	sub, err := mq.Dial(server,
		mq.WithClientID("client-rh-2"),
		mq.WithProtocolVersion(mq.ProtocolV50),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer sub.Disconnect(context.Background())

	received := make(chan string, 1)
	sub.Subscribe(topic, 1, func(_ *mq.Client, msg mq.Message) {
		received <- string(msg.Payload)
	}, mq.WithRetainHandling(2)).Wait(context.Background())

	// 3. Should NOT receive the retained message
	select {
	case msg := <-received:
		t.Fatalf("Received retained message '%s' despite RetainHandling=2", msg)
	case <-time.After(1 * time.Second):
		// Success
	}
}
