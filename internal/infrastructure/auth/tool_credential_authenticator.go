package auth

import (
	"context"
	"strings"
	"time"

	domainauth "github.com/yeying-community/warehouse/internal/domain/auth"
	"github.com/yeying-community/warehouse/internal/domain/user"
	"github.com/yeying-community/warehouse/internal/infrastructure/crypto"
	"github.com/yeying-community/warehouse/internal/infrastructure/repository"
	"github.com/yeying-community/warehouse/internal/interface/http/middleware"
	"go.uber.org/zap"
)

type ToolCredentialAuthenticator struct {
	userRepo    user.Repository
	repo        repository.ToolCredentialRepository
	tokenHasher *crypto.TokenHasher
	logger      *zap.Logger
}

func NewToolCredentialAuthenticator(userRepo user.Repository, repo repository.ToolCredentialRepository, logger *zap.Logger) *ToolCredentialAuthenticator {
	return &ToolCredentialAuthenticator{userRepo: userRepo, repo: repo, tokenHasher: crypto.NewTokenHasher(), logger: logger}
}
func (a *ToolCredentialAuthenticator) Name() string { return "warehouse-tool-credential" }
func (a *ToolCredentialAuthenticator) CanHandle(credentials interface{}) bool {
	c, ok := credentials.(*domainauth.BearerCredentials)
	return ok && strings.HasPrefix(strings.TrimSpace(c.Token), "wts_")
}
func (a *ToolCredentialAuthenticator) Authenticate(ctx context.Context, credentials interface{}) (*user.User, error) {
	c, ok := credentials.(*domainauth.BearerCredentials)
	if !ok {
		return nil, domainauth.ErrInvalidCredentials
	}
	token := strings.TrimSpace(c.Token)
	parts := strings.SplitN(strings.TrimPrefix(token, "wts_"), "_", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return nil, domainauth.ErrInvalidCredentials
	}
	item, err := a.repo.FindByID(ctx, "wt_"+parts[0])
	if err != nil {
		return nil, domainauth.ErrInvalidCredentials
	}
	if !item.IsUsable(time.Now()) || a.tokenHasher.Verify(item.SecretHash, token) != nil {
		return nil, domainauth.ErrInvalidCredentials
	}
	owner, err := a.userRepo.FindByID(ctx, item.OwnerUserID)
	if err != nil {
		return nil, err
	}
	if err := a.repo.TouchByID(ctx, item.ID, time.Now()); err != nil {
		a.logger.Warn("touch tool credential", zap.Error(err))
	}
	return owner, nil
}
func (a *ToolCredentialAuthenticator) EnrichContext(ctx context.Context, credentials interface{}) context.Context {
	c, ok := credentials.(*domainauth.BearerCredentials)
	if !ok {
		return ctx
	}
	token := strings.TrimPrefix(strings.TrimSpace(c.Token), "wts_")
	parts := strings.SplitN(token, "_", 2)
	if len(parts) != 2 {
		return ctx
	}
	item, err := a.repo.FindByID(ctx, "wt_"+parts[0])
	if err != nil {
		return ctx
	}
	return middleware.WithToolCredentialContext(ctx, &middleware.ToolCredentialContext{CredentialID: item.ID, Scopes: item.Scopes, PathPrefixes: item.PathPrefixes})
}
