package mq_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gonzalop/mq"
	"github.com/gonzalop/mq/internal/packets"
)

// tapProxy forwards TCP traffic between clients and the broker. It records
// the topics of every SUBSCRIBE a client sends, per connection, and can pause
// the client-to-broker direction to simulate a stalled link.
type tapProxy struct {
	listener net.Listener
	target   string
	version  uint8 // protocol version used to decode client packets

	gate sync.Mutex // held while paused; the client->broker copy waits on it

	mu         sync.Mutex
	conns      []net.Conn
	subscribes [][]string // SUBSCRIBE topics sent on each connection, in accept order
}

func newTapProxy(t *testing.T, server string, version uint8) *tapProxy {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("proxy listen: %v", err)
	}
	p := &tapProxy{listener: l, target: strings.TrimPrefix(server, "tcp://"), version: version}
	t.Cleanup(func() {
		l.Close()
		p.dropAll()
	})
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			go p.handle(c)
		}
	}()
	return p
}

func (p *tapProxy) server() string { return "tcp://" + p.listener.Addr().String() }

func (p *tapProxy) handle(client net.Conn) {
	// A small receive buffer keeps the kernel from absorbing a whole burst,
	// so pausing the proxy really stalls the client's writes.
	if tc, ok := client.(*net.TCPConn); ok {
		_ = tc.SetReadBuffer(16 << 10)
	}
	broker, err := net.Dial("tcp", p.target)
	if err != nil {
		client.Close()
		return
	}

	p.mu.Lock()
	idx := len(p.subscribes)
	p.subscribes = append(p.subscribes, nil)
	p.conns = append(p.conns, client)
	p.mu.Unlock()

	go func() {
		_, _ = io.Copy(client, broker)
		client.Close()
	}()

	pr, pw := io.Pipe()
	go p.sniff(idx, pr)

	buf := make([]byte, 16<<10)
	for {
		p.gate.Lock()
		p.gate.Unlock() //nolint:staticcheck // waiting for a paused proxy to resume
		n, err := client.Read(buf)
		if n > 0 {
			if _, werr := broker.Write(buf[:n]); werr != nil {
				break
			}
			_, _ = pw.Write(buf[:n])
		}
		if err != nil {
			break
		}
	}
	pw.Close()
	broker.Close() // the broker must see the drop, not a half-open connection
	client.Close()
}

func (p *tapProxy) sniff(idx int, r io.Reader) {
	for {
		pkt, err := packets.ReadPacket(r, p.version, 0)
		if err != nil {
			_, _ = io.Copy(io.Discard, r)
			return
		}
		if s, ok := pkt.(*packets.SubscribePacket); ok {
			p.mu.Lock()
			p.subscribes[idx] = append(p.subscribes[idx], s.Topics...)
			p.mu.Unlock()
		}
	}
}

// subscribedOn returns the SUBSCRIBE topics recorded on connection idx.
func (p *tapProxy) subscribedOn(idx int) []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	if idx >= len(p.subscribes) {
		return nil
	}
	return slices.Clone(p.subscribes[idx])
}

func (p *tapProxy) dropAll() {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, c := range p.conns {
		c.Close()
	}
}

func (p *tapProxy) pause()  { p.gate.Lock() }
func (p *tapProxy) resume() { p.gate.Unlock() }

// waitFor polls cond until it is true or the timeout expires.
func waitFor(timeout time.Duration, cond func() bool) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return cond()
}

// restrictedDynsec lets anonymous clients do anything except subscribe to
// denied/#, which the broker rejects with 0x87 (v5) or 0x80 (v3.1.1).
const restrictedDynsec = `{
  "defaultACLAccess": {"publishClientSend": true, "publishClientReceive": true, "subscribe": true, "unsubscribe": true},
  "clients": [],
  "groups": [{"groupname": "anonymous", "roles": [{"rolename": "restricted"}]}],
  "roles": [{"rolename": "restricted", "acls": [
    {"acltype": "subscribePattern", "topic": "denied/#", "allow": false}
  ]}],
  "anonymousGroup": "anonymous"
}`

