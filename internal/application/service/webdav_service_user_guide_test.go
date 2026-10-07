package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/yeying-community/warehouse/internal/domain/quota"
	"github.com/yeying-community/warehouse/internal/domain/user"
	"github.com/yeying-community/warehouse/internal/infrastructure/config"
	"github.com/yeying-community/warehouse/internal/interface/http/middleware"
	"go.uber.org/zap"
)

func TestWebDAVDoesNotExposeSidebarUserGuide(t *testing.T) {
	rootDir := t.TempDir()
	cfg := &config.Config{
		WebDAV: config.WebDAVConfig{
			Prefix:              "/dav",
			Directory:           rootDir,
			AutoCreateDirectory: true,
		},
	}

	userRepo := newTestUserRepo()
	u := user.NewUser("alice", "alice")
	u.Permissions = user.FullPermissions()
	if err := userRepo.Save(context.Background(), u); err != nil {
		t.Fatalf("save user: %v", err)
	}

	svc := NewWebDAVService(
		cfg,
		allowPermissionChecker{},
		quota.NewService(userRepo),
		userRepo,
		&testRecycleRepo{},
		nil,
		nil,
		zap.NewNop(),
	)

	for _, requestPath := range []string{
		"/dav/Warehouse 用户使用指南.md",
		"/dav/personal/Warehouse 用户使用指南.md",
		"/dav/apps/Warehouse 用户使用指南.md",
		"/dav/services/Warehouse 用户使用指南.md",
	} {
		t.Run(requestPath, func(t *testing.T) {
			target := (&url.URL{Path: requestPath}).String()
			req := httptest.NewRequest(http.MethodGet, target, nil)
			req = req.WithContext(context.WithValue(req.Context(), middleware.UserContextKey, u))
			resp := httptest.NewRecorder()

			svc.ServeHTTP(resp, req)

			if resp.Code != http.StatusNotFound {
				t.Fatalf("GET %s status = %d, want %d; body=%q",
					requestPath, resp.Code, http.StatusNotFound, resp.Body.String())
			}
		})
	}
}
