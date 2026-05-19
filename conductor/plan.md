# Project Modularization Plan (Lean Root Directory)

**Objective:**
Reorganize the top-level directory to contain primarily the public API (`Client`, `Options`, `Token`, `Message`, `SessionStore`). Move independent internal data structures, persistence implementations, and their corresponding tests into subpackages to adhere to Go best practices and keep the root lean. 

Based on the chosen strategy ("Moderate: Extract Utilities"), we will extract independent domains without attempting to completely hide the core `Client` and `logicLoop`.

## Strategy & Phases

### Phase 1: Consolidate Black-Box Tests
Tests in the root that only use the public API (currently declared as `package mq` or `package mq_test`) will be moved out of the root.
- **Action:** Move these tests into the `integration/` directory (which currently serves as the black-box test suite). Ensure they use `package mq_test`.
- **Target Files:** `benchmark_test.go`, `client_test.go`, `client_subscribe_test.go`, `keepalive_test.go`, `compliance_test.go`, `server_limits_test.go`, etc.

### Phase 2: Extract Routing (Topic Trie)
The Radix Tree used for topic matching is a pure data structure that doesn't need to be in the root.
- **Action:** Create `internal/routing` or `internal/trie`.
- **Action:** Move `topic_trie.go` to `internal/trie/trie.go`.
- **Action:** Extract trie-specific tests from `topic_test.go` and move them to `internal/trie/trie_test.go`.

### Phase 3: Extract Queues
The logic queue used for buffering publishes is another independent structure.
- **Action:** Create `internal/queue`.
- **Action:** Move `logic_queue.go` to `internal/queue/queue.go`.
- **Action:** Update imports in the root package to use `queue.PublishQueue`.

### Phase 4: Extract Persistence Implementations
While `SessionStore` is a core interface, specific implementations don't need to clutter the root.
- **Action:** Create a public subpackage `store/filestore` (or similar).
- **Action:** Move `file_store.go`, `session_store.go` (the AsyncStore part, maybe into `store/async`), and `file_store_test.go` into these new subpackages. 
- **Action:** `SessionStore`, `PersistedPublish`, and `PersistedSubscription` interfaces/types remain in the root `mq` package since they define the contract.
