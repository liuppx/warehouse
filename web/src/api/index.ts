// API 统一封装
import { authFetch, getAccessToken } from '@yeying-community/web3-bs'
import type { components as OpenAPIComponents } from './generated/openapi'

type OpenAPISchema<Name extends keyof OpenAPIComponents['schemas']> = OpenAPIComponents['schemas'][Name]

const API_BASE = import.meta.env.VITE_API_BASE || ''
const AUTH_BASE = API_BASE ? `${API_BASE.replace(/\/+$/, '')}/api/v1/public/auth` : '/api/v1/public/auth'

interface RequestOptions {
  method?: string
  body?: Record<string, unknown> | FormData
  headers?: Record<string, string>
}

async function request<T>(url: string, options: RequestOptions = {}): Promise<T> {
  const headers: Record<string, string> = {
    'accept': 'application/json',
    ...options.headers
  }

  if (!(options.body instanceof FormData)) {
    headers['Content-Type'] = 'application/json'
  }

  const response = await authFetch(`${API_BASE}${url}`, {
    method: options.method || 'GET',
    headers,
    body: options.body instanceof FormData ? options.body : (options.body ? JSON.stringify(options.body) : undefined)
  }, {
    baseUrl: AUTH_BASE,
    accessToken: getAccessToken()
  })

  if (!response.ok) {
    const error = await response.text()
    throw new Error(error || `HTTP ${response.status}`)
  }

  const text = await response.text()
  if (!text.trim()) {
    return undefined as T
  }
  return JSON.parse(text) as T
}

// 认证相关 API
export const authApi = {
  // 获取挑战
  getChallenge(address: string) {
    return request<{
      code: number
      message: string
      data: {
        address: string
        challenge: string
        nonce: string
        issuedAt: number
        expiresAt: number
      }
      timestamp: number
    }>(
      '/api/v1/public/auth/challenge',
      { method: 'POST', body: { address } }
    )
  },

  // 验证签名
  verifySignature(address: string, signature: string) {
    return request<{
      code: number
      message: string
      data: {
        address: string
        token: string
        expiresAt: number
        refreshExpiresAt: number
      }
      timestamp: number
    }>('/api/v1/public/auth/verify', {
      method: 'POST',
      body: { address, signature }
    })
  }
}

// 配额 API
export const quotaApi = {
  get() {
    return request<{
      quota: number
      used: number
      available: number
      percentage: number
      unlimited: boolean
    }>('/api/v1/public/webdav/quota')
  }
}

// 用户信息 API
export const userApi = {
  getInfo() {
    return request<{
      username: string
      wallet_address?: string
      email?: string
      permissions: string[]
      capabilities?: {
        manageUsers?: boolean
      }
      created_at?: string
      updated_at?: string
      has_password?: boolean
    }>('/api/v1/public/webdav/user/info')
  },

  updatePassword(oldPassword: string | null, newPassword: string) {
    return request('/api/v1/public/webdav/user/password', {
      method: 'POST',
      body: { oldPassword, newPassword }
    })
  }
}

export type NotificationItem = OpenAPISchema<'Notification'>

export interface NotificationPreferenceItem {
  Type?: string
  type?: string
  Enabled?: boolean
  enabled?: boolean
}

export interface AdminNotificationCreatePayload {
  recipientRole: 'all' | 'user' | 'admin'
  targetUsernames?: string[]
  title: string
  content: string
  severity: 'info' | 'warning' | 'error'
  actionUrl?: string
}

export const notificationApi = {
  list(limit = 20) {
    return request<{ items: NotificationItem[]; canAnnounce?: boolean }>(`/api/v1/public/notifications/list?limit=${limit}`)
  },
  unreadCount() {
    return request<{ count: number }>('/api/v1/public/notifications/unread-count')
  },
  markRead(ids: string[]) {
    return request('/api/v1/public/notifications/read', {
      method: 'POST',
      body: { ids }
    })
  },
  markAllRead() {
    return request('/api/v1/public/notifications/read-all', { method: 'POST' })
  },
  preferences() {
    return request<{ items: NotificationPreferenceItem[] }>('/api/v1/public/notifications/preferences')
  },
  setPreference(type: string, enabled: boolean) {
    return request('/api/v1/public/notifications/preferences', {
      method: 'POST',
      body: { type, enabled }
    })
  },
  streamUrl() {
    return `${API_BASE}/api/v1/public/notifications/stream`
  },
  adminCreate(payload: AdminNotificationCreatePayload) {
    return request<{ message: string; count: number }>('/api/v1/admin/notifications/create', {
      method: 'POST',
      body: payload as unknown as Record<string, unknown>
    })
  }
}

