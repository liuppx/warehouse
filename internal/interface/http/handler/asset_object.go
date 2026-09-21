package handler

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"strings"

	"github.com/yeying-community/warehouse/internal/application/service"
	"github.com/yeying-community/warehouse/internal/domain/auth"
	"github.com/yeying-community/warehouse/internal/domain/user"
	"github.com/yeying-community/warehouse/internal/infrastructure/config"
	"github.com/yeying-community/warehouse/internal/interface/http/middleware"
	"go.uber.org/zap"
)

type AssetObjectHandler struct {
	config  *config.Config
	objects *service.ObjectService
	logger  *zap.Logger
}

type assetObjectResponse struct {
	Path           string `json:"path"`
	Bucket         string `json:"bucket"`
	Key            string `json:"key"`
	Size           int64  `json:"size"`
	ETag           string `json:"etag"`
	ChecksumSHA256 string `json:"checksumSha256,omitempty"`
	ContentType    string `json:"contentType"`
	ModifiedAt     string `json:"modifiedAt"`
	IsPrefix       bool   `json:"isPrefix"`
}

type assetObjectListResponse struct {
	Prefix   string                `json:"prefix"`
	Objects  []assetObjectResponse `json:"objects"`
	Prefixes []string              `json:"prefixes"`
}

func NewAssetObjectHandler(cfg *config.Config, objects *service.ObjectService, logger *zap.Logger) *AssetObjectHandler {
	return &AssetObjectHandler{config: cfg, objects: objects, logger: logger}
}

func (h *AssetObjectHandler) HandleObject(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		h.handleMetadata(w, r)
	default:
		h.writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "method not allowed")
	}
}

func (h *AssetObjectHandler) HandleObjectContent(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodHead:
		h.handleContentHead(w, r)
	case http.MethodGet:
		h.handleContentGet(w, r)
	case http.MethodPut:
		h.handleContentPut(w, r)
	default:
		h.writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "method not allowed")
	}
}

func (h *AssetObjectHandler) HandleObjects(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "method not allowed")
		return
	}
	u, ok := h.currentUser(w, r)
	if !ok {
		return
	}
	rawPrefix := strings.TrimSpace(r.URL.Query().Get("prefix"))
	if rawPrefix == "" {
		rawPrefix = strings.TrimSpace(r.URL.Query().Get("path"))
	}
	ref, err := parseAssetPath(rawPrefix, true)
	if err != nil {
		h.writeError(w, http.StatusBadRequest, "INVALID_PATH", err.Error())
		return
	}
	if err := service.EnforceAppScope(r.Context(), h.config, ref.Path, "read"); err != nil {
		h.writeScopeError(w, err)
		return
	}
	delimiter := rune(0)
	if r.URL.Query().Get("delimiter") == "/" {
		delimiter = '/'
	}
	result, err := h.objects.List(r.Context(), u.Directory, ref.Bucket, ref.Key, delimiter)
	if err != nil {
		h.writeObjectError(w, err)
		return
	}
	objects := make([]assetObjectResponse, 0, len(result.Objects))
	for _, info := range result.Objects {
		objects = append(objects, h.objectResponse(info, ""))
	}
	prefixes := make([]string, 0, len(result.Prefixes))
	for _, prefix := range result.Prefixes {
		prefixes = append(prefixes, "/"+ref.Bucket+"/"+prefix)
	}
	h.writeJSON(w, http.StatusOK, assetObjectListResponse{
		Prefix:   ref.Path,
		Objects:  objects,
		Prefixes: prefixes,
	})
}

func (h *AssetObjectHandler) handleMetadata(w http.ResponseWriter, r *http.Request) {
	u, ok := h.currentUser(w, r)
	if !ok {
		return
	}
	ref, err := parseAssetPath(r.URL.Query().Get("path"), false)
	if err != nil {
		h.writeError(w, http.StatusBadRequest, "INVALID_PATH", err.Error())
		return
	}
	if err := service.EnforceAppScope(r.Context(), h.config, ref.Path, "read"); err != nil {
		h.writeScopeError(w, err)
		return
	}
	file, info, err := h.objects.Open(r.Context(), u.Directory, ref.Bucket, ref.Key)
	if err != nil {
		h.writeObjectError(w, err)
		return
	}
	checksum, err := sha256Hex(file)
	_ = file.Close()
	if err != nil {
		h.writeObjectError(w, err)
		return
	}
	h.writeJSON(w, http.StatusOK, h.objectResponse(info, checksum))
}

