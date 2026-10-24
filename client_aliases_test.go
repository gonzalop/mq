package mq

import (
	"context"
	"io"
	"log/slog"
	"maps"
	"net"
	"testing"
	"time"

	"github.com/gonzalop/mq/internal/packets"
)

func TestTopicAliasesPrepare(t *testing.T) {
	tests := []struct {
		name        string
		limit       uint16
		existing    map[string]uint16
		useAlias    bool
		topic       string
		wantAliasID uint16 // 0 = no alias
		wantTopic   string
		wantMapSize int
	}{
		{
			name:        "aliases disabled (limit 0)",
			limit:       0,
			useAlias:    true,
			topic:       "test/topic",
			wantTopic:   "test/topic",
			wantMapSize: 0,
		},
		{
			name:        "publish without WithAlias",
			limit:       10,
			useAlias:    false,
			topic:       "test/topic",
			wantTopic:   "test/topic",
			wantMapSize: 0,
		},
		{
			name:        "first alias allocation",
			limit:       10,
			useAlias:    true,
			topic:       "sensors/temp",
			wantAliasID: 1,
			wantTopic:   "sensors/temp", // First time sends both
			wantMapSize: 1,
		},
		{
			name:        "reuse existing alias",
			limit:       10,
			existing:    map[string]uint16{"sensors/temp": 1},
			useAlias:    true,
			topic:       "sensors/temp",
			wantAliasID: 1,
			wantTopic:   "", // Subsequent sends empty topic
			wantMapSize: 1,
		},
		{
			name:        "allocate second alias",
			limit:       10,
			existing:    map[string]uint16{"topic1": 1},
			useAlias:    true,
			topic:       "topic2",
			wantAliasID: 2,
			wantTopic:   "topic2",
			wantMapSize: 2,
		},
		{
			name:        "last alias below the limit",
			limit:       2,
			existing:    map[string]uint16{"topic1": 1},
			useAlias:    true,
			topic:       "topic2",
			wantAliasID: 2,
			wantTopic:   "topic2",
			wantMapSize: 2,
		},
		{
			name:        "limit reached",
			limit:       2,
			existing:    map[string]uint16{"topic1": 1, "topic2": 2},
			useAlias:    true,
			topic:       "new/topic",
			wantTopic:   "new/topic", // Graceful degradation
			wantMapSize: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			aliases := newTopicAliases(tt.limit)
			maps.Copy(aliases.ids, tt.existing)

			pkt := &packets.PublishPacket{Topic: tt.topic, UseAlias: tt.useAlias}
			out := aliases.prepare(pkt).(*packets.PublishPacket)

			var gotAlias uint16
			if out.Properties != nil && out.Properties.Presence&packets.PresTopicAlias != 0 {
				gotAlias = out.Properties.TopicAlias
			}
			if gotAlias != tt.wantAliasID {
				t.Errorf("alias = %d, want %d", gotAlias, tt.wantAliasID)
			}
			if out.Topic != tt.wantTopic {
				t.Errorf("topic = %q, want %q", out.Topic, tt.wantTopic)
			}
			if len(aliases.ids) != tt.wantMapSize {
				t.Errorf("map size = %d, want %d", len(aliases.ids), tt.wantMapSize)
			}
			// The stored packet is never modified.
			if pkt.Topic != tt.topic || pkt.Properties != nil {
				t.Errorf("prepare modified the original packet: %+v", pkt)
			}
		})
	}
}

func TestTopicAliasesPrepareKeepsProperties(t *testing.T) {
	aliases := newTopicAliases(10)
	pkt := &packets.PublishPacket{
		Topic:    "a/b",
		UseAlias: true,
		Properties: &packets.Properties{
			ContentType: "text/plain",
			Presence:    packets.PresContentType,
		},
	}
	out := aliases.prepare(pkt).(*packets.PublishPacket)
	if out.Properties.ContentType != "text/plain" || out.Properties.Presence&packets.PresContentType == 0 {
		t.Errorf("content type lost: %+v", out.Properties)
	}
	if pkt.Properties.Presence&packets.PresTopicAlias != 0 {
		t.Error("prepare set the alias on the original properties")
	}
}

