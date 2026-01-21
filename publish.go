package mq

import (
	"fmt"

	"github.com/gonzalop/mq/internal/packets"
)

// PublishOptions holds configuration for a publish operation.
type PublishOptions struct {
	QoS        uint8
	Retain     bool
	Properties *Properties
	UseAlias   bool
}

// PublishOption is a functional option for configuring a PUBLISH packet.
type PublishOption func(*PublishOptions)

// WithQoS sets the Quality of Service level for the publish.
//
// QoS levels:
//   - 0: At most once delivery (fire and forget)
//   - 1: At least once delivery (acknowledged)
//   - 2: Exactly once delivery (assured)
//
// Default is QoS 0.
func WithQoS(qos QoS) PublishOption {
	return func(o *PublishOptions) {
		o.QoS = uint8(qos)
	}
}

// WithRetain sets the retain flag for the publish.
//
// When true, the server stores the message and delivers it to future
// subscribers of the topic. Only the most recent retained message per
// topic is stored.
//
// Default is false.
func WithRetain(retain bool) PublishOption {
	return func(o *PublishOptions) {
		o.Retain = retain
	}
}

// WithContentType sets the MQTT v5.0 content type property.
// This specifies the MIME type of the message payload.
// Only used when protocol version is 5.0, ignored for v3.1.1.
//
// Example:
//
//	client.Publish("data/json", payload, mq.WithContentType("application/json"))
func WithContentType(contentType string) PublishOption {
	return func(o *PublishOptions) {
		if o.Properties == nil {
			o.Properties = &Properties{}
		}
		o.Properties.ContentType = contentType
	}
}

// WithResponseTopic sets the response topic for request/response pattern.
// The receiver can publish responses to this topic.
// Only used when protocol version is 5.0, ignored for v3.1.1.
//
// Example:
//
//	client.Publish("requests/data", payload,
//	    mq.WithResponseTopic("responses/data"),
//	    mq.WithCorrelationData([]byte("req-123")))
func WithResponseTopic(topic string) PublishOption {
	return func(o *PublishOptions) {
		if o.Properties == nil {
			o.Properties = &Properties{}
		}
		o.Properties.ResponseTopic = topic
	}
}

// WithCorrelationData sets correlation data for request/response pattern.
// Used to match responses with requests.
// Only used when protocol version is 5.0, ignored for v3.1.1.
//
// Example:
//
//	client.Publish("requests/data", payload,
//	    mq.WithResponseTopic("responses/data"),
//	    mq.WithCorrelationData([]byte("req-123")))
func WithCorrelationData(data []byte) PublishOption {
	return func(o *PublishOptions) {
		if o.Properties == nil {
			o.Properties = &Properties{}
		}
		o.Properties.CorrelationData = data
	}
}

// WithUserProperty adds a user-defined property key-value pair.
// Can be called multiple times to add multiple properties.
// Only used when protocol version is 5.0, ignored for v3.1.1.
//
// Example:
//
//	client.Publish("sensors/temp", payload,
//	    mq.WithUserProperty("sensor-id", "temp-01"),
//	    mq.WithUserProperty("location", "warehouse-a"))
func WithUserProperty(key, value string) PublishOption {
	return func(o *PublishOptions) {
		if o.Properties == nil {
			o.Properties = &Properties{}
		}
		o.Properties.SetUserProperty(key, value)
	}
}

// WithMessageExpiry sets the message expiry interval in seconds.
// The message will be discarded if not delivered within this time.
// Only used when protocol version is 5.0, ignored for v3.1.1.
//
// Example:
//
//	client.Publish("events/temp", payload, mq.WithMessageExpiry(300)) // 5 minutes
func WithMessageExpiry(seconds uint32) PublishOption {
	return func(o *PublishOptions) {
		if o.Properties == nil {
			o.Properties = &Properties{}
		}
		o.Properties.MessageExpiry = &seconds
	}
}

