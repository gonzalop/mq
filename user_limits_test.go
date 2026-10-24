package mq

import (
	"context"
	"strings"
	"testing"
)

func TestPublish_OversizedPropertyErrors(t *testing.T) {
	c := &Client{
		opts: defaultOptions("tcp://localhost:1883"),
	}
	c.publish = c.basePublish
	ctx := context.Background()

	longStr := strings.Repeat("x", 65536)
	longBytes := make([]byte, 65536)

	tests := []struct {
		name string
		opt  PublishOption
	}{
		{"UserProperty Key", WithUserProperty(longStr, "val")},
		{"UserProperty Value", WithUserProperty("key", longStr)},
		{"ContentType", WithContentType(longStr)},
		{"ResponseTopic", WithResponseTopic(longStr)},
		{"CorrelationData", WithCorrelationData(longBytes)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tok := c.Publish(ctx, "test/topic", []byte("hello"), tt.opt)
			if tok.Error() == nil {
				t.Fatalf("expected error for oversized %s, got nil", tt.name)
			}
			if !strings.Contains(tok.Error().Error(), "exceeds maximum 65535") {
				t.Errorf("error %q should mention exceeds maximum 65535", tok.Error())
			}
		})
	}
}

func TestSubscribe_OversizedUserPropertyErrors(t *testing.T) {
	c := &Client{
		opts: defaultOptions("tcp://localhost:1883"),
	}
	ctx := context.Background()
	longStr := strings.Repeat("x", 65536)

	tokKey := c.Subscribe(ctx, "test/topic", 1, func(*Client, Message) {}, WithSubscribeUserProperty(longStr, "val"))
	if tokKey.Error() == nil {
		t.Fatal("expected error for oversized subscribe user property key, got nil")
	}

	tokVal := c.Subscribe(ctx, "test/topic", 1, func(*Client, Message) {}, WithSubscribeUserProperty("key", longStr))
	if tokVal.Error() == nil {
		t.Fatal("expected error for oversized subscribe user property value, got nil")
	}
}

func TestUnsubscribe_OversizedUserPropertyErrors(t *testing.T) {
	c := &Client{
		opts: defaultOptions("tcp://localhost:1883"),
	}
	ctx := context.Background()
	longStr := strings.Repeat("x", 65536)

	tokKey := c.Unsubscribe(ctx, "test/topic", WithUnsubscribeUserProperty(longStr, "val"))
	if tokKey.Error() == nil {
		t.Fatal("expected error for oversized unsubscribe user property key, got nil")
	}

	tokVal := c.Unsubscribe(ctx, "test/topic", WithUnsubscribeUserProperty("key", longStr))
	if tokVal.Error() == nil {
		t.Fatal("expected error for oversized unsubscribe user property value, got nil")
	}
}

func TestDial_OversizedOptionsErrors(t *testing.T) {
	ctx := context.Background()
	longStr := strings.Repeat("x", 65536)
	longBytes := make([]byte, 65536)

	tests := []struct {
		name string
		opt  Option
	}{
		{"ClientID", WithClientID(longStr)},
		{"Username", WithCredentials(longStr, "pass")},
		{"Password", WithCredentials("user", longStr)},
		{"ConnectUserProperty Key", WithConnectUserProperties(map[string]string{longStr: "v"})},
		{"ConnectUserProperty Value", WithConnectUserProperties(map[string]string{"k": longStr})},
		{"Will Topic", WithWill(longStr, []byte("offline"), 1, false)},
		{"Will Payload", WithWill("status", longBytes, 1, false)},
		{"Will Property", WithWill("status", []byte("offline"), 1, false, &Properties{ContentType: longStr})},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := DialContext(ctx, "tcp://localhost:1883", tt.opt)
			if err == nil {
				t.Fatalf("expected DialContext error for oversized %s, got nil", tt.name)
			}
			if !strings.Contains(err.Error(), "exceeds maximum 65535") {
				t.Errorf("error %q should mention exceeds maximum 65535", err)
			}
		})
	}
}