func TestOutgoingAliasLimit(t *testing.T) {
	tests := []struct {
		name        string
		version     uint8
		clientLimit uint16
		serverLimit uint16
		want        uint16
	}{
		{"v3.1.1", ProtocolV311, 10, 10, 0},
		{"client did not enable aliases", ProtocolV50, 0, 10, 0},
		{"server does not accept aliases", ProtocolV50, 10, 0, 0},
		{"server limit is smaller", ProtocolV50, 10, 3, 3},
		{"client limit is smaller", ProtocolV50, 3, 10, 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := defaultOptions("tcp://x:1883")
			opts.ProtocolVersion = tt.version
			opts.TopicAliasMaximum = tt.clientLimit
			c := newTestClient(opts)
			st := &connectionState{caps: extractServerCapabilities(nil)}
			st.caps.TopicAliasMaximum = tt.serverLimit
			c.connState.Store(st)
			if got := c.outgoingAliasLimit(); got != tt.want {
				t.Errorf("outgoingAliasLimit() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestHandleIncomingTopicAlias(t *testing.T) {
	t.Run("register and resolve alias", func(t *testing.T) {
		c := &Client{trie: newTopicTrie(),
			opts: &clientOptions{
				ProtocolVersion:   ProtocolV50,
				TopicAliasMaximum: 10,
				Logger:            testLogger(),
			},
			receivedAliases: make(map[uint16]string),
		}

		// 1. Incoming packet with both topic and alias
		p1 := &packets.PublishPacket{
			Topic: "sensors/temp",
			Properties: &packets.Properties{
				TopicAlias: 1,
				Presence:   packets.PresTopicAlias,
			},
		}
		c.handlePublish(p1)

		// Verify registration
		c.receivedAliasesLock.RLock()
		if c.receivedAliases[1] != "sensors/temp" {
			t.Errorf("expected alias 1 to be 'sensors/temp', got %q", c.receivedAliases[1])
		}
		c.receivedAliasesLock.RUnlock()

		// 2. Incoming packet with only alias
		p2 := &packets.PublishPacket{
			Topic: "",
			Properties: &packets.Properties{
				TopicAlias: 1,
				Presence:   packets.PresTopicAlias,
			},
		}
		c.handlePublish(p2)

		// Verify resolution
		if p2.Topic != "sensors/temp" {
			t.Errorf("expected p2.Topic to be 'sensors/temp', got %q", p2.Topic)
		}
	})

	t.Run("invalid alias 0", func(t *testing.T) {
		c := &Client{trie: newTopicTrie(),
			opts: &clientOptions{
				ProtocolVersion: ProtocolV50,
				Logger:          testLogger(),
			},
		}

		p := &packets.PublishPacket{
			Topic: "test",
			Properties: &packets.Properties{
				TopicAlias: 0,
				Presence:   packets.PresTopicAlias,
			},
		}
		// This should log an error and NOT register anything
		c.handlePublish(p)

		if len(c.receivedAliases) != 0 {
			t.Errorf("expected no aliases to be registered for alias 0")
		}
	})

	t.Run("server exceeds TopicAliasMaximum", func(t *testing.T) {
		c := &Client{trie: newTopicTrie(),
			opts: &clientOptions{
				ProtocolVersion:   ProtocolV50,
				TopicAliasMaximum: 5,
				Logger:            testLogger(),
			},
		}

		p := &packets.PublishPacket{
			Topic: "test",
			Properties: &packets.Properties{
				TopicAlias: 10, // Exceeds 5
				Presence:   packets.PresTopicAlias,
			},
		}
		c.handlePublish(p)

		if len(c.receivedAliases) != 0 {
			t.Errorf("expected no aliases to be registered when limit exceeded")
		}
	})

	t.Run("server sends alias when TopicAliasMaximum is zero", func(t *testing.T) {
		c := &Client{trie: newTopicTrie(),
			opts: &clientOptions{
				ProtocolVersion:   ProtocolV50,
				TopicAliasMaximum: 0,
				Logger:            testLogger(),
			},
			receivedAliases: make(map[uint16]string),
		}

		p := &packets.PublishPacket{
			Topic: "test",
			Properties: &packets.Properties{
				TopicAlias: 1, // Any alias should be rejected when max is 0
				Presence:   packets.PresTopicAlias,
			},
		}
		c.handlePublish(p)

		if len(c.receivedAliases) != 0 {
			t.Errorf("expected no aliases to be registered when TopicAliasMaximum is 0")
		}
	})

	t.Run("unknown alias", func(t *testing.T) {
		c := &Client{trie: newTopicTrie(),
			opts: &clientOptions{
				ProtocolVersion:   ProtocolV50,
				TopicAliasMaximum: 100,
				Logger:            testLogger(),
			},
			receivedAliases: make(map[uint16]string),
		}

		p := &packets.PublishPacket{
			Topic: "",
			Properties: &packets.Properties{
				TopicAlias: 99,
				Presence:   packets.PresTopicAlias,
			},
		}
		if pkts := c.handlePublish(p); len(pkts) != 0 {
			t.Errorf("expected no ack for unknown alias, got %v", pkts)
		}

		if p.Topic != "" {
			t.Errorf("expected topic to remain empty for unknown alias")
		}
	})

	// The message must be discarded, not handed to the default handler with
	// an empty topic.
	t.Run("unknown alias is not delivered", func(t *testing.T) {
		opts := defaultOptions("tcp://x:1883")
		opts.ProtocolVersion = ProtocolV50
		opts.TopicAliasMaximum = 100
		opts.Logger = testLogger()
		delivered := make(chan string, 1)
		opts.DefaultPublishHandler = func(_ *Client, m Message) { delivered <- m.Topic }
		c := newTestClient(opts)

		p := &packets.PublishPacket{
			QoS:      1,
			PacketID: 7,
			Properties: &packets.Properties{
				TopicAlias: 99,
				Presence:   packets.PresTopicAlias,
			},
		}
		if pkts := c.handlePublish(p); len(pkts) != 0 {
			t.Errorf("expected no ack for unknown alias, got %v", pkts)
		}
		select {
		case topic := <-delivered:
			t.Errorf("message with unknown alias was delivered (topic %q)", topic)
		case <-time.After(100 * time.Millisecond):
		}
	})
}

// TestTopicAliasDefinedBeforeUse reproduces the ordering bug: a QoS 1 publish
// held back by flow control, then a QoS 0 publish to the same topic. Aliases
// used to be assigned at Publish time, so the QoS 0 packet went out first
// carrying only an alias the server had never seen. They are now assigned
// when the packet is written, so whichever packet is written first defines
// the alias.
func TestTopicAliasDefinedBeforeUse(t *testing.T) {
	opts := defaultOptions("tcp://x:1883")
	opts.ProtocolVersion = ProtocolV50
	opts.Logger = testLogger()
	c := newTestClient(opts)
	st := &connectionState{caps: extractServerCapabilities(nil)}
	st.caps.ReceiveMaximum = 1
	c.connState.Store(st)
	c.inFlightCount = 1 // flow control window is full

	ctx := context.Background()
	c.Publish(ctx, "a/b", []byte("1"), WithQoS(1), WithAlias())
	c.Publish(ctx, "a/b", []byte("2"), WithQoS(0), WithAlias())
	if len(c.publishQueue) != 1 {
		t.Fatalf("expected the QoS 1 publish to be queued, queue=%d", len(c.publishQueue))
	}

	aliases := newTopicAliases(10)

	// The QoS 0 publish is written first: it must define the alias.
	first := aliases.prepare(<-c.outgoing).(*packets.PublishPacket)
	if first.Topic != "a/b" || first.Properties.TopicAlias != 1 {
		t.Errorf("first packet on the wire: topic=%q alias=%d, want topic \"a/b\" with alias 1",
			first.Topic, first.Properties.TopicAlias)
	}

	// The queued QoS 1 publish is written later and can use the alias.
	second := aliases.prepare(c.publishQueue[0].packet).(*packets.PublishPacket)
	if second.Topic != "" || second.Properties.TopicAlias != 1 {
		t.Errorf("second packet: topic=%q alias=%d, want alias-only 1", second.Topic, second.Properties.TopicAlias)
	}
}

// TestTopicAliasNewConnection checks that a stored packet (pending or
// retransmitted) defines its alias again on a new connection: each write loop
// starts with an empty alias table, and prepare never strips the topic from
// the stored packet.
func TestTopicAliasNewConnection(t *testing.T) {
	pkt := &packets.PublishPacket{Topic: "test/pending", QoS: 1, PacketID: 100, UseAlias: true, Version: 5}

	conn1 := newTopicAliases(10)
	conn1.prepare(pkt)
	if out := conn1.prepare(pkt).(*packets.PublishPacket); out.Topic != "" {
		t.Fatalf("second send on the same connection should be alias-only, got topic %q", out.Topic)
	}

	conn2 := newTopicAliases(10)
	out := conn2.prepare(pkt).(*packets.PublishPacket)
	if out.Topic != "test/pending" || out.Properties.TopicAlias != 1 {
		t.Errorf("first send on a new connection: topic=%q alias=%d, want full topic with alias 1",
			out.Topic, out.Properties.TopicAlias)
	}
}

// TestWriteLoopAppliesTopicAliases checks the wiring: packets queued with
// WithAlias leave the write loop with the alias, in order.
func TestWriteLoopAppliesTopicAliases(t *testing.T) {
	opts := defaultOptions("tcp://x:1883")
	opts.ProtocolVersion = ProtocolV50
	opts.TopicAliasMaximum = 10
	opts.Logger = testLogger()
	c := newTestClient(opts)
	st := &connectionState{caps: extractServerCapabilities(nil)}
	st.caps.TopicAliasMaximum = 5
	c.connState.Store(st)

	conn, peer := net.Pipe()
	defer peer.Close()
	connDone := make(chan struct{})
	c.wg.Add(1)
	go c.runWriteLoop(conn, connDone, 0)
	defer func() {
		close(connDone)
		c.wg.Wait()
	}()

	for range 2 {
		c.outgoing <- &packets.PublishPacket{Topic: "x/y", UseAlias: true, Version: 5}
	}

	want := []string{"x/y", ""}
	for i, wantTopic := range want {
		_ = peer.SetReadDeadline(time.Now().Add(time.Second))
		raw, err := packets.ReadPacket(peer, 5, 0)
		if err != nil {
			t.Fatalf("read packet %d: %v", i, err)
		}
		p := raw.(*packets.PublishPacket)
		if p.Topic != wantTopic || p.Properties == nil || p.Properties.TopicAlias != 1 {
			t.Errorf("packet %d: topic=%q props=%+v, want topic %q with alias 1", i, p.Topic, p.Properties, wantTopic)
		}
	}
}

func BenchmarkTopicAlias_FirstPublish(b *testing.B) {
	pkt := &packets.PublishPacket{
		Topic:    "sensors/building-a/floor-3/room-42/temperature",
		Payload:  []byte("25.5"),
		QoS:      1,
		PacketID: 10,
		UseAlias: true,
	}

	for b.Loop() {
		// A fresh table each iteration simulates the first publish
		newTopicAliases(100).prepare(pkt)
	}
}

func BenchmarkTopicAlias_SubsequentPublish(b *testing.B) {
	aliases := newTopicAliases(100)
	pkt := &packets.PublishPacket{
		Topic:    "sensors/building-a/floor-3/room-42/temperature",
		Payload:  []byte("25.5"),
		QoS:      1,
		PacketID: 10,
		UseAlias: true,
	}
	aliases.prepare(pkt)

	for b.Loop() {
		aliases.prepare(pkt)
	}
}

func BenchmarkTopicAlias_Disabled(b *testing.B) {
	aliases := newTopicAliases(0) // Disabled - early return
	pkt := &packets.PublishPacket{
		Topic:    "sensors/building-a/floor-3/room-42/temperature",
		Payload:  []byte("25.5"),
		QoS:      1,
		PacketID: 10,
		UseAlias: true,
	}

	for b.Loop() {
		aliases.prepare(pkt)
	}
}

func BenchmarkTopicAlias_Encoding_WithAlias(b *testing.B) {
	alias := uint16(1)
	pkt := &packets.PublishPacket{
		Topic:    "", // Empty when using alias
		Payload:  []byte("25.5"),
		QoS:      1,
		PacketID: 10,
		Properties: &packets.Properties{
			TopicAlias: alias,
			Presence:   packets.PresTopicAlias,
		},
	}

	for b.Loop() {
		encodeToBytes(pkt)
	}
}

func BenchmarkTopicAlias_Encoding_WithoutAlias(b *testing.B) {
	pkt := &packets.PublishPacket{
		Topic:    "sensors/building-a/floor-3/room-42/temperature",
		Payload:  []byte("25.5"),
		QoS:      1,
		PacketID: 10,
	}

	for b.Loop() {
		encodeToBytes(pkt)
	}
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
