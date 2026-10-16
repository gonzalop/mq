package mq

import "github.com/gonzalop/mq/internal/packets"

// WithAlias enables topic alias optimization for this publish.
//
// Only applicable for MQTT v5.0 when WithTopicAliasMaximum() is set.
// Topic aliases allow the client to send a short alias ID instead of the
// full topic name, reducing bandwidth usage for frequently published topics.
//
// On the first publish to a topic with WithAlias():
//   - Sends full topic name + assigns an alias ID
//   - Subsequent publishes automatically use the alias (sends empty topic)
//
// The library automatically manages alias allocation and tracking.
// If the alias limit is reached, gracefully falls back to sending the full topic.
//
// Example:
//
//	client, _ := mq.Dial("tcp://localhost:1883",
//	    mq.WithProtocolVersion(mq.ProtocolV50),
//	    mq.WithTopicAliasMaximum(100))
//
//	// First publish - sends full topic + assigns alias
//	client.Publish(context.Background(), "sensors/building-a/floor-3/room-42/temperature", data,
//	    mq.WithAlias())
//
//	// Subsequent publishes - automatically uses alias (saves ~50 bytes)
//	client.Publish(context.Background(), "sensors/building-a/floor-3/room-42/temperature", data,
//	    mq.WithAlias())
func WithAlias() PublishOption {
	return func(o *PublishOptions) {
		o.UseAlias = true
	}
}

// topicAliases assigns client → server topic aliases (MQTT v5.0).
//
// Each write loop owns one, so aliases are assigned in the order packets reach
// the wire (the packet that defines an alias is always written before the
// packets that use it) and start fresh on every connection.
type topicAliases struct {
	max uint16
	ids map[string]uint16
}

func newTopicAliases(limit uint16) *topicAliases {
	return &topicAliases{max: limit, ids: make(map[string]uint16)}
}

// prepare returns the packet to write: pkt itself, or for a PUBLISH sent with
// WithAlias a copy that carries the alias. The first copy for a topic has both
// the topic and the alias; later ones only the alias. When the limit is
// reached the full topic is sent. pkt is never modified, so a stored packet
// keeps its topic for persistence and retransmission.
func (t *topicAliases) prepare(pkt packets.Packet) packets.Packet {
	pub, ok := pkt.(*packets.PublishPacket)
	if !ok || !pub.UseAlias || t.max == 0 {
		return pkt
	}

	id, known := t.ids[pub.Topic]
	if !known {
		if len(t.ids) >= int(t.max) {
			return pkt
		}
		id = uint16(len(t.ids) + 1)
		t.ids[pub.Topic] = id
	}

	out := *pub
	var props packets.Properties
	if pub.Properties != nil {
		props = *pub.Properties
	}
	props.TopicAlias = id
	props.Presence |= packets.PresTopicAlias
	out.Properties = &props
	if known {
		out.Topic = ""
	}
	return &out
}

// outgoingAliasLimit returns how many topic aliases the client may use on the
// current connection: the smaller of the server's Topic Alias Maximum from
// CONNACK and the client's WithTopicAliasMaximum (0 disables aliases).
func (c *Client) outgoingAliasLimit() uint16 {
	if c.opts.ProtocolVersion < ProtocolV50 || c.opts.TopicAliasMaximum == 0 {
		return 0
	}
	state := c.connState.Load()
	if state == nil {
		return 0
	}
	return min(state.caps.TopicAliasMaximum, c.opts.TopicAliasMaximum)
}
