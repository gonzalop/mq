package mq

import (
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/gonzalop/mq/internal/packets"
)

// MQTT 4.6: messages on one topic and QoS must reach the application in the
// order they were received, and QoS 1 PUBACKs must be sent in the order the
// PUBLISH packets arrived [MQTT-4.6.0-2].
func TestDispatch_PreservesOrderPerTopic(t *testing.T) {
	for _, qos := range []uint8{0, 1} {
		t.Run("QoS"+strconv.Itoa(int(qos)), func(t *testing.T) {
			const n = 500
			opts := defaultOptions("tcp://localhost:1883")
			opts.OutgoingQueueSize = n + 10
			opts.IncomingQueueSize = n + 10
			c := newTestClient(opts)

			var mu sync.Mutex
			var got []int
			var done sync.WaitGroup
			done.Add(n)
			h := func(_ *Client, m Message) {
				v, _ := strconv.Atoi(string(m.Payload))
				mu.Lock()
				got = append(got, v)
				mu.Unlock()
				done.Done()
			}
			c.sessionLock.Lock()
			c.addSubscriptionLocked("sensors/temp", subscriptionEntry{handler: h})
			c.sessionLock.Unlock()

			c.wg.Add(1)
			go c.logicLoop()
			defer func() {
				close(c.stop)
				c.wg.Wait()
			}()

			for i := range n {
				c.incoming <- &packets.PublishPacket{
					Topic:    "sensors/temp",
					QoS:      qos,
					PacketID: uint16(i + 1),
					Payload:  []byte(strconv.Itoa(i)),
				}
			}

			waitCh := make(chan struct{})
			go func() { done.Wait(); close(waitCh) }()
			select {
			case <-waitCh:
			case <-time.After(10 * time.Second):
				t.Fatal("handlers did not finish")
			}

			mu.Lock()
			for i, v := range got {
				if v != i {
					mu.Unlock()
					t.Fatalf("handler call %d got message %d: delivery out of order", i, v)
				}
			}
			mu.Unlock()

			if qos == 0 {
				return
			}
			for i := range n {
				select {
				case p := <-c.outgoing:
					if w, isAck := p.(*inboundAck); isAck {
						p = w.Packet
					}
					ack, ok := p.(*packets.PubackPacket)
					if !ok {
						t.Fatalf("expected PUBACK, got %T", p)
					}
					if ack.PacketID != uint16(i+1) {
						t.Fatalf("ack %d has packet ID %d: acks out of order", i, ack.PacketID)
					}
				case <-time.After(5 * time.Second):
					t.Fatalf("missing ack %d", i)
				}
			}
		})
	}
}
