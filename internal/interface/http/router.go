package http

import (
	"net/http"
	"strings"

	"github.com/yeying-community/warehouse/internal/domain/auth"
	"github.com/yeying-community/warehouse/internal/infrastructure/config"
	"github.com/yeying-community/warehouse/internal/interface/http/handler"
	"github.com/yeying-community/warehouse/internal/interface/http/middleware"
	"go.uber.org/zap"
)

// Router HTTP 路由器
type Router struct {
	config                     *config.Config
	authenticators             []auth.Authenticator
	healthHandler              *handler.HealthHandler
	internalReplicationHandler *handler.InternalReplicationHandler
	web3Handler                *handler.Web3Handler
	identityHandler            *handler.IdentityHandler
	emailAuthHandler           *handler.EmailAuthHandler
	assetsHandler              *handler.AssetsHandler
	assetObjectHandler         *handler.AssetObjectHandler
	assetToolHandler           *handler.AssetToolHandler
	webdavHandler              *handler.WebDAVHandler
	quotaHandler               *handler.QuotaHandler
	userHandler                *handler.UserHandler
	adminUserHandler           *handler.AdminUserHandler
	recycleHandler             *handler.RecycleHandler
	shareHandler               *handler.ShareHandler
	shareUserHandler           *handler.ShareUserHandler
	webdavAccessKeyHandler     *handler.WebDAVAccessKeyHandler
	groupHandler               *handler.GroupHandler
	notificationHandler        *handler.NotificationHandler
	s3CredentialHandler        *handler.S3CredentialHandler
	uploadSessionHandler       *handler.UploadSessionHandler
	toolCredentialHandler      *handler.ToolCredentialHandler
	logger                     *zap.Logger
}

// NewRouter 创建路由器
func NewRouter(
	cfg *config.Config,
	authenticators []auth.Authenticator,
	healthHandler *handler.HealthHandler,
	internalReplicationHandler *handler.InternalReplicationHandler,
	web3Handler *handler.Web3Handler,
	identityHandler *handler.IdentityHandler,
	emailAuthHandler *handler.EmailAuthHandler,
	assetsHandler *handler.AssetsHandler,
	assetObjectHandler *handler.AssetObjectHandler,
	assetToolHandler *handler.AssetToolHandler,
	webdavHandler *handler.WebDAVHandler,
	quotaHandler *handler.QuotaHandler,
	userHandler *handler.UserHandler,
	adminUserHandler *handler.AdminUserHandler,
	recycleHandler *handler.RecycleHandler,
	shareHandler *handler.ShareHandler,
	shareUserHandler *handler.ShareUserHandler,
	webdavAccessKeyHandler *handler.WebDAVAccessKeyHandler,
	groupHandler *handler.GroupHandler,
	notificationHandler *handler.NotificationHandler,
	s3CredentialHandler *handler.S3CredentialHandler,
	uploadSessionHandler *handler.UploadSessionHandler,
	toolCredentialHandler *handler.ToolCredentialHandler,
	logger *zap.Logger,
) *Router {
	return &Router{
		config:                     cfg,
		authenticators:             authenticators,
		healthHandler:              healthHandler,
		internalReplicationHandler: internalReplicationHandler,
		web3Handler:                web3Handler,
		identityHandler:            identityHandler,
		emailAuthHandler:           emailAuthHandler,
		assetsHandler:              assetsHandler,
		assetObjectHandler:         assetObjectHandler,
		assetToolHandler:           assetToolHandler,
		webdavHandler:              webdavHandler,
		quotaHandler:               quotaHandler,
		userHandler:                userHandler,
		adminUserHandler:           adminUserHandler,
		recycleHandler:             recycleHandler,
		shareHandler:               shareHandler,
		shareUserHandler:           shareUserHandler,
		webdavAccessKeyHandler:     webdavAccessKeyHandler,
		groupHandler:               groupHandler,
		notificationHandler:        notificationHandler,
		s3CredentialHandler:        s3CredentialHandler,
		uploadSessionHandler:       uploadSessionHandler,
		toolCredentialHandler:      toolCredentialHandler,
		logger:                     logger,
	}
}

