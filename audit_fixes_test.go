package mq

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/gonzalop/mq/internal/packets"
)

// Overlapping subscriptions must each get their own Message.Context, and one
// handler finishing must not cancel another's context.
func TestDispatch_MultipleHandlersOwnContext(t *testing.T) {
	c := newTestClient(nil)

	var started sync.WaitGroup
	started.Add(2)
	release := make(chan struct{})
	ctxs := make(chan context.Context, 2)
	errs := make(chan error, 1)

	first := func(_ *Client, m Message) {
		ctxs <- m.Context
		started.Done()
		// Return right away: this cancels only this handler's context.
	}
	second := func(_ *Client, m Message) {
		ctxs <- m.Context
		started.Done()
		<-release
		if m.Context.Err() != nil {
			errs <- m.Context.Err()
		}
		close(errs)
	}

	p := &packets.PublishPacket{Topic: "sensors/temp"}
	c.dispatchAndAcknowledge(p, Message{Topic: p.Topic}, []MessageHandler{first, second})

	started.Wait()
	time.Sleep(20 * time.Millisecond) // let first return and cancel its ctx
	close(release)

	if err := <-errs; err != nil {
		t.Fatalf("second handler's context was cancelled: %v", err)
	}
	if a, b := <-ctxs, <-ctxs; a == b {
		t.Fatal("handlers shared the same context")
	}
}

func TestHandleSuback_RejectedTopicRemoved(t *testing.T) {
	c := newTestClient(nil)
	store := newMockSubscriptionStore()
	store.storedSubs["denied/#"] = &PersistedSubscription{}
	c.opts.SessionStore = store

	h := func(*Client, Message) {}
	c.addSubscriptionLocked("ok/#", subscriptionEntry{handler: h, options: SubscribeOptions{Persistence: true}})
	c.addSubscriptionLocked("denied/#", subscriptionEntry{handler: h, options: SubscribeOptions{Persistence: true}})

	tok := newToken()
	c.pending[1] = &pendingOp{
		token:  tok,
		packet: &packets.SubscribePacket{Topics: []string{"ok/#", "denied/#"}},
	}
	c.handleSuback(&packets.SubackPacket{PacketID: 1, ReturnCodes: []uint8{0x00, 0x80}})

	if tok.Error() == nil {
		t.Error("expected subscription error")
	}
	if _, ok := c.subscriptions["denied/#"]; ok {
		t.Error("rejected filter still in subscriptions")
	}
	if len(c.trie.Match("denied/x")) != 0 {
		t.Error("rejected filter still routed")
	}
	if _, ok := store.storedSubs["denied/#"]; ok {
		t.Error("rejected filter still persisted")
	}
	if _, ok := c.subscriptions["ok/#"]; !ok {
		t.Error("accepted filter was removed")
	}
}

func TestHandleSuback_FewerReturnCodesRemovesUnacknowledgedTopics(t *testing.T) {
	c := newTestClient(nil)
	store := newMockSubscriptionStore()
	store.storedSubs["trailing/#"] = &PersistedSubscription{}
	c.opts.SessionStore = store

	h := func(*Client, Message) {}
	c.addSubscriptionLocked("first/#", subscriptionEntry{handler: h, options: SubscribeOptions{Persistence: true}})
	c.addSubscriptionLocked("trailing/#", subscriptionEntry{handler: h, options: SubscribeOptions{Persistence: true}})

	tok := newToken()
	c.pending[1] = &pendingOp{
		token:  tok,
		packet: &packets.SubscribePacket{Topics: []string{"first/#", "trailing/#"}},
	}
	// Server responds with only 1 return code for 2 requested topics
	c.handleSuback(&packets.SubackPacket{PacketID: 1, ReturnCodes: []uint8{0x00}})

	if tok.Error() == nil {
		t.Error("expected subscription error due to return codes count mismatch")
	}
	if !errors.Is(tok.Error(), ErrSubscriptionFailed) {
		t.Errorf("expected ErrSubscriptionFailed, got: %v", tok.Error())
	}
	if _, ok := c.subscriptions["trailing/#"]; ok {
		t.Error("unacknowledged trailing topic filter still in subscriptions")
	}
	if len(c.trie.Match("trailing/item")) != 0 {
		t.Error("unacknowledged trailing topic filter still routed")
	}
	if _, ok := store.storedSubs["trailing/#"]; ok {
		t.Error("unacknowledged trailing topic filter still persisted")
	}
	// Note: because the overall operation errored, nothing from this batch should be newly persisted
	if _, ok := store.storedSubs["first/#"]; ok {
		t.Error("topics from partially-acknowledged batch should not be persisted on error")
	}
}

func TestHandleUnsuback_FailureKeepsPersistedSubscription(t *testing.T) {
	opts := defaultOptions("tcp://localhost:1883")
	opts.ProtocolVersion = ProtocolV50
	c := newTestClient(opts)
	store := newMockSubscriptionStore()
	store.storedSubs["t"] = &PersistedSubscription{}
	c.opts.SessionStore = store

	c.pending[1] = &pendingOp{
		token:  newToken(),
		packet: &packets.UnsubscribePacket{Topics: []string{"t"}},
	}
	c.handleUnsuback(&packets.UnsubackPacket{PacketID: 1, ReasonCodes: []uint8{0x87}, Version: 5})

	if _, ok := store.storedSubs["t"]; !ok {
		t.Error("subscription deleted from store although unsubscribe failed")
	}
}

func TestProcessPublishQueue_SkipsExpired(t *testing.T) {
	c := newTestClient(nil)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	expired := &publishRequest{ctx: ctx, packet: &packets.PublishPacket{Topic: "a", QoS: 1}, token: newToken()}
	live := &publishRequest{ctx: context.Background(), packet: &packets.PublishPacket{Topic: "b", QoS: 1}, token: newToken()}
	c.publishQueue = []*publishRequest{expired, live}

	toSend := c.processPublishQueueLocked()

	if !errors.Is(expired.token.Error(), context.Canceled) {
		t.Errorf("expired request error = %v, want context.Canceled", expired.token.Error())
	}
	if len(toSend) != 1 || toSend[0].(*packets.PublishPacket).Topic != "b" {
		t.Fatalf("toSend = %v, want only topic b", toSend)
	}
	if c.inFlightCount != 1 {
		t.Errorf("inFlightCount = %d, want 1", c.inFlightCount)
	}
}

func TestToPublishProperties_DropsReasonString(t *testing.T) {
	props := toPublishProperties(&Properties{ReasonString: "x", ContentType: "text/plain"})
	if props.Presence&packets.PresReasonString != 0 || props.ReasonString != "" {
		t.Error("ReasonString should not be sent on PUBLISH")
	}
	if props.ContentType != "text/plain" {
		t.Error("other properties should be kept")
	}
	if toPublishProperties(nil) != nil {
		t.Error("nil in should give nil out")
	}
}
