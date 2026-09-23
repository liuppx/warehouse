# warehouse

## 项目简介

`warehouse` 是一个面向文件管理场景的 WebDAV 服务，提供：

- 标准 WebDAV 文件访问与目录操作
- Web 管理界面
- Basic / JWT / Web3 / UCAN 认证能力
- 基于 PostgreSQL 元数据与本地文件目录的运行模式
- `1 active + N standby` 的阶段一高可用复制能力

适用场景：

- 自建个人或团队文件仓库
- 需要兼容第三方 WebDAV 客户端的文件服务
- 需要“本地盘 + PostgreSQL”而不是对象存储的部署形态

## 目录说明

```text
.
├── cmd/warehouse/                    # 服务主程序与 HA CLI 入口
├── internal/                         # 后端核心实现
├── web/                              # 前端工程（Vite + Vue 3）
├── scripts/                          # 启动、打包、本地调试、挂载等脚本
├── resources/                        # 邮件模板等静态资源
├── docs/                             # 中文设计与部署文档
├── config.yaml.template              # 配置模板
├── Dockerfile                        # 容器构建文件
└── README.md
```

关键目录补充：

- `scripts/local.sh`：本地快速拉起 active / standby 调试实例
- `scripts/package.sh`：构建前端与后端并生成安装包
- `scripts/starter.sh`：安装包内的服务启停入口
- `scripts/mount_davfs.sh`：Linux 下通过 `davfs2` 挂载 WebDAV 目录

## 本地环境要求

- macOS 或 Linux
- Go `1.24.2`
- PostgreSQL `16+` 或兼容版本

可选工具：

- Node.js `20+` 与 npm：仅在需要重建前端资源时使用
- `jq`：便于查看 JSON 接口返回
- `xq` / `libxml2-utils`：便于查看 WebDAV `PROPFIND` 返回

## 本地快速开始

### 1. 克隆代码

```bash
git clone <repo-url>
cd warehouse
```

### 2. 准备 PostgreSQL

本项目本地运行依赖 PostgreSQL。

如果你本地还没有数据库，建议先看 `deployer` 项目中的 PostgreSQL 中间件说明：

- `deployer/middleware/postgresql/README.md`

如果你的本地目录是：

- `/Users/liuxin2/Workspace/opensource/deployer`

那么可以直接进入：

```bash
cd /Users/liuxin2/Workspace/opensource/deployer/middleware/postgresql
```

再按其中的 README 启动 PostgreSQL。

### 3. 初始化本地配置

```bash
cp config.yaml.template config.yaml
```

至少需要确认这些配置：

- `database.*`
- `webdav.directory`
- `web3.jwt_secret`

如果要启用自动额度对账，还可以补充：

- `quota.auto_reconcile_enabled`
- `quota.auto_reconcile_interval`
- `quota.auto_reconcile_batch_size`
- `quota.auto_reconcile_batch_pause`

如果要本地验证 active / standby 复制，还需要确认：

- `node.id`
- `node.role`
- `node.advertise_url`
- `replication.enabled`
- `replication.shared_secret`

### 4. 启动项目

开发态最短启动方式：

```bash
go run ./cmd/warehouse -c config.yaml
```

如果你更习惯先编译再启动：

```bash
go build -o build/warehouse ./cmd/warehouse
./build/warehouse -c config.yaml
```

### 5. 本地快速验证

健康检查：

```bash
curl http://localhost:6065/api/v1/public/health/heartbeat
curl http://localhost:6065/api/v1/public/health/readiness
```

正式安装包由 `scripts/starter.sh` 启动后，使用统一健康检查脚本：

```bash
./scripts/health-check.sh
./scripts/health-check.sh --level all --format json
```

脚本默认读取项目目录下的 `config.yaml`，没有该文件时读取 `config.yaml.template`；也可以通过 `--base-url` 或 `HEALTH_BASE_URL` 指定服务地址。默认层级是 `readiness`。健康检查会强制检查 `run/warehouse.pid` 对应的进程是否存在且不是僵尸进程，因此不用于没有 PID 文件的 `go run` 开发方式。

统一自动化测试入口：

```bash
./scripts/test.sh                         # 默认执行 unit
./scripts/test.sh --suite integration
./scripts/test.sh --suite unit --format junit --output test-results/unit.xml
```

