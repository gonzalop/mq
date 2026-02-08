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
	for i := 0; i < count; i++ {
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
