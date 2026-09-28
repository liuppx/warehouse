package toolcredential

import (
	"testing"
	"time"
)

func TestCredentialAllowsScopedPath(t *testing.T) {
	c, err := New("user-1", "reviews", "hash", []string{ScopeAssetWrite}, []string{"/personal/reviews/conversations/"}, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if !c.Allows(ScopeAssetWrite, "/personal/reviews/conversations/a.md") {
		t.Fatal("expected path to be allowed")
	}
	if c.Allows(ScopeAssetRead, "/personal/reviews/conversations/a.md") {
		t.Fatal("unexpected read permission")
	}
	if c.Allows(ScopeAssetWrite, "/personal/other/a.md") {
		t.Fatal("unexpected path permission")
	}
}

func TestCredentialRejectsUnsafePath(t *testing.T) {
	if _, err := New("user-1", "reviews", "hash", []string{ScopeAssetRead}, []string{"/"}, time.Now().Add(time.Hour)); err != ErrInvalidPathPrefix {
		t.Fatalf("got %v", err)
	}
}