// Setup 设置路由
func (r *Router) Setup() http.Handler {
	mux := http.NewServeMux()

	// 健康检查路由（无需认证）
	mux.HandleFunc("/api/v1/public/health/heartbeat", r.healthHandler.Handle)
	mux.HandleFunc("/api/v1/public/health/readiness", r.healthHandler.HandleReadiness)
	if r.internalReplicationHandler != nil {
		mux.Handle("/api/v1/internal/replication/fs/apply", r.createInternalHandler(http.HandlerFunc(r.internalReplicationHandler.HandleFSApply)))
		mux.Handle("/api/v1/internal/replication/file", r.createInternalHandler(http.HandlerFunc(r.internalReplicationHandler.HandleFileApply)))
		mux.Handle("/api/v1/internal/replication/status", r.createInternalHandler(http.HandlerFunc(r.internalReplicationHandler.HandleStatus)))
		mux.Handle("/api/v1/internal/replication/bootstrap/mark", r.createInternalHandler(http.HandlerFunc(r.internalReplicationHandler.HandleBootstrapMark)))
		mux.Handle("/api/v1/internal/replication/reconcile/start", r.createInternalHandler(http.HandlerFunc(r.internalReplicationHandler.HandleReconcileStart)))
		mux.Handle("/api/v1/internal/replication/reconcile/apply-batch", r.createInternalHandler(http.HandlerFunc(r.internalReplicationHandler.HandleReconcileApplyBatch)))
	}

	// Web3 认证路由（无需认证）
	mux.HandleFunc("/api/v1/public/auth/challenge", r.web3Handler.HandleChallenge)
	mux.HandleFunc("/api/v1/public/auth/verify", r.web3Handler.HandleVerify)
	mux.HandleFunc("/api/v1/public/auth/refresh", r.web3Handler.HandleRefresh)
	mux.HandleFunc("/api/v1/public/auth/logout", r.web3Handler.HandleLogout)
	mux.HandleFunc("/api/v1/public/auth/password/login", r.web3Handler.HandlePasswordLogin)
	if r.identityHandler != nil {
		mux.HandleFunc("/api/v1/public/auth/identity/login/session", r.identityHandler.HandleIdentityLoginSession)
		mux.HandleFunc("/api/v1/public/auth/identity/login/verify", r.identityHandler.HandleIdentityLoginVerify)
		mux.HandleFunc("/api/v1/public/auth/identity/session", r.identityHandler.HandleSession)
		mux.HandleFunc("/api/v1/public/auth/identity/status", r.identityHandler.HandleStatus)
		mux.HandleFunc("/api/v1/public/auth/identity/callback", r.identityHandler.HandleCallback)
	}
	if r.emailAuthHandler != nil {
		mux.HandleFunc("/api/v1/public/auth/email/code", r.emailAuthHandler.HandleSendCode)
		mux.HandleFunc("/api/v1/public/auth/email/login", r.emailAuthHandler.HandleLogin)
	}

	// API 路由（需要认证）
	if r.assetsHandler != nil {
		mux.Handle("/api/v1/public/assets/spaces", r.createAuthenticatedHandler(http.HandlerFunc(r.assetsHandler.GetSpaces)))
	}
	if r.assetObjectHandler != nil {
		mux.Handle("/api/v1/public/assets/object", r.createAuthenticatedHandler(http.HandlerFunc(r.assetObjectHandler.HandleObject)))
		mux.Handle("/api/v1/public/assets/object/content", r.createAuthenticatedHandler(http.HandlerFunc(r.assetObjectHandler.HandleObjectContent)))
		mux.Handle("/api/v1/public/assets/objects", r.createAuthenticatedHandler(http.HandlerFunc(r.assetObjectHandler.HandleObjects)))
	}
	if r.assetToolHandler != nil {
		mux.Handle("/api/v1/public/tools/warehouse", r.createAuthenticatedHandler(http.HandlerFunc(r.assetToolHandler.HandleCatalog)))
		mux.Handle("/api/v1/public/tools/warehouse/call", r.createAuthenticatedHandler(http.HandlerFunc(r.assetToolHandler.HandleCall)))
	}
	mux.Handle("/api/v1/public/webdav/quota", r.createAuthenticatedHandler(http.HandlerFunc(r.quotaHandler.GetUserQuota)))
	mux.Handle("/api/v1/public/webdav/user/info", r.createAuthenticatedHandler(http.HandlerFunc(r.userHandler.GetUserInfo)))
	mux.Handle("/api/v1/public/webdav/user/password", r.createAuthenticatedHandler(http.HandlerFunc(r.userHandler.UpdatePassword)))
	if r.notificationHandler != nil {
		mux.Handle("/api/v1/public/notifications/list", r.createAuthenticatedHandler(http.HandlerFunc(r.notificationHandler.HandleList)))
		mux.Handle("/api/v1/public/notifications/unread-count", r.createAuthenticatedHandler(http.HandlerFunc(r.notificationHandler.HandleUnreadCount)))
		mux.Handle("/api/v1/public/notifications/read", r.createAuthenticatedHandler(http.HandlerFunc(r.notificationHandler.HandleMarkRead)))
		mux.Handle("/api/v1/public/notifications/read-all", r.createAuthenticatedHandler(http.HandlerFunc(r.notificationHandler.HandleMarkAllRead)))
		mux.Handle("/api/v1/public/notifications/preferences", r.createAuthenticatedHandler(http.HandlerFunc(r.notificationHandler.HandlePreferences)))
		mux.Handle("/api/v1/public/notifications/stream", r.createAuthenticatedHandler(http.HandlerFunc(r.notificationHandler.HandleStream)))
	}

	// 管理员用户管理（需要认证 + 管理员权限）
	mux.Handle("/api/v1/admin/users/list", r.createAdminHandler(http.HandlerFunc(r.adminUserHandler.HandleList)))
	mux.Handle("/api/v1/admin/users/create", r.createAdminHandler(http.HandlerFunc(r.adminUserHandler.HandleCreate)))
	mux.Handle("/api/v1/admin/users/update", r.createAdminHandler(http.HandlerFunc(r.adminUserHandler.HandleUpdate)))
	mux.Handle("/api/v1/admin/users/delete", r.createAdminHandler(http.HandlerFunc(r.adminUserHandler.HandleDelete)))
	mux.Handle("/api/v1/admin/users/reset-password", r.createAdminHandler(http.HandlerFunc(r.adminUserHandler.HandleResetPassword)))
	if r.notificationHandler != nil {
		mux.Handle("/api/v1/admin/notifications/list", r.createAdminHandler(http.HandlerFunc(r.notificationHandler.HandleAdminList)))
		mux.Handle("/api/v1/admin/notifications/unread-count", r.createAdminHandler(http.HandlerFunc(r.notificationHandler.HandleAdminUnreadCount)))
		mux.Handle("/api/v1/admin/notifications/read", r.createAdminHandler(http.HandlerFunc(r.notificationHandler.HandleAdminMarkRead)))
		mux.Handle("/api/v1/admin/notifications/read-all", r.createAdminHandler(http.HandlerFunc(r.notificationHandler.HandleAdminMarkAllRead)))
		mux.Handle("/api/v1/admin/notifications/create", r.createAdminHandler(http.HandlerFunc(r.notificationHandler.HandleAdminCreate)))
		mux.Handle("/api/v1/admin/notifications/stream", r.createAdminHandler(http.HandlerFunc(r.notificationHandler.HandleAdminStream)))
	}

	// 回收站路由
	mux.Handle("/api/v1/public/webdav/recycle/list", r.createAuthenticatedHandler(http.HandlerFunc(r.recycleHandler.HandleList)))
	mux.Handle("/api/v1/public/webdav/recycle/recover", r.createAuthenticatedHandler(http.HandlerFunc(r.recycleHandler.HandleRecover)))
	mux.Handle("/api/v1/public/webdav/recycle/permanent", r.createAuthenticatedHandler(http.HandlerFunc(r.recycleHandler.HandleRemove)))
	mux.Handle("/api/v1/public/webdav/recycle/clear", r.createAuthenticatedHandler(http.HandlerFunc(r.recycleHandler.HandleClear)))

	// 分组管理
	mux.Handle("/api/v1/public/webdav/group/groups", r.createAuthenticatedHandler(http.HandlerFunc(r.groupHandler.HandleGroupList)))
	mux.Handle("/api/v1/public/webdav/group/groups/create", r.createAuthenticatedHandler(http.HandlerFunc(r.groupHandler.HandleGroupCreate)))
	mux.Handle("/api/v1/public/webdav/group/groups/update", r.createAuthenticatedHandler(http.HandlerFunc(r.groupHandler.HandleGroupUpdate)))
	mux.Handle("/api/v1/public/webdav/group/groups/delete", r.createAuthenticatedHandler(http.HandlerFunc(r.groupHandler.HandleGroupDelete)))
	mux.Handle("/api/v1/public/webdav/group/members", r.createAuthenticatedHandler(http.HandlerFunc(r.groupHandler.HandleMemberList)))
	mux.Handle("/api/v1/public/webdav/group/members/create", r.createAuthenticatedHandler(http.HandlerFunc(r.groupHandler.HandleMemberCreate)))
	mux.Handle("/api/v1/public/webdav/group/members/update", r.createAuthenticatedHandler(http.HandlerFunc(r.groupHandler.HandleMemberUpdate)))
	mux.Handle("/api/v1/public/webdav/group/members/name", r.createAuthenticatedHandler(http.HandlerFunc(r.groupHandler.HandleMemberNameUpdate)))
	mux.Handle("/api/v1/public/webdav/group/members/alias", r.createAuthenticatedHandler(http.HandlerFunc(r.groupHandler.HandleMemberAliasUpdate)))
	mux.Handle("/api/v1/public/webdav/group/members/approve", r.createAuthenticatedHandler(http.HandlerFunc(r.groupHandler.HandleMemberApprove)))
	mux.Handle("/api/v1/public/webdav/group/members/reject", r.createAuthenticatedHandler(http.HandlerFunc(r.groupHandler.HandleMemberReject)))
	mux.Handle("/api/v1/public/webdav/group/members/delete", r.createAuthenticatedHandler(http.HandlerFunc(r.groupHandler.HandleMemberDelete)))
	if r.webdavAccessKeyHandler != nil {
		mux.Handle("/api/v1/public/webdav/access-keys/list", r.createAuthenticatedHandler(http.HandlerFunc(r.webdavAccessKeyHandler.HandleList)))
		mux.Handle("/api/v1/public/webdav/access-keys/create", r.createAuthenticatedHandler(http.HandlerFunc(r.webdavAccessKeyHandler.HandleCreate)))
		mux.Handle("/api/v1/public/webdav/access-keys/bind", r.createAuthenticatedHandler(http.HandlerFunc(r.webdavAccessKeyHandler.HandleBind)))
		mux.Handle("/api/v1/public/webdav/access-keys/revoke", r.createAuthenticatedHandler(http.HandlerFunc(r.webdavAccessKeyHandler.HandleRevoke)))
		mux.Handle("/api/v1/public/webdav/access-keys/delete", r.createAuthenticatedHandler(http.HandlerFunc(r.webdavAccessKeyHandler.HandleDelete)))
	}
	if r.s3CredentialHandler != nil {
		mux.Handle("/api/v1/public/s3/credentials/list", r.createAuthenticatedHandler(http.HandlerFunc(r.s3CredentialHandler.HandleList)))
		mux.Handle("/api/v1/public/s3/credentials/create", r.createAuthenticatedHandler(http.HandlerFunc(r.s3CredentialHandler.HandleCreate)))
		mux.Handle("/api/v1/public/s3/credentials/revoke", r.createAuthenticatedHandler(http.HandlerFunc(r.s3CredentialHandler.HandleRevoke)))
		mux.Handle("/api/v1/public/s3/credentials/delete", r.createAuthenticatedHandler(http.HandlerFunc(r.s3CredentialHandler.HandleDelete)))
	}
	if r.toolCredentialHandler != nil {
		mux.Handle("/api/v1/public/tools/credentials", r.createAuthenticatedHandler(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			switch req.Method {
			case http.MethodGet:
				r.toolCredentialHandler.HandleList(w, req)
			case http.MethodPost:
				r.toolCredentialHandler.HandleCreate(w, req)
			default:
				w.WriteHeader(http.StatusMethodNotAllowed)
			}
		})))
		mux.Handle("/api/v1/public/tools/credentials/", r.createAuthenticatedHandler(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			if req.Method != http.MethodPost {
				w.WriteHeader(http.StatusMethodNotAllowed)
				return
			}
			if strings.HasSuffix(req.URL.Path, "/rotate") {
				r.toolCredentialHandler.HandleRotate(w, req)
				return
			}
			r.toolCredentialHandler.HandleRevoke(w, req)
		})))
		mux.Handle("/api/v1/public/tools/audits", r.createAuthenticatedHandler(http.HandlerFunc(r.toolCredentialHandler.HandleAudit)))
	}
	if r.uploadSessionHandler != nil {
		mux.Handle("/api/v1/public/uploads/sessions", r.createAuthenticatedHandler(http.HandlerFunc(r.uploadSessionHandler.HandleCreate)))
		mux.Handle("/api/v1/public/uploads/sessions/", r.createAuthenticatedHandler(http.HandlerFunc(r.uploadSessionHandler.HandleItem)))
	}

	// 分享路由
	mux.Handle("/api/v1/public/share/create", r.createAuthenticatedHandler(http.HandlerFunc(r.shareHandler.HandleCreate)))
	mux.Handle("/api/v1/public/share/create-from-resource", r.createAuthenticatedHandler(http.HandlerFunc(r.shareHandler.HandleCreateFromReceivedResource)))
	mux.Handle("/api/v1/public/share/list", r.createAuthenticatedHandler(http.HandlerFunc(r.shareHandler.HandleList)))
	mux.Handle("/api/v1/public/share/revoke", r.createAuthenticatedHandler(http.HandlerFunc(r.shareHandler.HandleRevoke)))
	mux.HandleFunc("/api/v1/public/share/", r.shareHandler.HandleAccess)

	// 定向分享路由（需要认证）
	mux.Handle("/api/v1/public/share/user/create", r.createAuthenticatedHandler(http.HandlerFunc(r.shareUserHandler.HandleCreate)))
	mux.Handle("/api/v1/public/share/user/list", r.createAuthenticatedHandler(http.HandlerFunc(r.shareUserHandler.HandleListMine)))
	mux.Handle("/api/v1/public/share/user/received", r.createAuthenticatedHandler(http.HandlerFunc(r.shareUserHandler.HandleListReceived)))
	mux.Handle("/api/v1/public/share/user/revoke", r.createAuthenticatedHandler(http.HandlerFunc(r.shareUserHandler.HandleRevoke)))
	mux.Handle("/api/v1/public/share/user/audiences", r.createAuthenticatedHandler(http.HandlerFunc(r.shareUserHandler.HandleListAudiences)))
	mux.Handle("/api/v1/public/share/user/entries", r.createAuthenticatedHandler(http.HandlerFunc(r.shareUserHandler.HandleEntries)))
	mux.Handle("/api/v1/public/share/resource/entries", r.createAuthenticatedHandler(http.HandlerFunc(r.shareUserHandler.HandleResourceEntries)))
	mux.Handle("/api/v1/public/share/resource/download", r.createAuthenticatedHandler(http.HandlerFunc(r.shareUserHandler.HandleResourceDownload)))
	mux.Handle("/api/v1/public/share/resource/folder", r.createAuthenticatedHandler(http.HandlerFunc(r.shareUserHandler.HandleResourceCreateFolder)))
	mux.Handle("/api/v1/public/share/resource/rename", r.createAuthenticatedHandler(http.HandlerFunc(r.shareUserHandler.HandleResourceRename)))
	mux.Handle("/api/v1/public/share/resource/delete", r.createAuthenticatedHandler(http.HandlerFunc(r.shareUserHandler.HandleResourceDelete)))
	mux.Handle("/api/v1/public/share/user/download", r.createAuthenticatedHandler(http.HandlerFunc(r.shareUserHandler.HandleDownload)))
	mux.Handle("/api/v1/public/share/user/upload", r.createAuthenticatedHandler(http.HandlerFunc(r.shareUserHandler.HandleUpload)))
	mux.Handle("/api/v1/public/share/user/folder", r.createAuthenticatedHandler(http.HandlerFunc(r.shareUserHandler.HandleCreateFolder)))
	mux.Handle("/api/v1/public/share/user/rename", r.createAuthenticatedHandler(http.HandlerFunc(r.shareUserHandler.HandleRename)))
	mux.Handle("/api/v1/public/share/user/item", r.createAuthenticatedHandler(http.HandlerFunc(r.shareUserHandler.HandleDelete)))

	// WebDAV 路由（需要认证）
	webdavPrefix := r.normalizePrefix(r.config.WebDAV.Prefix)
	mux.Handle(webdavPrefix+"share/", r.createAuthenticatedHandler(http.HandlerFunc(r.shareUserHandler.HandleDAV)))
	mux.Handle(webdavPrefix, r.createAuthenticatedHandler(http.HandlerFunc(r.webdavHandler.Handle)))

	// 应用全局中间件
	handler := r.applyMiddlewares(mux)

	return handler
}

