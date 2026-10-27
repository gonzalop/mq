package mq

import (
	"context"
	"errors"
	"net"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gonzalop/mq/internal/packets"
)

// TestDisconnectWhenNotConnectedStopsLoops checks that Disconnect still stops
// the client when it is called while disconnected (for example during an
// automatic reconnect). Before the fix it returned early without closing stop,
// so the reconnect loop kept running and calls waiting on a token never
// completed.
func TestDisconnectWhenNotConnectedStopsLoops(t *testing.T) {
	c := &Client{
		stop: make(chan struct{}),
		opts: defaultOptions(""),
	}
	if c.connected.Load() {
		t.Fatal("precondition: the client starts disconnected")
	}

	if err := c.disconnectWithReason(context.Background(), uint8(ReasonCodeNormalDisconnect), nil, true); err != nil {
		t.Fatalf("Disconnect returned error: %v", err)
	}

	select {
	case <-c.stop:
		// stopped as expected
	default:
		t.Fatal("stop was not closed: the background loops would keep running")
	}

	// Idempotent: a second call must not panic on a double close.
	if err := c.disconnectWithReason(context.Background(), uint8(ReasonCodeNormalDisconnect), nil, true); err != nil {
		t.Fatalf("second Disconnect returned error: %v", err)
	}
}

// Regression test for Finding 11: a graceful Disconnect should not take a fixed 100ms+ when the
// DISCONNECT can be written immediately.
func TestDisconnect_NoFixedSleep(t *testing.T) {
	opts := defaultOptions("tcp://x")
	c := &Client{opts: opts, outgoing: make(chan packets.Packet, 10), stop: make(chan struct{})}
	c.connected.Store(true)

	start := time.Now()
	_ = c.disconnectWithReason(context.Background(), 0, nil, false)
	if d := time.Since(start); d >= 100*time.Millisecond {
		t.Errorf("disconnect took %v; it sleeps a fixed 100ms", d)
	}
}

// TestRequestsAfterDisconnect checks that Publish, Subscribe and Unsubscribe
// after Disconnect fail with ErrClientDisconnected instead of panicking on the
// session maps the logic loop cleared when it stopped.
func TestRequestsAfterDisconnect(t *testing.T) {
	c := newTestClient(nil)
	c.wg.Add(1)
	go c.logicLoop()
	if err := c.Disconnect(context.Background()); err != nil {
		t.Fatalf("Disconnect: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	toks := map[string]Token{
		"publish":     c.Publish(ctx, "a/b", []byte("x"), WithQoS(1)),
		"subscribe":   c.Subscribe(ctx, "a/b", 1, func(*Client, Message) {}),
		"unsubscribe": c.Unsubscribe(ctx, "a/b"),
	}
	for name, tok := range toks {
		if err := tok.Wait(ctx); !errors.Is(err, ErrClientDisconnected) {
			t.Errorf("%s after Disconnect: got %v, want ErrClientDisconnected", name, err)
		}
	}
}

func TestRequestsStopDuringSend_ReturnsSentinelError(t *testing.T) {
	c := &Client{
		trie:          newTopicTrie(),
		opts:          defaultOptions("tcp://x"),
		pending:       make(map[uint16]*pendingOp),
		subscriptions: make(map[string]subscriptionEntry),
		outgoing:      make(chan packets.Packet), // unbuffered so send blocks
		stop:          make(chan struct{}),
	}
	c.connState.Store(&connectionState{caps: extractServerCapabilities(nil)})

	// 1. Publish blocked on outgoing
	pubTok := newToken()
	pubDone := make(chan struct{})
	go func() {
		c.internalPublish(&publishRequest{
			ctx:    context.Background(),
			packet: &packets.PublishPacket{Topic: "t", QoS: 1},
			token:  pubTok,
		})
		close(pubDone)
	}()

	// 2. Subscribe blocked on outgoing
	subTok := newToken()
	subDone := make(chan struct{})
	go func() {
		c.internalSubscribe(&subscribeRequest{
			ctx:    context.Background(),
			packet: &packets.SubscribePacket{Topics: []string{"sub/t"}},
			token:  subTok,
		})
		close(subDone)
	}()

	// 3. Unsubscribe blocked on outgoing
	unsubTok := newToken()
	unsubDone := make(chan struct{})
	go func() {
		c.internalUnsubscribe(&unsubscribeRequest{
			ctx:    context.Background(),
			packet: &packets.UnsubscribePacket{Topics: []string{"unsub/t"}},
			token:  unsubTok,
		})
		close(unsubDone)
	}()

	// Each request registers itself in pending just before blocking on the
	// unbuffered outgoing channel, which has no reader. Once all three are
	// registered, closing stop can only resolve through the stop branch.
	deadline := time.Now().Add(5 * time.Second)
	for {
		c.sessionLock.Lock()
		n := len(c.pending)
		c.sessionLock.Unlock()
		if n == 3 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("requests never reached the send: %d pending", n)
		}
		time.Sleep(time.Millisecond)
	}
	close(c.stop)

	<-pubDone
	<-subDone
	<-unsubDone

	if err := pubTok.Error(); !errors.Is(err, ErrClientDisconnected) {
		t.Errorf("publish token on stop: got %v, want ErrClientDisconnected", err)
	}
	if err := subTok.Error(); !errors.Is(err, ErrClientDisconnected) {
		t.Errorf("subscribe token on stop: got %v, want ErrClientDisconnected", err)
	}
	if err := unsubTok.Error(); !errors.Is(err, ErrClientDisconnected) {
		t.Errorf("unsubscribe token on stop: got %v, want ErrClientDisconnected", err)
	}
}

