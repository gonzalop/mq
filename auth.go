package mq

// Authenticator handles the authentication exchange for a specific authentication method.
//
// Users implement this interface to provide custom authentication logic for
// MQTT v5.0 Enhanced Authentication (AUTH packet flow). This enables support for
// challenge/response mechanisms such as SCRAM, OAuth, Kerberos, or custom methods.
//
// The authentication flow:
//  1. InitialData() is called to get data for CONNECT packet
//  2. HandleChallenge() is called for each AUTH packet from server
//  3. Complete() is called when CONNACK is received (authentication succeeded)
//
// Example implementation (simple token):
//
//	type TokenAuth struct {
//	    token string
//	}
//
//	func (t *TokenAuth) Method() string {
//	    return "TOKEN"
//	}
//
//	func (t *TokenAuth) InitialData() ([]byte, error) {
//	    return []byte(t.token), nil
//	}
//
//	func (t *TokenAuth) HandleChallenge(data []byte, code uint8) ([]byte, error) {
//	    return nil, fmt.Errorf("unexpected challenge")
//	}
//
//	func (t *TokenAuth) Complete() error {
//	    return nil
//	}
type Authenticator interface {
	// Method returns the authentication method name.
	//
	// This is sent in the CONNECT packet's AuthenticationMethod property.
	// Common values: "SCRAM-SHA-1", "SCRAM-SHA-256", "OAUTH2", "KERBEROS".
	//
	// The method name should match what the server expects.
	Method() string

	// InitialData returns the initial authentication data to send in CONNECT.
	//
	// This data is included in the CONNECT packet's AuthenticationData property.
	// Return nil or empty slice if no initial data is needed.
	//
	// For SCRAM, this would be the client-first-message.
	// For OAuth, this might be an access token.
	InitialData() ([]byte, error)

	// HandleChallenge processes a challenge from the server and returns response data.
	//
	// This method is called when the client receives an AUTH packet from the server
	// during the authentication exchange. The reasonCode will typically be:
	//   - 0x18 (Continue authentication) - Server is continuing the exchange
	//   - 0x00 (Success) - Authentication completed successfully
	//
	// Return the response data to send back to the server in an AUTH packet.
	// Return an error if the challenge cannot be processed or authentication fails.
	//
	// During the initial connection, this method is called synchronously as part of
	// the handshake, so its running time counts against ConnectTimeout.
	//
	// During re-authentication, it is called in its own goroutine and does not block
	// processing of PUBLISH, PUBACK, and other packets; the response is sent when it
	// returns. Because it runs on a goroutine owned by the client, any state it shares
	// with the rest of your code must be safe for concurrent use.
	HandleChallenge(challengeData []byte, reasonCode uint8) ([]byte, error)

	// Complete is called when authentication succeeds (CONNACK received).
	//
	// This allows the authenticator to perform any cleanup, store tokens, or
	// finalize the authentication state.
	//
	// Return an error if post-authentication setup fails. This will be logged
	// but won't affect the connection (CONNACK was already successful).
	//
	// After a successful re-authentication, Complete is called in its own goroutine.
	Complete() error
}
