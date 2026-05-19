# Alternative Modularization Plan (Lean Root Directory)

**Objective:**
Same as `plan.md`: reduce the ~77-file root to a clean public API surface
(`Client`, `Options`, `Token`, `Message`, `SessionStore`) by extracting
independent domains into subpackages. This plan differs in **execution order**,
**API conservatism**, and adds two phases not present in the original.

**Strategy:** Extract-then-validate. Tackle zero-risk structural moves first,
establish the pattern, then address the judgment calls. **CRITICAL:** Wait for explicit user approval before moving on to the next phase.

---

## Phase 0 — Dependency Graph Analysis (prerequisite, ~30 min)

Before moving any file, verify the actual import graph so no phase surprises
you mid-refactor.

```bash
# Full dependency graph for root package
go list -f '{{.GoFiles}}' .
go list -f '{{.TestImports}}' .

# Check what each candidate file actually uses
grep -n "^func\|^type" topic_trie.go logic_queue.go session_store.go file_store.go

# Verify no file being "extracted" calls unexported Client methods
# (would make the move non-trivial)
grep -n "c\.\|Client\." logic_queue.go topic_trie.go
```

**Gate:** Do not proceed until you can confirm that:
1. `topic_trie.go` — only uses `strings`, no `Client` fields.
2. `logic_queue.go` — only calls `c.processPublishQueueLocked()` (a receiver
   method on `*Client`; this means it stays in root, see Phase 2 below).
3. `file_store.go` — only imports stdlib; no `Client` dependency.
4. `session_store.go`'s `AsyncStore` — uses no unexported `Client` state.

**Finding from analysis:** `logic_queue.go` defines a method *on* `*Client`, so
it cannot be moved to a separate package. Phase 2 is redesigned accordingly.

---

## Phase 1 — Split Large Source Files (same package, zero risk)

**Rationale:** `client.go` is 1,587 lines / 49 KB. `options.go` is 902 lines /
31 KB. These are harder to navigate than 40 small files, and the fix is
completely compiler-transparent (same package, just different file names).
Do this first because it reduces cognitive load for every subsequent phase.

### client.go → split into:

| New file | Contents (by line range) |
|---|---|
| `client.go` | `Client` struct definition, `DialContext`, `Dial`, `connect`, lifecycle |
| `client_connection.go` | `dialServer`, `buildConnectPacket`, `sendConnectPacket`, `validateConnack`, `finalizeConnection`, `prepareConnectionState` |
| `client_loops.go` | `readLoop`, `writeLoop`, `reconnectLoop`, `handleDisconnect` |
| `client_negotiation.go` | `performHandshake`, `processConnackProperties`, `extractServerCapabilities`, `ServerCapabilities`, `ConnectionUserProperties` |
| `client_accessors.go` | `IsConnected`, `Disconnect`, `disconnectWithReason`, `AssignedClientID`, `ServerKeepAlive`, `ServerReference`, `SessionExpiryInterval`, `ResponseInformation`, `GetStats` |
| `client_subscriptions.go` | `addSubscriptionLocked`, `removeSubscriptionLocked`, `wrapHandler` |

### options.go → split into:

| New file | Contents |
|---|---|
| `options.go` | Core `Option` type, `clientOptions` struct, primary option constructors |
| `options_auth.go` | Auth-related options |
| `options_tls.go` | TLS-related options |
| `options_session.go` | Session/persistence-related options |

**Verification:** `go build ./...` and `go test ./...` must pass unchanged.

---

## Phase 2 — Extract Topic Trie → `internal/trie`

**Rationale:** `topic_trie.go` is a pure data structure with no dependency on
`Client` or MQTT semantics. It only imports `"strings"`. It is a clean,
self-contained extraction.

**Note on `logic_queue.go`:** This file defines `processPublishQueueLocked()`
as a method on `*Client`. It is NOT extractable — it is effectively part of
`client.go` and should be merged into `client_loops.go` during Phase 1.

### Actions

1. Create `internal/trie/trie.go`:
   - Move `topicNode`, `topicTrie`, `newTopicTrie`, and all methods.
   - Package declaration: `package trie`.
   - No external imports beyond stdlib.

