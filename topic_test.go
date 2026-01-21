package mq

import (
	"fmt"
	"strings"
	"testing"
)

func TestMatchTopic(t *testing.T) {
	tests := []struct {
		filter string
		topic  string
		match  bool
	}{
		// Exact matches
		{"test/topic", "test/topic", true},
		{"test/topic", "test/other", false},

		// Single-level wildcard (+)
		{"test/+", "test/topic", true},
		{"test/+", "test/other", true},
		{"test/+", "test/topic/sub", false},
		{"test/+/sub", "test/topic/sub", true},
		{"+/topic", "test/topic", true},
		{"+/+", "test/topic", true},

		// Multi-level wildcard (#)
		{"test/#", "test/topic", true},
		{"test/#", "test/topic/sub", true},
		{"test/#", "test/topic/sub/deep", true},
		{"test/#", "other/topic", false},
		{"#", "any/topic/here", true},
		{"test/topic/#", "test/topic", true},
		{"test/topic/#", "test/topic/sub", true},

		// Combined wildcards
		{"+/+/#", "test/topic/sub/deep", true},
		{"test/+/#", "test/topic/sub", true},

		// Edge cases
		{"", "", true},
		{"test", "test", true},
		{"test/", "test/", true},
	}

	for _, tt := range tests {
		t.Run(tt.filter+"_vs_"+tt.topic, func(t *testing.T) {
			result := MatchTopic(tt.filter, tt.topic)
			if result != tt.match {
				t.Errorf("MatchTopic(%q, %q) = %v, want %v", tt.filter, tt.topic, result, tt.match)
			}
		})
	}
}

func ExampleMatchTopic() {
	filter := "sensors/+/temperature"
	topic1 := "sensors/living-room/temperature"
	topic2 := "sensors/kitchen/humidity"

	fmt.Printf("%s matches %s: %v\n", topic1, filter, MatchTopic(filter, topic1))
	fmt.Printf("%s matches %s: %v\n", topic2, filter, MatchTopic(filter, topic2))

	filterHash := "sensors/#"
	topic3 := "sensors/basement/temperature/current"
	fmt.Printf("%s matches %s: %v\n", topic3, filterHash, MatchTopic(filterHash, topic3))

	// Output:
	// sensors/living-room/temperature matches sensors/+/temperature: true
	// sensors/kitchen/humidity matches sensors/+/temperature: false
	// sensors/basement/temperature/current matches sensors/#: true
}

