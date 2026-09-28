package handler

import (
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/yeying-community/warehouse/internal/domain/toolcredential"
	"github.com/yeying-community/warehouse/internal/infrastructure/crypto"
	"github.com/yeying-community/warehouse/internal/infrastructure/repository"
	"github.com/yeying-community/warehouse/internal/interface/http/middleware"
	"go.uber.org/zap"
)

type ToolCredentialHandler struct {
	repo        repository.ToolCredentialRepository
	logger      *zap.Logger
	tokenHasher *crypto.TokenHasher
}

func NewToolCredentialHandler(repo repository.ToolCredentialRepository, logger *zap.Logger) *ToolCredentialHandler {
	return &ToolCredentialHandler{repo: repo, logger: logger, tokenHasher: crypto.NewTokenHasher()}
}

func (h *ToolCredentialHandler) HandleList(w http.ResponseWriter, r *http.Request) {
	u, ok := middleware.GetUserFromContext(r.Context())
	if !ok {
		http.Error(w, "Unauthorized", 401)
		return
	}
	items, err := h.repo.ListByOwner(r.Context(), u.ID)
	if err != nil {
		h.logger.Error("list tool credentials", zap.Error(err))
		http.Error(w, "Failed to list tool credentials", 500)
		return
	}
	rows := make([]map[string]any, 0, len(items))
	for _, item := range items {
		rows = append(rows, map[string]any{"id": item.ID, "name": item.Name, "scopes": item.Scopes, "pathPrefixes": item.PathPrefixes, "status": item.Status, "expiresAt": item.ExpiresAt.Format(timeLayout), "createdAt": item.CreatedAt.Format(timeLayout), "lastUsedAt": item.LastUsedAt})
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"items": rows})
}

