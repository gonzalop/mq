package mq_test

import (
	"bytes"

	"github.com/gonzalop/mq/internal/packets"
)

func encodeToBytes(pkt packets.Packet) []byte {
	var buf bytes.Buffer
	if _, err := pkt.WriteTo(&buf); err != nil {
		panic(err)
	}
	return buf.Bytes()
}
