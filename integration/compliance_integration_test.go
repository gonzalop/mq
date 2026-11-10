package mq_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/gonzalop/mq"
)

// TestComplianceIntegration_AssignedClientID_Resumption verifies that a server-assigned
// client ID is used for subsequent reconnections to resume the session.
func TestComplianceIntegration_AssignedClientID_Resumption(t *testing.T) {
	t.Parallel()
	// Need a dedicated server to allow disconnect/reconnect simulation without interference
	server, cleanup := startMosquitto(t, "# Dedicated for assigned ID test")
	defer cleanup()

	// Extract port to reuse on restart
	// server is "tcp://localhost:port"
	parts := strings.Split(server, ":")
	port := parts[len(parts)-1]

	// 1. Connect with empty ClientID and persistent session (CleanSession=false)
	lost := make(chan struct{}, 1)
	client, err := mq.Dial(server,
		mq.WithClientID(""),
		mq.WithCleanSession(false),
		mq.WithProtocolVersion(mq.ProtocolV50),
		mq.WithAutoReconnect(true),
		mq.WithOnConnectionLost(func(*mq.Client, error) {
			select {
			case lost <- struct{}{}:
			default:
			}
		}),
		mq.WithReconnectBackoff(50*time.Millisecond, 1*time.Second, false),
		mq.WithSessionExpiryInterval(3600))
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer client.Disconnect(context.Background())

	assignedID := client.AssignedClientID()
	if assignedID == "" {
		t.Fatal("Server did not assign a ClientID")
	}
	t.Logf("Server assigned ID: %s", assignedID)

	// 2. Kill the server to force a disconnection
	cleanup()

	// The client must notice the loss, or the reconnect below proves nothing.
	select {
	case <-lost:
	case <-time.After(10 * time.Second):
		t.Fatal("Client never noticed that the server is gone")
	}

	// 3. Restart the server on SAME PORT
	_, cleanup2 := startMosquitto(t, "# Dedicated for assigned ID test restart", port)
	defer cleanup2()

	// 4. Wait for auto-reconnect
	t.Log("Waiting for auto-reconnect...")
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if client.IsConnected() {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}

	if !client.IsConnected() {
		t.Fatal("Client failed to reconnect")
	}

	// 5. Verify the ClientID used for reconnection is the SAME as assignedID
	// If it used empty string again, Mosquitto would assign a NEW one.
	if client.AssignedClientID() != assignedID {
		t.Errorf("AssignedClientID changed after reconnect! got %q, want %q",
			client.AssignedClientID(), assignedID)
	}
}

// TestComplianceIntegration_NoLocal_Persistence verifies that the NoLocal option
// is correctly re-applied after an automatic reconnection.
func TestComplianceIntegration_NoLocal_Persistence(t *testing.T) {
	t.Parallel()
	server, cleanup := startMosquitto(t, "# Dedicated for NoLocal test")
	defer cleanup()

	// Extract port to reuse on restart
	parts := strings.Split(server, ":")
	port := parts[len(parts)-1]

	client, err := mq.Dial(server,
		mq.WithClientID("nolocal-reconnect-test"),
		mq.WithCleanSession(true),
		mq.WithProtocolVersion(mq.ProtocolV50),
		mq.WithAutoReconnect(true),
		mq.WithReconnectBackoff(50*time.Millisecond, 1*time.Second, false))
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer client.Disconnect(context.Background())

	topic := "test/nolocal/reconnect"
	received := make(chan string, 10)

	// Subscribe with NoLocal = true
	token := client.Subscribe(context.Background(), topic, mq.AtLeastOnce, func(_ *mq.Client, msg mq.Message) {
		received <- string(msg.Payload)
	}, mq.WithNoLocal(true))

	if err := token.Wait(context.Background()); err != nil {
		t.Fatalf("Subscribe failed: %v", err)
	}

	// 1. Verify NoLocal works initially
	client.Publish(context.Background(), topic, []byte("msg1"), mq.WithQoS(1)).Wait(context.Background())
	select {
	case msg := <-received:
		t.Fatalf("Received own message '%s' BEFORE reconnect", msg)
	case <-time.After(500 * time.Millisecond):
		t.Log("NoLocal working correctly initially")
	}

	// 2. Restart server to trigger reconnect
	cleanup()
	time.Sleep(1 * time.Second)
	_, cleanup2 := startMosquitto(t, "# Dedicated for NoLocal test restart", port)
	defer cleanup2()

	// 3. Wait for auto-reconnect
	t.Log("Waiting for auto-reconnect...")
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if client.IsConnected() {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}

	if !client.IsConnected() {
		t.Fatal("Client failed to reconnect")
	}

	// Wait until the resubscription is live: a message from another client
	// must arrive, or the check below would pass with no subscription at all.
	other, err := mq.Dial(server, mq.WithClientID("nolocal-reconnect-other"))
	if err != nil {
		t.Fatalf("Failed to connect second client: %v", err)
	}
	defer other.Disconnect(context.Background())

	resubscribed := false
	for deadline := time.Now().Add(15 * time.Second); !resubscribed && time.Now().Before(deadline); {
		other.Publish(context.Background(), topic, []byte("control"), mq.WithQoS(1)).Wait(context.Background())
		select {
		case <-received:
			resubscribed = true
		case <-time.After(500 * time.Millisecond):
		}
	}
	if !resubscribed {
		t.Fatal("Subscription was not restored after reconnect")
	}

	// 4. Verify NoLocal STILL works
	client.Publish(context.Background(), topic, []byte("msg2"), mq.WithQoS(1)).Wait(context.Background())
	timeout := time.After(1 * time.Second)
	for {
		select {
		case msg := <-received:
			if msg == "control" { // a late copy from the loop above
				continue
			}
			t.Fatalf("Received own message '%s' AFTER reconnect. NoLocal option was lost!", msg)
		case <-timeout:
			t.Log("✅ NoLocal option persisted through reconnection!")
			return
		}
	}
}
