# scripts 目录脚本说明

本文档说明 `scripts/` 目录下脚本、模板和运维 SQL 的用途、常见用法与执行注意事项，便于部署、升级、测试和日常开发时引用。

## 脚本总览

| 文件 | 用途 | 常见执行方式 |
| --- | --- | --- |
| `config_backup.sh` | 备份当前部署目录下的配置文件，并生成加密压缩包 | `bash scripts/config_backup.sh` |
| `copy-for-upgrade.sh` | 升级时把当前版本的 `config.yaml` 复制到目标版本目录 | `bash scripts/copy-for-upgrade.sh /path/to/target` |
| `health-check.sh` | 执行 Warehouse 存活、就绪和依赖健康检查 | `bash scripts/health-check.sh --level readiness` |
| `local.sh` | 基于现有 `config.yaml` 生成本地 active/standby 调试配置并启动服务 | `bash scripts/local.sh active` |
| `merge.sh` | 同步分支、推送当前分支并辅助创建 GitHub Pull Request | `bash scripts/merge.sh` |
| `mount_davfs.sh` | 挂载或配置 WebDAV 目录到本地文件系统 | `bash scripts/mount_davfs.sh mount <url> <mount_point> <username> [password]` |
| `package.sh` | 构建前端和后端产物，并生成发布安装包 | `bash scripts/package.sh [vX.Y.Z]` |
| `starter.sh` | 启动、停止或重启已打包部署的 Warehouse 服务 | `bash scripts/starter.sh start` |
| `sync.sh` | 从 upstream 拉取当前分支并执行 rebase，可选推送到 origin | `bash scripts/sync.sh` |
| `test.sh` | 统一执行单元、集成、冒烟和端到端测试 | `bash scripts/test.sh --suite unit` |
| `fix_control_plane_timestamps.sql` | 修复历史控制面表中存在本地时区残留的时间字段 | `psql "$DATABASE_URL" -v source_tz='Asia/Shanghai' -f scripts/fix_control_plane_timestamps.sql` |
| `backup.conf.template` | `config_backup.sh` 的备份配置模板 | 复制为 `/data/<module>/backup.conf` 后按环境修改 |

## `config_backup.sh`

用于在部署环境中备份配置文件。脚本会读取 `/data/<module>/backup.conf`，根据开关决定是否执行备份，并使用 `/data/<module>/.passphrase-file` 中的口令通过 GPG 生成加密压缩包。

- 备份源：项目根目录下的 `config.yaml`。
- 可选备份源：如果存在 `/etc/nginx/conf.d/test-webdv.conf` 或 `/etc/nginx/conf.d/warehouse.conf`，也会一并备份。
- 备份内容布局：所有文件直接放在临时备份目录根部，不保留 `/etc/nginx/conf.d/` 子目录结构。
- 输出目录：默认写入 `/opt/backup`。
- 日志目录：默认写入 `/opt/logs/config-backup-<module>.log`。
- 运行时配置目录：`/data/<module>/`，其中 `<module>` 为当前模块名；当部署目录名形如 `<module>-v<version>-<hash>` 时，会自动取版本前的模块名。

返回值：

- `0`：备份成功，或 `BACKUP_CONF_FLAG=False` 时跳过备份。
- `1`：配置缺失、口令文件缺失、复制失败或加密失败。
- `255`：目标备份文件已存在，脚本不会覆盖旧备份。

## `copy-for-upgrade.sh`

用于升级过程中把当前版本配置复制到目标版本目录。

用法：

```bash
bash scripts/copy-for-upgrade.sh <target-dir>
```

行为：

- 必须传入一个参数，参数为目标版本目录的全路径。
- 从当前项目根目录读取 `config.yaml`。
- 将 `config.yaml` 复制到目标目录下，并直接替换目标目录中已有的 `config.yaml`。
- 不创建备份文件。

返回值：

- `0`：复制成功。
- 非 `0`：参数数量不正确、源配置不存在、目标目录不存在或复制失败。

## `health-check.sh`

用于检查 Warehouse 服务健康状态，支持存活检查、就绪检查、依赖检查和全量检查。

常见用法：

```bash
bash scripts/health-check.sh --level readiness
bash scripts/health-check.sh --level all --format json
bash scripts/health-check.sh --base-url http://localhost:6065 --timeout 10
```

主要参数：

