package main

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"strings"

	"golang.org/x/crypto/pbkdf2"
)

// ScramAuthenticator implements the mq.Authenticator interface for SCRAM-SHA-256.
type ScramAuthenticator struct {
	username string
	password string

	// State
	clientNonce string
	serverNonce string
	authMsg     string
}

func (s *ScramAuthenticator) Method() string {
	return "SCRAM-SHA-256"
}

// InitialData sends the client-first-message: n,,n=user,r=nonce
func (s *ScramAuthenticator) InitialData() ([]byte, error) {
	// Generate random client nonce
	nonce := make([]byte, 16)
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	s.clientNonce = base64.RawStdEncoding.EncodeToString(nonce)

	// gs2-header: n,, (no channel binding)
	// client-first-message-bare: n=user,r=nonce
	msg := fmt.Sprintf("n,,n=%s,r=%s", s.username, s.clientNonce)
	s.authMsg = msg[3:] // store bare message (n=user,r=nonce) for signature calc

	return []byte(msg), nil
}

// HandleChallenge processes the server-first-message and returns client-final-message
func (s *ScramAuthenticator) HandleChallenge(data []byte, reasonCode uint8) ([]byte, error) {
	// Parse server-first-message: r=nonce,s=salt,i=iter
	parts := parseSCRAMMessage(string(data))

	r, ok := parts["r"]
	if !ok || !strings.HasPrefix(r, s.clientNonce) {
		return nil, fmt.Errorf("invalid server nonce")
	}
	s.serverNonce = r

	saltStr, ok := parts["s"]
	if !ok {
		return nil, fmt.Errorf("missing salt")
	}
	salt, err := base64.StdEncoding.DecodeString(saltStr)
	if err != nil {
		return nil, fmt.Errorf("invalid salt: %v", err)
	}

	iterStr, ok := parts["i"]
	if !ok {
		return nil, fmt.Errorf("missing iterations")
	}
	var iter int
	fmt.Sscanf(iterStr, "%d", &iter)
	if iter < 1 {
		return nil, fmt.Errorf("invalid iterations")
	}

	// Update auth message for signature calculation
	// AuthMessage = client-first-message-bare + "," + server-first-message + "," + client-final-message-without-proof
	s.authMsg += "," + string(data) + ",c=biws,r=" + s.serverNonce

	// SaltedPassword := Hi(Normalize(password), salt, i)
	saltedPassword := pbkdf2.Key([]byte(s.password), salt, iter, 32, sha256.New)

	// ClientKey := HMAC(SaltedPassword, "Client Key")
	clientKey := computeHMAC(saltedPassword, []byte("Client Key"))

	// StoredKey := H(ClientKey)
	storedKey := sha256.Sum256(clientKey)

	// ClientSignature := HMAC(StoredKey, AuthMessage)
	clientSignature := computeHMAC(storedKey[:], []byte(s.authMsg))

	// ClientProof := ClientKey XOR ClientSignature
	clientProof := make([]byte, len(clientKey))
	for i := 0; i < len(clientKey); i++ {
		clientProof[i] = clientKey[i] ^ clientSignature[i]
	}

	proofStr := base64.StdEncoding.EncodeToString(clientProof)

	// client-final-message: c=biws,r=nonce,p=proof
	finalMsg := fmt.Sprintf("c=biws,r=%s,p=%s", s.serverNonce, proofStr)
	return []byte(finalMsg), nil
}

func (s *ScramAuthenticator) Complete() error {
	// In a full implementation, we would verify the server signature here.
	// ServerSignature := HMAC(ServerKey, AuthMessage)
	return nil
}

// Helper: Compute HMAC-SHA256
func computeHMAC(key, data []byte) []byte {
	h := hmac.New(sha256.New, key)
	h.Write(data)
	return h.Sum(nil)
}

// Helper: Parse SCRAM attributes (k=v,k=v)
func parseSCRAMMessage(msg string) map[string]string {
	parts := strings.Split(msg, ",")
	m := make(map[string]string)
	for _, p := range parts {
		if len(p) > 2 && p[1] == '=' {
			m[string(p[0])] = p[2:]
		}
	}
	return m
}
