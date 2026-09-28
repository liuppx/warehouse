package container

import (
	"context"
	"fmt"
	"strings"

	"github.com/yeying-community/warehouse/internal/application/assetspace"
	"github.com/yeying-community/warehouse/internal/application/service"
	"github.com/yeying-community/warehouse/internal/domain/auth"
	"github.com/yeying-community/warehouse/internal/domain/quota"
	"github.com/yeying-community/warehouse/internal/domain/user"
	apphealth "github.com/yeying-community/warehouse/internal/health"
	infraAuth "github.com/yeying-community/warehouse/internal/infrastructure/auth"
	"github.com/yeying-community/warehouse/internal/infrastructure/config"
	infraCrypto "github.com/yeying-community/warehouse/internal/infrastructure/crypto"
	"github.com/yeying-community/warehouse/internal/infrastructure/database"
	infraEmail "github.com/yeying-community/warehouse/internal/infrastructure/email"
	"github.com/yeying-community/warehouse/internal/infrastructure/logger"
	"github.com/yeying-community/warehouse/internal/infrastructure/permission"
	"github.com/yeying-community/warehouse/internal/infrastructure/repository"
	"github.com/yeying-community/warehouse/internal/interface/http"
	"github.com/yeying-community/warehouse/internal/interface/http/handler"
	"github.com/yeying-community/warehouse/internal/interface/s3"
	"go.uber.org/zap"
	"golang.org/x/net/webdav"
)

// Container 依赖注入容器
type Container struct {
	Config *config.Config
	Logger *zap.Logger

	// Database
	DB *database.PostgresDB

	// Repositories
	UserRepository                user.Repository
	RecycleRepository             repository.RecycleRepository
	ShareRepository               repository.ShareRepository
	UserShareRepository           repository.UserShareRepository
	SharedResourceGrantRepository repository.SharedResourceGrantRepository
	GroupRepository               repository.GroupRepository
	WebDAVAccessKeyRepo           repository.WebDAVAccessKeyRepository
	S3CredentialRepo              repository.S3CredentialRepository
	ToolCredentialRepo            repository.ToolCredentialRepository
	S3MultipartRepo               repository.S3MultipartRepository
	S3ObjectMetadataRepo          repository.S3ObjectMetadataRepository
	NotificationRepo              repository.NotificationRepository
	ReplicationOutboxRepo         repository.ReplicationOutboxRepository
	ReplicationOffsetRepo         repository.ReplicationOffsetRepository
	ReconcileRepo                 repository.ReplicationReconcileRepository
	ClusterNodeRepo               repository.ClusterNodeRepository
	ClusterAssignmentRepo         repository.ClusterReplicationAssignmentRepository

	// Services
	QuotaService                quota.Service
	QuotaReconciler             *service.QuotaReconciler
	AssetSpaceManager           *assetspace.Manager
	MutationRecorder            service.MutationRecorder
	NodeHeartbeat               *service.NodeHeartbeatRegistrar
	PeerResolver                service.ReplicationPeerResolver
	AssignmentAllocator         *service.ReplicationAssignmentAllocator
	ReplicationWorker           *service.ReplicationWorker
	ReconcileScanner            *service.ReconcileScanner
	ReplicationCleaner          *service.ReplicationLifecycleCleaner
	WebDAVService               *service.WebDAVService
	RecycleService              *service.RecycleService
	ShareService                *service.ShareService
	ShareUserService            *service.ShareUserService
	SharedResourceAccessService *service.SharedResourceAccessService
	GroupService                *service.GroupService
	WebDAVAccessKeyService      *service.WebDAVAccessKeyService
	NotificationService         *service.NotificationService
	UploadSessionService        *service.UploadSessionService

	// Authenticators
	Authenticators       []auth.Authenticator
	AccessKeyAuth        *infraAuth.AccessKeyAuthenticator
	BasicAuth            *infraAuth.BasicAuthenticator
	Web3Auth             *infraAuth.Web3Authenticator
	ToolCredentialAuth   *infraAuth.ToolCredentialAuthenticator
	S3SecretBox          *infraCrypto.SecretBox
	MultipartService     *service.MultipartService
	S3CredentialResolver s3.CredentialResolver
	ObjectService        *service.ObjectService

	// Handlers
	HealthHandler              *handler.HealthHandler
	InternalReplicationHandler *handler.InternalReplicationHandler
	Web3Handler                *handler.Web3Handler
	IdentityHandler            *handler.IdentityHandler
	EmailAuthHandler           *handler.EmailAuthHandler
	AssetsHandler              *handler.AssetsHandler
	AssetObjectHandler         *handler.AssetObjectHandler
	AssetToolHandler           *handler.AssetToolHandler
	WebDAVHandler              *handler.WebDAVHandler
	QuotaHandler               *handler.QuotaHandler
	UserHandler                *handler.UserHandler
	AdminUserHandler           *handler.AdminUserHandler
	RecycleHandler             *handler.RecycleHandler
	ShareHandler               *handler.ShareHandler
	ShareUserHandler           *handler.ShareUserHandler
	WebDAVAccessKeyHandler     *handler.WebDAVAccessKeyHandler
	GroupHandler               *handler.GroupHandler
	NotificationHandler        *handler.NotificationHandler
	S3CredentialHandler        *handler.S3CredentialHandler
	UploadSessionHandler       *handler.UploadSessionHandler
	ToolCredentialHandler      *handler.ToolCredentialHandler

	// HTTP
	Router   *http.Router
	Server   *http.Server
	S3Server *s3.Server
}

