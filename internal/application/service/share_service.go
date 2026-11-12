package service

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/yeying-community/warehouse/internal/domain/share"
	"github.com/yeying-community/warehouse/internal/domain/user"
	"github.com/yeying-community/warehouse/internal/infrastructure/config"
	"github.com/yeying-community/warehouse/internal/infrastructure/repository"
	"go.uber.org/zap"
)

// ShareService 文件分享服务
type ShareService struct {
	shareRepo            repository.ShareRepository
	userRepo             user.Repository
	config               *config.Config
	logger               *zap.Logger
	shareUserService     *ShareUserService
	sharedResourceAccess *SharedResourceAccessService
}

func (s *ShareService) SetShareUserService(service *ShareUserService) {
	s.shareUserService = service
}

func (s *ShareService) SetSharedResourceAccess(access *SharedResourceAccessService) {
	s.sharedResourceAccess = access
}

// NewShareService 创建分享服务
func NewShareService(
	shareRepo repository.ShareRepository,
	userRepo user.Repository,
	cfg *config.Config,
	logger *zap.Logger,
) *ShareService {
	return &ShareService{
		shareRepo: shareRepo,
		userRepo:  userRepo,
		config:    cfg,
		logger:    logger,
	}
}

type ShareCreateInput struct {
	Expiry ShareExpiryInput
	Mode   string
}

type PublicShareEntry struct {
	Name     string    `json:"name"`
	Path     string    `json:"path"`
	IsDir    bool      `json:"isDir"`
	Size     int64     `json:"size"`
	Modified time.Time `json:"modified"`
}

// CreateFromReceivedResource creates a public file link only while the
// recipient retains effective read access to the V3 resource. The access is
// rechecked whenever the public link is resolved.
func (s *ShareService) CreateFromReceivedResource(ctx context.Context, creator *user.User, resourceID, relativePath string, input ShareCreateInput) (*share.ShareItem, error) {
	if s.sharedResourceAccess == nil {
		return nil, fmt.Errorf("shared resource access service is unavailable")
	}
	resource, err := s.sharedResourceAccess.Authorize(ctx, resourceID, creator.ID, "read", time.Now())
	if err != nil {
		return nil, err
	}
	owner, err := s.userRepo.FindByID(ctx, resource.OwnerUserID)
	if err != nil {
		return nil, err
	}
	cleanRelative := ""
	if strings.TrimSpace(relativePath) != "" {
		cleanRelative, err = normalizeSharePath(relativePath, "")
		if err != nil {
			return nil, err
		}
	}
	resourcePath := path.Join("/", strings.TrimPrefix(resource.NormalizedPath, "/"), strings.TrimPrefix(cleanRelative, "/"))
	fullPath := s.resolveFullPath(owner, resourcePath)
	info, err := os.Stat(fullPath)
	if err != nil || info.IsDir() {
		return nil, fmt.Errorf("shared file not found")
	}
	expiresAt, err := input.Expiry.Resolve(time.Now())
	if err != nil {
		return nil, err
	}
	mode, err := share.NormalizeMode(input.Mode)
	if err != nil {
		return nil, err
	}
	item := share.NewResourceDerivedShareItem(owner.ID, owner.Username, creator.ID, resource.ID, resourcePath, info.Name(), mode, expiresAt)
	if err := s.shareRepo.Create(ctx, item); err != nil {
		return nil, err
	}
	return item, nil
}

