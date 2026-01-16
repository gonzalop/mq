package mq_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/gonzalop/mq"
)

// TestTopicAliases verifies that topic aliases are correctly used to minimize bandwidth.
// This is a "black box" test: we verify that the subscriber receives all messages correctly,
// implying that the server successfully resolved the aliases sent by the publisher.
func TestTopicAliases(t *testing.T) {
	t.Parallel()
	server, cleanup := startMosquitto(t, "")
	defer cleanup()

	const (
		topic      = "test/topic/alias/benchmark" // Long topic to make alias savings obvious
		numMsgs    = 10
		maxAlias   = 5
		subTimeout = 5 * time.Second
	)

	// 1. Start Subscriber (Client A)
	subReceived := make(chan mq.Message, numMsgs)
	subClient, err := mq.Dial(server,
		mq.WithClientID("test-alias-sub"),
		mq.WithProtocolVersion(mq.ProtocolV50),
	)
	if err != nil {
		t.Fatalf("Subscriber failed to connect: %v", err)
	}
	defer subClient.Disconnect(context.Background())

	token := subClient.Subscribe(topic, 1, func(c *mq.Client, msg mq.Message) {
		subReceived <- msg
	})
	if err := token.Wait(context.Background()); err != nil {
		t.Fatalf("Failed to subscribe: %v", err)
	}

	// 2. Start Publisher (Client B)
	// Configure it to use aliases (TopicAliasMaximum > 0)
	pubClient, err := mq.Dial(server,
		mq.WithClientID("test-alias-pub"),
		mq.WithProtocolVersion(mq.ProtocolV50),
		mq.WithTopicAliasMaximum(maxAlias),
	)
	if err != nil {
		t.Fatalf("Publisher failed to connect: %v", err)
	}
	defer pubClient.Disconnect(context.Background())

	// 3. Publish messages
	// The first message should send full topic + Alias=1
	// Subsequent messages should send Alias=1 (empty topic)
	// The library handles this automatically.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	for i := 0; i < numMsgs; i++ {
		payload := fmt.Sprintf("msg-%d", i)
		token := pubClient.Publish(topic, []byte(payload),
			mq.WithQoS(1),
			mq.WithAlias(), // Explicitly request alias usage
		)
		if err := token.Wait(ctx); err != nil {
			t.Fatalf("Failed to publish message %d: %v", i, err)
		}
	}

	// 4. Verify Subscriber received all messages
	// If aliases were malformed or logic was wrong, the server would likely
	// disconnect the publisher or not deliver the messages.
	// 4. Verify Subscriber received all messages
	// Note: The library invokes handlers in goroutines, so ordering is not guaranteed
	// at the application level. We verified received set.
	expected := make(map[string]bool)
	for i := 0; i < numMsgs; i++ {
		expected[fmt.Sprintf("msg-%d", i)] = true
	}

	timeout := time.After(subTimeout)
	for i := 0; i < numMsgs; i++ {
		select {
		case msg := <-subReceived:
			payload := string(msg.Payload)
			if !expected[payload] {
				t.Errorf("Received unexpected message: %sw", payload)
			}
			if msg.Topic != topic {
				t.Errorf("Message topic = %s, want %s", msg.Topic, topic)
			}
			delete(expected, payload)
		case <-timeout:
			t.Fatalf("Timeout waiting for messages. Missing %d messages", len(expected))
		}
	}

	if len(expected) > 0 {
		t.Errorf("Missing messages: %v", expected)
	}
}
