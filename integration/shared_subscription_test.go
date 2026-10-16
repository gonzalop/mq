package mq_test

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gonzalop/mq"
)

// TestSharedSubscriptionDelivery verifies that messages for a shared
// subscription ($share/<group>/<filter>), which the broker delivers on the
// plain topic, reach the handler of that subscription: each message goes to
// exactly one member of the group, while a plain subscriber to the same filter
// still gets every message.
func TestSharedSubscriptionDelivery(t *testing.T) {
	t.Parallel()
	server, cleanup := startMosquitto(t, "")
	defer cleanup()

	const numMsgs = 20
	base := "test/shared/" + t.Name()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var shared, plain atomic.Int32
	badTopic := make(chan string, numMsgs*3)
	check := func(counter *atomic.Int32) mq.MessageHandler {
		return func(_ *mq.Client, msg mq.Message) {
			if !strings.HasPrefix(msg.Topic, base+"/") {
				badTopic <- msg.Topic
			}
			counter.Add(1)
		}
	}

	subscribe := func(id, filter string, counter *atomic.Int32) {
		t.Helper()
		c, err := mq.Dial(server, mq.WithClientID(id), mq.WithProtocolVersion(mq.ProtocolV50))
		if err != nil {
			t.Fatalf("%s: connect: %v", id, err)
		}
		t.Cleanup(func() { c.Disconnect(context.Background()) })
		if err := c.Subscribe(ctx, filter, mq.AtLeastOnce, check(counter)).Wait(ctx); err != nil {
			t.Fatalf("%s: subscribe %q: %v", id, filter, err)
		}
	}
	subscribe("shared-member-1", "$share/workers/"+base+"/#", &shared)
	subscribe("shared-member-2", "$share/workers/"+base+"/#", &shared)
	subscribe("shared-plain", base+"/#", &plain)

	pub, err := mq.Dial(server, mq.WithClientID("shared-pub"), mq.WithProtocolVersion(mq.ProtocolV50))
	if err != nil {
		t.Fatalf("publisher connect: %v", err)
	}
	defer pub.Disconnect(context.Background())

	for i := range numMsgs {
		topic := fmt.Sprintf("%s/job-%d", base, i)
		if err := pub.Publish(ctx, topic, []byte("work"), mq.WithQoS(1)).Wait(ctx); err != nil {
			t.Fatalf("publish %d: %v", i, err)
		}
	}

	deadline := time.Now().Add(5 * time.Second)
	for shared.Load() < numMsgs || plain.Load() < numMsgs {
		if time.Now().After(deadline) {
			t.Fatalf("timeout: shared group got %d, plain subscriber got %d, want %d each",
				shared.Load(), plain.Load(), numMsgs)
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Give duplicates a moment to show up.
	time.Sleep(200 * time.Millisecond)
	if n := shared.Load(); n != numMsgs {
		t.Errorf("shared group got %d messages, want %d (each delivered to exactly one member)", n, numMsgs)
	}
	if n := plain.Load(); n != numMsgs {
		t.Errorf("plain subscriber got %d messages, want %d", n, numMsgs)
	}
	select {
	case topic := <-badTopic:
		t.Errorf("handler got a message on unexpected topic %q", topic)
	default:
	}
}
