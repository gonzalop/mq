package mq

import "time"

// WithMaxTopicLength sets the maximum allowed topic length.
// Default is 1024 (MQTT spec maximum is 65535).
// Set to a lower value to reject topics exceeding your application's needs.
func WithMaxTopicLength(maxLength int) Option {
	return func(o *clientOptions) {
		o.MaxTopicLength = maxLength
	}
}

// WithMaxPayloadSize sets the maximum allowed outgoing payload size.
// Default is 1048576 (1MB, MQTT spec maximum is 256MB).
// Set to a lower value to prevent sending large messages.
func WithMaxPayloadSize(maxLength int) Option {
	return func(o *clientOptions) {
		o.MaxPayloadSize = maxLength
	}
}

// WithMaxIncomingPacket sets the maximum allowed incoming packet size.
// Default is 1048576 (1MB, MQTT spec maximum is 256MB).
// Set to a lower value to protect against memory exhaustion from large incoming packets.
// Example: WithMaxIncomingPacket(10 * 1024 * 1024) limits incoming packets to 10MB.
func WithMaxIncomingPacket(maxLength int) Option {
	return func(o *clientOptions) {
		o.MaxIncomingPacket = maxLength
	}
}

// WithMaxHandlerConcurrency limits the number of message handler goroutines
// that can run simultaneously.
//
// When the limit is reached, each new message still gets a goroutine, which
// waits for a free slot before running its handler. The internal processing
// loop is not blocked, so there is no backpressure: slow handlers lead to
// many waiting goroutines (and unacknowledged QoS 1/2 messages).
//
// Default is 100. Set to 0 for unlimited (not recommended for production).
func WithMaxHandlerConcurrency(concurrency int) Option {
	return func(o *clientOptions) {
		o.MaxHandlerConcurrency = concurrency
	}
}

// WithMaxAuthExchanges limits the number of AUTH packet exchanges per connection.
// This prevents infinite authentication loops with a malicious or misconfigured server.
// Default is 10.
func WithMaxAuthExchanges(limit uint16) Option {
	return func(o *clientOptions) {
		o.MaxAuthExchanges = limit
	}
}

// WithHandlerTimeout sets the maximum time a message handler is allowed to run.
// When the timeout is reached, the handler's Message.Context is cancelled.
// The handler cannot be forcibly stopped, and its concurrency slot is only
// released when the handler returns.
// 0 = no timeout (default).
func WithHandlerTimeout(timeout time.Duration) Option {
	return func(o *clientOptions) {
		o.HandlerTimeout = timeout
	}
}