func (h *ToolCredentialHandler) HandleCreate(w http.ResponseWriter, r *http.Request) {
	u, ok := middleware.GetUserFromContext(r.Context())
	if !ok {
		http.Error(w, "Unauthorized", 401)
		return
	}
	var req struct {
		Name         string    `json:"name"`
		Scopes       []string  `json:"scopes"`
		PathPrefixes []string  `json:"pathPrefixes"`
		ExpiresAt    time.Time `json:"expiresAt"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", 400)
		return
	}
	credentialID := "wt_" + uuid.NewString()
	secret, hash, err := h.newSecret(credentialID)
	if err != nil {
		http.Error(w, "Failed to generate secret", 500)
		return
	}
	item, err := toolcredential.New(u.ID, strings.TrimSpace(req.Name), hash, req.Scopes, req.PathPrefixes, req.ExpiresAt)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	item.ID = credentialID
	if err := h.repo.Create(r.Context(), item); err != nil {
		h.logger.Error("create tool credential", zap.Error(err))
		http.Error(w, "Failed to create tool credential", 500)
		return
	}
	h.recordAudit(r, item, "credential.create", "", "success")
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"id": item.ID, "name": item.Name, "secret": secret, "scopes": item.Scopes, "pathPrefixes": item.PathPrefixes, "expiresAt": item.ExpiresAt.Format(timeLayout), "warning": "The secret is shown once and cannot be recovered."})
}

func (h *ToolCredentialHandler) HandleRotate(w http.ResponseWriter, r *http.Request) {
	u, ok := middleware.GetUserFromContext(r.Context())
	if !ok {
		http.Error(w, "Unauthorized", 401)
		return
	}
	id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/v1/public/tools/credentials/"), "/rotate")
	item, err := h.repo.FindByID(r.Context(), id)
	if errors.Is(err, sql.ErrNoRows) || item == nil || item.OwnerUserID != u.ID {
		http.Error(w, "Tool credential not found", 404)
		return
	}
	if err != nil {
		http.Error(w, "Failed to find tool credential", 500)
		return
	}
	var req struct {
		ExpiresAt *time.Time `json:"expiresAt"`
	}
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}
	expiresAt := item.ExpiresAt
	if req.ExpiresAt != nil {
		expiresAt = *req.ExpiresAt
	}
	if !expiresAt.After(time.Now()) {
		http.Error(w, "expiresAt must be in the future", 400)
		return
	}
	secret, hash, err := h.newSecret(item.ID)
	if err != nil {
		http.Error(w, "Failed to generate secret", 500)
		return
	}
	if err := h.repo.RotateByID(r.Context(), u.ID, item.ID, hash, expiresAt); err != nil {
		h.logger.Error("rotate tool credential", zap.Error(err))
		http.Error(w, "Failed to rotate tool credential", 500)
		return
	}
	h.recordAudit(r, item, "credential.rotate", "", "success")
	_ = json.NewEncoder(w).Encode(map[string]any{"id": item.ID, "secret": secret, "expiresAt": expiresAt.Format(timeLayout), "warning": "The previous secret is revoked and this secret is shown once."})
}

func (h *ToolCredentialHandler) HandleAudit(w http.ResponseWriter, r *http.Request) {
	u, ok := middleware.GetUserFromContext(r.Context())
	if !ok {
		http.Error(w, "Unauthorized", 401)
		return
	}
	credentialID := strings.TrimSpace(r.URL.Query().Get("credentialId"))
	items, err := h.repo.ListAuditByOwner(r.Context(), u.ID, credentialID, 100)
	if err != nil {
		h.logger.Error("list tool credential audits", zap.Error(err))
		http.Error(w, "Failed to list tool credential audits", 500)
		return
	}
	rows := make([]map[string]any, 0, len(items))
	for _, item := range items {
		rows = append(rows, map[string]any{"id": item.ID, "credentialId": item.CredentialID, "toolName": item.ToolName, "action": item.Action, "path": item.Path, "outcome": item.Outcome, "requestId": item.RequestID, "traceId": item.TraceID, "createdAt": item.CreatedAt.Format(timeLayout)})
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"items": rows})
}

func (h *ToolCredentialHandler) HandleRevoke(w http.ResponseWriter, r *http.Request) {
	u, ok := middleware.GetUserFromContext(r.Context())
	if !ok {
		http.Error(w, "Unauthorized", 401)
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/api/v1/public/tools/credentials/")
	id = strings.TrimSuffix(id, "/revoke")
	if id == "" {
		http.Error(w, "credential id is required", 400)
		return
	}
	if err := h.repo.RevokeByID(r.Context(), u.ID, id); errors.Is(err, sql.ErrNoRows) {
		http.Error(w, "Tool credential not found", 404)
		return
	} else if err != nil {
		h.logger.Error("revoke tool credential", zap.Error(err))
		http.Error(w, "Failed to revoke tool credential", 500)
		return
	}
	if item, findErr := h.repo.FindByID(r.Context(), id); findErr == nil {
		h.recordAudit(r, item, "credential.revoke", "", "success")
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *ToolCredentialHandler) newSecret(id string) (string, string, error) {
	secretBytes := make([]byte, 32)
	if _, err := rand.Read(secretBytes); err != nil {
		return "", "", err
	}
	secret := "wts_" + strings.TrimPrefix(id, "wt_") + "_" + base64.RawURLEncoding.EncodeToString(secretBytes)
	return secret, h.tokenHasher.Hash(secret), nil
}

func (h *ToolCredentialHandler) recordAudit(r *http.Request, item *toolcredential.Credential, action, path, outcome string) {
	if item == nil {
		return
	}
	_ = h.repo.RecordAudit(r.Context(), &toolcredential.AuditEvent{ID: "wta_" + uuid.NewString(), CredentialID: item.ID, OwnerUserID: item.OwnerUserID, Action: action, Path: path, Outcome: outcome, RequestID: middleware.RequestID(r.Context()), TraceID: strings.TrimSpace(r.Header.Get("X-Trace-ID")), CreatedAt: time.Now()})
}
