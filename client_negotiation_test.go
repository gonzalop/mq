package mq

import (
	"context"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/gonzalop/mq/internal/packets"
)

func TestProtocolNegotiation(t *testing.T) {
	// Start a mock server that refuses v5.0 and accepts v3.1.1
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	addr := ln.Addr().String()

	go func() {
		// First attempt: client sends v5.0 CONNECT
		conn1, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn1.Close()

		// Read CONNECT
		pkt, err := packets.ReadPacket(conn1, ProtocolV50, 0)
		if err != nil {
			return
		}
		cpkt, ok := pkt.(*packets.ConnectPacket)
		if !ok || cpkt.ProtocolLevel != ProtocolV50 {
			return
		}

		// Refuse with Unacceptable Protocol Version (v3.1.1 style return code)
		connack1 := &packets.ConnackPacket{
			ReturnCode: uint8(packets.ConnRefusedUnacceptableProtocol),
		}
		_, _ = connack1.WriteTo(conn1)
		conn1.Close()

		// Second attempt: client should send v3.1.1 CONNECT
		conn2, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn2.Close()

		pkt, err = packets.ReadPacket(conn2, ProtocolV311, 0)
		if err != nil {
			return
		}
		cpkt, ok = pkt.(*packets.ConnectPacket)
		if !ok || cpkt.ProtocolLevel != ProtocolV311 {
			fmt.Printf("Expected v3.1.1, got %d\n", cpkt.ProtocolLevel)
			return
		}

		// Accept connection
		connack2 := &packets.ConnackPacket{
			ReturnCode: uint8(packets.ConnAccepted),
		}
		_, _ = connack2.WriteTo(conn2)
	}()

	client, err := Dial("tcp://"+addr,
		WithClientID("negotiator"),
		WithConnectTimeout(2*time.Second),
		WithAutoReconnect(false),
	)

	if err != nil {
		t.Fatalf("Dial failed: %v", err)
	}
	defer func() { _ = client.Disconnect(context.Background()) }()

	if client.opts.ProtocolVersion != ProtocolV311 {
		t.Errorf("expected protocol version %d, got %d", ProtocolV311, client.opts.ProtocolVersion)
	}
}
