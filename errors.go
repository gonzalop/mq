package mq

import (
	"errors"
	"fmt"
)

// Standard errors returned by the client
var (
	// ErrConnectionRefused is returned when the server rejects the connection.
	// You can unwrap this error to find the specific reason if available.
	ErrConnectionRefused = errors.New("connection refused")

	// Specific connection refusal reasons (v3.1.1)
	ErrUnacceptableProtocolVersion = errors.New("unacceptable protocol version")
	ErrIdentifierRejected          = errors.New("identifier rejected")
	ErrServerUnavailable           = errors.New("server unavailable")
	ErrBadUsernameOrPassword       = errors.New("bad username or password")
	ErrNotAuthorized               = errors.New("not authorized")

	// ErrSubscriptionFailed is returned when the server rejects a subscription.
	ErrSubscriptionFailed = errors.New("subscription failed")

	// ErrClientDisconnected is returned when an operation is cancelled because
	// the client was disconnected or stopped.
	ErrClientDisconnected = errors.New("client disconnected")

	// ErrNoPacketIDsAvailable is returned when all 65535 packet IDs are in use.
	ErrNoPacketIDsAvailable = errors.New("no packet IDs available")

	// Server capability violation errors (MQTT v5.0).
	// These are returned by Publish or Subscribe when the requested operation
	// violates a capability the server declared in its CONNACK packet.

	// ErrServerNoWildcards is returned when subscribing with a wildcard topic
	// filter ('+' or '#') but the server declared WildcardSubscriptionAvailable=false.
	ErrServerNoWildcards = errors.New("mqtt: server does not support wildcard subscriptions")

	// ErrServerNoRetain is returned when publishing with Retain=true but the
	// server declared RetainAvailable=false.
	ErrServerNoRetain = errors.New("mqtt: server does not support retained messages")

	// ErrServerNoSharedSubs is returned when subscribing to a shared subscription
	// topic ($share/...) but the server declared SharedSubscriptionAvailable=false.
	ErrServerNoSharedSubs = errors.New("mqtt: server does not support shared subscriptions")

	// ErrQoSExceedsServerMax is returned when publishing or subscribing with a
	// QoS level higher than the server's declared MaximumQoS.
	ErrQoSExceedsServerMax = errors.New("mqtt: QoS level exceeds server maximum")

	// ErrPacketExceedsServerMax is returned when a packet's estimated size
	// exceeds the server's declared MaximumPacketSize.
	ErrPacketExceedsServerMax = errors.New("mqtt: packet size exceeds server maximum")
)

// MqttError represents an error returned by the MQTT server, including
// the MQTT v5.0 reason code.
type MqttError struct {
	ReasonCode ReasonCode
	Message    string
	Parent     error
}

func (e *MqttError) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("mqtt error (0x%02X): %s", uint8(e.ReasonCode), e.Message)
	}
	if e.Parent != nil {
		return fmt.Sprintf("mqtt error (0x%02X): %s", uint8(e.ReasonCode), e.Parent.Error())
	}
	return fmt.Sprintf("mqtt error (0x%02X)", uint8(e.ReasonCode))
}

func (e *MqttError) Unwrap() error {
	return e.Parent
}

// Is implements the errors.Is interface, allowing checks against ReasonCode constants.
func (e *MqttError) Is(target error) bool {
	if rc, ok := target.(ReasonCode); ok {
		return e.ReasonCode == rc
	}
	return false
}

// DisconnectError represents a DISCONNECT packet received from the server,
// containing potential MQTT v5.0 properties.
type DisconnectError struct {
	ReasonCode            ReasonCode
	ReasonString          string
	SessionExpiryInterval uint32            // 0 if not set
	ServerReference       string            // Empty if not set
	UserProperties        map[string]string // Nil if not set
}

func (e *DisconnectError) Error() string {
	msg := e.ReasonString
	if msg == "" {
		if name, ok := disconnectReasonCodeNames[e.ReasonCode]; ok {
			msg = name
		} else {
			msg = fmt.Sprintf("disconnect code 0x%02X", uint8(e.ReasonCode))
		}
	}
	return fmt.Sprintf("server disconnected: %s", msg)
}

// Is implements the errors.Is interface, allowing checks against ReasonCode constants.
func (e *DisconnectError) Is(target error) bool {
	if rc, ok := target.(ReasonCode); ok {
		return e.ReasonCode == rc
	}
	return false
}
