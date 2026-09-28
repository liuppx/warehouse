# Knowledge 数据接入

本文定义 Warehouse 支撑 Knowledge 的资产接入方案。Warehouse 保存用户第一手资料和加工产物，Knowledge 负责将资料加工成知识或记忆；目标是让 Knowledge 可以安全、可靠、可校验地读取原始资产，并把 manifest、artifact 和反馈资产写回 Warehouse。

本文只描述 Warehouse 侧的数据面能力，不定义 Agent Run、Context Manifest、Service Principal、Artifact Provenance 的业务模型。这些上层语义由 Knowledge 维护。

Knowledge、Chat、Agent 或第三方 AI 应用应通过稳定的 Warehouse 接口接入，而不是持有用户长期主密码或无限范围存储密钥。当前可用接口为 HTTP 对象 API、WebDAV、S3 和 HTTP Tool 适配入口；Tool 已覆盖 P0 只读能力和受授权约束的小对象写入。后续如提供 MCP 适配层，也必须复用相同的路径范围、权限、配额、checksum 和审计边界。资产 Tool 契约见 [资产 Tool 契约](./资产Tool契约.md)。

## 1. 目标

V2 阶段先解决四件事：

1. Knowledge 能用稳定路径读取 Warehouse 中的原始资产。
2. Knowledge 能把处理结果写回用户资产空间。
3. Warehouse 对写入结果提供 checksum、size、content type、mtime 等可验证元数据。
4. WebDAV、S3、Web 页面和 HTTP API 看到同一份资产，不产生平行数据面。

## 2. 非目标

V2 不做：

- Agent Run 控制面。
- Context Manifest schema 的业务定义。
- Service Principal 生命周期。
- Artifact Provenance 图谱。
- 向量索引、RAG 检索、agent memory。
- 独立于现有用户权限体系的新 IAM。

## 3. 资产空间约定

Warehouse 继续使用三类顶层空间：

| 空间 | 路径 | 用途 | 归属 |
| --- | --- | --- | --- |
| 个人资产 | `/personal/...` | 用户主动上传和管理的原始资料 | 用户 |
| 应用资产 | `/apps/<appId>/...` | 应用在用户授权下产生或使用的数据 | 用户 |
| 服务资产 | `/services/<service>/...` | 后台服务、Knowledge 或自动化流程为用户产生的数据 | 用户 |

V2 建议 Knowledge 写回到 `/services/knowledge/...` 下，例如：

```text
/services/knowledge/
  manifests/
    <asset-id>.json
  artifacts/
    <run-or-task-id>/
      <file>
  feedback/
    <asset-id>.json
```

这只是 Warehouse 侧推荐路径，不要求 Warehouse 理解 manifest、artifact 或 feedback 的业务语义。

## 4. HTTP 对象接口

V2 先提供一组用户态对象接口，复用现有登录态、JWT、Basic 和 UCAN 认证，不允许 WebDAV access key 直接访问普通 HTTP API。

### 4.1 查看对象元数据

```http
GET /api/v1/public/assets/object?path=/services/knowledge/artifacts/report.md
```

返回：

```json
{
  "path": "/services/knowledge/artifacts/report.md",
  "bucket": "services",
  "key": "knowledge/artifacts/report.md",
  "size": 1234,
  "etag": "...",
  "checksumSha256": "...",
  "contentType": "text/markdown; charset=utf-8",
  "modifiedAt": "2026-08-03T12:00:00Z",
  "isPrefix": false
}
```

### 4.2 下载对象内容

```http
GET /api/v1/public/assets/object/content?path=/personal/docs/source.pdf
```

响应头需要包含：

- `Content-Type`
- `Content-Length`
- `ETag`
- `X-Warehouse-Checksum-SHA256`

### 4.3 写入对象内容

```http
PUT /api/v1/public/assets/object/content?path=/services/knowledge/artifacts/report.md
X-Warehouse-Checksum-SHA256: <hex-or-base64-sha256>
Content-Type: text/markdown; charset=utf-8
```

规则：

- `path` 必须位于 `/personal`、`/apps` 或 `/services`。
- 写入必须走现有 quota、mutation recorder 和对象 metadata 逻辑。
- `X-Warehouse-Checksum-SHA256` 可选；提供时必须校验通过，否则拒绝写入。
- 返回对象元数据和 `checksumSha256`。

### 4.4 列出对象

```http
GET /api/v1/public/assets/objects?prefix=/services/knowledge/&delimiter=/
```

返回：

```json
{
  "prefix": "/services/knowledge/",
  "objects": [],
  "prefixes": ["/services/knowledge/artifacts/"]
}
```

## 5. 权限规则

V2 复用当前用户态权限：

- 普通登录用户只能访问自己的资产空间。
- UCAN app scope 仍只允许访问授权 app 下的 `/apps/<appId>/...`。
- Knowledge 如果通过用户授权接入，也必须落在用户授权边界内。
- WebDAV access key 仍只允许 WebDAV 路径，不扩大到 HTTP 对象接口。

后续如果需要服务级身份或跨用户批处理，由 Knowledge、Node、Wallet 和社区授权协议先定义，不在 Warehouse V2 中自行扩展。

## 6. 错误语义

| 场景 | HTTP 状态 |
| --- | --- |
| 未认证 | `401` |
| 路径不在允许空间 | `400` |
| UCAN app scope 不允许 | `403` |
| 对象不存在 | `404` |
| checksum 不匹配 | `400` |
| quota 不足 | `413` |
| 服务端写入失败 | `500` |

## 7. 验收标准

- Knowledge 可以通过 HTTP API 读取 `/personal` 或 `/services` 下对象，并获得 checksum。
- Knowledge 可以把 artifact 写入 `/services/knowledge/...`，WebDAV/S3/Web 页面可见同一文件。
- 写入时 checksum 不匹配会失败。
- UCAN app scope 不能借 HTTP 对象接口越权访问其他 app 或 `/personal`、`/services`。
- Warehouse 不新增 Agent Run、Context Manifest、Service Principal、Artifact Provenance 表或业务状态机。
