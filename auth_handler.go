package mq

import (
	"context"

	"github.com/gonzalop/mq/internal/packets"
)

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
