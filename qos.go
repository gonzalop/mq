package mq

// QoS represents the MQTT Quality of Service level.
type QoS uint8

// MQTT Quality of Service levels.
//
// These constants provide readable names for the three QoS levels defined
// in the MQTT specification. Using named constants improves code readability
// compared to numeric literals.
//
// Example:
//
//	// More readable
//	client.Subscribe("sensors/temp", mq.AtLeastOnce, handler)
//	client.Publish("alert", data, mq.WithQoS(mq.ExactlyOnce))
//
//	// vs numeric literals
//	client.Subscribe("sensors/temp", 1, handler)
//	client.Publish("alert", data, mq.WithQoS(2))
const (
	// AtMostOnce (QoS 0) - Fire and forget delivery.
	// The message is delivered at most once, or it may not be delivered at all.
	// No acknowledgment is sent by the receiver, and the message is not retried.
	AtMostOnce QoS = 0

	// AtLeastOnce (QoS 1) - Acknowledged delivery.
	// The message is always delivered at least once. The receiver sends an
	// acknowledgment (PUBACK), and the sender retries until acknowledged.
	// Duplicate messages may occur.
	AtLeastOnce QoS = 1

	// ExactlyOnce (QoS 2) - Assured delivery.
	// The message is always delivered exactly once using a four-step handshake
	// (PUBLISH, PUBREC, PUBREL, PUBCOMP). This is the safest but slowest option.
	ExactlyOnce QoS = 2
)
