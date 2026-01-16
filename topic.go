package mq

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// matchTopic checks if a topic matches a topic filter with MQTT wildcards.
// Supports:
// - '+' matches a single level
// - '#' matches multiple levels (must be last character)
func matchTopic(filter, topic string) bool {
	// Following MQTT-4.7.2-1 (even though the spec mentions "Server", this client
	// library enforces it for local message dispatching):
	// "The Server MUST NOT match Topic Filters starting with a wildcard
	// character (# or +) to Topic Names beginning with a $ character."
	if len(topic) > 0 && topic[0] == '$' {
		if len(filter) > 0 && (filter[0] == '+' || filter[0] == '#') {
			return false
		}
	}

	fIdx := 0
	tIdx := 0
	fLen := len(filter)
	tLen := len(topic)

	for fIdx <= fLen {
		var fLevel string
		var fNext int

		// Find next level in filter
		if idx := strings.IndexByte(filter[fIdx:], '/'); idx >= 0 {
			fNext = fIdx + idx
			fLevel = filter[fIdx:fNext]
		} else {
			fNext = fLen
			fLevel = filter[fIdx:]
		}

		if fLevel == "#" {
			// Multi-level wildcard matches everything remaining (including nothing)
			return true
		}

		// Check if we've run out of topic levels
		if tIdx > tLen {
			return false
		}

		var tLevel string
		var tNext int

		// Find next level in topic
		if idx := strings.IndexByte(topic[tIdx:], '/'); idx >= 0 {
			tNext = tIdx + idx
			tLevel = topic[tIdx:tNext]
		} else {
			tNext = tLen
			tLevel = topic[tIdx:]
		}

		if fLevel == "+" {
			// Single-level wildcard matches this level
		} else if fLevel != tLevel {
			// No match
			return false
		}

		// Advance indices
		if fNext == fLen {
			fIdx = fLen + 1
		} else {
			fIdx = fNext + 1
		}

		if tNext == tLen {
			tIdx = tLen + 1
		} else {
			tIdx = tNext + 1
		}
	}

	return tIdx > tLen
}

// MQTT specification limits (defaults when not configured)
const (
	// DefaultMaxTopicLength is the maximum length of an MQTT topic (2 bytes for length prefix)
	DefaultMaxTopicLength = 65535

	// DefaultMaxPayloadSize is the maximum size of an MQTT message payload (256MB)
	DefaultMaxPayloadSize = 268435455 // 256MB - 1

	// DefaultMaxIncomingPacket is the maximum size of an incoming MQTT packet
	DefaultMaxIncomingPacket = 268435455 // 256MB - 1

	// MaxClientIDLength is the recommended maximum client ID length
	MaxClientIDLength = 23
)

// getLimit returns the configured limit or the default if not set
func getLimit(configured, defaultLimit int) int {
	if configured > 0 {
		return configured
	}
	return defaultLimit
}

// validatePublishTopic validates a topic for publishing.
// Publish topics must not contain wildcards and must follow MQTT rules.
func validatePublishTopic(topic string, opts *clientOptions) error {
	if topic == "" {
		return fmt.Errorf("topic cannot be empty")
	}

	maxLen := getLimit(opts.MaxTopicLength, DefaultMaxTopicLength)
	if len(topic) > maxLen {
		return fmt.Errorf("topic length %d exceeds maximum %d", len(topic), maxLen)
	}

	if strings.Contains(topic, "+") {
		return fmt.Errorf("topic contains single-level wildcard '+' which is not allowed in PUBLISH")
	}

	if strings.Contains(topic, "#") {
		return fmt.Errorf("topic contains multi-level wildcard '#' which is not allowed in PUBLISH")
	}

	if strings.Contains(topic, "\x00") {
		return fmt.Errorf("topic contains null byte which is not allowed")
	}

	if !utf8.ValidString(topic) {
		return fmt.Errorf("topic is not valid UTF-8")
	}

	return nil
}

// validateSubscribeTopic validates a topic filter for subscribing.
// Subscribe topics may contain wildcards but must follow MQTT rules.
func validateSubscribeTopic(topic string, opts *clientOptions) error {
	if topic == "" {
		return fmt.Errorf("topic filter cannot be empty")
	}

	maxLen := getLimit(opts.MaxTopicLength, DefaultMaxTopicLength)
	if len(topic) > maxLen {
		return fmt.Errorf("topic filter length %d exceeds maximum %d", len(topic), maxLen)
	}

	// Null bytes are not allowed
	if strings.Contains(topic, "\x00") {
		return fmt.Errorf("topic filter contains null byte which is not allowed")
	}

	if !utf8.ValidString(topic) {
		return fmt.Errorf("topic filter is not valid UTF-8")
	}

	// Validate wildcard usage
	parts := strings.Split(topic, "/")
	for i, part := range parts {
		// Single-level wildcard must be alone in the level
		if strings.Contains(part, "+") && part != "+" {
			return fmt.Errorf("single-level wildcard '+' must occupy entire topic level")
		}

		// Multi-level wildcard must be last and alone
		if strings.Contains(part, "#") {
			if part != "#" {
				return fmt.Errorf("multi-level wildcard '#' must occupy entire topic level")
			}
			if i != len(parts)-1 {
				return fmt.Errorf("multi-level wildcard '#' must be the last character")
			}
		}
	}

	return nil
}

// validatePayload validates message payload size.
func validatePayload(payload []byte, opts *clientOptions) error {
	maxSize := getLimit(opts.MaxPayloadSize, DefaultMaxPayloadSize)
	if len(payload) > maxSize {
		return fmt.Errorf("payload size %d exceeds maximum %d", len(payload), maxSize)
	}
	return nil
}

// validatePayloadFormat checks if the payload is valid for the specified format.
// If format is 1 (UTF-8), the payload must be valid UTF-8.
func validatePayloadFormat(payload []byte, props *Properties) error {
	if props == nil || props.PayloadFormat == nil || *props.PayloadFormat == PayloadFormatBytes {
		return nil
	}

	if !utf8.Valid(payload) {
		return fmt.Errorf("payload is not valid UTF-8 as required by PayloadFormat indicator")
	}
	return nil
}