// TestRejectedSubscription_NotResubscribed checks against a broker that
// denies a filter that a rejected filter fails its token with the broker's reason code
// and is not sent again after a reconnect, while accepted filters are.
func TestRejectedSubscription_NotResubscribed(t *testing.T) {
	t.Parallel()
	server, cleanup := startMosquittoWithDynsec(t, restrictedDynsec)
	defer cleanup()

	for _, tc := range []struct {
		name    string
		version uint8
	}{{"v5.0", mq.ProtocolV50}, {"v3.1.1", mq.ProtocolV311}} {
		version := tc.version
		t.Run(tc.name, func(t *testing.T) {
			proxy := newTapProxy(t, server, version)

			var connects atomic.Int32
			client, err := mq.Dial(proxy.server(),
				mq.WithClientID(fmt.Sprintf("acl-resub-v%d", version)),
				mq.WithProtocolVersion(version),
				mq.WithReconnectBackoff(50*time.Millisecond, 100*time.Millisecond, false),
				mq.WithOnConnect(func(*mq.Client) { connects.Add(1) }),
			)
			if err != nil {
				t.Fatalf("Dial: %v", err)
			}
			defer client.Disconnect(context.Background())

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			received := make(chan string, 10)
			handler := func(_ *mq.Client, m mq.Message) { received <- m.Topic }
			if err := client.Subscribe(ctx, "allowed/#", mq.AtLeastOnce, handler).Wait(ctx); err != nil {
				t.Fatalf("allowed subscribe: %v", err)
			}

			tok := client.Subscribe(ctx, "denied/#", mq.AtLeastOnce, handler)
			err = tok.Wait(ctx)
			if !errors.Is(err, mq.ErrSubscriptionFailed) {
				t.Fatalf("denied subscribe error = %v, want ErrSubscriptionFailed", err)
			}
			if version == mq.ProtocolV50 {
				var mErr *mq.MqttError
				if !errors.As(err, &mErr) || mErr.ReasonCode != mq.ReasonCodeNotAuthorized {
					t.Errorf("denied subscribe error = %v, want reason code 0x87 (not authorized)", err)
				}
			} else if tok.ReasonCode() != 0x80 {
				t.Errorf("denied subscribe return code = 0x%02x, want 0x80", uint8(tok.ReasonCode()))
			}

			// Drop the connection and let the client reconnect.
			proxy.dropAll()
			if !waitFor(5*time.Second, func() bool { return connects.Load() >= 2 && client.IsConnected() }) {
				t.Fatal("client did not reconnect")
			}
			if !waitFor(3*time.Second, func() bool { return slices.Contains(proxy.subscribedOn(1), "allowed/#") }) {
				t.Fatalf("allowed/# was not resubscribed after reconnect, got %v", proxy.subscribedOn(1))
			}
			if slices.Contains(proxy.subscribedOn(1), "denied/#") {
				t.Errorf("rejected filter denied/# was resubscribed after reconnect: %v", proxy.subscribedOn(1))
			}

			// The accepted subscription still delivers messages.
			if err := client.Publish(ctx, "allowed/x", []byte("hi"), mq.WithQoS(mq.AtLeastOnce)).Wait(ctx); err != nil {
				t.Fatalf("publish: %v", err)
			}
			select {
			case topic := <-received:
				if topic != "allowed/x" {
					t.Errorf("received topic %q, want allowed/x", topic)
				}
			case <-time.After(3 * time.Second):
				t.Error("no message on allowed/# after reconnect")
			}
		})
	}
}

// TestRejectedSubscription_RemovedFromStore covers a subscription saved in
// an earlier run that the broker now denies: on restart it is
// resubscribed, rejected, and removed from the session store.
func TestRejectedSubscription_RemovedFromStore(t *testing.T) {
	t.Parallel()
	server, cleanup := startMosquittoWithDynsec(t, restrictedDynsec)
	defer cleanup()

	const clientID = "acl-store"
	store, err := mq.NewFileStore(t.TempDir(), clientID)
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}
	// Saved by a previous run, before the ACL changed.
	for _, topic := range []string{"allowed/#", "denied/#"} {
		if err := store.SaveSubscription(topic, &mq.PersistedSubscription{QoS: 1}); err != nil {
			t.Fatalf("SaveSubscription: %v", err)
		}
	}

	client, err := mq.Dial(server,
		mq.WithClientID(clientID),
		mq.WithProtocolVersion(mq.ProtocolV50),
		mq.WithCleanSession(false),
		mq.WithSessionStore(store),
	)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer client.Disconnect(context.Background())

	stored := func() map[string]*mq.PersistedSubscription {
		subs, err := store.LoadSubscriptions()
		if err != nil {
			t.Fatalf("LoadSubscriptions: %v", err)
		}
		return subs
	}
	if !waitFor(3*time.Second, func() bool { _, ok := stored()["denied/#"]; return !ok }) {
		t.Fatal("rejected subscription denied/# is still in the session store")
	}
	if _, ok := stored()["allowed/#"]; !ok {
		t.Error("accepted subscription allowed/# was removed from the session store")
	}
}

