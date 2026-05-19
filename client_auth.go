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
func (c *Client) Reauthenticate(_ context.Context) error {
	// ctx is currently unused but kept for future use and API consistency.
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

	c.authExchangeCount.Store(0)
	c.outgoing <- authPkt
	c.opts.Logger.Debug("initiated re-authentication", "method", c.opts.Authenticator.Method())

	return nil
}

// handleAuth processes an AUTH packet from the server during authentication exchange.
// Returns a slice of packets to be sent in response.
func (c *Client) handleAuth(p *packets.AuthPacket) []packets.Packet {
	if c.opts.Authenticator == nil {

		c.opts.Logger.Warn("received AUTH packet but no authenticator configured")
		return nil
	}

	// Reset counter on success
	if p.ReasonCode == uint8(ReasonCodeSuccess) {
		c.authExchangeCount.Store(0)
		if err := c.opts.Authenticator.Complete(); err != nil {
			c.opts.Logger.Warn("authenticator completion failed", "error", err)
		}
		return nil
	}

	count := c.authExchangeCount.Add(1)
	if c.opts.MaxAuthExchanges > 0 && count > uint32(c.opts.MaxAuthExchanges) {
		c.opts.Logger.Error("maximum authentication exchanges exceeded", "limit", c.opts.MaxAuthExchanges)
		_ = c.disconnectWithReason(context.Background(), uint8(ReasonCodeBadAuthenticationMethod), nil, false)
		return nil
	}

	var challengeData []byte
	if p.Properties != nil && len(p.Properties.AuthenticationData) > 0 {
		challengeData = p.Properties.AuthenticationData
	}

	// Verify authentication method matches
	if p.Properties != nil && p.Properties.Presence&packets.PresAuthenticationMethod != 0 {
		if p.Properties.AuthenticationMethod != c.opts.Authenticator.Method() {
			c.opts.Logger.Error("authentication method mismatch",
				"expected", c.opts.Authenticator.Method(),
				"received", p.Properties.AuthenticationMethod)
			return nil
		}
	}

	responseData, err := c.opts.Authenticator.HandleChallenge(challengeData, p.ReasonCode)
	if err != nil {
		c.opts.Logger.Error("authentication challenge failed", "error", err)
		// Note: We can't use disconnectWithReason here because we're in logicLoop
		return nil
	}

	// Send AUTH response
	authResp := &packets.AuthPacket{
		ReasonCode: packets.AuthReasonContinue, // Continue authentication
		Properties: &packets.Properties{
			AuthenticationMethod: c.opts.Authenticator.Method(),
			AuthenticationData:   responseData,
			Presence:             packets.PresAuthenticationMethod,
		},
		Version: c.opts.ProtocolVersion,
	}

	return []packets.Packet{authResp}
}
