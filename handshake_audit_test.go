package mq

import (
	"bytes"
	"context"
	"io"
	"net"
	"testing"
	"time"

	"github.com/gonzalop/mq/internal/packets"
)

// TestPerformHandshake_AuthWithoutProperties checks that an AUTH packet with
// no properties (remaining length 0) does not panic the handshake. c.conn is
// left nil, as when Disconnect races a reconnect: the handshake must use the
// connection it was given.
func TestPerformHandshake_AuthWithoutProperties(t *testing.T) {
	opts := defaultOptions("tcp://x:1883")
	opts.ProtocolVersion = ProtocolV50
	opts.Logger = testLogger()
	auth := &tokenAuthenticator{token: "t"}
	opts.Authenticator = auth
	c := newTestClient(opts)

	cli, srv := net.Pipe()
	defer cli.Close()
	defer srv.Close()

	go func() {
		// AUTH, remaining length 0: reason code Success, no properties.
		if _, err := srv.Write([]byte{0xF0, 0x00}); err != nil {
			return
		}
		if _, err := packets.ReadPacket(srv, ProtocolV50, 0); err != nil {
			return
		}
		connack := &packets.ConnackPacket{ReturnCode: packets.ConnAccepted, Properties: &packets.Properties{}}
		_, _ = connack.WriteTo(srv)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	connack, err := c.performHandshake(ctx, cli, cli, cli)
	if err != nil {
		t.Fatalf("performHandshake: %v", err)
	}
	if connack == nil {
		t.Fatal("expected CONNACK")
	}
	if auth.getChallengeCount() != 1 {
		t.Errorf("HandleChallenge called %d times, want 1", auth.getChallengeCount())
	}
}

// TestWriteLoop_NothingAfterDisconnect checks that packets queued behind a
// DISCONNECT are not written [MQTT-3.14.4-1].
func TestWriteLoop_NothingAfterDisconnect(t *testing.T) {
	opts := defaultOptions("tcp://x:1883")
	opts.Logger = testLogger()
	c := newTestClient(opts)

	c.outgoing <- &packets.DisconnectPacket{}
	c.outgoing <- &packets.PingreqPacket{}

	conn, peer := net.Pipe()
	defer peer.Close()
	got := make(chan []byte, 1)
	go func() {
		data, _ := io.ReadAll(peer)
		got <- data
	}()

	c.wg.Add(1)
	c.runWriteLoop(conn, make(chan struct{}), 0)
	conn.Close()

	var want bytes.Buffer
	_, _ = (&packets.DisconnectPacket{}).WriteTo(&want)
	if data := <-got; !bytes.Equal(data, want.Bytes()) {
		t.Errorf("wrote %x, want only DISCONNECT %x", data, want.Bytes())
	}
}
