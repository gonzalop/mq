package mq

import (
	"maps"
	"testing"

	"github.com/gonzalop/mq/internal/packets"
)

func TestPropertiesConversion(t *testing.T) {
	tests := []struct {
		name    string
		public  *Properties
		wantNil bool
	}{
		{
			name:    "nil properties",
			public:  nil,
			wantNil: true,
		},
		{
			name: "content type only",
			public: &Properties{
				ContentType: "application/json",
			},
		},
		{
			name: "response topic and correlation data",
			public: &Properties{
				ResponseTopic:   "responses/test",
				CorrelationData: []byte("req-123"),
			},
		},
		{
			name: "user properties",
			public: &Properties{
				UserProperties: map[string]string{
					"key1": "value1",
					"key2": "value2",
				},
			},
		},
		{
			name: "message expiry",
			public: func() *Properties {
				p := &Properties{}
				expiry := uint32(300)
				p.MessageExpiry = &expiry
				return p
			}(),
		},
		{
			name: "payload format",
			public: func() *Properties {
				p := &Properties{}
				format := uint8(1)
				p.PayloadFormat = &format
				return p
			}(),
		},
		{
			name: "will delay interval",
			public: func() *Properties {
				p := &Properties{}
				delay := uint32(60)
				p.WillDelayInterval = &delay
				return p
			}(),
		},
		{
			name: "all properties",
			public: func() *Properties {
				p := &Properties{
					ContentType:     "application/json",
					ResponseTopic:   "responses/test",
					CorrelationData: []byte("correlation-123"),
					UserProperties: map[string]string{
						"version": "1.0",
						"source":  "sensor-01",
					},
				}
				expiry := uint32(600)
				format := uint8(1)
				delay := uint32(120)
				p.MessageExpiry = &expiry
				p.PayloadFormat = &format
				p.WillDelayInterval = &delay
				return p
			}(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Convert to internal
			internal := toInternalProperties(tt.public)

			if tt.wantNil {
				if internal != nil {
					t.Errorf("toInternalProperties() = %v, want nil", internal)
				}
				return
			}

			// Convert back to public
			result := toPublicProperties(internal)

			// Compare
			if tt.public.ContentType != result.ContentType {
				t.Errorf("ContentType = %v, want %v", result.ContentType, tt.public.ContentType)
			}
			if tt.public.ResponseTopic != result.ResponseTopic {
				t.Errorf("ResponseTopic = %v, want %v", result.ResponseTopic, tt.public.ResponseTopic)
			}
			if string(tt.public.CorrelationData) != string(result.CorrelationData) {
				t.Errorf("CorrelationData = %v, want %v", result.CorrelationData, tt.public.CorrelationData)
			}

			// Compare optional fields
			if tt.public.MessageExpiry != nil {
				if result.MessageExpiry == nil {
					t.Error("MessageExpiry is nil, want non-nil")
				} else if *result.MessageExpiry != *tt.public.MessageExpiry {
					t.Errorf("MessageExpiry = %v, want %v", *result.MessageExpiry, *tt.public.MessageExpiry)
				}
			}

			if tt.public.PayloadFormat != nil {
				if result.PayloadFormat == nil {
					t.Error("PayloadFormat is nil, want non-nil")
				} else if *result.PayloadFormat != *tt.public.PayloadFormat {
					t.Errorf("PayloadFormat = %v, want %v", *result.PayloadFormat, *tt.public.PayloadFormat)
				}
			}

			if tt.public.WillDelayInterval != nil {
				if result.WillDelayInterval == nil {
					t.Error("WillDelayInterval is nil, want non-nil")
				} else if *result.WillDelayInterval != *tt.public.WillDelayInterval {
					t.Errorf("WillDelayInterval = %v, want %v", *result.WillDelayInterval, *tt.public.WillDelayInterval)
				}
			}

			// Compare user properties
			if len(tt.public.UserProperties) != len(result.UserProperties) {
				t.Errorf("UserProperties length = %v, want %v", len(result.UserProperties), len(tt.public.UserProperties))
			}
			for key, value := range tt.public.UserProperties {
				if result.UserProperties[key] != value {
					t.Errorf("UserProperties[%s] = %v, want %v", key, result.UserProperties[key], value)
				}
			}
		})
	}
}

func TestToPublicPropertiesEmpty(t *testing.T) {
	// Empty internal properties should return nil
	internal := &packets.Properties{}
	result := toPublicProperties(internal)

	if result != nil {
		t.Errorf("toPublicProperties(empty) = %v, want nil", result)
	}
}

