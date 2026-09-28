package handler

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/yeying-community/warehouse/internal/application/service"
	"github.com/yeying-community/warehouse/internal/domain/user"
	"github.com/yeying-community/warehouse/internal/infrastructure/config"
	"github.com/yeying-community/warehouse/internal/interface/http/middleware"
	"go.uber.org/zap"
)

func TestAssetToolHandlerCatalogAndCalls(t *testing.T) {
	objects := service.NewObjectService(t.TempDir())
	handler := NewAssetToolHandler(&config.Config{}, nil, objects, zap.NewNop())
	owner := &user.User{ID: "u1", Username: "alice", Directory: "alice", Quota: 0}
	payload := "hello knowledge"
	checksumBytes := sha256.Sum256([]byte(payload))
	checksum := hex.EncodeToString(checksumBytes[:])

	if _, err := objects.PutForUserWithOptions(context.Background(), owner, "services", "knowledge/artifacts/report.md", strings.NewReader(payload), service.ObjectWriteOptions{ContentType: "text/plain; charset=utf-8"}); err != nil {
		t.Fatalf("seed object: %v", err)
	}

	catalogReq := newAssetToolRequest(t, http.MethodGet, "/api/v1/public/tools/warehouse", nil, owner)
	catalogRec := httptest.NewRecorder()
	handler.HandleCatalog(catalogRec, catalogReq)
	if catalogRec.Code != http.StatusOK {
		t.Fatalf("catalog status=%d body=%s", catalogRec.Code, catalogRec.Body.String())
	}
	var catalog struct {
		Tools []assetToolDefinition `json:"tools"`
	}
	if err := json.NewDecoder(catalogRec.Body).Decode(&catalog); err != nil {
		t.Fatalf("decode catalog: %v", err)
	}
	if len(catalog.Tools) != 5 {
		t.Fatalf("unexpected tool count: %d", len(catalog.Tools))
	}
	put := catalog.Tools[len(catalog.Tools)-1]
	if put.Name != assetToolObjectPut || put.SideEffects != "write" || put.ConfirmationRequired || put.Idempotency != "idempotent" {
		t.Fatalf("unexpected put tool definition: %+v", put)
	}

	spaceResult := callAssetTool(t, handler, owner, map[string]any{"name": assetToolSpaceList})
	if spaceResult["defaultSpace"] != "personal" {
		t.Fatalf("unexpected space result: %+v", spaceResult)
	}

	listResult := callAssetTool(t, handler, owner, map[string]any{
		"name":      assetToolObjectList,
		"arguments": map[string]any{"prefix": "/services/knowledge/", "delimiter": "/"},
	})
	prefixes, ok := listResult["prefixes"].([]any)
	if !ok || len(prefixes) != 1 || prefixes[0] != "/services/knowledge/artifacts/" {
		t.Fatalf("unexpected list result: %+v", listResult)
	}

	statResult := callAssetTool(t, handler, owner, map[string]any{
		"name":      assetToolObjectStat,
		"arguments": map[string]any{"path": "/services/knowledge/artifacts/report.md"},
	})
	if statResult["path"] != "/services/knowledge/artifacts/report.md" || statResult["checksumSha256"] != checksum {
		t.Fatalf("unexpected stat result: %+v", statResult)
	}

	readResult := callAssetTool(t, handler, owner, map[string]any{
		"name":      assetToolObjectRead,
		"traceId":   "trace-asset-tool",
		"arguments": map[string]any{"path": "/services/knowledge/artifacts/report.md"},
	})
	if readResult["encoding"] != "utf-8" || readResult["content"] != payload || readResult["mode"] != "content" {
		t.Fatalf("unexpected read result: %+v", readResult)
	}
	metadata, ok := readResult["metadata"].(map[string]any)
	if !ok || metadata["checksumSha256"] != checksum {
		t.Fatalf("unexpected read metadata: %+v", readResult)
	}
}

