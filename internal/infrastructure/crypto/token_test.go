package crypto

import "testing"

func TestTokenHasherSupportsLongToken(t *testing.T) {
	hasher := NewTokenHasher()
	token := "wts_0123456789abcdef0123456789abcdef_abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_-"
	hash := hasher.Hash(token)
	if err := hasher.Verify(hash, token); err != nil {
		t.Fatalf("verify token: %v", err)
	}
	if err := hasher.Verify(hash, token+"x"); err == nil {
		t.Fatal("expected mismatched token to fail")
	}
}
