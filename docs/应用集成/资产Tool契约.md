# Warehouse 资产 Tool 契约

本文定义 Warehouse 面向 Chat、Knowledge、Agent 和其他模型客户端的第一批资产 Tool。当前已提供 HTTP Tool 适配入口，但不表示 Warehouse 已经提供独立 MCP Server。

## 1. 接入边界

Warehouse 是用户第一手资料和对象事实的数据面，负责：

- 逻辑资产空间、目录和对象内容；
- 对象大小、类型、ETag、SHA-256 和修改时间；
- 认证、授权范围、配额、条件写入和存储错误；
- WebDAV、S3 和 JSON HTTP API 的协议接入。

Knowledge 负责资料导入、解析、检索、Context、证据、Provenance 和 Agent Run。Agent 负责长任务、工具装配、重试、取消和运行诊断。Warehouse 不在本契约中新增这些上层业务模型。

调用链应保持为：

```text
Chat / Agent
    ↓ HTTP Tool 适配，后续可增加 MCP 适配
Warehouse HTTP/OpenAPI
    ↓ Warehouse 自身认证、范围检查、配额和审计
资产空间、目录、对象和元数据
```

MCP 只能作为适配协议，不能绕过 Warehouse API 直接访问数据库、文件系统或内部服务。

当前 HTTP Tool 入口：

| 入口 | 用途 |
| --- | --- |
| `GET /api/v1/public/tools/warehouse` | 返回当前支持的 Warehouse Tool 定义 |
| `POST /api/v1/public/tools/warehouse/call` | 按 Tool 名称调用 P0 只读能力 |

## 2. Tool 命名

Tool 名称使用：

```text
warehouse.<resource>.<action>
```

Tool 路线：

| Tool | 当前 API 映射 | 副作用 | 状态 |
| --- | --- | --- | --- |
| `warehouse.space.list` | `GET /api/v1/public/assets/spaces` | 无 | 已通过 HTTP Tool 入口暴露 |
| `warehouse.object.list` | `GET /api/v1/public/assets/objects` | 无 | 已通过 HTTP Tool 入口暴露 |
| `warehouse.object.stat` | `GET /api/v1/public/assets/object` | 无 | 已通过 HTTP Tool 入口暴露 |
| `warehouse.object.read` | `GET/HEAD /api/v1/public/assets/object/content` | 无 | 已通过 HTTP Tool 入口暴露 |
| `warehouse.object.put` | `PUT /api/v1/public/assets/object/content` | 写入 | P1，未作为 Tool 暴露 |
| `warehouse.upload.create` | `POST /api/v1/public/uploads/sessions` | 创建上传会话 | P1，未作为 Tool 暴露 |
| `warehouse.upload.complete` | `POST /api/v1/public/uploads/sessions/{sessionId}/complete` | 写入对象 | P1，未作为 Tool 暴露 |
| `warehouse.object.copy` | 需要稳定的服务接口 | 写入 | P2，未实现 |
| `warehouse.object.delete` | 需要明确的服务语义 | 删除 | P2，未作为 Tool 暴露 |

已暴露的 P0 Tool 用于先验证读取闭环；P1 用于 Knowledge 回写 artifact；P2 涉及不可逆或复杂副作用，必须先完成确认、幂等、审计和回滚设计。

## 3. P0 Tool 契约

### 3.1 `warehouse.space.list`

用途：列出当前调用身份可访问的逻辑资产空间。

输入：

```json
{}
```

调用示例：

```json
{
  "name": "warehouse.space.list"
}
```

输出至少包含：

```json
{
  "items": [
    {
      "name": "personal",
      "rootPath": "/personal"
    }
  ]
}
```

### 3.2 `warehouse.object.list`

用途：按授权范围列出对象或一层公共前缀。

输入：

```json
{
  "prefix": "/services/knowledge/",
  "delimiter": "/"
}
```

调用示例：

```json
{
  "name": "warehouse.object.list",
  "arguments": {
    "prefix": "/services/knowledge/",
    "delimiter": "/"
  }
}
```

约束：

- `prefix` 必须落在 `/personal`、`/apps` 或 `/services` 下；
- 返回结果必须经过 Warehouse 的路径范围和权限检查；
- 不把宿主机物理路径、数据库字段或内部目录暴露给模型；
- 大结果集应使用分页或后续游标协议，不要求模型一次接收全部文件列表。

### 3.3 `warehouse.object.stat`

用途：获取对象事实，供 Knowledge 绑定输入、输出和版本。

