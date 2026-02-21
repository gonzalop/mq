package mq

import (
	"sync/atomic"
	"testing"
)

func TestHandlerInterceptor(t *testing.T) {
	var count atomic.Int32

	interceptor := func(next MessageHandler) MessageHandler {
		return func(c *Client, m Message) {
			count.Add(1)
			next(c, m)
		}
	}

	client := &Client{
		opts: &clientOptions{
			HandlerInterceptors: []HandlerInterceptor{interceptor},
		},
	}

	handlerCalled := false
	handler := func(c *Client, m Message) {
		handlerCalled = true
	}

	wrapped := client.wrapHandler(handler)
	wrapped(client, Message{Topic: "test"})

	if count.Load() != 1 {
		t.Errorf("expected interceptor to be called once, got %d", count.Load())
	}
	if !handlerCalled {
		t.Error("expected handler to be called")
	}
}

func TestPublishInterceptor(t *testing.T) {
	var count atomic.Int32

	interceptor := func(next PublishFunc) PublishFunc {
		return func(topic string, payload []byte, opts ...PublishOption) Token {
			count.Add(1)
			// Modify payload
			return next(topic, []byte(string(payload)+"_intercepted"), opts...)
		}
	}

	basePublishCalled := false
	basePublish := func(topic string, payload []byte, opts ...PublishOption) Token {
		basePublishCalled = true
		if string(payload) != "hello_intercepted" {
			t.Errorf("expected modified payload, got %s", string(payload))
		}
		return newToken()
	}

	wrappedPublish := applyPublishInterceptors(basePublish, []PublishInterceptor{interceptor})

	wrappedPublish("test", []byte("hello"))

	if count.Load() != 1 {
		t.Errorf("expected interceptor to be called once, got %d", count.Load())
	}
	if !basePublishCalled {
		t.Error("expected basePublish to be called")
	}
}

func TestMultipleInterceptors(t *testing.T) {
	var order []int

	interceptor1 := func(next PublishFunc) PublishFunc {
		return func(topic string, payload []byte, opts ...PublishOption) Token {
			order = append(order, 1)
			return next(topic, payload, opts...)
		}
	}

	interceptor2 := func(next PublishFunc) PublishFunc {
		return func(topic string, payload []byte, opts ...PublishOption) Token {
			order = append(order, 2)
			return next(topic, payload, opts...)
		}
	}

	basePublish := func(topic string, payload []byte, opts ...PublishOption) Token {
		order = append(order, 3)
		return newToken()
	}

	wrappedPublish := applyPublishInterceptors(basePublish, []PublishInterceptor{interceptor1, interceptor2})

	wrappedPublish("test", []byte("hello"))

	expected := []int{1, 2, 3}
	if len(order) != len(expected) {
		t.Fatalf("expected %d calls, got %d", len(expected), len(order))
	}
	for i, v := range expected {
		if order[i] != v {
			t.Errorf("at index %d: expected %d, got %d", i, v, order[i])
		}
	}
}

func TestIntegrationInterceptor(t *testing.T) {
	interceptor := func(next MessageHandler) MessageHandler {
		return func(c *Client, m Message) {
			next(c, m)
		}
	}

	opts := defaultOptions("tcp://localhost:1883")
	WithHandlerInterceptor(interceptor)(opts)

	if len(opts.HandlerInterceptors) != 1 {
		t.Errorf("expected 1 interceptor, got %d", len(opts.HandlerInterceptors))
	}
}