// WithPayloadFormat sets the payload format indicator.
//
// Format values:
//   - mq.PayloadFormatBytes (0): Unspecified bytes (default)
//   - mq.PayloadFormatUTF8 (1): UTF-8 encoded character data
//
// If PayloadFormatUTF8 is used, the client will validate that the payload
// is valid UTF-8 before sending.
//
// Only used when protocol version is 5.0, ignored for v3.1.1.
//
// Example:
//
//	client.Publish("data/text", []byte("hello"), mq.WithPayloadFormat(mq.PayloadFormatUTF8))
func WithPayloadFormat(format uint8) PublishOption {
	return func(o *PublishOptions) {
		if o.Properties == nil {
			o.Properties = &Properties{}
		}
		o.Properties.PayloadFormat = &format
	}
}

// WithProperties sets multiple v5.0 properties at once.
// This is a convenience function for setting multiple properties.
// Only used when protocol version is 5.0, ignored for v3.1.1.
//
// Example:
//
//	props := mq.NewProperties()
//	props.ContentType = "application/json"
//	props.SetUserProperty("version", "1.0")
//	client.Publish("data/json", payload, mq.WithProperties(props))
func WithProperties(props *Properties) PublishOption {
	return func(o *PublishOptions) {
		o.Properties = props
	}
}

// Publish publishes a message to the specified topic.
//
// The function returns a Token that can be used to wait for completion.
// For QoS 0, the token completes immediately after sending. For QoS 1 and 2,
// the token completes after receiving the appropriate acknowledgment from the server.
//
// Example (QoS 0 - fire and forget):
//
//	client.Publish("sensors/temp", []byte("22.5"), mq.WithQoS(0))
//
// Example (QoS 1 - wait for acknowledgment):
//
//	token := client.Publish("sensors/temp", []byte("22.5"), mq.WithQoS(1))
//	if err := token.Wait(context.Background()); err != nil {
//	    log.Printf("Publish failed: %v", err)
//	}
//
// Example (retained message):
//
//	client.Publish("status/online", []byte("true"),
//	    mq.WithQoS(1),
//	    mq.WithRetain(true))
//
// Example (QoS 2 with timeout):
//
//	token := client.Publish("critical/alert", []byte("fire"),
//	    mq.WithQoS(2))
//	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
//	defer cancel()
//	if err := token.Wait(ctx); err != nil {
//	    log.Printf("Publish timeout or failed: %v", err)
//	}
func (c *Client) Publish(topic string, payload []byte, opts ...PublishOption) Token {
	c.opts.Logger.Debug("publishing message", "topic", topic, "payload_size", len(payload))

	if err := validatePublishTopic(topic, c.opts); err != nil {
		tok := newToken()
		tok.complete(fmt.Errorf("invalid topic: %w", err))
		return tok
	}

	if err := validatePayloadSize(payload, c.opts); err != nil {
		tok := newToken()
		tok.complete(fmt.Errorf("invalid payload: %w", err))
		return tok
	}

	pubOpts := &PublishOptions{}
	for _, opt := range opts {
		opt(pubOpts)
	}

	// Validate payload format if specified (MQTT v5.0)
	if err := validatePayloadFormat(payload, pubOpts.Properties); err != nil {
		tok := newToken()
		tok.complete(fmt.Errorf("invalid payload format: %w", err))
		return tok
	}

	pkt := &packets.PublishPacket{
		Topic:      topic,
		Payload:    payload,
		QoS:        pubOpts.QoS,
		Retain:     pubOpts.Retain,
		Version:    c.opts.ProtocolVersion,
		Properties: toInternalProperties(pubOpts.Properties),
		UseAlias:   pubOpts.UseAlias,
	}

	if pkt.UseAlias && c.opts.ProtocolVersion >= ProtocolV50 {
		c.applyTopicAlias(pkt)
	}

	tok := newToken()

	req := &publishRequest{
		packet: pkt,
		token:  tok,
	}

	// Execute directly (synchronous until packet is in outgoing channel or queue)
	c.internalPublish(req)

	return tok
}
