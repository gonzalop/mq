package mq

import (
	"testing"
)

// BenchmarkMatchTopic measures the performance of topic matching.
// This is a critical hot path for message dispatching.

func BenchmarkMatchTopic_Exact(b *testing.B) {
	filter := "sensors/building-a/floor-3/room-42/temperature"
	topic := "sensors/building-a/floor-3/room-42/temperature"

	for b.Loop() {
		matchTopic(filter, topic)
	}
}

func BenchmarkMatchTopic_WildcardPlus(b *testing.B) {
	filter := "sensors/+/floor-3/+/temperature"
	topic := "sensors/building-a/floor-3/room-42/temperature"

	for b.Loop() {
		matchTopic(filter, topic)
	}
}

func BenchmarkMatchTopic_WildcardHash(b *testing.B) {
	filter := "sensors/building-a/#"
	topic := "sensors/building-a/floor-3/room-42/temperature"

	for b.Loop() {
		matchTopic(filter, topic)
	}
}

func BenchmarkMatchTopic_WildcardHash_Root(b *testing.B) {
	filter := "#"
	topic := "sensors/building-a/floor-3/room-42/temperature"

	for b.Loop() {
		matchTopic(filter, topic)
	}
}

func BenchmarkMatchTopic_NoMatch_Early(b *testing.B) {
	filter := "sensors/building-b/floor-3/room-42/temperature"
	topic := "sensors/building-a/floor-3/room-42/temperature"

	for b.Loop() {
		matchTopic(filter, topic)
	}
}

func BenchmarkMatchTopic_NoMatch_Late(b *testing.B) {
	filter := "sensors/building-a/floor-3/room-42/humidity"
	topic := "sensors/building-a/floor-3/room-42/temperature"

	for b.Loop() {
		matchTopic(filter, topic)
	}
}
