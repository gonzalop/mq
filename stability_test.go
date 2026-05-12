package mq

import (
	"context"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/gonzalop/mq/internal/packets"
)

// TestSemaphoreStallPrevention verifies that the logicLoop does not stall
// when the message handler semaphore is full.
func TestSemaphoreStallPrevention(t *testing.T) {
	// 1. Setup Client with 1 concurrent handler limit
	opts := defaultOptions("tcp://localhost:1883")
	WithMaxHandlerConcurrency(1)(opts)
	c := newTestClient(opts)
	c.handlerSem = make(chan struct{}, 1)

	// 2. Start logicLoop
	c.wg.Add(1)
	go c.logicLoop()
	defer func() {
		close(c.stop)
		c.wg.Wait()
	}()

	// 3. Block the first handler
	handler1Started := make(chan struct{})
	blockHandler1 := make(chan struct{})

	h1 := func(_ *Client, _ Message) {
		close(handler1Started)
		<-blockHandler1
	}

	c.subscriptions["topic/1"] = subscriptionEntry{handler: h1}

	// Send first message
	c.incoming <- &packets.PublishPacket{Topic: "topic/1", QoS: 0, Payload: []byte("1")}

	// Wait for handler 1 to start
	select {
	case <-handler1Started:
		// h1 is now holding the only semaphore slot
	case <-time.After(200 * time.Millisecond):
		t.Fatal("Handler 1 never started")
	}

	// 4. Send second message (should block its handler, but NOT logicLoop)
	handler2Started := make(chan struct{})
	h2 := func(_ *Client, _ Message) {
		close(handler2Started)
	}
	c.subscriptions["topic/2"] = subscriptionEntry{handler: h2}

	c.incoming <- &packets.PublishPacket{Topic: "topic/2", QoS: 0, Payload: []byte("2")}

	// 5. Verify logicLoop is still alive by sending an ACK and waiting for completion
	token := newToken()
	c.sessionLock.Lock()
	c.pending[100] = &pendingOp{token: token, qos: 1}
	c.sessionLock.Unlock()

	c.incoming <- &packets.PubackPacket{PacketID: 100}

	select {
	case <-token.Done():
		// Logic loop processed the PUBACK! Success.
	case <-time.After(200 * time.Millisecond):
		t.Fatal("logicLoop stalled while waiting for handler semaphore")
	}

	// 6. Unblock everything
	close(blockHandler1)

	select {
	case <-handler2Started:
		// Handler 2 eventually ran
	case <-time.After(200 * time.Millisecond):
		t.Fatal("Handler 2 never ran after semaphore freed")
	}
}

// TestHandlerPanicRecovery verifies that a panic in a message handler
// doesn't crash the client and subsequent messages are still processed.
func TestHandlerPanicRecovery(t *testing.T) {
	opts := defaultOptions("tcp://localhost:1883")
	c := newTestClient(opts)

	c.wg.Add(1)
	go c.logicLoop()
	defer func() {
		close(c.stop)
		c.wg.Wait()
	}()

	// 1. Register a panicking handler
	panicked := false
	var mu sync.Mutex
	hPanic := func(_ *Client, _ Message) {
		mu.Lock()
		panicked = true
		mu.Unlock()
		panic("boom")
	}
	c.subscriptions["panic"] = subscriptionEntry{handler: hPanic}

	// 2. Register a normal handler
	receivedNormal := make(chan struct{})
	hNormal := func(_ *Client, _ Message) {
		close(receivedNormal)
	}
	c.subscriptions["normal"] = subscriptionEntry{handler: hNormal}

	// 3. Send panicking message
	c.incoming <- &packets.PublishPacket{Topic: "panic", QoS: 0}

	// Wait a bit for panic to happen
	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	if !panicked {
		t.Fatal("Handler should have panicked")
	}
	mu.Unlock()

	// 4. Send normal message (should still work)
	c.incoming <- &packets.PublishPacket{Topic: "normal", QoS: 0}

	select {
	case <-receivedNormal:
		// Success!
	case <-time.After(200 * time.Millisecond):
		t.Fatal("Normal message not processed after previous handler panic")
	}
}

// TestCallbackPanicRecovery verifies that lifecycle callbacks are protected from panics.
func TestCallbackPanicRecovery(t *testing.T) {
	onConnectPanicked := make(chan struct{})
	opts := defaultOptions("tcp://localhost:1883")
	opts.OnConnect = func(_ *Client) {
		close(onConnectPanicked)
		panic("onconnect-boom")
	}
	c := newTestClient(opts)
	conn1, conn2 := net.Pipe()
	defer conn1.Close()
	defer conn2.Close()
	c.conn = conn1
	// Trigger OnConnect recovery via finalizeConnection (internal)
	// We don't start the loops for this test, just call the hook wrapper
	c.finalizeConnection(&packets.ConnackPacket{})

	select {
	case <-onConnectPanicked:
		// Hook was called and panicked (hopefully recovered)
	case <-time.After(100 * time.Millisecond):
		t.Fatal("OnConnect was never called")
	}

	// Verify we can still do things with the client
	if !c.IsConnected() {
		t.Error("Client should still be marked as connected")
	}
}

// TestDisconnectWaitGroup verifies that Disconnect waits correctly for goroutines to exit.
func TestDisconnectWaitGroup(t *testing.T) {
	opts := defaultOptions("tcp://localhost:1883")
	c := newTestClient(opts)

	// Simulate a running loop
	c.wg.Add(1)
	loopStarted := make(chan struct{})
	loopExited := make(chan struct{})
	go func() {
		close(loopStarted)
		<-c.stop
		time.Sleep(50 * time.Millisecond) // Simulate some work during shutdown
		c.wg.Done()
		close(loopExited)
	}()

	<-loopStarted
	c.connected.Store(true)

	// Call disconnectWithReason(block=true)
	start := time.Now()
	err := c.disconnectWithReason(context.Background(), 0, nil, true)
	elapsed := time.Since(start)

	if err != nil {
		t.Errorf("Disconnect failed: %v", err)
	}

	select {
	case <-loopExited:
		// Success
	default:
		t.Error("Disconnect returned before loop actually exited")
	}

	if elapsed < 50*time.Millisecond {
		t.Errorf("Disconnect returned too early (%v), didn't wait for work", elapsed)
	}
}
