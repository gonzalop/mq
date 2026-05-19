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
	parts := strings.Split(topic, "/")
	var handlers []T
	t.matchRecursive(t.root, parts, 0, &handlers)
	return handlers
}

func (t *TopicTrie[T]) matchRecursive(node *topicNode[T], parts []string, index int, handlers *[]T) {
	// 1. Check for multi-level wildcard matches at this level
	if wildcardNode, ok := node.children["#"]; ok {
		*handlers = append(*handlers, wildcardNode.handlers...)
	}

	// 2. Check if we've reached the end of the topic
	if index == len(parts) {
		*handlers = append(*handlers, node.handlers...)
		return
	}

	part := parts[index]

	// 3. Check for single-level wildcard match
	if singleWildcardNode, ok := node.children["+"]; ok {
		t.matchRecursive(singleWildcardNode, parts, index+1, handlers)
	}

	// 4. Check for exact match
	if exactNode, ok := node.children[part]; ok {
		t.matchRecursive(exactNode, parts, index+1, handlers)
	}
}