2. Move trie-specific tests from `topic_test.go`:
   - Tests that only exercise `insert`/`remove`/`match` → `internal/trie/trie_test.go`.
   - Tests that exercise MQTT-level subscription behavior stay in root `topic_test.go`.

3. Update `client.go`:
   ```go
   import "github.com/gonzalop/mq/internal/trie"
   // trie field:
   trie *trie.TopicTrie
   ```

4. Export the previously-unexported types minimally:
   - `topicTrie` → `TopicTrie`, `topicNode` → keep unexported inside `internal/`.
   - `MessageHandler` interface stays in root `mq` package; trie accepts it as a
     type parameter or interface — decide at implementation time.

**Verification:** `go build ./...` and `go test ./...` pass.

---

## Phase 3 — Persistence: API Audit Before Action

**Correction from initial draft:** The public API surface of `file_store.go` and
`session_store.go` is larger than it first appeared. These are all currently
exported from the root `mq` package and are part of the public API:

| Symbol | File | Type |
|---|---|---|
| `FileStore` | `file_store.go` | public struct |
| `FileStoreOption` | `file_store.go` | public type |
| `WithPermissions()` | `file_store.go` | public func |
| `NewFileStore()` | `file_store.go` | public func |
| `AsyncStore` | `session_store.go` | public struct |
| `NewAsyncStore()` | `session_store.go` | public func |

Moving any of these to a different package — whether `internal/` or a public
subpackage — changes their import path and **breaks existing callers**.

Since the module is pre-`v1` (`v0.7.x`, no `/v2` suffix in `go.mod`), a
breaking change is semver-permissible, but the decision must be conscious.

### Option A — Leave persistence in root (recommended default)

**No action for Phase 3.** `file_store.go` and `session_store.go` stay where
they are. The root is still cleaner after Phases 1 and 2, and there is no API
break.

What *does* move naturally: when Phase 3 is skipped, `file_store_test.go` and
`persistence_perf_test.go` also stay in root, which is correct since
`FileStore` is still a root-package type.

### Option B — Move to a public subpackage `mq/store` (breaking change)

If the team decides to make this a deliberate, versioned break:

1. Create `store/filestore/filestore.go` and `store/async/async.go`.
2. Move all six symbols above to their new packages.
3. In root, add deprecated shims for one release cycle:
   ```go
   // Deprecated: Use mq/store/filestore.FileStore instead.
   type FileStore = filestore.FileStore
   // Deprecated: Use mq/store/filestore.WithPermissions instead.
   var WithPermissions = filestore.WithPermissions
   ```
   Go type aliases (`=`) allow this without duplication.
4. Announce the break in the changelog and bump to `v0.8.0`.

### What always stays in root regardless of option chosen

- `SessionStore` interface  
- `PersistedPublish`, `PersistedSubscription`, `PublishProperties` types  

These define the contract used by `Client` internals and must remain in the
root `mq` package to avoid circular imports.

**Recommendation:** Choose Option A unless there is an active reason to
reorganize the public persistence API. The file count reduction from moving
persistence is small (3 files) and not worth a breaking change on its own.

---

## Phase 4 — Consolidate Black-Box Tests → `integration/`

**Rationale:** The `integration/` directory already exists and is the
established home for tests that use an external MQTT broker and only exercise
the public API (`package mq_test`). Tests in the root that fit this description
are misplaced.

**Important:** Almost ALL root test files currently declare `package mq` (not
`package mq_test`), meaning they access unexported fields and are white-box
tests. These **must stay in root**. Only the three `package mq_test` files and
any tests that can be migrated cleanly belong in `integration/`.

### Current `package mq_test` files in root:
- `disconnect_properties_test.go`
- `disconnect_v5_test.go`
- `helper_test.go`

### Decision matrix for each file:

| File | Package | Accesses unexported? | Action |
|---|---|---|---|
| `disconnect_properties_test.go` | `mq_test` | verify | move to `integration/` |
| `disconnect_v5_test.go` | `mq_test` | verify | move to `integration/` |
| `helper_test.go` | `mq_test` | verify | move or keep as shared helper |
| All others | `mq` | yes | **stay in root** |

