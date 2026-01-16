package mq

// Payload format indicators
const (
	PayloadFormatBytes uint8 = 0
	PayloadFormatUTF8  uint8 = 1
)

// Properties represents MQTT v5.0 properties for messages.
//
// All fields are optional and only used when the protocol version is 5.0.
// For MQTT v3.1.1 connections, properties are ignored.
//
// Common properties for application use:
//   - ContentType: MIME type of the payload (e.g., "application/json")
//   - ResponseTopic: Topic for response messages in request/response pattern
//   - CorrelationData: Correlation data for matching requests with responses
//   - UserProperties: Application-specific key-value pairs
//   - MessageExpiry: Message expiry interval in seconds
type Properties struct {
	// ContentType specifies the MIME content type of the message payload.
	// Example: "application/json", "text/plain", "application/octet-stream"
	ContentType string

	// ResponseTopic specifies the topic for response messages.
	// Used in request/response messaging patterns.
	ResponseTopic string

	// CorrelationData is used to correlate request and response messages.
	// Typically used with ResponseTopic for request/response patterns.
	CorrelationData []byte

	// MessageExpiry specifies the message expiry interval in seconds.
	// If set, the message will be discarded if not delivered within this time.
	MessageExpiry *uint32

	// PayloadFormat indicates the format of the payload.
	// 0 = unspecified bytes (default)
	// 1 = UTF-8 encoded character data
	PayloadFormat *uint8

	// SubscriptionIdentifier contains the subscription identifier(s) that matched
	// this message. Only present in received messages when the server supports
	// subscription identifiers and the subscription was created with an ID.
	// This is a receive-only property. If set when publishing, it will be silently
	// ignored and not sent to the server.
	SubscriptionIdentifier []int

	// ReasonString contains a human-readable explanation from the server.
	// Typically used for diagnostic purposes when operations fail or behave
	// unexpectedly. Common in error responses and server notifications.
	// This is a receive-only property. If set when publishing, it will be silently
	// ignored and not sent to the server.
	ReasonString string

	// WillDelayInterval specifies the delay in seconds before the Will Message is sent.
	// If the connection is re-established before this time, the Will Message is not sent.
	WillDelayInterval *uint32

	// SessionExpiryInterval specifies the session expiry interval in seconds.
	// Used in DISCONNECT packets to update the expiry interval.
	SessionExpiryInterval *uint32

	// UserProperties contains application-specific properties as key-value pairs.
	// These can be used to pass custom metadata with messages.
	UserProperties map[string]string
}

// NewProperties creates a new Properties instance with initialized maps.
func NewProperties() *Properties {
	return &Properties{
		UserProperties: make(map[string]string),
	}
}

// SetUserProperty adds or updates a user property.
func (p *Properties) SetUserProperty(key, value string) {
	if p.UserProperties == nil {
		p.UserProperties = make(map[string]string)
	}
	p.UserProperties[key] = value
}

// GetUserProperty retrieves a user property value.
// Returns empty string if the property doesn't exist.
func (p *Properties) GetUserProperty(key string) string {
	if p.UserProperties == nil {
		return ""
	}
	return p.UserProperties[key]
}