type s3ObjectMetadataRepoAdapter struct {
	repo repository.S3ObjectMetadataRepository
}

func (a s3ObjectMetadataRepoAdapter) Upsert(ctx context.Context, userDirectory, bucket, key string, metadata service.ObjectMetadata) error {
	if a.repo == nil {
		return nil
	}
	return a.repo.Upsert(ctx, &repository.S3ObjectMetadata{
		UserDirectory: userDirectory,
		Bucket:        bucket,
		ObjectKey:     key,
		ETag:          metadata.ETag,
		ContentType:   metadata.ContentType,
		UpdatedAt:     metadata.UpdatedAt,
	})
}

func (a s3ObjectMetadataRepoAdapter) Find(ctx context.Context, userDirectory, bucket, key string) (*service.ObjectMetadata, error) {
	if a.repo == nil {
		return nil, nil
	}
	item, err := a.repo.Find(ctx, userDirectory, bucket, key)
	if err != nil || item == nil {
		return nil, err
	}
	return &service.ObjectMetadata{
		ETag:        item.ETag,
		ContentType: item.ContentType,
		UpdatedAt:   item.UpdatedAt,
	}, nil
}

func (a s3ObjectMetadataRepoAdapter) Delete(ctx context.Context, userDirectory, bucket, key string) error {
	if a.repo == nil {
		return nil
	}
	return a.repo.Delete(ctx, userDirectory, bucket, key)
}

func (a s3ObjectMetadataRepoAdapter) ListByPrefix(ctx context.Context, userDirectory, bucket, prefix string) (map[string]service.ObjectMetadata, error) {
	if a.repo == nil {
		return nil, nil
	}
	items, err := a.repo.ListByPrefix(ctx, userDirectory, bucket, prefix)
	if err != nil {
		return nil, err
	}
	result := make(map[string]service.ObjectMetadata, len(items))
	for key, item := range items {
		result[key] = service.ObjectMetadata{
			ETag:        item.ETag,
			ContentType: item.ContentType,
			UpdatedAt:   item.UpdatedAt,
		}
	}
	return result, nil
}