func (h *AssetObjectHandler) handleContentHead(w http.ResponseWriter, r *http.Request) {
	h.serveContent(w, r, false)
}

func (h *AssetObjectHandler) handleContentGet(w http.ResponseWriter, r *http.Request) {
	h.serveContent(w, r, true)
}

func (h *AssetObjectHandler) serveContent(w http.ResponseWriter, r *http.Request, includeBody bool) {
	u, ok := h.currentUser(w, r)
	if !ok {
		return
	}
	ref, err := parseAssetPath(r.URL.Query().Get("path"), false)
	if err != nil {
		h.writeError(w, http.StatusBadRequest, "INVALID_PATH", err.Error())
		return
	}
	if err := service.EnforceAppScope(r.Context(), h.config, ref.Path, "read"); err != nil {
		h.writeScopeError(w, err)
		return
	}
	file, info, err := h.objects.Open(r.Context(), u.Directory, ref.Bucket, ref.Key)
	if err != nil {
		h.writeObjectError(w, err)
		return
	}
	defer file.Close()
	checksum, err := sha256Hex(file)
	if err != nil {
		h.writeObjectError(w, err)
		return
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		h.writeObjectError(w, err)
		return
	}
	h.writeObjectHeaders(w, info, checksum)
	if !includeBody {
		w.WriteHeader(http.StatusOK)
		return
	}
	http.ServeContent(w, r, path.Base(info.Key), info.ModifiedAt, file)
}

func (h *AssetObjectHandler) handleContentPut(w http.ResponseWriter, r *http.Request) {
	u, ok := h.currentUser(w, r)
	if !ok {
		return
	}
	ref, err := parseAssetPath(r.URL.Query().Get("path"), false)
	if err != nil {
		h.writeError(w, http.StatusBadRequest, "INVALID_PATH", err.Error())
		return
	}
	if err := service.EnforceAppScope(r.Context(), h.config, ref.Path, "write", "create", "update"); err != nil {
		h.writeScopeError(w, err)
		return
	}
	expectedSHA256, err := normalizeExpectedSHA256(r.Header.Get("X-Warehouse-Checksum-SHA256"))
	if err != nil {
		h.writeError(w, http.StatusBadRequest, "INVALID_CHECKSUM", err.Error())
		return
	}
	contentType := strings.TrimSpace(r.Header.Get("Content-Type"))
	info, err := h.objects.PutForUserWithOptions(r.Context(), u, ref.Bucket, ref.Key, r.Body, service.ObjectWriteOptions{
		ExpectedSHA256: expectedSHA256,
		ContentType:    contentType,
	})
	if err != nil {
		h.writeObjectError(w, err)
		return
	}
	file, _, err := h.objects.Open(r.Context(), u.Directory, ref.Bucket, ref.Key)
	if err != nil {
		h.writeObjectError(w, err)
		return
	}
	checksum, err := sha256Hex(file)
	_ = file.Close()
	if err != nil {
		h.writeObjectError(w, err)
		return
	}
	h.writeJSON(w, http.StatusOK, h.objectResponse(info, checksum))
}

func (h *AssetObjectHandler) writeObjectHeaders(w http.ResponseWriter, info service.ObjectInfo, checksum string) {
	if info.ContentType != "" {
		w.Header().Set("Content-Type", info.ContentType)
	}
	if info.ETag != "" {
		w.Header().Set("ETag", `"`+strings.Trim(info.ETag, `"`)+`"`)
	}
	if checksum != "" {
		w.Header().Set("X-Warehouse-Checksum-SHA256", checksum)
	}
	w.Header().Set("Content-Length", fmt.Sprintf("%d", info.Size))
	w.Header().Set("Last-Modified", info.ModifiedAt.UTC().Format(http.TimeFormat))
	setInlineContentDisposition(w, path.Base(info.Key))
}