func TestAssetToolHandlerWritesIdempotentlyAndRequiresExplicitOverwrite(t *testing.T) {
	objects := service.NewObjectService(t.TempDir())
	handler := NewAssetToolHandler(&config.Config{}, nil, objects, zap.NewNop())
	owner := &user.User{ID: "u1", Username: "alice", Directory: "alice", Quota: 0}
	path := "/personal/reviews/conversations/session-1/transcript.md"
	body := map[string]any{
		"name":    assetToolObjectPut,
		"traceId": "archive-session-1",
		"arguments": map[string]any{
			"path":        path,
			"content":     "# Review\n\nArchived conversation.\n",
			"contentType": "text/markdown; charset=utf-8",
		},
	}
	first := callAssetTool(t, handler, owner, body)
	if first["path"] != path || first["contentType"] != "text/markdown; charset=utf-8" || first["checksumSha256"] == "" {
		t.Fatalf("unexpected first write: %+v", first)
	}
	second := callAssetTool(t, handler, owner, body)
	if second["checksumSha256"] != first["checksumSha256"] {
		t.Fatalf("idempotent retry changed object: first=%+v second=%+v", first, second)
	}

	conflict := executeAssetTool(t, handler, owner, map[string]any{
		"name":      assetToolObjectPut,
		"arguments": map[string]any{"path": path, "content": "different"},
	})
	if conflict.Code != http.StatusConflict || decodeAssetToolErrorCode(t, conflict) != "OBJECT_EXISTS" {
		t.Fatalf("unexpected conflict: status=%d body=%s", conflict.Code, conflict.Body.String())
	}

	overwrite := callAssetTool(t, handler, owner, map[string]any{
		"name":      assetToolObjectPut,
		"arguments": map[string]any{"path": path, "content": "updated", "overwrite": true},
	})
	if overwrite["checksumSha256"] == first["checksumSha256"] {
		t.Fatalf("overwrite did not change content: %+v", overwrite)
	}

	precondition := executeAssetTool(t, handler, owner, map[string]any{
		"name":      assetToolObjectPut,
		"arguments": map[string]any{"path": path, "content": "again", "overwrite": true, "ifMatch": "stale-etag"},
	})
	if precondition.Code != http.StatusPreconditionFailed || decodeAssetToolErrorCode(t, precondition) != "PRECONDITION_FAILED" {
		t.Fatalf("unexpected precondition response: status=%d body=%s", precondition.Code, precondition.Body.String())
	}
}

func TestAssetToolHandlerRejectsUnknownTool(t *testing.T) {
	handler := NewAssetToolHandler(&config.Config{}, nil, service.NewObjectService(t.TempDir()), zap.NewNop())
	owner := &user.User{ID: "u1", Username: "alice", Directory: "alice", Quota: 0}

	rec := executeAssetTool(t, handler, owner, map[string]any{"name": "warehouse.object.unknown", "arguments": map[string]any{}})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("write tool status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if body["code"] != "UNKNOWN_TOOL" || body["requestId"] == "" {
		t.Fatalf("unexpected error body: %+v", body)
	}
}

