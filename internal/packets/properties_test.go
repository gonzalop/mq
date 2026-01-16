package packets

import (
	"reflect"
	"testing"
)

func TestProperties(t *testing.T) {
	tests := []struct {
		name     string
		props    *Properties
		expected *Properties // If nil, expect same as props
	}{
		{
			name:  "Nil properties",
			props: nil,
		},
		{
			name:  "Empty properties",
			props: &Properties{},
		},
		{
			name: "Basic properties",
			props: &Properties{
				PayloadFormatIndicator: 1,
				MessageExpiryInterval:  60,
				ContentType:            "application/json",
				ReasonString:           "user desc",
				Presence:               PresPayloadFormatIndicator | PresMessageExpiryInterval | PresContentType | PresReasonString,
			},
		},
		{
			name: "User properties",
			props: &Properties{
				UserProperties: []UserProperty{
					{Key: "key1", Value: "val1"},
					{Key: "key2", Value: "val2"},
				},
			},
		},
		{
			name: "All properties",
			props: &Properties{
				PayloadFormatIndicator:          1,
				MessageExpiryInterval:           3600,
				ContentType:                     "text/plain",
				ResponseTopic:                   "resp/topic",
				CorrelationData:                 []byte("12345"),
				SubscriptionIdentifier:          []int{1, 2},
				SessionExpiryInterval:           7200,
				AssignedClientIdentifier:        "client-assigned",
				ServerKeepAlive:                 120,
				AuthenticationMethod:            "oauth",
				AuthenticationData:              []byte("token"),
				RequestProblemInformation:       1,
				WillDelayInterval:               30,
				RequestResponseInformation:      0,
				ResponseInformation:             "resp-info",
				ServerReference:                 "server-ref",
				ReasonString:                    "reason",
				ReceiveMaximum:                  100,
				TopicAliasMaximum:               10,
				TopicAlias:                      5,
				MaximumQoS:                      1,
				RetainAvailable:                 true,
				UserProperties:                  []UserProperty{{Key: "k", Value: "v"}},
				MaximumPacketSize:               1024,
				WildcardSubscriptionAvailable:   true,
				SubscriptionIdentifierAvailable: true,
				SharedSubscriptionAvailable:     true,
				Presence: PresPayloadFormatIndicator |
					PresMessageExpiryInterval |
					PresContentType |
					PresResponseTopic |
					PresSessionExpiryInterval |
					PresAssignedClientIdentifier |
					PresServerKeepAlive |
					PresAuthenticationMethod |
					PresRequestProblemInformation |
					PresWillDelayInterval |
					PresRequestResponseInformation |
					PresResponseInformation |
					PresServerReference |
					PresReasonString |
					PresReceiveMaximum |
					PresTopicAliasMaximum |
					PresTopicAlias |
					PresMaximumQoS |
					PresRetainAvailable |
					PresMaximumPacketSize |
					PresWildcardSubscriptionAvailable |
					PresSubscriptionIdentifierAvailable |
					PresSharedSubscriptionAvailable,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Encode
			encoded := encodeProperties(tt.props)

			// Decode
			// Handle nil/empty case: encode returns nil or empty slice, decode should handle it
			if len(encoded) == 0 {
				if tt.props != nil && !isEmpty(tt.props) {
					t.Fatal("encoded bytes empty but props not empty")
				}
				// Decoding empty/nil byte slice depends on how decodeProperties handles it.
				// decodeProperties expects [Length] [Props...].
				// encodeProperties adds Length.
				// If props is nil, encodeProperties returns nil.
				if tt.props == nil && encoded != nil {
					t.Errorf("expected nil encoded for nil props, got %v", encoded)
				}
				return
			}

			decoded, n, err := decodeProperties(encoded)
			if err != nil {
				t.Fatalf("failed to decode: %v", err)
			}
			if n != len(encoded) {
				t.Errorf("decoded length = %d, want %d", n, len(encoded))
			}

			// Compare
			expected := tt.expected
			if expected == nil {
				expected = tt.props
			}

			if !compareProperties(decoded, expected) {
				// Special case: if expected is nil but decoded is empty struct (all fields nil/zero), treat as equal
				if expected == nil && isEmpty(decoded) {
					return
				}
				t.Errorf("decoded properties mismatch.\nGot: %+v\nWant: %+v", decoded, expected)
			}
		})
	}
}

func isEmpty(p *Properties) bool {
	if p == nil {
		return true
	}
	return p.Presence == 0 && len(p.UserProperties) == 0 && len(p.CorrelationData) == 0 && len(p.SubscriptionIdentifier) == 0 && len(p.AuthenticationData) == 0
}

// compareProperties compares two Properties structs.
func compareProperties(a, b *Properties) bool {
	if isEmpty(a) && isEmpty(b) {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return reflect.DeepEqual(a, b)
}
