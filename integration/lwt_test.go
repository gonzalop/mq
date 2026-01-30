package mq_test

import (
	"context"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/gonzalop/mq"
)

// simpleProxy forwards traffic between a local listener and a target address.
// It allows forcibly closing active connections to simulate network failures.
type simpleProxy struct {
	listener net.Listener
	target   string
	conns    sync.Map // map[net.Conn]struct{}
	wg       sync.WaitGroup
	done     chan struct{}
}

func newProxy(t *testing.T, target string) *simpleProxy {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to start proxy listener: %v", err)
	}
	p := &simpleProxy{
		listener: l,
		target:   target,
		done:     make(chan struct{}),
	}

	p.wg.Add(1)
	go p.acceptLoop()
	return p
}

func (p *simpleProxy) address() string {
	return p.listener.Addr().String()
}

func (p *simpleProxy) acceptLoop() {
	defer p.wg.Done()
	for {
		clientConn, err := p.listener.Accept()
		if err != nil {
			select {
			case <-p.done:
				return
			default:
				// Log error if desired, but acceptable during shutdown
				return
			}
		}

		p.wg.Add(1)
		go p.handleConn(clientConn)
	}
}

func (p *simpleProxy) handleConn(clientConn net.Conn) {
	defer p.wg.Done()
	p.conns.Store(clientConn, struct{}{})
	defer p.conns.Delete(clientConn)
	defer clientConn.Close()

	targetConn, err := net.Dial("tcp", p.target)
	if err != nil {
		return
	}
	defer targetConn.Close()

	p.conns.Store(targetConn, struct{}{})
	defer p.conns.Delete(targetConn)

	// Pipe data
	go io.Copy(targetConn, clientConn)
	io.Copy(clientConn, targetConn)
}

func (p *simpleProxy) closeConnections() {
	p.conns.Range(func(key, value any) bool {
		if conn, ok := key.(net.Conn); ok {
			conn.Close()
		}
		return true
	})
}

func (p *simpleProxy) cleanup() {
	close(p.done)
	p.listener.Close()
	p.closeConnections()
	p.wg.Wait()
}

func TestLastWillWithDelay(t *testing.T) {
	t.Parallel()
	server, cleanup := startMosquitto(t, "")
	defer cleanup()

	// Extract port from valid server address "tcp://127.0.0.1:xyz"
	targetAddr := server[6:] // Strip "tcp://"

	// Start Proxy
	proxy := newProxy(t, targetAddr)
	defer proxy.cleanup()

	proxyAddr := "tcp://" + proxy.address()

	topic := "wills/victim/" + t.Name()

	// Client A: The Victim (Connects via Proxy)
	willDelay := uint32(3) // See 'delay' below
	sessionExpiry := uint32(10)

	_, err := mq.Dial(proxyAddr,
		mq.WithClientID("victim-client-"+t.Name()),
		mq.WithProtocolVersion(mq.ProtocolV50),
		mq.WithCleanSession(false), // Session persistence required for Will Delay? No, but good practice
		mq.WithSessionExpiryInterval(sessionExpiry),
		mq.WithWill(
			topic,
			[]byte("I died ungracefully"),
			1,
			false, // Retain
			&mq.Properties{
				WillDelayInterval: &willDelay,
			},
		),
	)
	if err != nil {
		t.Fatalf("Failed to connect victim: %v", err)
	}
	// Note: We don't defer victim.Disconnect() because we simulate crash.

	// Client B: The Witness (Connects directly to Server)
	witness, err := mq.Dial(server,
		mq.WithClientID("witness-client-"+t.Name()),
		mq.WithProtocolVersion(mq.ProtocolV50),
	)
	if err != nil {
		t.Fatalf("Failed to connect witness: %v", err)
	}
	defer witness.Disconnect(context.Background())

	// Watch for Will
	wills := make(chan mq.Message, 1)
	if err := witness.Subscribe(topic, 1, func(c *mq.Client, msg mq.Message) {
		wills <- msg
	}).Wait(context.Background()); err != nil {
		t.Fatalf("Failed to subscribe witness: %v", err)
	}

	// Wait a bit to ensure victim is connected
	time.Sleep(500 * time.Millisecond)

	// KILL THE CONNECTION
	t.Log("Simulating network failure (closing proxy connections & listener)...")
	// We must close the listener to prevent the client from reconnecting immediately!
	// If it reconnects within WillDelay (2s), the Will is NOT sent.
	proxy.listener.Close()
	proxy.closeConnections()

	// Verification
	start := time.Now()

	select {
	case msg := <-wills:
		elapsed := time.Since(start)
		t.Logf("Received Will message after %v", elapsed)

		if elapsed < 2*time.Second { // Allow slight margin, but shouldn't be instant
			t.Errorf("Will message received too early (%v), expected > 2s delay", elapsed)
		}

		if string(msg.Payload) != "I died ungracefully" {
			t.Errorf("Wrong payload: %s", string(msg.Payload))
		}

	case <-time.After(10 * time.Second):
		t.Fatal("Timeout waiting for Will message")
	}
}
