package packets

import (
	"bytes"
	"testing"
)

// BenchmarkEncodeProperties benchmarks property encoding
func BenchmarkEncodeProperties_Empty(b *testing.B) {
	props := &Properties{}

	for b.Loop() {
		_ = encodeProperties(props)
	}
}

func BenchmarkEncodeProperties_Small(b *testing.B) {
	props := &Properties{
		PayloadFormatIndicator: 1,
		ContentType:            "application/json",
		Presence:               PresPayloadFormatIndicator | PresContentType,
	}

	for b.Loop() {
		_ = encodeProperties(props)
	}
}

func BenchmarkEncodeProperties_Large(b *testing.B) {
	props := &Properties{
		PayloadFormatIndicator: 1,
		MessageExpiryInterval:  3600,
		ContentType:            "application/json",
		ResponseTopic:          "response/topic",
		CorrelationData:        []byte("correlation-id-12345"),
		SessionExpiryInterval:  7200,
		UserProperties: []UserProperty{
			{Key: "key1", Value: "value1"},
			{Key: "key2", Value: "value2"},
		},
		Presence: PresPayloadFormatIndicator | PresMessageExpiryInterval | PresContentType | PresResponseTopic | PresSessionExpiryInterval,
	}

	for b.Loop() {
		_ = encodeProperties(props)
	}
}

// BenchmarkDecodeProperties benchmarks property decoding
func BenchmarkDecodeProperties_Empty(b *testing.B) {
	data := []byte{0x00}

	for b.Loop() {
		_, _, _ = decodeProperties(data)
	}
}

func BenchmarkDecodeProperties_Small(b *testing.B) {
	props := &Properties{
		PayloadFormatIndicator: 1,
		ContentType:            "application/json",
		Presence:               PresPayloadFormatIndicator | PresContentType,
	}
	data := encodeProperties(props)

	for b.Loop() {
		_, _, _ = decodeProperties(data)
	}
}

func BenchmarkDecodeProperties_Large(b *testing.B) {
	props := &Properties{
		PayloadFormatIndicator: 1,
		MessageExpiryInterval:  3600,
		ContentType:            "application/json",
		ResponseTopic:          "response/topic",
		CorrelationData:        []byte("correlation-id-12345"),
		SessionExpiryInterval:  7200,
		UserProperties: []UserProperty{
			{Key: "key1", Value: "value1"},
			{Key: "key2", Value: "value2"},
		},
		Presence: PresPayloadFormatIndicator | PresMessageExpiryInterval | PresContentType | PresResponseTopic | PresSessionExpiryInterval,
	}
	data := encodeProperties(props)

	for b.Loop() {
		_, _, _ = decodeProperties(data)
	}
}

// BenchmarkEncodeVarInt benchmarks variable integer encoding
func BenchmarkEncodeVarInt_Small(b *testing.B) {

	for b.Loop() {
		_ = encodeVarInt(127)
	}
}

func BenchmarkEncodeVarInt_Medium(b *testing.B) {

	for b.Loop() {
		_ = encodeVarInt(16383)
	}
}

func BenchmarkEncodeVarInt_Large(b *testing.B) {

	for b.Loop() {
		_ = encodeVarInt(268435455)
	}
}

// BenchmarkDecodeVarIntBuf benchmarks variable integer decoding from buffer
func BenchmarkDecodeVarIntBuf_Small(b *testing.B) {
	data := []byte{0x7f}

	for b.Loop() {
		_, _, _ = decodeVarIntBuf(data)
	}
}

func BenchmarkDecodeVarIntBuf_Medium(b *testing.B) {
	data := []byte{0xff, 0x7f}

	for b.Loop() {
		_, _, _ = decodeVarIntBuf(data)
	}
}

func BenchmarkDecodeVarIntBuf_Large(b *testing.B) {
	data := []byte{0xff, 0xff, 0xff, 0x7f}

	for b.Loop() {
		_, _, _ = decodeVarIntBuf(data)
	}
}

// BenchmarkPublishPacket_V3vsV5 compares v3.1.1 vs v5.0 encoding overhead
func BenchmarkPublishPacket_V3(b *testing.B) {
	pkt := &PublishPacket{
		Topic:    "test/topic",
		QoS:      1,
		PacketID: 1,
		Payload:  []byte("test payload data"),
		Version:  4,
	}

	for b.Loop() {
		_ = encodeToBytes(pkt)
	}
}

func BenchmarkPublishPacket_V5_NoProperties(b *testing.B) {
	pkt := &PublishPacket{
		Topic:    "test/topic",
		QoS:      1,
		PacketID: 1,
		Payload:  []byte("test payload data"),
		Version:  5,
	}

	for b.Loop() {
		_ = encodeToBytes(pkt)
	}
}

func BenchmarkPublishPacket_V5_WithProperties(b *testing.B) {
	pkt := &PublishPacket{
		Topic:    "test/topic",
		QoS:      1,
		PacketID: 1,
		Payload:  []byte("test payload data"),
		Properties: &Properties{
			ContentType: "application/json",
			Presence:    PresContentType,
		},
		Version: 5,
	}

	for b.Loop() {
		_ = encodeToBytes(pkt)
	}
}

// BenchmarkPublishPacket_WriteTo measures the performance of zero-copy encoding.
func BenchmarkPublishPacket_WriteTo(b *testing.B) {
	payload := make([]byte, 1024)
	pkt := &PublishPacket{
		Topic:    "sensors/temperature",
		Payload:  payload,
		QoS:      1,
		PacketID: 10,
	}

	// Reusable buffer to write into
	buf := bytes.NewBuffer(make([]byte, 0, 4096))

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		buf.Reset()
		if _, err := pkt.WriteTo(buf); err != nil {
			b.Fatal(err)
		}
	}
}
