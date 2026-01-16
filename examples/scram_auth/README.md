# SCRAM-SHA-256 Authentication Example

This example demonstrates how to implement **Enhanced Authentication** (MQTT v5.0) using the **SCRAM-SHA-256** mechanism.

It uses the `mq.Authenticator` interface to handle the SASL-style challenge/response handshake with the server.

## Prerequisites

- An MQTT server configured to support SCRAM-SHA-256 (e.g., Mosquitto with `mosquitto-go-auth` or similar plugin, or a compliant cloud server).
- Go 1.24+

## Description

The `ScramAuthenticator` struct implements the logic for:
1.  Sending the **client-first-message** (initial authentication data).
2.  Processing the **server-first-message** (challenge), which includes the salt and iteration count.
3.  Deriving the salted password using **PBKDF2** (via `golang.org/x/crypto/pbkdf2`).
4.  Sending the **client-final-message** (proof).

> **Tip:** You can copy the `scram_authenticator.go` file directly into your project to add SCRAM support! It is designed to be a standalone, drop-in component.

## Usage

The example connects to `tcp://localhost:1883` by default. You can configure the credentials via environment variables:

```bash
# Default credentials (user / password123)
go build
./scram_auth

# Custom credentials
MQTT_USERNAME=myuser MQTT_PASSWORD=mypass ./scram_auth
```

## Code Highlight

The core logic resides in `HandleChallenge`:

```go
func (s *ScramAuthenticator) HandleChallenge(data []byte, reasonCode uint8) ([]byte, error) {
    // 1. Parse server challenge (nonce, salt, iterations)
    // 2. Derive key using PBKDF2
    // 3. Calculate ClientProof
    // 4. Return client-final-message
}
```
