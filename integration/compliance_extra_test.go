package mq_test

import (
	"context"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gonzalop/mq"
	"github.com/gonzalop/mq/internal/packets"
)

// TestCompliance_OverlappingSubscriptions verifies that when multiple subscriptions match a topic,
// all corresponding handlers are invoked for the matching message.
func TestCompliance_OverlappingSubscriptions(t *testing.T) {
	t.Parallel()
	server, cleanup := startMosquitto(t, "")
	defer cleanup()

	client, err := mq.Dial(server, mq.WithClientID("overlapping-sub-"+t.Name()))
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer client.Disconnect(context.Background())

	base := "overlapping/" + t.Name()
	topic := base + "/room1/temp"

	var wg sync.WaitGroup
	wg.Add(3)

	var once1, once2, once3 sync.Once
	var h1, h2, h3 atomic.Int32

	// Sub 1: Exact match
	t1 := client.Subscribe(context.Background(), topic, 1, func(_ *mq.Client, _ mq.Message) {
		h1.Add(1)
		once1.Do(wg.Done)
	})

	// Sub 2: Single-level wildcard
	t2 := client.Subscribe(context.Background(), base+"/+/temp", 1, func(_ *mq.Client, _ mq.Message) {
		h2.Add(1)
		once2.Do(wg.Done)
	})

	// Sub 3: Multi-level wildcard
	t3 := client.Subscribe(context.Background(), base+"/#", 1, func(_ *mq.Client, _ mq.Message) {
		h3.Add(1)
		once3.Do(wg.Done)
	})

	ctx := context.Background()
	if err := t1.Wait(ctx); err != nil {
		t.Fatal(err)
	}
	if err := t2.Wait(ctx); err != nil {
		t.Fatal(err)
	}
	if err := t3.Wait(ctx); err != nil {
		t.Fatal(err)
	}

	// Publish message
	pubToken := client.Publish(context.Background(), topic, []byte("23.5"), mq.WithQoS(1))
	if err := pubToken.Wait(context.Background()); err != nil {
		t.Fatalf("Failed to publish: %v", err)
	}

	// Wait for all handlers to be invoked at least once
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Success: all handlers called at least once
	case <-time.After(5 * time.Second):
		t.Fatalf("Timeout waiting for handlers: h1=%d, h2=%d, h3=%d", h1.Load(), h2.Load(), h3.Load())
	}

	// Allow pending broker deliveries (e.g. per-subscription copies from Mosquitto) to complete.
	time.Sleep(200 * time.Millisecond)

	c1, c2, c3 := h1.Load(), h2.Load(), h3.Load()
	if c1 == 0 || c1 != c2 || c2 != c3 {
		t.Errorf("Expected all matching handlers called equally and >0, got: h1=%d, h2=%d, h3=%d", c1, c2, c3)
	}
}

// TestCompliance_QoS_Downgrade verifies that the server (and client) respect the maximum QoS
// granted in the subscription.
func TestCompliance_QoS_Downgrade(t *testing.T) {
	t.Parallel()
	server, cleanup := startMosquitto(t, "")
	defer cleanup()

	// 1. Client A subscribes with QoS 0
	clientA, err := mq.Dial(server, mq.WithClientID("client-qos-0-"+t.Name()))
	if err != nil {
		t.Fatal(err)
	}
	defer clientA.Disconnect(context.Background())

	receivedQoS := make(chan mq.QoS, 1)
	topic := "qos/downgrade/" + t.Name()
	clientA.Subscribe(context.Background(), topic, 0, func(_ *mq.Client, msg mq.Message) {
		receivedQoS <- msg.QoS
	}).Wait(context.Background())

	// 2. Client B publishes with QoS 2
	clientB, err := mq.Dial(server, mq.WithClientID("client-pub-2-"+t.Name()))
	if err != nil {
		t.Fatal(err)
	}
	defer clientB.Disconnect(context.Background())

	clientB.Publish(context.Background(), topic, []byte("downgrade-me"), mq.WithQoS(2)).Wait(context.Background())

	// 3. Client A should receive the message with QoS 0 (downgraded by server)
	select {
	case qos := <-receivedQoS:
		if qos != 0 {
			t.Errorf("Expected QoS 0 (downgraded), got %d", qos)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Timeout waiting for message")
	}
}

// TestCompliance_SubscriptionIdentifier verifies that MQTT v5.0 subscription identifiers
// are correctly delivered with messages.
func TestCompliance_SubscriptionIdentifier(t *testing.T) {
	t.Parallel()
	server, cleanup := startMosquitto(t, "")
	defer cleanup()

	client, err := mq.Dial(server,
		mq.WithClientID("sub-id-client-"+t.Name()),
		mq.WithProtocolVersion(mq.ProtocolV50),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Disconnect(context.Background())

	receivedIDs := make(chan []int, 1)
	topic := "sensors/temp/subid/" + t.Name()
	subID := 123

	token := client.Subscribe(context.Background(), topic, 1, func(_ *mq.Client, msg mq.Message) {
		if msg.Properties != nil {
			receivedIDs <- msg.Properties.SubscriptionIdentifier
		}
	}, mq.WithSubscriptionIdentifier(subID))

	if err := token.Wait(context.Background()); err != nil {
		t.Fatalf("Subscribe failed: %v", err)
	}

	// Publish message
	client.Publish(context.Background(), topic, []byte("data"), mq.WithQoS(1))

	select {
	case ids := <-receivedIDs:
		if len(ids) == 0 {
			t.Error("No subscription identifiers received")
		} else if ids[0] != subID {
			t.Errorf("Received sub ID %d, want %d", ids[0], subID)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Timeout waiting for message with subscription identifier")
	}
}

// TestCompliance_Subscribe_MaxPacketSize verifies that the client enforces the server's
// Maximum Packet Size on SUBSCRIBE requests.
func TestCompliance_Subscribe_MaxPacketSize(t *testing.T) {
	t.Parallel()
	// Use a mock server that advertises a very small Max Packet Size
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		// Read CONNECT
		_, _ = packets.ReadPacket(conn, 5, 0)

		// Send CONNACK with MaxPacketSize = 20 bytes
		connack := &packets.ConnackPacket{
			ReturnCode: packets.ConnAccepted,
			Properties: &packets.Properties{
				MaximumPacketSize: 20,
				Presence:          packets.PresMaximumPacketSize,
			},
		}
		_, _ = connack.WriteTo(conn)

		// Keep connection open for a bit
		time.Sleep(1 * time.Second)
	}()

	client, err := mq.Dial("tcp://"+ln.Addr().String(),
		mq.WithProtocolVersion(mq.ProtocolV50),
		mq.WithAutoReconnect(false),
	)
	if err != nil {
		t.Fatalf("Dial failed: %v", err)
	}
	defer client.Disconnect(context.Background())

	// Attempt a large SUBSCRIBE that exceeds 20 bytes
	// Variable header (2) + topic string (2+15) + QoS (1) = ~20 bytes + Fixed Header (2+)
	err = client.Subscribe(context.Background(), "very/long/topic/filter/that/exceeds/limit", 1, nil).Wait(context.Background())
	if err == nil {
		t.Error("Expected error for large SUBSCRIBE, got nil")
	} else if !strings.Contains(err.Error(), "exceeds server maximum") {
		t.Errorf("Unexpected error message: %v", err)
	}
}
