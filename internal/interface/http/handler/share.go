package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/yeying-community/warehouse/internal/application/service"
	"github.com/yeying-community/warehouse/internal/domain/auth"
	"github.com/yeying-community/warehouse/internal/domain/share"
	"github.com/yeying-community/warehouse/internal/interface/http/middleware"
	"go.uber.org/zap"
)

// ShareHandler 文件分享处理器
type ShareHandler struct {
	shareService *service.ShareService
	logger       *zap.Logger
}

// NewShareHandler 创建分享处理器
func NewShareHandler(shareService *service.ShareService, logger *zap.Logger) *ShareHandler {
	return &ShareHandler{
		shareService: shareService,
		logger:       logger,
	}
}

// HandleCreate 创建分享链接
func (h *ShareHandler) HandleCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	u, ok := middleware.GetUserFromContext(r.Context())
	if !ok {
		h.logger.Error("user not found in context")
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	var req struct {
		Path         string `json:"path"`
		Mode         string `json:"mode"`
		ExpiresIn    int64  `json:"expiresIn"`
		ExpiresValue int64  `json:"expiresValue"`
		ExpiresUnit  string `json:"expiresUnit"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.logger.Warn("invalid request body", zap.Error(err))
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.Path) == "" {
		http.Error(w, "path is required", http.StatusBadRequest)
		return
	}

	item, err := h.shareService.Create(r.Context(), u, req.Path, service.ShareCreateInput{
		Mode: req.Mode,
		Expiry: service.ShareExpiryInput{
			ExpiresIn:    req.ExpiresIn,
			ExpiresValue: req.ExpiresValue,
			ExpiresUnit:  req.ExpiresUnit,
		},
	})
	if err != nil {
		if errors.Is(err, auth.ErrAppScopeDenied) || errors.Is(err, auth.ErrAppScopeRequired) {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
		h.logger.Error("failed to create share",
			zap.String("username", u.Username),
			zap.String("path", req.Path),
			zap.Error(err))
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	h.writeShareCreateResponse(w, r, item)
}

func (h *ShareHandler) writeShareCreateResponse(w http.ResponseWriter, r *http.Request, item *share.ShareItem) {
	resp := map[string]any{
		"token":         item.Token,
		"name":          item.Name,
		"path":          item.Path,
		"isDir":         item.IsDir,
		"mode":          item.Mode,
		"url":           h.buildShareURL(r, item.Token, item.Name, item.IsDir),
		"viewCount":     item.ViewCount,
		"downloadCount": item.DownloadCount,
	}
	if item.ExpiresAt != nil {
		resp["expiresAt"] = item.ExpiresAt.Format(timeLayout)
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		h.logger.Error("failed to encode response", zap.Error(err))
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

func (h *ShareHandler) HandleCreateFromReceivedResource(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	u, ok := middleware.GetUserFromContext(r.Context())
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	var req struct {
		ResourceID   string `json:"resourceId"`
		RelativePath string `json:"relativePath"`
		Mode         string `json:"mode"`
		ExpiresValue int64  `json:"expiresValue"`
		ExpiresUnit  string `json:"expiresUnit"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.ResourceID) == "" {
		http.Error(w, "resourceId is required", http.StatusBadRequest)
		return
	}
	item, err := h.shareService.CreateFromReceivedResource(r.Context(), u, req.ResourceID, req.RelativePath, service.ShareCreateInput{Mode: req.Mode, Expiry: service.ShareExpiryInput{ExpiresValue: req.ExpiresValue, ExpiresUnit: req.ExpiresUnit}})
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	h.writeShareCreateResponse(w, r, item)
}