// NewContainer 创建容器
func NewContainer(cfg *config.Config) (*Container, error) {
	c := &Container{
		Config:         cfg,
		Authenticators: make([]auth.Authenticator, 0),
	}

	// 初始化组件
	if err := c.initLogger(); err != nil {
		return nil, fmt.Errorf("failed to init logger: %w", err)
	}

	if err := c.initDatabase(); err != nil {
		return nil, fmt.Errorf("failed to init database: %w", err)
	}

	if err := c.initRepositories(); err != nil {
		return nil, fmt.Errorf("failed to init repositories: %w", err)
	}

	if err := c.initServices(); err != nil {
		return nil, fmt.Errorf("failed to init services: %w", err)
	}

	if err := c.initAuthenticators(); err != nil {
		return nil, fmt.Errorf("failed to init authenticators: %w", err)
	}

	if err := c.initHandlers(); err != nil {
		return nil, fmt.Errorf("failed to init handlers: %w", err)
	}

	if err := c.initHTTP(); err != nil {
		return nil, fmt.Errorf("failed to init http: %w", err)
	}

	return c, nil
}

// initLogger 初始化日志器
func (c *Container) initLogger() error {
	l, err := logger.NewLogger(c.Config.Log)
	if err != nil {
		return err
	}

	c.Logger = l
	c.Logger.Info("logger initialized",
		zap.String("level", c.Config.Log.Level),
		zap.String("format", c.Config.Log.Format))

	return nil
}

// initDatabase 初始化数据库
func (c *Container) initDatabase() error {
	// 仅支持 PostgreSQL
	if !(c.Config.Database.Type == "postgres" || c.Config.Database.Type == "postgresql") {
		return fmt.Errorf("unsupported database type %q: only postgres/postgresql is supported", c.Config.Database.Type)
	}

	db, err := database.NewPostgresDB(c.Config.Database)
	if err != nil {
		return fmt.Errorf("failed to connect to postgres: %w", err)
	}
	c.DB = db

	// 执行数据库迁移
	ctx := context.Background()
	if err := c.DB.Migrate(ctx); err != nil {
		return fmt.Errorf("failed to migrate database: %w", err)
	}
	if err := c.DB.ReconcileSharedResources(ctx); err != nil {
		return fmt.Errorf("failed to reconcile shared resources: %w", err)
	}

	c.Logger.Info("database initialized",
		zap.String("type", "postgres"),
		zap.String("host", c.Config.Database.Host),
		zap.Int("port", c.Config.Database.Port))
	return nil
}

// initRepositories 初始化仓储
func (c *Container) initRepositories() error {
	if c.DB == nil {
		return fmt.Errorf("database not initialized")
	}

	// 用户仓储
	repo, err := repository.NewPostgresUserRepository(c.DB)
	if err != nil {
		return fmt.Errorf("failed to create postgres repository: %w", err)
	}
	c.UserRepository = repo

	// 回收站仓储
	c.RecycleRepository = repository.NewPostgresRecycleRepository(c.DB.DB)
	// 分享仓储
	c.ShareRepository = repository.NewPostgresShareRepository(c.DB.DB)
	// 定向分享仓储
	c.UserShareRepository = repository.NewPostgresUserShareRepository(c.DB.DB)
	c.SharedResourceGrantRepository = repository.NewPostgresSharedResourceGrantRepository(c.DB.DB)
	// 分组管理仓储
	c.GroupRepository = repository.NewPostgresGroupRepository(c.DB.DB)
	// WebDAV 访问密钥仓储
	c.WebDAVAccessKeyRepo = repository.NewPostgresWebDAVAccessKeyRepository(c.DB.DB)
	c.ToolCredentialRepo = repository.NewPostgresToolCredentialRepository(c.DB.DB)
	c.S3MultipartRepo = repository.NewPostgresS3MultipartRepository(c.DB.DB)
	c.S3ObjectMetadataRepo = repository.NewPostgresS3ObjectMetadataRepository(c.DB.DB)
	if c.Config.S3.Enabled {
		secretBox, err := infraCrypto.NewSecretBoxBase64(c.Config.S3.CredentialMasterKey)
		if err != nil {
			return fmt.Errorf("failed to initialize s3 credential encryption: %w", err)
		}
		c.S3SecretBox = secretBox
		c.S3CredentialRepo = repository.NewPostgresS3CredentialRepository(c.DB.DB, secretBox)
		c.S3CredentialResolver = s3.NewRepositoryCredentialResolver(c.S3CredentialRepo)
	}
	// 站内消息仓储
	c.NotificationRepo = repository.NewPostgresNotificationRepository(c.DB.DB)
	// 复制仓储
	c.ReplicationOutboxRepo = repository.NewPostgresReplicationOutboxRepository(c.DB.DB)
	c.ReplicationOffsetRepo = repository.NewPostgresReplicationOffsetRepository(c.DB.DB)
	c.ReconcileRepo = repository.NewPostgresReplicationReconcileRepository(c.DB.DB)
	c.ClusterNodeRepo = repository.NewPostgresClusterNodeRepository(c.DB.DB)
	c.ClusterAssignmentRepo = repository.NewPostgresClusterReplicationAssignmentRepository(c.DB.DB)

	c.Logger.Info("using PostgreSQL user repository")
	c.Logger.Info("repositories initialized")
	return nil
}

