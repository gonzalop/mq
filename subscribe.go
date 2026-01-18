package mq

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/gonzalop/mq/internal/packets"
)

// SubscribeOptions holds configuration for a subscription.
type SubscribeOptions struct {
	NoLocal           bool
	RetainAsPublished bool
	RetainHandling    uint8
	Persistence       bool              // Persistence enabled by default (must be manually set to true by default logic)
	SubscriptionID    int               // MQTT v5.0: Subscription identifier (1-268435455, 0 = none).
	UserProperties    map[string]string // MQTT v5.0: User properties
}

// SubscribeOption is a functional option for configuring a subscription.
type SubscribeOption func(*SubscribeOptions)

// WithSubscribeUserProperty (MQTT v5.0) adds a user property to the subscription.
// User properties are key-value pairs that can be used to send metadata to the server.
//
// This option is ignored when using MQTT v3.1.1.
func WithSubscribeUserProperty(key, value string) SubscribeOption {
	return func(o *SubscribeOptions) {
		if o.UserProperties == nil {
			o.UserProperties = make(map[string]string)
		}
		o.UserProperties[key] = value
	}
}

// WithPersistence sets whether the subscription should be persisted to the session store.
// If true (default), the subscription is saved and restored on process restart.
// If false, the subscription is ephemeral and lost on client restart.
// This is independent of the MQTT CleanSession/CleanStart flag which controls server-side persistence.
func WithPersistence(persistence bool) SubscribeOption {
	return func(o *SubscribeOptions) {
		o.Persistence = persistence
	}
}

// WithNoLocal (MQTT v5.0) prevents the server from sending messages published by this client
// back to this client.
//
// This option is ignored when using MQTT v3.1.1.
func WithNoLocal(noLocal bool) SubscribeOption {
	return func(o *SubscribeOptions) {
		o.NoLocal = noLocal
	}
}

// WithRetainAsPublished (MQTT v5.0) requests that the server keeps the Retain flag
// as set by the publisher when forwarding the message.
//
// This option is ignored when using MQTT v3.1.1.
func WithRetainAsPublished(retain bool) SubscribeOption {
	return func(o *SubscribeOptions) {
		o.RetainAsPublished = retain
	}
}

// WithRetainHandling (MQTT v5.0) specifies when retained messages are sent.
// 0 = Send retained messages at time of subscribe (default)
// 1 = Send retained messages at subscribe only if subscription doesn't exist
// 2 = Do not send retained messages at time of subscribe
//
// This option is ignored when using MQTT v3.1.1.
func WithRetainHandling(handling uint8) SubscribeOption {
	return func(o *SubscribeOptions) {
		o.RetainHandling = handling
	}
}

// WithSubscriptionIdentifier (MQTT v5.0) sets a subscription identifier for this subscription.
// The identifier will be included in PUBLISH packets that match this subscription,
// allowing the application to determine which subscription(s) matched the message.
//
// Subscription identifiers must be in the range 1-268,435,455.
// A value of 0 means no identifier (default).
//
// This is useful when:
//   - Multiple subscriptions have overlapping topic filters
//   - You need to route messages differently based on which subscription matched
//   - Implementing complex message routing logic
//
// The subscription identifier is available in received messages via
// msg.Properties.SubscriptionIdentifier (a slice, as multiple subscriptions may match).
//
// This option is ignored when using MQTT v3.1.1.
//
// Example:
//
//	client.Subscribe("sensors/+/temp", 1, tempHandler,
//	    mq.WithSubscriptionIdentifier(100))
func WithSubscriptionIdentifier(id int) SubscribeOption {
	return func(o *SubscribeOptions) {
		o.SubscriptionID = id
	}
}