正式部署后的低风险冒烟测试使用 `--suite smoke`。将 `.env.template` 复制为被 Git 忽略的 `.env`，脚本会自动加载；CI 也可以直接注入环境变量，已导出的变量优先于文件配置。WebDAV 普通账号、WebDAV 目录访问密钥和 S3 凭证分别配置、分别验证，未配置的协议凭证用例会跳过。当前 smoke 只执行 readiness、WebDAV 只读 `PROPFIND` 和 S3 `ListBuckets`，不会创建、删除或遗留业务资产。

```bash
./scripts/test.sh --suite smoke --environment prod --format json
```

CLI readiness 检查：

```bash
./build/warehouse -c config.yaml --check-ready
```

可选的 WebDAV 验证：

```bash
# 将 <username>:<password> 替换成你本地实际可用的账号
curl -X MKCOL -u <username>:<password> http://localhost:6065/dav/demo
echo "hello" | curl -X PUT -u <username>:<password> --data-binary @- http://localhost:6065/dav/demo/hello.txt
curl -u <username>:<password> http://localhost:6065/dav/demo/hello.txt
```

## 配置说明

本地开发主要使用：

- `config.yaml`
- `config.yaml.template`

建议方式：

1. 从 `config.yaml.template` 复制出 `config.yaml`
2. 只修改本地启动必须的配置
3. 其他保持默认值，避免引入无关变量

本地最常改的配置项：

- `database.host / port / username / password / database`
- `webdav.directory`
- `web3.jwt_secret`

如果验证高可用复制，再补这些：

- `node.id`：每个实例必须唯一
- `node.role`：只能是 `active` 或 `standby`
- `node.advertise_url`：必须是其他实例可访问的 internal 地址
- `replication.shared_secret`：active / standby 必须一致
- `replication.retry_backoff_base` / `replication.max_retry_backoff`：同时影响 outbox 重试和 assignment error 自动恢复
- `replication.reconcile_auto_pause_failures`：连续 `reconcile` 失败达到阈值后自动切到 `paused`，默认 `3`，设为 `0` 表示关闭自动暂停
- `replication.lifecycle_cleanup_*` / `*_retention`：控制 active 节点的 generation 历史清理；默认每 `24h` 执行，outbox / reconcile item 保留 7 天，job 摘要保留 30 天

## 本地开发与调试

### 本地双实例调试

如果你要在一台机器上快速拉起 active / standby：

```bash
bash scripts/local.sh active
bash scripts/local.sh standby
```

说明：

- `scripts/local.sh` 只基于现有 `config.yaml` 生成 `.tmp/active.yaml` / `.tmp/standby.yaml`
- 不会直接读取 `config.yaml.template`
- 数据目录分别使用 `.tmp/active/data` 和 `.tmp/standby/data`
- 默认端口为 `6065` 和 `6066`

### 前端资源重建

如果你需要修改前端并重新构建静态资源：

```bash
cd web
npm install
npm run build
cd ..
```

### HA CLI

编译后的 `build/warehouse` 也可以作为运维 CLI 使用：

```bash
./build/warehouse ha status -c config.yaml
./build/warehouse ha assignments status -c config.yaml
./build/warehouse ha assignments pause -c config.yaml --standby-node-id warehouse-standby-1
./build/warehouse ha assignments retry -c config.yaml --standby-node-id warehouse-standby-1
./build/warehouse ha assignments resume -c config.yaml --standby-node-id warehouse-standby-1
./build/warehouse ha reconcile start -c config.yaml --target-node-id warehouse-standby-1
./build/warehouse ha reconcile status -c config.yaml --target-node-id warehouse-standby-1
./build/warehouse ha bootstrap mark -c config.yaml --peer --target-node-id warehouse-standby-1 --outbox-id 123
./build/warehouse ha cleanup history -c config.yaml --dry-run
./build/warehouse ha cleanup history -c config.yaml
```

补充说明：

- `ha assignments status` 直接读取 PostgreSQL 控制面表，不走 HTTP
- 当前可以直接观察 assignment 的 `state / generation / failure_count / next_retry_at`
- 当某条 assignment 连续失败达到阈值后，会自动切到 `paused`
- `paused` 无论来自系统自动暂停还是运维手工暂停，都需要显式 `resume`

