package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/yeying-community/warehouse/internal/application/assetspace"
	"github.com/yeying-community/warehouse/internal/domain/auth"
	"github.com/yeying-community/warehouse/internal/domain/permission"
	"github.com/yeying-community/warehouse/internal/domain/quota"
	"github.com/yeying-community/warehouse/internal/domain/recycle"
	"github.com/yeying-community/warehouse/internal/domain/user"
	"github.com/yeying-community/warehouse/internal/infrastructure/config"
	"github.com/yeying-community/warehouse/internal/infrastructure/repository"
	webdavfs "github.com/yeying-community/warehouse/internal/infrastructure/webdav"
	"github.com/yeying-community/warehouse/internal/interface/http/middleware"
	"go.uber.org/zap"
	"golang.org/x/net/webdav"
)

// WebDAVService WebDAV 服务
type WebDAVService struct {
	config           *config.Config
	permissionCheck  permission.Checker
	quotaService     quota.Service
	userRepo         user.Repository
	recycleRepo      repository.RecycleRepository
	userShareRepo    repository.UserShareRepository
	publicShareRepo  repository.ShareRepository
	mutationRecorder MutationRecorder
	assetSpace       *assetspace.Manager
	logger           *zap.Logger
	lockSystem       webdav.LockSystem
	recycleDir       string // 回收站目录
}

func (s *WebDAVService) SetPublicShareRepository(repo repository.ShareRepository) {
	s.publicShareRepo = repo
}

type usedSpaceMutation struct {
	method         string
	targetPath     string
	sourcePath     string
	existingTarget int64
	copyDelta      int64
}

// statusRecorder 记录响应状态码
type statusRecorder struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func (r *statusRecorder) WriteHeader(code int) {
	if r.wroteHeader {
		return
	}
	r.status = code
	r.wroteHeader = true
	r.ResponseWriter.WriteHeader(code)
}

func (r *statusRecorder) Write(data []byte) (int, error) {
	if !r.wroteHeader {
		r.WriteHeader(r.status)
	}
	return r.ResponseWriter.Write(data)
}

// NewWebDAVService 创建 WebDAV 服务
func NewWebDAVService(
	cfg *config.Config,
	permissionCheck permission.Checker,
	quotaService quota.Service,
	userRepo user.Repository,
	recycleRepo repository.RecycleRepository,
	userShareRepo repository.UserShareRepository,
	mutationRecorder MutationRecorder,
	logger *zap.Logger,
) *WebDAVService {
	recycleDir := filepath.Join(cfg.WebDAV.Directory, ".recycle")
	if mutationRecorder == nil {
		mutationRecorder = noopMutationRecorder{}
	}
	return &WebDAVService{
		config:           cfg,
		permissionCheck:  permissionCheck,
		quotaService:     quotaService,
		userRepo:         userRepo,
		recycleRepo:      recycleRepo,
		userShareRepo:    userShareRepo,
		mutationRecorder: mutationRecorder,
		assetSpace:       assetspace.NewManager(cfg, logger),
		logger:           logger,
		lockSystem:       webdav.NewMemLS(),
		recycleDir:       recycleDir,
	}
}