// Subscribe subscribes to a topic with the specified QoS level.
//
// The handler function is called for each message received on topics matching
// the subscription filter. If a message matches multiple subscription filters,
// the handlers for all matching subscriptions will be called.
//
// The handler is called in a separate goroutine, so it should not block for
// long periods.
//
// Topic filters support MQTT wildcards:
//   - '+' matches a single level (e.g., "sensors/+/temperature")
//   - '#' matches multiple levels (e.g., "sensors/#")
//
// The function returns a Token that completes when the subscription is
// acknowledged by the server.
//
// For persistent sessions (CleanSession=false), it is recommended to use the
// mq.WithSubscription option during Dial instead. This ensures handlers are
// automatically re-registered if the session is lost and the client must
// re-subscribe.
//
// Example (simple subscription):
//
//	token := client.Subscribe("sensors/temperature", 1,
//	    func(c *mq.Client, msg mq.Message) {
//	        fmt.Printf("Temperature: %s\n", string(msg.Payload))
//	    })
//	if err := token.Wait(context.Background()); err != nil {
//	    log.Fatal(err)
//	}
//
// Example with options (MQTT v5.0):
//
//	client.Subscribe("chat/room", 1, handler, mq.WithNoLocal(true))
func (c *Client) Subscribe(topic string, qos QoS, handler MessageHandler, opts ...SubscribeOption) Token {
	c.opts.Logger.Debug("subscribing to topic", "topic", topic, "qos", qos)

	if err := validateSubscribeTopic(topic, c.opts); err != nil {
		tok := newToken()
		tok.complete(fmt.Errorf("invalid topic filter: %w", err))
		return tok
	}

	subOpts := &SubscribeOptions{
		Persistence: true,
	}
	for _, opt := range opts {
		opt(subOpts)
	}

	// Validate subscription ID (MQTT v5.0)
	if subOpts.SubscriptionID != 0 && (subOpts.SubscriptionID < 1 || subOpts.SubscriptionID > 268435455) {
		tok := newToken()
		tok.complete(fmt.Errorf("subscription identifier must be in range 0-268435455, got %d", subOpts.SubscriptionID))
		return tok
	}

	// Validate Shared Subscription constraints (MQTT v5.0)
	// it is a Protocol Error to set the No Local option to 1 on a Shared Subscription
	if subOpts.NoLocal && strings.HasPrefix(topic, "$share/") {
		tok := newToken()
		tok.complete(fmt.Errorf("protocol error: NoLocal cannot be set on a Shared Subscription"))
		return tok
	}

	pkt := &packets.SubscribePacket{
		PacketID:          0, // Assigned by internalSubscribe
		Topics:            []string{topic},
		QoS:               []uint8{uint8(qos)},
		NoLocal:           []bool{subOpts.NoLocal},
		RetainAsPublished: []bool{subOpts.RetainAsPublished},
		RetainHandling:    []uint8{subOpts.RetainHandling},
		Version:           c.opts.ProtocolVersion,
	}

	if c.opts.ProtocolVersion >= ProtocolV50 {
		props := &packets.Properties{}
		hasProps := false

		if subOpts.SubscriptionID > 0 {
			props.SubscriptionIdentifier = []int{subOpts.SubscriptionID}
			hasProps = true
		}
		if len(subOpts.UserProperties) > 0 {
			for k, v := range subOpts.UserProperties {
				props.UserProperties = append(props.UserProperties, packets.UserProperty{
					Key:   k,
					Value: v,
				})
			}
			hasProps = true
		}

		if hasProps {
			pkt.Properties = props
		}
	}

	tok := newToken()

	req := &subscribeRequest{
		packet:      pkt,
		handler:     handler,
		token:       tok,
		persistence: subOpts.Persistence,
	}

	c.internalSubscribe(req)

	return tok
}

// Unsubscribe unsubscribes from one or more topics.
//
// After unsubscribing, the client will no longer receive messages on the
// specified topics. The function returns a Token that completes when the
// unsubscription is acknowledged by the server.
//
// Example (single topic):
//
//	token := client.Unsubscribe("sensors/temperature")
//	token.Wait(context.Background())
//
// Example (multiple topics):
//
//	token := client.Unsubscribe("sensors/temp", "sensors/humidity", "sensors/pressure")
//	if err := token.Wait(context.Background()); err != nil {
//	    log.Printf("Unsubscribe failed: %v", err)
//	}
func (c *Client) Unsubscribe(topics ...string) Token {
	c.opts.Logger.Debug("unsubscribing from topics", "topics", topics)

	if len(topics) == 0 {
		tok := newToken()
		tok.complete(nil)
		return tok
	}

	pkt := &packets.UnsubscribePacket{
		Topics:  topics,
		Version: c.opts.ProtocolVersion,
	}
	tok := newToken()
	req := &unsubscribeRequest{
		packet: pkt,
		topics: topics,
		token:  tok,
	}
	c.internalUnsubscribe(req)

	return tok
}

