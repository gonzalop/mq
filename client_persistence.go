package mq

import (
	"fmt"
	"time"

	"github.com/gonzalop/mq/internal/packets"
)

// loadSessionState loads the persisted session state into the client.
// This must be called BEFORE the CONNECT packet is sent.
func (c *Client) loadSessionState() error {
	if c.opts.SessionStore == nil {
		return nil
	}

	c.opts.Logger.Debug("loading persistent session state")

	// 1. Load Pending Publishes
	pending, err := c.opts.SessionStore.LoadPendingPublishes()
	if err != nil {
		return fmt.Errorf("failed to load pending publishes: %w", err)
	}

	c.pending = make(map[uint16]*pendingOp)
	c.inFlightCount = 0
	for id, pub := range pending {
		op := c.convertFromPersistedPublish(pub)
		if pkt, ok := op.packet.(*packets.PublishPacket); ok {
			pkt.PacketID = id // Restore PacketID from map key
			if pkt.QoS > 0 {
				c.inFlightCount++
			}
		}
		c.pending[id] = op
	}

	// 2. Load Subscriptions
	// note: handlers are lost, but we restore the subscription state
	// so we know what topics we are subscribed to.
	subs, err := c.opts.SessionStore.LoadSubscriptions()
	if err != nil {
		return fmt.Errorf("failed to load subscriptions: %w", err)
	}

	if c.subscriptions == nil {
		c.subscriptions = make(map[string]subscriptionEntry)
	}

	for topic, sub := range subs {
		entry := c.convertFromPersistedSubscription(sub)
		if handler, ok := c.opts.InitialSubscriptions[topic]; ok {
			entry.handler = handler
		}
		c.subscriptions[topic] = entry
	}

	// 3. Load Received QoS 2 IDs
	qos2, err := c.opts.SessionStore.LoadReceivedQoS2()
	if err != nil {
		return fmt.Errorf("failed to load qos2 IDs: %w", err)
	}
	c.receivedQoS2 = qos2

	c.opts.Logger.Info("loaded session state",
		"pending", len(c.pending),
		"subscriptions", len(c.subscriptions),
		"qos2_received", len(c.receivedQoS2))

	return nil
}

// checkSessionPresent handles the Session Present flag from CONNACK.
// If valid, it keeps the loaded state.
// If invalid (false), it clears stale persistent state and resubscribes.
//
// NOTE: This runs in the connection/reconnection loop.
func (c *Client) checkSessionPresent(sessionPresent bool) error {
	if sessionPresent {
		c.opts.Logger.Debug("session present, keeping loaded state")
		return nil
	}

	c.opts.Logger.Debug("session not present (clean start), clearing stale state and resubscribing")

	// 1. Clear Stale Persistence State (Server doesn't know about it)
	// Only clear ephemeral state like QoS 2 received IDs.
	// Pending publishes and subscriptions are preserved for re-delivery/re-subscription.
	if c.opts.SessionStore != nil {
		if err := c.opts.SessionStore.DeleteReceivedQoS2(0); err != nil {
			c.opts.Logger.Warn("failed to clear stale QoS2 IDs", "error", err)
		}
	}

	// 2. Trigger Logic Loop Reset
	// Safely clears c.receivedQoS2.
	c.internalResetState()

	// 3. Resubscribe to subscriptions added via WithSubscription
	go c.resubscribeAll()

	return nil
}

// --- Conversion Helpers ---

func (c *Client) convertToPersistedPublish(req *publishRequest) *PersistedPublish {
	return &PersistedPublish{
		Topic:   req.packet.Topic,
		Payload: req.packet.Payload,
		QoS:     req.packet.QoS,
		Retain:  req.packet.Retain,
	}
}

func (c *Client) convertFromPersistedPublish(p *PersistedPublish) *pendingOp {
	// Reconstruct the pending operation
	pkt := &packets.PublishPacket{
		Topic:    p.Topic,
		Payload:  p.Payload,
		QoS:      p.QoS,
		Retain:   p.Retain,
		PacketID: 0, // Will be set by caller
	}

	return &pendingOp{
		packet:    pkt,
		token:     newToken(),
		qos:       p.QoS,
		timestamp: time.Now(), // Reset timestamp
	}
}

func (c *Client) convertToPersistedSubscription(entry subscriptionEntry) *PersistedSubscription {
	return &PersistedSubscription{
		QoS: entry.qos,
		Options: &PersistedSubscriptionOptions{
			NoLocal:           entry.options.NoLocal,
			RetainAsPublished: entry.options.RetainAsPublished,
			RetainHandling:    entry.options.RetainHandling,
		},
	}
}

func (c *Client) convertFromPersistedSubscription(sub *PersistedSubscription) subscriptionEntry {
	opts := SubscribeOptions{}
	if sub.Options != nil {
		opts.NoLocal = sub.Options.NoLocal
		opts.RetainAsPublished = sub.Options.RetainAsPublished
		opts.RetainHandling = sub.Options.RetainHandling
	}

	return subscriptionEntry{
		qos:     sub.QoS,
		options: opts,
		// handler is set by caller if available in the initial subscriptions
	}
}