// initServices 初始化服务
func (c *Container) initServices() error {
	c.AssetSpaceManager = assetspace.NewManager(c.Config, c.Logger)
	c.ObjectService = service.NewObjectService(c.Config.WebDAV.Directory)
	c.ObjectService.SetMetadataRepository(s3ObjectMetadataRepoAdapter{repo: c.S3ObjectMetadataRepo})
	// 配额服务
	c.QuotaService = quota.NewService(c.UserRepository)
	c.QuotaReconciler = service.NewQuotaReconciler(
		c.Config,
		c.UserRepository,
		c.RecycleRepository,
		c.QuotaService,
		c.Logger,
	)
	c.PeerResolver = service.NewReplicationPeerResolver(c.Config, c.ClusterNodeRepo, c.ClusterAssignmentRepo)
	c.NodeHeartbeat = service.NewNodeHeartbeatRegistrar(c.Config, c.ClusterNodeRepo, c.Logger)
	c.AssignmentAllocator = service.NewReplicationAssignmentAllocator(c.Config, c.ClusterNodeRepo, c.ClusterAssignmentRepo, c.Logger)
	c.MutationRecorder = service.NewMutationRecorder(c.Config, c.ReplicationOutboxRepo, c.PeerResolver, c.Logger)
	c.ObjectService.SetGuards(c.QuotaService, c.UserRepository, c.MutationRecorder)
	c.ObjectService.SetShareReferences(c.Config, c.UserShareRepository, c.ShareRepository)
	c.MultipartService = service.NewMultipartService(c.Config.WebDAV.Directory, c.S3MultipartRepo)
	c.MultipartService.SetObjectService(c.ObjectService)
	c.MultipartService.SetQuotaService(c.QuotaService)
	c.ReplicationWorker = service.NewReplicationWorker(c.Config, c.ReplicationOutboxRepo, c.PeerResolver, c.Logger)
	reconcileScanner, err := service.NewReconcileScanner(c.Config.WebDAV.Directory)
	if err != nil {
		return fmt.Errorf("failed to create reconcile scanner: %w", err)
	}
	c.ReconcileScanner = reconcileScanner
	c.ReplicationCleaner = service.NewReplicationLifecycleCleaner(c.Config, c.ReconcileRepo, c.Logger)

	// WebDAV 服务
	fileSystem := webdav.Dir(c.Config.WebDAV.Directory)
	permissionChecker := permission.NewWebDAVChecker(fileSystem, c.Logger)

	c.WebDAVService = service.NewWebDAVService(
		c.Config,
		permissionChecker,
		c.QuotaService,
		c.UserRepository,
		c.RecycleRepository,
		c.UserShareRepository,
		c.MutationRecorder,
		c.Logger,
	)

	// 回收站服务
	c.RecycleService = service.NewRecycleService(
		c.RecycleRepository,
		c.UserRepository,
		c.MutationRecorder,
		c.Config,
		c.Logger,
	)

	// 分享服务
	c.ShareService = service.NewShareService(
		c.ShareRepository,
		c.UserRepository,
		c.Config,
		c.Logger,
	)
	// 分组管理服务
	c.GroupService = service.NewGroupService(c.GroupRepository, c.UserRepository)
	// WebDAV 访问密钥服务
	c.WebDAVAccessKeyService = service.NewWebDAVAccessKeyService(c.WebDAVAccessKeyRepo)
	// 站内消息服务
	c.NotificationService = service.NewNotificationService(c.NotificationRepo, c.UserRepository, c.Logger)
	c.NotificationService.SetGroupRepository(c.GroupRepository)
	c.NotificationService.SetQuotaNotificationThresholdPercent(c.Config.Quota.NotificationThresholdPercent)
	c.NotificationService.SetQuotaEmailTemplateID(c.Config.Email.QuotaWarningTemplateID)
	c.NotificationService.SetEmailPublisher(infraEmail.NewSender(c.Config.Email, c.Logger))
	c.GroupService.SetNotificationService(c.NotificationService)
	if c.QuotaReconciler != nil {
		c.QuotaReconciler.SetNotificationService(c.NotificationService)
	}
	// 定向分享服务
	c.ShareUserService = service.NewShareUserService(
		c.UserShareRepository,
		c.UserRepository,
		c.GroupService,
		c.NotificationService,
		c.Config,
		c.Logger,
	)
	c.ShareService.SetShareUserService(c.ShareUserService)
	c.UploadSessionService = service.NewUploadSessionService(
		c.Config,
		permissionChecker,
		c.QuotaService,
		c.UserRepository,
		c.ShareUserService,
		c.MutationRecorder,
		c.Logger,
	)
	c.SharedResourceAccessService = service.NewSharedResourceAccessService(c.SharedResourceGrantRepository)
	c.ShareService.SetSharedResourceAccess(c.SharedResourceAccessService)
	c.UploadSessionService.SetSharedResourceAccess(c.SharedResourceAccessService)

	c.Logger.Info("services initialized", zap.Bool("quota_enabled", true))

	return nil
}

