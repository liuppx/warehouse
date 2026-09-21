package handler

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/yeying-community/warehouse/internal/application/assetspace"
	"github.com/yeying-community/warehouse/internal/application/service"
	"github.com/yeying-community/warehouse/internal/domain/auth"
	"github.com/yeying-community/warehouse/internal/domain/user"
	"github.com/yeying-community/warehouse/internal/infrastructure/config"
	"github.com/yeying-community/warehouse/internal/interface/http/middleware"
	"go.uber.org/zap"
)

const (
	assetToolSpaceList  = "warehouse.space.list"
	assetToolObjectList = "warehouse.object.list"
	assetToolObjectStat = "warehouse.object.stat"
	assetToolObjectRead = "warehouse.object.read"

	defaultToolReadMaxBytes int64 = 1 << 20
	maxToolReadMaxBytes     int64 = 5 << 20
)

// AssetToolHandler exposes the read-only Warehouse asset Tool adapter.
// It maps stable Tool names to existing Warehouse API semantics without
// introducing an MCP server or a second authorization layer.
type AssetToolHandler struct {
	config            *config.Config
	assetSpaceManager *assetspace.Manager
	objects           *service.ObjectService
	logger            *zap.Logger
}

type assetToolDefinition struct {
	Name                 string         `json:"name"`
	Version              string         `json:"version"`
	Description          string         `json:"description"`
	InputSchema          map[string]any `json:"inputSchema"`
	OutputSchema         map[string]any `json:"outputSchema"`
	RequiredScopes       []string       `json:"requiredScopes"`
	SideEffects          string         `json:"sideEffects"`
	Idempotency          string         `json:"idempotency"`
	ConfirmationRequired bool           `json:"confirmationRequired"`
	SourceAPI            assetToolAPI   `json:"sourceApi"`
}

type assetToolAPI struct {
	Method string `json:"method"`
	Path   string `json:"path"`
}

type assetToolCallRequest struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
	TraceID   string          `json:"traceId,omitempty"`
}

type assetToolCallResponse struct {
	Name      string `json:"name"`
	Result    any    `json:"result"`
	RequestID string `json:"requestId,omitempty"`
	TraceID   string `json:"traceId,omitempty"`
}

type assetToolSpaceListResult struct {
	DefaultSpace string             `json:"defaultSpace"`
	Spaces       []assetspace.Space `json:"spaces"`
}

type assetToolObjectListArgs struct {
	Prefix    string `json:"prefix"`
	Delimiter string `json:"delimiter,omitempty"`
}

type assetToolObjectPathArgs struct {
	Path string `json:"path"`
}

type assetToolObjectReadArgs struct {
	Path     string `json:"path"`
	Mode     string `json:"mode,omitempty"`
	MaxBytes int64  `json:"maxBytes,omitempty"`
}

type assetToolObjectReadResult struct {
	Metadata  assetObjectResponse `json:"metadata"`
	Mode      string              `json:"mode"`
	Encoding  string              `json:"encoding,omitempty"`
	Content   string              `json:"content,omitempty"`
	Truncated bool                `json:"truncated"`
}

func NewAssetToolHandler(cfg *config.Config, assetSpaceManager *assetspace.Manager, objects *service.ObjectService, logger *zap.Logger) *AssetToolHandler {
	return &AssetToolHandler{
		config:            cfg,
		assetSpaceManager: assetSpaceManager,
		objects:           objects,
		logger:            logger,
	}
}

func (h *AssetToolHandler) HandleCatalog(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.writeError(w, r, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "method not allowed")
		return
	}
	if _, ok := h.currentUser(w, r); !ok {
		return
	}
	h.writeJSON(w, http.StatusOK, map[string]any{
		"tools": assetToolDefinitions(),
	})
}

