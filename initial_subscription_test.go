package mq

import (
	"context"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/gonzalop/mq/internal/packets"
)

// TestInitialSubscriptionsSentOnFirstConnect verifies that a filter registered
// through WithSubscription is actually subscribed on the first connection when
// the session is clean (the default). Before the fix the handler was only
// registered locally and the SUBSCRIBE was not sent until the first reconnect,
// so the client received nothing on a matching topic.
func TestInitialSubscriptionsSentOnFirstConnect(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	const topic = "test/initial-subscription"
	gotSub := make(chan *packets.SubscribePacket, 1)
	serverErr := make(chan error, 1)

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			serverErr <- err
			return
		}
		defer conn.Close()

		if _, err := packets.ReadPacket(conn, ProtocolV311, 0); err != nil {
			serverErr <- err
			return
		}
		connack := &packets.ConnackPacket{ReturnCode: uint8(packets.ConnAccepted)}
		if _, err := connack.WriteTo(conn); err != nil {
			serverErr <- err
			return
		}

		pkt, err := packets.ReadPacket(conn, ProtocolV311, 0)
		if err != nil {
			serverErr <- err
			return
		}
		sub, ok := pkt.(*packets.SubscribePacket)
		if !ok {
			serverErr <- fmt.Errorf("expected SUBSCRIBE, got %T", pkt)
			return
		}
		gotSub <- sub
	}()

	client, err := Dial("tcp://"+ln.Addr().String(),
		WithProtocolVersion(ProtocolV311),
		WithClientID("initial-subscription-test"),
		WithAutoReconnect(false),
		WithSubscription(topic, func(*Client, Message) {}),
	)
	if err != nil {
		t.Fatalf("Dial failed: %v", err)
	}
	defer func() { _ = client.Disconnect(context.Background()) }()

	select {
	case sub := <-gotSub:
		found := false
		for _, tp := range sub.Topics {
			if tp == topic {
				found = true
			}
		}
		if !found {
			t.Fatalf("SUBSCRIBE did not include %q: %v", topic, sub.Topics)
		}
	case err := <-serverErr:
		t.Fatalf("server error: %v", err)
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for the initial SUBSCRIBE")
	}
}
