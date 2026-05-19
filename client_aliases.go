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
//	client.Publish("sensors/building-a/floor-3/room-42/temperature", data,
//	    mq.WithAlias())
//
//	// Subsequent publishes - automatically uses alias (saves ~50 bytes)
//	client.Publish("sensors/building-a/floor-3/room-42/temperature", data,
//	    mq.WithAlias())
func WithAlias() PublishOption {
	return func(o *PublishOptions) {
		o.UseAlias = true
	}
}

// applyTopicAlias applies topic alias optimization to a publish packet.
// This is called automatically when WithAlias() is used.
//
// On first publish to a topic:
//   - Assigns a new alias ID
//   - Sends both topic and alias
//
// On subsequent publishes:
//   - Uses existing alias
//   - Sends empty topic (bandwidth savings)
//
// If alias limit is reached, gracefully falls back to sending full topic.
func (c *Client) applyTopicAlias(pkt *packets.PublishPacket) {
	c.topicAliasesLock.Lock()
	defer c.topicAliasesLock.Unlock()

	// Check if aliases are disabled
	if c.maxAliases == 0 {
		return
	}

	// Preserve original topic if not already set
	if pkt.OriginalTopic == "" {
		pkt.OriginalTopic = pkt.Topic
	} else if pkt.Topic == "" {
		// Restore topic for re-evaluation
		pkt.Topic = pkt.OriginalTopic
	}

	// Check if we already have an alias for this topic
	if aliasID, exists := c.topicAliases[pkt.Topic]; exists {
		// Use existing alias - send empty topic
		if pkt.Properties == nil {
			pkt.Properties = &packets.Properties{}
		}
		pkt.Properties.TopicAlias = aliasID
		pkt.Properties.Presence |= packets.PresTopicAlias
		pkt.Topic = "" // Empty topic when using alias
		c.opts.Logger.Debug("using topic alias", "alias_id", aliasID)
		return
	}

	// Check if we can allocate a new alias
	if c.nextAliasID > c.maxAliases {
		// At limit - just send full topic (graceful degradation)
		c.opts.Logger.Debug("topic alias limit reached, sending full topic",
			"limit", c.maxAliases)
		return
	}

	// Allocate new alias
	aliasID := c.nextAliasID
	c.nextAliasID++
	c.topicAliases[pkt.Topic] = aliasID

	// Send both topic and alias on first use
	if pkt.Properties == nil {
		pkt.Properties = &packets.Properties{}
	}
	pkt.Properties.TopicAlias = aliasID
	pkt.Properties.Presence |= packets.PresTopicAlias
	// Keep pkt.Topic as-is for first message
	c.opts.Logger.Debug("assigned new topic alias",
		"topic", pkt.Topic,
		"alias_id", aliasID,
		"total_aliases", len(c.topicAliases))
}

// resetPacketTopicAlias restores the original topic and removes the alias.
func (c *Client) resetPacketTopicAlias(pkt *packets.PublishPacket) {
	if pkt.OriginalTopic != "" {
		pkt.Topic = pkt.OriginalTopic
	}
	if pkt.Properties != nil {
		pkt.Properties.TopicAlias = 0
		pkt.Properties.Presence &= ^packets.PresTopicAlias
	}
}

// resetAllTopicAliases clears all topic alias state and resets all queued packets.
func (c *Client) resetAllTopicAliases() {
	c.topicAliasesLock.Lock()
	c.topicAliases = make(map[string]uint16)
	c.nextAliasID = 1
	c.maxAliases = 0
	c.topicAliasesLock.Unlock()

	c.sessionLock.Lock()
	defer c.sessionLock.Unlock()

	// 1. Reset pending packets (QoS > 0)
	for _, op := range c.pending {
		if pub, ok := op.packet.(*packets.PublishPacket); ok {
			c.resetPacketTopicAlias(pub)
		}
	}

	// 2. Reset publish queue (flow control)
	for _, req := range c.publishQueue {
		c.resetPacketTopicAlias(req.packet)
	}

	// 3. Reset outgoing channel (mostly QoS 0)
	// We drain and re-queue to ensure no stale aliases remain.
	count := len(c.outgoing)
	for range count {
		select {
		case pkt := <-c.outgoing:
			if pub, ok := pkt.(*packets.PublishPacket); ok {
				c.resetPacketTopicAlias(pub)
			}
			// Re-queue. Since we are holding sessionLock, logicLoop won't
			// be pushing more packets that could fill the channel and block us.
			c.outgoing <- pkt
		default:
			// Channel was drained by something else
		}
	}
}
