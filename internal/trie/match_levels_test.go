package trie

import "testing"

func TestMatch_EmptyLevels(t *testing.T) {
	tr := New[int]()
	tr.Insert("a/+", 1)
	tr.Insert("+/+", 2)
	tr.Insert("/a", 3)
	tr.Insert("#", 4)

	cases := map[string]int{"a/": 3, "/a": 3, "a": 1, "": 1, "a/b/c": 1}
	for topic, want := range cases {
		if got := len(tr.Match(topic)); got != want {
			t.Errorf("Match(%q) = %d handlers, want %d", topic, got, want)
		}
	}
}
