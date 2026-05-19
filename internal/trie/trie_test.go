package trie

import (
	"reflect"
	"testing"
)

func TestTopicTrie_Match(t *testing.T) {
	tests := []struct {
		name    string
		filters map[string]string // filter -> handler name
		topic   string
		want    []string // expected handler names
	}{
		{
			name: "Exact match",
			filters: map[string]string{
				"test/topic": "h1",
			},
			topic: "test/topic",
			want:  []string{"h1"},
		},
		{
			name: "No match",
			filters: map[string]string{
				"test/topic": "h1",
			},
			topic: "test/other",
			want:  nil,
		},
		{
			name: "Single level wildcard",
			filters: map[string]string{
				"test/+": "h1",
			},
			topic: "test/topic",
			want:  []string{"h1"},
		},
		{
			name: "Single level wildcard mismatch",
			filters: map[string]string{
				"test/+": "h1",
			},
			topic: "test/topic/sub",
			want:  nil,
		},
		{
			name: "Multi-level wildcard",
			filters: map[string]string{
				"test/#": "h1",
			},
			topic: "test/topic/sub",
			want:  []string{"h1"},
		},
		{
			name: "Multi-level wildcard at root",
			filters: map[string]string{
				"#": "h1",
			},
			topic: "any/topic",
			want:  []string{"h1"},
		},
		{
			name: "Multiple matches",
			filters: map[string]string{
				"sensors/+/temperature": "h1",
				"sensors/#":             "h2",
				"sensors/living-room/temperature": "h3",
			},
			topic: "sensors/living-room/temperature",
			want:  []string{"h2", "h1", "h3"}, // # matches first in matchRecursive implementation
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tr := New[string]()
			for filter, handler := range tt.filters {
				tr.Insert(filter, handler)
			}
			got := tr.Match(tt.topic)
			if !reflect.DeepEqual(got, tt.want) && !(len(got) == 0 && len(tt.want) == 0) {
				t.Errorf("Match() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestTopicTrie_Remove(t *testing.T) {
	tr := New[string]()
	tr.Insert("test/topic", "h1")
	tr.Insert("test/+", "h2")

	// Verify both match
	got := tr.Match("test/topic")
	if len(got) != 2 {
		t.Errorf("Expected 2 handlers, got %d", len(got))
	}

	// Remove exact
	tr.Remove("test/topic")
	got = tr.Match("test/topic")
	if len(got) != 1 || got[0] != "h2" {
		t.Errorf("Expected only h2, got %v", got)
	}

	// Remove wildcard
	tr.Remove("test/+")
	got = tr.Match("test/topic")
	if len(got) != 0 {
		t.Errorf("Expected 0 handlers, got %d", len(got))
	}
}