// TestDisconnect_StalledLink checks how long Disconnect waits for the
// DISCONNECT packet when the link is stalled behind a burst of publishes,
// and whether the broker then publishes the Last Will.
func TestDisconnect_StalledLink(t *testing.T) {
	t.Parallel()
	server, cleanup := startMosquitto(t, "")
	defer cleanup()

	const stall = 2500 * time.Millisecond

	tests := []struct {
		name        string
		ctx         func() (context.Context, context.CancelFunc)
		minDuration time.Duration // Disconnect must wait at least this long
		maxDuration time.Duration
		wantWill    bool
	}{
		{
			// No deadline: Disconnect gives up after about 1s, before the
			// link recovers, so the DISCONNECT is lost and the Will is sent.
			name:        "no deadline gives up",
			ctx:         func() (context.Context, context.CancelFunc) { return context.Background(), func() {} },
			minDuration: 900 * time.Millisecond,
			maxDuration: stall - 500*time.Millisecond,
			wantWill:    true,
		},
		{
			// A deadline longer than the stall: Disconnect waits for the
			// link to recover and the DISCONNECT suppresses the Will.
			name: "deadline outlasts stall",
			ctx: func() (context.Context, context.CancelFunc) {
				return context.WithTimeout(context.Background(), 10*time.Second)
			},
			minDuration: stall,
			maxDuration: 8 * time.Second,
			wantWill:    false,
		},
	}

	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			willTopic := fmt.Sprintf("will/stalled/%d", i)

			witness, err := mq.Dial(server, mq.WithClientID(fmt.Sprintf("stall-witness-%d", i)))
			if err != nil {
				t.Fatalf("witness Dial: %v", err)
			}
			defer witness.Disconnect(context.Background())
			wills := make(chan struct{}, 1)
			wctx, wcancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer wcancel()
			if err := witness.Subscribe(wctx, willTopic, mq.AtLeastOnce, func(*mq.Client, mq.Message) {
				select {
				case wills <- struct{}{}:
				default:
				}
			}).Wait(wctx); err != nil {
				t.Fatalf("witness subscribe: %v", err)
			}

			proxy := newTapProxy(t, server, mq.ProtocolV50)
			// A small socket send buffer, so the burst cannot hide in the kernel.
			// A custom dialer receives the server URL as given to Dial.
			dialer := mq.DialFunc(func(ctx context.Context, _, addr string) (net.Conn, error) {
				var d net.Dialer
				c, err := d.DialContext(ctx, "tcp", strings.TrimPrefix(addr, "tcp://"))
				if err == nil {
					_ = c.(*net.TCPConn).SetWriteBuffer(16 << 10)
				}
				return c, err
			})
			victim, err := mq.Dial(proxy.server(),
				mq.WithClientID(fmt.Sprintf("stall-victim-%d", i)),
				mq.WithProtocolVersion(mq.ProtocolV50),
				mq.WithDialer(dialer),
				mq.WithAutoReconnect(false),
				mq.WithWill(willTopic, []byte("offline"), 1, false),
			)
			if err != nil {
				t.Fatalf("victim Dial: %v", err)
			}

			// Stall the link, then queue a burst (about 4MB) ahead of the DISCONNECT.
			proxy.pause()
			payload := make([]byte, 8<<10)
			for range 500 {
				victim.Publish(context.Background(), "burst/data", payload)
			}
			time.Sleep(200 * time.Millisecond) // let the write loop block on the full socket

			ctx, cancel := tt.ctx()
			defer cancel()
			done := make(chan time.Duration, 1)
			go func() {
				start := time.Now()
				_ = victim.Disconnect(ctx)
				done <- time.Since(start)
			}()

			time.Sleep(stall)
			proxy.resume()

			var elapsed time.Duration
			select {
			case elapsed = <-done:
			case <-time.After(10 * time.Second):
				t.Fatal("Disconnect did not return")
			}
			if elapsed < tt.minDuration || elapsed > tt.maxDuration {
				t.Errorf("Disconnect took %v, want between %v and %v", elapsed, tt.minDuration, tt.maxDuration)
			}

			select {
			case <-wills:
				if !tt.wantWill {
					t.Error("broker published the Last Will after a graceful disconnect")
				}
			case <-time.After(3 * time.Second):
				if tt.wantWill {
					t.Error("broker did not publish the Last Will after the DISCONNECT was cut off")
				}
			}
		})
	}
}

// TestDisconnect_IdleLinkIsQuick checks the common case: with nothing queued,
// Disconnect(context.Background()) returns at once and no Will is published.
func TestDisconnect_IdleLinkIsQuick(t *testing.T) {
	t.Parallel()
	server, cleanup := startMosquitto(t, "")
	defer cleanup()

	const willTopic = "will/idle"
	witness, err := mq.Dial(server, mq.WithClientID("idle-witness"))
	if err != nil {
		t.Fatalf("witness Dial: %v", err)
	}
	defer witness.Disconnect(context.Background())
	wills := make(chan struct{}, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := witness.Subscribe(ctx, willTopic, mq.AtLeastOnce, func(*mq.Client, mq.Message) {
		select {
		case wills <- struct{}{}:
		default:
		}
	}).Wait(ctx); err != nil {
		t.Fatalf("witness subscribe: %v", err)
	}

	victim, err := mq.Dial(server,
		mq.WithClientID("idle-victim"),
		mq.WithAutoReconnect(false),
		mq.WithWill(willTopic, []byte("offline"), 1, false),
	)
	if err != nil {
		t.Fatalf("victim Dial: %v", err)
	}

	start := time.Now()
	if err := victim.Disconnect(context.Background()); err != nil {
		t.Errorf("Disconnect: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Errorf("Disconnect on an idle link took %v, want well under 1s", elapsed)
	}

	select {
	case <-wills:
		t.Error("broker published the Last Will after a graceful disconnect")
	case <-time.After(time.Second):
	}
}
