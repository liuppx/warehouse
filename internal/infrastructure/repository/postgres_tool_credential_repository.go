package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/yeying-community/warehouse/internal/domain/toolcredential"
)

type ToolCredentialRepository interface {
	Create(context.Context, *toolcredential.Credential) error
	ListByOwner(context.Context, string) ([]*toolcredential.Credential, error)
	FindByID(context.Context, string) (*toolcredential.Credential, error)
	RevokeByID(context.Context, string, string) error
	TouchByID(context.Context, string, time.Time) error
	RotateByID(context.Context, string, string, string, time.Time) error
	RecordAudit(context.Context, *toolcredential.AuditEvent) error
	ListAuditByOwner(context.Context, string, string, int) ([]*toolcredential.AuditEvent, error)
}

func (r *PostgresToolCredentialRepository) FindByID(ctx context.Context, id string) (*toolcredential.Credential, error) {
	item := new(toolcredential.Credential)
	var scopes, paths pq.StringArray
	err := r.db.QueryRowContext(ctx, `SELECT id, owner_user_id, name, secret_hash, scopes, path_prefixes, status, expires_at, last_used_at, created_at, updated_at FROM warehouse_tool_credentials WHERE id=$1`, id).Scan(&item.ID, &item.OwnerUserID, &item.Name, &item.SecretHash, &scopes, &paths, &item.Status, &item.ExpiresAt, &item.LastUsedAt, &item.CreatedAt, &item.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, sql.ErrNoRows
	}
	if err != nil {
		return nil, fmt.Errorf("find tool credential: %w", err)
	}
	item.Scopes, item.PathPrefixes = []string(scopes), []string(paths)
	return item, nil
}

type PostgresToolCredentialRepository struct{ db *sql.DB }

func NewPostgresToolCredentialRepository(db *sql.DB) *PostgresToolCredentialRepository {
	return &PostgresToolCredentialRepository{db: db}
}

func (r *PostgresToolCredentialRepository) Create(ctx context.Context, item *toolcredential.Credential) error {
	if item.ID == "" {
		item.ID = "wt_" + uuid.NewString()
	}
	_, err := r.db.ExecContext(ctx, `INSERT INTO warehouse_tool_credentials
		(id, owner_user_id, name, secret_hash, scopes, path_prefixes, status, expires_at, last_used_at, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`, item.ID, item.OwnerUserID, item.Name,
		item.SecretHash, pq.Array(item.Scopes), pq.Array(item.PathPrefixes), item.Status, item.ExpiresAt,
		item.LastUsedAt, item.CreatedAt, item.UpdatedAt)
	if err != nil {
		return fmt.Errorf("create tool credential: %w", err)
	}
	return nil
}

func (r *PostgresToolCredentialRepository) ListByOwner(ctx context.Context, ownerID string) ([]*toolcredential.Credential, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id, owner_user_id, name, secret_hash, scopes, path_prefixes, status, expires_at, last_used_at, created_at, updated_at FROM warehouse_tool_credentials WHERE owner_user_id=$1 ORDER BY created_at DESC`, ownerID)
	if err != nil {
		return nil, fmt.Errorf("list tool credentials: %w", err)
	}
	defer rows.Close()
	items := make([]*toolcredential.Credential, 0)
	for rows.Next() {
		item := new(toolcredential.Credential)
		var scopes, paths pq.StringArray
		if err := rows.Scan(&item.ID, &item.OwnerUserID, &item.Name, &item.SecretHash, &scopes, &paths, &item.Status, &item.ExpiresAt, &item.LastUsedAt, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan tool credential: %w", err)
		}
		item.Scopes, item.PathPrefixes = []string(scopes), []string(paths)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate tool credentials: %w", err)
	}
	return items, nil
}

func (r *PostgresToolCredentialRepository) RevokeByID(ctx context.Context, ownerID, id string) error {
	result, err := r.db.ExecContext(ctx, `UPDATE warehouse_tool_credentials SET status=$1, updated_at=NOW() WHERE owner_user_id=$2 AND id=$3`, toolcredential.StatusRevoked, ownerID, id)
	if err != nil {
		return fmt.Errorf("revoke tool credential: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("check revoked tool credential: %w", err)
	}
	if count == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (r *PostgresToolCredentialRepository) TouchByID(ctx context.Context, id string, usedAt time.Time) error {
	_, err := r.db.ExecContext(ctx, `UPDATE warehouse_tool_credentials SET last_used_at=$1, updated_at=$1 WHERE id=$2`, usedAt, id)
	return err
}

func (r *PostgresToolCredentialRepository) RotateByID(ctx context.Context, ownerID, id, secretHash string, expiresAt time.Time) error {
	result, err := r.db.ExecContext(ctx, `UPDATE warehouse_tool_credentials SET secret_hash=$1, expires_at=$2, status=$3, updated_at=NOW() WHERE owner_user_id=$4 AND id=$5`, secretHash, expiresAt, toolcredential.StatusActive, ownerID, id)
	if err != nil {
		return fmt.Errorf("rotate tool credential: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (r *PostgresToolCredentialRepository) RecordAudit(ctx context.Context, event *toolcredential.AuditEvent) error {
	_, err := r.db.ExecContext(ctx, `INSERT INTO warehouse_tool_credential_audits (id, credential_id, owner_user_id, tool_name, action, path, outcome, request_id, trace_id, created_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, event.ID, event.CredentialID, event.OwnerUserID, event.ToolName, event.Action, event.Path, event.Outcome, event.RequestID, event.TraceID, event.CreatedAt)
	if err != nil {
		return fmt.Errorf("record tool credential audit: %w", err)
	}
	return nil
}

func (r *PostgresToolCredentialRepository) ListAuditByOwner(ctx context.Context, ownerID, credentialID string, limit int) ([]*toolcredential.AuditEvent, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	query := `SELECT id, credential_id, owner_user_id, tool_name, action, path, outcome, request_id, trace_id, created_at FROM warehouse_tool_credential_audits WHERE owner_user_id=$1`
	args := []any{ownerID}
	if credentialID != "" {
		query += ` AND credential_id=$2`
		args = append(args, credentialID)
	}
	query += ` ORDER BY created_at DESC LIMIT ` + fmt.Sprint(limit)
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list tool credential audits: %w", err)
	}
	defer rows.Close()
	items := make([]*toolcredential.AuditEvent, 0)
	for rows.Next() {
		item := new(toolcredential.AuditEvent)
		if err := rows.Scan(&item.ID, &item.CredentialID, &item.OwnerUserID, &item.ToolName, &item.Action, &item.Path, &item.Outcome, &item.RequestID, &item.TraceID, &item.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}