// initAuthenticators 初始化认证器
func (c *Container) initAuthenticators() error {
	// WebDAV 访问密钥认证器（必须在 Basic 认证器之前）
	c.AccessKeyAuth = infraAuth.NewAccessKeyAuthenticator(
		c.UserRepository,
		c.WebDAVAccessKeyRepo,
		c.Logger,
	)
	c.Authenticators = append(c.Authenticators, c.AccessKeyAuth)
	c.ToolCredentialAuth = infraAuth.NewToolCredentialAuthenticator(c.UserRepository, c.ToolCredentialRepo, c.Logger)
	c.Authenticators = append(c.Authenticators, c.ToolCredentialAuth)

	// Basic 认证器
	c.BasicAuth = infraAuth.NewBasicAuthenticator(
		c.UserRepository,
		c.Config.Security.NoPassword,
		c.Logger,
	)
	c.Authenticators = append(c.Authenticators, c.BasicAuth)

	// Web3 认证器
	ucanAudience := strings.TrimSpace(c.Config.Web3.UCAN.Audience)
	if ucanAudience == "" {
		ucanAudience = fmt.Sprintf("did:web:localhost:%d", c.Config.Server.Port)
	}
	requiredCaps := make([]infraAuth.UcanCapability, 0, len(c.Config.Web3.UCAN.RequiredCapabilities))
	for _, cfgCap := range c.Config.Web3.UCAN.RequiredCapabilities {
		requiredCaps = append(requiredCaps, infraAuth.UcanCapability{
			With:     cfgCap.With,
			Can:      cfgCap.Can,
			Resource: cfgCap.Resource,
			Action:   cfgCap.Action,
		})
	}
	requiredResource := c.Config.Web3.UCAN.RequiredResource
	requiredAction := c.Config.Web3.UCAN.RequiredAction
	if len(requiredCaps) > 0 {
		requiredResource = ""
		requiredAction = ""
	}
	ucanCaps := infraAuth.BuildRequiredUcanCaps(
		requiredResource,
		requiredAction,
		requiredCaps,
	)
	ucanVerifier := infraAuth.NewUcanVerifier(
		c.Config.Web3.UCAN.Enabled,
		ucanAudience,
		ucanCaps,
		c.Config.Web3.UCAN.TrustedIssuerDIDs,
		c.Logger,
	)
	c.Web3Auth = infraAuth.NewWeb3Authenticator(
		c.UserRepository,
		c.Config.Web3.JWTSecret,
		c.Config.Web3.TokenExpiration,
		c.Config.Web3.RefreshTokenExpiration,
		ucanVerifier,
		c.AssetSpaceManager,
		c.Logger,
		c.Config.Web3.AutoCreateOnUCAN,
	)
	c.Authenticators = append(c.Authenticators, c.Web3Auth)

	c.Logger.Info("authenticators initialized", zap.Int("count", len(c.Authenticators)))

	return nil
}

