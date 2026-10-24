package mq

import (
	"fmt"
	"slices"
)

// Payload format indicators
const (
	PayloadFormatBytes uint8 = 0
	PayloadFormatUTF8  uint8 = 1
)

// UserProperty represents a single MQTT v5.0 User Property key-value pair.
type UserProperty struct {
	Key   string
	Value string
}

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
	// When duplicate keys are present in a received packet, this map holds the last
	// value for each key; use UserPropertyList to inspect all pairs in wire order.
	UserProperties map[string]string

	// UserPropertyList contains application-specific properties in wire order,
	// preserving duplicate keys as allowed by MQTT v5.0.
	UserPropertyList []UserProperty
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

	// Replace the first entry for key in place and drop any later duplicates,
	// or append if key is not present yet.
	updated := false
	n := 0
	for _, up := range p.UserPropertyList {
		if up.Key == key {
			if updated {
				continue
			}
			up.Value = value
			updated = true
		}
		p.UserPropertyList[n] = up
		n++
	}
	p.UserPropertyList = p.UserPropertyList[:n]
	if !updated {
		p.UserPropertyList = append(p.UserPropertyList, UserProperty{Key: key, Value: value})
	}
}

// AddUserProperty appends a user property key-value pair, preserving insertion
// order and allowing duplicate keys as permitted by MQTT v5.0.
func (p *Properties) AddUserProperty(key, value string) {
	if p.UserProperties == nil {
		p.UserProperties = make(map[string]string)
	}
	p.UserProperties[key] = value
	p.UserPropertyList = append(p.UserPropertyList, UserProperty{Key: key, Value: value})
}

// GetUserProperty retrieves a user property value.
// Returns empty string if the property doesn't exist.
func (p *Properties) GetUserProperty(key string) string {
	if p.UserProperties != nil {
		if v, ok := p.UserProperties[key]; ok {
			return v
		}
	}
	for _, v := range slices.Backward(p.UserPropertyList) {
		if v.Key == key {
			return v.Value
		}
	}
	return ""
}

// maxFieldLength is the maximum length of an MQTT 2-byte length-prefixed string or binary data (65,535 bytes).
const maxFieldLength = 65535

// validatePropertiesFieldLengths verifies that length-prefixed string and binary fields
// do not exceed the MQTT 16-bit length prefix maximum of 65,535 bytes.
func validatePropertiesFieldLengths(props *Properties) error {
	if props == nil {
		return nil
	}
	if len(props.ContentType) > maxFieldLength {
		return fmt.Errorf("content type length %d exceeds maximum %d", len(props.ContentType), maxFieldLength)
	}
	if len(props.ResponseTopic) > maxFieldLength {
		return fmt.Errorf("response topic length %d exceeds maximum %d", len(props.ResponseTopic), maxFieldLength)
	}
	if len(props.ReasonString) > maxFieldLength {
		return fmt.Errorf("reason string length %d exceeds maximum %d", len(props.ReasonString), maxFieldLength)
	}
	if len(props.CorrelationData) > maxFieldLength {
		return fmt.Errorf("correlation data length %d exceeds maximum %d", len(props.CorrelationData), maxFieldLength)
	}
	for _, up := range props.UserPropertyList {
		if len(up.Key) > maxFieldLength {
			return fmt.Errorf("user property key length %d exceeds maximum %d", len(up.Key), maxFieldLength)
		}
		if len(up.Value) > maxFieldLength {
			return fmt.Errorf("user property value length %d exceeds maximum %d", len(up.Value), maxFieldLength)
		}
	}
	for k, v := range props.UserProperties {
		if len(k) > maxFieldLength {
			return fmt.Errorf("user property key length %d exceeds maximum %d", len(k), maxFieldLength)
		}
		if len(v) > maxFieldLength {
			return fmt.Errorf("user property value length %d exceeds maximum %d", len(v), maxFieldLength)
		}
	}
	return nil
}