export interface AdminUserItem {
  id: string
  username: string
  wallet_address?: string
  email?: string
  directory: string
  permissions: string[]
  quota: number
  used_space: number
  quota_status: 'unlimited' | 'over_quota' | 'near_limit' | 'ok'
  quota_usage_percent?: number
  created_at?: string
  updated_at?: string
  has_password: boolean
}

export const adminUserApi = {
  list() {
    return request<{ items: AdminUserItem[] }>('/api/v1/admin/users/list')
  },
  updateQuota(username: string, quota: number) {
    return request<AdminUserItem>('/api/v1/admin/users/update', {
      method: 'POST',
      body: { username, quota }
    })
  }
}

export interface AssetSpaceInfo {
  key: string
  name: string
  path: string
}

export const assetsApi = {
  async getSpaces() {
    const response = await request<{
      code: number
      message: string
      data?: {
        defaultSpace?: string
        spaces?: AssetSpaceInfo[]
      }
      timestamp: number
    }>('/api/v1/public/assets/spaces')

    const data = response?.data || {}
    return {
      defaultSpace: data.defaultSpace || 'personal',
      spaces: Array.isArray(data.spaces) ? data.spaces : []
    }
  }
}

// 回收站项目类型
export type RecycleItem = OpenAPISchema<'RecycleItem'>

// 回收站 API
export const recycleApi = {
  // 获取回收站列表（全局）
  list(params?: { page?: number; pageSize?: number; search?: string }) {
    const query = new URLSearchParams()
    if (params?.page && params.page > 0) query.set('page', String(params.page))
    if (params?.pageSize && params.pageSize > 0) query.set('page_size', String(params.pageSize))
    if (params?.search?.trim()) query.set('search', params.search.trim())
    const suffix = query.toString() ? `?${query.toString()}` : ''
    return request<{
      items: RecycleItem[]
      total: number
      page: number
      pageSize: number
    }>(`/api/v1/public/webdav/recycle/list${suffix}`)
  },

  // 恢复文件到原始目录
  recover(hash: string) {
    return request('/api/v1/public/webdav/recycle/recover', {
      method: 'POST',
      body: { hash }
    })
  },

  // 永久删除
  remove(hash: string) {
    return request('/api/v1/public/webdav/recycle/permanent', {
      method: 'DELETE',
      body: { hash }
    })
  },

  clear() {
    return request<{ deleted: number }>('/api/v1/public/webdav/recycle/clear', {
      method: 'DELETE'
    })
  }
}

// 分享项目类型
export type ShareItem = OpenAPISchema<'PublicShare'>

export type ShareMode = 'download' | 'preview'

export interface ShareTargetGroup {
  id: string
  name: string
}

// 定向分享项目类型
export type DirectShareItem = OpenAPISchema<'DirectedShare'>

export interface ReceivedSharedResource {
  resourceId: string
  name: string
  path: string
  isDir: boolean
  permissions: string[]
  grantCount: number
  ownerName: string
  ownerWallet?: string
  createdAt: string
}

export type ShareExpiryUnit = 'minute' | 'hour' | 'day' | 'week' | 'month' | 'year'

export interface ManagedGroup {
  id: string
  name: string
  canManage?: boolean
  canInvite?: boolean
  createdAt?: string
}

export interface GroupMember {
  id: string
  name: string
  alias?: string
  walletAddress: string
  groupId: string
  status?: 'active' | 'pending' | string
  isOwner?: boolean
  isSelf?: boolean
  canManage?: boolean
  canRespond?: boolean
  createdAt?: string
}

export type AccessKeyPermission = 'read' | 'create' | 'update' | 'delete'

export type WebDAVAccessKeyItem = OpenAPISchema<'WebDAVAccessKey'>

export interface CreateWebDAVAccessKeyPayload {
  name: string
  rootPath: string
  permissions?: AccessKeyPermission[]
  expiresValue?: number
  expiresUnit?: ShareExpiryUnit
}

