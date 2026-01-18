package mq

// SessionStore handles persistence of session state across process restarts.
// This enables session state to survive client restarts, crashes, or reboots.
//
// Note: State is only loaded from the store when the client process starts.
// During normal network reconnections (when in-memory state is still available),
// the store is not consulted - the in-memory state is used directly.
//
// What Gets Persisted:
//
//   - Pending QoS 1 and QoS 2 publishes (not yet acknowledged by server)
//   - Active subscriptions (to restore on reconnect)
//   - Received QoS 2 packet IDs (to prevent duplicate delivery)
//
// What Does NOT Get Persisted:
//
//   - QoS 0 publishes (fire-and-forget, no delivery guarantee)
//   - Messages already acknowledged (PUBACK/PUBCOMP received)
//   - Connection state (handled by MQTT protocol on reconnect)
//
// Threading Model:
//
// All methods are called from a single goroutine (the client's logic loop).
// Implementations do NOT need to be thread-safe for concurrent calls from mq.
//
// Async Implementations:
//
// Save/Delete methods MAY return immediately and perform I/O asynchronously
// in a background goroutine. This allows implementations to batch writes or
// use async I/O without blocking the client's logic loop.
//
// However, Load methods MUST complete synchronously and return the actual
// data, as they are called during connection setup when the data is needed
// immediately.
//
// Error Handling:
//
//   - Save/Delete errors are logged but do not fail the operation. The in-memory
//     state is authoritative. Implementations should handle errors gracefully
//     (e.g., retry, log, alert).
//   - Load errors will cause connection failure, as session state cannot be restored.
type SessionStore interface {
	// SavePendingPublish stores an outgoing publish that hasn't been acknowledged.
	// Called when a QoS 1/2 publish is sent.
	// MAY return immediately and persist asynchronously.
	SavePendingPublish(packetID uint16, pub *PersistedPublish) error

	// DeletePendingPublish removes a publish after it's been acknowledged.
	// Called when PUBACK (QoS 1) or PUBCOMP (QoS 2) is received.
	// MAY return immediately and delete asynchronously.
	DeletePendingPublish(packetID uint16) error

	// LoadPendingPublishes retrieves all pending publishes on reconnect.
	// Called once during connection establishment.
	// MUST complete synchronously and return actual data.
	LoadPendingPublishes() (map[uint16]*PersistedPublish, error)

	// ClearPendingPublishes removes all pending publishes.
	// Called when SessionPresent=false (server lost our session).
	ClearPendingPublishes() error

	// SaveSubscription stores an active subscription.
	// Called when SUBACK is received.
	// MAY return immediately and persist asynchronously.
	SaveSubscription(topic string, sub *SubscriptionInfo) error

	// DeleteSubscription removes a subscription.
	// Called when UNSUBACK is received.
	// MAY return immediately and delete asynchronously.
	DeleteSubscription(topic string) error

	// LoadSubscriptions retrieves all subscriptions on reconnect.
	// Called once during connection establishment.
	//
	// Note: Only topic filters and options are restored. The associated MessageHandlers
	// are NOT persisted.
	// - Callers should use mq.WithSubscription to re-associate handlers with these topics.
	// - If no handler is found, messages will fallback to the DefaultPublishHandler if set.
	//
	// MUST complete synchronously and return actual data.
	LoadSubscriptions() (map[string]*SubscriptionInfo, error)

	// SaveReceivedQoS2 marks a QoS 2 packet ID as received (prevent duplicates).
	// Called when QoS 2 PUBLISH is received.
	// MAY return immediately and persist asynchronously.
	SaveReceivedQoS2(packetID uint16) error

	// DeleteReceivedQoS2 removes a QoS 2 packet ID after PUBCOMP sent.
	// Called when QoS 2 flow completes.
	// MAY return immediately and delete asynchronously.
	DeleteReceivedQoS2(packetID uint16) error

	// LoadReceivedQoS2 retrieves all received QoS 2 packet IDs.
	// Called once during connection establishment.
	// MUST complete synchronously and return actual data.
	LoadReceivedQoS2() (map[uint16]struct{}, error)

	// ClearReceivedQoS2 removes all received QoS 2 packet IDs.
	// Called when SessionPresent=false (server lost our session).
	ClearReceivedQoS2() error

	// Clear removes all session state.
	// Called when CleanSession=true or session expires.
	Clear() error
}

// PersistedPublish represents a publish for persistence.
// This is a simplified representation containing only the data needed
// to restore a pending publish after reconnection.
type PersistedPublish struct {
	Topic      string
	Payload    []byte
	QoS        uint8
	Retain     bool
	Properties *PublishProperties
}

// SubscriptionInfo represents a subscription for persistence.
// This contains the data needed to restore a subscription after reconnection.
type SubscriptionInfo struct {
	QoS     uint8
	Options *SubscriptionOptions
}

// PublishProperties represents MQTT v5.0 publish properties for persistence.
type PublishProperties struct {
	PayloadFormat          *uint8
	MessageExpiry          *uint32
	TopicAlias             *uint16
	ResponseTopic          string
	CorrelationData        []byte
	UserProperties         map[string]string
	SubscriptionIdentifier *uint32
	ContentType            string
}

// SubscriptionOptions represents MQTT v5.0 subscription options for persistence.
type SubscriptionOptions struct {
	NoLocal           bool
	RetainAsPublished bool
	RetainHandling    uint8
	SubscriptionID    *uint32
	UserProperties    map[string]string
}