func TestNewProperties(t *testing.T) {
	props := NewProperties()

	if props == nil {
		t.Fatal("NewProperties() returned nil")
	}

	if props.UserProperties == nil {
		t.Error("UserProperties map not initialized")
	}

	if len(props.UserProperties) != 0 {
		t.Errorf("UserProperties length = %d, want 0", len(props.UserProperties))
	}
}

func TestPropertiesSetUserProperty(t *testing.T) {
	props := &Properties{}

	// Set property on nil map
	props.SetUserProperty("key1", "value1")

	if props.UserProperties == nil {
		t.Fatal("UserProperties map not initialized")
	}

	if props.UserProperties["key1"] != "value1" {
		t.Errorf("UserProperties[key1] = %v, want value1", props.UserProperties["key1"])
	}

	// Set another property
	props.SetUserProperty("key2", "value2")

	if len(props.UserProperties) != 2 {
		t.Errorf("UserProperties length = %d, want 2", len(props.UserProperties))
	}

	// Overwrite existing property
	props.SetUserProperty("key1", "new-value")

	if props.UserProperties["key1"] != "new-value" {
		t.Errorf("UserProperties[key1] = %v, want new-value", props.UserProperties["key1"])
	}
}

func TestPropertiesGetUserProperty(t *testing.T) {
	props := &Properties{
		UserProperties: map[string]string{
			"key1": "value1",
		},
	}

	// Get existing property
	if got := props.GetUserProperty("key1"); got != "value1" {
		t.Errorf("GetUserProperty(key1) = %v, want value1", got)
	}

	// Get non-existent property
	if got := props.GetUserProperty("nonexistent"); got != "" {
		t.Errorf("GetUserProperty(nonexistent) = %v, want empty string", got)
	}

	// Get from nil map
	props.UserProperties = nil
	if got := props.GetUserProperty("key1"); got != "" {
		t.Errorf("GetUserProperty(key1) on nil map = %v, want empty string", got)
	}
}

func TestPublishOptionsWithProperties(t *testing.T) {
	tests := []struct {
		name string
		opts []PublishOption
		want func(*PublishOptions) bool
	}{
		{
			name: "WithContentType",
			opts: []PublishOption{WithContentType("application/json")},
			want: func(o *PublishOptions) bool {
				return o.Properties != nil &&
					o.Properties.ContentType == "application/json"
			},
		},
		{
			name: "WithResponseTopic",
			opts: []PublishOption{WithResponseTopic("responses/test")},
			want: func(o *PublishOptions) bool {
				return o.Properties != nil &&
					o.Properties.ResponseTopic == "responses/test"
			},
		},
		{
			name: "WithCorrelationData",
			opts: []PublishOption{WithCorrelationData([]byte("req-123"))},
			want: func(o *PublishOptions) bool {
				return o.Properties != nil &&
					string(o.Properties.CorrelationData) == "req-123"
			},
		},
		{
			name: "WithUserProperty",
			opts: []PublishOption{
				WithUserProperty("key1", "value1"),
				WithUserProperty("key2", "value2"),
			},
			want: func(o *PublishOptions) bool {
				if o.Properties == nil || len(o.Properties.UserProperties) != 2 {
					return false
				}
				found := make(map[string]string)
				maps.Copy(found, o.Properties.UserProperties)
				return found["key1"] == "value1" && found["key2"] == "value2"
			},
		},
		{
			name: "WithMessageExpiry",
			opts: []PublishOption{WithMessageExpiry(300)},
			want: func(o *PublishOptions) bool {
				return o.Properties != nil &&
					o.Properties.MessageExpiry != nil &&
					*o.Properties.MessageExpiry == 300
			},
		},
		{
			name: "WithPayloadFormat",
			opts: []PublishOption{WithPayloadFormat(1)},
			want: func(o *PublishOptions) bool {
				return o.Properties != nil &&
					o.Properties.PayloadFormat != nil &&
					*o.Properties.PayloadFormat == 1
			},
		},
		{
			name: "WithProperties",
			opts: []PublishOption{
				WithProperties(&Properties{
					ContentType:   "text/plain",
					ResponseTopic: "responses/test",
				}),
			},
			want: func(o *PublishOptions) bool {
				return o.Properties != nil &&
					o.Properties.ContentType == "text/plain" &&
					o.Properties.ResponseTopic == "responses/test"
			},
		},
		{
			name: "multiple options",
			opts: []PublishOption{
				WithContentType("application/json"),
				WithUserProperty("version", "1.0"),
				WithMessageExpiry(600),
			},
			want: func(o *PublishOptions) bool {
				if o.Properties == nil {
					return false
				}
				if o.Properties.ContentType != "application/json" {
					return false
				}
				if o.Properties.MessageExpiry == nil || *o.Properties.MessageExpiry != 600 {
					return false
				}
				if len(o.Properties.UserProperties) != 1 {
					return false
				}
				return o.Properties.UserProperties["version"] == "1.0"
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := &PublishOptions{}

			for _, opt := range tt.opts {
				opt(opts)
			}

			if !tt.want(opts) {
				t.Errorf("packet properties do not match expected values")
			}
		})
	}
}

