package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/spf13/pflag"
	"github.com/yeying-community/warehouse/internal/container"
	apphealth "github.com/yeying-community/warehouse/internal/health"
	"github.com/yeying-community/warehouse/internal/infrastructure/config"
	"github.com/yeying-community/warehouse/internal/infrastructure/database"
	"go.uber.org/zap"
)

var (
	version   = "2.0.0"
	buildTime = "unknown"
	gitCommit = "unknown"
)

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "ha":
			if err := runHACommand(os.Args[2:]); err != nil {
				fmt.Fprintf(os.Stderr, "Failed to run HA command: %v\n", err)
				os.Exit(1)
			}
			return
		case "quota":
			if err := runQuotaCommand(os.Args[2:]); err != nil {
				fmt.Fprintf(os.Stderr, "Failed to run quota command: %v\n", err)
				os.Exit(1)
			}
			return
		case "recycle":
			if err := runRecycleCommand(os.Args[2:]); err != nil {
				fmt.Fprintf(os.Stderr, "Failed to run recycle command: %v\n", err)
				os.Exit(1)
			}
			return
		case "share":
			if err := runShareCommand(os.Args[2:]); err != nil {
				fmt.Fprintf(os.Stderr, "Failed to run share command: %v\n", err)
				os.Exit(1)
			}
			return
		case "serve":
			runServer(os.Args[2:])
			return
		}
	}

	runServer(os.Args[1:])
}

func runServer(args []string) {
	// 解析命令行参数
	flags := parseFlags(args)

	// 显示版本信息
	if showVersion, _ := flags.GetBool("version"); showVersion {
		printVersion()
		os.Exit(0)
	}

	// 加载配置
	configFile, _ := flags.GetString("config")
	cfg, err := loadConfig(configFile, flags)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load config: %v\n", err)
		os.Exit(1)
	}

	if checkReady, _ := flags.GetBool("check-ready"); checkReady {
		if err := runReadinessCheck(cfg); err != nil {
			fmt.Fprintf(os.Stderr, "Readiness check failed: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("ready")
		os.Exit(0)
	}

	// 创建容器
	c, err := container.NewContainer(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create container: %v\n", err)
		os.Exit(1)
	}

	defer func() {
		if err := c.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to close container: %v\n", err)
		}
	}()

	// 打印启动信息
	printStartupInfo(c)

	backgroundCtx, stopBackground := context.WithCancel(context.Background())
	var backgroundWG sync.WaitGroup
	startBackground := func(run func(context.Context)) {
		if run == nil {
			return
		}
		backgroundWG.Add(1)
		go func() {
			defer backgroundWG.Done()
			run(backgroundCtx)
		}()
	}
	if c.NodeHeartbeat != nil && c.NodeHeartbeat.Enabled() {
		startBackground(c.NodeHeartbeat.Run)
	}
	if c.AssignmentAllocator != nil && c.AssignmentAllocator.Enabled() {
		startBackground(c.AssignmentAllocator.Run)
	}
	if c.ReplicationWorker != nil && c.ReplicationWorker.Enabled() {
		startBackground(c.ReplicationWorker.Run)
	}
	if c.ReplicationCleaner != nil && c.ReplicationCleaner.Enabled() {
		startBackground(c.ReplicationCleaner.Run)
	}
	if c.QuotaReconciler != nil && c.QuotaReconciler.Enabled() {
		startBackground(c.QuotaReconciler.Run)
	}
	if c.MultipartService != nil && c.Config.Node.Role != "standby" {
		startBackground(c.MultipartService.Run)
	}
	if c.UploadSessionService != nil && c.Config.Node.Role != "standby" {
		startBackground(c.UploadSessionService.Run)
	}
	if c.InternalReplicationHandler != nil {
		startBackground(c.InternalReplicationHandler.RunAutoReconcile)
	}
	backgroundDone := make(chan struct{})
	go func() {
		backgroundWG.Wait()
		close(backgroundDone)
	}()

	// 启动服务器
	serverErrors := make(chan error, 1)
	go func() {
		c.Logger.Info("starting http server")
		serverErrors <- c.Server.Start()
	}()
	if c.S3Server != nil {
		go func() {
			serverErrors <- c.S3Server.Start()
		}()
	}

	// 等待中断信号或服务器错误
	waitForShutdown(c, serverErrors, stopBackground, backgroundDone)
}

// parseFlags 解析命令行参数
func parseFlags(args []string) *pflag.FlagSet {
	flags := pflag.NewFlagSet("warehouse", pflag.ExitOnError)

	// 基础选项
	flags.StringP("config", "c", "", "Config file path (default: config.yaml)")
	flags.BoolP("version", "v", false, "Show version")
	flags.BoolP("help", "h", false, "Show help")
	flags.Bool("check-ready", false, "Run dependency readiness check and exit")

	// 服务器选项
	flags.String("address", "", "Server address")
	flags.IntP("port", "p", 0, "Server port")
	flags.Bool("tls", false, "Enable TLS")
	flags.String("cert", "", "TLS certificate file")
	flags.String("key", "", "TLS key file")

	// WebDAV 选项
	flags.String("prefix", "", "WebDAV prefix")
	flags.StringP("directory", "d", "", "WebDAV directory")

	// 数据库选项
	flags.String("db-type", "", "Database type (postgres)")
	flags.String("db-host", "", "Database host")
	flags.Int("db-port", 0, "Database port")
	flags.String("db-name", "", "Database name")
	flags.String("db-user", "", "Database username")
	flags.String("db-password", "", "Database password")

	flags.Parse(args)

	if help, _ := flags.GetBool("help"); help {
		printHelp(flags)
		os.Exit(0)
	}

	return flags
}

