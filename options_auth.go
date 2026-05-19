package mq

// WithCredentials sets the username and password for authentication.
func WithCredentials(username, password string) Option {
	return func(o *clientOptions) {
		o.Username = username
		o.Password = password
	}
}

// WithAuthenticator sets the authenticator for enhanced authentication (MQTT v5.0).
//
// Enhanced authentication allows challenge/response authentication mechanisms
// such as SCRAM, OAuth, Kerberos, or custom methods. The authenticator handles
// the authentication exchange via the AUTH packet flow.
//
// If set, the client will:
//  1. Send AuthenticationMethod + InitialData in CONNECT
//  2. Handle AUTH challenges from the server via HandleChallenge
//  3. Complete authentication when CONNACK is received
//
// This option is only relevant for MQTT v5.0. For v3.1.1, use WithCredentials
// for simple username/password authentication.
//
// Example:
//
//	auth := &MyScramAuthenticator{username: "user", password: "pass"}
//	client, err := mq.Dial("tcp://localhost:1883",
//	    mq.WithProtocolVersion(mq.ProtocolV50),
//	    mq.WithAuthenticator(auth))
func WithAuthenticator(auth Authenticator) Option {
	return func(o *clientOptions) {
		o.Authenticator = auth
	}
}