func TestReasonStringProperty(t *testing.T) {
	tests := []struct {
		name         string
		reasonString string
		want         string
	}{
		{
			name:         "empty reason string",
			reasonString: "",
			want:         "",
		},
		{
			name:         "simple reason",
			reasonString: "Message accepted",
			want:         "Message accepted",
		},
		{
			name:         "error reason",
			reasonString: "QoS downgraded to 1",
			want:         "QoS downgraded to 1",
		},
		{
			name:         "server message",
			reasonString: "Server shutting down for maintenance",
			want:         "Server shutting down for maintenance",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Simulate receiving a message with reason string
			internal := toInternalProperties(&Properties{
				ContentType: "application/json", // Need at least one property
			})
			if tt.reasonString != "" {
				internal.ReasonString = tt.reasonString
				internal.Presence |= packets.PresReasonString
			}

			// Convert to public
			result := toPublicProperties(internal)

			if result == nil {
				t.Fatal("result is nil")
			}

			if result.ReasonString != tt.want {
				t.Errorf("ReasonString = %q, want %q", result.ReasonString, tt.want)
			}
		})
	}
}

func TestReasonStringReceiveOnly(t *testing.T) {
	// Test that ReasonString is receive-only
	// When converting from public to internal for sending,
	// ReasonString should not be set
	props := &Properties{
		ReasonString: "This should be ignored",
		ContentType:  "application/json",
	}

	internal := toInternalProperties(props)

	// ReasonString SHOULD be set when converting for sending
	// (it's used in DISCONNECT and AUTH packets)
	if internal.Presence&packets.PresReasonString == 0 {
		t.Error("ReasonString should be set when converting for sending")
	}
	if internal.ReasonString != "This should be ignored" {
		t.Errorf("ReasonString = %q, want %q", internal.ReasonString, "This should be ignored")
	}

	// Other properties should still be set
	if internal.Presence&packets.PresContentType == 0 || internal.ContentType != "application/json" {
		t.Error("ContentType should be set")
	}
}

func TestSubscriptionIdentifierProperty(t *testing.T) {
	tests := []struct {
		name   string
		subIDs []int
		wantID []int
	}{
		{
			name:   "nil subscription identifier",
			subIDs: nil,
			wantID: nil,
		},
		{
			name:   "single subscription identifier",
			subIDs: []int{42},
			wantID: []int{42},
		},
		{
			name:   "multiple subscription identifiers",
			subIDs: []int{1, 2, 3},
			wantID: []int{1, 2, 3},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Simulate receiving a message with subscription IDs
			internal := toInternalProperties(&Properties{
				ContentType: "application/json", // Need at least one property
			})
			internal.SubscriptionIdentifier = tt.subIDs

			// Convert to public
			result := toPublicProperties(internal)

			if result == nil && tt.wantID == nil {
				return
			}

			if result == nil {
				t.Fatal("result is nil")
			}

			if len(result.SubscriptionIdentifier) != len(tt.wantID) {
				t.Errorf("SubscriptionIdentifier length = %d, want %d",
					len(result.SubscriptionIdentifier), len(tt.wantID))
				return
			}

			for i, id := range tt.wantID {
				if result.SubscriptionIdentifier[i] != id {
					t.Errorf("SubscriptionIdentifier[%d] = %d, want %d",
						i, result.SubscriptionIdentifier[i], id)
				}
			}
		})
	}
}

func TestSubscriptionIdentifierReceiveOnly(t *testing.T) {
	// Test that SubscriptionIdentifier is receive-only
	// When converting from public to internal for sending,
	// SubscriptionIdentifier should not be set
	props := &Properties{
		SubscriptionIdentifier: []int{1, 2, 3},
		ContentType:            "application/json",
	}

	internal := toInternalProperties(props)

	// SubscriptionIdentifier should NOT be set when converting for sending
	// (it's a receive-only property)
	if len(internal.SubscriptionIdentifier) != 0 {
		t.Errorf("SubscriptionIdentifier should not be set when converting for sending, got %v",
			internal.SubscriptionIdentifier)
	}

	// Other properties should still be set
	if internal.Presence&packets.PresContentType == 0 || internal.ContentType != "application/json" {
		t.Error("ContentType should be set")
	}
}
