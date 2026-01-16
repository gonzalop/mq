package mq

// WithMaxTopicLength sets the maximum allowed topic length.
// Default is 65535 (MQTT spec maximum).
// Set to a lower value to reject topics exceeding your application's needs.
func WithMaxTopicLength(max int) Option {
	return func(o *clientOptions) {
		o.MaxTopicLength = max
	}
}

// WithMaxPayloadSize sets the maximum allowed outgoing payload size.
// Default is 268435455 (256MB, MQTT spec maximum).
// Set to a lower value to prevent sending large messages.
func WithMaxPayloadSize(max int) Option {
	return func(o *clientOptions) {
		o.MaxPayloadSize = max
	}
}

// WithMaxIncomingPacket sets the maximum allowed incoming packet size.
// Default is 268435455 (256MB, MQTT spec maximum).
// Set to a lower value to protect against memory exhaustion from large incoming packets.
// Example: WithMaxIncomingPacket(1024 * 1024) limits incoming packets to 1MB.
func WithMaxIncomingPacket(max int) Option {
	return func(o *clientOptions) {
		o.MaxIncomingPacket = max
	}
}