// loadConfig 加载配置
func loadConfig(configFile string, flags *pflag.FlagSet) (*config.Config, error) {
	if configFile == "" {
		configFile = "config.yaml"
	}

	loader := config.NewLoader()
	cfg, err := loader.Load(configFile, flags)
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}

	// 验证配置
	if err := validateConfig(cfg); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	return cfg, nil
}

// validateConfig 验证配置
func validateConfig(cfg *config.Config) error {
	// 验证服务器配置
	if cfg.Server.Port <= 0 || cfg.Server.Port > 65535 {
		return fmt.Errorf("invalid server port: %d", cfg.Server.Port)
	}

	// 验证 TLS 配置
	if cfg.Server.TLS {
		if cfg.Server.CertFile == "" || cfg.Server.KeyFile == "" {
			return fmt.Errorf("TLS enabled but cert or key file not specified")
		}
	}

	// 验证 WebDAV 配置
	if cfg.WebDAV.Directory == "" {
		return fmt.Errorf("webdav directory not specified")
	}

	// 验证数据库配置
	if !(cfg.Database.Type == "postgres" || cfg.Database.Type == "postgresql") {
		return fmt.Errorf("unsupported database type: %s (only postgres/postgresql supported)", cfg.Database.Type)
	}
	if cfg.Database.Host == "" {
		return fmt.Errorf("database host not specified")
	}
	if cfg.Database.Database == "" {
		return fmt.Errorf("database name not specified")
	}

	if cfg.Database.Username == "" {
		return fmt.Errorf("database username not specified")
	}

	// 验证 Web3 配置
	if cfg.Web3.JWTSecret == "" {
		return fmt.Errorf("web3 jwt secret not specified")
	}
	if len(cfg.Web3.JWTSecret) < 32 {
		return fmt.Errorf("web3 jwt secret too short (minimum 32 characters)")
	}

	return nil
}

// printVersion 打印版本信息
func printVersion() {
	fmt.Printf("Warehouse Server with Web3 Authentication\n")
	fmt.Printf("Version:    %s\n", version)
	fmt.Printf("Build Time: %s\n", buildTime)
	fmt.Printf("Git Commit: %s\n", gitCommit)
}

// printHelp 打印帮助信息
func printHelp(flags *pflag.FlagSet) {
	fmt.Println("Warehouse Server with Web3 Authentication")
	fmt.Println()
	fmt.Println("Usage:")
	fmt.Println("  warehouse [flags]")
	fmt.Println("  warehouse serve [flags]")
	fmt.Println("  warehouse ha <subcommand> [flags]")
	fmt.Println("  warehouse quota <subcommand> [flags]")
	fmt.Println("  warehouse recycle <subcommand> [flags]")
	fmt.Println()
	fmt.Println("Flags:")
	flags.PrintDefaults()
	fmt.Println()
	fmt.Println("Examples:")
	fmt.Println("  # Start with config.yaml in the current directory")
	fmt.Println("  warehouse")
	fmt.Println("  warehouse -c config.yaml")
	fmt.Println()
	fmt.Println("  # Start with explicit serve subcommand")
	fmt.Println("  warehouse serve -c config.yaml")
	fmt.Println()
	fmt.Println("  # Start with command line flags")
	fmt.Println("  warehouse -p 8080 -d /data")
	fmt.Println()
	fmt.Println("  # Start with TLS")
	fmt.Println("  warehouse -c config.yaml --tls --cert cert.pem --key key.pem")
	fmt.Println()
	fmt.Println("  # Start with PostgreSQL")
	fmt.Println("  warehouse -c config.yaml --db-type postgres --db-host localhost")
	fmt.Println()
	fmt.Println("  # Run readiness check")
	fmt.Println("  warehouse -c config.yaml --check-ready")
	fmt.Println()
	fmt.Println("  # Show version")
	fmt.Println("  warehouse --version")
	fmt.Println()
	fmt.Println("  # Show HA status")
	fmt.Println("  warehouse ha status -c config.yaml")
	fmt.Println("  warehouse ha cleanup history -c config.yaml --dry-run")
	fmt.Println()
	fmt.Println("  # Check quota drift for one user")
	fmt.Println("  warehouse quota check -c config.yaml --username alice")
	fmt.Println()
	fmt.Println("  # Backfill recycle is_dir for historical rows")
	fmt.Println("  warehouse recycle backfill-is-dir -c config.yaml --dry-run")
	fmt.Println()
	fmt.Println("  # Clean historical sync artifacts from recycle")
	fmt.Println("  warehouse recycle clean-sync-artifacts -c config.yaml --dry-run")
}

