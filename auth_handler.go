package mq

import (
	"github.com/gonzalop/mq/internal/packets"
)

// handleAuth processes an AUTH packet from the server during authentication exchange.
func (c *Client) handleAuth(p *packets.AuthPacket) {
	if c.opts.Authenticator == nil {
		c.opts.Logger.Warn("received AUTH packet but no authenticator configured")
		return
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
			return
		}
	}

	responseData, err := c.opts.Authenticator.HandleChallenge(challengeData, p.ReasonCode)
	if err != nil {
		c.opts.Logger.Error("authentication challenge failed", "error", err)
		// Note: We can't use disconnectWithReason here because we're in logicLoop
		return
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

	c.outgoing <- authResp
	c.opts.Logger.Debug("sent AUTH response", "reason_code", authResp.ReasonCode)
}