- `--level <level>`：检查级别，支持 `liveness`、`readiness`、`dependency`、`all`，默认 `readiness`。
- `--timeout <seconds>`：单项检查超时时间，默认 `10` 秒。
- `--retries <count>`：失败后的重试次数，默认 `0`。
- `--interval <seconds>`：重试间隔，默认 `2` 秒。
- `--format <format>`：输出格式，支持 `text` 或 `json`，默认 `text`。
- `--config <path>`：指定 Warehouse 配置文件，用于解析本地服务地址。
- `--base-url <url>`：直接指定 Warehouse HTTP 地址。
- `--quiet`：仅输出最终结果和错误信息。

返回值：

- `0`：检查通过。
- `1`：检查失败。
- `2`：命令行参数错误。
- `3`：依赖命令缺失或健康检查框架执行异常。
- `4`：检查超时。

## `local.sh`

用于本地调试 active/standby 双实例。脚本会基于项目根目录下已有的 `config.yaml` 生成本地临时配置，并以前台方式执行 `go run ./cmd/warehouse -c <generated-config>`。

用法：

```bash
bash scripts/local.sh
bash scripts/local.sh active
bash scripts/local.sh standby
```

说明：

- 默认启动 `active`。
- 生成配置文件到 `.tmp/active.yaml` 或 `.tmp/standby.yaml`。
- 数据目录分别使用 `.tmp/active/data` 和 `.tmp/standby/data`。
- 可通过环境变量 `ACTIVE_PORT`、`STANDBY_PORT`、`INTERNAL_SHARED_SECRET` 覆盖默认值。
- 需要先准备项目根目录下的 `config.yaml`。
- 依赖 `go` 和 `awk`。

## `merge.sh`

用于辅助开发分支合并流程。脚本会检查 GitHub CLI，按配置同步远程分支、推送当前分支，并创建 Pull Request。

主要配置项：

- `DEFAULT_BASE_BRANCH`：默认 PR 目标分支，当前为 `main`。
- `AUTO_PUSH`：是否自动推送当前分支到 `origin`，默认 `true`。
- `INTERACTIVE`：创建 PR 前是否交互确认，默认 `true`。
- `AUTO_FILL_PR`：是否使用最近提交信息自动填充 PR 标题和描述，默认 `true`。
- `PREFER_GH_AUTH_LOGIN`：检测到 `GH_TOKEN` 时是否优先使用本地 `gh auth login` 凭据，默认 `false`。

注意事项：

- 依赖 Git 和 GitHub CLI `gh`。
- Linux/macOS 环境下可尝试自动安装 `gh`。
- 会根据仓库远程地址推断 GitHub 仓库信息。
- 执行前应确认当前分支、远程仓库和工作区状态符合合并预期。

## `mount_davfs.sh`

用于通过 `davfs2` 挂载 WebDAV 地址，或写入 `/etc/fstab` 以支持开机自动挂载。

用法：

```bash
bash scripts/mount_davfs.sh mount <url> <mount_point> <username> [password]
bash scripts/mount_davfs.sh umount <mount_point>
bash scripts/mount_davfs.sh install-fstab <url> <mount_point> <username> [password]
bash scripts/mount_davfs.sh remove-fstab <mount_point>
```

说明：

- 如果未传入密码，脚本会从终端安全读取。
- 凭据会写入 `/etc/davfs2/secrets`，并设置为 `600` 权限。
- 需要 `sudo` 权限。
- Linux 环境缺少 `davfs2` 时，脚本会尝试通过系统包管理器自动安装。

## `package.sh`

用于发布打包。脚本会准备版本 tag、构建前端资源、构建后端二进制，并在 `output/` 下生成安装包。

用法：

```bash
bash scripts/package.sh
bash scripts/package.sh v1.2.3
```

行为：

- 不传 tag 时，从 `main` 分支和已有语义化 tag 推导下一个 patch 版本。
- 传入 tag 时，仅当该 tag 已存在时才使用该 tag 打包。
- 构建前端：在 `web/` 下安装依赖并执行 `npm run build`。
- 构建后端：执行 `go build` 生成 `build/warehouse`。
- 打包内容包括二进制、前端资源、启动脚本、健康检查脚本、测试脚本、备份脚本和配置模板。
- macOS 下会尽量清理 AppleDouble 和扩展属性元数据，避免污染 tar 包。

