# Warehouse Tool 凭证与授权方案

本文定义模型客户端、Skill 和 Agent 调用 Warehouse HTTP Tool 的凭证边界。独立 Tool credential 已上线最小闭环。

## 1. 当前状态

HTTP Tool 当前复用 Warehouse 用户态 Bearer JWT：

```http
Authorization: Bearer <Warehouse 登录 JWT>
```

客户端可以配置：

```bash
YEYING_WAREHOUSE_URL=http://localhost:6065
YEYING_WAREHOUSE_TOKEN=<Warehouse 登录 JWT>
```

这适合浏览器登录后的本地验证，不适合生产 Agent 长期保存。安装 Skill 不会自动获得用户身份，也不会自动创建 Token。

以下凭证有明确边界，不得混用：

| 凭证 | 允许的入口 | 是否可调用 HTTP Tool |
| --- | --- | --- |
| 用户登录 JWT | Warehouse 用户态 API | 当前可以 |
| WebDAV access key/secret | WebDAV | 不可以 |
| S3 access key/secret | S3 Signature V4 | 不可以 |
| Node 内部 AK/SK | Node 服务调用 | 不可以 |

## 2. Tool credential 目标

后续增加独立的短期 scoped Tool credential。它不是用户密码、钱包私钥、WebDAV 密钥或 S3 secret，至少包含：

```yaml
credentialId: wt_...
subjectType: user | service
subjectId: <user-or-service-id>
scopes:
  - asset:read
  - asset:write
pathPrefixes:
  - /personal/reviews/conversations/
expiresAt: 2026-10-01T00:00:00Z
status: active | revoked
```

密钥只在创建时显示一次，服务端只保存不可逆摘要；请求使用：

```http
Authorization: Bearer <短期 Tool credential>
```

Warehouse 仍是最终权限裁决者。凭证范围不能绕过用户权限、UCAN app scope、配额、对象条件写入或路径规范化检查。

## 3. 生命周期

1. 已认证用户或受控管理员创建 Tool credential，必须明确名称、路径前缀、动作和过期时间。
2. Warehouse 返回一次性 secret；调用方立即存入 Secret Manager 或本地受保护环境变量。
3. 每次 Tool 请求校验状态、过期时间、主体、动作和规范化后的路径。
4. 创建者或管理员可以撤销；撤销立即生效，不允许删除后继续使用。
5. 认证失败、越权、过期和撤销分别记录稳定错误码与审计事件，但不记录 secret 或请求正文。

## 4. 计划接口

第一阶段只开放用户自助管理接口，路径建议为：

```text
POST /api/v1/public/tools/credentials
GET  /api/v1/public/tools/credentials
POST /api/v1/public/tools/credentials/{id}/revoke
```

创建请求至少包括：

```json
{
  "name": "conversation-review",
  "scopes": ["asset:read", "asset:write"],
  "pathPrefixes": ["/personal/reviews/conversations/"],
  "expiresAt": "2026-10-01T00:00:00Z"
}
```

响应只返回一次 secret：

```json
{
  "id": "wt_123",
  "secret": "wts_...",
  "expiresAt": "2026-10-01T00:00:00Z"
}
```

## 5. 实施状态

已完成：

1. `warehouse_tool_credentials` 表、secret 哈希、状态、过期、撤销、路径前缀和 scope 校验。
2. 创建、列表、撤销接口。
3. 独立 Tool credential authenticator，仅对 `/api/v1/public/tools/*` 生效。
4. Tool handler 的 scope/path 二次校验。
5. Skill 客户端优先使用 `YEYING_WAREHOUSE_TOOL_TOKEN`。

已提供凭证轮换和审计查询：

```text
POST /api/v1/public/tools/credentials/{id}/rotate
GET  /api/v1/public/tools/audits?credentialId=<可选>
```

轮换会让旧 secret 立即失效，只返回一次新 secret；不传 `expiresAt` 时保留原过期时间。审计记录凭证生命周期和 Tool 调用的主体、工具、动作、路径、结果、requestId、traceId 和时间，不记录 secret、secret hash 或对象正文。

凭证管理和审计查询接口只接受用户登录 JWT，Tool credential 只能调用资产 catalog/call，不能管理其他凭证或读取审计。

后续补充管理界面和 Secret Manager 集成。