func (h *AssetToolHandler) HandleCall(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		h.writeError(w, r, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "method not allowed")
		return
	}

	u, ok := h.currentUser(w, r)
	if !ok {
		return
	}

	var req assetToolCallRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, r, http.StatusBadRequest, "INVALID_REQUEST", "invalid request body")
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		h.writeError(w, r, http.StatusBadRequest, "INVALID_TOOL", "tool name is required")
		return
	}

	var result any
	switch req.Name {
	case assetToolSpaceList:
		value, ok := h.callSpaceList(w, r, u)
		if !ok {
			return
		}
		result = value
	case assetToolObjectList:
		value, ok := h.callObjectList(w, r, u, req.Arguments)
		if !ok {
			return
		}
		result = value
	case assetToolObjectStat:
		value, ok := h.callObjectStat(w, r, u, req.Arguments)
		if !ok {
			return
		}
		result = value
	case assetToolObjectRead:
		value, ok := h.callObjectRead(w, r, u, req.Arguments)
		if !ok {
			return
		}
		result = value
	default:
		h.writeError(w, r, http.StatusNotFound, "UNKNOWN_TOOL", "unknown warehouse tool")
		return
	}

	h.writeJSON(w, http.StatusOK, assetToolCallResponse{
		Name:      req.Name,
		Result:    result,
		RequestID: middleware.RequestID(r.Context()),
		TraceID:   firstNonEmptyAssetToolValue(strings.TrimSpace(req.TraceID), strings.TrimSpace(r.Header.Get("X-Trace-ID"))),
	})
}

func (h *AssetToolHandler) callSpaceList(w http.ResponseWriter, r *http.Request, u *user.User) (assetToolSpaceListResult, bool) {
	if h.assetSpaceManager != nil {
		if err := h.assetSpaceManager.EnsureForUser(u); err != nil {
			if h.logger != nil {
				h.logger.Error("failed to ensure user asset spaces",
					zap.String("username", u.Username),
					zap.String("directory", u.Directory),
					zap.Error(err))
			}
			h.writeError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to initialize user spaces")
			return assetToolSpaceListResult{}, false
		}
	}

	defaultSpace := assetspace.PersonalSpaceKey
	spaces := []assetspace.Space{
		{Key: assetspace.PersonalSpaceKey, Name: "个人资产", Path: "/personal"},
		{Key: assetspace.AppsSpaceKey, Name: "应用资产", Path: "/apps"},
		{Key: assetspace.ServicesSpaceKey, Name: "服务资产", Path: "/services"},
	}
	if h.assetSpaceManager != nil {
		defaultSpace = h.assetSpaceManager.DefaultSpace()
		spaces = h.assetSpaceManager.Spaces()
	}
	return assetToolSpaceListResult{DefaultSpace: defaultSpace, Spaces: spaces}, true
}

func (h *AssetToolHandler) callObjectList(w http.ResponseWriter, r *http.Request, u *user.User, raw json.RawMessage) (assetObjectListResponse, bool) {
	var args assetToolObjectListArgs
	if !h.decodeArguments(w, r, raw, &args) {
		return assetObjectListResponse{}, false
	}
	ref, err := parseAssetPath(args.Prefix, true)
	if err != nil {
		h.writeError(w, r, http.StatusBadRequest, "INVALID_PATH", err.Error())
		return assetObjectListResponse{}, false
	}
	if err := service.EnforceAppScope(r.Context(), h.config, ref.Path, "read"); err != nil {
		h.writeScopeError(w, r, err)
		return assetObjectListResponse{}, false
	}
	delimiter := rune(0)
	if strings.TrimSpace(args.Delimiter) == "/" {
		delimiter = '/'
	} else if strings.TrimSpace(args.Delimiter) != "" {
		h.writeError(w, r, http.StatusBadRequest, "INVALID_DELIMITER", "delimiter must be / when provided")
		return assetObjectListResponse{}, false
	}
	result, err := h.objects.List(r.Context(), u.Directory, ref.Bucket, ref.Key, delimiter)
	if err != nil {
		h.writeObjectError(w, r, err)
		return assetObjectListResponse{}, false
	}
	objects := make([]assetObjectResponse, 0, len(result.Objects))
	for _, info := range result.Objects {
		objects = append(objects, assetObjectResponseForInfo(info, ""))
	}
	prefixes := make([]string, 0, len(result.Prefixes))
	for _, prefix := range result.Prefixes {
		prefixes = append(prefixes, "/"+ref.Bucket+"/"+prefix)
	}
	return assetObjectListResponse{Prefix: ref.Path, Objects: objects, Prefixes: prefixes}, true
}

func (h *AssetToolHandler) callObjectStat(w http.ResponseWriter, r *http.Request, u *user.User, raw json.RawMessage) (assetObjectResponse, bool) {
	var args assetToolObjectPathArgs
	if !h.decodeArguments(w, r, raw, &args) {
		return assetObjectResponse{}, false
	}
	meta, ok := h.readObjectMetadata(w, r, u, args.Path)
	return meta, ok
}

