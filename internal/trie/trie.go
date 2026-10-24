package trie

import (
	"strings"
)

// topicNode represents a node in the topic trie.
type topicNode[T any] struct {
	children map[string]*topicNode[T]
	handlers []T
}

// TopicTrie is a trie-based structure for efficient MQTT topic matching.
// It supports '+' and '#' wildcards in filters.
type TopicTrie[T any] struct {
	root *topicNode[T]
}

// New creates a new TopicTrie.
func New[T any]() *TopicTrie[T] {
	return &TopicTrie[T]{
		root: &topicNode[T]{
			children: make(map[string]*topicNode[T]),
		},
	}
}

// Insert adds a topic filter and its associated handler to the trie.
func (t *TopicTrie[T]) Insert(filter string, handler T) {
	parts := strings.Split(filter, "/")
	node := t.root

	for _, part := range parts {
		child, ok := node.children[part]
		if !ok {
			child = &topicNode[T]{
				children: make(map[string]*topicNode[T]),
			}
			node.children[part] = child
		}
		node = child
	}

	node.handlers = append(node.handlers, handler)
}

// Remove removes a topic filter from the trie.
// It currently removes ALL handlers for that filter.
func (t *TopicTrie[T]) Remove(filter string) {
	parts := strings.Split(filter, "/")
	t.removeRecursive(t.root, parts, 0)
}

func (t *TopicTrie[T]) removeRecursive(node *topicNode[T], parts []string, index int) bool {
	if index == len(parts) {
		node.handlers = nil
		return len(node.children) == 0
	}

	part := parts[index]
	child, ok := node.children[part]
	if !ok {
		return false
	}

	canDeleteChild := t.removeRecursive(child, parts, index+1)
	if canDeleteChild {
		delete(node.children, part)
		return len(node.children) == 0 && len(node.handlers) == 0
	}

	return false
}

// Match finds all handlers that match the given topic name.
func (t *TopicTrie[T]) Match(topic string) []T {
	var handlers []T
	t.matchRecursive(t.root, topic, 0, &handlers)
	return handlers
}

// matchRecursive matches the topic levels starting at byte offset start.
// start == len(topic)+1 means every level has been consumed. Walking offsets
// instead of splitting avoids allocating a slice per message.
func (t *TopicTrie[T]) matchRecursive(node *topicNode[T], topic string, start int, handlers *[]T) {
	// [MQTT-4.7.2-1]: Wildcard filters starting with '#' or '+' must not match
	// topic names beginning with '$'.
	isDollarTopicAtRoot := start == 0 && len(topic) > 0 && topic[0] == '$'

	// 1. Check for multi-level wildcard matches at this level
	if !isDollarTopicAtRoot {
		if wildcardNode, ok := node.children["#"]; ok {
			*handlers = append(*handlers, wildcardNode.handlers...)
		}
	}

	// 2. Check if we've reached the end of the topic
	if start > len(topic) {
		*handlers = append(*handlers, node.handlers...)
		return
	}

	part, next := topic[start:], len(topic)+1
	if i := strings.IndexByte(part, '/'); i >= 0 {
		part, next = part[:i], start+i+1
	}

	// 3. Check for single-level wildcard match
	if !isDollarTopicAtRoot {
		if singleWildcardNode, ok := node.children["+"]; ok {
			t.matchRecursive(singleWildcardNode, topic, next, handlers)
		}
	}

	// 4. Check for exact match
	if exactNode, ok := node.children[part]; ok {
		t.matchRecursive(exactNode, topic, next, handlers)
	}
}
