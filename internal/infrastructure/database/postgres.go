package database

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	_ "github.com/lib/pq"
	"github.com/yeying-community/warehouse/internal/infrastructure/config"
)

// PostgresDB PostgreSQL 数据库连接
type PostgresDB struct {
	DB *sql.DB
}

const (
	schemaMigrationLockID              int64 = 846273910527
	notificationDedupeMigrationVersion       = "2026072601"
	notificationDedupeMigrationName          = "notification_dedupe_partial_unique_index"
)

// NewPostgresDB 创建 PostgreSQL 数据库连接
func NewPostgresDB(cfg config.DatabaseConfig) (*PostgresDB, error) {
	dsn := fmt.Sprintf(
		"host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
		cfg.Host,
		cfg.Port,
		cfg.Username,
		cfg.Password,
		cfg.Database,
		cfg.SSLMode,
	)

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// 设置连接池参数
	db.SetMaxOpenConns(cfg.MaxOpenConns)
	db.SetMaxIdleConns(cfg.MaxIdleConns)
	db.SetConnMaxLifetime(cfg.MaxLifetime)

	// 测试连接
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	return &PostgresDB{DB: db}, nil
}

// Close 关闭数据库连接
func (p *PostgresDB) Close() error {
	return p.DB.Close()
}

// Migrate 执行数据库迁移
func (p *PostgresDB) Migrate(ctx context.Context) error {
	queries := []string{
		// 创建用户表
		`CREATE TABLE IF NOT EXISTS users (
			id VARCHAR(50) PRIMARY KEY,
			username VARCHAR(255) UNIQUE NOT NULL,
			password TEXT,
			identity_did VARCHAR(128) UNIQUE,
			wallet_address VARCHAR(42) UNIQUE,
			email VARCHAR(255) UNIQUE,
			directory TEXT NOT NULL,
			permissions VARCHAR(10) NOT NULL DEFAULT 'R',
			quota BIGINT NOT NULL DEFAULT 1073741824,
			used_space BIGINT NOT NULL DEFAULT 0,
			created_at TIMESTAMP NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMP NOT NULL DEFAULT NOW()
		)`,

		// 兼容旧表结构：新增 email 字段
		`ALTER TABLE IF EXISTS users
			ADD COLUMN IF NOT EXISTS email VARCHAR(255)`,

		// 兼容旧表结构：新增 YeYing Identity DID 字段
		`ALTER TABLE IF EXISTS users
			ADD COLUMN IF NOT EXISTS identity_did VARCHAR(128)`,

		// 创建用户规则表
		`CREATE TABLE IF NOT EXISTS user_rules (
			id SERIAL PRIMARY KEY,
			user_id VARCHAR(50) NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			creator_user_id VARCHAR(50) NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			source_share_id VARCHAR(50) NULL,
			path TEXT NOT NULL,
			permissions VARCHAR(10) NOT NULL,
			regex BOOLEAN NOT NULL DEFAULT FALSE,
			created_at TIMESTAMP NOT NULL DEFAULT NOW()
		)`,

		// WebDAV 目录级访问密钥（最小权限凭证）
		`CREATE TABLE IF NOT EXISTS webdav_access_keys (
			id VARCHAR(50) PRIMARY KEY,
			owner_user_id VARCHAR(50) NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			name VARCHAR(255) NOT NULL,
			key_id VARCHAR(100) UNIQUE NOT NULL,
			secret_hash TEXT NOT NULL,
			root_path TEXT NOT NULL DEFAULT '/',
			permissions VARCHAR(10) NOT NULL DEFAULT 'R',
			status VARCHAR(20) NOT NULL DEFAULT 'active',
			expires_at TIMESTAMP NULL,
			last_used_at TIMESTAMP NULL,
			created_at TIMESTAMP NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMP NOT NULL DEFAULT NOW()
		)`,

		// WebDAV 访问密钥目录绑定（一个密钥可绑定多个目录）
		`CREATE TABLE IF NOT EXISTS webdav_access_key_bindings (
			id BIGSERIAL PRIMARY KEY,
			access_key_id VARCHAR(50) NOT NULL REFERENCES webdav_access_keys(id) ON DELETE CASCADE,
			owner_user_id VARCHAR(50) NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			root_path TEXT NOT NULL,
			created_at TIMESTAMP NOT NULL DEFAULT NOW(),
			UNIQUE(access_key_id, root_path)
		)`,

		// S3 Signature V4 凭证；secret 只保存 AES-256-GCM 密文
		`CREATE TABLE IF NOT EXISTS s3_credentials (
			id VARCHAR(50) PRIMARY KEY,
			owner_user_id VARCHAR(50) NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			name VARCHAR(255) NOT NULL,
			access_key_id VARCHAR(100) UNIQUE NOT NULL,
			secret_ciphertext TEXT NOT NULL,
			secret_key_version INTEGER NOT NULL DEFAULT 1,
			root_path TEXT NOT NULL DEFAULT '/',
			permissions VARCHAR(40) NOT NULL DEFAULT 'read',
			status VARCHAR(20) NOT NULL DEFAULT 'active',
			expires_at TIMESTAMP NULL,
			last_used_at TIMESTAMP NULL,
			created_at TIMESTAMP NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMP NOT NULL DEFAULT NOW()
		)`,

		// HTTP Tool scoped credentials; secret is stored only as a password hash.
		`CREATE TABLE IF NOT EXISTS warehouse_tool_credentials (
			id VARCHAR(50) PRIMARY KEY,
			owner_user_id VARCHAR(50) NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			name VARCHAR(255) NOT NULL,
			secret_hash TEXT NOT NULL,
			scopes TEXT[] NOT NULL,
			path_prefixes TEXT[] NOT NULL,
			status VARCHAR(20) NOT NULL DEFAULT 'active',
			expires_at TIMESTAMP NOT NULL,
			last_used_at TIMESTAMP NULL,
			created_at TIMESTAMP NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMP NOT NULL DEFAULT NOW()
		)`,
		`CREATE TABLE IF NOT EXISTS warehouse_tool_credential_audits (
			id VARCHAR(50) PRIMARY KEY,
			credential_id VARCHAR(50) NOT NULL REFERENCES warehouse_tool_credentials(id) ON DELETE CASCADE,
			owner_user_id VARCHAR(50) NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			tool_name VARCHAR(120) NOT NULL DEFAULT '',
			action VARCHAR(40) NOT NULL,
			path TEXT NOT NULL DEFAULT '',
			outcome VARCHAR(40) NOT NULL,
			request_id VARCHAR(100) NOT NULL DEFAULT '',
			trace_id VARCHAR(100) NOT NULL DEFAULT '',
			created_at TIMESTAMP NOT NULL DEFAULT NOW()
		)`,
		`ALTER TABLE IF EXISTS s3_credentials ADD COLUMN IF NOT EXISTS root_path TEXT NOT NULL DEFAULT '/'`,
		`ALTER TABLE IF EXISTS s3_credentials ADD COLUMN IF NOT EXISTS permissions VARCHAR(40) NOT NULL DEFAULT 'read'`,

		// S3 Multipart 上传会话和分片元数据；分片文件只存在 active 节点 staging 目录
		`CREATE TABLE IF NOT EXISTS s3_multipart_uploads (
			id VARCHAR(100) PRIMARY KEY,
			owner_user_id VARCHAR(50) NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			bucket VARCHAR(63) NOT NULL,
			object_key TEXT NOT NULL,
			staging_path TEXT NOT NULL,
			status VARCHAR(20) NOT NULL DEFAULT 'active',
			content_type TEXT,
			initiated_at TIMESTAMP NOT NULL DEFAULT NOW(),
			expires_at TIMESTAMP NOT NULL,
			completed_at TIMESTAMP NULL,
			updated_at TIMESTAMP NOT NULL DEFAULT NOW()
		)`,
		`CREATE TABLE IF NOT EXISTS s3_multipart_parts (
			upload_id VARCHAR(100) NOT NULL REFERENCES s3_multipart_uploads(id) ON DELETE CASCADE,
			part_number INTEGER NOT NULL,
			staging_path TEXT NOT NULL,
			etag VARCHAR(255) NOT NULL,
			size BIGINT NOT NULL,
			checksum_sha256 VARCHAR(255),
			created_at TIMESTAMP NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
			PRIMARY KEY (upload_id, part_number)
		)`,
		`CREATE TABLE IF NOT EXISTS s3_multipart_staging_usage (
			user_id VARCHAR(50) PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
			bytes BIGINT NOT NULL DEFAULT 0 CHECK (bytes >= 0),
			updated_at TIMESTAMP NOT NULL DEFAULT NOW()
		)`,
		`CREATE TABLE IF NOT EXISTS s3_object_metadata (
			user_directory TEXT NOT NULL,
			bucket VARCHAR(63) NOT NULL,
			object_key TEXT NOT NULL,
			etag VARCHAR(255) NOT NULL,
			content_type TEXT NOT NULL DEFAULT 'application/octet-stream',
			updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
			PRIMARY KEY (user_directory, bucket, object_key)
		)`,

		// 创建回收站表
		`CREATE TABLE IF NOT EXISTS recycle_items (
			id VARCHAR(50) PRIMARY KEY,
			hash VARCHAR(50) UNIQUE NOT NULL,
			user_id VARCHAR(50) NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			username VARCHAR(255) NOT NULL,
			directory TEXT NOT NULL,
			name TEXT NOT NULL,
			path TEXT NOT NULL,
			is_dir BOOLEAN NOT NULL DEFAULT FALSE,
			size BIGINT NOT NULL DEFAULT 0,
			deleted_at TIMESTAMP NOT NULL DEFAULT NOW(),
			created_at TIMESTAMP NOT NULL DEFAULT NOW()
		)`,

		// 创建分享表
		`CREATE TABLE IF NOT EXISTS share_items (
			id VARCHAR(50) PRIMARY KEY,
			token VARCHAR(50) UNIQUE NOT NULL,
			user_id VARCHAR(50) NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			username VARCHAR(255) NOT NULL,
			name TEXT NOT NULL,
			path TEXT NOT NULL,
			mode VARCHAR(20) NOT NULL DEFAULT 'download',
			expires_at TIMESTAMP NULL,
			view_count BIGINT NOT NULL DEFAULT 0,
			download_count BIGINT NOT NULL DEFAULT 0,
			created_at TIMESTAMP NOT NULL DEFAULT NOW()
		)`,

		// 内部共享主表：支持单用户/分组/全员共享的统一共享对象
		`CREATE TABLE IF NOT EXISTS internal_share_items (
			id VARCHAR(50) PRIMARY KEY,
			owner_user_id VARCHAR(50) NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			owner_username VARCHAR(255) NOT NULL,
			name TEXT NOT NULL,
			path TEXT NOT NULL,
			is_dir BOOLEAN NOT NULL DEFAULT FALSE,
			permissions VARCHAR(10) NOT NULL,
			expires_at TIMESTAMP NULL,
			status VARCHAR(20) NOT NULL DEFAULT 'active',
			created_at TIMESTAMP NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMP NOT NULL DEFAULT NOW()
		)`,

		// V3 共享资源/授权模型。旧 internal_share_items 与 audiences 在切换读写路径前继续保留。
		`CREATE TABLE IF NOT EXISTS internal_shared_resources (
			id VARCHAR(50) PRIMARY KEY,
			owner_user_id VARCHAR(50) NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			normalized_path TEXT NOT NULL,
			is_dir BOOLEAN NOT NULL,
			created_at TIMESTAMP NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
			UNIQUE(owner_user_id, normalized_path, is_dir)
		)`,
		`CREATE TABLE IF NOT EXISTS internal_share_grants (
			id VARCHAR(50) PRIMARY KEY,
			resource_id VARCHAR(50) NOT NULL REFERENCES internal_shared_resources(id) ON DELETE CASCADE,
			legacy_share_id VARCHAR(50) UNIQUE NULL REFERENCES internal_share_items(id) ON DELETE RESTRICT,
			permissions VARCHAR(10) NOT NULL,
			expires_at TIMESTAMP NULL,
			status VARCHAR(20) NOT NULL DEFAULT 'active',
			created_at TIMESTAMP NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMP NOT NULL DEFAULT NOW()
		)`,

		// 分组
		`CREATE TABLE IF NOT EXISTS address_groups (
			id VARCHAR(50) PRIMARY KEY,
			user_id VARCHAR(50) NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			name VARCHAR(255) NOT NULL,
			created_at TIMESTAMP NOT NULL DEFAULT NOW()
		)`,

		// 内部共享受众表：共享对象可以绑定多个受众（用户或全员）
		`CREATE TABLE IF NOT EXISTS internal_share_audiences (
			id VARCHAR(50) PRIMARY KEY,
			share_id VARCHAR(50) NOT NULL REFERENCES internal_share_items(id) ON DELETE CASCADE,
			grant_id VARCHAR(50) NULL REFERENCES internal_share_grants(id) ON DELETE CASCADE,
			audience_type VARCHAR(20) NOT NULL,
			target_user_id VARCHAR(50) NULL REFERENCES users(id) ON DELETE CASCADE,
			target_wallet_address VARCHAR(255) NULL,
			source_group_id VARCHAR(50) NULL REFERENCES address_groups(id) ON DELETE SET NULL,
			created_at TIMESTAMP NOT NULL DEFAULT NOW()
		)`,

		// 分组成员
		`CREATE TABLE IF NOT EXISTS group_members (
			id VARCHAR(50) PRIMARY KEY,
			user_id VARCHAR(50) NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			group_id VARCHAR(50) NOT NULL REFERENCES address_groups(id) ON DELETE CASCADE,
			name VARCHAR(255) NOT NULL,
			wallet_address VARCHAR(255) NOT NULL,
			tags TEXT[] NOT NULL DEFAULT '{}',
			status VARCHAR(20) NOT NULL DEFAULT 'active',
			created_at TIMESTAMP NOT NULL DEFAULT NOW()
		)`,

		// 分组成员别名：每个用户给可见成员设置自己的私有别名
		`CREATE TABLE IF NOT EXISTS group_member_aliases (
			owner_user_id VARCHAR(50) NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			member_id VARCHAR(50) NOT NULL REFERENCES group_members(id) ON DELETE CASCADE,
			alias TEXT NOT NULL,
			updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
			PRIMARY KEY (owner_user_id, member_id)
		)`,

		// 复制 outbox：active 记录文件变更，后台异步分发到 standby
		`CREATE TABLE IF NOT EXISTS replication_outbox (
			id BIGSERIAL PRIMARY KEY,
			source_node_id TEXT NOT NULL,
			target_node_id TEXT NOT NULL,
			op TEXT NOT NULL,
			path TEXT NULL,
			from_path TEXT NULL,
			to_path TEXT NULL,
			is_dir BOOLEAN NOT NULL DEFAULT FALSE,
			content_sha256 TEXT NULL,
			file_size BIGINT NULL,
			assignment_generation BIGINT NULL,
			status TEXT NOT NULL DEFAULT 'pending',
			attempt_count INT NOT NULL DEFAULT 0,
			next_retry_at TIMESTAMP NOT NULL DEFAULT NOW(),
			last_error TEXT NULL,
			created_at TIMESTAMP NOT NULL DEFAULT NOW(),
			dispatched_at TIMESTAMP NULL
		)`,

		// 复制位点：记录 standby 已应用到哪个 outbox 序号
		`CREATE TABLE IF NOT EXISTS replication_offsets (
			source_node_id TEXT NOT NULL,
			target_node_id TEXT NOT NULL,
			assignment_generation BIGINT NULL,
			last_applied_outbox_id BIGINT NOT NULL,
			last_applied_at TIMESTAMP NOT NULL,
			updated_at TIMESTAMP NOT NULL,
			PRIMARY KEY (source_node_id, target_node_id)
		)`,

		// 历史补齐任务：记录每次 reconcile 运行情况
		`CREATE TABLE IF NOT EXISTS replication_reconcile_jobs (
			id BIGSERIAL PRIMARY KEY,
			source_node_id TEXT NOT NULL,
			target_node_id TEXT NOT NULL,
			assignment_generation BIGINT NULL,
			watermark_outbox_id BIGINT NOT NULL DEFAULT 0,
			status TEXT NOT NULL,
			scanned_items BIGINT NOT NULL DEFAULT 0,
			pending_items BIGINT NOT NULL DEFAULT 0,
			started_at TIMESTAMP NOT NULL DEFAULT NOW(),
			completed_at TIMESTAMP NULL,
			last_error TEXT NULL,
			created_at TIMESTAMP NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMP NOT NULL DEFAULT NOW()
		)`,

		// 历史补齐条目：记录任务扫描出的待补齐路径
		`CREATE TABLE IF NOT EXISTS replication_reconcile_items (
			id BIGSERIAL PRIMARY KEY,
			job_id BIGINT NOT NULL REFERENCES replication_reconcile_jobs(id) ON DELETE CASCADE,
			path TEXT NOT NULL,
			is_dir BOOLEAN NOT NULL DEFAULT FALSE,
			file_size BIGINT NULL,
			modified_at TIMESTAMP NULL,
			state TEXT NOT NULL DEFAULT 'pending',
			created_at TIMESTAMP NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
			UNIQUE(job_id, path)
		)`,

		// 共享控制面节点注册表：standby/active 通过数据库心跳注册，便于 peer 自动发现
		`CREATE TABLE IF NOT EXISTS cluster_nodes (
			node_id TEXT PRIMARY KEY,
			role TEXT NOT NULL,
			advertise_url TEXT NOT NULL,
			last_heartbeat_at TIMESTAMP NOT NULL,
			created_at TIMESTAMP NOT NULL DEFAULT TIMEZONE('UTC', NOW()),
			updated_at TIMESTAMP NOT NULL DEFAULT TIMEZONE('UTC', NOW())
		)`,

		// 共享控制面复制分配表：记录 active 与 standby 的正式 assignment。
		// 当前阶段仅用于 schema 准备与运维观察，还未接管复制流量。
		`CREATE TABLE IF NOT EXISTS cluster_replication_assignments (
			id BIGSERIAL PRIMARY KEY,
			active_node_id TEXT NOT NULL,
			standby_node_id TEXT NOT NULL,
			state TEXT NOT NULL,
			generation BIGINT NOT NULL DEFAULT 1,
			lease_expires_at TIMESTAMP NULL,
			last_reconcile_job_id BIGINT NULL,
			last_error TEXT NULL,
			failure_count INTEGER NOT NULL DEFAULT 0,
			next_retry_at TIMESTAMP NULL,
			created_at TIMESTAMP NOT NULL DEFAULT TIMEZONE('UTC', NOW()),
			updated_at TIMESTAMP NOT NULL DEFAULT TIMEZONE('UTC', NOW()),
			UNIQUE(active_node_id, standby_node_id)
		)`,

		// 站内消息盒子：用户和管理员提醒
		`CREATE TABLE IF NOT EXISTS notifications (
			id VARCHAR(50) PRIMARY KEY,
			recipient_user_id VARCHAR(50) NULL REFERENCES users(id) ON DELETE CASCADE,
			recipient_role VARCHAR(20) NOT NULL DEFAULT 'user',
			type VARCHAR(40) NOT NULL,
			title TEXT NOT NULL,
			content TEXT NOT NULL,
			severity VARCHAR(20) NOT NULL DEFAULT 'info',
			action_url TEXT NULL,
			dedupe_key TEXT NULL,
			read_at TIMESTAMP NULL,
			created_at TIMESTAMP NOT NULL DEFAULT NOW(),
			expires_at TIMESTAMP NULL
		)`,

		// 消息偏好：控制未来是否生成某类用户消息
		`CREATE TABLE IF NOT EXISTS notification_preferences (
			user_id VARCHAR(50) NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			type VARCHAR(40) NOT NULL,
			enabled BOOLEAN NOT NULL DEFAULT TRUE,
			updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
			PRIMARY KEY (user_id, type)
		)`,

		// 补充分享表字段（兼容已存在表）
		`ALTER TABLE replication_outbox ADD COLUMN IF NOT EXISTS assignment_generation BIGINT NULL`,
		`ALTER TABLE replication_offsets ADD COLUMN IF NOT EXISTS assignment_generation BIGINT NULL`,
		`ALTER TABLE replication_reconcile_jobs ADD COLUMN IF NOT EXISTS assignment_generation BIGINT NULL`,
		`ALTER TABLE cluster_nodes ALTER COLUMN created_at SET DEFAULT TIMEZONE('UTC', NOW())`,
		`ALTER TABLE cluster_nodes ALTER COLUMN updated_at SET DEFAULT TIMEZONE('UTC', NOW())`,
		`ALTER TABLE cluster_replication_assignments ALTER COLUMN created_at SET DEFAULT TIMEZONE('UTC', NOW())`,
		`ALTER TABLE cluster_replication_assignments ALTER COLUMN updated_at SET DEFAULT TIMEZONE('UTC', NOW())`,
		`ALTER TABLE cluster_replication_assignments ADD COLUMN IF NOT EXISTS failure_count INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE cluster_replication_assignments ADD COLUMN IF NOT EXISTS next_retry_at TIMESTAMP NULL`,
		`ALTER TABLE share_items ADD COLUMN IF NOT EXISTS view_count BIGINT NOT NULL DEFAULT 0`,
		`ALTER TABLE share_items ADD COLUMN IF NOT EXISTS download_count BIGINT NOT NULL DEFAULT 0`,
		`ALTER TABLE share_items ADD COLUMN IF NOT EXISTS mode VARCHAR(20) NOT NULL DEFAULT 'download'`,
		`ALTER TABLE share_items ADD COLUMN IF NOT EXISTS creator_user_id VARCHAR(50)`,
		`UPDATE share_items SET creator_user_id = user_id WHERE creator_user_id IS NULL`,
		`ALTER TABLE share_items ALTER COLUMN creator_user_id SET NOT NULL`,
		`ALTER TABLE share_items ADD COLUMN IF NOT EXISTS source_share_id VARCHAR(50) NULL`,
		`ALTER TABLE share_items ADD COLUMN IF NOT EXISTS source_resource_id VARCHAR(50) NULL`,
		`ALTER TABLE internal_share_items ADD COLUMN IF NOT EXISTS status VARCHAR(20) NOT NULL DEFAULT 'active'`,
		`ALTER TABLE internal_share_audiences ADD COLUMN IF NOT EXISTS grant_id VARCHAR(50) NULL REFERENCES internal_share_grants(id) ON DELETE CASCADE`,
		`ALTER TABLE recycle_items ADD COLUMN IF NOT EXISTS is_dir BOOLEAN NOT NULL DEFAULT FALSE`,

		// 创建回收站的哈希索引
		`CREATE INDEX IF NOT EXISTS idx_recycle_items_hash ON recycle_items(hash)`,

		// 创建回收站的用户ID索引
		`CREATE INDEX IF NOT EXISTS idx_recycle_items_user_id ON recycle_items(user_id)`,

		// 创建分享的 token 索引
		`CREATE INDEX IF NOT EXISTS idx_share_items_token ON share_items(token)`,

		// 创建分享的用户ID索引
		`CREATE INDEX IF NOT EXISTS idx_share_items_user_id ON share_items(user_id)`,
		`CREATE INDEX IF NOT EXISTS idx_share_items_creator_user_id ON share_items(creator_user_id)`,

		`CREATE INDEX IF NOT EXISTS idx_internal_share_items_owner_created
			ON internal_share_items(owner_user_id, created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_internal_share_items_path ON internal_share_items(path)`,
		`CREATE INDEX IF NOT EXISTS idx_internal_shared_resources_owner_path
			ON internal_shared_resources(owner_user_id, normalized_path)`,
		`CREATE INDEX IF NOT EXISTS idx_internal_share_grants_resource_status
			ON internal_share_grants(resource_id, status)`,
		`CREATE INDEX IF NOT EXISTS idx_internal_share_audiences_share ON internal_share_audiences(share_id)`,
		`CREATE INDEX IF NOT EXISTS idx_internal_share_audiences_grant ON internal_share_audiences(grant_id) WHERE grant_id IS NOT NULL`,
		`CREATE INDEX IF NOT EXISTS idx_internal_share_audiences_target_user
			ON internal_share_audiences(target_user_id)
			WHERE target_user_id IS NOT NULL`,
		`CREATE INDEX IF NOT EXISTS idx_internal_share_audiences_all_users
			ON internal_share_audiences(audience_type)
			WHERE audience_type = 'all_users'`,
		`CREATE INDEX IF NOT EXISTS idx_internal_share_audiences_group
			ON internal_share_audiences(source_group_id)
			WHERE source_group_id IS NOT NULL`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_internal_share_audiences_share_user
			ON internal_share_audiences(share_id, audience_type, target_user_id)
			WHERE audience_type = 'user' AND target_user_id IS NOT NULL`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_internal_share_audiences_share_all_users
			ON internal_share_audiences(share_id, audience_type)
			WHERE audience_type = 'all_users'`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_internal_share_audiences_share_group
			ON internal_share_audiences(share_id, audience_type, source_group_id)
			WHERE audience_type = 'group' AND source_group_id IS NOT NULL`,

		// 兼容历史库：旧版 group_members 表缺少审批状态列，必须先补列再执行数据迁移
		`ALTER TABLE group_members ADD COLUMN IF NOT EXISTS status VARCHAR(20) NOT NULL DEFAULT 'active'`,

		// 兼容历史库：如果旧版成员表存在，则迁移到 group_members
		`DO $$
		BEGIN
			IF to_regclass('public.address_contacts') IS NOT NULL THEN
				ALTER TABLE address_contacts ADD COLUMN IF NOT EXISTS tags TEXT[] NOT NULL DEFAULT '{}';
				INSERT INTO group_members (id, user_id, group_id, name, wallet_address, tags, status, created_at)
				SELECT id, user_id, group_id, name, wallet_address, tags, 'active', created_at
				FROM address_contacts
				WHERE group_id IS NOT NULL
				ON CONFLICT DO NOTHING;
			END IF;
		END $$`,

		// 兼容历史库：分组创建者本身也应该是分组 active 成员
		`INSERT INTO group_members (id, user_id, group_id, name, wallet_address, tags, status, created_at)
		SELECT
			'grp_owner_' || md5(g.id || '|' || g.user_id),
			g.user_id,
			g.id,
			COALESCE(NULLIF(u.username, ''), u.wallet_address),
			u.wallet_address,
			'{}'::TEXT[],
			'active',
			g.created_at
		FROM address_groups g
		JOIN users u ON u.id = g.user_id
		WHERE u.wallet_address IS NOT NULL
			AND TRIM(u.wallet_address) <> ''
			AND NOT EXISTS (
				SELECT 1
				FROM group_members m
				WHERE m.user_id = g.user_id
					AND m.group_id = g.id
					AND LOWER(m.wallet_address) = LOWER(u.wallet_address)
			)
		ON CONFLICT DO NOTHING`,

		// 分组索引
		`CREATE INDEX IF NOT EXISTS idx_address_groups_user_id ON address_groups(user_id)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_address_groups_user_name ON address_groups(user_id, name)`,

		// 分组成员索引
		`CREATE INDEX IF NOT EXISTS idx_group_members_user_id ON group_members(user_id)`,
		`CREATE INDEX IF NOT EXISTS idx_group_members_group_id ON group_members(group_id)`,
		`CREATE INDEX IF NOT EXISTS idx_group_members_group_status ON group_members(group_id, status)`,
		`CREATE INDEX IF NOT EXISTS idx_group_members_wallet_lower ON group_members(LOWER(wallet_address))`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_group_members_user_group_wallet
			ON group_members(user_id, group_id, wallet_address)`,
		`CREATE INDEX IF NOT EXISTS idx_group_member_aliases_member_id ON group_member_aliases(member_id)`,

		// 复制 outbox 索引
		`CREATE INDEX IF NOT EXISTS idx_replication_outbox_pair_pending
			ON replication_outbox(source_node_id, target_node_id, status, next_retry_at, id)`,
		`CREATE INDEX IF NOT EXISTS idx_replication_outbox_pair_created
			ON replication_outbox(source_node_id, target_node_id, created_at)`,
		`CREATE INDEX IF NOT EXISTS idx_replication_reconcile_jobs_pair
			ON replication_reconcile_jobs(source_node_id, target_node_id, id DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_replication_reconcile_items_job_state
			ON replication_reconcile_items(job_id, state, id)`,
		`CREATE INDEX IF NOT EXISTS idx_cluster_nodes_role_heartbeat
			ON cluster_nodes(role, last_heartbeat_at DESC, node_id)`,
		`CREATE INDEX IF NOT EXISTS idx_cluster_replication_assignments_active
			ON cluster_replication_assignments(active_node_id, updated_at DESC, id DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_cluster_replication_assignments_standby
			ON cluster_replication_assignments(standby_node_id, updated_at DESC, id DESC)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_cluster_replication_assignments_standby_effective
			ON cluster_replication_assignments(standby_node_id)
			WHERE state IN ('pending', 'reconciling', 'replicating', 'draining')`,

		// 消息盒子索引
		`CREATE INDEX IF NOT EXISTS idx_notifications_user_created
			ON notifications(recipient_user_id, created_at DESC)
			WHERE recipient_user_id IS NOT NULL`,
		`CREATE INDEX IF NOT EXISTS idx_notifications_role_created
			ON notifications(recipient_role, created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_notifications_user_unread
			ON notifications(recipient_user_id, read_at)
			WHERE recipient_user_id IS NOT NULL`,
		`CREATE INDEX IF NOT EXISTS idx_notifications_role_unread
			ON notifications(recipient_role, read_at)`,
		`CREATE INDEX IF NOT EXISTS idx_notification_preferences_user
			ON notification_preferences(user_id)`,

		// 创建钱包地址索引
		`CREATE INDEX IF NOT EXISTS idx_users_wallet_address ON users(wallet_address) WHERE wallet_address IS NOT NULL`,

		// 创建 Identity DID 索引
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_users_identity_did ON users(identity_did) WHERE identity_did IS NOT NULL`,

		// 创建邮箱索引
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_users_email ON users(email) WHERE email IS NOT NULL`,

		// 创建用户名索引
		`CREATE INDEX IF NOT EXISTS idx_users_username ON users(username)`,

		// 访问密钥索引
		`CREATE INDEX IF NOT EXISTS idx_webdav_access_keys_owner_created
			ON webdav_access_keys(owner_user_id, created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_webdav_access_keys_owner_status
			ON webdav_access_keys(owner_user_id, status, created_at DESC)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_webdav_access_keys_owner_name
			ON webdav_access_keys(owner_user_id, name)`,
		`CREATE INDEX IF NOT EXISTS idx_s3_credentials_owner_status
			ON s3_credentials(owner_user_id, status, created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_s3_multipart_owner_status
			ON s3_multipart_uploads(owner_user_id, status, expires_at)`,
		`CREATE INDEX IF NOT EXISTS idx_s3_multipart_expiry
			ON s3_multipart_uploads(status, expires_at)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_s3_credentials_owner_name
			ON s3_credentials(owner_user_id, name)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_warehouse_tool_credentials_owner_name
			ON warehouse_tool_credentials(owner_user_id, name)`,
		`CREATE INDEX IF NOT EXISTS idx_warehouse_tool_credentials_secret_status
			ON warehouse_tool_credentials(status, expires_at)`,
		`CREATE INDEX IF NOT EXISTS idx_warehouse_tool_credential_audits_owner_created
			ON warehouse_tool_credential_audits(owner_user_id, created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_webdav_access_key_bindings_key
			ON webdav_access_key_bindings(access_key_id, root_path)`,
		`CREATE INDEX IF NOT EXISTS idx_webdav_access_key_bindings_owner
			ON webdav_access_key_bindings(owner_user_id, root_path)`,

		// 创建用户规则的用户ID索引
		`CREATE INDEX IF NOT EXISTS idx_user_rules_user_id ON user_rules(user_id)`,

		// 创建更新时间触发器函数
		`CREATE OR REPLACE FUNCTION update_updated_at_column()
		RETURNS TRIGGER AS $$
		BEGIN
			NEW.updated_at = NOW();
			RETURN NEW;
		END;
		$$ language 'plpgsql'`,

		// 创建用户表的更新时间触发器
		`DROP TRIGGER IF EXISTS update_users_updated_at ON users`,
		`CREATE TRIGGER update_users_updated_at
		BEFORE UPDATE ON users
		FOR EACH ROW
		EXECUTE FUNCTION update_updated_at_column()`,

		// 创建内部共享表的更新时间触发器
		`DROP TRIGGER IF EXISTS update_internal_share_items_updated_at ON internal_share_items`,
		`CREATE TRIGGER update_internal_share_items_updated_at
		BEFORE UPDATE ON internal_share_items
		FOR EACH ROW
		EXECUTE FUNCTION update_updated_at_column()`,

		// 创建访问密钥表的更新时间触发器
		`DROP TRIGGER IF EXISTS update_webdav_access_keys_updated_at ON webdav_access_keys`,
		`CREATE TRIGGER update_webdav_access_keys_updated_at
		BEFORE UPDATE ON webdav_access_keys
		FOR EACH ROW
		EXECUTE FUNCTION update_updated_at_column()`,

		// 历史升级：若旧 share_user_items 存在，则一次性导入到 internal_share_*。
		`DO $$
		BEGIN
			IF to_regclass('public.share_user_items') IS NOT NULL THEN
				ALTER TABLE share_user_items ADD COLUMN IF NOT EXISTS is_dir BOOLEAN NOT NULL DEFAULT FALSE;
				ALTER TABLE share_user_items ADD COLUMN IF NOT EXISTS permissions VARCHAR(10) NOT NULL DEFAULT 'R';
				ALTER TABLE share_user_items ADD COLUMN IF NOT EXISTS expires_at TIMESTAMP NULL;

				CREATE INDEX IF NOT EXISTS idx_share_user_items_owner_id ON share_user_items(owner_user_id);
				CREATE INDEX IF NOT EXISTS idx_share_user_items_target_id ON share_user_items(target_user_id);
				CREATE INDEX IF NOT EXISTS idx_share_user_items_target_wallet ON share_user_items(target_wallet_address);

				INSERT INTO internal_share_items (
					id, owner_user_id, owner_username, name, path, is_dir, permissions, expires_at, status, created_at, updated_at
				)
				SELECT
					s.id,
					s.owner_user_id,
					s.owner_username,
					s.name,
					s.path,
					s.is_dir,
					s.permissions,
					s.expires_at,
					'active',
					s.created_at,
					s.created_at
				FROM share_user_items s
				ON CONFLICT (id) DO NOTHING;

				INSERT INTO internal_share_audiences (
					id, share_id, audience_type, target_user_id, target_wallet_address, source_group_id, created_at
				)
				SELECT
					'aud_' || md5(s.id || '|user|' || s.target_user_id || '|' || s.target_wallet_address || '|'),
					s.id,
					'user',
					s.target_user_id,
					s.target_wallet_address,
					NULL,
					s.created_at
				FROM share_user_items s
				ON CONFLICT (id) DO NOTHING;
			END IF;
		END $$`,
	}

	tx, err := p.DB.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// 多实例共用数据库时只允许一个实例执行结构升级，其他实例等待事务完成。
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock($1)`, schemaMigrationLockID); err != nil {
		return fmt.Errorf("failed to acquire schema migration lock: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version VARCHAR(50) PRIMARY KEY,
		name VARCHAR(255) NOT NULL,
		applied_at TIMESTAMP NOT NULL DEFAULT NOW()
	)`); err != nil {
		return fmt.Errorf("failed to initialize schema migration history: %w", err)
	}

	for _, query := range queries {
		if _, err := tx.ExecContext(ctx, query); err != nil {
			return fmt.Errorf("failed to execute migration query: %w", err)
		}
	}

	if err := ensureNotificationDedupeConstraint(ctx, tx); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations (version, name)
		VALUES ($1, $2)
		ON CONFLICT (version) DO UPDATE SET name = EXCLUDED.name`,
		notificationDedupeMigrationVersion,
		notificationDedupeMigrationName,
	); err != nil {
		return fmt.Errorf("failed to record notification schema migration: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

type notificationConstraintQuerier interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

type notificationConstraintDB interface {
	notificationConstraintQuerier
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

func ensureNotificationDedupeConstraint(ctx context.Context, db notificationConstraintDB) error {
	valid, err := notificationDedupeConstraintValid(ctx, db)
	if err != nil {
		return fmt.Errorf("failed to inspect notification dedupe constraint: %w", err)
	}
	if valid {
		return nil
	}

	queries := []string{
		`WITH duplicate_notifications AS (
			SELECT
				id,
				dedupe_key,
				ROW_NUMBER() OVER (
					PARTITION BY dedupe_key
					ORDER BY created_at DESC, id DESC
				) AS row_number
			FROM notifications
			WHERE dedupe_key IS NOT NULL
		)
		UPDATE notifications n
		SET dedupe_key = n.dedupe_key || ':legacy:' || n.id
		FROM duplicate_notifications d
		WHERE n.id = d.id AND d.row_number > 1`,
		`DROP INDEX IF EXISTS idx_notifications_dedupe_key`,
		`CREATE UNIQUE INDEX idx_notifications_dedupe_key
			ON notifications(dedupe_key)
			WHERE dedupe_key IS NOT NULL`,
	}
	for _, query := range queries {
		if _, err := db.ExecContext(ctx, query); err != nil {
			return fmt.Errorf("failed to repair notification dedupe constraint: %w", err)
		}
	}
	return verifyNotificationDedupeConstraint(ctx, db)
}

func notificationDedupeConstraintValid(ctx context.Context, db notificationConstraintQuerier) (bool, error) {
	unique, columnName, predicate, err := loadNotificationDedupeConstraint(ctx, db)
	if err != nil {
		if err == sql.ErrNoRows {
			return false, nil
		}
		return false, err
	}
	normalizedPredicate := strings.ToLower(strings.Join(strings.Fields(predicate), " "))
	return unique && columnName == "dedupe_key" && strings.Contains(normalizedPredicate, "dedupe_key") && strings.Contains(normalizedPredicate, "is not null"), nil
}

// verifyNotificationDedupeConstraint prevents the service from becoming ready with a
// notification schema that cannot satisfy UpsertByDedupeKey's ON CONFLICT target.
func verifyNotificationDedupeConstraint(ctx context.Context, db notificationConstraintQuerier) error {
	valid, err := notificationDedupeConstraintValid(ctx, db)
	if err != nil {
		return fmt.Errorf("failed to verify notification dedupe constraint: %w", err)
	}
	if !valid {
		return fmt.Errorf("notification dedupe constraint is incompatible: expected UNIQUE (dedupe_key) WHERE dedupe_key IS NOT NULL")
	}
	return nil
}

func loadNotificationDedupeConstraint(ctx context.Context, db notificationConstraintQuerier) (bool, string, string, error) {
	var unique bool
	var columnName string
	var predicate string
	err := db.QueryRowContext(ctx, `
		SELECT i.indisunique, a.attname, COALESCE(pg_get_expr(i.indpred, i.indrelid), '')
		FROM pg_class idx
		JOIN pg_index i ON i.indexrelid = idx.oid
		JOIN pg_class tbl ON tbl.oid = i.indrelid
		JOIN pg_namespace ns ON ns.oid = tbl.relnamespace
		JOIN pg_attribute a ON a.attrelid = tbl.oid AND a.attnum = i.indkey[0]
		WHERE ns.nspname = current_schema()
			AND tbl.relname = 'notifications'
			AND idx.relname = 'idx_notifications_dedupe_key'
			AND i.indnatts = 1
	`).Scan(&unique, &columnName, &predicate)
	return unique, columnName, predicate, err
}