依赖：

- `git`
- `npm`
- `go`
- `tar`

输出：

- 安装包路径：`output/<project>-<tag>-<git_hash>.tar.gz`。

## `starter.sh`

用于已打包部署目录中的服务生命周期管理。

用法：

```bash
bash scripts/starter.sh start
bash scripts/starter.sh stop
bash scripts/starter.sh restart
```

说明：

- 默认动作为 `start`。
- 启动二进制：`bin/warehouse`。
- 优先使用项目根目录下的 `config.yaml`，不存在时回退到 `config.yaml.template`。
- PID 文件：`run/warehouse.pid`。
- 日志文件：`logs/warehouse.log`。
- `stop` 会先发送普通终止信号，超时后再发送 `SIGKILL`。

## `sync.sh`

用于同步当前分支与 upstream 仓库对应分支。

行为：

- 检查当前目录是否在 Git 仓库内。
- 检查当前分支是否为有效分支，拒绝在 detached HEAD 状态下执行。
- 如果没有 `upstream` 远程，会提示输入上游仓库地址，并默认使用 `git@github.com:yeying-community/<project>.git`。
- 拉取 `upstream` 后，将当前分支 rebase 到 `upstream/<current-branch>`。
- 工作区或暂存区存在未提交变更时会拒绝继续。
- 可通过 `AUTO_PUSH=false` 禁止 rebase 后自动推送到 `origin`。

## `test.sh`

用于统一运行 Warehouse 的自动化测试套件，并支持文本、JSON 和 JUnit 格式报告。

常见用法：

```bash
bash scripts/test.sh --suite unit
bash scripts/test.sh --suite smoke --base-url http://localhost:6065
bash scripts/test.sh --suite all --format junit --output test-report.xml
```

主要参数：

- `--suite <suite>`：测试套件，支持 `unit`、`integration`、`smoke`、`e2e`、`all`，默认 `unit`。
- `--timeout <seconds>`：整体测试超时时间，默认 `600` 秒。
- `--format <format>`：报告格式，支持 `text`、`json`、`junit`，默认 `text`。
- `--output <path>`：将最终报告写入文件。
- `--case <id>`：只运行指定用例，可重复指定。
- `--base-url <url>`：冒烟测试目标服务地址。
- `--environment <name>`：环境名称，支持 `local`、`ci`、`test`、`staging`、`prod`。
- `--fail-fast`：首次失败后停止调度后续用例。
- `--keep-data`：调试参数，当前 smoke 套件为只读。
- `--quiet`：减少输出。
- `--verbose`：将捕获的命令输出打印到 stderr。

返回值：

- `0`：测试通过。
- `1`：存在失败用例。
- `2`：命令行参数错误。
- `3`：依赖命令缺失或测试框架执行异常。
- `4`：测试超时。

## `fix_control_plane_timestamps.sql`

用于修复历史控制面数据中因 UTC 规范化前写入导致的本地时区残留问题。

用法：

```bash
psql "$DATABASE_URL" -v source_tz='Asia/Shanghai' -f scripts/fix_control_plane_timestamps.sql
```

修复范围：

- `cluster_nodes.created_at`
- `cluster_nodes.updated_at`
- `cluster_replication_assignments.created_at`
- `cluster_replication_assignments.updated_at`

说明：

- 默认源时区为 `Asia/Shanghai`。
- 只更新明显存在时区偏移的历史行。
- `lease_expires_at` 和 `last_heartbeat_at` 被视作 UTC 基准。
- 脚本开启 `ON_ERROR_STOP`，执行异常时会停止。

## 配置模板

### `backup.conf.template`

`config_backup.sh` 的配置模板。使用时复制为 `/data/<module>/backup.conf`。

可配置项：

- `BACKUP_CONF_FLAG`：是否启用备份，取值为 `True` 或 `False`。
- `BACKUP_CONF_PREFIX`：备份文件名前缀，可为空。
- `BACKUP_CONF_SUFFIX`：备份文件名后缀，默认 `.conf.tar.gz.gpg`。

### `.passphrase-file.template`

`config_backup.sh` 使用的 GPG 口令文件模板。使用时复制为 `/data/<module>/.passphrase-file`，并写入实际加密口令。

注意：

- 实际口令文件不应提交到 Git。
- 生产环境应限制该文件的访问权限。