// resubscribeAll resubscribes to all active subscriptions after reconnection.
// This is called automatically by the reconnect loop.
func (c *Client) resubscribeAll() {
	c.sessionLock.Lock()
	defer c.sessionLock.Unlock()

	if len(c.subscriptions) == 0 {
		return
	}

	c.opts.Logger.Debug("resubscribing to topics", "count", len(c.subscriptions))

	var topics []string
	var entries []subscriptionEntry
	for topic, entry := range c.subscriptions {
		topics = append(topics, topic)
		entries = append(entries, entry)
	}

	// Batch subscriptions to avoid exceeding server limits
	// Most servers limit SUBSCRIBE packets to 100-200 topics
	const batchSize = 100

	for i := 0; i < len(topics); i += batchSize {
		end := min(i+batchSize, len(topics))

		batchTopics := topics[i:end]
		batchEntries := entries[i:end]

		// Group by (SubscriptionID + UserProperties) to comply with MQTT v5.0
		// "A SUBSCRIBE packet MUST NOT contain more than one Subscription Identifier."
		// Also User Properties apply to the whole packet.
		groups := make(map[string]struct {
			topics            []string
			qos               []uint8
			noLocal           []bool
			retainAsPublished []bool
			retainHandling    []uint8
			id                int
			userProps         map[string]string
		})

		for j, entry := range batchEntries {
			key := subGroupKey(entry.options.SubscriptionID, entry.options.UserProperties)
			g := groups[key]

			// Initialize if new group
			if len(g.topics) == 0 {
				g.id = entry.options.SubscriptionID
				g.userProps = entry.options.UserProperties
			}

			g.topics = append(g.topics, batchTopics[j])
			g.qos = append(g.qos, entry.qos)

			if c.opts.ProtocolVersion >= ProtocolV50 {
				g.noLocal = append(g.noLocal, entry.options.NoLocal)
				g.retainAsPublished = append(g.retainAsPublished, entry.options.RetainAsPublished)
				g.retainHandling = append(g.retainHandling, entry.options.RetainHandling)
			}
			groups[key] = g
		}

		// Send one packet for each group
		for _, g := range groups {
			pkt := &packets.SubscribePacket{
				PacketID:          c.nextID(),
				Topics:            g.topics,
				QoS:               g.qos,
				NoLocal:           g.noLocal,
				RetainAsPublished: g.retainAsPublished,
				RetainHandling:    g.retainHandling,
				Version:           c.opts.ProtocolVersion,
			}

			if c.opts.ProtocolVersion >= ProtocolV50 {
				props := &packets.Properties{}
				hasProps := false

				if g.id > 0 {
					props.SubscriptionIdentifier = []int{g.id}
					hasProps = true
				}
				if len(g.userProps) > 0 {
					for k, v := range g.userProps {
						props.UserProperties = append(props.UserProperties, packets.UserProperty{
							Key:   k,
							Value: v,
						})
					}
					hasProps = true
				}

				if hasProps {
					pkt.Properties = props
				}
			}

			// Store pending operation BEFORE sending packet to avoid race conditions
			c.pending[pkt.PacketID] = &pendingOp{
				packet:    pkt,
				token:     newToken(),
				qos:       1,
				timestamp: time.Now(),
			}

			select {
			case c.outgoing <- pkt:
			case <-c.stop:
				return
			}

			c.opts.Logger.Debug("resubscribe packet sent",
				"packet_id", pkt.PacketID,
				"sub_id", g.id,
				"topics_count", len(g.topics))
		}
	}
}

// subGroupKey generates a unique key for grouping subscriptions by ID and User Properties.
func subGroupKey(id int, props map[string]string) string {
	if len(props) == 0 {
		return fmt.Sprintf("%d", id)
	}
	// Sort keys for deterministic output
	keys := make([]string, 0, len(props))
	for k := range props {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var sb strings.Builder
	fmt.Fprintf(&sb, "%d|", id)
	for _, k := range keys {
		fmt.Fprintf(&sb, "%s=%s|", k, props[k])
	}
	return sb.String()
}
