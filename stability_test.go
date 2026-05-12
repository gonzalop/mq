package mq

import (
	"context"
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

	c.sessionLock.Lock()
	c.addSubscriptionLocked("topic/1", subscriptionEntry{handler: h1})
	c.sessionLock.Unlock()

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
	c.addSubscriptionLocked("topic/2", subscriptionEntry{handler: h2})

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
