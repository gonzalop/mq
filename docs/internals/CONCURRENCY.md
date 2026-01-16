# Concurrency Model

This document describes the concurrency model used in the `mq` library.

## Overview

The `mq` library uses a shared-state concurrency model protected by mutexes. This design allows for high throughput, particularly for publishing, by enabling concurrent access to the publish queue and session state while maintaining thread safety.

## Locking Strategy

The `Client` struct uses two primary locks to protect its state:

1.  **`sessionLock` (`sync.Mutex`)**: This is the "inner" lock and protects the core session state.
2.  **`connLock` (`sync.RWMutex`)**: This protects the network connection and connection status.

### `sessionLock` Protected State

The `sessionLock` MUST be held when accessing or modifying the following fields:

-   `pending`: Map of pending operations (PacketID -> pendingOp).
-   `nextPacketID`: Counter for generating packet IDs.
-   `subscriptions`: Map of active subscriptions.
-   `inFlightCount`: Count of QoS 1 & 2 messages currently in flight.
-   `publishQueue`: Slice of buffered publish requests awaiting flow control credits.
-   `receivedQoS2`: Map of received QoS 2 packet IDs (for exactly-once semantics).

### `connLock` Protected State

The `connLock` MUST be held when accessing:

-   `conn`: The underlying network connection (`net.Conn`).
-   `connected`: Boolean flag indicating connection status.
-   `lastDisconnectReason`: Stores the reason for the last disconnection.

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

### Subscribing (`Subscribe`, `Unsubscribe`)

1.  The method acquires `sessionLock`.
2.  It assigns a PacketID.
3.  It updates `pending` map.
4.  It releases `sessionLock`.
5.  It sends the packet to the `outgoing` channel.

### Incoming Packets (`logicLoop`)

The `logicLoop` runs in a separate goroutine and handles incoming packets from the `incoming` channel (populated by `readLoop`).

1.  When a packet arrives (e.g., `PUBACK`, `SUBACK`), `logicLoop` acquires `sessionLock`.
2.  It processes the packet (updates `pending`, `inFlightCount`, `subscriptions`).
3.  If a `PUBACK`/`PUBCOMP` frees up a flow control slot, it drains the `publishQueue` (sending queued messages).
4.  It releases `sessionLock`.
5.  It completes the associated `Token` (which notifies the user).

## Deadlock Prevention

-   **Lock Ordering**: If both locks are needed, `connLock` should generally be acquired *before* `sessionLock` if meaningful, but in practice they protect disjoint sets of state and are rarely held simultaneously.
-   **No Blocking IO under Lock**: We avoid blocking IO (reading/writing to network) while holding `sessionLock`. Packets are sent via the buffered `outgoing` channel. The `writeLoop` handles the actual socket write.
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

## Thread Safety

All public methods of `Client` are thread-safe and can be called concurrently. Internal methods (prefixed with `internal` or `handle`) usually assume the caller holds the necessary locks (check method documentation).
