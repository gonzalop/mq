package mq

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
// When the limit is reached, incoming messages will block the internal
// processing loop until a handler finishes. This provides natural backpressure
// but can affect keepalive if handlers are extremely slow.
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