// createAuthenticatedHandler 创建需要认证的处理器
func (r *Router) createAuthenticatedHandler(handler http.Handler) http.Handler {
	// 应用认证中间件
	authMiddleware := middleware.NewAuthMiddleware(r.authenticators, true, r.config.WebDAV.Prefix, r.logger)
	return authMiddleware.Handle(handler)
}

// createAdminHandler 创建管理员处理器
func (r *Router) createAdminHandler(handler http.Handler) http.Handler {
	adminMiddleware := middleware.NewAdminMiddleware(r.config.Security.AdminAddresses, r.logger)
	authMiddleware := middleware.NewAuthMiddleware(r.authenticators, true, r.config.WebDAV.Prefix, r.logger)
	return authMiddleware.Handle(adminMiddleware.Handle(handler))
}

// createInternalHandler creates handlers used only for internal service-to-service traffic.
func (r *Router) createInternalHandler(handler http.Handler) http.Handler {
	internalMiddleware := middleware.NewInternalAuthMiddleware(r.config.Replication, r.logger)
	return internalMiddleware.Handle(handler)
}

// applyMiddlewares 应用全局中间件
func (r *Router) applyMiddlewares(handler http.Handler) http.Handler {
	// 1. 恢复中间件（最外层）
	recoveryMiddleware := middleware.NewRecoveryMiddleware(r.logger)
	handler = recoveryMiddleware.Handle(handler)

	// 请求关联标识必须在日志和处理器之前建立
	handler = middleware.RequestIDMiddleware(handler)

	// 2. 日志中间件
	loggerMiddleware := middleware.NewLoggerMiddleware(r.logger, r.config.Security.BehindProxy)
	handler = loggerMiddleware.Handle(handler)

	// 3. CORS 中间件
	if r.config.CORS.Enabled {
		corsConfig := &middleware.CORSConfig{
			Enabled:        r.config.CORS.Enabled,
			Credentials:    r.config.CORS.Credentials,
			AllowedOrigins: r.config.CORS.AllowedOrigins,
			AllowedMethods: r.config.CORS.AllowedMethods,
			AllowedHeaders: r.config.CORS.AllowedHeaders,
			ExposedHeaders: r.config.CORS.ExposedHeaders,
		}
		corsMiddleware := middleware.NewCORSMiddleware(corsConfig)
		handler = corsMiddleware.Handle(handler)
	}

	return handler
}

// normalizePrefix 规范化前缀
func (r *Router) normalizePrefix(prefix string) string {
	if prefix == "" {
		return "/"
	}

	// 确保以 / 开头
	if !strings.HasPrefix(prefix, "/") {
		prefix = "/" + prefix
	}

	// 确保以 / 结尾
	if !strings.HasSuffix(prefix, "/") {
		prefix = prefix + "/"
	}

	return prefix
}
