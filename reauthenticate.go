package mq

import (
	"context"
	"fmt"

	"github.com/gonzalop/mq/internal/packets"
)

// Reauthenticate initiates re-authentication with the server (MQTT v5.0).
//
// This sends an AUTH packet with reason code 0x19 (Re-authenticate) to start
// a new authentication exchange. The authenticator's HandleChallenge method
// will be called for each challenge from the server.
//
// Re-authentication is useful for:
//   - Refreshing expired tokens
//   - Rotating credentials
//   - Periodic security validation
//
// Note: The Client and Server can continue to send and receive other Control
// Packets (such as PUBLISH) during the re-authentication exchange. The
// connection remains fully functional.
//
// This method returns immediately. Authentication happens asynchronously.
// Use the authenticator's Complete method to know when it succeeds.
//
// Returns an error if:
//   - Not using MQTT v5.0
//   - No authenticator configured
//   - Not connected
//
// Example:
//
//	// Refresh authentication periodically
//	ticker := time.NewTicker(30 * time.Minute)
//	go func() {
//	    for range ticker.C {
//	        if err := client.Reauthenticate(context.Background()); err != nil {
//	            log.Printf("Re-authentication failed: %v", err)
//	        }
//	    }
//	}()
func (c *Client) Reauthenticate(ctx context.Context) error {
	if c.opts.ProtocolVersion < ProtocolV50 {
		return fmt.Errorf("re-authentication requires MQTT v5.0")
	}

	if c.opts.Authenticator == nil {
		return fmt.Errorf("no authenticator configured")
	}

	if !c.IsConnected() {
		return fmt.Errorf("not connected")
	}

	// Get initial data for re-auth
	initialData, err := c.opts.Authenticator.InitialData()
	if err != nil {
		return fmt.Errorf("failed to get re-auth data: %w", err)
	}

	// Send AUTH packet with Re-authenticate reason code
	authPkt := &packets.AuthPacket{
		ReasonCode: packets.AuthReasonReauthenticate, // Re-authenticate (0x19)
		Properties: &packets.Properties{
			AuthenticationMethod: c.opts.Authenticator.Method(),
			AuthenticationData:   initialData,
			Presence:             packets.PresAuthenticationMethod,
		},
		Version: c.opts.ProtocolVersion,
	}

	c.outgoing <- authPkt
	c.opts.Logger.Debug("initiated re-authentication", "method", c.opts.Authenticator.Method())

	return nil
}
