package mq_test

import (
	"context"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gonzalop/mq"
	"github.com/gonzalop/mq/internal/packets"
)

// TestCompliance_OverlappingSubscriptions verifies that when multiple subscriptions match a topic,
// all corresponding handlers are called exactly once.
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
	handler1Called := 0
	handler2Called := 0
	handler3Called := 0
	var mu sync.Mutex

	// Sub 1: Exact match
	t1 := client.Subscribe(topic, 1, func(_ *mq.Client, _ mq.Message) {
		mu.Lock()
		handler1Called++
		mu.Unlock()
		once1.Do(wg.Done)
	})

	// Sub 2: Single-level wildcard
	t2 := client.Subscribe(base + "/+/temp", 1, func(_ *mq.Client, _ mq.Message) {
		mu.Lock()
		handler2Called++
		mu.Unlock()
		once2.Do(wg.Done)
	})

	// Sub 3: Multi-level wildcard
	t3 := client.Subscribe(base + "/#", 1, func(_ *mq.Client, _ mq.Message) {
		mu.Lock()
		handler3Called++
		mu.Unlock()
		once3.Do(wg.Done)
	})

	ctx := context.Background()
	if err := t1.Wait(ctx); err != nil { t.Fatal(err) }
	if err := t2.Wait(ctx); err != nil { t.Fatal(err) }
	if err := t3.Wait(ctx); err != nil { t.Fatal(err) }

	// Publish message
	client.Publish(topic, []byte("23.5"), mq.WithQoS(1))

	// Wait for all handlers
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Success
	case <-time.After(5 * time.Second):
		t.Errorf("Handlers not called enough: h1=%d, h2=%d, h3=%d", handler1Called, handler2Called, handler3Called)
	}

	if handler1Called != 1 || handler2Called != 1 || handler3Called != 1 {
		t.Errorf("Expected each handler to be called once, got: h1=%d, h2=%d, h3=%d", 
			handler1Called, handler2Called, handler3Called)
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
	if err != nil { t.Fatal(err) }
	defer clientA.Disconnect(context.Background())

	receivedQoS := make(chan mq.QoS, 1)
	topic := "qos/downgrade/" + t.Name()
	clientA.Subscribe(topic, 0, func(_ *mq.Client, msg mq.Message) {
		receivedQoS <- msg.QoS
	}).Wait(context.Background())

	// 2. Client B publishes with QoS 2
	clientB, err := mq.Dial(server, mq.WithClientID("client-pub-2-"+t.Name()))
	if err != nil { t.Fatal(err) }
	defer clientB.Disconnect(context.Background())

	clientB.Publish(topic, []byte("downgrade-me"), mq.WithQoS(2)).Wait(context.Background())

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
	if err != nil { t.Fatal(err) }
	defer client.Disconnect(context.Background())

	receivedIDs := make(chan []int, 1)
	topic := "sensors/temp/subid/" + t.Name()
	subID := 123

	token := client.Subscribe(topic, 1, func(_ *mq.Client, msg mq.Message) {
		if msg.Properties != nil {
			receivedIDs <- msg.Properties.SubscriptionIdentifier
		}
	}, mq.WithSubscriptionIdentifier(subID))

	if err := token.Wait(context.Background()); err != nil {
		t.Fatalf("Subscribe failed: %v", err)
	}

	// Publish message
	client.Publish(topic, []byte("data"), mq.WithQoS(1))

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
	if err != nil { t.Fatal(err) }
	defer ln.Close()

	go func() {
		conn, err := ln.Accept()
		if err != nil { return }
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
	if err != nil { t.Fatalf("Dial failed: %v", err) }
	defer client.Disconnect(context.Background())

	// Attempt a large SUBSCRIBE that exceeds 20 bytes
	// Variable header (2) + topic string (2+15) + QoS (1) = ~20 bytes + Fixed Header (2+)
	err = client.Subscribe("very/long/topic/filter/that/exceeds/limit", 1, nil).Wait(context.Background())
	if err == nil {
		t.Error("Expected error for large SUBSCRIBE, got nil")
	} else if !strings.Contains(err.Error(), "exceeds server maximum") {
		t.Errorf("Unexpected error message: %v", err)
	}
}
