package mq_test

import (
	"context"
	"testing"
	"time"

	"github.com/gonzalop/mq"
)

func TestV311Isolation(t *testing.T) {
	t.Parallel()
	// 1. Start a client forcing MQTT v3.1.1
	// We connect to our Mosquitto instance (which is v5 capable, but supports v3)
	// If we send v5 packets on a v3 connection, Mosquitto should disconnect us.
	server, cleanup := startMosquitto(t, "")
	defer cleanup()

	client, err := mq.Dial(
		server,
		mq.WithProtocolVersion(mq.ProtocolV311),
		mq.WithClientID("v3-isolation-test"),
		mq.WithCleanSession(true),
	)
	if err != nil {
		t.Fatalf("Failed to dial: %v", err)
	}
	defer client.Disconnect(context.Background())

	// 2. Attempt to Publish with v5.0 properties
	// The library should SILENTLY STRIP these.
	// If it leaks them, Mosquitto receives a malformed v3.1.1 packet and disconnects.
	token := client.Publish(
		"test/isolation",
		[]byte("payload"),
		mq.WithQoS(1),
		mq.WithUserProperty("key", "value"), // v5 feature
		mq.WithContentType("text/plain"),    // v5 feature
	)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := token.Wait(ctx); err != nil {
		t.Fatalf("Publish failed: %v", err)
	}

	// 3. Verify connection is still alive by doing a basic ping/publish
	// If the previous publish caused a disconnect, this will fail or we'll be disconnected.
	time.Sleep(100 * time.Millisecond) // Give time for server to react if it was going to kill us

	token2 := client.Publish("test/isolation/check", []byte("check"), mq.WithQoS(1))
	if err := token2.Wait(ctx); err != nil {
		t.Fatalf("Connection died after sending v5 properties on v3 link: %v", err)
	}
}

func TestV311SubscribeIsolation(t *testing.T) {
	t.Parallel()
	server, cleanup := startMosquitto(t, "")
	defer cleanup()

	client, err := mq.Dial(
		server,
		mq.WithProtocolVersion(mq.ProtocolV311),
		mq.WithClientID("v3-sub-isolation"),
	)
	if err != nil {
		t.Fatalf("Failed to dial: %v", err)
	}
	defer client.Disconnect(context.Background())

	// Attempt to Subscribe with v5 options (NoLocal)
	// Library should strip NoLocal.
	token := client.Subscribe(
		"test/isolation/sub",
		1,
		func(c *mq.Client, m mq.Message) {},
		mq.WithNoLocal(true), // v5 feature
	)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := token.Wait(ctx); err != nil {
		// If encoding leaked v5 options into payload, server would reject or disconnect
		t.Fatalf("Subscribe with v5 options failed on v3: %v", err)
	}
}
