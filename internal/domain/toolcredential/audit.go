package toolcredential

import "time"

type AuditEvent struct {
	ID           string
	CredentialID string
	OwnerUserID  string
	ToolName     string
	Action       string
	Path         string
	Outcome      string
	RequestID    string
	TraceID      string
	CreatedAt    time.Time
}
