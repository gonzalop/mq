# Concurrency Model

This document describes the concurrency model used in the `mq` library.

## Overview

The `mq` library uses a shared-state concurrency model optimized for high throughput and low latency. The core of the library is a single-threaded "Logic Loop" that manages state transitions, while I/O and user callbacks are handled in separate, concurrent goroutines.

## Locking Strategy

The `Client` struct uses several mechanisms to protect its state:

1.  **`sessionLock` (`sync.Mutex`)**: This protects the core session state. It is held only for short-lived, in-memory state updates.
2.  **`connLock` (`sync.RWMutex`)**: This protects the network connection and connection status.
3.  **`connState` (`atomic.Pointer[connectionState]`)**: This provides a thread-safe, immutable snapshot of connection-time properties and server capabilities (MQTT v5.0).
4.  **`receivedAliasesLock` (`sync.RWMutex`)**: This protects the mapping of inbound Topic Aliases (ID -> Topic Name) for MQTT v5.0.
5.  **`topicAliasesLock` (`sync.Mutex`)**: This protects the mapping of outbound Topic Aliases (Topic Name -> ID) for MQTT v5.0.

### `sessionLock` Protected State

The `sessionLock` MUST be held when accessing or modifying the following fields:

-   `pending`: Map of pending operations (PacketID -> pendingOp).
-   `nextPacketID`: Counter for generating packet IDs.
-   `subscriptions`: Map of active subscriptions.
-   `inFlightCount`: Count of QoS 1 & 2 messages currently in flight.
-   `publishQueue`: Slice of buffered publish requests awaiting flow control credits.
-   `receivedQoS2`: Map of received QoS 2 packet IDs (for exactly-once semantics).
-   `inboundUnacked`: Map of received QoS 1 & 2 packet IDs that are awaiting acknowledgment (used for inbound flow control).

### Lifecycle Tracking

The client uses an atomic counter, `activeLoops`, to track the number of long-running background goroutines (`logicLoop`, `reconnectLoop`, `readLoop`, `writeLoop`). 

This is used by the public `Disconnect()` method to wait for a clean shutdown without leaking helper goroutines.

## The Logic Loop (`logicLoop`)

The `logicLoop` is the single-threaded state machine that manages the client's internal state. By confining state changes to this loop, we minimize lock contention and prevent complex race conditions.

1.  **Incoming Packets**: When a packet arrives from the `readLoop`, the `logicLoop` invokes `handleIncoming`.
2.  **Functional Handling**: Internal handler methods (`handlePublish`, `handleAck`, etc.) process the state changes and return a slice of `[]packets.Packet` representing the necessary response (e.g., a `PUBACK`).
3.  **Non-blocking Outbound**: The `logicLoop` calls the `sendPackets` helper to dispatch these responses to the `outgoing` channel.
4.  **Lock Isolation**: Most state updates happen within the handlers (under `sessionLock`), but the actual network I/O (via `sendPackets`) and the execution of user callbacks happen **outside** the critical path of the logic loop.

### `sendPackets` Strategy

The `sendPackets` helper uses a non-blocking `select` when sending to the `outgoing` channel. If the channel is full, the packet is dropped (and will be retransmitted later if it's a QoS 1 or 2 packet). This ensures that a stalled `writeLoop` (due to network backpressure) cannot block the `logicLoop`, allowing it to continue processing ACKs from the server and freeing up resources.

## Request Flow

### Publishing (`Publish`)

1.  The `Publish` method acquires `sessionLock`.
2.  It validates the request and checks flow control limits (`inFlightCount` vs `ReceiveMaximum`).
3.  If window is available:
    -   It assigns a PacketID (if QoS > 0).
    -   It adds the operation to `pending`.
    -   It releases `sessionLock`.
    -   It sends the packet to the `outgoing` channel.
4.  If window is full:
    -   It appends the request to `publishQueue`.
    -   It releases `sessionLock`.

### Incoming Packets

1.  The `readLoop` parses a packet and sends it to the `incoming` channel.
2.  The `logicLoop` receives the packet and calls `handleIncoming(pkt)`.
3.  `handleIncoming` dispatches to specific handlers:
    -   **`handlePublish`**: Finds matching handlers using the **Radix Tree** (`topicTrie`), dispatches them asynchronously, and returns a `PUBACK`/`PUBREC`.
    -   **`handleAck`**: Removes the original packet from `pending`, decrements `inFlightCount`, and attempts to drain the `publishQueue`.
4.  The `logicLoop` sends any returned response packets using `sendPackets`.

## Topic Routing (Radix Tree)

Subscription matching is performed using a high-performance **Radix Tree** (`topicTrie`). 
- **Efficiency**: Matching complexity is O(K) where K is the number of levels in the topic, rather than O(N) where N is the number of subscriptions.
- **Concurrent Handlers**: The trie supports multiple handlers for the same topic filter. If multiple handlers are registered for the same filter, all will be executed.
- **Unsubscribe**: Unsubscribing from a topic filter removes **all** handlers associated with that specific filter.

## Deadlock Prevention

-   **No Blocking IO under Lock**: We never perform network I/O or disk I/O (via `SessionStore`) while holding a mutex.
-   **Async Callbacks**: All user callbacks are invoked in separate goroutines.
-   **Lock Ordering**: The library maintains a strict hierarchy to prevent circular waits.

## Reliability and Fail-Fast

### User Callbacks and Panics

In accordance with Go's "fail-fast" philosophy, the library **does not** wrap user-facing callbacks (`MessageHandler`, `OnConnect`, `OnConnectionLost`, etc.) in recovery blocks.

If a user-provided handler panics, it will propagate up and terminate the entire application. This ensures that logic errors in user code are not silenced and can be caught early during development. Users who require the client to survive handler panics are responsible for implementing their own `recover()` logic within their handlers.

### Callback Execution

| Callback | Execution Mode | Rationale |
| :--- | :--- | :--- |
| `OnConnect` | **Asynchronous** | Allows setup logic (subscriptions, publishing) without blocking connection. |
| `OnConnectionLost` | **Asynchronous** | Ensures cleanup logic doesn't delay reconnection. |
| `MessageHandler` | **Asynchronous** | Prevents slow processing from blocking the `logicLoop` or ACKs. |

### Handler Safety

- **`MaxHandlerConcurrency`**: Limits the number of concurrent handler goroutines.
- **`HandlerTimeout`**: Automatically cancels the `Message.Context` if a handler exceeds its time limit.
