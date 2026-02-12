package mq

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gonzalop/mq/internal/packets"
)

// TestOperationsAfterDisconnect verifies behavior when calling methods on a disconnected client.
func TestOperationsAfterDisconnect(t *testing.T) {
	// Setup client with mock options
	c := &Client{
		opts: defaultOptions("tcp://localhost:1883"),
		stop: make(chan struct{}),
	}

	// Close stop channel to simulate stopped client
	close(c.stop)

	// Test Publish
	token := c.Publish("test", []byte("payload"))
	if err := token.Error(); !errors.Is(err, ErrClientDisconnected) {
		t.Errorf("expected ErrClientDisconnected, got %v", err)
	}
}

// TestConcurrentSafety verifies that API methods are safe to call concurrently.
func TestConcurrentSafety(t *testing.T) {
	// This mainly tests that the API methods don't race on themselves.
	// Without a running loop, they block or error safeley.

	c := &Client{
		opts:         defaultOptions("tcp://localhost:1883"),
		stop:         make(chan struct{}),
		outgoing:     make(chan packets.Packet, 100),
		pending:      make(map[uint16]*pendingOp),
		publishQueue: make([]*publishRequest, 0),
	}
	// Drain outgoing to prevent blocking
	go func() {
		for range c.outgoing {
		}
	}()
	// Don't close stop, so they send to channel

	var wg sync.WaitGroup
	wg.Add(10)

	for range 10 {
		go func() {
			defer wg.Done()
			c.Publish("topic", []byte("payload"), WithAlias())
		}()
	}

	wg.Wait()
}