// ServeHTTP 处理 WebDAV 请求
func (s *WebDAVService) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// 从上下文获取用户
	u, ok := middleware.GetUserFromContext(r.Context())
	if !ok {
		s.logger.Error("user not found in context")
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// WebDAV 可能包含大文件上传/下载。清空当前请求的连接 deadline，
	// 避免被全局 ReadTimeout/WriteTimeout（默认 30s）中途截断。
	s.clearWebDAVDeadlines(w)

	if isIgnoredWebDAVPath(r.URL.Path) {
		if r.Body != nil {
			_, _ = io.Copy(io.Discard, r.Body)
		}
		switch r.Method {
		case http.MethodGet, http.MethodHead, "PROPFIND":
			http.Error(w, "Not Found", http.StatusNotFound)
		default:
			w.WriteHeader(http.StatusNoContent)
		}
		return
	}

	// 获取用户目录
	userDir := s.getUserDirectory(u)
	s.logger.Debug("user directory", zap.String("username", u.Username), zap.String("directory", userDir))

	// 确保目录存在
	if err := s.ensureDirectory(userDir); err != nil {
		s.logger.Error("failed to ensure directory",
			zap.String("directory", userDir),
			zap.Error(err))
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	// 确保资产空间目录存在（personal + apps + services）
	if err := s.ensureAssetSpaces(userDir); err != nil {
		s.logger.Error("failed to ensure asset spaces",
			zap.String("directory", userDir),
			zap.Error(err))
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	// 规范化 MOVE/COPY 的 Destination 头，避免编码或代理导致的路径异常
	if r.Method == "MOVE" || r.Method == "COPY" {
		normalizeDestinationHeader(r)
	}

	// UCAN app scope 校验
	if err := s.checkAppScope(r.Context(), r); err != nil {
		s.logger.Warn("ucan app scope denied",
			zap.String("username", u.Username),
			zap.String("method", r.Method),
			zap.String("path", r.URL.Path),
			zap.String("destination", r.Header.Get("Destination")),
			zap.Error(err),
		)
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	// 检查权限
	if err := s.checkPermission(r.Context(), u, r); err != nil {
		s.logger.Warn("permission denied",
			zap.String("username", u.Username),
			zap.String("method", r.Method),
			zap.String("path", r.URL.Path),
			zap.Error(err))
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	// 对于上传操作，检查配额
	if isUploadMethod(r.Method) {
		if err := s.checkQuota(r.Context(), u, r); err != nil {
			s.logger.Warn("quota exceeded",
				zap.String("username", u.Username),
				zap.String("method", r.Method),
				zap.String("path", r.URL.Path),
				zap.Error(err))
			http.Error(w, "Insufficient Storage", http.StatusInsufficientStorage)
			return
		}
	}

	// 创建 WebDAV 处理器（使用自定义的 Unicode FileSystem）
	unicodeFS := webdavfs.NewUnicodeFileSystem(userDir)
	handler := &webdav.Handler{
		Prefix:     s.config.WebDAV.Prefix,
		FileSystem: unicodeFS,
		LockSystem: s.lockSystem,
		Logger:     s.createLogger(u.Username),
	}

	// 设置响应头
	if s.config.WebDAV.NoSniff {
		w.Header().Set("X-Content-Type-Options", "nosniff")
	}

	// 处理 DELETE 请求：将文件移动到回收站
	if r.Method == http.MethodDelete {
		s.handleDeleteWithRecycle(w, r, u, userDir, handler)
		return
	}

	if isMutatingMethod(r.Method) {
		mutation, err := s.prepareUsedSpaceMutation(u, userDir, r)
		if err != nil {
			s.logger.Error("failed to prepare used space mutation",
				zap.String("username", u.Username),
				zap.String("method", r.Method),
				zap.String("path", r.URL.Path),
				zap.Error(err))
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}

		rec := newBufferedStatusRecorder()
		handler.ServeHTTP(rec, r)

		if rec.status >= 200 && rec.status < 300 {
			if err := s.syncUserSharePathsForMove(r.Context(), u, userDir, r); err != nil {
				s.logger.Error("failed to sync share paths after move",
					zap.String("username", u.Username),
					zap.String("method", r.Method),
					zap.String("path", r.URL.Path),
					zap.String("destination", r.Header.Get("Destination")),
					zap.Error(err))
				http.Error(w, "Internal Server Error", http.StatusInternalServerError)
				return
			}
			if err := s.recordMutation(r.Context(), userDir, r); err != nil {
				if s.handleMutationRecordError("write mutation skipped because no standby is currently available",
					err,
					zap.String("username", u.Username),
					zap.String("method", r.Method),
					zap.String("path", r.URL.Path),
				) {
					http.Error(w, "Internal Server Error", http.StatusInternalServerError)
					return
				}
			}
			s.applyUsedSpaceMutation(r.Context(), u, mutation)
		}

		if err := rec.FlushTo(w); err != nil {
			s.logger.Error("failed to flush buffered webdav response", zap.Error(err))
		}
		return
	}

	rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
	handler.ServeHTTP(rec, r)

}

func (s *WebDAVService) syncUserSharePathsForMove(ctx context.Context, u *user.User, userDir string, r *http.Request) error {
	if (s.userShareRepo == nil && s.publicShareRepo == nil) || u == nil || r == nil || strings.ToUpper(strings.TrimSpace(r.Method)) != "MOVE" {
		return nil
	}
	destination := strings.TrimSpace(r.Header.Get("Destination"))
	if destination == "" {
		return nil
	}
	fromPath := s.resolveUserFullPath(userDir, r.URL.Path)
	toPath := s.resolveUserFullPath(userDir, destination)
	return SyncAllSharePathsForOwnerMove(ctx, s.userShareRepo, s.publicShareRepo, s.config, u, fromPath, toPath)
}

func (s *WebDAVService) clearWebDAVDeadlines(w http.ResponseWriter) {
	controller := http.NewResponseController(w)
	if err := controller.SetReadDeadline(time.Time{}); err != nil && !errors.Is(err, http.ErrNotSupported) {
		s.logger.Debug("failed to clear webdav read deadline", zap.Error(err))
	}
	if err := controller.SetWriteDeadline(time.Time{}); err != nil && !errors.Is(err, http.ErrNotSupported) {
		s.logger.Debug("failed to clear webdav write deadline", zap.Error(err))
	}
}

func isIgnoredWebDAVPath(rawPath string) bool {
	if rawPath == "" || rawPath == "/" {
		return false
	}
	cleaned := strings.TrimSuffix(rawPath, "/")
	base := path.Base(cleaned)
	return webdavfs.IsIgnoredName(base)
}

// handleDeleteWithRecycle 处理删除请求（带回收站功能）
func (s *WebDAVService) handleDeleteWithRecycle(w http.ResponseWriter, r *http.Request, u *user.User, userDir string, handler *webdav.Handler) {
	// 获取文件相对路径（剥离 WebDAV 前缀）
	normalizedPath := s.normalizeWebdavRequestPath(r.URL.Path)
	filePath := strings.TrimPrefix(normalizedPath, "/")

	// 获取文件的完整路径
	fullPath := filepath.Join(userDir, filePath)

	// 检查是否存在
	if _, err := os.Stat(fullPath); err != nil {
		if os.IsNotExist(err) {
			http.Error(w, "Not Found", http.StatusNotFound)
			return
		}
		s.logger.Error("failed to stat file", zap.Error(err))
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	info, err := os.Stat(fullPath)
	if err != nil {
		s.logger.Error("failed to stat file before recycle", zap.Error(err))
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	if shouldHardDeleteSyncArtifact(normalizedPath) {
		s.handleDirectDelete(w, r, u, fullPath, info.IsDir(), calculateFileSizeOrZero(info), handler)
		return
	}

	// 文件/目录移动到回收站目录
	moved, err := s.moveToRecycle(r.Context(), u, filePath, fullPath, info.IsDir())
	if err != nil {
		s.logger.Error("failed to move file to recycle", zap.Error(err))
		if moved {
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
		// 如果移动失败，直接删除
		rec := newBufferedStatusRecorder()
		handler.ServeHTTP(rec, r)
		if rec.status >= 200 && rec.status < 300 {
			if err := RemoveAllShareReferencesForOwnerPath(r.Context(), s.userShareRepo, s.publicShareRepo, s.config, u, fullPath); err != nil {
				http.Error(w, "Internal Server Error", http.StatusInternalServerError)
				return
			}
			if err := s.mutationRecorder.RemovePath(r.Context(), fullPath, info.IsDir()); err != nil {
				if s.handleMutationRecordError("fallback delete mutation skipped because no standby is currently available",
					err,
					zap.String("username", u.Username),
					zap.String("path", r.URL.Path),
					zap.Bool("is_dir", info.IsDir()),
				) {
					http.Error(w, "Internal Server Error", http.StatusInternalServerError)
					return
				}
			}
			s.applyUsedSpaceDelta(r.Context(), u, -calculateFileSizeOrZero(info))
		}
		if err := rec.FlushTo(w); err != nil {
			s.logger.Error("failed to flush delete fallback response", zap.Error(err))
		}
		return
	}
	if err := RemoveAllShareReferencesForOwnerPath(r.Context(), s.userShareRepo, s.publicShareRepo, s.config, u, fullPath); err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	// 返回成功
	w.WriteHeader(http.StatusOK)
}

func (s *WebDAVService) handleDirectDelete(
	w http.ResponseWriter,
	r *http.Request,
	u *user.User,
	fullPath string,
	isDir bool,
	sizeHint int64,
	handler *webdav.Handler,
) {
	sizeDelta := sizeHint
	if isDir {
		totalSize, err := calculatePathSize(fullPath)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			s.logger.Error("failed to calculate path size before hard delete",
				zap.String("path", fullPath),
				zap.Error(err))
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
		sizeDelta = totalSize
	}

	rec := newBufferedStatusRecorder()
	handler.ServeHTTP(rec, r)
	if rec.status >= 200 && rec.status < 300 {
		if err := RemoveAllShareReferencesForOwnerPath(r.Context(), s.userShareRepo, s.publicShareRepo, s.config, u, fullPath); err != nil {
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
		if err := s.mutationRecorder.RemovePath(r.Context(), fullPath, isDir); err != nil {
			if s.handleMutationRecordError("hard delete mutation skipped because no standby is currently available",
				err,
				zap.String("username", u.Username),
				zap.String("path", r.URL.Path),
				zap.Bool("is_dir", isDir),
			) {
				http.Error(w, "Internal Server Error", http.StatusInternalServerError)
				return
			}
		}
		s.applyUsedSpaceDelta(r.Context(), u, -sizeDelta)
	}
	if err := rec.FlushTo(w); err != nil {
		s.logger.Error("failed to flush hard delete response", zap.Error(err))
	}
}

func isEphemeralSyncArtifactPath(normalizedPath string) bool {
	cleaned := path.Clean("/" + strings.TrimSpace(normalizedPath))
	if cleaned == "/" {
		return false
	}
	parts := strings.Split(strings.TrimPrefix(cleaned, "/"), "/")
	if len(parts) < 2 {
		return false
	}
	appsIndex := -1
	for i, part := range parts {
		if part == "apps" {
			appsIndex = i
			break
		}
	}
	if appsIndex < 0 || len(parts) <= appsIndex+1 {
		return false
	}

	base := path.Base(cleaned)
	if base == "backup.__sync_txn_head_v1.json" ||
		base == "backup.__sync_txn_head_v1_bak.json" {
		return true
	}
	if strings.HasPrefix(base, "backup.__sync_txn_data_v1.") &&
		strings.HasSuffix(base, ".json") {
		return true
	}

	if base == "lock.json" {
		parent := path.Base(path.Dir(cleaned))
		return parent == "backup.__sync_mutex_v1.__sync_lock_v1"
	}

	return base == "backup.__sync_mutex_v1.__sync_lock_v1"
}

func shouldHardDeleteSyncArtifact(normalizedPath string) bool {
	return isEphemeralSyncArtifactPath(normalizedPath)
}

// IsEphemeralSyncArtifactPath reports whether a logical WebDAV path is an
// internal sync artifact that should not enter user-facing lifecycle flows.
func IsEphemeralSyncArtifactPath(normalizedPath string) bool {
	return isEphemeralSyncArtifactPath(normalizedPath)
}

// moveToRecycle 将文件移动到回收站并保存记录
func (s *WebDAVService) moveToRecycle(ctx context.Context, u *user.User, relativePath, fullPath string, isDir bool) (bool, error) {
	// 获取文件信息
	info, err := os.Stat(fullPath)
	if err != nil {
		return false, fmt.Errorf("failed to stat file: %w", err)
	}
	fileSize := info.Size()
	if info.IsDir() {
		fileSize = 0
	}

	// 确保回收站目录存在
	if err := os.MkdirAll(s.recycleDir, 0755); err != nil {
		return false, fmt.Errorf("failed to create recycle dir: %w", err)
	}

	// 获取文件名和目录
	cleanRelative := strings.TrimSuffix(relativePath, "/")
	cleanRelative = filepath.Clean(filepath.FromSlash(cleanRelative))
	if cleanRelative == "." {
		cleanRelative = ""
	}
	fileName := filepath.Base(cleanRelative)
	dirName := filepath.Dir(cleanRelative)
	if dirName == "." {
		dirName = u.Directory
		if dirName == "" {
			dirName = u.Username
		}
	}

	// 创建回收站记录（先生成 hash，便于文件命名）
	item := recycle.NewRecycleItem(u.ID, u.Username, dirName, fileName, cleanRelative, isDir, fileSize)

	// 生成唯一的回收站文件名：{hash}_{原文件名}
	recycleFileName := fmt.Sprintf("%s_%s", item.Hash, fileName)
	recyclePath := filepath.Join(s.recycleDir, recycleFileName)

	// 移动文件
	if err := os.Rename(fullPath, recyclePath); err != nil {
		return false, fmt.Errorf("failed to move file to recycle: %w", err)
	}

	// 创建回收站记录并保存到数据库
	if err := s.recycleRepo.Create(ctx, item); err != nil {
		s.logger.Error("failed to save recycle item", zap.Error(err))
		// 不返回错误，因为文件已经移动了
	}
	if err := s.mutationRecorder.EnsureDir(ctx, s.recycleDir); err != nil {
		if s.handleMutationRecordError("recycle directory replication ensure skipped because no standby is currently available",
			err,
			zap.String("username", u.Username),
			zap.String("recycle_dir", s.recycleDir),
		) {
			return true, fmt.Errorf("record recycle directory ensure event: %w", err)
		}
	}
	if err := s.mutationRecorder.MovePath(ctx, fullPath, recyclePath, isDir); err != nil {
		if s.handleMutationRecordError("recycle move replication event skipped because no standby is currently available",
			err,
			zap.String("username", u.Username),
			zap.String("from_path", fullPath),
			zap.String("to_path", recyclePath),
			zap.Bool("is_dir", isDir),
		) {
			return true, fmt.Errorf("record recycle move event: %w", err)
		}
	}

	s.logger.Info("file moved to recycle",
		zap.String("username", u.Username),
		zap.String("original_path", relativePath),
		zap.String("recycle_path", recyclePath),
		zap.String("hash", item.Hash),
	)

	return true, nil
}

// isUploadMethod 判断是否为上传方法
func isUploadMethod(method string) bool {
	return method == "PUT" || method == "POST" || method == "MKCOL" || method == "COPY"
}

// isMutatingMethod 判断是否为可能改变存储的 WebDAV 方法
func isMutatingMethod(method string) bool {
	switch method {
	case "PUT", "POST", "MKCOL", "DELETE", "MOVE", "COPY":
		return true
	default:
		return false
	}
}

// checkQuota 检查配额
func (s *WebDAVService) checkQuota(ctx context.Context, u *user.User, r *http.Request) error {
	// 如果用户没有配额限制，跳过检查
	if u.Quota <= 0 {
		return nil
	}

	additionalSize, err := s.estimateQuotaAdditionalSize(u, r)
	if err != nil {
		return err
	}

	// 检查是否超过配额
	if err := s.quotaService.CheckQuota(ctx, u, additionalSize); err != nil {
		return err
	}

	return nil
}

func (s *WebDAVService) estimateQuotaAdditionalSize(u *user.User, r *http.Request) (int64, error) {
	switch r.Method {
	case "MKCOL":
		return 0, nil
	case "PUT", "POST":
		newSize, err := s.readRequestBodySize(r)
		if err != nil {
			return 0, err
		}

		userDir := s.getUserDirectory(u)
		targetPath := s.resolveUserFullPath(userDir, r.URL.Path)
		oldSize, err := getExistingFileSize(targetPath)
		if err != nil {
			return 0, err
		}

		if newSize <= oldSize {
			return 0, nil
		}
		return newSize - oldSize, nil
	case "COPY":
		userDir := s.getUserDirectory(u)
		sourcePath := s.resolveUserFullPath(userDir, r.URL.Path)
		destination := strings.TrimSpace(r.Header.Get("Destination"))
		if destination == "" {
			return 0, fmt.Errorf("missing Destination header for COPY")
		}
		targetPath := s.resolveUserFullPath(userDir, destination)
		return estimateCopyQuotaDelta(sourcePath, targetPath)
	default:
		return 0, nil
	}
}

func (s *WebDAVService) readRequestBodySize(r *http.Request) (int64, error) {
	var fileSize int64

	// 从 Content-Length 头获取大小
	if contentLength := r.Header.Get("Content-Length"); contentLength != "" {
		size, err := strconv.ParseInt(contentLength, 10, 64)
		if err == nil {
			fileSize = size
		}
	}

	// 如果没有 Content-Length，尝试读取 body
	if fileSize == 0 && r.Body != nil {
		// 注意：这会消耗 body，需要重新设置
		body, err := io.ReadAll(r.Body)
		if err != nil {
			return 0, fmt.Errorf("failed to read body: %w", err)
		}
		fileSize = int64(len(body))
		// 重新设置 body
		r.Body = io.NopCloser(io.NewSectionReader(
			io.NewSectionReader(
				&bodyReader{data: body},
				0,
				int64(len(body)),
			),
			0,
			int64(len(body)),
		))
	}

	return fileSize, nil
}

func getExistingFileSize(targetPath string) (int64, error) {
	info, err := os.Stat(targetPath)
	if err == nil {
		if info.IsDir() {
			return 0, nil
		}
		return info.Size(), nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	return 0, fmt.Errorf("stat existing target: %w", err)
}

func calculateFileSizeOrZero(info os.FileInfo) int64 {
	if info == nil || info.IsDir() {
		return 0
	}
	return info.Size()
}

func getExistingPathSize(targetPath string) (int64, error) {
	size, err := calculatePathSize(targetPath)
	if err == nil {
		return size, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	return 0, fmt.Errorf("stat existing path: %w", err)
}

func calculatePathSize(targetPath string) (int64, error) {
	info, err := os.Stat(targetPath)
	if err != nil {
		return 0, err
	}
	if !info.IsDir() {
		return info.Size(), nil
	}

	var totalSize int64
	err = filepath.Walk(targetPath, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == targetPath {
			return nil
		}
		if info == nil || info.IsDir() {
			return nil
		}
		totalSize += info.Size()
		return nil
	})
	if err != nil {
		return 0, err
	}
	return totalSize, nil
}

func estimateCopyQuotaDelta(sourcePath, targetPath string) (int64, error) {
	sourceInfo, err := os.Stat(sourcePath)
	if err != nil {
		return 0, err
	}
	if !sourceInfo.IsDir() {
		targetSize, err := getExistingFileSize(targetPath)
		if err != nil {
			return 0, err
		}
		if sourceInfo.Size() <= targetSize {
			return 0, nil
		}
		return sourceInfo.Size() - targetSize, nil
	}

	if targetInfo, err := os.Stat(targetPath); err == nil && !targetInfo.IsDir() {
		sourceSize, err := calculatePathSize(sourcePath)
		if err != nil {
			return 0, err
		}
		if sourceSize <= targetInfo.Size() {
			return 0, nil
		}
		return sourceSize - targetInfo.Size(), nil
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return 0, fmt.Errorf("stat copy target: %w", err)
	}

	var delta int64
	err = filepath.WalkDir(sourcePath, func(current string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if current == sourcePath || entry.IsDir() {
			return nil
		}
		sourceFileInfo, err := entry.Info()
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(sourcePath, current)
		if err != nil {
			return err
		}
		targetFile := filepath.Join(targetPath, rel)
		targetSize, err := getExistingFileSize(targetFile)
		if err != nil {
			return err
		}
		if sourceFileInfo.Size() > targetSize {
			delta += sourceFileInfo.Size() - targetSize
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return delta, nil
}

// bodyReader 用于重新读取 body
type bodyReader struct {
	data []byte
	pos  int
}

func (b *bodyReader) Read(p []byte) (n int, err error) {
	if b.pos >= len(b.data) {
		return 0, io.EOF
	}
	n = copy(p, b.data[b.pos:])
	b.pos += n
	return n, nil
}

func (b *bodyReader) ReadAt(p []byte, off int64) (n int, err error) {
	if off >= int64(len(b.data)) {
		return 0, io.EOF
	}
	n = copy(p, b.data[off:])
	if n < len(p) {
		err = io.EOF
	}
	return n, err
}

// normalizeDestinationHeader 规范化 Destination 头，处理编码和代理前缀差异
func normalizeDestinationHeader(r *http.Request) {
	dest := r.Header.Get("Destination")
	if dest == "" {
		return
	}

	u, err := url.Parse(dest)
	if err != nil {
		return
	}

	if u.Path == "" {
		return
	}

	if decoded, err := url.PathUnescape(u.Path); err == nil {
		u.Path = decoded
	}

	// 强制使用路径形式，避免代理导致的 host 不匹配
	path := "/" + strings.TrimLeft(u.Path, "/")
	r.Header.Set("Destination", path)
}

// createLogger 创建 WebDAV 日志记录器
func (s *WebDAVService) createLogger(username string) func(*http.Request, error) {
	return func(r *http.Request, err error) {
		if err == nil {
			return
		}

		// 分类错误
		fields := []zap.Field{
			zap.String("username", username),
			zap.String("method", r.Method),
			zap.String("path", r.URL.Path),
		}

		// 判断错误类型
		if isNoSuchLockError(err) {
			// Finder/客户端常见的无锁解锁请求，降级为 DEBUG
			s.logger.Debug("webdav lock not found",
				append(fields, zap.String("error", err.Error()))...)
			return
		}
		if isNotFoundError(err) {
			// 文件不存在 - WARN 级别，不打印堆栈
			s.logger.Warn("resource not found",
				append(fields, zap.String("error", err.Error()))...)
		} else if isPermissionError(err) {
			// 权限错误 - WARN 级别
			s.logger.Warn("permission denied",
				append(fields, zap.String("error", err.Error()))...)
		} else if isExistsError(err) {
			// 文件已存在 - WARN 级别
			s.logger.Warn("resource already exists",
				append(fields, zap.String("error", err.Error()))...)
		} else if isClientError(err) {
			// 客户端错误 - INFO 级别
			s.logger.Info("client error",
				append(fields, zap.String("error", err.Error()))...)
		} else {
			// 系统错误 - ERROR 级别，打印堆栈
			s.logger.Error("webdav error", append(fields, zap.Error(err))...)
		}
	}
}

// isNotFoundError 判断是否为文件不存在错误
func isNotFoundError(err error) bool {
	if err == nil {
		return false
	}

	// 检查是否为 os.ErrNotExist
	if errors.Is(err, os.ErrNotExist) {
		return true
	}

	// 检查错误消息
	errMsg := err.Error()
	return contains(errMsg, "no such file") ||
		contains(errMsg, "not found") ||
		contains(errMsg, "does not exist")
}

// isNoSuchLockError 判断是否为不存在的锁错误
func isNoSuchLockError(err error) bool {
	if err == nil {
		return false
	}
	return contains(err.Error(), "no such lock")
}

// isPermissionError 判断是否为权限错误
func isPermissionError(err error) bool {
	if err == nil {
		return false
	}

	// 检查是否为 os.ErrPermission
	if errors.Is(err, os.ErrPermission) {
		return true
	}

	// 检查错误消息
	errMsg := err.Error()
	return contains(errMsg, "permission denied") ||
		contains(errMsg, "access denied") ||
		contains(errMsg, "forbidden")
}

// isClientError 判断是否为客户端错误
func isClientError(err error) bool {
	if err == nil {
		return false
	}

	errMsg := err.Error()
	return contains(errMsg, "invalid") ||
		contains(errMsg, "bad request") ||
		contains(errMsg, "malformed")
}

// isExistsError 判断是否为文件/目录已存在错误
func isExistsError(err error) bool {
	if err == nil {
		return false
	}

	// 检查是否为 os.ErrExist
	if errors.Is(err, os.ErrExist) {
		return true
	}

	// 检查错误消息
	errMsg := err.Error()
	return contains(errMsg, "file exists") ||
		contains(errMsg, "already exists") ||
		contains(errMsg, "cannot create")
}

// contains 检查字符串是否包含子串（不区分大小写）
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 || containsCaseInsensitive(s, substr))
}

// containsCaseInsensitive 不区分大小写的字符串包含检查
func containsCaseInsensitive(s, substr string) bool {
	s = toLower(s)
	substr = toLower(substr)
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// toLower 转换为小写
func toLower(s string) string {
	b := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if 'A' <= c && c <= 'Z' {
			c += 'a' - 'A'
		}
		b[i] = c
	}
	return string(b)
}

// getUserDirectory 获取用户目录
func (s *WebDAVService) getUserDirectory(u *user.User) string {
	// 如果用户有自定义目录，使用用户目录
	if u.Directory != "" {
		// 如果是绝对路径，直接使用
		if filepath.IsAbs(u.Directory) {
			return u.Directory
		}
		// 否则拼接到基础目录
		return filepath.Join(s.config.WebDAV.Directory, u.Directory)
	}

	// 使用基础目录
	return s.config.WebDAV.Directory
}

// ensureDirectory 确保目录存在
func (s *WebDAVService) ensureDirectory(dir string) error {
	info, err := os.Stat(dir)
	if err != nil {
		if os.IsNotExist(err) {
			// 创建目录
			if err := os.MkdirAll(dir, 0755); err != nil {
				return fmt.Errorf("failed to create directory: %w", err)
			}
			s.logger.Info("directory created", zap.String("directory", dir))
			return nil
		}
		return fmt.Errorf("failed to stat directory: %w", err)
	}

	if !info.IsDir() {
		return fmt.Errorf("path is not a directory: %s", dir)
	}

	return nil
}

func (s *WebDAVService) ensureAssetSpaces(userDir string) error {
	if s == nil || s.assetSpace == nil {
		return nil
	}
	return s.assetSpace.EnsureForUserDirectory(userDir)
}

func (s *WebDAVService) recordMutation(ctx context.Context, userDir string, r *http.Request) error {
	fullPath := s.resolveUserFullPath(userDir, r.URL.Path)

	switch r.Method {
	case "MKCOL":
		return s.mutationRecorder.EnsureDir(ctx, fullPath)
	case "PUT", "POST":
		info, err := os.Stat(fullPath)
		if err != nil {
			return fmt.Errorf("stat mutated path: %w", err)
		}
		if info.IsDir() {
			return s.mutationRecorder.EnsureDir(ctx, fullPath)
		}
		if err := s.mutationRecorder.EnsureDir(ctx, filepath.Dir(fullPath)); err != nil {
			return err
		}
		return s.mutationRecorder.UpsertFile(ctx, fullPath)
	case "MOVE":
		destination := strings.TrimSpace(r.Header.Get("Destination"))
		if destination == "" {
			return fmt.Errorf("missing Destination header for MOVE")
		}
		toPath := s.resolveUserFullPath(userDir, destination)
		info, err := os.Stat(toPath)
		if err != nil {
			return fmt.Errorf("stat destination after MOVE: %w", err)
		}
		if err := s.mutationRecorder.EnsureDir(ctx, filepath.Dir(toPath)); err != nil {
			return err
		}
		return s.mutationRecorder.MovePath(ctx, fullPath, toPath, info.IsDir())
	case "COPY":
		destination := strings.TrimSpace(r.Header.Get("Destination"))
		if destination == "" {
			return fmt.Errorf("missing Destination header for COPY")
		}
		toPath := s.resolveUserFullPath(userDir, destination)
		info, err := os.Stat(toPath)
		if err != nil {
			return fmt.Errorf("stat destination after COPY: %w", err)
		}
		if err := s.mutationRecorder.EnsureDir(ctx, filepath.Dir(toPath)); err != nil {
			return err
		}
		return s.mutationRecorder.CopyPath(ctx, fullPath, toPath, info.IsDir())
	default:
		return nil
	}
}

func (s *WebDAVService) resolveUserFullPath(userDir, rawPath string) string {
	normalizedPath := s.normalizeWebdavRequestPath(rawPath)
	relativePath := strings.TrimPrefix(normalizedPath, "/")
	return filepath.Join(userDir, filepath.FromSlash(relativePath))
}

func (s *WebDAVService) prepareUsedSpaceMutation(u *user.User, userDir string, r *http.Request) (*usedSpaceMutation, error) {
	mutation := &usedSpaceMutation{
		method: r.Method,
	}

	switch r.Method {
	case "PUT", "POST":
		mutation.targetPath = s.resolveUserFullPath(userDir, r.URL.Path)
		size, err := getExistingPathSize(mutation.targetPath)
		if err != nil {
			return nil, err
		}
		mutation.existingTarget = size
	case "COPY":
		mutation.sourcePath = s.resolveUserFullPath(userDir, r.URL.Path)
		destination := strings.TrimSpace(r.Header.Get("Destination"))
		if destination == "" {
			return nil, fmt.Errorf("missing Destination header for COPY")
		}
		mutation.targetPath = s.resolveUserFullPath(userDir, destination)
		delta, err := estimateCopyQuotaDelta(mutation.sourcePath, mutation.targetPath)
		if err != nil {
			return nil, err
		}
		mutation.copyDelta = delta
	}

	return mutation, nil
}

func (s *WebDAVService) applyUsedSpaceMutation(ctx context.Context, u *user.User, mutation *usedSpaceMutation) {
	if mutation == nil {
		return
	}

	var delta int64
	var err error

	switch mutation.method {
	case "PUT", "POST":
		newSize, calcErr := getExistingPathSize(mutation.targetPath)
		if calcErr != nil {
			err = calcErr
			break
		}
		delta = newSize - mutation.existingTarget
	case "COPY":
		delta = mutation.copyDelta
	default:
		return
	}

	if err != nil {
		s.logger.Error("failed to calculate used space delta",
			zap.String("username", u.Username),
			zap.String("method", mutation.method),
			zap.String("target_path", mutation.targetPath),
			zap.Error(err))
		return
	}

	s.applyUsedSpaceDelta(ctx, u, delta)
}

func (s *WebDAVService) applyUsedSpaceDelta(ctx context.Context, u *user.User, delta int64) {
	if u == nil || delta == 0 {
		return
	}

	used, err := s.userRepo.UpdateUsedSpaceDelta(ctx, u.Username, delta)
	if err != nil {
		s.logger.Error("failed to update used space delta",
			zap.String("username", u.Username),
			zap.Int64("delta", delta),
			zap.Error(err))
		return
	}
	if err := u.UpdateUsedSpace(used); err != nil {
		s.logger.Error("failed to update in-memory used space",
			zap.String("username", u.Username),
			zap.Int64("used_space", used),
			zap.Error(err))
		return
	}
	s.logger.Debug("used space delta applied",
		zap.String("username", u.Username),
		zap.Int64("delta", delta),
		zap.Int64("used_space", used))
}

func (s *WebDAVService) handleMutationRecordError(message string, err error, fields ...zap.Field) bool {
	if err == nil {
		return false
	}

	if isReplicationPeerUnavailable(err) {
		if s != nil && s.logger != nil {
			fields = append(fields, zap.Error(err))
			s.logger.Warn(message, fields...)
		}
		return false
	}

	if s != nil && s.logger != nil {
		fields = append(fields, zap.Error(err))
		s.logger.Error("failed to record replication mutation", fields...)
	}
	return true
}

// checkPermission 检查权限
func (s *WebDAVService) checkPermission(ctx context.Context, u *user.User, r *http.Request) error {
	// 映射 HTTP 方法到操作。PUT 需要区分“新建文件”与“覆盖已有文件”：
	// - 目标不存在：按 Create(C) 校验，允许仅有上传权限的访问密钥写入新文件
	// - 目标已存在：按 Write(U) 校验，避免仅有上传权限覆盖已有文件
	operation := s.resolvePermissionOperation(u, r)

	// 拼接用户目录和请求路径，得到相对于 webdav 根目录的完整路径
	// 例如：用户目录是 "BraveWolf44"，请求路径是 "/test/icon16.png"
	// 需要检查的是 "BraveWolf44/test/icon16.png"
	userDir := u.Directory
	if userDir == "" {
		userDir = u.Username
	}
	normalizedPath := s.normalizeWebdavRequestPath(r.URL.Path)
	fullPath := filepath.Join(userDir, strings.TrimPrefix(normalizedPath, "/"))

	// 检查权限
	if err := s.permissionCheck.Check(ctx, u, fullPath, operation); err != nil {
		return err
	}

	// MOVE/COPY 还需要检查目标路径权限，避免绕过目录级权限边界。
	if r.Method == "MOVE" || r.Method == "COPY" {
		destination := strings.TrimSpace(r.Header.Get("Destination"))
		if destination != "" {
			destPath := s.normalizeWebdavRequestPath(destination)
			destFullPath := filepath.Join(userDir, strings.TrimPrefix(destPath, "/"))
			if err := s.permissionCheck.Check(ctx, u, destFullPath, operation); err != nil {
				return err
			}
		}
	}

	return nil
}

func (s *WebDAVService) resolvePermissionOperation(u *user.User, r *http.Request) permission.Operation {
	method := strings.ToUpper(strings.TrimSpace(r.Method))
	if method != http.MethodPut {
		return permission.MapHTTPMethodToOperation(method)
	}

	userDir := s.getUserDirectory(u)
	fullPath := s.resolveUserFullPath(userDir, r.URL.Path)
	if _, err := os.Stat(fullPath); err == nil {
		return permission.OperationWrite
	} else if os.IsNotExist(err) {
		return permission.OperationCreate
	}

	// 读取目标状态失败时，回退到旧行为，避免因为一次 stat 异常放宽权限。
	return permission.OperationWrite
}

func (s *WebDAVService) checkAppScope(ctx context.Context, r *http.Request) error {
	scope, err := resolveAppScope(ctx, s.config)
	if err != nil {
		return err
	}
	if !scope.active {
		return nil
	}

	sourcePath := s.normalizeWebdavRequestPath(r.URL.Path)
	if isAppScopeRootPath(sourcePath, scope.prefix) {
		if isAppScopeRootMethod(r.Method) {
			return nil
		}
		return auth.ErrAppScopeDenied
	}
	actions := requiredActionsForWebdavMethod(r.Method)
	if !scope.allowsAny(sourcePath, actions...) {
		return auth.ErrAppScopeDenied
	}

	if r.Method == "MOVE" || r.Method == "COPY" {
		dest := strings.TrimSpace(r.Header.Get("Destination"))
		if dest != "" {
			destPath := s.normalizeWebdavRequestPath(dest)
			if isAppScopeRootPath(destPath, scope.prefix) {
				return auth.ErrAppScopeDenied
			}
			if !scope.allowsAny(destPath, actions...) {
				return auth.ErrAppScopeDenied
			}
		}
	}

	return nil
}

func isAppScopeRootPath(rawPath, prefix string) bool {
	normalizedPath := normalizeScopePath(rawPath)
	normalizedPrefix := strings.TrimSuffix(normalizeScopePrefix(prefix), "/")
	return normalizedPath == normalizedPrefix
}

func isAppScopeRootMethod(method string) bool {
	switch strings.ToUpper(method) {
	case "GET", "HEAD", "OPTIONS", "PROPFIND", "REPORT", "SEARCH", "MKCOL":
		return true
	default:
		return false
	}
}

func requiredActionsForWebdavMethod(method string) []string {
	switch strings.ToUpper(method) {
	case "GET", "HEAD", "OPTIONS", "PROPFIND", "REPORT", "SEARCH":
		return []string{"read"}
	case "MKCOL", "POST":
		return []string{"create"}
	case "PUT":
		return []string{"update", "create"}
	case "PATCH", "PROPPATCH", "LOCK", "UNLOCK":
		return []string{"update"}
	case "DELETE":
		return []string{"delete"}
	case "MOVE":
		return []string{"move"}
	case "COPY":
		return []string{"copy"}
	default:
		op := permission.MapHTTPMethodToOperation(method)
		if op == permission.OperationRead {
			return []string{"read"}
		}
		return []string{"write"}
	}
}

func (s *WebDAVService) normalizeWebdavRequestPath(rawPath string) string {
	rawPath = strings.TrimSpace(rawPath)
	if rawPath == "" {
		return "/"
	}
	if strings.HasPrefix(rawPath, "http://") || strings.HasPrefix(rawPath, "https://") {
		if u, err := url.Parse(rawPath); err == nil && u.Path != "" {
			rawPath = u.Path
		}
	}

	prefix := strings.TrimSpace(s.config.WebDAV.Prefix)
	if prefix == "" || prefix == "/" {
		return rawPath
	}
	if !strings.HasPrefix(prefix, "/") {
		prefix = "/" + prefix
	}
	prefix = strings.TrimSuffix(prefix, "/")
	if prefix != "" && prefix != "/" && strings.HasPrefix(rawPath, prefix) {
		rawPath = strings.TrimPrefix(rawPath, prefix)
		if rawPath == "" {
			rawPath = "/"
		}
	}
	return rawPath
}
