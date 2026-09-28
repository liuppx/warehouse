package toolcredential

import (
	"errors"
	"path"
	"strings"
	"time"
)

var (
	ErrInvalidName       = errors.New("tool credential name is required")
	ErrInvalidOwner      = errors.New("tool credential owner is required")
	ErrInvalidSecret     = errors.New("tool credential secret is required")
	ErrInvalidScope      = errors.New("tool credential scope is invalid")
	ErrInvalidPathPrefix = errors.New("tool credential path prefix is invalid")
	ErrInvalidExpiry     = errors.New("tool credential expiry is invalid")
)

const (
	StatusActive    = "active"
	StatusRevoked   = "revoked"
	ScopeAssetRead  = "asset:read"
	ScopeAssetWrite = "asset:write"
)

type Credential struct {
	ID           string
	OwnerUserID  string
	Name         string
	SecretHash   string
	Scopes       []string
	PathPrefixes []string
	Status       string
	ExpiresAt    time.Time
	LastUsedAt   *time.Time
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

func New(ownerUserID, name, secretHash string, scopes, pathPrefixes []string, expiresAt time.Time) (*Credential, error) {
	if strings.TrimSpace(ownerUserID) == "" {
		return nil, ErrInvalidOwner
	}
	if strings.TrimSpace(name) == "" {
		return nil, ErrInvalidName
	}
	if strings.TrimSpace(secretHash) == "" {
		return nil, ErrInvalidSecret
	}
	if expiresAt.IsZero() || !expiresAt.After(time.Now()) {
		return nil, ErrInvalidExpiry
	}
	normalizedScopes, err := normalizeScopes(scopes)
	if err != nil {
		return nil, err
	}
	normalizedPaths, err := normalizePaths(pathPrefixes)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	return &Credential{OwnerUserID: strings.TrimSpace(ownerUserID), Name: strings.TrimSpace(name), SecretHash: strings.TrimSpace(secretHash), Scopes: normalizedScopes, PathPrefixes: normalizedPaths, Status: StatusActive, ExpiresAt: expiresAt, CreatedAt: now, UpdatedAt: now}, nil
}

func (c *Credential) IsUsable(now time.Time) bool {
	return c != nil && c.Status == StatusActive && now.Before(c.ExpiresAt)
}

func (c *Credential) Allows(scope, rawPath string) bool {
	if c == nil || !c.IsUsable(time.Now()) {
		return false
	}
	allowedScope := false
	for _, item := range c.Scopes {
		if item == scope {
			allowedScope = true
			break
		}
	}
	if !allowedScope {
		return false
	}
	clean := normalizePath(rawPath)
	for _, prefix := range c.PathPrefixes {
		if clean == prefix || strings.HasPrefix(clean, strings.TrimSuffix(prefix, "/")+"/") {
			return true
		}
	}
	return false
}

func normalizeScopes(values []string) ([]string, error) {
	seen := map[string]bool{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != ScopeAssetRead && value != ScopeAssetWrite {
			return nil, ErrInvalidScope
		}
		if !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	if len(result) == 0 {
		return nil, ErrInvalidScope
	}
	return result, nil
}

func normalizePaths(values []string) ([]string, error) {
	if len(values) == 0 {
		return nil, ErrInvalidPathPrefix
	}
	seen := map[string]bool{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = normalizePath(value)
		if value != "/personal" && value != "/apps" && value != "/services" && !strings.HasPrefix(value, "/personal/") && !strings.HasPrefix(value, "/apps/") && !strings.HasPrefix(value, "/services/") {
			return nil, ErrInvalidPathPrefix
		}
		if !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	return result, nil
}

func normalizePath(value string) string {
	value = strings.TrimSpace(strings.ReplaceAll(value, "\\", "/"))
	if value == "" {
		return "/"
	}
	return path.Clean("/" + strings.TrimLeft(value, "/"))
}
