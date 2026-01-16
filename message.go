package mq

// Message represents an MQTT message received on a subscribed topic.
//
// This struct is designed to be compatible with both MQTT v3.1.1 and v5.0.
//
// The message is passed to subscription handlers and contains all relevant
// information about the received message including topic, payload, QoS level,
// and flags.
type Message struct {
	// Topic the message was published to
	Topic string

	// Message payload
	Payload []byte

	// Quality of Service level
	QoS QoS

	// Retained message flag
	Retained bool

	// Duplicate delivery flag
	Duplicate bool

	// MQTT v5.0 properties.
	// This field is nil for MQTT v3.1.1 connections or when no properties are present.
	Properties *Properties
}
