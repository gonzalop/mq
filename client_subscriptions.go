package mq

type subscriptionEntry struct {
	handler MessageHandler
	options SubscribeOptions
	qos     uint8
}

// addSubscriptionLocked adds a subscription to both the map and the trie.
// Assumes sessionLock is HELD.
func (c *Client) addSubscriptionLocked(topic string, entry subscriptionEntry) {
	c.subscriptions[topic] = entry
	if entry.handler != nil {
		c.trie.insert(topic, entry.handler)
	}
}

// removeSubscriptionLocked removes a subscription from both the map and the trie.
// Assumes sessionLock is HELD.
func (c *Client) removeSubscriptionLocked(topic string) {
	delete(c.subscriptions, topic)
	c.trie.remove(topic)
}

// wrapHandler applies handler interceptors to a MessageHandler.
func (c *Client) wrapHandler(handler MessageHandler) MessageHandler {
	if handler == nil || c.opts == nil {
		return handler
	}
	return applyHandlerInterceptors(handler, c.opts.HandlerInterceptors)
}