func (h *AssetObjectHandler) objectResponse(info service.ObjectInfo, checksum string) assetObjectResponse {
	return assetObjectResponseForInfo(info, checksum)
}

func (h *AssetObjectHandler) currentUser(w http.ResponseWriter, r *http.Request) (*user.User, bool) {
	u, ok := middleware.GetUserFromContext(r.Context())
	if !ok || u == nil {
		h.writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "unauthorized")
		return nil, false
	}
	return u, true
}

func (h *AssetObjectHandler) writeScopeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, auth.ErrAppScopeRequired), errors.Is(err, auth.ErrAppScopeDenied):
		h.writeError(w, http.StatusForbidden, "FORBIDDEN", "forbidden")
	default:
		h.writeError(w, http.StatusBadRequest, "INVALID_SCOPE", err.Error())
	}
}

func (h *AssetObjectHandler) writeObjectError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, os.ErrNotExist):
		h.writeError(w, http.StatusNotFound, "NOT_FOUND", "not found")
	case errors.Is(err, user.ErrQuotaExceeded):
		h.writeError(w, http.StatusRequestEntityTooLarge, "QUOTA_EXCEEDED", "storage quota exceeded")
	default:
		if h.logger != nil {
			h.logger.Error("asset object request failed", zap.Error(err))
		}
		status := http.StatusInternalServerError
		code := "INTERNAL_ERROR"
		if strings.Contains(strings.ToLower(err.Error()), "checksum") {
			status = http.StatusBadRequest
			code = "CHECKSUM_MISMATCH"
		}
		h.writeError(w, status, code, err.Error())
	}
}

func (h *AssetObjectHandler) writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(data); err != nil && h.logger != nil {
		h.logger.Error("failed to write asset object response", zap.Error(err))
	}
}

func (h *AssetObjectHandler) writeError(w http.ResponseWriter, status int, code, message string) {
	h.writeJSON(w, status, map[string]any{
		"code":    code,
		"message": message,
	})
}

type assetPathRef struct {
	Path   string
	Bucket string
	Key    string
}

func parseAssetPath(raw string, allowBucketRoot bool) (assetPathRef, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return assetPathRef{}, fmt.Errorf("path is required")
	}
	value = strings.ReplaceAll(value, "\\", "/")
	value = "/" + strings.TrimLeft(value, "/")
	for _, segment := range strings.Split(strings.TrimPrefix(value, "/"), "/") {
		if segment == ".." {
			return assetPathRef{}, fmt.Errorf("path cannot contain ..")
		}
	}
	clean := path.Clean(value)
	if clean == "." || clean == "/" {
		return assetPathRef{}, fmt.Errorf("path must include an asset space")
	}
	parts := strings.SplitN(strings.TrimPrefix(clean, "/"), "/", 2)
	bucket := parts[0]
	switch bucket {
	case "personal", "apps", "services":
	default:
		return assetPathRef{}, fmt.Errorf("path must start with /personal, /apps, or /services")
	}
	key := ""
	if len(parts) == 2 {
		key = strings.TrimPrefix(parts[1], "/")
	}
	if key == "" && !allowBucketRoot {
		return assetPathRef{}, fmt.Errorf("path must include an object key")
	}
	if allowBucketRoot && strings.HasSuffix(value, "/") && key != "" {
		key += "/"
	}
	refPath := "/" + bucket
	if key != "" {
		refPath += "/" + key
	}
	return assetPathRef{Path: refPath, Bucket: bucket, Key: key}, nil
}

func normalizeExpectedSHA256(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", nil
	}
	if decoded, err := hex.DecodeString(value); err == nil && len(decoded) == sha256.Size {
		return base64.StdEncoding.EncodeToString(decoded), nil
	}
	if decoded, err := base64.StdEncoding.DecodeString(value); err == nil && len(decoded) == sha256.Size {
		return value, nil
	}
	return "", fmt.Errorf("sha256 checksum must be hex or base64 encoded")
}

func sha256Hex(r io.Reader) (string, error) {
	hash := sha256.New()
	if _, err := io.Copy(hash, r); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}
