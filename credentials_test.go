package mq

import (
	"strings"
	"testing"
)

// TestPasswordWithoutUsernameIsRejected checks [MQTT-3.1.2-22]: the password
// flag requires the username flag, so a password without a username must not
// be sent as a (malformed) CONNECT.
func TestPasswordWithoutUsernameIsRejected(t *testing.T) {
	_, err := Dial("tcp://127.0.0.1:1",
		WithClientID("password-without-username"),
		WithCredentials("", "secret"),
		WithAutoReconnect(false),
	)
	if err == nil {
		t.Fatal("expected an error for a password without a username")
	}
	if !strings.Contains(err.Error(), "username") {
		t.Fatalf("unexpected error: %v", err)
	}
}
