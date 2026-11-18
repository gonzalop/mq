package mq

import (
	"context"
	"io"
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
		{"ConnectUserPropertyList Key", WithConnectUserProperty(longStr, "v")},
		{"ConnectUserPropertyList Value", WithConnectUserProperty("k", longStr)},
		{"Will Topic", func(o *clientOptions) {
			WithMaxTopicLength(65535)(o)
			WithWill(longStr, []byte("offline"), 1, false)(o)
		}},
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

func TestSendConnect_OversizedAuthDataErrors(t *testing.T) {
	c := &Client{trie: newTopicTrie(),
		opts: &clientOptions{
			ProtocolVersion: ProtocolV50,
			Authenticator:   &tokenAuthenticator{token: strings.Repeat("x", 65536)},
			Logger:          testLogger(),
		},
	}

	err := c.sendConnectPacket(io.Discard)
	if err == nil || !strings.Contains(err.Error(), "authentication data length 65536 exceeds maximum 65535") {
		t.Fatalf("sendConnectPacket() error = %v, want authentication data length error", err)
	}
}

func TestDial_InvalidWillTopic(t *testing.T) {
	ctx := context.Background()
	badTopics := []struct {
		name  string
		topic string
	}{
		{"empty", ""},
		{"plus wildcard", "status/+"},
		{"hash wildcard", "status/#"},
		{"null byte", "status\x00/offline"},
		{"invalid UTF-8", string([]byte{0xff, 0xfe})},
		{"exceeds default topic limit", strings.Repeat("a", DefaultMaxTopicLength+1)},
	}

	for _, tc := range badTopics {
		t.Run(tc.name, func(t *testing.T) {
			_, err := DialContext(ctx, "tcp://localhost:1883", WithWill(tc.topic, []byte("offline"), 1, false))
			if err == nil {
				t.Fatalf("expected DialContext error for will topic %q, got nil", tc.topic)
			}
			if !strings.Contains(err.Error(), "invalid will topic") {
				t.Errorf("error = %v, want invalid will topic", err)
			}
		})
	}
}

func TestValidation_UTF8AndNullBytes(t *testing.T) {
	ctx := context.Background()
	badStrings := []struct {
		name string
		val  string
	}{
		{"null byte", "a\x00b"},
		{"invalid UTF-8", string([]byte{0xff, 0xfe})},
	}

	for _, bad := range badStrings {
		t.Run("ClientID/"+bad.name, func(t *testing.T) {
			if _, err := DialContext(ctx, "tcp://localhost:1883", WithClientID(bad.val)); err == nil {
				t.Fatal("expected DialContext error for invalid ClientID")
			}
		})
		t.Run("Username/"+bad.name, func(t *testing.T) {
			if _, err := DialContext(ctx, "tcp://localhost:1883", WithCredentials(bad.val, "pass")); err == nil {
				t.Fatal("expected DialContext error for invalid Username")
			}
		})
		t.Run("ConnectUserProperty/"+bad.name, func(t *testing.T) {
			if _, err := DialContext(ctx, "tcp://localhost:1883", WithConnectUserProperty(bad.val, "v")); err == nil {
				t.Fatal("expected DialContext error for invalid ConnectUserProperty key")
			}
			if _, err := DialContext(ctx, "tcp://localhost:1883", WithConnectUserProperty("k", bad.val)); err == nil {
				t.Fatal("expected DialContext error for invalid ConnectUserProperty value")
			}
		})
		t.Run("PublishProperties/"+bad.name, func(t *testing.T) {
			c := &Client{opts: defaultOptions("tcp://localhost:1883")}
			c.publish = c.basePublish
			for _, opt := range []PublishOption{
				WithContentType(bad.val),
				WithResponseTopic(bad.val),
				WithUserProperty(bad.val, "v"),
				WithUserProperty("k", bad.val),
			} {
				if err := c.Publish(ctx, "t/1", []byte("hi"), opt).Error(); err == nil {
					t.Fatal("expected Publish error for invalid UTF-8/null property")
				}
			}
		})
		t.Run("ReasonString/"+bad.name, func(t *testing.T) {
			if err := validatePropertiesFieldLengths(&Properties{ReasonString: bad.val}); err == nil {
				t.Fatal("expected error for invalid ReasonString")
			}
		})
		t.Run("SubscribeUserProperty/"+bad.name, func(t *testing.T) {
			c := &Client{opts: defaultOptions("tcp://localhost:1883")}
			if err := c.Subscribe(ctx, "t/1", 1, func(*Client, Message) {}, WithSubscribeUserProperty(bad.val, "v")).Error(); err == nil {
				t.Fatal("expected Subscribe error")
			}
		})
		t.Run("UnsubscribeUserProperty/"+bad.name, func(t *testing.T) {
			c := &Client{opts: defaultOptions("tcp://localhost:1883")}
			if err := c.Unsubscribe(ctx, "t/1", WithUnsubscribeUserProperty("k", bad.val)).Error(); err == nil {
				t.Fatal("expected Unsubscribe error")
			}
		})
		t.Run("AuthMethod/"+bad.name, func(t *testing.T) {
			if err := checkAuthFieldLengths(bad.val, nil); err == nil {
				t.Fatal("expected error for invalid authentication method")
			}
		})
	}

	t.Run("ResponseTopic wildcards", func(t *testing.T) {
		for _, rt := range []string{"resp/+/id", "resp/#"} {
			if err := validatePropertiesFieldLengths(&Properties{ResponseTopic: rt}); err == nil {
				t.Errorf("expected error for ResponseTopic wildcard %q", rt)
			}
		}
	})

	t.Run("Binary password allowed", func(t *testing.T) {
		opts := defaultOptions("tcp://localhost:1883")
		WithCredentials("user", string([]byte{0x00, 0xff, 0xfe, 'a'}))(opts)
		if err := validateClientOptions(opts); err != nil {
			t.Fatalf("binary password should be allowed, got: %v", err)
		}
	})

	t.Run("MaxTopicLength clamped to 65535", func(t *testing.T) {
		opts := defaultOptions("tcp://localhost:1883")
		WithMaxTopicLength(70000)(opts)
		if err := validatePublishTopic(strings.Repeat("a", 65536), opts); err == nil {
			t.Fatal("expected error for 65536-byte topic even when MaxTopicLength=70000")
		}
		if err := validateSubscribeTopic(strings.Repeat("a", 65536), opts); err == nil {
			t.Fatal("expected error for 65536-byte filter even when MaxTopicLength=70000")
		}
	})
}