// Create 创建分享链接
func (s *ShareService) Create(ctx context.Context, u *user.User, rawPath string, input ShareCreateInput) (*share.ShareItem, error) {
	cleanPath, err := normalizeSharePath(rawPath, s.webdavPrefix())
	if err != nil {
		return nil, err
	}
	if err := enforceAppScope(ctx, s.config, cleanPath, "create"); err != nil {
		return nil, err
	}

	fullPath := s.resolveFullPath(u, cleanPath)
	info, err := os.Stat(fullPath)
	if err != nil {
		return nil, fmt.Errorf("failed to stat file: %w", err)
	}
	name := filepath.Base(fullPath)
	expiresAt, err := input.Expiry.Resolve(time.Now())
	if err != nil {
		return nil, err
	}
	mode, err := share.NormalizeMode(input.Mode)
	if err != nil {
		return nil, err
	}

	item := share.NewShareItem(u.ID, u.Username, cleanPath, name, mode, expiresAt)
	item.IsDir = info.IsDir()
	if err := s.shareRepo.Create(ctx, item); err != nil {
		return nil, err
	}

	s.logger.Info("share created",
		zap.String("username", u.Username),
		zap.String("path", cleanPath),
		zap.String("token", item.Token),
	)

	return item, nil
}

// List 获取用户分享列表
func (s *ShareService) List(ctx context.Context, u *user.User) ([]*share.ShareItem, error) {
	items, err := s.shareRepo.GetByUserID(ctx, u.ID)
	if err != nil {
		return nil, err
	}
	scope, err := resolveAppScope(ctx, s.config)
	if err != nil {
		return nil, err
	}
	if !scope.active {
		for _, item := range items {
			normalized, err := s.normalizeItemPath(item.Path)
			if err != nil {
				s.logger.Warn("invalid share path",
					zap.String("username", u.Username),
					zap.String("path", item.Path),
					zap.Error(err))
				continue
			}
			item.Path = normalized
		}
		return items, nil
	}
	filtered := make([]*share.ShareItem, 0, len(items))
	for _, item := range items {
		normalized, err := s.normalizeItemPath(item.Path)
		if err != nil {
			s.logger.Warn("invalid share path",
				zap.String("username", u.Username),
				zap.String("path", item.Path),
				zap.Error(err))
			continue
		}
		item.Path = normalized
		if scope.allowsAny(normalized, "read") {
			filtered = append(filtered, item)
		}
	}
	return filtered, nil
}

// Revoke 取消分享
func (s *ShareService) Revoke(ctx context.Context, u *user.User, token string) error {
	item, err := s.shareRepo.GetByToken(ctx, token)
	if err != nil {
		return err
	}
	if item.CreatorUserID != u.ID {
		return fmt.Errorf("permission denied: not your share")
	}
	normalized, err := s.normalizeItemPath(item.Path)
	if err != nil {
		return err
	}
	item.Path = normalized
	if item.SourceShareID == "" {
		if err := enforceAppScope(ctx, s.config, normalized, "delete"); err != nil {
			return err
		}
	}
	return s.shareRepo.DeleteByToken(ctx, token)
}

// IncrementView 记录访问次数
func (s *ShareService) IncrementView(ctx context.Context, token string) error {
	return s.shareRepo.IncrementView(ctx, token)
}

// IncrementDownload 记录下载次数
func (s *ShareService) IncrementDownload(ctx context.Context, token string) error {
	return s.shareRepo.IncrementDownload(ctx, token)
}

// Resolve 根据 token 获取分享文件
func (s *ShareService) activeShare(ctx context.Context, token string) (*share.ShareItem, *user.User, string, error) {
	item, err := s.shareRepo.GetByToken(ctx, token)
	if err != nil {
		return nil, nil, "", err
	}
	if item.IsExpired() {
		return nil, nil, "", share.ErrShareExpired
	}
	if item.SourceShareID != "" {
		if s.shareUserService == nil {
			return nil, nil, "", share.ErrInvalidShare
		}
		creator, err := s.userRepo.FindByID(ctx, item.CreatorUserID)
		if err != nil {
			return nil, nil, "", share.ErrInvalidShare
		}
		source, _, err := s.shareUserService.ResolveForTarget(ctx, creator, item.SourceShareID, "read")
		if err != nil || !user.ParsePermissions(source.Permissions).Read {
			return nil, nil, "", share.ErrInvalidShare
		}
	}
	if item.SourceResourceID != "" {
		if s.sharedResourceAccess == nil {
			return nil, nil, "", share.ErrInvalidShare
		}
		creator, err := s.userRepo.FindByID(ctx, item.CreatorUserID)
		if err != nil {
			return nil, nil, "", share.ErrInvalidShare
		}
		if _, err := s.sharedResourceAccess.Authorize(ctx, item.SourceResourceID, creator.ID, "read", time.Now()); err != nil {
			return nil, nil, "", share.ErrInvalidShare
		}
	}

	u, err := s.userRepo.FindByID(ctx, item.UserID)
	if err != nil {
		return nil, nil, "", fmt.Errorf("failed to get user: %w", err)
	}

	normalized, err := s.normalizeItemPath(item.Path)
	if err != nil {
		return nil, nil, "", share.ErrInvalidShare
	}
	item.Path = normalized
	rootPath := s.resolveFullPath(u, normalized)
	return item, u, rootPath, nil
}

