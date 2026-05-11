package mq

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// MatchTopic checks if a topic matches a topic filter with MQTT wildcards.
// It follows MQTT-4.7 rules for wildcard matching.
//
// Supports:
// - '+' matches exactly one topic level
// - '#' matches multiple topic levels (must be the last character in the filter)
func MatchTopic(filter, topic string) bool {
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

		if fLevel != "+" && fLevel != tLevel {
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
	// DefaultMaxTopicLength is the maximum length of an MQTT topic.
	// Reduced from spec maximum (65535) to 1024 for security.
	DefaultMaxTopicLength = 1024

	// DefaultMaxPayloadSize is the maximum size of an MQTT message payload.
	// Reduced from spec maximum (256MB) to 1MB for security.
	DefaultMaxPayloadSize = 1048576 // 1MB

	// DefaultMaxIncomingPacket is the maximum size of an incoming MQTT packet.
	// Reduced from spec maximum (256MB) to 1MB for security.
	DefaultMaxIncomingPacket = 1048576 // 1MB

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

// validatePayloadSize validates message payload size.
func validatePayloadSize(payload []byte, opts *clientOptions) error {
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

// validatePublishCaps enforces server capabilities declared in the CONNACK packet
// against the proposed publish operation. Returns a sentinel error (ErrServer*)
// if the operation would violate a server restriction.
//
// Called only for MQTT v5.0 connections. The serverCaps zero-value is
// permissive (all features allowed, QoS 2 max), so calls before the first
// CONNACK are safe.
func (c *Client) validatePublishCaps(topic string, payload []byte, opts *PublishOptions) error {
	state := c.connState.Load()
	if state == nil {
		return nil
	}
	caps := state.caps

	// MaximumQoS: server may only support QoS 0 or 1.
	if caps.MaximumQoS < opts.QoS {
		return fmt.Errorf("%w: requested QoS %d, server maximum is %d",
			ErrQoSExceedsServerMax, opts.QoS, caps.MaximumQoS)
	}

	// RetainAvailable: some servers (e.g. load-balanced clusters) disallow retained messages.
	if !caps.RetainAvailable && opts.Retain {
		return ErrServerNoRetain
	}

	// MaximumPacketSize: conservatively estimate the serialized packet size.
	// Actual size = fixed header (≥2 B) + topic length field (2 B) + topic +
	//               packet ID (2 B, QoS ≥ 1) + properties + payload.
	// We omit the fixed header to keep the estimate simple; this means we may
	// allow a packet that is 1-4 bytes over the limit, but we never block a
	// valid packet.
	if caps.MaximumPacketSize > 0 {
		estimated := uint32(2 + len(topic) + len(payload))
		if opts.QoS > 0 {
			estimated += 2 // packet identifier
		}
		if estimated > caps.MaximumPacketSize {
			return fmt.Errorf("%w: estimated size %d bytes, server maximum is %d",
				ErrPacketExceedsServerMax, estimated, caps.MaximumPacketSize)
		}
	}

	return nil
}

// validateSubscribeCaps enforces server capabilities declared in the CONNACK packet
// against the proposed subscribe operation. Returns a sentinel error (ErrServer*)
// if the operation would violate a server restriction.
//
// Called only for MQTT v5.0 connections. The serverCaps zero-value is
// permissive (all features allowed), so calls before the first CONNACK are safe.
func (c *Client) validateSubscribeCaps(topic string, qos QoS) error {
	state := c.connState.Load()
	if state == nil {
		return nil
	}
	caps := state.caps

	// WildcardAvailable: server may prohibit wildcard subscriptions entirely.
	if !caps.WildcardAvailable {
		if strings.Contains(topic, "+") || strings.Contains(topic, "#") {
			return ErrServerNoWildcards
		}
	}

	// SharedSubscriptionAvailable: server may not support $share/ topics.
	if !caps.SharedSubscriptionAvailable && strings.HasPrefix(topic, "$share/") {
		return ErrServerNoSharedSubs
	}

	// MaximumQoS: server may only support QoS 0 or 1.
	if caps.MaximumQoS < uint8(qos) {
		return fmt.Errorf("%w: requested QoS %d, server maximum is %d",
			ErrQoSExceedsServerMax, uint8(qos), caps.MaximumQoS)
	}

	return nil
}
