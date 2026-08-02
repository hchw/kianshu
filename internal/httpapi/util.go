package httpapi

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"time"
)

// now is indirection for tests.
var now = func() time.Time { return time.Now() }

// newToken returns a random hex token (32 bytes).
func newToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// hashToken hashes a session token for storage.
func hashToken(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}
