package mq

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/url"
	"time"

	"github.com/gonzalop/mq/internal/packets"
)

// dialServer establishes a TCP, TLS, or custom connection to the MQTT server.
func (c *Client) dialServer(ctx context.Context) (net.Conn, error) {
	// If a custom dialer is provided, trust it to handle the scheme and address.
	// Pass the raw server string as the address to allow flexibility (e.g. WebSocket paths).
	if c.opts.Dialer != nil {
		network := "tcp"
		if u, err := url.Parse(c.opts.Server); err == nil && u.Scheme != "" {
			network = u.Scheme
		}

		conn, err := c.opts.Dialer.DialContext(ctx, network, c.opts.Server)
		if err != nil {
			return nil, fmt.Errorf("custom dialer failed: %w", err)
		}
		return conn, nil
	}

	u, err := url.Parse(c.opts.Server)
	if err != nil {
		return nil, fmt.Errorf("invalid server URL: %w", err)
	}

	if u.Port() == "" {
		switch u.Scheme {
		case "tls", "ssl", "mqtts":
			u.Host = net.JoinHostPort(u.Host, "8883")
		case "tcp", "mqtt", "":
			u.Host = net.JoinHostPort(u.Host, "1883")
		}
	}

	useTLS := u.Scheme == "tls" || u.Scheme == "ssl" || u.Scheme == "mqtts" || c.opts.TLSConfig != nil
	if !useTLS && u.Scheme != "tcp" && u.Scheme != "mqtt" {
		return nil, fmt.Errorf("unsupported scheme: %s (supported: tcp, mqtt, tls, ssl, mqtts)", u.Scheme)
	}

	var conn net.Conn
	if useTLS {
		tlsConfig := c.opts.TLSConfig
		if tlsConfig == nil {
			tlsConfig = &tls.Config{}
		}
		dialer := &tls.Dialer{
			NetDialer: &net.Dialer{},
			Config:    tlsConfig,
		}
		conn, err = dialer.DialContext(ctx, "tcp", u.Host)
	} else {
		var d net.Dialer
		conn, err = d.DialContext(ctx, "tcp", u.Host)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to connect to server: %w", err)
	}

	return conn, nil
}

