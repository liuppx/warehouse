package share

import (
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

var (
	ErrShareNotFound = errors.New("share item not found")
	ErrShareExpired  = errors.New("share item expired")
	ErrInvalidShare  = errors.New("invalid share")
)

const (
	ModeDownload = "download"
	ModePreview  = "preview"
)

// ShareItem 文件分享实体
type ShareItem struct {
	ID               string
	Token            string
	UserID           string
	CreatorUserID    string
	SourceShareID    string
	SourceResourceID string
	Username         string
	Name             string
	Path             string
	IsDir            bool
	Mode             string
	ExpiresAt        *time.Time
	ViewCount        int64
	DownloadCount    int64
	CreatedAt        time.Time
}

func NewResourceDerivedShareItem(ownerUserID, ownerUsername, creatorUserID, sourceResourceID, path, name, mode string, expiresAt *time.Time) *ShareItem {
	item := NewShareItem(ownerUserID, ownerUsername, path, name, mode, expiresAt)
	item.CreatorUserID = creatorUserID
	item.SourceResourceID = sourceResourceID
	return item
}

// NewShareItem 创建分享记录
func NewShareItem(userID, username, path, name, mode string, expiresAt *time.Time) *ShareItem {
	now := time.Now()
	return &ShareItem{
		ID:            uuid.NewString(),
		Token:         uuid.NewString(),
		UserID:        userID,
		CreatorUserID: userID,
		Username:      username,
		Name:          name,
		Path:          path,
		Mode:          normalizeMode(mode),
		ExpiresAt:     expiresAt,
		ViewCount:     0,
		DownloadCount: 0,
		CreatedAt:     now,
	}
}

func NewDerivedShareItem(ownerUserID, ownerUsername, creatorUserID, sourceShareID, path, name, mode string, expiresAt *time.Time) *ShareItem {
	item := NewShareItem(ownerUserID, ownerUsername, path, name, mode, expiresAt)
	item.CreatorUserID = creatorUserID
	item.SourceShareID = sourceShareID
	return item
}

// IsExpired 判断是否过期
func (s *ShareItem) IsExpired() bool {
	if s.ExpiresAt == nil {
		return false
	}
	return time.Now().After(*s.ExpiresAt)
}

func (s *ShareItem) IsPreviewMode() bool {
	if s == nil {
		return false
	}
	return normalizeMode(s.Mode) == ModePreview
}

func NormalizeMode(mode string) (string, error) {
	switch strings.TrimSpace(mode) {
	case "", ModeDownload:
		return ModeDownload, nil
	case ModePreview:
		return ModePreview, nil
	default:
		return "", errors.New("invalid share mode")
	}
}

func normalizeMode(mode string) string {
	switch mode {
	case ModePreview:
		return ModePreview
	default:
		return ModeDownload
	}
}
