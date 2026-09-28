package middleware

import (
	"context"
	"net/http"
	"strings"

	"github.com/yeying-community/warehouse/internal/domain/auth"
	"github.com/yeying-community/warehouse/internal/domain/user"
	"go.uber.org/zap"
)

// contextKey 上下文键类型
type contextKey string

const (
	// UserContextKey 用户上下文键
	UserContextKey contextKey = "user"
)

// AuthMiddleware 认证中间件
type AuthMiddleware struct {
	authenticators []auth.Authenticator
	required       bool
	webdavPrefix   string
	logger         *zap.Logger
}

// NewAuthMiddleware 创建认证中间件
func NewAuthMiddleware(authenticators []auth.Authenticator, required bool, webdavPrefix string, logger *zap.Logger) *AuthMiddleware {
	return &AuthMiddleware{
		authenticators: authenticators,
		required:       required,
		webdavPrefix:   webdavPrefix,
		logger:         logger,
	}
}

// Handle 处理认证
func (m *AuthMiddleware) Handle(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		// OPTIONS 请求不需要认证
		if r.Method == "OPTIONS" {
			next.ServeHTTP(w, r)
			return
		}

		// 尝试从请求中提取凭证
		credentials := m.extractCredentials(r)
		if credentials == nil {
			if m.required {
				m.logger.Debug("no credentials provided")
				m.sendUnauthorized(w, r, "Authentication required")
				return
			}
			// 不需要认证，继续
			next.ServeHTTP(w, r)
			return
		}

		// 尝试使用所有认证器进行认证
		u, authenticator, err := m.authenticate(ctx, credentials)
		if err != nil {
			m.logger.Warn("authentication failed", zap.Error(err))
			m.sendUnauthorized(w, r, "Authentication failed")
			return
		}

		if enricher, ok := authenticator.(auth.ContextEnricher); ok {
			ctx = enricher.EnrichContext(ctx, credentials)
		}
		if _, ok := GetToolCredentialContext(ctx); ok {
			allowedToolPath := r.URL.Path == "/api/v1/public/tools/warehouse" || r.URL.Path == "/api/v1/public/tools/warehouse/call"
			if !allowedToolPath {
				http.Error(w, "Tool credential is only allowed for asset Tool paths", http.StatusForbidden)
				return
			}
		}

		// 将用户信息放入上下文
		ctx = context.WithValue(ctx, UserContextKey, u)
		r = r.WithContext(ctx)

		// access key 仅允许用于 WebDAV 路径，避免扩大 API 暴露面
		if _, isAccessKey := GetAccessKeyContext(ctx); isAccessKey {
			if !isAccessKeyRequestAllowed(r, m.webdavPrefix) {
				http.Error(w, "Access key is only allowed for WebDAV path", http.StatusForbidden)
				return
			}
		}

		m.logger.Debug("user authenticated", zap.String("username", u.Username))

		next.ServeHTTP(w, r)
	})
}

// authenticate 认证用户
func (m *AuthMiddleware) authenticate(ctx context.Context, credentials interface{}) (*user.User, auth.Authenticator, error) {
	// 遍历所有认证器
	for _, authenticator := range m.authenticators {
		// 检查是否可以处理该凭证
		if !authenticator.CanHandle(credentials) {
			continue
		}

		m.logger.Debug("trying authenticator",
			zap.String("authenticator", authenticator.Name()))

		// 尝试认证
		u, err := authenticator.Authenticate(ctx, credentials)
		if err != nil {
			m.logger.Debug("authentication failed",
				zap.String("authenticator", authenticator.Name()),
				zap.Error(err))
			return nil, nil, err
		}

		m.logger.Debug("authentication successful",
			zap.String("authenticator", authenticator.Name()),
			zap.String("username", u.Username))

		return u, authenticator, nil
	}

	return nil, nil, auth.ErrInvalidCredentials
}

// extractCredentials 提取凭证
func (m *AuthMiddleware) extractCredentials(r *http.Request) interface{} {
	// 1. 尝试 Bearer Token (Authorization header)
	authHeader := r.Header.Get("Authorization")
	if strings.HasPrefix(authHeader, "Bearer ") {
		token := strings.TrimPrefix(authHeader, "Bearer ")
		return &auth.BearerCredentials{Token: token}
	}

	// 2. 尝试从 Cookie 读取 token
	if cookie, err := r.Cookie("authToken"); err == nil {
		return &auth.BearerCredentials{Token: cookie.Value}
	}

	// 3. 尝试 Basic Auth
	username, password, ok := r.BasicAuth()
	if ok {
		return &auth.BasicCredentials{
			Username: username,
			Password: password,
		}
	}

	return nil
}

// sendUnauthorized 发送未授权响应
func (m *AuthMiddleware) sendUnauthorized(w http.ResponseWriter, r *http.Request, message string) {
	if isWebDAVRequest(r) {
		w.Header().Set("WWW-Authenticate", `Basic realm="WebDAV"`)
	}
	// 默认不设置 WWW-Authenticate，避免浏览器弹出 Basic Auth 对话框
	http.Error(w, message, http.StatusUnauthorized)
}

func isWebDAVRequest(r *http.Request) bool {
	if r == nil {
		return false
	}
	method := strings.ToUpper(r.Method)
	switch method {
	case "PROPFIND", "PROPPATCH", "MKCOL", "COPY", "MOVE", "LOCK", "UNLOCK", "REPORT", "SEARCH":
		return true
	}
	ua := strings.ToLower(r.UserAgent())
	if strings.Contains(ua, "webdav") || strings.Contains(ua, "davfs") {
		return true
	}
	if r.Header.Get("Depth") != "" || r.Header.Get("Destination") != "" || r.Header.Get("Lock-Token") != "" {
		return true
	}
	return false
}

func isWebDAVPath(rawPath, webdavPrefix string) bool {
	prefix := normalizeWebDAVPrefix(webdavPrefix)

	path := strings.TrimSpace(rawPath)
	if path == "" {
		path = "/"
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}

	if prefix == "/" {
		return true
	}
	return path == prefix || strings.HasPrefix(path, prefix+"/")
}

func isAccessKeyRequestAllowed(r *http.Request, webdavPrefix string) bool {
	if !isWebDAVPath(r.URL.Path, webdavPrefix) {
		return false
	}
	if normalizeWebDAVPrefix(webdavPrefix) != "/" {
		return true
	}
	path := strings.TrimSpace(r.URL.Path)
	if path == "" {
		path = "/"
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	// WebDAV 挂载到根路径时，显式拦截 API 命名空间。
	return !strings.HasPrefix(path, "/api/")
}

func normalizeWebDAVPrefix(webdavPrefix string) string {
	prefix := strings.TrimSpace(webdavPrefix)
	if prefix == "" {
		return "/"
	}
	if !strings.HasPrefix(prefix, "/") {
		prefix = "/" + prefix
	}
	prefix = strings.TrimSuffix(prefix, "/")
	if prefix == "" {
		return "/"
	}
	return prefix
}

// GetUserFromContext 从上下文获取用户
func GetUserFromContext(ctx context.Context) (*user.User, bool) {
	u, ok := ctx.Value(UserContextKey).(*user.User)
	return u, ok
}
