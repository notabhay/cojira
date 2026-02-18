package output

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
)

// IdempotencyKey produces a deterministic SHA-256 hex digest from the given parts.
// Parts are JSON-serialised with sorted keys before hashing.
func IdempotencyKey(parts ...any) string {
	b, _ := json.Marshal(parts)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// RequestID generates a short (16 hex chars) pseudo-random request identifier
// by hashing a timestamp, PID, and random bytes.
func RequestID() string {
	randomBytes := make([]byte, 8)
	_, _ = rand.Read(randomBytes)
	payload := fmt.Sprintf("%s::%d::%s", UTCNowISO(), os.Getpid(), hex.EncodeToString(randomBytes))
	h := sha256.Sum256([]byte(payload))
	return hex.EncodeToString(h[:])[:16]
}