func TestValidatePublishTopic(t *testing.T) {
	opts := defaultOptions("tcp://test:1883")

	tests := []struct {
		name    string
		topic   string
		wantErr bool
	}{
		{"valid simple", "sensors/temperature", false},
		{"valid multi-level", "home/room1/sensor/temp", false},
		{"empty topic", "", true},
		{"wildcard plus", "sensors/+/temp", true},
		{"wildcard hash", "sensors/#", true},
		{"null byte", "sensors\x00temp", true},
		{"too long", strings.Repeat("a", DefaultMaxTopicLength+1), true},
		{"max length ok", strings.Repeat("a", DefaultMaxTopicLength), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validatePublishTopic(tt.topic, opts)
			if (err != nil) != tt.wantErr {
				t.Errorf("validatePublishTopic() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidateSubscribeTopic(t *testing.T) {
	opts := defaultOptions("tcp://test:1883")

	tests := []struct {
		name    string
		topic   string
		wantErr bool
	}{
		{"valid simple", "sensors/temperature", false},
		{"valid single wildcard", "sensors/+/temp", false},
		{"valid multi wildcard", "sensors/#", false},
		{"valid multi wildcard deep", "sensors/room1/#", false},
		{"valid all wildcard", "#", false},
		{"valid multiple plus", "+/+/+", false},
		{"empty topic", "", true},
		{"invalid plus not alone", "sensors/+temp/data", true},
		{"invalid hash not alone", "sensors/#temp", true},
		{"invalid hash not last", "sensors/#/temp", true},
		{"null byte", "sensors\x00temp", true},
		{"too long", strings.Repeat("a", DefaultMaxTopicLength+1), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateSubscribeTopic(tt.topic, opts)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateSubscribeTopic() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidatePayloadSize(t *testing.T) {
	opts := defaultOptions("tcp://test:1883")

	tests := []struct {
		name    string
		size    int
		wantErr bool
	}{
		{"empty", 0, false},
		{"small", 100, false},
		{"1MB", 1024 * 1024, false},
		{"max size", DefaultMaxPayloadSize, false},
		{"too large", DefaultMaxPayloadSize + 1, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			payload := make([]byte, tt.size)
			err := validatePayloadSize(payload, opts)
			if (err != nil) != tt.wantErr {
				t.Errorf("validatePayloadSize() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// FuzzValidatePublishTopic fuzzes publish topic validation
func FuzzValidatePublishTopic(f *testing.F) {
	f.Add("sensors/temperature")
	f.Add("home/room1/temp")
	f.Add("")
	f.Add("sensors/+/temp")
	f.Add("sensors/#")

	opts := defaultOptions("tcp://test:1883")
	f.Fuzz(func(t *testing.T, topic string) {
		// Should never panic
		_ = validatePublishTopic(topic, opts)
	})
}

// FuzzValidateSubscribeTopic fuzzes subscribe topic validation
func FuzzValidateSubscribeTopic(f *testing.F) {
	f.Add("sensors/temperature")
	f.Add("sensors/+/temp")
	f.Add("sensors/#")
	f.Add("+/+/+")
	f.Add("#")

	opts := defaultOptions("tcp://test:1883")
	f.Fuzz(func(t *testing.T, topic string) {
		// Should never panic
		_ = validateSubscribeTopic(topic, opts)
	})
}

// TestCustomTopicLimit verifies that custom topic length limits are enforced
func TestCustomTopicLimit(t *testing.T) {
	opts := defaultOptions("tcp://test:1883")
	opts.MaxTopicLength = 10 // Set custom limit of 10 bytes

	// Should pass - under limit
	if err := validatePublishTopic("short", opts); err != nil {
		t.Errorf("Expected short topic to pass, got error: %v", err)
	}

	// Should fail - over custom limit
	if err := validatePublishTopic("this-is-too-long", opts); err == nil {
		t.Error("Expected long topic to fail with custom limit")
	}

	// Same for subscribe
	if err := validateSubscribeTopic("short", opts); err != nil {
		t.Errorf("Expected short topic filter to pass, got error: %v", err)
	}

	if err := validateSubscribeTopic("this-is-too-long", opts); err == nil {
		t.Error("Expected long topic filter to fail with custom limit")
	}
}

// TestCustomPayloadLimit verifies that custom payload size limits are enforced
func TestCustomPayloadLimit(t *testing.T) {
	opts := defaultOptions("tcp://test:1883")
	opts.MaxPayloadSize = 100 // Set custom limit of 100 bytes

	// Should pass - under limit
	smallPayload := make([]byte, 50)
	if err := validatePayloadSize(smallPayload, opts); err != nil {
		t.Errorf("Expected small payload to pass, got error: %v", err)
	}

	// Should fail - over custom limit
	largePayload := make([]byte, 200)
	if err := validatePayloadSize(largePayload, opts); err == nil {
		t.Error("Expected large payload to fail with custom limit")
	}
}

// TestDefaultLimitsWhenNotSet verifies that defaults are used when limits are 0
func TestDefaultLimitsWhenNotSet(t *testing.T) {
	opts := defaultOptions("tcp://test:1883")
	// Limits should be 0 (use defaults)

	if opts.MaxTopicLength != 0 {
		t.Errorf("Expected MaxTopicLength to be 0, got %d", opts.MaxTopicLength)
	}
	if opts.MaxPayloadSize != 0 {
		t.Errorf("Expected MaxPayloadSize to be 0, got %d", opts.MaxPayloadSize)
	}
	if opts.MaxIncomingPacket != 0 {
		t.Errorf("Expected MaxIncomingPacket to be 0, got %d", opts.MaxIncomingPacket)
	}

	// Should use default limits (65535 for topic, 256MB for payload)
	// Test with a topic at the default max
	longTopic := make([]byte, DefaultMaxTopicLength)
	for i := range longTopic {
		longTopic[i] = 'a'
	}
	if err := validatePublishTopic(string(longTopic), opts); err != nil {
		t.Errorf("Expected topic at default max to pass, got error: %v", err)
	}

	// Test with a topic over the default max
	tooLongTopic := make([]byte, DefaultMaxTopicLength+1)
	for i := range tooLongTopic {
		tooLongTopic[i] = 'a'
	}
	if err := validatePublishTopic(string(tooLongTopic), opts); err == nil {
		t.Error("Expected topic over default max to fail")
	}
}

// TestClientValidationIntegration verifies that Client methods correctly use validation.
func TestClientValidationIntegration(t *testing.T) {
	c := &Client{
		opts: defaultOptions("tcp://localhost:1883"),
	}

	// Publish to wildcard should fail
	tok := c.Publish("test/+", []byte("payload"))
	if tok.Error() == nil {
		t.Error("expected error publishing to wildcard, got nil")
	}

	// Subscribe to empty topic should fail
	tok2 := c.Subscribe("", AtLeastOnce, nil)
	if tok2.Error() == nil {
		t.Error("expected error subscribing to empty topic, got nil")
	}
}

// FuzzMatchTopic fuzzes the topic matching function to find edge cases
func FuzzMatchTopic(f *testing.F) {
	// Seed with valid topic patterns
	f.Add("sensors/+/temperature", "sensors/living-room/temperature")
	f.Add("sensors/#", "sensors/living-room/temperature")
	f.Add("sensors/#", "sensors/living-room/temperature/current")
	f.Add("sensors/+/+", "sensors/room1/temp")
	f.Add("+/+/+", "a/b/c")
	f.Add("#", "any/topic/here")
	f.Add("exact/match", "exact/match")
	f.Add("no/match", "different/topic")

	f.Fuzz(func(t *testing.T, filter, topic string) {
		// Should never panic, just return true or false
		_ = MatchTopic(filter, topic)
	})
}

// TestTopicMatch_WildcardStartingWithDollar_Compliance ensures we abide by MQTT-4.7.2-1.
// Although the spec specifies "Server", we follow this rule for client-side
// message dispatching to match expected MQTT behavior.
// "The Server MUST NOT match Topic Filters starting with a wildcard character (# or +)
// to Topic Names beginning with a $ character."
func TestTopicMatch_WildcardStartingWithDollar_Compliance(t *testing.T) {
	tests := []struct {
		filter string
		topic  string
		match  bool
	}{
		// Should NOT match (Rule MQTT-4.7.2-1)
		{"#", "$SYS/broker/version", false},
		{"+/monitor", "$SYS/monitor", false},
		{"+/+", "$SYS/broker", false},
		{"#", "$share/group/topic", false},

		// Should match (Normal cases)
		{"#", "a/b/c", true},
		{"+/monitor", "a/monitor", true},

		// Edge cases: Filter does NOT start with wildcard, so it can match $ topic levels
		// if they are not the first level (though $ is usually only at the start).
		{"a/+/c", "a/$SYS/c", true},
	}

	for _, tt := range tests {
		t.Run(tt.filter+"_vs_"+tt.topic, func(t *testing.T) {
			result := MatchTopic(tt.filter, tt.topic)
			if result != tt.match {
				t.Errorf("MatchTopic(%q, %q) = %v, want %v", tt.filter, tt.topic, result, tt.match)
			}
		})
	}
}
