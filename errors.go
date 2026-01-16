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
)

// MqttError represents an error returned by the MQTT server, including
// the MQTT v5.0 reason code.
type MqttError struct {
	ReasonCode uint8
	Message    string
	Parent     error
}

func (e *MqttError) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("mqtt error (0x%02X): %s", e.ReasonCode, e.Message)
	}
	if e.Parent != nil {
		return fmt.Sprintf("mqtt error (0x%02X): %s", e.ReasonCode, e.Parent.Error())
	}
	return fmt.Sprintf("mqtt error (0x%02X)", e.ReasonCode)
}

func (e *MqttError) Unwrap() error {
	return e.Parent
}

// IsReasonCode checks if the error is an MqttError with the specified reason code.
func IsReasonCode(err error, code uint8) bool {
	var mErr *MqttError
	if errors.As(err, &mErr) {
		return mErr.ReasonCode == code
	}
	return false
}
