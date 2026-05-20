package trie

import (
	"fmt"
	"math/rand"
	"testing"
)

// Generate a set of realistic topic filters and match topics
func generateFiltersAndTopics(numFilters int) (filters []string, matchTopics []string) {
	// Seed with a deterministic source
	r := rand.New(rand.NewSource(42))

	levels := []string{"sensors", "devices", "home", "office", "factory", "kitchen", "living-room", "basement", "temperature", "humidity", "status", "power", "light"}

	for i := 0; i < numFilters; i++ {
		// Construct a random 3-level topic filter
		// e.g. sensors/living-room/temperature
		// e.g. sensors/+/temperature
		// e.g. sensors/kitchen/#
		lvl1 := levels[r.Intn(len(levels))]
		lvl2 := "+"
		if r.Float32() > 0.3 {
			lvl2 = levels[r.Intn(len(levels))]
		}
		lvl3 := "#"
		if r.Float32() > 0.3 {
			lvl3 = levels[r.Intn(len(levels))]
		}

		filter := fmt.Sprintf("%s/%s/%s", lvl1, lvl2, lvl3)
		filters = append(filters, filter)

		// Create a corresponding concrete topic to match against
		concreteLvl2 := levels[r.Intn(len(levels))]
		concreteLvl3 := levels[r.Intn(len(levels))]
		matchTopic := fmt.Sprintf("%s/%s/%s", lvl1, concreteLvl2, concreteLvl3)
		matchTopics = append(matchTopics, matchTopic)
	}

	return filters, matchTopics
}

func BenchmarkTrieInsert(b *testing.B) {
	sizes := []int{10, 100, 1000}
	for _, size := range sizes {
		b.Run(fmt.Sprintf("Size-%d", size), func(b *testing.B) {
			filters, _ := generateFiltersAndTopics(size)
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				tr := New[string]()
				for _, filter := range filters {
					tr.Insert(filter, "handler")
				}
			}
		})
	}
}

func BenchmarkTrieMatch(b *testing.B) {
	sizes := []int{10, 100, 1000}
	for _, size := range sizes {
		b.Run(fmt.Sprintf("Size-%d", size), func(b *testing.B) {
			filters, topics := generateFiltersAndTopics(size)
			tr := New[string]()
			for _, filter := range filters {
				tr.Insert(filter, "handler")
			}

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				// Cycle through the topics to match
				topic := topics[i%len(topics)]
				_ = tr.Match(topic)
			}
		})
	}
}

func BenchmarkTrieRemove(b *testing.B) {
	sizes := []int{10, 100, 1000}
	for _, size := range sizes {
		b.Run(fmt.Sprintf("Size-%d", size), func(b *testing.B) {
			filters, _ := generateFiltersAndTopics(size)
			tr := New[string]()
			for _, filter := range filters {
				tr.Insert(filter, "handler")
			}

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				filterToRemove := filters[i%len(filters)]
				tr.Remove(filterToRemove)
			}
		})
	}
}