输入：

```json
{
  "path": "/services/knowledge/runs/run-123/manifest.json"
}
```

调用示例：

```json
{
  "name": "warehouse.object.stat",
  "arguments": {
    "path": "/services/knowledge/runs/run-123/manifest.json"
  }
}
```

输出至少包含：

```json
{
  "path": "/services/knowledge/runs/run-123/manifest.json",
  "size": 2048,
  "contentType": "application/json",
  "etag": "…",
  "checksumSha256": "…",
  "modifiedAt": "2026-09-15T00:00:00Z"
}
```

`checksumSha256`、`size`、`etag` 和 `modifiedAt` 是存储侧事实；Knowledge 不应仅凭文件名判断版本或内容是否变化。

### 3.4 `warehouse.object.read`

用途：读取调用身份有权访问的对象内容，或只读取对象头。

输入：

```json
{
  "path": "/personal/research/source.pdf",
  "mode": "content",
  "maxBytes": 1048576
}
```

约束：

- 读取必须绑定用户或受控服务身份；
- 大文件应优先使用受限上传/下载能力，不把完整内容直接塞入模型上下文；
- `mode=head` 只返回对象 metadata；
- `mode=content` 返回小对象内容，默认最大 1MiB，调用方可通过 `maxBytes` 下调或上调，服务端最大允许 5MiB；
- UTF-8 文本返回 `encoding=utf-8`，二进制返回 `encoding=base64`；
- Tool 返回内容时应同时返回 `path`、`etag`、`checksumSha256` 和 `size`；
- 不把用户主密码、钱包私钥或长期管理员密钥传给模型。

## 4. 写入和长任务

P1 写入 Tool 在实现前必须补齐：

1. 短期、可撤销、目录范围受限的 scoped credential；
2. `Idempotency-Key` 或等价幂等键；
3. `If-Match` / `If-None-Match` 条件写语义；
4. `X-Warehouse-Checksum-SHA256` 校验和；
5. 上传会话的过期、取消、重试和完整性校验；
6. `requestId`、`traceId`、调用主体、目标路径和结果审计。

大文件、批量导入和模型产物不应要求单个 Tool 调用长时间保持连接。应返回 `taskId` 或 `uploadId`，由 Agent 或 Knowledge 查询状态、处理重试和展示最终结果。

## 5. 权限和审计

每个 Tool 调用至少需要能够确定：

- `subject`：用户或受控服务主体；
- `owner`：资产归属用户；
- `root`：授权根目录；
- `actions`：允许的动作；
- `expiresAt`：授权过期时间；
- `requestId` / `traceId`：请求和链路标识。

MCP Server、Chat 或 Agent 可以做前置策略判断，但最终权限必须由 Warehouse 执行。拒绝结果使用结构化错误码，例如：

```text
AUTH_INVALID
SCOPE_DENIED
OBJECT_NOT_FOUND
OBJECT_CONFLICT
CHECKSUM_MISMATCH
QUOTA_EXCEEDED
UPLOAD_EXPIRED
RATE_LIMITED
STORAGE_UNAVAILABLE
```

## 6. 首条验证闭环

建议先实现以下低风险流程：

```text
用户授权 Knowledge 访问一个目录
        ↓
Knowledge / Agent 调用 warehouse.object.list
        ↓
调用 warehouse.object.stat 和 warehouse.object.read
        ↓
Knowledge 生成带来源引用的 context
        ↓
Knowledge 将 manifest 或 artifact 写回 Warehouse
        ↓
通过 stat 校验 path、size、checksumSha256、etag
```

验收要求：

- 未授权路径被 Warehouse 拒绝；
- 同一对象的内容和元数据可以通过 `checksumSha256` 复核；
- 调用失败能返回稳定错误码和 `requestId`；
- 重试不会伪造成功或覆盖不应覆盖的对象；
- Chat / Agent 能明确区分已完成、异步处理中、部分成功和失败；
- 整条链路可以从 `runId`、`requestId` 或 `traceId` 定位到授权和对象变更。

## 7. 当前状态

当前 Warehouse 已通过 HTTP Tool 适配入口暴露 P0 只读资产 Tool，并继续复用已有认证、UCAN app scope、资产路径和对象服务。独立 MCP 适配层、统一 scoped credential、对象事件补拉和完整审计查询仍需按实际调用场景逐步实现。

因此，本文中的 Tool 名称是统一的目标契约；只有在代码、OpenAPI、测试和部署验证同步完成后，才能标记为已上线能力。