## 本地验证方式

你可以通过下面几种方式确认项目真的跑起来了：

- 健康检查接口：
  - `GET /api/v1/public/health/heartbeat`
  - `GET /api/v1/public/health/readiness`
- 正式安装包统一健康检查脚本（由 `starter.sh` 启动并生成 PID 文件后）：
  - `scripts/health-check.sh --level liveness`
  - `scripts/health-check.sh --level readiness`
  - `scripts/health-check.sh --level dependency`
  - `scripts/health-check.sh --level all --format json`
- 统一自动化测试：
  - `scripts/test.sh`（默认 unit）
  - `scripts/test.sh --suite integration`
  - `scripts/test.sh --suite smoke --environment prod`（只读生产冒烟测试）
- WebDAV 基本操作：
  - `MKCOL`
  - `PUT`
  - `GET`
- 日志输出：
  - 开发态直接看终端输出
  - 安装包部署态查看 `logs/warehouse.log`
- 端口检查：

```bash
lsof -iTCP:6065 -sTCP:LISTEN
```

## 常见问题

### 1. 启动时报数据库连接失败

通常是：

- PostgreSQL 没启动
- `config.yaml` 里的 `database.*` 配置不对
- 本地防火墙或端口不通

优先检查：

```bash
psql -h 127.0.0.1 -p 5432 -U postgres -d warehouse
```

### 2. 启动时报 `web3.jwt_secret` 不合法

原因通常是：

- 没配置
- 长度不够

解决方式：

- 在 `config.yaml` 中设置一个至少 32 字符的随机字符串

### 3. `scripts/local.sh` 无法启动

通常是：

- 根目录没有 `config.yaml`
- `go run ./cmd/warehouse -c config.yaml` 本身就无法启动

先单实例跑通，再用 `scripts/local.sh`。

### 4. standby 一直不追平

优先检查：

- `node.advertise_url` 是否能互相访问
- `replication.shared_secret` 是否一致
- assignment 是否已经进入 `paused`
- `ha assignments status` 中的 `failure_count` / `next_retry_at` / `last_error`

### 5. WebDAV 客户端能连上但上传失败

优先检查：

- `webdav.directory` 是否可写
- 当前账号或访问密钥是否具备对应权限
- 反向代理是否限制了请求体大小

## 相关文档

- 文档入口：[docs/README.md](docs/README.md)
- 文档总入口：[docs/README.md](docs/README.md)
- 用户指南：[docs/产品概览/用户指南.md](docs/产品概览/用户指南.md)
- 当前架构：[docs/产品架构/架构演进/系统架构-V2.md](docs/产品架构/架构演进/系统架构-V2.md)
- JSON API 契约：[docs/应用集成/OpenAPI/接口契约说明.md](docs/应用集成/OpenAPI/接口契约说明.md)
- 部署手册：[docs/运维部署/部署手册.md](docs/运维部署/部署手册.md)
- 运维手册：[docs/运维部署/运维手册.md](docs/运维部署/运维手册.md)

README 只覆盖本地开发、调试和最短启动路径。  
正式环境部署、Nginx/入口配置、active/standby 上线、验证与回滚，请直接看部署手册。

## quota 运维示例

如果开启：

- `quota.auto_reconcile_enabled=true`
- `quota.auto_reconcile_interval=6h`
- `quota.auto_reconcile_batch_size=100`
- `quota.auto_reconcile_batch_pause=0s`

系统会在后台周期性分批重算所有用户的 `used_space`，自动修正“主路径增量记账”长期运行后的统计漂移。该任务默认关闭，只在非 `standby` 节点运行。

查看某个用户当前记录值与重算值是否一致：

```bash
./bin/warehouse quota check -c config.yaml --username alice
```

把某个用户的 `used_space` 修正为当前重算值：

```bash
./bin/warehouse quota rebuild -c config.yaml --username alice
```

清理历史误入回收站的 WebDAV 同步运行态对象：

```bash
./bin/warehouse recycle clean-sync-artifacts -c config.yaml --dry-run
./bin/warehouse recycle clean-sync-artifacts -c config.yaml
```
