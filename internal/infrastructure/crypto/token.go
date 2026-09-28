package crypto

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"strings"
)

var ErrInvalidTokenHash = errors.New("invalid token hash")

// TokenHasher is for high-entropy API tokens, not user passwords.
type TokenHasher struct{}

func NewTokenHasher() *TokenHasher { return &TokenHasher{} }

func (h *TokenHasher) Hash(token string) string {
	digest := sha256.Sum256([]byte(token))
	return "{sha256}" + hex.EncodeToString(digest[:])
}

func (h *TokenHasher) Verify(encoded, token string) error {
	encoded = strings.TrimSpace(encoded)
	if !strings.HasPrefix(encoded, "{sha256}") {
		return ErrInvalidTokenHash
	}
	expected, err := hex.DecodeString(strings.TrimPrefix(encoded, "{sha256}"))
	if err != nil || len(expected) != sha256.Size {
		return ErrInvalidTokenHash
	}
	digest := sha256.Sum256([]byte(token))
	if subtle.ConstantTimeCompare(expected, digest[:]) != 1 {
		return ErrInvalidTokenHash
	}
	return nil
}