func (h *AssetToolHandler) callObjectRead(w http.ResponseWriter, r *http.Request, u *user.User, raw json.RawMessage) (assetToolObjectReadResult, bool) {
	var args assetToolObjectReadArgs
	if !h.decodeArguments(w, r, raw, &args) {
		return assetToolObjectReadResult{}, false
	}
	mode := strings.ToLower(strings.TrimSpace(args.Mode))
	if mode == "" {
		mode = "content"
	}
	if mode != "head" && mode != "content" {
		h.writeError(w, r, http.StatusBadRequest, "INVALID_MODE", "mode must be head or content")
		return assetToolObjectReadResult{}, false
	}
	limit := int64(0)
	if mode == "content" {
		if args.MaxBytes < 0 || args.MaxBytes > maxToolReadMaxBytes {
			h.writeError(w, r, http.StatusBadRequest, "INVALID_MAX_BYTES", fmt.Sprintf("maxBytes must be between 0 and %d", maxToolReadMaxBytes))
			return assetToolObjectReadResult{}, false
		}
		limit = normalizeToolReadMaxBytes(args.MaxBytes)
	}

	ref, err := parseAssetPath(args.Path, false)
	if err != nil {
		h.writeError(w, r, http.StatusBadRequest, "INVALID_PATH", err.Error())
		return assetToolObjectReadResult{}, false
	}
	if err := service.EnforceAppScope(r.Context(), h.config, ref.Path, "read"); err != nil {
		h.writeScopeError(w, r, err)
		return assetToolObjectReadResult{}, false
	}
	file, info, err := h.objects.Open(r.Context(), u.Directory, ref.Bucket, ref.Key)
	if err != nil {
		h.writeObjectError(w, r, err)
		return assetToolObjectReadResult{}, false
	}
	defer file.Close()
	if mode == "content" && info.Size > limit {
		h.writeError(w, r, http.StatusRequestEntityTooLarge, "CONTENT_TOO_LARGE", fmt.Sprintf("object size %d exceeds tool read limit %d", info.Size, limit))
		return assetToolObjectReadResult{}, false
	}

	checksum, err := sha256Hex(file)
	if err != nil {
		h.writeObjectError(w, r, err)
		return assetToolObjectReadResult{}, false
	}
	metadata := assetObjectResponseForInfo(info, checksum)
	if mode == "head" {
		return assetToolObjectReadResult{Metadata: metadata, Mode: mode}, true
	}

	if _, err := file.Seek(0, io.SeekStart); err != nil {
		h.writeObjectError(w, r, err)
		return assetToolObjectReadResult{}, false
	}
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		h.writeObjectError(w, r, err)
		return assetToolObjectReadResult{}, false
	}
	if int64(len(data)) > limit {
		h.writeError(w, r, http.StatusRequestEntityTooLarge, "CONTENT_TOO_LARGE", fmt.Sprintf("object content exceeds tool read limit %d", limit))
		return assetToolObjectReadResult{}, false
	}

	encoding := "base64"
	content := base64.StdEncoding.EncodeToString(data)
	if isTextToolContent(info.ContentType, data) {
		encoding = "utf-8"
		content = string(data)
	}
	return assetToolObjectReadResult{
		Metadata:  metadata,
		Mode:      mode,
		Encoding:  encoding,
		Content:   content,
		Truncated: false,
	}, true
}

func (h *AssetToolHandler) readObjectMetadata(w http.ResponseWriter, r *http.Request, u *user.User, rawPath string) (assetObjectResponse, bool) {
	ref, err := parseAssetPath(rawPath, false)
	if err != nil {
		h.writeError(w, r, http.StatusBadRequest, "INVALID_PATH", err.Error())
		return assetObjectResponse{}, false
	}
	if err := service.EnforceAppScope(r.Context(), h.config, ref.Path, "read"); err != nil {
		h.writeScopeError(w, r, err)
		return assetObjectResponse{}, false
	}
	file, info, err := h.objects.Open(r.Context(), u.Directory, ref.Bucket, ref.Key)
	if err != nil {
		h.writeObjectError(w, r, err)
		return assetObjectResponse{}, false
	}
	checksum, err := sha256Hex(file)
	_ = file.Close()
	if err != nil {
		h.writeObjectError(w, r, err)
		return assetObjectResponse{}, false
	}
	return assetObjectResponseForInfo(info, checksum), true
}