export interface CreateWebDAVAccessKeyResult extends WebDAVAccessKeyItem {
  keySecret: string
}

export type S3CredentialItem = OpenAPISchema<'S3Credential'>

export interface CreateS3CredentialResult extends S3CredentialItem {
  secret: string
  warning?: string
}

export interface CreateS3CredentialPayload {
  name: string
  rootPath: string
  permissions: string[]
}

// 分享 API
export const shareApi = {
  create(path: string, expiry?: {
    expiresIn?: number
    expiresValue?: number
    expiresUnit?: ShareExpiryUnit
    mode?: ShareMode
  }) {
    return request<{
      token: string
      name: string
      path: string
      mode: ShareMode
      url: string
      viewCount: number
      downloadCount: number
      expiresAt?: string
    }>('/api/v1/public/share/create', {
      method: 'POST',
      body: { path, ...expiry }
    })
  },

  createFromReceivedResource(payload: {
    resourceId: string
    relativePath: string
    mode?: ShareMode
    expiresValue?: number
    expiresUnit?: ShareExpiryUnit
  }) {
    return request<{
      token: string
      name: string
      path: string
      mode: ShareMode
      url: string
      viewCount: number
      downloadCount: number
      expiresAt?: string
    }>('/api/v1/public/share/create-from-resource', { method: 'POST', body: payload })
  },

  list() {
    return request<{
      items: ShareItem[]
    }>('/api/v1/public/share/list')
  },

  revoke(token: string) {
    return request('/api/v1/public/share/revoke', {
      method: 'POST',
      body: { token }
    })
  }
}

// 定向分享 API
export const directShareApi = {
  create(payload: {
    path: string
    targetAddresses?: string[]
    targetMode: 'addresses' | 'groups' | 'all_users'
    groupIds?: string[]
    permissions: string[]
    expiresIn?: number
    expiresValue?: number
    expiresUnit?: ShareExpiryUnit
  }) {
    return request<DirectShareItem>('/api/v1/public/share/user/create', {
      method: 'POST',
      body: payload as unknown as Record<string, unknown>
    })
  },

  listMine() {
    return request<{
      items: DirectShareItem[]
    }>('/api/v1/public/share/user/list')
  },

  listReceived() {
    return request<{
      items: ReceivedSharedResource[]
    }>('/api/v1/public/share/user/received')
  },

  listReceivedResourceEntries(resourceId: string, path = '') {
    const query = new URLSearchParams({ resourceId, path })
    return request<{ items: Array<{ name: string; path: string; isDir: boolean; size: number; modified: string }> }>(`/api/v1/public/share/resource/entries?${query}`)
  },

  revoke(id: string) {
    return request('/api/v1/public/share/user/revoke', {
      method: 'POST',
      body: { id }
    })
  },

  listAudiences(shareId: string) {
    const query = new URLSearchParams({ shareId })
    return request<{
      items: Array<{
        type: string
        targetUserId?: string
        targetWallet?: string
        sourceGroupId?: string
        sourceGroupName?: string
      }>
    }>(`/api/v1/public/share/user/audiences?${query.toString()}`)
  }
}

export const webdavAccessKeyApi = {
  list() {
    return request<{
      items: WebDAVAccessKeyItem[]
    }>('/api/v1/public/webdav/access-keys/list')
  },
  create(payload: CreateWebDAVAccessKeyPayload) {
    return request<CreateWebDAVAccessKeyResult>('/api/v1/public/webdav/access-keys/create', {
      method: 'POST',
      body: payload as unknown as Record<string, unknown>
    })
  },
  revoke(id: string) {
    return request<{ message: string }>('/api/v1/public/webdav/access-keys/revoke', {
      method: 'POST',
      body: { id }
    })
  },
  remove(id: string) {
    return request<{ message: string }>('/api/v1/public/webdav/access-keys/delete', {
      method: 'POST',
      body: { id }
    })
  },
  bind(id: string, path: string) {
    return request<{ message: string }>('/api/v1/public/webdav/access-keys/bind', {
      method: 'POST',
      body: { id, path }
    })
  }
}