// TestProtocolErrorReconnects checks that a protocol violation by the server
// drops only the connection: the client sends DISCONNECT 0x82 (v5.0), reports
// the loss through OnConnectionLost and reconnects. It used to call the
// user-facing Disconnect, which stopped the client for good without telling
// anyone.
func TestProtocolErrorReconnects(t *testing.T) {
	var dials atomic.Int32
	gotReason := make(chan uint8, 1)

	dialer := DialFunc(func(context.Context, string, string) (net.Conn, error) {
		n := dials.Add(1)
		cli, srv := net.Pipe()
		go func() {
			defer srv.Close()
			if _, err := packets.ReadPacket(srv, 5, 0); err != nil {
				return
			}
			connack := &packets.ConnackPacket{ReturnCode: packets.ConnAccepted, Properties: &packets.Properties{}}
			if _, err := connack.WriteTo(srv); err != nil {
				return
			}
			if n == 1 {
				// Reserved packet type 0: a protocol error.
				if _, err := srv.Write([]byte{0x00, 0x00}); err != nil {
					return
				}
			}
			for {
				pkt, err := packets.ReadPacket(srv, 5, 0)
				if err != nil {
					return
				}
				if d, ok := pkt.(*packets.DisconnectPacket); ok && n == 1 {
					gotReason <- d.ReasonCode
				}
			}
		}()
		return cli, nil
	})

	lost := make(chan error, 1)
	c, err := Dial("tcp://mock:1883",
		WithClientID("proto-err"),
		WithProtocolVersion(ProtocolV50),
		WithDialer(dialer),
		WithReconnectBackoff(10*time.Millisecond, 10*time.Millisecond, false),
		WithOnConnectionLost(func(_ *Client, err error) { lost <- err }),
	)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer c.Disconnect(context.Background())

	select {
	case rc := <-gotReason:
		if rc != uint8(ReasonCodeProtocolError) {
			t.Errorf("DISCONNECT reason = 0x%02x, want 0x82", rc)
		}
	case <-time.After(2 * time.Second):
		t.Error("server never received DISCONNECT 0x82")
	}

	select {
	case <-lost:
	case <-time.After(2 * time.Second):
		t.Fatal("OnConnectionLost was not called")
	}

	deadline := time.Now().Add(2 * time.Second)
	for dials.Load() < 2 || !c.IsConnected() {
		if time.Now().After(deadline) {
			t.Fatalf("client did not reconnect (dials=%d, connected=%v)", dials.Load(), c.IsConnected())
		}
		time.Sleep(10 * time.Millisecond)
	}
	if c.stopped() {
		t.Error("client was stopped by a protocol error")
	}
}

type blockingCloseStore struct {
	dummySessionStore
	release chan struct{}
}

func (b *blockingCloseStore) Close() error {
	<-b.release
	return nil
}

// TestDisconnect_StoreCloseRespectsContext checks that a session store whose
// Close does not return (e.g. an AsyncStore draining to a stuck backend) does
// not make Disconnect ignore its context.
func TestDisconnect_StoreCloseRespectsContext(t *testing.T) {
	store := &blockingCloseStore{release: make(chan struct{})}
	defer close(store.release)

	opts := defaultOptions("tcp://x:1883")
	opts.SessionStore = store
	c := newTestClient(opts)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	start := time.Now()
	err := c.Disconnect(ctx)
	if d := time.Since(start); d > time.Second {
		t.Fatalf("Disconnect took %v with a 100ms context", d)
	}
	if err == nil || !strings.Contains(err.Error(), "session store still flushing") {
		t.Errorf("Disconnect() = %v, want a session store flushing error", err)
	}
}