func (h *AssetToolHandler) decodeArguments(w http.ResponseWriter, r *http.Request, raw json.RawMessage, target any) bool {
	if len(raw) == 0 || strings.TrimSpace(string(raw)) == "null" {
		raw = []byte("{}")
	}
	if err := json.Unmarshal(raw, target); err != nil {
		h.writeError(w, r, http.StatusBadRequest, "INVALID_ARGUMENTS", "invalid tool arguments")
		return false
	}
	return true
}

func (h *AssetToolHandler) currentUser(w http.ResponseWriter, r *http.Request) (*user.User, bool) {
	u, ok := middleware.GetUserFromContext(r.Context())
	if !ok || u == nil {
		h.writeError(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "unauthorized")
		return nil, false
	}
	return u, true
}

func (h *AssetToolHandler) writeScopeError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, auth.ErrAppScopeRequired), errors.Is(err, auth.ErrAppScopeDenied):
		h.writeError(w, r, http.StatusForbidden, "FORBIDDEN", "forbidden")
	default:
		h.writeError(w, r, http.StatusBadRequest, "INVALID_SCOPE", err.Error())
	}
}

func (h *AssetToolHandler) writeObjectError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, os.ErrNotExist):
		h.writeError(w, r, http.StatusNotFound, "NOT_FOUND", "not found")
	case errors.Is(err, user.ErrQuotaExceeded):
		h.writeError(w, r, http.StatusRequestEntityTooLarge, "QUOTA_EXCEEDED", "storage quota exceeded")
	default:
		if h.logger != nil {
			h.logger.Error("asset tool request failed", zap.Error(err))
		}
		status := http.StatusInternalServerError
		code := "INTERNAL_ERROR"
		if strings.Contains(strings.ToLower(err.Error()), "checksum") {
			status = http.StatusBadRequest
			code = "CHECKSUM_MISMATCH"
		}
		h.writeError(w, r, status, code, err.Error())
	}
}

func (h *AssetToolHandler) writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(data); err != nil && h.logger != nil {
		h.logger.Error("failed to write asset tool response", zap.Error(err))
	}
}

func (h *AssetToolHandler) writeError(w http.ResponseWriter, r *http.Request, status int, code, message string) {
	h.writeJSON(w, status, map[string]any{
		"code":      code,
		"message":   message,
		"requestId": middleware.RequestID(r.Context()),
	})
}

func assetObjectResponseForInfo(info service.ObjectInfo, checksum string) assetObjectResponse {
	return assetObjectResponse{
		Path:           "/" + info.Bucket + "/" + strings.TrimPrefix(info.Key, "/"),
		Bucket:         info.Bucket,
		Key:            info.Key,
		Size:           info.Size,
		ETag:           info.ETag,
		ChecksumSHA256: checksum,
		ContentType:    info.ContentType,
		ModifiedAt:     info.ModifiedAt.UTC().Format(time.RFC3339),
		IsPrefix:       info.IsPrefix,
	}
}

func normalizeToolReadMaxBytes(value int64) int64 {
	if value <= 0 {
		return defaultToolReadMaxBytes
	}
	return value
}

func isTextToolContent(contentType string, data []byte) bool {
	mediaType, _, err := mime.ParseMediaType(strings.TrimSpace(contentType))
	if err != nil {
		mediaType = strings.TrimSpace(contentType)
	}
	mediaType = strings.ToLower(mediaType)
	if mediaType == "" {
		return utf8.Valid(data)
	}
	if strings.HasPrefix(mediaType, "text/") {
		return utf8.Valid(data)
	}
	switch mediaType {
	case "application/json", "application/xml", "application/yaml", "application/x-yaml", "application/javascript", "application/x-ndjson", "application/octet-stream":
		return utf8.Valid(data)
	default:
		return false
	}
}

