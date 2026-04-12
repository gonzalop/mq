package mq

import (
	"context"
	"sync"
)

// Token represents an asynchronous operation that can be waited on.
//
// Tokens are returned by Publish, Subscribe, and Unsubscribe operations.
// They provide both blocking (Wait) and non-blocking (Done + Error) patterns
// for handling operation completion.
//
// Example (blocking wait):
//
//	token := client.Publish("topic", []byte("data"), mq.WithQoS(1))
//	if err := token.Wait(context.Background()); err != nil {
//	    log.Printf("Operation failed: %v", err)
//	}
//
// Example (non-blocking with select):
//
//	token := client.Publish("topic", []byte("data"), mq.WithQoS(1))
//	select {
//	case <-token.Done():
//	    if err := token.Error(); err != nil {
//	        log.Printf("Failed: %v", err)
//	    }
//	case <-time.After(5 * time.Second):
//	    log.Println("Timeout")
//	}
//
// Example (with context timeout):
//
//	token := client.Subscribe("topic", 1, handler)
//	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
//	defer cancel()
//	if err := token.Wait(ctx); err != nil {
//	    log.Printf("Subscribe failed or timed out: %v", err)
//	}
type Token interface {
	// Wait blocks until the operation completes or the context is cancelled.
	// It returns nil if successful, or the error (timeout/nack/connection loss).
	Wait(ctx context.Context) error

	// Done returns a channel that closes when the operation is complete.
	// This allows the token to be used in select statements.
	Done() <-chan struct{}

	// Error returns the error if finished, mostly for use with Done().
	Error() error

	// ReasonCode returns the MQTT v5.0 reason code from the server's acknowledgment.
	// This is useful for inspecting non-error status codes such as:
	//   - ReasonCodeNoMatchingSubscribers (0x10) - message accepted but no one is subscribed
	//   - ReasonCodeGrantedQoS1 (0x01) - subscription accepted at QoS 1 (may differ from requested)
	//   - ReasonCodeNoSubscriptionExisted (0x11) - unsubscribed from a non-existent subscription
	//
	// Returns ReasonCodeSuccess (0x00) if the operation succeeded normally,
	// if no reason code was provided by the server, or for MQTT v3.1.1 connections.
	// Only valid after the token has completed (Wait returned or Done channel closed).
	ReasonCode() ReasonCode

	// Dropped returns true if the message was dropped due to a full internal buffer (QoS 0).
	// This only occurs when Using QoS0LimitPolicyDrop.
	Dropped() bool
}

// token is the internal implementation of Token.
type token struct {
	done       chan struct{}
	err        error
	reasonCode ReasonCode
	dropped    bool
	once       sync.Once
}

// newToken creates a new token.
func newToken() *token {
	return &token{
		done: make(chan struct{}),
	}
}

// Wait blocks until the operation completes or the context is cancelled.
func (t *token) Wait(ctx context.Context) error {
	select {
	case <-t.done:
		return t.err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Done returns a channel that closes when the operation is complete.
func (t *token) Done() <-chan struct{} {
	return t.done
}

// Error returns the error if the operation has completed.
func (t *token) Error() error {
	return t.err
}

// ReasonCode returns the MQTT v5.0 reason code from the server's acknowledgment.
func (t *token) ReasonCode() ReasonCode {
	return t.reasonCode
}

// Dropped returns true if the message was dropped due to a full internal buffer.
// This is only possible if QoS0LimitPolicyDrop is set (default).
func (t *token) Dropped() bool {
	return t.dropped
}

// complete marks the token as complete with the given error.
// This can only be called once; subsequent calls are ignored.
func (t *token) complete(err error) {
	t.once.Do(func() {
		t.err = err
		close(t.done)
	})
}
