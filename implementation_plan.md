# Implementation Plan - Fix Critical Bugs Identified in Review

This plan details the code changes and unit tests to fix the four issues discovered during the review of the commits since `origin/HEAD`.

## User Review Required

> [!IMPORTANT]
> The fixes proposed here correct core reliability, correctness, and safety behavior in the MQ client:
> 1. Restoring chronological order of pending packet retransmissions upon session load.
> 2. Preventing handler memory leaks/duplicate calls on subscription updates.
> 3. Eliminating the logic-loop blocking stall risk in `AsyncStore`.
> 4. Preventing disk leaks by clearing persistence on stateless (`CleanSession=true`) connections.

Please review the proposed changes and verification plan below.

## Proposed Changes

---

### Component: Persistence Session Recovery

#### [MODIFY] [client_persistence.go](file:///home/gonzalo/go/src/github.com/gonzalop/mq/client_persistence.go)

Modify `loadSessionState()` to rebuild and sort `c.pendingOrder` numerically based on the restored packet IDs.
- Collect packet IDs from `pending` map.
- Sort them in ascending numerical order.
- Populate `c.pending` and append to `c.pendingOrder` in that sorted order.

---

### Component: Topic Trie Subscriptions

#### [MODIFY] [client_subscriptions.go](file:///home/gonzalo/go/src/github.com/gonzalop/mq/client_subscriptions.go)

Modify `addSubscriptionLocked()` to check if the topic filter already exists in the local map, and if so, remove it from the trie first:
```go
func (c *Client) addSubscriptionLocked(topic string, entry subscriptionEntry) {
	if _, exists := c.subscriptions[topic]; exists {
		c.trie.Remove(topic)
	}
	c.subscriptions[topic] = entry
	if entry.handler != nil {
		c.trie.Insert(topic, entry.handler)
	}
}
```

---

### Component: AsyncStore Queue Blocking Prevention

#### [MODIFY] [session_store.go](file:///home/gonzalo/go/src/github.com/gonzalop/mq/session_store.go)

Refactor `AsyncStore` to use a thread-safe, mutex-backed unbounded queue (`sync.Cond` and slice) instead of a buffered channel.
- Update `AsyncStore` fields to include a mutex, condition variable, `queue []func()`, and `stop` flag.
- Update `NewAsyncStore` to initialize the mutex, condition variable, and slice.
- Implement the non-blocking `enqueue` function and the background `run()` loop.
- Ensure that `Close` drains the queue safely and shuts down the background goroutine.

---

### Component: Stateless Connect Cleanup

#### [MODIFY] [client.go](file:///home/gonzalo/go/src/github.com/gonzalop/mq/client.go)

In `Dial()`, if `c.opts.CleanSession` is true, clear the session store if configured:
```go
	if !c.opts.CleanSession {
		if err := c.loadSessionState(); err != nil {
			c.opts.Logger.Warn("failed to load session state", "error", err)
		}
	} else if c.opts.SessionStore != nil {
		if err := c.opts.SessionStore.Clear(); err != nil {
			c.opts.Logger.Warn("failed to clear session store", "error", err)
		}
	}
```

---

## Verification Plan

### Automated Tests
- Add a new unit test `TestPersistencePendingOrder` in `client_publish_test.go` to verify that when loading session state, `pendingOrder` is correctly populated and sorted.
- Add a new unit test `TestSubscriptionOverwriting` in `client_subscriptions_test.go` to verify that overwriting a subscription does not execute the old handler.
- Add a unit test `TestAsyncStoreNonBlocking` in `session_store_test.go` or `persistence_perf_test.go` to verify that `AsyncStore` does not block writes even when disk writes are slow.
- Run `go test ./...` and `make fuzz` to ensure all tests pass.