// HandleList 获取分享列表
func (h *ShareHandler) HandleList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	u, ok := middleware.GetUserFromContext(r.Context())
	if !ok {
		h.logger.Error("user not found in context")
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	items, err := h.shareService.List(r.Context(), u)
	if err != nil {
		if errors.Is(err, auth.ErrAppScopeDenied) || errors.Is(err, auth.ErrAppScopeRequired) {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
		h.logger.Error("failed to list share items",
			zap.String("username", u.Username),
			zap.Error(err))
		http.Error(w, "Failed to list share items", http.StatusInternalServerError)
		return
	}

	type itemResp struct {
		Token         string `json:"token"`
		Name          string `json:"name"`
		Path          string `json:"path"`
		Mode          string `json:"mode"`
		URL           string `json:"url"`
		ViewCount     int64  `json:"viewCount"`
		DownloadCount int64  `json:"downloadCount"`
		ExpiresAt     string `json:"expiresAt,omitempty"`
		CreatedAt     string `json:"createdAt"`
		IsDir         bool   `json:"isDir"`
	}

	resp := struct {
		Items []itemResp `json:"items"`
	}{
		Items: make([]itemResp, 0, len(items)),
	}

	for _, item := range items {
		rsp := itemResp{
			Token:         item.Token,
			Name:          item.Name,
			Path:          item.Path,
			IsDir:         item.IsDir,
			Mode:          item.Mode,
			URL:           h.buildShareURL(r, item.Token, item.Name, item.IsDir),
			ViewCount:     item.ViewCount,
			DownloadCount: item.DownloadCount,
			CreatedAt:     item.CreatedAt.Format(timeLayout),
		}
		if item.ExpiresAt != nil {
			rsp.ExpiresAt = item.ExpiresAt.Format(timeLayout)
		}
		resp.Items = append(resp.Items, rsp)
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		h.logger.Error("failed to encode response", zap.Error(err))
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

// HandleRevoke 取消分享
func (h *ShareHandler) HandleRevoke(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	u, ok := middleware.GetUserFromContext(r.Context())
	if !ok {
		h.logger.Error("user not found in context")
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	var req struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.logger.Warn("invalid request body", zap.Error(err))
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.Token) == "" {
		http.Error(w, "token is required", http.StatusBadRequest)
		return
	}

	if err := h.shareService.Revoke(r.Context(), u, req.Token); err != nil {
		if errors.Is(err, auth.ErrAppScopeDenied) || errors.Is(err, auth.ErrAppScopeRequired) {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
		h.logger.Error("failed to revoke share",
			zap.String("username", u.Username),
			zap.String("token", req.Token),
			zap.Error(err))
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.WriteHeader(http.StatusOK)
	if _, err := w.Write([]byte(`{"message":"revoked successfully"}`)); err != nil {
		h.logger.Error("failed to write response", zap.Error(err))
	}
}

// HandleAccess 访问分享链接（公开）
func (h *ShareHandler) HandleAccess(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	token, hasFilename := parseShareAccessPath(r.URL.Path)
	if token == "" {
		http.NotFound(w, r)
		return
	}

	metadata, err := h.shareService.ResolveMetadata(r.Context(), token)
	if err != nil {
		h.writePublicShareError(w, r, err)
		return
	}
	if metadata.IsDir {
		if hasFilename {
			http.NotFound(w, r)
			return
		}
		http.Redirect(w, r, "/share/"+url.PathEscape(token), http.StatusFound)
		return
	}
	item, file, info, err := h.shareService.Resolve(r.Context(), token)
	if err != nil {
		h.writePublicShareError(w, r, err)
		return
	}
	defer file.Close()

	if !hasFilename {
		location := h.buildShareURL(r, item.Token, item.Name, item.IsDir)
		if r.URL.RawQuery != "" {
			location += "?" + r.URL.RawQuery
		}
		http.Redirect(w, r, location, http.StatusFound)
		return
	}

	if shouldCountAccess(r) {
		_ = h.shareService.IncrementView(r.Context(), token)
		if r.Method == http.MethodGet && !item.IsPreviewMode() {
			_ = h.shareService.IncrementDownload(r.Context(), token)
		}
	}

	if item.IsPreviewMode() {
		setInlineContentDisposition(w, item.Name)
	} else {
		setAttachmentContentDisposition(w, item.Name)
	}

	http.ServeContent(w, r, item.Name, info.ModTime(), file)
}

func (h *ShareHandler) HandleDirectoryAccess(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	trimmed := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/v1/public/share/directory/"), "/")
	parts := strings.SplitN(trimmed, "/", 2)
	if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" {
		http.NotFound(w, r)
		return
	}
	token, action := parts[0], parts[1]
	w.Header().Set("Cache-Control", "no-store")
	switch action {
	case "entries":
		offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		item, entries, hasMore, err := h.shareService.ResolvePublicDirectory(r.Context(), token, r.URL.Query().Get("path"), offset, limit)
		if err != nil {
			h.writePublicShareError(w, r, err)
			return
		}
		if r.Method == http.MethodGet && r.URL.Query().Get("path") == "" && offset == 0 {
			_ = h.shareService.IncrementView(r.Context(), token)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(struct {
			Name    string                     `json:"name"`
			Path    string                     `json:"path"`
			Entries []service.PublicShareEntry `json:"entries"`
			Offset  int                        `json:"offset"`
			Limit   int                        `json:"limit"`
			HasMore bool                       `json:"hasMore"`
		}{Name: item.Name, Path: r.URL.Query().Get("path"), Entries: entries, Offset: max(0, offset), Limit: normalizedShareLimit(limit), HasMore: hasMore})
	case "download":
		relativePath := r.URL.Query().Get("path")
		if relativePath == "" {
			http.NotFound(w, r)
			return
		}
		item, file, info, err := h.shareService.ResolvePublicFile(r.Context(), token, relativePath)
		if err != nil {
			h.writePublicShareError(w, r, err)
			return
		}
		defer file.Close()
		if shouldCountAccess(r) && r.Method == http.MethodGet {
			_ = h.shareService.IncrementDownload(r.Context(), token)
		}
		if item.IsPreviewMode() {
			setInlineContentDisposition(w, filepath.Base(relativePath))
		} else {
			setAttachmentContentDisposition(w, filepath.Base(relativePath))
		}
		http.ServeContent(w, r, filepath.Base(relativePath), info.ModTime(), file)
	default:
		http.NotFound(w, r)
	}
}

func normalizedShareLimit(limit int) int {
	if limit <= 0 || limit > 200 {
		return 100
	}
	return limit
}

func (h *ShareHandler) writePublicShareError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, share.ErrShareNotFound) || errors.Is(err, share.ErrInvalidShare) || errors.Is(err, os.ErrNotExist) {
		http.NotFound(w, r)
		return
	}
	if errors.Is(err, share.ErrShareExpired) {
		http.Error(w, "share expired", http.StatusGone)
		return
	}
	h.logger.Error("failed to resolve share", zap.Error(err))
	http.Error(w, "Internal server error", http.StatusInternalServerError)
}

func (h *ShareHandler) buildShareURL(r *http.Request, token, fileName string, isDir bool) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if forwarded := r.Header.Get("X-Forwarded-Proto"); forwarded != "" {
		scheme = forwarded
	}
	host := r.Host
	if forwardedHost := r.Header.Get("X-Forwarded-Host"); forwardedHost != "" {
		host = forwardedHost
	}
	if isDir {
		return scheme + "://" + host + "/share/" + url.PathEscape(token)
	}
	if strings.TrimSpace(fileName) == "" {
		return scheme + "://" + host + "/api/v1/public/share/" + token
	}
	return scheme + "://" + host + "/api/v1/public/share/" + token + "/" + url.PathEscape(fileName)
}

const timeLayout = "2006-01-02 15:04:05"

func shouldCountAccess(r *http.Request) bool {
	rangeHeader := r.Header.Get("Range")
	if rangeHeader == "" {
		return true
	}
	// 只统计首段请求，避免 Range 分片导致计数膨胀
	return strings.HasPrefix(rangeHeader, "bytes=0-")
}

func parseShareAccessPath(requestPath string) (token string, hasFilename bool) {
	sharePath := strings.TrimPrefix(requestPath, "/api/v1/public/share/")
	sharePath = strings.Trim(sharePath, "/")
	if sharePath == "" {
		return "", false
	}
	parts := strings.Split(sharePath, "/")
	return parts[0], len(parts) > 1
}