func TestAssetToolHandlerPutEnforcesUcanAppScope(t *testing.T) {
	handler := NewAssetToolHandler(&config.Config{}, nil, service.NewObjectService(t.TempDir()), zap.NewNop())
	owner := &user.User{ID: "u1", Username: "alice", Directory: "alice", Quota: 0}
	body := map[string]any{
		"name":      assetToolObjectPut,
		"arguments": map[string]any{"path": "/apps/other-app/review.md", "content": "denied"},
	}
	rec := executeAssetToolWithContext(t, handler, owner, body, func(ctx context.Context) context.Context {
		return middleware.WithUcanContext(ctx, &middleware.UcanContext{
			HasAppCaps: true,
			AppCaps:    map[string][]string{"allowed-app": {"write"}},
		})
	})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func decodeAssetToolErrorCode(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var body map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	return fmt.Sprint(body["code"])
}

func TestAssetToolHandlerEnforcesUcanAppScope(t *testing.T) {
	objects := service.NewObjectService(t.TempDir())
	handler := NewAssetToolHandler(&config.Config{}, nil, objects, zap.NewNop())
	owner := &user.User{ID: "u1", Username: "alice", Directory: "alice", Quota: 0}
	if _, err := objects.PutForUser(context.Background(), owner, "services", "knowledge/private.txt", strings.NewReader("secret")); err != nil {
		t.Fatalf("seed object: %v", err)
	}

	body := map[string]any{
		"name":      assetToolObjectStat,
		"arguments": map[string]any{"path": "/services/knowledge/private.txt"},
	}
	rec := executeAssetToolWithContext(t, handler, owner, body, func(ctx context.Context) context.Context {
		return middleware.WithUcanContext(ctx, &middleware.UcanContext{
			HasAppCaps: true,
			AppCaps:    map[string][]string{"demo.app": {"read"}},
		})
	})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestAssetToolHandlerRejectsLargeContentRead(t *testing.T) {
	objects := service.NewObjectService(t.TempDir())
	handler := NewAssetToolHandler(&config.Config{}, nil, objects, zap.NewNop())
	owner := &user.User{ID: "u1", Username: "alice", Directory: "alice", Quota: 0}
	if _, err := objects.PutForUser(context.Background(), owner, "personal", "large.txt", strings.NewReader("0123456789")); err != nil {
		t.Fatalf("seed object: %v", err)
	}

	rec := executeAssetTool(t, handler, owner, map[string]any{
		"name":      assetToolObjectRead,
		"arguments": map[string]any{"path": "/personal/large.txt", "maxBytes": 4},
	})
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if body["code"] != "CONTENT_TOO_LARGE" {
		t.Fatalf("unexpected error body: %+v", body)
	}
}

func callAssetTool(t *testing.T, handler *AssetToolHandler, owner *user.User, body map[string]any) map[string]any {
	t.Helper()
	rec := executeAssetTool(t, handler, owner, body)
	if rec.Code != http.StatusOK {
		t.Fatalf("tool status=%d body=%s", rec.Code, rec.Body.String())
	}
	var envelope map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&envelope); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	if envelope["name"] != body["name"] || envelope["requestId"] == "" {
		t.Fatalf("unexpected envelope: %+v", envelope)
	}
	result, ok := envelope["result"].(map[string]any)
	if !ok {
		t.Fatalf("unexpected result: %+v", envelope["result"])
	}
	return result
}

func executeAssetTool(t *testing.T, handler *AssetToolHandler, owner *user.User, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	return executeAssetToolWithContext(t, handler, owner, body, func(ctx context.Context) context.Context { return ctx })
}

func executeAssetToolWithContext(t *testing.T, handler *AssetToolHandler, owner *user.User, body map[string]any, enrich func(context.Context) context.Context) *httptest.ResponseRecorder {
	t.Helper()
	payload, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	req := newAssetToolRequest(t, http.MethodPost, "/api/v1/public/tools/warehouse/call", bytes.NewReader(payload), owner)
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(enrich(req.Context()))
	rec := httptest.NewRecorder()
	middleware.RequestIDMiddleware(http.HandlerFunc(handler.HandleCall)).ServeHTTP(rec, req)
	return rec
}

func newAssetToolRequest(t *testing.T, method, target string, body *bytes.Reader, u *user.User) *http.Request {
	t.Helper()
	var reader *bytes.Reader
	if body == nil {
		reader = bytes.NewReader(nil)
	} else {
		reader = body
	}
	req := httptest.NewRequest(method, target, reader)
	ctx := context.WithValue(req.Context(), middleware.UserContextKey, u)
	return req.WithContext(ctx)
}