// TestDisconnectDuringReconnect checks that Disconnect called while a
// reconnect attempt is dialing stops that attempt: when the dial completes
// afterwards, the new connection is closed, OnConnect does not run, and
// Disconnect returns without timing out.
func TestDisconnectDuringReconnect(t *testing.T) {
	var dials, connects atomic.Int32
	dialing := make(chan struct{})
	release := make(chan struct{})
	secondClosed := make(chan struct{})

	serve := func(srv net.Conn, dropAfterConnack bool) {
		defer srv.Close()
		if _, err := packets.ReadPacket(srv, 4, 0); err != nil {
			return
		}
		if _, err := (&packets.ConnackPacket{ReturnCode: packets.ConnAccepted}).WriteTo(srv); err != nil {
			return
		}
		if dropAfterConnack {
			return
		}
		for {
			if _, err := packets.ReadPacket(srv, 4, 0); err != nil {
				return
			}
		}
	}

	dialer := DialFunc(func(context.Context, string, string) (net.Conn, error) {
		cli, srv := net.Pipe()
		if dials.Add(1) == 1 {
			go serve(srv, true) // drop the first connection to start a reconnect
			return cli, nil
		}
		// The reconnect attempt: dial completes only after Disconnect began,
		// and ignores ctx like a slow network would.
		close(dialing)
		<-release
		go func() {
			serve(srv, false)
			close(secondClosed)
		}()
		return cli, nil
	})

	c, err := Dial("tcp://mock:1883",
		WithClientID("reconnect-stop"),
		WithProtocolVersion(ProtocolV311),
		WithDialer(dialer),
		WithReconnectBackoff(10*time.Millisecond, 10*time.Millisecond, false),
		WithOnConnect(func(*Client) { connects.Add(1) }),
	)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}

	select {
	case <-dialing:
	case <-time.After(2 * time.Second):
		t.Fatal("reconnect attempt never started")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	disconnected := make(chan error, 1)
	go func() { disconnected <- c.Disconnect(ctx) }()

	time.Sleep(50 * time.Millisecond) // let Disconnect stop the client
	close(release)

	select {
	case err := <-disconnected:
		if err != nil {
			t.Errorf("Disconnect: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Disconnect did not return")
	}

	select {
	case <-secondClosed:
	case <-time.After(time.Second):
		t.Error("the connection completed after Disconnect was left open")
	}

	time.Sleep(50 * time.Millisecond) // OnConnect runs in a goroutine
	if n := connects.Load(); n != 1 {
		t.Errorf("OnConnect ran %d times, want 1 (not for the connection completed after Disconnect)", n)
	}
}

// newStalledDisconnectClient returns a connected client whose write loop never
// runs, so the DISCONNECT is queued but never flushed unless the test signals it.
func newStalledDisconnectClient(t *testing.T) *Client {
	t.Helper()
	c := newTestClient(nil)
	cli, srv := net.Pipe()
	t.Cleanup(func() { cli.Close(); srv.Close() })
	c.conn = cli
	c.connDone = make(chan struct{})
	c.connected.Store(true)
	return c
}

func TestDisconnect_NoDeadlineCapsFlushWait(t *testing.T) {
	c := newStalledDisconnectClient(t)

	start := time.Now()
	_ = c.disconnectWithReason(context.Background(), 0, nil, false)
	elapsed := time.Since(start)

	if elapsed < disconnectFlushTimeout-100*time.Millisecond || elapsed > disconnectFlushTimeout+time.Second {
		t.Errorf("Disconnect took %v, want about %v", elapsed, disconnectFlushTimeout)
	}
}

func TestDisconnect_DeadlineExtendsFlushWait(t *testing.T) {
	c := newStalledDisconnectClient(t)

	flushAfter := disconnectFlushTimeout + 300*time.Millisecond
	go func() {
		time.Sleep(flushAfter)
		c.disconnectFlushed <- struct{}{}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	start := time.Now()
	_ = c.disconnectWithReason(ctx, 0, nil, false)
	elapsed := time.Since(start)

	if elapsed < flushAfter {
		t.Errorf("Disconnect gave up after %v, before the flush at %v", elapsed, flushAfter)
	}
	if elapsed > 4*time.Second {
		t.Errorf("Disconnect took %v, should return right after the flush", elapsed)
	}
}

// disconnectingStore calls Disconnect from inside finalizeConnection, after
// its first stopped() check, to hit the window where Disconnect used to leave
// the client marked as connected.
type disconnectingStore struct {
	dummySessionStore
	c *Client
}

func (s *disconnectingStore) ClearReceivedQoS2() error {
	return s.c.disconnectWithReason(context.Background(), 0, nil, false)
}

// TestDisconnectDuringFinalizeConnection checks that a Disconnect racing with
// the end of a connect leaves the client disconnected and skips OnConnect.
func TestDisconnectDuringFinalizeConnection(t *testing.T) {
	var connects atomic.Int32
	opts := defaultOptions("tcp://localhost:1883")
	opts.CleanSession = false
	opts.Logger = testLogger()
	opts.OnConnect = func(*Client) { connects.Add(1) }
	store := &disconnectingStore{}
	opts.SessionStore = store
	c := newTestClient(opts)
	store.c = c

	cli, srv := net.Pipe()
	defer srv.Close()
	c.conn = cli

	err := c.finalizeConnection(&packets.ConnackPacket{ReturnCode: packets.ConnAccepted})
	if !errors.Is(err, ErrClientDisconnected) {
		t.Fatalf("finalizeConnection error = %v, want ErrClientDisconnected", err)
	}
	if c.IsConnected() {
		t.Error("client reports connected after Disconnect")
	}
	if n := connects.Load(); n != 0 {
		t.Errorf("OnConnect ran %d times, want 0", n)
	}
}