// Resolve 根据 token 获取分享文件。
func (s *ShareService) Resolve(ctx context.Context, token string) (*share.ShareItem, *os.File, os.FileInfo, error) {
	item, _, rootPath, err := s.activeShare(ctx, token)
	if err != nil {
		return nil, nil, nil, err
	}
	if item.IsDir {
		return nil, nil, nil, share.ErrInvalidShare
	}
	f, err := os.Open(rootPath)
	if err != nil {
		return nil, nil, nil, err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, nil, nil, err
	}
	if !info.Mode().IsRegular() {
		f.Close()
		return nil, nil, nil, share.ErrInvalidShare
	}
	return item, f, info, nil
}

func (s *ShareService) ResolveMetadata(ctx context.Context, token string) (*share.ShareItem, error) {
	item, _, _, err := s.activeShare(ctx, token)
	return item, err
}

// ResolvePublicDirectory returns a paginated, direct-child listing for a public directory share.
func (s *ShareService) ResolvePublicDirectory(ctx context.Context, token, relativePath string, offset, limit int) (*share.ShareItem, []PublicShareEntry, bool, error) {
	item, _, rootPath, err := s.activeShare(ctx, token)
	if err != nil {
		return nil, nil, false, err
	}
	if !item.IsDir {
		return nil, nil, false, share.ErrInvalidShare
	}
	directory, err := resolvePublicSharePath(rootPath, relativePath)
	if err != nil {
		return nil, nil, false, share.ErrInvalidShare
	}
	info, err := os.Stat(directory)
	if err != nil {
		return nil, nil, false, err
	}
	if !info.IsDir() {
		return nil, nil, false, share.ErrInvalidShare
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		return nil, nil, false, err
	}
	if offset < 0 {
		offset = 0
	}
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	result := make([]PublicShareEntry, 0, limit)
	seen := 0
	hasMore := false
	for _, entry := range entries {
		entryInfo, err := entry.Info()
		if err != nil || entryInfo.Mode()&os.ModeSymlink != 0 {
			continue
		}
		if !entryInfo.IsDir() && !entryInfo.Mode().IsRegular() {
			continue
		}
		if seen < offset {
			seen++
			continue
		}
		if len(result) == limit {
			hasMore = true
			break
		}
		entryPath := path.Join(relativePath, entry.Name())
		result = append(result, PublicShareEntry{
			Name: entry.Name(), Path: entryPath, IsDir: entryInfo.IsDir(),
			Size: entryInfo.Size(), Modified: entryInfo.ModTime(),
		})
		seen++
	}
	return item, result, hasMore, nil
}

// ResolvePublicFile opens a regular file within the root of a directory share.
func (s *ShareService) ResolvePublicFile(ctx context.Context, token, relativePath string) (*share.ShareItem, *os.File, os.FileInfo, error) {
	item, _, rootPath, err := s.activeShare(ctx, token)
	if err != nil {
		return nil, nil, nil, err
	}
	if !item.IsDir {
		return nil, nil, nil, share.ErrInvalidShare
	}
	targetPath, err := resolvePublicSharePath(rootPath, relativePath)
	if err != nil {
		return nil, nil, nil, share.ErrInvalidShare
	}
	f, err := os.Open(targetPath)
	if err != nil {
		return nil, nil, nil, err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, nil, nil, err
	}
	if !info.Mode().IsRegular() {
		f.Close()
		return nil, nil, nil, share.ErrInvalidShare
	}
	return item, f, info, nil
}

