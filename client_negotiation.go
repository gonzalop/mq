package mq

import (
	"maps"
	"time"

	"github.com/gonzalop/mq/internal/packets"
)

// serverCapabilities holds MQTT v5.0 server capabilities received in CONNACK.
// These are used to validate client operations against server limits.
type serverCapabilities struct {
	// MaximumPacketSize is the maximum packet size the server will accept.
	// 0 means no limit specified by server.
	MaximumPacketSize uint32

	// ReceiveMaximum is the maximum number of QoS 1 and QoS 2 publications
	// the server is willing to process concurrently.
	// Default is 65535 if not specified.
	ReceiveMaximum uint16

	// TopicAliasMaximum is the maximum topic alias value the server accepts.
	// 0 means topic aliases are not supported.
	TopicAliasMaximum uint16

	// MaximumQoS is the maximum QoS level the server supports.
	// Can be 0, 1, or 2.
	MaximumQoS uint8

	// RetainAvailable indicates if the server supports retained messages.
	RetainAvailable bool

	// WildcardAvailable indicates if the server supports wildcard subscriptions.
	WildcardAvailable bool

	// SubscriptionIDAvailable indicates if the server supports subscription identifiers.
	SubscriptionIDAvailable bool

	// SharedSubscriptionAvailable indicates if the server supports shared subscriptions.
	SharedSubscriptionAvailable bool
}

// connectionState holds MQTT v5.0 server capabilities and connection properties
// received in CONNACK.
type connectionState struct {
	// caps holds the protocol-level capabilities.
	caps serverCapabilities

	// assignedClientID is the client ID assigned by the server.
	assignedClientID string

	// serverKeepAlive is the keepalive interval (in seconds) the server wants the client to use.
	serverKeepAlive uint16

	// sessionExpiry is the session expiry interval (in seconds) the server will use.
	sessionExpiry uint32

	// responseInfo is the response information provided by the server.
	responseInfo string

	// serverReference is the server reference URI provided by the server.
	serverReference string
}

// ServerCapabilities represents the capabilities and limits advertised by the MQTT server.
// These are only available when using MQTT v5.0.
type ServerCapabilities struct {
	// MaximumPacketSize is the maximum packet size the server will accept.
	// 0 means no limit was specified by the server.
	MaximumPacketSize uint32

	// ReceiveMaximum is the maximum number of QoS 1 and QoS 2 publications
	// the server is willing to process concurrently.
	ReceiveMaximum uint16

	// TopicAliasMaximum is the maximum topic alias value the server accepts.
	// 0 means topic aliases are not supported by the server.
	TopicAliasMaximum uint16

	// MaximumQoS is the maximum QoS level the server supports (0, 1, or 2).
	MaximumQoS uint8

	// RetainAvailable indicates if the server supports retained messages.
	RetainAvailable bool

	// WildcardAvailable indicates if the server supports wildcard subscriptions.
	WildcardAvailable bool

	// SubscriptionIDAvailable indicates if the server supports subscription identifiers.
	SubscriptionIDAvailable bool

	// SharedSubscriptionAvailable indicates if the server supports shared subscriptions.
	SharedSubscriptionAvailable bool
}

// ServerCapabilities returns the server capabilities received in the CONNACK packet.
// This is only populated for MQTT v5.0 connections.
// For v3.1.1 connections, default values are returned.
func (c *Client) ServerCapabilities() ServerCapabilities {
	state := c.connState.Load()
	if state == nil {
		return ServerCapabilities{}
	}
	caps := state.caps

	return ServerCapabilities{
		MaximumPacketSize:           caps.MaximumPacketSize,
		ReceiveMaximum:              caps.ReceiveMaximum,
		TopicAliasMaximum:           caps.TopicAliasMaximum,
		MaximumQoS:                  caps.MaximumQoS,
		RetainAvailable:             caps.RetainAvailable,
		WildcardAvailable:           caps.WildcardAvailable,
		SubscriptionIDAvailable:     caps.SubscriptionIDAvailable,
		SharedSubscriptionAvailable: caps.SharedSubscriptionAvailable,
	}
}

// ConnectionUserProperties returns the User Properties received from the server
// in the CONNACK packet. These are application-specific key-value pairs provided
// by the server during the connection handshake.
//
// This is only populated for MQTT v5.0 connections.
// Returns a copy of the map to prevent concurrent modification.
func (c *Client) ConnectionUserProperties() map[string]string {
	// We return a copy to avoid race conditions if the map was mutable,
	// though currently it's set once on connect.
	if c.connackUserProperties == nil {
		return nil
	}
	props := make(map[string]string, len(c.connackUserProperties))
	maps.Copy(props, c.connackUserProperties)
	return props
}

