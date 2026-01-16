package mq

// WithAlias enables topic alias optimization for this publish.
//
// Only applicable for MQTT v5.0 when WithTopicAliasMaximum() is set.
// Topic aliases allow the client to send a short alias ID instead of the
// full topic name, reducing bandwidth usage for frequently published topics.
//
// On the first publish to a topic with WithAlias():
//   - Sends full topic name + assigns an alias ID
//   - Subsequent publishes automatically use the alias (sends empty topic)
//
// The library automatically manages alias allocation and tracking.
// If the alias limit is reached, gracefully falls back to sending the full topic.
//
// Example:
//
//	client, _ := mq.Dial("tcp://localhost:1883",
//	    mq.WithProtocolVersion(mq.ProtocolV50),
//	    mq.WithTopicAliasMaximum(100))
//
//	// First publish - sends full topic + assigns alias
//	client.Publish("sensors/building-a/floor-3/room-42/temperature", data,
//	    mq.WithAlias())
//
//	// Subsequent publishes - automatically uses alias (saves ~50 bytes)
//	client.Publish("sensors/building-a/floor-3/room-42/temperature", data,
//	    mq.WithAlias())
func WithAlias() PublishOption {
	return func(o *PublishOptions) {
		o.UseAlias = true
	}
}