**Do not bulk-move** `client_test.go`, `keepalive_test.go`, etc. — they are
white-box tests and moving them would require either making internals public
(wrong) or rewriting them (expensive).

**Verification:** `go test ./...` and `go test ./integration/...` both pass.

---

## Phase 6 — Lean Root Consolidation (Machinery & Tests)

**Rationale:** The root folder remains crowded with functional "islands" and a
high volume of test files. This phase merges related files and consolidates
test files into domain groups to reduce raw file count without touching the
public API or moving code across package boundaries.

### 1. Merge Internal `*Client` Method Files

Files that define methods on `*Client` cannot move to `internal/`. Instead,
merge small "island" files into logical group files.

- **Auth**: Merge `auth_handler.go` + `reauthenticate.go` → `client_auth.go`.
- **Aliases**: Merge `topic_alias.go` + `publish_alias.go` → `client_aliases.go`.
- **Requests**: Rename `requests.go` → `client_requests.go` for naming
  consistency. Do **not** merge into `logic.go` — the request types are used
  across `client.go`, `publish.go`, `subscribe.go`, and `client_persistence.go`;
  merging would bloat `logic.go` without reducing coupling.

**Do not extract** `topic.go` or `properties_convert.go` to `internal/`:
- `topic.go` — `validatePublishTopic` takes `*clientOptions` (unexported);
  `validatePublishCaps`/`validateSubscribeCaps` are `*Client` receivers.
  Only `MatchTopic` is standalone but it is part of the public API — an
  `internal/` shim would be pointless indirection for a single function.
- `properties_convert.go` — bridges `*Properties` (root public type) and
  `*packets.Properties` (internal). Moving it away from the boundary it
  serves adds import complexity without benefit.

### 2. Consolidate Internal Tests

The root test file count can be reduced significantly by grouping white-box
tests into domain-focused files. All merged tests remain `package mq`.

| Merged file | Source files absorbed |
|---|---|
| `logic_test.go` | + `deadlock_test.go`, `stability_test.go`, `fuzz_logic_test.go` |
| `client_aliases_test.go` | `topic_alias_test.go`, `topic_alias_benchmark_test.go`, `topic_alias_reconnect_test.go` |
| `client_connect_test.go` | `connect_limits_test.go`, `connect_user_properties_test.go`, `client_dial_test.go`, `client_negotiation_test.go` |
| `client_publish_test.go` | `payload_format_test.go`, `receive_maximum_test.go`, `reliability_test.go` |

**Verification:** `go build ./...` and `go test ./...` pass.

---

## Phase 7 — Final Cleanup

1. Remove now-empty or near-empty files created by splits.
2. Run `go vet ./...` and `staticcheck ./...`.
3. Update `README.md` package structure diagram if one exists.
4. Update `doc.go` if it references file names.
5. Tag a minor version bump (no API changes occurred).

---

## Key Differences vs `plan.md`

| Topic | `plan.md` | This plan |
|---|---|---|
| Order | tests → trie → queue → persistence | files → trie → persistence → tests |
| `logic_queue.go` | extract to `internal/queue` | merge into `client_loops.go` (it's a `*Client` method) |
| Persistence subpackage | public `store/filestore` | Option A: leave in root (no API break); Option B: `mq/store` with shims + `v0.8.0` bump |
| Big files | not addressed | Phase 1 splits `client.go` and `options.go` |
| Prerequisite | — | Phase 0 import graph analysis |
| Test migration scope | broader list of files | only the 3 actual `package mq_test` files |

---

## Risk Register

| Phase | Risk | Mitigation |
|---|---|---|
| 1 (split files) | None — same package | `go build` is the test |
| 2 (trie) | `MessageHandler` type crossing package boundary | Define trie with generic callback or keep `MessageHandler` in `internal/` |
| 3 (persistence) | `FileStore`/`AsyncStore`/`FileStoreOption`/`WithPermissions` are all **public API** — any move is a breaking change | Default to Option A (leave in root); only do Option B with explicit versioned break |
| 4 (tests) | White-box tests broken by move | Only move confirmed `package mq_test` files |
