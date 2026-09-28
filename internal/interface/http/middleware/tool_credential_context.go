package middleware

import "context"

type toolCredentialContextKey struct{}

type ToolCredentialContext struct {
	CredentialID string
	Scopes       []string
	PathPrefixes []string
}

func WithToolCredentialContext(ctx context.Context, value *ToolCredentialContext) context.Context {
	return context.WithValue(ctx, toolCredentialContextKey{}, value)
}
func GetToolCredentialContext(ctx context.Context) (*ToolCredentialContext, bool) {
	value, ok := ctx.Value(toolCredentialContextKey{}).(*ToolCredentialContext)
	return value, ok
}
