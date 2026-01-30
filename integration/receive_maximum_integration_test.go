package mq_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/gonzalop/mq"
)

// TestReceiveMaximum_FlowControl verifies that the client correctly handles
// proper flow control behavior (processing messages and releasing slots)
// when connected to a real broker with a low Receive Maximum.
func TestReceiveMaximum_FlowControl(t *testing.T) {
	t.Parallel()
	server, cleanup := startMosquitto(t, "")
	defer cleanup()

	// 1. Connect with low Receive Maximum (e.g., 2)
	// We use a small buffer to ensure we rely on ACKs to unblock the flow
	receiver, err := mq.Dial(server,
		mq.WithClientID("receiver-client"),
		mq.WithProtocolVersion(mq.ProtocolV50),
		mq.WithReceiveMaximum(2, mq.LimitPolicyIgnore),
		mq.WithAutoReconnect(false),
	)
	if err != nil {
		t.Fatalf("Failed to connect receiver: %v", err)
	}
	defer receiver.Disconnect(context.Background())

	topic := "test/flowlimit"
	msgCount := 10
	receivedCount := 0
	done := make(chan struct{})

	// 2. Subscribe
	var mu sync.Mutex
	handler := func(c *mq.Client, m mq.Message) {
		mu.Lock()
		defer mu.Unlock()
		receivedCount++
		if receivedCount == msgCount {
			close(done)
		}
	}

	if token := receiver.Subscribe(topic, 1, handler); token.Wait(context.Background()) != nil {
		t.Fatalf("Failed to subscribe: %v", token.Error())
	}

	// 3. Sender floods messages
	sender, err := mq.Dial(server, mq.WithClientID("sender-client"))
	if err != nil {
		t.Fatalf("Failed to connect sender: %v", err)
	}
	defer sender.Disconnect(context.Background())

	var wg sync.WaitGroup
	wg.Add(msgCount)

	for i := 0; i < msgCount; i++ {
		go func(id int) {
			defer wg.Done()
			token := sender.Publish(topic, []byte("payload"), mq.WithQoS(1))
			token.Wait(context.Background())
		}(i)
	}
	wg.Wait()

	// 4. Wait for all messages
	// This proves the client did not disconnect.
	select {
	case <-done:
		// Success
	case <-time.After(5 * time.Second):
		t.Errorf("Timeout waiting for messages. Received %d/%d", receivedCount, msgCount)
	}
}