// buildConnectPacket creates a CONNECT packet with the client's configuration.
func (c *Client) buildConnectPacket() *packets.ConnectPacket {
	// Use the original requested keepalive, not the potentially server-overridden value
	keepalive := c.requestedKeepAlive
	if keepalive == 0 {
		keepalive = c.opts.KeepAlive
	}

	pkt := &packets.ConnectPacket{
		ProtocolName:  "MQTT",
		ProtocolLevel: c.opts.ProtocolVersion,
		CleanSession:  c.opts.CleanSession,
		KeepAlive:     uint16(keepalive.Seconds()),
		ClientID:      c.opts.ClientID,
	}

	// Add MQTT v5.0 properties if using v5.0
	if c.opts.ProtocolVersion >= ProtocolV50 {
		pkt.Properties = &packets.Properties{}

		if c.opts.RequestProblemInformation {
			pkt.Properties.RequestProblemInformation = 1
			pkt.Properties.Presence |= packets.PresRequestProblemInformation
		}

		if c.opts.RequestResponseInformation {
			pkt.Properties.RequestResponseInformation = 1
			pkt.Properties.Presence |= packets.PresRequestResponseInformation
		}

		if c.opts.TopicAliasMaximum > 0 {
			pkt.Properties.TopicAliasMaximum = c.opts.TopicAliasMaximum
			pkt.Properties.Presence |= packets.PresTopicAliasMaximum
		}

		if c.opts.SessionExpirySet {
			pkt.Properties.SessionExpiryInterval = c.opts.SessionExpiryInterval
			pkt.Properties.Presence |= packets.PresSessionExpiryInterval
		}

		if c.opts.ReceiveMaximum > 0 {
			pkt.Properties.ReceiveMaximum = c.opts.ReceiveMaximum
			pkt.Properties.Presence |= packets.PresReceiveMaximum
		}

		if c.opts.MaxIncomingPacket > 0 {
			pkt.Properties.MaximumPacketSize = uint32(c.opts.MaxIncomingPacket)
			pkt.Properties.Presence |= packets.PresMaximumPacketSize
		}

		if len(c.opts.ConnectUserProperties) > 0 {
			pkt.Properties.UserProperties = make([]packets.UserProperty, 0, len(c.opts.ConnectUserProperties))
			for k, v := range c.opts.ConnectUserProperties {
				pkt.Properties.UserProperties = append(pkt.Properties.UserProperties, packets.UserProperty{
					Key:   k,
					Value: v,
				})
			}
		}

		if c.opts.Authenticator != nil {
			pkt.Properties.AuthenticationMethod = c.opts.Authenticator.Method()
			pkt.Properties.Presence |= packets.PresAuthenticationMethod

			initialData, err := c.opts.Authenticator.InitialData()
			if err != nil {
				c.opts.Logger.Error("failed to get initial auth data", "error", err)
			} else if len(initialData) > 0 {
				pkt.Properties.AuthenticationData = initialData
			}
		}
	}

	if c.opts.Username != "" {
		pkt.UsernameFlag = true
		pkt.Username = c.opts.Username
	}
	if c.opts.Password != "" {
		pkt.PasswordFlag = true
		pkt.Password = c.opts.Password
	}

	if c.opts.will != nil {
		pkt.WillFlag = true
		pkt.WillTopic = c.opts.will.Topic
		pkt.WillMessage = c.opts.will.Payload
		pkt.WillQoS = c.opts.will.QoS
		pkt.WillRetain = c.opts.will.Retained

		if c.opts.will.Properties != nil {
			pkt.WillProperties = toInternalProperties(c.opts.will.Properties)
		}
	}

	return pkt
}

// sendConnectPacket builds and sends the CONNECT packet.
func (c *Client) sendConnectPacket(w io.Writer) error {
	connectPkt := c.buildConnectPacket()
	if _, err := connectPkt.WriteTo(w); err != nil {
		return fmt.Errorf("failed to send CONNECT: %w", err)
	}
	c.packetsSent.Add(1)
	return nil
}

// validateConnack checks the CONNACK return code and returns a detailed error if rejected.
func (c *Client) validateConnack(conn net.Conn, connack *packets.ConnackPacket) error {
	if connack.ReturnCode == packets.ConnAccepted {
		return nil
	}

	conn.Close()

	if c.opts.ProtocolVersion >= ProtocolV50 {
		err := &MqttError{
			ReasonCode: ReasonCode(connack.ReturnCode),
			Parent:     ErrConnectionRefused,
		}
		if connack.Properties != nil && connack.Properties.Presence&packets.PresReasonString != 0 {
			err.Message = connack.Properties.ReasonString
		}
		return err
	}

	switch connack.ReturnCode {
	case packets.ConnRefusedUnacceptableProtocol:
		return ErrUnacceptableProtocolVersion
	case packets.ConnRefusedIdentifierRejected:
		return ErrIdentifierRejected
	case packets.ConnRefusedServerUnavailable:
		return ErrServerUnavailable
	case packets.ConnRefusedBadUsernameOrPassword:
		return ErrBadUsernameOrPassword
	case packets.ConnRefusedNotAuthorized:
		return ErrNotAuthorized
	default:
		return fmt.Errorf("%w: code %d", ErrConnectionRefused, connack.ReturnCode)
	}
}

