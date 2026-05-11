# Enhanced Authentication (MQTT 5.0)

MQTT 5.0 introduced **Enhanced Authentication**, allowing for challenge-response mechanisms like SCRAM, Kerberos, or OAuth. This is a significant improvement over the simple username/password model used in MQTT 3.1.1.

## The `Authenticator` Interface

To implement a custom authentication mechanism, you must satisfy the `Authenticator` interface:

```go
type Authenticator interface {
    // Method returns the name of the authentication method (e.g., "SCRAM-SHA-256").
    Method() string

    // InitialData returns the data to be sent in the CONNECT packet.
    // Return nil if no initial data is required.
    InitialData() ([]byte, error)

    // HandleChallenge processes an AUTH packet from the server and returns 
    // the data for the client's AUTH response.
    HandleChallenge(data []byte, reason packets.ReasonCode) ([]byte, error)

    // Success processes the final successful AUTH or CONNACK properties.
    Success(data []byte) error
}
```

---

## How the Flow Works

1.  **CONNECT**: The client sends a `CONNECT` packet containing the `Authentication Method` and optional `InitialData`.
2.  **Challenge**: If the server requires a challenge, it responds with an `AUTH` packet (Reason Code `0x18 - Continue authentication`).
3.  **Response**: The client calls `HandleChallenge` and sends back a new `AUTH` packet.
4.  **Completion**: This can repeat multiple times until the server sends a `CONNACK` (Success) or disconnects the client.

---

## Usage Example

```go
client, err := mq.Dial("tcp://broker:1883",
    mq.WithProtocolVersion(mq.ProtocolV50),
    mq.WithAuthenticator(myScramAuthenticator),
)
```

---

## Built-in Security Features

The `mq` library provides several protections for enhanced authentication:

### 1. Exchange Limits
To prevent "Auth Loops" or DoS attacks from a malicious server, you can limit the number of AUTH exchanges:

```go
mq.Dial(...,
    mq.WithMaxAuthExchanges(3), // Fail if more than 3 AUTH packets are exchanged
)
```

### 2. Timeout
Enhanced authentication must complete within the `ConnectTimeout` period.

### 3. Reason Code Mapping
The library passes the MQTT Reason Code into `HandleChallenge`, allowing your authenticator to distinguish between different types of server challenges or intermediate errors.

## Re-authentication

MQTT 5.0 also allows a client to re-authenticate *after* the connection is established (e.g., to rotate tokens).

```go
// Trigger a new authentication flow on an existing connection
token := client.Reauthenticate(ctx)
if err := token.Wait(ctx); err != nil {
    // Re-auth failed, the connection might be closed by the server
}
```

This will trigger the `Authenticator.InitialData()` and start a new `AUTH` exchange over the current connection.