func runReadinessCheck(cfg *config.Config) error {
	db, err := database.NewPostgresDB(cfg.Database)
	if err != nil {
		return fmt.Errorf("connect database: %w", err)
	}
	defer func() {
		_ = db.Close()
	}()

	checker := apphealth.NewReadinessChecker(db.DB, cfg.WebDAV.Directory)
	result := checker.Check(context.Background())
	if !result.Ready() {
		return fmt.Errorf("%s", formatReadinessResult(result))
	}
	return nil
}

func formatReadinessResult(result apphealth.Result) string {
	message := result.Status
	for _, check := range result.Checks {
		if check.Status == apphealth.StatusReady {
			continue
		}
		message = fmt.Sprintf("%s: %s=%s", message, check.Name, check.Error)
	}
	return message
}

// printStartupInfo 打印启动信息
func printStartupInfo(c *container.Container) {
	c.Logger.Info("=================================")
	c.Logger.Info("Warehouse Server Starting")
	c.Logger.Info("=================================")
	c.Logger.Info("version",
		zap.String("version", version),
		zap.String("build_time", buildTime),
		zap.String("git_commit", gitCommit))
	c.Logger.Info("=================================")

	// 服务器信息
	c.Logger.Info("server",
		zap.String("address", c.Config.Server.Address),
		zap.Int("port", c.Config.Server.Port),
		zap.Bool("tls", c.Config.Server.TLS),
		zap.Duration("read_timeout", c.Config.Server.ReadTimeout),
		zap.Duration("write_timeout", c.Config.Server.WriteTimeout))

	// WebDAV 信息
	c.Logger.Info("webdav",
		zap.String("prefix", c.Config.WebDAV.Prefix),
		zap.String("directory", c.Config.WebDAV.Directory),
		zap.String("default_permissions", c.Config.WebDAV.Permissions))

	// 数据库信息
	c.Logger.Info("database",
		zap.String("type", c.Config.Database.Type),
		zap.String("host", c.Config.Database.Host),
		zap.Int("port", c.Config.Database.Port),
		zap.String("database", c.Config.Database.Database))

	// 节点与复制信息
	c.Logger.Info("node",
		zap.String("id", c.Config.Node.ID),
		zap.String("role", c.Config.Node.Role),
		zap.String("advertise_url", c.Config.Node.AdvertiseURL))
	c.Logger.Info("replication",
		zap.Bool("enabled", c.Config.Replication.Enabled),
		zap.Duration("dispatch_interval", c.Config.Replication.DispatchInterval),
		zap.Duration("request_timeout", c.Config.Replication.RequestTimeout),
		zap.Int("batch_size", c.Config.Replication.BatchSize),
		zap.Bool("worker_enabled", c.ReplicationWorker != nil && c.ReplicationWorker.Enabled()))

	// Web3 信息
	c.Logger.Info("web3",
		zap.Duration("token_expiration", c.Config.Web3.TokenExpiration))

	// CORS 信息
	c.Logger.Info("cors",
		zap.Bool("enabled", c.Config.CORS.Enabled),
		zap.Bool("credentials", c.Config.CORS.Credentials),
		zap.Strings("allowed_origins", c.Config.CORS.AllowedOrigins))

	// 安全信息
	c.Logger.Info("security",
		zap.Bool("no_password", c.Config.Security.NoPassword),
		zap.Bool("behind_proxy", c.Config.Security.BehindProxy))

	c.Logger.Info("=================================")
}

// waitForShutdown 等待关闭信号
func waitForShutdown(c *container.Container, serverErrors <-chan error, stopBackground context.CancelFunc, backgroundDone <-chan struct{}) {
	// 监听系统信号
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	defer signal.Stop(quit)

	// 等待信号或错误
	select {
	case err := <-serverErrors:
		if err != nil {
			c.Logger.Error("server error", zap.Error(err))
		}
	case sig := <-quit:
		c.Logger.Info("received shutdown signal", zap.String("signal", sig.String()))
	}

	// 优雅关闭
	c.Logger.Info("shutting down server gracefully",
		zap.Duration("timeout", c.Config.Server.ShutdownTimeout))

	ctx, cancel := context.WithTimeout(context.Background(), c.Config.Server.ShutdownTimeout)
	defer cancel()

	if stopBackground != nil {
		stopBackground()
	}
	if backgroundDone != nil {
		select {
		case <-backgroundDone:
			c.Logger.Info("background components stopped")
		case <-ctx.Done():
			c.Logger.Warn("timed out waiting for background components to stop")
		}
	}

	if err := c.Server.Shutdown(ctx); err != nil {
		c.Logger.Error("failed to shutdown server gracefully", zap.Error(err))
		os.Exit(1)
	}
	if c.S3Server != nil {
		if err := c.S3Server.Shutdown(ctx); err != nil {
			c.Logger.Error("failed to shutdown s3 server gracefully", zap.Error(err))
			os.Exit(1)
		}
	}

	c.Logger.Info("server stopped gracefully")
}