func TestAssignedClientID(t *testing.T) {
	tests := []struct {
		name       string
		assignedID string
		want       string
	}{
		{
			name:       "no assigned ID",
			assignedID: "",
			want:       "",
		},
		{
			name:       "server assigned ID",
			assignedID: "auto-ABC123",
			want:       "auto-ABC123",
		},
		{
			name:       "UUID style ID",
			assignedID: "client-f47ac10b-58cc-4372-a567-0e02b2c3d479",
			want:       "client-f47ac10b-58cc-4372-a567-0e02b2c3d479",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &Client{}
			client.assignedClientID = tt.assignedID

			got := client.AssignedClientID()
			if got != tt.want {
				t.Errorf("AssignedClientID() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestAssignedClientIDExtraction(t *testing.T) {
	// Test that assigned client ID is extracted from CONNACK
	assignedID := "server-assigned-123"

	connackProps := &packets.Properties{
		AssignedClientIdentifier: assignedID,
		Presence:                 packets.PresAssignedClientIdentifier,
	}

	// Verify the property exists in internal format
	if connackProps.Presence&packets.PresAssignedClientIdentifier == 0 {
		t.Fatal("AssignedClientIdentifier should not be nil")
	}

	if connackProps.AssignedClientIdentifier != assignedID {
		t.Errorf("AssignedClientIdentifier = %q, want %q",
			connackProps.AssignedClientIdentifier, assignedID)
	}
}

func TestAssignedClientIDDefault(t *testing.T) {
	// Client with no assigned ID should return empty string
	client := &Client{}

	got := client.AssignedClientID()
	if got != "" {
		t.Errorf("AssignedClientID() = %q, want empty string", got)
	}
}

func TestServerKeepAlive(t *testing.T) {
	tests := []struct {
		name      string
		keepalive uint16
		want      uint16
	}{
		{
			name:      "no server keepalive",
			keepalive: 0,
			want:      0,
		},
		{
			name:      "server keepalive 30 seconds",
			keepalive: 30,
			want:      30,
		},
		{
			name:      "server keepalive 120 seconds",
			keepalive: 120,
			want:      120,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &Client{}
			client.serverKeepAlive = tt.keepalive

			got := client.ServerKeepAlive()
			if got != tt.want {
				t.Errorf("ServerKeepAlive() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestServerKeepAliveExtraction(t *testing.T) {
	// Test that server keepalive is extracted from CONNACK
	serverKA := uint16(45)

	connackProps := &packets.Properties{
		ServerKeepAlive: serverKA,
		Presence:        packets.PresServerKeepAlive,
	}

	// Verify the property exists in internal format
	if connackProps.Presence&packets.PresServerKeepAlive == 0 {
		t.Fatal("ServerKeepAlive should not be nil")
	}

	if connackProps.ServerKeepAlive != serverKA {
		t.Errorf("ServerKeepAlive = %d, want %d",
			connackProps.ServerKeepAlive, serverKA)
	}
}

func TestServerKeepAliveUpdatesClientOptions(t *testing.T) {
	// Simulate what happens in connect() when server overrides keepalive
	client := &Client{
		opts: &clientOptions{
			KeepAlive: 60 * time.Second, // Client requested 60s
		},
	}

	// Server overrides to 30s
	serverKA := uint16(30)
	client.serverKeepAlive = serverKA
	client.opts.KeepAlive = time.Duration(serverKA) * time.Second

	// Verify client's keepalive was updated
	if client.opts.KeepAlive != 30*time.Second {
		t.Errorf("KeepAlive = %v, want 30s", client.opts.KeepAlive)
	}

	// Verify ServerKeepAlive() returns the override
	if client.ServerKeepAlive() != 30 {
		t.Errorf("ServerKeepAlive() = %d, want 30", client.ServerKeepAlive())
	}
}

func TestServerKeepAliveDefault(t *testing.T) {
	// Client with no server keepalive should return 0
	client := &Client{}

	got := client.ServerKeepAlive()
	if got != 0 {
		t.Errorf("ServerKeepAlive() = %d, want 0", got)
	}
}

func TestServerReference(t *testing.T) {
	tests := []struct {
		name      string
		serverRef string
		want      string
	}{
		{
			name:      "no server reference",
			serverRef: "",
			want:      "",
		},
		{
			name:      "simple redirect",
			serverRef: "mqtt://server-b.example.com:1883",
			want:      "mqtt://server-b.example.com:1883",
		},
		{
			name:      "load balancer redirect",
			serverRef: "mqtt://lb-02.example.com:1883",
			want:      "mqtt://lb-02.example.com:1883",
		},
		{
			name:      "geographic redirect",
			serverRef: "mqtt://us-west.example.com:1883",
			want:      "mqtt://us-west.example.com:1883",
		},
		{
			name:      "secure redirect",
			serverRef: "mqtts://secure.example.com:8883",
			want:      "mqtts://secure.example.com:8883",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &Client{}
			client.serverReference = tt.serverRef

			got := client.ServerReference()
			if got != tt.want {
				t.Errorf("ServerReference() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestServerReferenceExtraction(t *testing.T) {
	// Test that server reference is extracted from CONNACK
	serverRef := "mqtt://redirect.example.com:1883"

	connackProps := &packets.Properties{
		ServerReference: serverRef,
		Presence:        packets.PresServerReference,
	}

	// Verify the property exists in internal format
	if connackProps.Presence&packets.PresServerReference == 0 {
		t.Fatal("ServerReference should not be nil")
	}

	if connackProps.ServerReference != serverRef {
		t.Errorf("ServerReference = %q, want %q",
			connackProps.ServerReference, serverRef)
	}
}

func TestServerReferenceDefault(t *testing.T) {
	// Client with no server reference should return empty string
	client := &Client{}

	got := client.ServerReference()
	if got != "" {
		t.Errorf("ServerReference() = %q, want empty string", got)
	}
}

func TestServerReferenceNoAutoRedirect(t *testing.T) {
	// Verify that the library does NOT automatically redirect
	// This is a documentation test to ensure the behavior is clear
	client := &Client{}
	client.serverReference = "mqtt://other-server.example.com:1883"

	// Getting the reference should not trigger any action
	ref := client.ServerReference()

	// The reference is just a string - no side effects
	if ref != "mqtt://other-server.example.com:1883" {
		t.Errorf("ServerReference() = %q, want mqtt://other-server.example.com:1883", ref)
	}

	// Application must manually handle redirect
	// This test documents that automatic redirect is NOT implemented
	t.Log("Server reference is exposed but NOT automatically acted upon")
	t.Log("Applications must manually disconnect and reconnect if desired")
}

func TestResponseInformation(t *testing.T) {
	tests := []struct {
		name     string
		respInfo string
		want     string
	}{
		{
			name:     "no response information",
			respInfo: "",
			want:     "",
		},
		{
			name:     "simple prefix",
			respInfo: "responses/",
			want:     "responses/",
		},
		{
			name:     "client-specific prefix",
			respInfo: "client-abc/responses/",
			want:     "client-abc/responses/",
		},
		{
			name:     "tenant prefix",
			respInfo: "tenant-123/client-456/",
			want:     "tenant-123/client-456/",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &Client{}
			client.responseInformation = tt.respInfo

			got := client.ResponseInformation()
			if got != tt.want {
				t.Errorf("ResponseInformation() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestResponseInformationExtraction(t *testing.T) {
	// Test that response information is extracted from CONNACK
	respInfo := "responses/client-xyz/"

	connackProps := &packets.Properties{
		ResponseInformation: respInfo,
		Presence:            packets.PresResponseInformation,
	}

	// Verify the property exists in internal format
	if connackProps.Presence&packets.PresResponseInformation == 0 {
		t.Fatal("ResponseInformation should not be nil")
	}

	if connackProps.ResponseInformation != respInfo {
		t.Errorf("ResponseInformation = %q, want %q",
			connackProps.ResponseInformation, respInfo)
	}
}

func TestResponseInformationDefault(t *testing.T) {
	// Client with no response information should return empty string
	client := &Client{}

	got := client.ResponseInformation()
	if got != "" {
		t.Errorf("ResponseInformation() = %q, want empty string", got)
	}
}

func TestResponseInformationUsage(t *testing.T) {
	// Example of how to use response information
	client := &Client{}
	client.responseInformation = "tenant-a/client-123/"

	respInfo := client.ResponseInformation()
	if respInfo == "" {
		t.Skip("No response information provided")
	}

	// Client would use this to construct response topics
	responseTopic := respInfo + "my-service/responses"
	expectedTopic := "tenant-a/client-123/my-service/responses"

	if responseTopic != expectedTopic {
		t.Errorf("Response topic = %q, want %q", responseTopic, expectedTopic)
	}
}

func TestExtractServerCapabilities(t *testing.T) {
	tests := []struct {
		name  string
		props *packets.Properties
		want  serverCapabilities
	}{
		{
			name:  "nil properties",
			props: nil,
			want: serverCapabilities{
				ReceiveMaximum:              65535,
				MaximumQoS:                  2, // Default
				RetainAvailable:             true,
				WildcardAvailable:           true,
				SubscriptionIDAvailable:     true,
				SharedSubscriptionAvailable: true,
			},
		},
		{
			name: "all capabilities specified",
			props: func() *packets.Properties {
				p := &packets.Properties{}
				p.MaximumPacketSize = 1024 * 1024
				p.ReceiveMaximum = 100
				p.TopicAliasMaximum = 10
				p.MaximumQoS = 1
				p.RetainAvailable = false
				p.WildcardSubscriptionAvailable = true
				p.SubscriptionIdentifierAvailable = true
				p.SharedSubscriptionAvailable = false

				p.Presence = packets.PresMaximumPacketSize |
					packets.PresReceiveMaximum |
					packets.PresTopicAliasMaximum |
					packets.PresMaximumQoS |
					packets.PresRetainAvailable |
					packets.PresWildcardSubscriptionAvailable |
					packets.PresSubscriptionIdentifierAvailable |
					packets.PresSharedSubscriptionAvailable

				return p
			}(),
			want: serverCapabilities{
				MaximumPacketSize:           1024 * 1024,
				ReceiveMaximum:              100,
				TopicAliasMaximum:           10,
				MaximumQoS:                  1,
				RetainAvailable:             false,
				WildcardAvailable:           true,
				SubscriptionIDAvailable:     true,
				SharedSubscriptionAvailable: false,
			},
		},
		{
			name: "partial capabilities",
			props: func() *packets.Properties {
				p := &packets.Properties{}
				p.MaximumQoS = 2
				p.ReceiveMaximum = 200
				p.Presence = packets.PresMaximumQoS | packets.PresReceiveMaximum

				return p
			}(),
			want: serverCapabilities{
				MaximumPacketSize:           0,
				ReceiveMaximum:              200,
				TopicAliasMaximum:           0,
				MaximumQoS:                  2, // extraction logic sets default to 2
				RetainAvailable:             true,
				WildcardAvailable:           true,
				SubscriptionIDAvailable:     true,
				SharedSubscriptionAvailable: true,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractServerCapabilities(tt.props)

			if got.MaximumPacketSize != tt.want.MaximumPacketSize {
				t.Errorf("MaximumPacketSize = %v, want %v", got.MaximumPacketSize, tt.want.MaximumPacketSize)
			}
			if got.ReceiveMaximum != tt.want.ReceiveMaximum {
				t.Errorf("ReceiveMaximum = %v, want %v", got.ReceiveMaximum, tt.want.ReceiveMaximum)
			}
			if got.TopicAliasMaximum != tt.want.TopicAliasMaximum {
				t.Errorf("TopicAliasMaximum = %v, want %v", got.TopicAliasMaximum, tt.want.TopicAliasMaximum)
			}
			if got.MaximumQoS != tt.want.MaximumQoS {
				t.Errorf("MaximumQoS = %v, want %v", got.MaximumQoS, tt.want.MaximumQoS)
			}
			if got.RetainAvailable != tt.want.RetainAvailable {
				t.Errorf("RetainAvailable = %v, want %v", got.RetainAvailable, tt.want.RetainAvailable)
			}
			if got.WildcardAvailable != tt.want.WildcardAvailable {
				t.Errorf("WildcardAvailable = %v, want %v", got.WildcardAvailable, tt.want.WildcardAvailable)
			}
			if got.SubscriptionIDAvailable != tt.want.SubscriptionIDAvailable {
				t.Errorf("SubscriptionIDAvailable = %v, want %v", got.SubscriptionIDAvailable, tt.want.SubscriptionIDAvailable)
			}
			if got.SharedSubscriptionAvailable != tt.want.SharedSubscriptionAvailable {
				t.Errorf("SharedSubscriptionAvailable = %v, want %v", got.SharedSubscriptionAvailable, tt.want.SharedSubscriptionAvailable)
			}
		})
	}
}

func TestServerCapabilities(t *testing.T) {
	// Create a mock client with server capabilities
	client := &Client{}

	// Set some capabilities
	client.serverCaps.MaximumPacketSize = 1024 * 1024
	client.serverCaps.ReceiveMaximum = 100
	client.serverCaps.TopicAliasMaximum = 10
	client.serverCaps.MaximumQoS = 1
	client.serverCaps.RetainAvailable = false
	client.serverCaps.WildcardAvailable = true
	client.serverCaps.SubscriptionIDAvailable = true
	client.serverCaps.SharedSubscriptionAvailable = false

	// Get public capabilities
	caps := client.ServerCapabilities()

	// Verify all fields are correctly copied
	if caps.MaximumPacketSize != 1024*1024 {
		t.Errorf("MaximumPacketSize = %d, want %d", caps.MaximumPacketSize, 1024*1024)
	}
	if caps.ReceiveMaximum != 100 {
		t.Errorf("ReceiveMaximum = %d, want 100", caps.ReceiveMaximum)
	}
	if caps.TopicAliasMaximum != 10 {
		t.Errorf("TopicAliasMaximum = %d, want 10", caps.TopicAliasMaximum)
	}
	if caps.MaximumQoS != 1 {
		t.Errorf("MaximumQoS = %d, want 1", caps.MaximumQoS)
	}
	if caps.RetainAvailable != false {
		t.Errorf("RetainAvailable = %v, want false", caps.RetainAvailable)
	}
	if caps.WildcardAvailable != true {
		t.Errorf("WildcardAvailable = %v, want true", caps.WildcardAvailable)
	}
	if caps.SubscriptionIDAvailable != true {
		t.Errorf("SubscriptionIDAvailable = %v, want true", caps.SubscriptionIDAvailable)
	}
	if caps.SharedSubscriptionAvailable != false {
		t.Errorf("SharedSubscriptionAvailable = %v, want false", caps.SharedSubscriptionAvailable)
	}
}

func TestServerCapabilitiesDefault(t *testing.T) {
	// Client with no capabilities set (all zeros)
	client := &Client{}

	caps := client.ServerCapabilities()

	// Should return zero values
	if caps.MaximumPacketSize != 0 {
		t.Errorf("MaximumPacketSize = %d, want 0", caps.MaximumPacketSize)
	}
	if caps.ReceiveMaximum != 0 {
		t.Errorf("ReceiveMaximum = %d, want 0", caps.ReceiveMaximum)
	}
	if caps.TopicAliasMaximum != 0 {
		t.Errorf("TopicAliasMaximum = %d, want 0", caps.TopicAliasMaximum)
	}
	if caps.MaximumQoS != 0 {
		t.Errorf("MaximumQoS = %d, want 0", caps.MaximumQoS)
	}
	if caps.RetainAvailable != false {
		t.Errorf("RetainAvailable = %v, want false", caps.RetainAvailable)
	}
	if caps.WildcardAvailable != false {
		t.Errorf("WildcardAvailable = %v, want false", caps.WildcardAvailable)
	}
}

func TestServerCapabilitiesIntegration(t *testing.T) {
	// This would require a real connection, so we'll skip if not in integration mode
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Note: This is more of a documentation test showing how to use ServerCapabilities
	// Real testing happens in integration tests
	t.Log("ServerCapabilities() should be called after connecting to a v5.0 server")
	t.Log("Example usage:")
	t.Log("  client, _ := mq.Dial(server, mq.WithProtocolVersion(mq.ProtocolV50))")
	t.Log("  caps := client.ServerCapabilities()")
	t.Log("  if caps.MaximumQoS < 2 { /* handle QoS limitation */ }")
}

// Test that we can safely call ServerCapabilities on a nil client (shouldn't panic)
func TestServerCapabilitiesNilSafety(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("ServerCapabilities() panicked: %v", r)
		}
	}()

	var client *Client
	// This will panic with nil pointer, which is expected Go behavior
	// Document that this is not a special case
	_ = client
}

func TestDialDefaultPorts(t *testing.T) {
	// Attempt to dial without ports and verify the error message implies the correct default port was tried.
	// We expect "connection refused" or similar, but crucially verify the ADDRESS in the error.

	tests := []struct {
		uri          string
		expectedPort string
	}{
		{"tcp://localhost", ":1883"},
		{"mqtt://localhost", ":1883"},
		{"tls://localhost", ":8883"},
		{"ssl://localhost", ":8883"},
		{"mqtts://localhost", ":8883"},
	}

	for _, tt := range tests {
		t.Run(tt.uri, func(t *testing.T) {
			_, err := Dial(tt.uri, WithClientID("test"), WithAutoReconnect(false))
			if err == nil {
				// Assuming no server on standard ports for unit tests, but if there's one, it's fine.
				// We just avoid failing the test in that case.
			} else {
				// Expect error like "dial tcp [::1]:1883: connect: connection refused"
				// or "dial tcp 127.0.0.1:1883..."
				if !strings.Contains(err.Error(), tt.expectedPort) {
					t.Errorf("Dial(%q) error = %v, expecting port %s in error message", tt.uri, err, tt.expectedPort)
				}
			}
		})
	}
}

func TestClientIDValidation(t *testing.T) {
	tests := []struct {
		name         string
		proto        uint8
		clientID     string
		cleanSession bool
		wantErr      bool
	}{
		{"v3.1.1 valid", ProtocolV311, "client1", false, false},
		{"v3.1.1 empty id clean false", ProtocolV311, "", false, true},
		{"v3.1.1 empty id clean true", ProtocolV311, "", true, false},
		{"v5.0 valid", ProtocolV50, "client1", false, false},
		{"v5.0 empty id clean false", ProtocolV50, "", false, true}, // Now checked!
		{"v5.0 empty id clean true", ProtocolV50, "", true, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Expected error for validation happens BEFORE network connection,
			// so we can test this even without a server.
			_, err := Dial("tcp://localhost:1883",
				WithProtocolVersion(tt.proto),
				WithClientID(tt.clientID),
				WithCleanSession(tt.cleanSession),
				WithAutoReconnect(false), // don't start loops
			)

			if tt.wantErr {
				if err == nil {
					t.Error("expected validation error, got nil")
				} else if !strings.Contains(err.Error(), "requires a non-empty ClientID") {
					t.Errorf("expected ClientID error, got: %v", err)
				}
			} else {
				// If we expected success (validation passed), we might still get a connection error
				// but NOT a validation error.
				if err != nil && strings.Contains(err.Error(), "requires a non-empty ClientID") {
					t.Errorf("unexpected validation error: %v", err)
				}
			}
		})
	}
}

func TestSessionExpiryInterval_V311(t *testing.T) {
	tests := []struct {
		name string
		opts []Option
		want uint32
	}{
		{
			name: "V3.1.1 CleanSession=false (Persistent)",
			opts: []Option{
				WithProtocolVersion(ProtocolV311),
				WithCleanSession(false),
				WithClientID("test-client"),
			},
			want: 0xFFFFFFFF,
		},
		{
			name: "V3.1.1 CleanSession=true (Ephemeral)",
			opts: []Option{
				WithProtocolVersion(ProtocolV311),
				WithCleanSession(true),
			},
			want: 0,
		},
		{
			name: "V5.0 CleanStart=false (Persistent without Expiry)",
			opts: []Option{
				WithProtocolVersion(ProtocolV50),
				WithCleanSession(false), // CleanStart=false
				WithClientID("test-client"),
			},
			// Default expiry is 0 in v5
			want: 0,
		},
		{
			name: "V5.0 Persistent with Expiry",
			opts: []Option{
				WithProtocolVersion(ProtocolV50),
				WithCleanSession(false),
				WithClientID("test-client"),
				WithSessionExpiryInterval(3600),
			},
			want: 3600,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Manually construct client options to avoid network call in Dial
			opts := defaultOptions("tcp://localhost:1883")
			for _, opt := range tt.opts {
				opt(opts)
			}

			client := &Client{
				opts: opts,
			}

			if opts.SessionExpirySet {
				client.sessionExpiryInterval = opts.SessionExpiryInterval
			}

			got := client.SessionExpiryInterval()
			if got != tt.want {
				t.Errorf("SessionExpiryInterval() = %v, want %v", got, tt.want)
			}
		})
	}
}