func firstNonEmptyAssetToolValue(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func assetToolDefinitions() []assetToolDefinition {
	assetPathSchema := map[string]any{
		"type":     "string",
		"pattern":  "^/(personal|apps|services)(/.*)?$",
		"examples": []string{"/personal/docs/source.md", "/services/knowledge/artifacts/report.md"},
	}
	objectSchema := map[string]any{
		"type":     "object",
		"required": []string{"path", "bucket", "key", "size", "etag", "contentType", "modifiedAt", "isPrefix"},
		"properties": map[string]any{
			"path":           assetPathSchema,
			"bucket":         map[string]any{"type": "string", "enum": []string{"personal", "apps", "services"}},
			"key":            map[string]any{"type": "string"},
			"size":           map[string]any{"type": "integer", "format": "int64", "minimum": 0},
			"etag":           map[string]any{"type": "string"},
			"checksumSha256": map[string]any{"type": "string"},
			"contentType":    map[string]any{"type": "string"},
			"modifiedAt":     map[string]any{"type": "string", "format": "date-time"},
			"isPrefix":       map[string]any{"type": "boolean"},
		},
	}
	return []assetToolDefinition{
		{
			Name:        assetToolSpaceList,
			Version:     "1.0",
			Description: "列出当前调用身份可访问的 Warehouse 资产空间。",
			InputSchema: map[string]any{"type": "object", "additionalProperties": false},
			OutputSchema: map[string]any{"type": "object", "required": []string{"defaultSpace", "spaces"}, "properties": map[string]any{
				"defaultSpace": map[string]any{"type": "string"},
				"spaces":       map[string]any{"type": "array"},
			}},
			RequiredScopes:       []string{"asset:read"},
			SideEffects:          "none",
			Idempotency:          "safe",
			ConfirmationRequired: false,
			SourceAPI:            assetToolAPI{Method: http.MethodGet, Path: "/api/v1/public/assets/spaces"},
		},
		{
			Name:        assetToolObjectList,
			Version:     "1.0",
			Description: "按 prefix 和 delimiter 列出调用身份有权访问的对象和公共前缀。",
			InputSchema: map[string]any{"type": "object", "required": []string{"prefix"}, "properties": map[string]any{
				"prefix":    assetPathSchema,
				"delimiter": map[string]any{"type": "string", "enum": []string{"/"}},
			}, "additionalProperties": false},
			OutputSchema: map[string]any{"type": "object", "required": []string{"prefix", "objects", "prefixes"}, "properties": map[string]any{
				"prefix":   assetPathSchema,
				"objects":  map[string]any{"type": "array", "items": objectSchema},
				"prefixes": map[string]any{"type": "array", "items": assetPathSchema},
			}},
			RequiredScopes:       []string{"asset:read"},
			SideEffects:          "none",
			Idempotency:          "safe",
			ConfirmationRequired: false,
			SourceAPI:            assetToolAPI{Method: http.MethodGet, Path: "/api/v1/public/assets/objects"},
		},
		{
			Name:                 assetToolObjectStat,
			Version:              "1.0",
			Description:          "获取调用身份有权访问的单个对象元数据和 SHA-256。",
			InputSchema:          objectPathInputSchema(assetPathSchema),
			OutputSchema:         objectSchema,
			RequiredScopes:       []string{"asset:read"},
			SideEffects:          "none",
			Idempotency:          "safe",
			ConfirmationRequired: false,
			SourceAPI:            assetToolAPI{Method: http.MethodGet, Path: "/api/v1/public/assets/object"},
		},
		{
			Name:        assetToolObjectRead,
			Version:     "1.0",
			Description: "读取调用身份有权访问的小对象内容，或只读取对象头信息。",
			InputSchema: map[string]any{"type": "object", "required": []string{"path"}, "properties": map[string]any{
				"path":     assetPathSchema,
				"mode":     map[string]any{"type": "string", "enum": []string{"head", "content"}, "default": "content"},
				"maxBytes": map[string]any{"type": "integer", "format": "int64", "minimum": 0, "maximum": maxToolReadMaxBytes, "default": defaultToolReadMaxBytes},
			}, "additionalProperties": false},
			OutputSchema: map[string]any{"type": "object", "required": []string{"metadata", "mode", "truncated"}, "properties": map[string]any{
				"metadata":  objectSchema,
				"mode":      map[string]any{"type": "string", "enum": []string{"head", "content"}},
				"encoding":  map[string]any{"type": "string", "enum": []string{"utf-8", "base64"}},
				"content":   map[string]any{"type": "string"},
				"truncated": map[string]any{"type": "boolean"},
			}},
			RequiredScopes:       []string{"asset:read"},
			SideEffects:          "none",
			Idempotency:          "safe",
			ConfirmationRequired: false,
			SourceAPI:            assetToolAPI{Method: "GET/HEAD", Path: "/api/v1/public/assets/object/content"},
		},
	}
}

func objectPathInputSchema(pathSchema map[string]any) map[string]any {
	return map[string]any{"type": "object", "required": []string{"path"}, "properties": map[string]any{
		"path": pathSchema,
	}, "additionalProperties": false}
}