export const s3CredentialApi = {
  list() {
    return request<{ items: S3CredentialItem[] }>('/api/v1/public/s3/credentials/list')
  },
  create(payload: CreateS3CredentialPayload) {
    return request<CreateS3CredentialResult>('/api/v1/public/s3/credentials/create', {
      method: 'POST',
      body: payload as unknown as Record<string, unknown>
    })
  },
  revoke(id: string) {
    return request<{ message?: string }>('/api/v1/public/s3/credentials/revoke', {
      method: 'POST',
      body: { id }
    })
  },
  remove(id: string) {
    return request<{ message?: string }>('/api/v1/public/s3/credentials/delete', {
      method: 'POST',
      body: { id }
    })
  }
}

export interface WarehouseToolCredentialItem {
  id: string
  name: string
  scopes: string[]
  pathPrefixes: string[]
  status: 'active' | 'revoked'
  expiresAt: string
  createdAt: string
  lastUsedAt?: string | null
}

export interface WarehouseToolAuditItem {
  id: string
  credentialId: string
  toolName?: string
  action: string
  path?: string
  outcome: string
  requestId?: string
  traceId?: string
  createdAt: string
}

export const warehouseToolCredentialApi = {
  list() {
    return request<{ items: WarehouseToolCredentialItem[] }>('/api/v1/public/tools/credentials')
  },
  create(payload: { name: string; scopes: string[]; pathPrefixes: string[]; expiresAt: string }) {
    return request<WarehouseToolCredentialItem & { secret: string; warning: string }>('/api/v1/public/tools/credentials', {
      method: 'POST', body: payload
    })
  },
  rotate(id: string, expiresAt?: string) {
    return request<{ id: string; secret: string; expiresAt: string; warning: string }>(`/api/v1/public/tools/credentials/${encodeURIComponent(id)}/rotate`, {
      method: 'POST', body: expiresAt ? { expiresAt } : {}
    })
  },
  revoke(id: string) {
    return request<void>(`/api/v1/public/tools/credentials/${encodeURIComponent(id)}/revoke`, { method: 'POST' })
  },
  audits(credentialId?: string) {
    const query = credentialId ? `?credentialId=${encodeURIComponent(credentialId)}` : ''
    return request<{ items: WarehouseToolAuditItem[] }>(`/api/v1/public/tools/audits${query}`)
  }
}

export const groupApi = {
  listGroups() {
    return request<{ items: ManagedGroup[] }>('/api/v1/public/webdav/group/groups')
  },
  createGroup(name: string) {
    return request<ManagedGroup>('/api/v1/public/webdav/group/groups/create', {
      method: 'POST',
      body: { name }
    })
  },
  updateGroup(id: string, name: string) {
    return request('/api/v1/public/webdav/group/groups/update', {
      method: 'PUT',
      body: { id, name }
    })
  },
  deleteGroup(id: string) {
    return request('/api/v1/public/webdav/group/groups/delete', {
      method: 'DELETE',
      body: { id }
    })
  },
  listMembers() {
    return request<{ items: GroupMember[] }>('/api/v1/public/webdav/group/members')
  },
  createMember(payload: { target: string; groupId: string; alias?: string }) {
    const target = payload.target.trim()
    const body: Record<string, unknown> = { ...payload, target }
    if (/^0x[0-9a-fA-F]{40}$/.test(target)) {
      body.name = target
      body.walletAddress = target
    }
    return request<GroupMember>('/api/v1/public/webdav/group/members/create', {
      method: 'POST',
      body
    })
  },
  updateMember(payload: { id: string; name?: string; walletAddress?: string; groupId?: string }) {
    return request<GroupMember>('/api/v1/public/webdav/group/members/update', {
      method: 'PUT',
      body: payload
    })
  },
  updateMemberName(payload: { id: string; name: string }) {
    return request<GroupMember>('/api/v1/public/webdav/group/members/name', {
      method: 'PUT',
      body: payload
    })
  },
  updateMemberAlias(payload: { id: string; alias: string }) {
    return request<GroupMember>('/api/v1/public/webdav/group/members/alias', {
      method: 'PUT',
      body: payload
    })
  },
  approveMember(id: string, name = "") {
    return request('/api/v1/public/webdav/group/members/approve', {
      method: 'POST',
      body: { id, name }
    })
  },
  rejectMember(id: string) {
    return request('/api/v1/public/webdav/group/members/reject', {
      method: 'POST',
      body: { id }
    })
  },
  deleteMember(id: string) {
    return request('/api/v1/public/webdav/group/members/delete', {
      method: 'DELETE',
      body: { id }
    })
  }
}