// initHandlers 初始化处理器
func (c *Container) initHandlers() error {
	// 健康检查处理器
	readinessChecker := apphealth.NewReadinessChecker(c.DB.DB, c.Config.WebDAV.Directory)
	c.HealthHandler = handler.NewHealthHandler(c.Logger, readinessChecker)
	if c.Config.Replication.Enabled {
		c.InternalReplicationHandler = handler.NewInternalReplicationHandler(
			c.Config,
			c.Logger,
			c.ReplicationOutboxRepo,
			c.ReplicationOffsetRepo,
			c.ReconcileRepo,
			c.ReconcileScanner,
			c.PeerResolver,
			c.ClusterAssignmentRepo,
		)
	}

	// 创建配额处理器
	c.QuotaHandler = handler.NewQuotaHandler(c.QuotaService, c.Logger)
	c.QuotaHandler.SetNotificationService(c.NotificationService)
	// 用户信息处理器
	c.UserHandler = handler.NewUserHandler(c.Logger, c.UserRepository, c.Config.Security.AdminAddresses)
	// 管理员用户处理器
	c.AdminUserHandler = handler.NewAdminUserHandler(c.Logger, c.UserRepository, c.AssetSpaceManager)

	// Web3 处理器
	if c.Web3Auth != nil {
		c.Web3Handler = handler.NewWeb3Handler(
			c.Web3Auth,
			c.UserRepository,
			c.AssetSpaceManager,
			c.Logger,
			c.Config.Web3.AutoCreateOnChallenge,
		)
		c.IdentityHandler = handler.NewIdentityHandler(
			c.Web3Auth,
			c.UserRepository,
			c.AssetSpaceManager,
			c.Config.Identity,
			c.Logger,
		)
	}

	// 邮箱验证码登录处理器
	emailStore := infraAuth.NewEmailCodeStore()
	emailSender := infraEmail.NewSender(c.Config.Email, c.Logger)
	c.EmailAuthHandler = handler.NewEmailAuthHandler(
		c.Web3Auth,
		c.UserRepository,
		c.AssetSpaceManager,
		emailStore,
		emailSender,
		c.Config.Email,
		c.Logger,
	)

	c.AssetsHandler = handler.NewAssetsHandler(c.AssetSpaceManager, c.Logger)
	c.AssetObjectHandler = handler.NewAssetObjectHandler(c.Config, c.ObjectService, c.Logger)
	c.AssetToolHandler = handler.NewAssetToolHandler(c.Config, c.AssetSpaceManager, c.ObjectService, c.Logger)
	c.AssetToolHandler.SetToolCredentialAuditRepository(c.ToolCredentialRepo)

	// WebDAV 处理器
	c.WebDAVHandler = handler.NewWebDAVHandler(
		c.WebDAVService,
		c.QuotaService,
		c.UserRepository,
		c.Logger,
	)
	c.WebDAVService.SetPublicShareRepository(c.ShareRepository)

	// 回收站处理器
	c.RecycleHandler = handler.NewRecycleHandler(
		c.RecycleService,
		c.UserRepository,
		c.Logger,
	)

	// 分享处理器
	c.ShareHandler = handler.NewShareHandler(
		c.ShareService,
		c.Logger,
	)
	// 定向分享处理器
	c.ShareUserHandler = handler.NewShareUserHandler(
		c.ShareUserService,
		c.UserRepository,
		c.MutationRecorder,
		c.Logger,
	)
	c.ShareUserHandler.SetSharedResourceAccess(c.SharedResourceAccessService)
	c.ShareUserHandler.SetPublicShareRepository(c.ShareRepository)
	// 分组管理处理器
	c.GroupHandler = handler.NewGroupHandler(
		c.GroupService,
		c.Logger,
	)
	// WebDAV 访问密钥处理器
	c.WebDAVAccessKeyHandler = handler.NewWebDAVAccessKeyHandler(
		c.WebDAVAccessKeyService,
		c.Logger,
	)
	c.NotificationHandler = handler.NewNotificationHandler(
		c.NotificationService,
		c.Config.Security.AdminAddresses,
		c.Logger,
	)
	if c.S3CredentialRepo != nil {
		c.S3CredentialHandler = handler.NewS3CredentialHandler(c.S3CredentialRepo, c.Logger)
	}
	c.UploadSessionHandler = handler.NewUploadSessionHandler(c.UploadSessionService, c.Logger)
	c.ToolCredentialHandler = handler.NewToolCredentialHandler(c.ToolCredentialRepo, c.Logger)

	c.Logger.Info("handlers initialized")

	return nil
}

