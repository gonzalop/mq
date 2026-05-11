# Concurrency Model

This document describes the concurrency model used in the `mq` library.

## Overview

The `mq` library uses a shared-state concurrency model protected by mutexes. This design allows for high throughput, particularly for publishing, by enabling concurrent access to the publish queue and session state while maintaining thread safety.

## Locking Strategy

The `Client` struct uses several mechanisms to protect its state:

1.  **`sessionLock` (`sync.Mutex`)**: This is the "inner" lock and protects the core session state.
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

This is used by the public `Disconnect()` method to wait for a clean shutdown without leaking helper goroutines. When `Disconnect()` is called, it signals all loops to stop and then polls `activeLoops` until it reaches zero or the timeout is exceeded. Internal disconnections (e.g., due to protocol errors) use a non-blocking path to avoid deadlocking if the disconnect is triggered from within one of these loops.

### `connLock` Protected State

The `connLock` MUST be held when accessing:

-   `conn`: The underlying network connection (`net.Conn`).
-   `connected`: Boolean flag indicating connection status.
-   `lastDisconnectReason`: Stores the reason for the last disconnection.

### `receivedAliasesLock` Protected State

This lock guards the `receivedAliases` map, which stores the mapping between Topic Alias IDs and their corresponding topic names for incoming messages. It uses an `RWMutex` because reads (resolving an alias) are much more frequent than writes (registering a new alias).

### Atomic Statistics

Client statistics (`PacketsSent`, `BytesReceived`, etc.) are stored using `atomic` types (e.g., `atomic.Uint64`). They can be accessed safely from any goroutine via `client.GetStats()` without acquiring any of the main locks. This ensures that monitoring does not contend with the critical path.

## Request Flow

### Publishing (`Publish`)

1.  The `Publish` method acquires `sessionLock`.
2.  It validates the request and checks flow control limits (`inFlightCount` vs `ReceiveMaximum`).
3.  If window is available:
    -   It assigns a PacketID (if QoS > 0).
    -   It adds the operation to `pending`.
    -   It releases `sessionLock`.
    -   It sends the packet to the `outgoing` channel (which is processed by `writeLoop`).
4.  If window is full:
    -   It appends the request to `publishQueue`.
    -   It releases `sessionLock`.

### Flow Control

It is important to distinguish between **Outbound** and **Inbound** flow control:

-   **Outbound (Client Publishing)**: Managed by `inFlightCount`, `ReceiveMaximum` (server's limit), and the `publishQueue`. This ensures we don't flood the server.
-   **Inbound (Server Publishing)**: Managed by `ReceiveMaximum` (client's limit). The client enforces this strict limit when processing incoming packets in `logicLoop` (under `sessionLock`). If the server violates it, the client may disconnect with a protocol error.

### Subscribing (`Subscribe`, `Unsubscribe`)

1.  The method acquires `sessionLock`.
2.  It assigns a PacketID.
3.  It updates `pending` map.
4.  It releases `sessionLock`.
5.  It sends the packet to the `outgoing` channel.

### Incoming Packets (`logicLoop`)

The `logicLoop` runs in a separate goroutine and handles incoming packets from the `incoming` channel (populated by `readLoop`).

1.  When a packet arrives (e.g., `PUBACK`, `SUBACK`), `logicLoop` acquires `sessionLock`.
2.  For `PUBLISH` packets, the `logicLoop` orchestrates processing through a sequence of modular helpers:
    -   **`processTopicAlias`**: Resolves or registers v5.0 topic aliases (acquires `receivedAliasesLock`).
    -   **`enforceReceiveMaximum`**: Validates inbound flow control against the client's `ReceiveMaximum`.
    -   **`handleQoS2Duplicate`**: Ensures exactly-once delivery by checking the `receivedQoS2` map and persistence store.
    -   **`dispatchAndAcknowledge`**: Manages handler execution and sends MQTT acknowledgments (`PUBACK`/`PUBREC`).
3.  It processes acknowledgment packets (updates `pending`, `inFlightCount`, `subscriptions`).
4.  If a `PUBACK`/`PUBCOMP` frees up a flow control slot, it drains the `publishQueue` (sending queued messages).
5.  It releases `sessionLock`.
6.  It completes the associated `Token` (which notifies the user).

## Deadlock Prevention

-   **Lock Ordering**: If both locks are needed, `connLock` should generally be acquired *before* `sessionLock` if meaningful, but in practice they protect disjoint sets of state and are rarely held simultaneously.
-   **No Blocking IO under Lock**: We avoid blocking IO (reading/writing to network) while holding `sessionLock`. Packets are sent via the buffered `outgoing` channel. The `writeLoop` handles the actual socket write.
-   **Non-Blocking Internal Disconnect**: To prevent deadlocks, internal methods that trigger a client shutdown (like protocol error handlers in the `logicLoop`) must use the non-blocking disconnect path. This allows the calling loop to terminate gracefully after signaling the other loops to stop.
-   **Callbacks**: User callbacks are invoked in separate goroutines *without* holding `sessionLock`. This ensures that slow or blocking user code does not block the client's internal logic loop or cause deadlocks if the callback calls client methods.
    -   See "Callback Execution" section below for details on each callback.

## Callback Execution

All callbacks are executed asynchronously in their own goroutines. This design prevents user code from blocking the critical `logicLoop` or network I/O, and prevents deadlocks if the callback calls client methods (e.g. `Subscribe` inside `OnConnect`).

| Callback | Execution Mode | Rationale |
| :--- | :--- | :--- |
| `OnConnect` | **Asynchronous** | Allows implementing complex setup logic (subscriptions, publishing) without blocking the connection flow. |
| `OnConnectionLost` | **Asynchronous** | Ensures cleanup or alerting logic doesn't delay internal teardown or reconnection attempts. |
| `OnServerRedirect` | **Asynchronous** | Prevents blocking the processing of CONNACK properties; allows user to decide on reconnection strategy independently. |
| `MessageHandler` | **Asynchronous** | Critical for high throughput; slow message processing shouldn't block reception of other packets or ACKs. |

### Handler Safety

While handlers are asynchronous, the library provides two mechanisms to prevent them from overwhelming the system:
1. **`MaxHandlerConcurrency`**: A semaphore-based limit on the number of handler goroutines that can run at once.
2. **`HandlerTimeout`**: A cancelable `context.Context` is provided in the `Message` struct. If the handler takes longer than the configured timeout, the context is canceled. This allows handlers to respond to timeouts and clean up resources. Additionally, a watchdog logs a warning and releases the semaphore slot to ensure that a single hung handler cannot permanently reduce the client's processing capacity.

## Interceptors (Middleware)

Interceptors are executed synchronously within the goroutine that invokes the wrapped function:

- **Handler Interceptors**: Executed within the asynchronous goroutine spawned for the `MessageHandler`. They do not block the `logicLoop`.
- **Publish Interceptors**: Executed within the caller's goroutine when `client.Publish` is called.

Because they are part of the execution chain, interceptors should be non-blocking and efficient to avoid introducing latency in message processing or publishing.

## Thread Safety

All public methods of `Client` are thread-safe and can be called concurrently. Internal methods (prefixed with `internal` or `handle`) usually assume the caller holds the necessary locks (check method documentation).
