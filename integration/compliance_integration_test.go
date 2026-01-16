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
	client, err := mq.Dial(server,
		mq.WithClientID(""),
		mq.WithCleanSession(false),
		mq.WithProtocolVersion(mq.ProtocolV50),
		mq.WithAutoReconnect(true),
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

	// Wait a bit for client to detect disconnect
	time.Sleep(2 * time.Second)
	if client.IsConnected() {
		t.Log("Client still thinks it is connected, but server is gone.")
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
		mq.WithAutoReconnect(true))
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer client.Disconnect(context.Background())

	topic := "test/nolocal/reconnect"
	received := make(chan string, 10)

	// Subscribe with NoLocal = true
	token := client.Subscribe(topic, mq.AtLeastOnce, func(c *mq.Client, msg mq.Message) {
		received <- string(msg.Payload)
	}, mq.WithNoLocal(true))

	if err := token.Wait(context.Background()); err != nil {
		t.Fatalf("Subscribe failed: %v", err)
	}

	// 1. Verify NoLocal works initially
	client.Publish(topic, []byte("msg1"), mq.WithQoS(1)).Wait(context.Background())
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

	// Give it a moment to finish resubscribing
	time.Sleep(2 * time.Second)

	// 4. Verify NoLocal STILL works
	client.Publish(topic, []byte("msg2"), mq.WithQoS(1)).Wait(context.Background())
	select {
	case msg := <-received:
		t.Fatalf("Received own message '%s' AFTER reconnect. NoLocal option was lost!", msg)
	case <-time.After(1 * time.Second):
		t.Log("✅ NoLocal option persisted through reconnection!")
	}
}