// finalizeConnection processes CONNACK properties and starts background loops.
func (c *Client) finalizeConnection(connack *packets.ConnackPacket) {
	// Reset keepalive to requested value before processing server override
	c.opts.KeepAlive = c.requestedKeepAlive

	c.processConnackProperties(connack)

	if !c.opts.CleanSession {
		if err := c.checkSessionPresent(connack.SessionPresent); err != nil {
			c.opts.Logger.Warn("failed to check session present", "error", err)
		}
	}

	c.opts.Logger.Debug("connection established", "server", c.opts.Server)
	c.connected.Store(true)

	if c.opts.Authenticator != nil {
		if err := c.opts.Authenticator.Complete(); err != nil {
			c.opts.Logger.Warn("authenticator complete failed", "error", err)
		}
	}

	if c.opts.OnConnect != nil {
		go func() {
			c.opts.OnConnect(c)
		}()
	}
	c.wg.Add(2)
	c.activeLoops.Add(2)
	go c.readLoop()
	go c.writeLoop()

	c.opts.Logger.Debug("client started", "client_id", c.opts.ClientID)
}

// prepareConnectionState resets internal state before a connection attempt.
func (c *Client) prepareConnectionState() {
	if c.requestedKeepAlive == 0 {
		c.requestedKeepAlive = c.opts.KeepAlive
	}
	if c.requestedSessionExpiry == 0 && c.opts.SessionExpirySet {
		c.requestedSessionExpiry = c.opts.SessionExpiryInterval
	}

	c.resetAllTopicAliases()
	c.receivedAliasesLock.Lock()
	c.receivedAliases = make(map[uint16]string)
	c.receivedAliasesLock.Unlock()
	c.receiveMaxExceededLogged = false
}

func (c *Client) performHandshake(ctx context.Context, r io.Reader, w io.Writer) (connack *packets.ConnackPacket, err error) {
	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(c.opts.ConnectTimeout)
	}

	c.connLock.RLock()
	conn := c.conn
	c.connLock.RUnlock()
	_ = conn.SetReadDeadline(deadline)
	defer func() { _ = conn.SetReadDeadline(time.Time{}) }()

	c.authExchangeCount.Store(0)

	for {
		pkt, err := packets.ReadPacket(r, c.opts.ProtocolVersion, c.opts.MaxIncomingPacket)
		if err != nil {
			conn.Close()
			return nil, fmt.Errorf("failed to read packet: %w", err)
		}
		c.packetsReceived.Add(1)

		switch p := pkt.(type) {
		case *packets.ConnackPacket:
			return p, nil

		case *packets.AuthPacket:
			if c.opts.ProtocolVersion < ProtocolV50 {
				conn.Close()
				return nil, fmt.Errorf("received AUTH packet in v3.1.1")
			}
			if c.opts.Authenticator == nil {
				conn.Close()
				return nil, fmt.Errorf("received AUTH packet but no authenticator configured")
			}

			count := c.authExchangeCount.Add(1)
			if c.opts.MaxAuthExchanges > 0 && count > uint32(c.opts.MaxAuthExchanges) {
				conn.Close()
				return nil, fmt.Errorf("maximum authentication exchanges (%d) exceeded", c.opts.MaxAuthExchanges)
			}

			respData, err := c.opts.Authenticator.HandleChallenge(p.Properties.AuthenticationData, p.ReasonCode)
			if err != nil {
				conn.Close()
				return nil, fmt.Errorf("authentication failed: %w", err)
			}

			// Send AUTH response
			authResp := &packets.AuthPacket{
				Version:    ProtocolV50,
				ReasonCode: packets.AuthReasonContinue, // 0x18
				Properties: &packets.Properties{
					AuthenticationMethod: c.opts.Authenticator.Method(),
					AuthenticationData:   respData,
				},
			}

			if _, err := authResp.WriteTo(w); err != nil {
				conn.Close()
				return nil, fmt.Errorf("failed to send AUTH response: %w", err)
			}
			c.packetsSent.Add(1)

		default:
			conn.Close()
			return nil, fmt.Errorf("expected CONNACK or AUTH, got packet type %d", pkt.Type())
		}
	}
}
