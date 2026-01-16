package mq

import "github.com/gonzalop/mq/internal/packets"

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