// extractServerCapabilities extracts server capabilities from CONNACK properties.
func extractServerCapabilities(props *packets.Properties) serverCapabilities {
	caps := serverCapabilities{
		// Set defaults per MQTT v5.0 spec
		ReceiveMaximum:              65535, // Default if not specified
		MaximumQoS:                  2,     // Default if not specified (supports 0, 1, 2)
		RetainAvailable:             true,  // Default if not specified
		WildcardAvailable:           true,  // Default if not specified
		SubscriptionIDAvailable:     true,  // Default if not specified
		SharedSubscriptionAvailable: true,  // Default if not specified
	}

	if props == nil {
		return caps
	}

	// Extract capabilities from properties
	if props.Presence&packets.PresMaximumPacketSize != 0 {
		caps.MaximumPacketSize = props.MaximumPacketSize
	}

	if props.Presence&packets.PresReceiveMaximum != 0 {
		caps.ReceiveMaximum = props.ReceiveMaximum
	}

	if props.Presence&packets.PresTopicAliasMaximum != 0 {
		caps.TopicAliasMaximum = props.TopicAliasMaximum
	}

	if props.Presence&packets.PresMaximumQoS != 0 {
		caps.MaximumQoS = props.MaximumQoS
	}

	if props.Presence&packets.PresRetainAvailable != 0 {
		caps.RetainAvailable = props.RetainAvailable
	}

	if props.Presence&packets.PresWildcardSubscriptionAvailable != 0 {
		caps.WildcardAvailable = props.WildcardSubscriptionAvailable
	}

	if props.Presence&packets.PresSubscriptionIdentifierAvailable != 0 {
		caps.SubscriptionIDAvailable = props.SubscriptionIdentifierAvailable
	}

	if props.Presence&packets.PresSharedSubscriptionAvailable != 0 {
		caps.SharedSubscriptionAvailable = props.SharedSubscriptionAvailable
	}

	return caps
}

func (c *Client) processConnackProperties(connack *packets.ConnackPacket) {
	if c.opts.ProtocolVersion >= ProtocolV50 && connack.Properties != nil {
		var oldAssignedID string
		if oldState := c.connState.Load(); oldState != nil {
			oldAssignedID = oldState.assignedClientID
		}

		newState := &connectionState{
			caps:             extractServerCapabilities(connack.Properties),
			assignedClientID: oldAssignedID,
		}

		c.opts.Logger.Debug("received server capabilities",
			"max_packet_size", newState.caps.MaximumPacketSize,
			"receive_maximum", newState.caps.ReceiveMaximum,
			"max_qos", newState.caps.MaximumQoS,
			"retain_available", newState.caps.RetainAvailable)

		if connack.Properties.Presence&packets.PresAssignedClientIdentifier != 0 {
			newState.assignedClientID = connack.Properties.AssignedClientIdentifier
			c.opts.ClientID = newState.assignedClientID
			c.opts.Logger.Debug("server assigned client ID", "client_id", newState.assignedClientID)
		}

		if connack.Properties.Presence&packets.PresResponseInformation != 0 {
			newState.responseInfo = connack.Properties.ResponseInformation
			c.opts.Logger.Debug("server provided response information", "response_info", newState.responseInfo)
		}

		if connack.Properties.Presence&packets.PresServerReference != 0 {
			newState.serverReference = connack.Properties.ServerReference
			c.opts.Logger.Debug("server provided redirect reference", "server_reference", newState.serverReference)

			if c.opts.OnServerRedirect != nil {
				go func() {
					c.opts.OnServerRedirect(connack.Properties.ServerReference)
				}()
			}

		}

		if c.opts.TopicAliasMaximum > 0 && connack.Properties.Presence&packets.PresTopicAliasMaximum != 0 {
			serverLimit := connack.Properties.TopicAliasMaximum
			if serverLimit > 0 {
				c.maxAliases = min(serverLimit, c.opts.TopicAliasMaximum)
				c.topicAliases = make(map[string]uint16)
				c.nextAliasID = 1
				c.opts.Logger.Debug("topic aliases enabled",
					"client_accepts", c.opts.TopicAliasMaximum,
					"server_accepts", serverLimit,
					"using", c.maxAliases)
			}
		}

		if connack.Properties.Presence&packets.PresServerKeepAlive != 0 {
			newState.serverKeepAlive = connack.Properties.ServerKeepAlive
			c.opts.KeepAlive = time.Duration(newState.serverKeepAlive) * time.Second
			c.opts.Logger.Debug("server overrode keepalive",
				"requested", uint16(c.requestedKeepAlive.Seconds()),
				"server_keepalive", newState.serverKeepAlive)
		}

		if connack.Properties.Presence&packets.PresSessionExpiryInterval != 0 {
			newState.sessionExpiry = connack.Properties.SessionExpiryInterval
			c.opts.Logger.Debug("server set session expiry",
				"requested", c.requestedSessionExpiry,
				"actual", newState.sessionExpiry)
		} else if c.opts.SessionExpirySet {
			newState.sessionExpiry = c.requestedSessionExpiry
		}

		// Update the connection state atomically
		c.connState.Store(newState)

		// Note: User properties are currently kept in Client for convenience,
		// but they are not accessed concurrently in a way that risks races
		// during normal operation (only updated here).
		if len(connack.Properties.UserProperties) > 0 {
			c.connackUserProperties = make(map[string]string)
			for _, up := range connack.Properties.UserProperties {
				c.connackUserProperties[up.Key] = up.Value
			}
			c.opts.Logger.Debug("received connack user properties", "count", len(c.connackUserProperties))
		}
	} else {
		// Use default capabilities for older protocols or if no properties sent
		c.connState.Store(&connectionState{
			caps: extractServerCapabilities(nil),
		})
		c.connackUserProperties = nil
	}
}
