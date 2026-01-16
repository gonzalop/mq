package mq

import "github.com/gonzalop/mq/internal/packets"

// toPublicProperties converts internal packet properties to the public API format.
// Returns nil if the internal properties are nil or empty.
func toPublicProperties(internal *packets.Properties) *Properties {
	if internal == nil {
		return nil
	}

	// Check if properties are actually empty
	if isEmpty(internal) {
		return nil
	}

	props := &Properties{
		UserProperties: make(map[string]string),
	}

	// Convert simple string fields
	if internal.Presence&packets.PresContentType != 0 {
		props.ContentType = internal.ContentType
	}
	if internal.Presence&packets.PresResponseTopic != 0 {
		props.ResponseTopic = internal.ResponseTopic
	}

	// Convert byte slices
	if len(internal.CorrelationData) > 0 {
		props.CorrelationData = internal.CorrelationData
	}

	// Convert optional numeric fields
	if internal.Presence&packets.PresMessageExpiryInterval != 0 {
		val := internal.MessageExpiryInterval
		props.MessageExpiry = &val
	}
	if internal.Presence&packets.PresPayloadFormatIndicator != 0 {
		val := internal.PayloadFormatIndicator
		props.PayloadFormat = &val
	}
	if internal.Presence&packets.PresWillDelayInterval != 0 {
		val := internal.WillDelayInterval
		props.WillDelayInterval = &val
	}
	if internal.Presence&packets.PresSessionExpiryInterval != 0 {
		val := internal.SessionExpiryInterval
		props.SessionExpiryInterval = &val
	}

	// Convert subscription identifiers (receive-only)
	if len(internal.SubscriptionIdentifier) > 0 {
		props.SubscriptionIdentifier = internal.SubscriptionIdentifier
	}

	// Convert reason string (receive-only)
	if internal.Presence&packets.PresReasonString != 0 {
		props.ReasonString = internal.ReasonString
	}

	// Convert user properties
	for _, up := range internal.UserProperties {
		props.UserProperties[up.Key] = up.Value
	}

	return props
}

// toInternalProperties converts public API properties to internal packet format.
// Returns nil if the public properties are nil.
func toInternalProperties(public *Properties) *packets.Properties {
	if public == nil {
		return nil
	}

	props := &packets.Properties{}

	// Convert simple string fields
	if public.ContentType != "" {
		props.ContentType = public.ContentType
		props.Presence |= packets.PresContentType
	}
	if public.ResponseTopic != "" {
		props.ResponseTopic = public.ResponseTopic
		props.Presence |= packets.PresResponseTopic
	}
	if public.ReasonString != "" {
		props.ReasonString = public.ReasonString
		props.Presence |= packets.PresReasonString
	}

	// Convert byte slices
	if len(public.CorrelationData) > 0 {
		props.CorrelationData = public.CorrelationData
	}

	// Convert optional numeric fields
	if public.MessageExpiry != nil {
		props.MessageExpiryInterval = *public.MessageExpiry
		props.Presence |= packets.PresMessageExpiryInterval
	}
	if public.PayloadFormat != nil {
		props.PayloadFormatIndicator = *public.PayloadFormat
		props.Presence |= packets.PresPayloadFormatIndicator
	}
	if public.WillDelayInterval != nil {
		props.WillDelayInterval = *public.WillDelayInterval
		props.Presence |= packets.PresWillDelayInterval
	}
	if public.SessionExpiryInterval != nil {
		props.SessionExpiryInterval = *public.SessionExpiryInterval
		props.Presence |= packets.PresSessionExpiryInterval
	}

	// Convert user properties
	if len(public.UserProperties) > 0 {
		props.UserProperties = make([]packets.UserProperty, 0, len(public.UserProperties))
		for key, value := range public.UserProperties {
			props.UserProperties = append(props.UserProperties, packets.UserProperty{
				Key:   key,
				Value: value,
			})
		}
	}

	return props
}

// isEmpty checks if internal properties are effectively empty.
func isEmpty(p *packets.Properties) bool {
	if p == nil {
		return true
	}

	return p.Presence == 0 &&
		len(p.CorrelationData) == 0 &&
		len(p.UserProperties) == 0 &&
		len(p.SubscriptionIdentifier) == 0 && // Check for subscription IDs
		len(p.AuthenticationData) == 0
}
