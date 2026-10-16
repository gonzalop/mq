package mq_test

import (
	"context"
	"fmt"
	"sync"
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

	token := subClient.Subscribe(context.Background(), topic, 1, func(_ *mq.Client, msg mq.Message) {
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
		token := pubClient.Publish(context.Background(), topic, []byte(payload),
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

// TestTopicAliasesConcurrentPublishers verifies that many goroutines publishing
// to the same topics with WithAlias, mixing QoS 0 and QoS 1, never send a
// packet that uses an alias before the packet defining it: the broker would
// close the connection with a protocol error.
func TestTopicAliasesConcurrentPublishers(t *testing.T) {
	t.Parallel()
	server, cleanup := startMosquitto(t, "")
	defer cleanup()

	const (
		publishers   = 10
		perPublisher = 20
	)
	topics := []string{
		"test/alias/concurrent/" + t.Name() + "/a",
		"test/alias/concurrent/" + t.Name() + "/b",
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	var mu sync.Mutex
	received := make(map[string]bool)
	sub, err := mq.Dial(server, mq.WithClientID("alias-concurrent-sub"), mq.WithProtocolVersion(mq.ProtocolV50))
	if err != nil {
		t.Fatalf("subscriber connect: %v", err)
	}
	defer sub.Disconnect(context.Background())
	filter := "test/alias/concurrent/" + t.Name() + "/+"
	if err := sub.Subscribe(ctx, filter, mq.AtLeastOnce, func(_ *mq.Client, msg mq.Message) {
		mu.Lock()
		received[string(msg.Payload)] = true
		mu.Unlock()
	}).Wait(ctx); err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	lost := make(chan error, 1)
	pub, err := mq.Dial(server,
		mq.WithClientID("alias-concurrent-pub"),
		mq.WithProtocolVersion(mq.ProtocolV50),
		mq.WithTopicAliasMaximum(5),
		mq.WithAutoReconnect(false),
		mq.WithOnConnectionLost(func(_ *mq.Client, err error) {
			select {
			case lost <- err:
			default:
			}
		}),
	)
	if err != nil {
		t.Fatalf("publisher connect: %v", err)
	}
	defer pub.Disconnect(context.Background())

	// Every even message is QoS 1 and must arrive; odd ones are QoS 0.
	var wg sync.WaitGroup
	errs := make(chan error, publishers*perPublisher)
	for p := range publishers {
		wg.Go(func() {
			for i := range perPublisher {
				payload := fmt.Sprintf("p%d-m%d", p, i)
				topic := topics[(p+i)%len(topics)]
				if i%2 == 1 {
					pub.Publish(ctx, topic, []byte(payload), mq.WithQoS(0), mq.WithAlias())
					continue
				}
				if err := pub.Publish(ctx, topic, []byte(payload), mq.WithQoS(1), mq.WithAlias()).Wait(ctx); err != nil {
					errs <- fmt.Errorf("publish %s: %w", payload, err)
					return
				}
			}
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}

	select {
	case err := <-lost:
		t.Fatalf("publisher lost its connection (topic alias used before it was defined?): %v", err)
	default:
	}
	if !pub.IsConnected() {
		t.Fatal("publisher is not connected")
	}

	missing := func() []string {
		mu.Lock()
		defer mu.Unlock()
		var out []string
		for p := range publishers {
			for i := 0; i < perPublisher; i += 2 {
				if payload := fmt.Sprintf("p%d-m%d", p, i); !received[payload] {
					out = append(out, payload)
				}
			}
		}
		return out
	}
	deadline := time.Now().Add(5 * time.Second)
	for m := missing(); len(m) > 0; m = missing() {
		if time.Now().After(deadline) {
			t.Fatalf("subscriber is missing %d QoS 1 messages, e.g. %v", len(m), m[0])
		}
		time.Sleep(20 * time.Millisecond)
	}
}
