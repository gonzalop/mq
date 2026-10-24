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

// TestInitialSubscriptionsWithOptions verifies that SubscribeOptions passed to
// WithSubscription (including WithSubscriptionQoS and v5 options like
// WithRetainAsPublished) are sent in the initial SUBSCRIBE packet.
func TestInitialSubscriptionsWithOptions(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	const topic = "test/initial-sub-opts"
	gotSub := make(chan *packets.SubscribePacket, 1)
	serverErr := make(chan error, 1)

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			serverErr <- err
			return
		}
		defer conn.Close()

		if _, err := packets.ReadPacket(conn, ProtocolV50, 0); err != nil {
			serverErr <- err
			return
		}
		connack := &packets.ConnackPacket{
			ReturnCode: uint8(packets.ConnAccepted),
			Properties: &packets.Properties{},
		}
		if _, err := connack.WriteTo(conn); err != nil {
			serverErr <- err
			return
		}

		pkt, err := packets.ReadPacket(conn, ProtocolV50, 0)
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
		WithProtocolVersion(ProtocolV50),
		WithClientID("initial-sub-opts-test"),
		WithAutoReconnect(false),
		WithSubscription(topic, func(*Client, Message) {},
			WithSubscriptionQoS(AtLeastOnce),
			WithRetainAsPublished(true),
			WithNoLocal(true),
			WithRetainHandling(1),
			WithSubscriptionIdentifier(42),
			WithSubscribeUserProperty("k", "v"),
		),
	)
	if err != nil {
		t.Fatalf("Dial failed: %v", err)
	}
	defer func() { _ = client.Disconnect(context.Background()) }()

	select {
	case sub := <-gotSub:
		if len(sub.Topics) != 1 || sub.Topics[0] != topic {
			t.Fatalf("unexpected topics: %v", sub.Topics)
		}
		if len(sub.QoS) != 1 || sub.QoS[0] != uint8(AtLeastOnce) {
			t.Errorf("QoS = %v, want [%d]", sub.QoS, AtLeastOnce)
		}
		if len(sub.RetainAsPublished) != 1 || !sub.RetainAsPublished[0] {
			t.Errorf("RetainAsPublished = %v, want [true]", sub.RetainAsPublished)
		}
		if len(sub.NoLocal) != 1 || !sub.NoLocal[0] {
			t.Errorf("NoLocal = %v, want [true]", sub.NoLocal)
		}
		if len(sub.RetainHandling) != 1 || sub.RetainHandling[0] != 1 {
			t.Errorf("RetainHandling = %v, want [1]", sub.RetainHandling)
		}
		if sub.Properties == nil || len(sub.Properties.SubscriptionIdentifier) != 1 || sub.Properties.SubscriptionIdentifier[0] != 42 {
			t.Errorf("SubscriptionIdentifier = %v, want [42]", sub.Properties)
		}
		if sub.Properties == nil || len(sub.Properties.UserProperties) != 1 || sub.Properties.UserProperties[0].Key != "k" || sub.Properties.UserProperties[0].Value != "v" {
			t.Errorf("UserProperties = %v, want [{k v}]", sub.Properties)
		}
	case err := <-serverErr:
		t.Fatalf("server error: %v", err)
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for the initial SUBSCRIBE")
	}
}

func TestInitialSubscriptionsValidation(t *testing.T) {
	tests := []struct {
		name  string
		topic string
		opts  []SubscribeOption
	}{
		{"empty topic", "", nil},
		{"invalid wildcard", "a/+b/c", nil},
		{"shared with NoLocal", "$share/g/topic", []SubscribeOption{WithNoLocal(true)}},
		{"invalid sub ID", "a/b", []SubscribeOption{WithSubscriptionIdentifier(-1)}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Dial("tcp://127.0.0.1:0",
				WithSubscription(tt.topic, func(*Client, Message) {}, tt.opts...),
			)
			if err == nil {
				t.Fatalf("expected Dial to fail for %s, got nil", tt.name)
			}
		})
	}
}