func resolvePublicSharePath(rootPath, relativePath string) (string, error) {
	if filepath.IsAbs(relativePath) || strings.Contains(relativePath, "\\") {
		return "", share.ErrInvalidShare
	}
	for _, segment := range strings.Split(relativePath, "/") {
		if segment == ".." {
			return "", share.ErrInvalidShare
		}
	}
	clean := path.Clean(relativePath)
	if clean == "." {
		clean = ""
	}
	if clean == ".." || strings.HasPrefix(clean, "../") {
		return "", share.ErrInvalidShare
	}
	resolvedRoot, err := filepath.EvalSymlinks(rootPath)
	if err != nil {
		return "", err
	}
	target := resolvedRoot
	if clean != "" {
		target = filepath.Join(resolvedRoot, filepath.FromSlash(clean))
	}
	resolvedTarget, err := filepath.EvalSymlinks(target)
	if err != nil {
		return "", err
	}
	if !pathWithinDirectory(resolvedRoot, resolvedTarget) {
		return "", share.ErrInvalidShare
	}
	return resolvedTarget, nil
}

func pathWithinDirectory(root, target string) bool {
	relative, err := filepath.Rel(root, target)
	if err != nil {
		return false
	}
	return relative == "." || (relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)))
}

func (s *ShareService) resolveFullPath(u *user.User, sharePath string) string {
	rel := strings.TrimPrefix(sharePath, "/")
	rel = filepath.FromSlash(rel)
	return filepath.Join(s.getUserRootDir(u), rel)
}

func (s *ShareService) getUserRootDir(u *user.User) string {
	userDir := u.Directory
	if userDir == "" {
		userDir = u.Username
	}
	if filepath.IsAbs(userDir) {
		return userDir
	}
	return filepath.Join(s.config.WebDAV.Directory, userDir)
}

func (s *ShareService) webdavPrefix() string {
	if s == nil || s.config == nil {
		return ""
	}
	return s.config.WebDAV.Prefix
}

func (s *ShareService) normalizeItemPath(raw string) (string, error) {
	return normalizeSharePath(raw, s.webdavPrefix())
}

func normalizeSharePath(raw string, prefix string) (string, error) {
	raw = stripWebdavPrefix(raw, prefix)
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("path is required")
	}
	clean := path.Clean("/" + strings.TrimLeft(raw, "/"))
	if clean == "/" || strings.HasPrefix(clean, "/..") {
		return "", fmt.Errorf("invalid path")
	}
	clean = strings.TrimSuffix(clean, "/")
	return clean, nil
}

func stripWebdavPrefix(rawPath string, prefix string) string {
	rawPath = strings.TrimSpace(rawPath)
	if rawPath == "" {
		return "/"
	}
	if strings.HasPrefix(rawPath, "http://") || strings.HasPrefix(rawPath, "https://") {
		if u, err := url.Parse(rawPath); err == nil && u.Path != "" {
			rawPath = u.Path
		}
	}
	if !strings.HasPrefix(rawPath, "/") {
		rawPath = "/" + rawPath
	}

	prefix = strings.TrimSpace(prefix)
	if prefix == "" || prefix == "/" {
		return rawPath
	}
	if !strings.HasPrefix(prefix, "/") {
		prefix = "/" + prefix
	}
	prefix = strings.TrimSuffix(prefix, "/")
	if rawPath == prefix {
		return "/"
	}
	if strings.HasPrefix(rawPath, prefix+"/") {
		trimmed := strings.TrimPrefix(rawPath, prefix)
		if trimmed == "" {
			return "/"
		}
		return trimmed
	}
	return rawPath
}
