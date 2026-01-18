package mq

import (
	"context"
	"testing"
	"time"
)

func TestDialContext_Cancellation(t *testing.T) {
	// 1. Create a context that is already cancelled
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// 2. Attempt to dial (should fail immediately)
	client, err := DialContext(ctx, "tcp://localhost:1883")
	if err == nil {
		_ = client.Disconnect(context.Background())
		t.Fatal("Expected error for cancelled context, got nil")
	}

	if err != context.Canceled {
		// It might be a wrapped error or net error depending on where it failed
		t.Logf("Got expected error: %v", err)
	}
}

func TestDialContext_Timeout(t *testing.T) {
	// 1. Create a context with a very short timeout
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Nanosecond)
	defer cancel()

	// 2. Attempt to dial (should fail due to timeout)
	// We use a non-existent server to ensure it doesn't accidentally connect fast
	client, err := DialContext(ctx, "tcp://192.0.2.1:1883")
	if err == nil {
		_ = client.Disconnect(context.Background())
		t.Fatal("Expected error for timed out context, got nil")
	}
}