// initHTTP 初始化 HTTP
func (c *Container) initHTTP() error {
	// 路由器
	c.Router = http.NewRouter(
		c.Config,
		c.Authenticators,
		c.HealthHandler,
		c.InternalReplicationHandler,
		c.Web3Handler,
		c.IdentityHandler,
		c.EmailAuthHandler,
		c.AssetsHandler,
		c.AssetObjectHandler,
		c.AssetToolHandler,
		c.WebDAVHandler,
		c.QuotaHandler,
		c.UserHandler,
		c.AdminUserHandler,
		c.RecycleHandler,
		c.ShareHandler,
		c.ShareUserHandler,
		c.WebDAVAccessKeyHandler,
		c.GroupHandler,
		c.NotificationHandler,
		c.S3CredentialHandler,
		c.UploadSessionHandler,
		c.ToolCredentialHandler,
		c.Logger,
	)

	// 服务器
	c.Server = http.NewServer(c.Config, c.Router, c.Logger)
	if c.Config.S3.Enabled {
		c.S3Server = s3.NewServer(c.Config.S3, c.S3CredentialResolver, c.ObjectService, c.UserRepository, c.MultipartService, c.Logger)
	}
	c.Logger.Info("http components initialized")

	return nil
}

// Close 关闭容器
func (c *Container) Close() error {
	if c.Logger != nil {
		c.Logger.Info("closing container")
	}

	// 关闭数据库连接
	if c.DB != nil {
		if err := c.DB.Close(); err != nil {
			c.Logger.Error("failed to close database", zap.Error(err))
		} else {
			c.Logger.Info("database connection closed")
		}
	}

	if c.Logger != nil {
		_ = c.Logger.Sync()
	}

	return nil
}
