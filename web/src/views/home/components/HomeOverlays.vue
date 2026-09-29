<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import type { ManagedGroup, DirectShareItem, ReceivedSharedResource, GroupMember, RecycleItem, ShareExpiryUnit, ShareItem, ShareMode } from '@/api'
import type { CipherSuiteOption, FileItem } from '../types'
import type { EncryptedDirectoryPasswordSource } from '@/utils/encryptedDirectory'
import { shortenAddress } from '@/utils/address'

const props = defineProps<{
  detailDrawerVisible: boolean
  detailTitle: string
  detailMode: 'file' | 'recycle' | 'share' | 'directShare' | 'receivedShare' | 'sharedEntry' | null
  detailFile: FileItem | null
  detailRecycle: RecycleItem | null
  detailShare: ShareItem | null
  detailDirectShare: DirectShareItem | null
  detailReceivedShare: ReceivedSharedResource | null
  detailSharedEntry: FileItem | null
  sharedCanRead: boolean
  sharedCanUpdate: boolean
  getPreviewMode: (item: FileItem) => 'text' | 'pdf' | 'word' | 'image' | 'audio' | 'video' | null
  openFilePreview: (item: FileItem) => void
  formatTime: (time: string | number) => string
  formatDeletedTime: (time: string) => string
  formatSizeDetail: (size: number) => string
  formatSharePermission: (permission: string) => string
  formatShareMode: (mode?: ShareMode | string) => string
  getShareLink: (item: ShareItem) => string
  copyShareLink: (item: ShareItem) => void
  revokeShare: (item: ShareItem) => void
  revokeDirectShare: (item: DirectShareItem) => void
  isDirectShareOwner: (item: DirectShareItem) => boolean
  enterDirectory: (item: FileItem) => void
  openAccessKeyDialog: (item: FileItem) => void
  getEncryptedDirectoryRoot: (item: FileItem) => string | null
  isEncryptedDirectoryPasswordCached: (rootPath: string | null) => boolean
  requiresEncryptedDirectoryPassword: (rootPath: string | null) => boolean
  getEncryptedDirectoryProtectionLabel: (rootPath: string | null) => string
  unlockEncryptedDirectory: (rootPath: string, forceReset?: boolean) => void | Promise<void>
  clearEncryptedDirectoryPasswordCache: (rootPath: string) => void
  enterSharedRoot: (item: ReceivedSharedResource) => void
  enterDirectShareRoot: (item: DirectShareItem) => void
  enterSharedDirectory: (item: FileItem) => void
  downloadSharedRoot: (item: ReceivedSharedResource) => void
  downloadDirectShareRoot: (item: DirectShareItem) => void
  downloadSharedFile: (item: FileItem) => void
  shareLinkDialogVisible: boolean
  shareLinkSubmitting: boolean
  shareLinkTarget: FileItem | null
  shareLinkForm: {
    expiresValue: string
    expiresUnit: ShareExpiryUnit
    mode: ShareMode
  }
  shareExpiryUnits: Array<{
    label: string
    value: ShareExpiryUnit
  }>
  shareModeOptions: Array<{
    label: string
    value: ShareMode
    description: string
    hint: string
  }>
  submitShareLink: () => void
  shareUserDialogVisible: boolean
  shareUserSubmitting: boolean
  shareUserTarget: FileItem | null
  shareUserForm: {
    targetMode: 'addresses' | 'groups' | 'all_users'
    targetAddresses: string[]
    groupIds: string[]
    permissions: string[]
    expiresValue: string
    expiresUnit: ShareExpiryUnit
  }
  groupMembers: GroupMember[]
  managedGroups: ManagedGroup[]
  selectedGroupMembers: GroupMember[]
  submitShareUser: () => void
  createFolderDialogVisible: boolean
  createFolderSubmitting: boolean
  createFolderForm: {
    name: string
    encrypted: boolean
    cipherSuite: string
    passwordSource: EncryptedDirectoryPasswordSource
    password: string
    confirmPassword: string
  }
  cipherSuiteOptions: CipherSuiteOption[]
  submitCreateFolder: () => void
  renameDialogVisible: boolean
  renameSubmitting: boolean
  renameTarget: FileItem | null
  renameForm: {
    name: string
  }
  submitRename: () => void
  passwordDialogVisible: boolean
  passwordSubmitting: boolean
  passwordForm: {
    oldPassword: string
    newPassword: string
    confirmPassword: string
  }
  userProfile: {
    hasPassword: boolean
  }
  submitPassword: () => void
}>()

const emit = defineEmits<{
  (event: 'update:detailDrawerVisible', value: boolean): void
  (event: 'update:shareLinkDialogVisible', value: boolean): void
  (event: 'update:shareUserDialogVisible', value: boolean): void
  (event: 'update:createFolderDialogVisible', value: boolean): void
  (event: 'update:renameDialogVisible', value: boolean): void
  (event: 'update:passwordDialogVisible', value: boolean): void
}>()

const detailDrawerModel = computed({
  get: () => props.detailDrawerVisible,
  set: value => emit('update:detailDrawerVisible', value)
})

const shareLinkDialogModel = computed({
  get: () => props.shareLinkDialogVisible,
  set: value => emit('update:shareLinkDialogVisible', value)
})

const shareUserDialogModel = computed({
  get: () => props.shareUserDialogVisible,
  set: value => emit('update:shareUserDialogVisible', value)
})

const createFolderDialogModel = computed({
  get: () => props.createFolderDialogVisible,
  set: value => emit('update:createFolderDialogVisible', value)
})

const renameDialogModel = computed({
  get: () => props.renameDialogVisible,
  set: value => emit('update:renameDialogVisible', value)
})

const passwordDialogModel = computed({
  get: () => props.passwordDialogVisible,
  set: value => emit('update:passwordDialogVisible', value)
})

const viewportWidth = ref(1280)
const drawerDragActive = ref(false)
const drawerWidthManual = ref<number | null>(null)
let drawerDragStartX = 0
let drawerDragStartWidth = 0

function updateViewportWidth() {
  if (typeof window === 'undefined') return
  viewportWidth.value = window.innerWidth
}

const canResizeDrawer = computed(() => viewportWidth.value > 768)

const detailDrawerBaseWidth = computed(() => {
  const width = viewportWidth.value
  if (width <= 480) return Math.max(280, width - 20)
  if (width <= 768) return Math.max(320, Math.floor(width * 0.9))

  const modeWidthMap: Record<string, number> = {
    recycle: 520,
    share: 500,
    directShare: 540,
    receivedShare: 520,
    file: 420,
    sharedEntry: 420
  }
  const mode = String(props.detailMode || '')
  const ideal = modeWidthMap[mode] || 420
  const max = Math.floor(width * 0.62)
  const min = 360
  const bounded = Math.min(Math.max(ideal, min), max)
  return Math.max(bounded, min)
})

const detailDrawerMinWidth = computed(() => (viewportWidth.value <= 480 ? 280 : viewportWidth.value <= 768 ? 320 : 360))
const detailDrawerMaxWidth = computed(() => {
  const ratio = viewportWidth.value <= 768 ? 0.95 : 0.8
  return Math.max(detailDrawerMinWidth.value, Math.floor(viewportWidth.value * ratio))
})

function clampDrawerWidth(width: number): number {
  return Math.min(Math.max(width, detailDrawerMinWidth.value), detailDrawerMaxWidth.value)
}

const detailDrawerWidth = computed(() => {
  const target = drawerWidthManual.value ?? detailDrawerBaseWidth.value
  return clampDrawerWidth(target)
})

const detailDrawerSize = computed(() => `${detailDrawerWidth.value}px`)

function stopDrawerResize() {
  if (typeof document === 'undefined' || !drawerDragActive.value) return
  drawerDragActive.value = false
  document.removeEventListener('mousemove', handleDrawerResizeMove)
  document.removeEventListener('mouseup', stopDrawerResize)
  document.body.style.removeProperty('cursor')
  document.body.style.removeProperty('user-select')
}

function handleDrawerResizeMove(event: MouseEvent) {
  if (!drawerDragActive.value) return
  const delta = drawerDragStartX - event.clientX
  drawerWidthManual.value = clampDrawerWidth(drawerDragStartWidth + delta)
}

function startDrawerResize(event: MouseEvent) {
  if (!canResizeDrawer.value || typeof document === 'undefined') return
  drawerDragActive.value = true
  drawerDragStartX = event.clientX
  drawerDragStartWidth = detailDrawerWidth.value
  document.addEventListener('mousemove', handleDrawerResizeMove)
  document.addEventListener('mouseup', stopDrawerResize)
  document.body.style.cursor = 'col-resize'
  document.body.style.userSelect = 'none'
}

function handleEnterDirectory(item: FileItem) {
  props.enterDirectory(item)
  emit('update:detailDrawerVisible', false)
}

function handleOpenAccessKeyDialog(item: FileItem) {
  props.openAccessKeyDialog(item)
  emit('update:detailDrawerVisible', false)
}

function getDetailEncryptedRoot(item: FileItem | null): string | null {
  if (!item) return null
  return props.getEncryptedDirectoryRoot(item)
}

function isDetailEncryptedPasswordCached(item: FileItem | null): boolean {
  return props.isEncryptedDirectoryPasswordCached(getDetailEncryptedRoot(item))
}

function detailRequiresEncryptedPassword(item: FileItem | null): boolean {
  return props.requiresEncryptedDirectoryPassword(getDetailEncryptedRoot(item))
}

function detailEncryptedProtectionLabel(item: FileItem | null): string {
  return props.getEncryptedDirectoryProtectionLabel(getDetailEncryptedRoot(item))
}

async function handleUnlockEncryptedDirectory(item: FileItem | null, forceReset = false) {
  const root = getDetailEncryptedRoot(item)
  if (!root) return
  await props.unlockEncryptedDirectory(root, forceReset)
}

function handleClearEncryptedDirectoryPasswordCache(item: FileItem | null) {
  const root = getDetailEncryptedRoot(item)
  if (!root) return
  props.clearEncryptedDirectoryPasswordCache(root)
}

function formatTargetScope(item: DirectShareItem | null): string {
  if (!item) return '-'
  if (item.allUsers || item.targetType === 'all_users') return '所有用户'
  if (item.targetType === 'groups') {
    const count = item.targetGroups?.length || item.targetCount || item.audienceCount || 0
    return count > 0 ? `动态分组（${count} 个）` : '动态分组'
  }
  const count = item.targetCount || item.audienceCount || 0
  if (count > 1) return `地址共享（${count} 个）`
  return '地址共享'
}

function formatTargetGroups(item: DirectShareItem | null): string {
  const groups = item?.targetGroups || []
  if (!groups.length) return '-'
  return groups.map(group => group.name || group.id).filter(Boolean).join('、') || '-'
}

function formatTargetAudienceLabel(item: DirectShareItem | null): string {
  if (!item) return '目标地址'
  if (item.allUsers || item.targetType === 'all_users') return '目标对象'
  if (item.targetType === 'groups') return '目标分组'
  return '目标地址'
}

function formatTargetAudience(item: DirectShareItem | null): string {
  if (!item) return '-'
  if (item.allUsers || item.targetType === 'all_users') return '所有用户'
  if (item.targetType === 'groups') return formatTargetGroups(item)
  return item.targetWallet || '-'
}

function isTargetAudienceMono(item: DirectShareItem | null): boolean {
  return !!item && item.targetType !== 'groups' && item.targetType !== 'all_users' && !item.allUsers
}

onMounted(() => {
  updateViewportWidth()
  if (typeof window !== 'undefined') {
    window.addEventListener('resize', updateViewportWidth)
  }
})

onBeforeUnmount(() => {
  stopDrawerResize()
  if (typeof window !== 'undefined') {
    window.removeEventListener('resize', updateViewportWidth)
  }
})
</script>

<template>
  <el-drawer
    v-model="detailDrawerModel"
    :title="detailTitle"
    direction="rtl"
    :size="detailDrawerSize"
    class="detail-drawer"
  >
    <div
      v-if="canResizeDrawer"
      class="drawer-resize-handle"
      :class="{ 'is-active': drawerDragActive }"
      @mousedown.prevent="startDrawerResize"
    />
    <div class="detail-panel" v-if="detailMode === 'file' && detailFile">
      <div class="detail-grid">
        <div class="detail-row">
          <span class="detail-label">名称</span>
          <span class="detail-value">{{ detailFile.name }}</span>
        </div>
        <div class="detail-row">
          <span class="detail-label">类型</span>
          <span class="detail-value">{{ detailFile.isDir ? '文件夹' : '文件' }}</span>
        </div>
        <div class="detail-row">
          <span class="detail-label">路径</span>
          <span class="detail-value mono">{{ detailFile.path }}</span>
        </div>
        <div class="detail-row">
          <span class="detail-label">大小</span>
          <span class="detail-value">{{ detailFile.isDir ? '-' : formatSizeDetail(detailFile.size) }}</span>
        </div>
        <div class="detail-row">
          <span class="detail-label">加密</span>
          <span class="detail-value">{{ detailFile.encrypted ? '已启用' : '未启用' }}</span>
        </div>
        <div v-if="detailFile.encrypted && getDetailEncryptedRoot(detailFile)" class="detail-row">
          <span class="detail-label">加密根</span>
          <span class="detail-value mono">{{ getDetailEncryptedRoot(detailFile) }}</span>
        </div>
        <div v-if="detailFile.encrypted && getDetailEncryptedRoot(detailFile)" class="detail-row">
          <span class="detail-label">保护方式</span>
          <span class="detail-value">{{ detailEncryptedProtectionLabel(detailFile) }}</span>
        </div>
        <div v-if="detailFile.encrypted && getDetailEncryptedRoot(detailFile) && detailRequiresEncryptedPassword(detailFile)" class="detail-row">
          <span class="detail-label">密码状态</span>
          <span class="detail-value">{{ isDetailEncryptedPasswordCached(detailFile) ? '已缓存' : '未缓存' }}</span>
        </div>
        <div v-if="detailFile.encrypted && getDetailEncryptedRoot(detailFile) && detailRequiresEncryptedPassword(detailFile)" class="detail-note">
          额外密码仅缓存在当前浏览器会话中，重新输入不会重加密已有文件。
        </div>
        <div class="detail-row">
          <span class="detail-label">修改时间</span>
          <span class="detail-value time-cell">{{ formatTime(detailFile.modified) }}</span>
        </div>
      </div>
      <div class="detail-actions" v-if="detailFile.isDir">
        <el-button type="primary" size="small" @click="handleEnterDirectory(detailFile)">
          进入目录
        </el-button>
        <el-button size="small" @click="handleOpenAccessKeyDialog(detailFile)">
          授权密钥
        </el-button>
        <template v-if="detailFile.encrypted && getDetailEncryptedRoot(detailFile) && detailRequiresEncryptedPassword(detailFile)">
          <el-button size="small" @click="handleUnlockEncryptedDirectory(detailFile, isDetailEncryptedPasswordCached(detailFile))">
            {{ isDetailEncryptedPasswordCached(detailFile) ? '重新输入密码' : '解锁目录' }}
          </el-button>
          <el-button
            v-if="isDetailEncryptedPasswordCached(detailFile)"
            size="small"
            @click="handleClearEncryptedDirectoryPasswordCache(detailFile)"
          >
            清除密码缓存
          </el-button>
        </template>
      </div>
      <div class="detail-actions" v-else-if="getPreviewMode(detailFile) === 'text'">
        <el-button type="primary" size="small" @click="openFilePreview(detailFile)">
          打开编辑
        </el-button>
      </div>
      <div class="detail-actions" v-else-if="getPreviewMode(detailFile)">
        <el-button type="primary" size="small" @click="openFilePreview(detailFile)">
          预览
        </el-button>
      </div>
      <div class="detail-actions" v-else>
        <el-button type="primary" size="small" @click="openFilePreview(detailFile)">
          选择格式打开
        </el-button>
      </div>
    </div>

    <div class="detail-panel" v-else-if="detailMode === 'recycle' && detailRecycle">
      <div class="detail-grid">
        <div class="detail-row">
          <span class="detail-label">名称</span>
          <span class="detail-value">{{ detailRecycle.name }}</span>
        </div>
        <div class="detail-row">
          <span class="detail-label">原始路径</span>
          <span class="detail-value mono">{{ detailRecycle.path }}</span>
        </div>
        <div class="detail-row">
          <span class="detail-label">所在目录</span>
          <span class="detail-value">{{ detailRecycle.directory }}</span>
        </div>
        <div class="detail-row">
          <span class="detail-label">大小</span>
          <span class="detail-value">{{ formatSizeDetail(detailRecycle.size) }}</span>
        </div>
        <div class="detail-row">
          <span class="detail-label">删除时间</span>
          <span class="detail-value time-cell">{{ formatDeletedTime(detailRecycle.deletedAt) }}</span>
        </div>
        <div class="detail-row">
          <span class="detail-label">标识</span>
          <span class="detail-value mono">{{ detailRecycle.hash }}</span>
        </div>
      </div>
    </div>

    <div class="detail-panel" v-else-if="detailMode === 'share' && detailShare">
      <div class="detail-grid">
        <div class="detail-row">
          <span class="detail-label">名称</span>
          <span class="detail-value">{{ detailShare.name }}</span>
        </div>
        <div class="detail-row">
          <span class="detail-label">路径</span>
          <span class="detail-value mono">{{ detailShare.path }}</span>
        </div>
        <div class="detail-row">
          <span class="detail-label">分享链接</span>
          <span class="detail-value mono">{{ getShareLink(detailShare) }}</span>
        </div>
        <div class="detail-row">
          <span class="detail-label">打开方式</span>
          <span class="detail-value">{{ formatShareMode(detailShare.mode) }}</span>
        </div>
        <div class="detail-row">
          <span class="detail-label">访问/下载</span>
          <span class="detail-value">{{ detailShare.viewCount ?? 0 }}/{{ detailShare.downloadCount ?? 0 }}</span>
        </div>
        <div class="detail-row">
          <span class="detail-label">过期时间</span>
          <span class="detail-value time-cell">{{ detailShare.expiresAt ? formatTime(detailShare.expiresAt) : '永不过期' }}</span>
        </div>
        <div class="detail-row">
          <span class="detail-label">创建时间</span>
          <span class="detail-value time-cell">{{ detailShare.createdAt ? formatTime(detailShare.createdAt) : '-' }}</span>
        </div>
      </div>
      <div class="detail-actions">
        <el-button type="primary" size="small" @click="copyShareLink(detailShare)">
          复制链接
        </el-button>
        <el-button type="danger" size="small" @click="revokeShare(detailShare)">
          取消分享
        </el-button>
      </div>
    </div>

    <div class="detail-panel" v-else-if="detailMode === 'directShare' && detailDirectShare">
      <div class="detail-grid">
        <div class="detail-row">
          <span class="detail-label">名称</span>
          <span class="detail-value">{{ detailDirectShare.name }}</span>
        </div>
        <div class="detail-row">
          <span class="detail-label">类型</span>
          <span class="detail-value">{{ detailDirectShare.isDir ? '目录' : '文件' }}</span>
        </div>
        <div class="detail-row">
          <span class="detail-label">路径</span>
          <span class="detail-value mono">{{ detailDirectShare.path }}</span>
        </div>
        <div class="detail-row">
          <span class="detail-label">所有者</span>
          <span class="detail-value">{{ detailDirectShare.ownerName || (isDirectShareOwner(detailDirectShare) ? '我' : '-') }}</span>
        </div>
        <div class="detail-row">
          <span class="detail-label">所有者地址</span>
          <span class="detail-value mono">{{ detailDirectShare.ownerWallet || '-' }}</span>
        </div>
        <div class="detail-row">
          <span class="detail-label">{{ formatTargetAudienceLabel(detailDirectShare) }}</span>
          <span class="detail-value" :class="{ mono: isTargetAudienceMono(detailDirectShare) }">{{ formatTargetAudience(detailDirectShare) }}</span>
        </div>
        <div class="detail-row">
          <span class="detail-label">目标范围</span>
          <span class="detail-value">{{ formatTargetScope(detailDirectShare) }}</span>
        </div>
        <div class="detail-row">
          <span class="detail-label">权限</span>
          <span class="detail-value">
            <span v-if="!detailDirectShare.permissions || !detailDirectShare.permissions.length">-</span>
            <span v-else class="user-tags">
              <el-tag v-for="permission in detailDirectShare.permissions" :key="permission" size="small" type="info">
                {{ formatSharePermission(permission) }}
              </el-tag>
            </span>
          </span>
        </div>
        <div class="detail-row">
          <span class="detail-label">过期时间</span>
          <span class="detail-value time-cell">{{ detailDirectShare.expiresAt ? formatTime(detailDirectShare.expiresAt) : '永不过期' }}</span>
        </div>
        <div class="detail-row">
          <span class="detail-label">创建时间</span>
          <span class="detail-value time-cell">{{ detailDirectShare.createdAt ? formatTime(detailDirectShare.createdAt) : '-' }}</span>
        </div>
      </div>
      <div class="detail-actions">
        <el-button
          v-if="detailDirectShare.isDir"
          type="primary"
          size="small"
          @click="enterDirectShareRoot(detailDirectShare)"
        >
          进入目录
        </el-button>
        <el-button
          v-else-if="detailDirectShare.permissions && detailDirectShare.permissions.includes('read')"
          type="primary"
          size="small"
          @click="downloadDirectShareRoot(detailDirectShare)"
        >
          下载
        </el-button>
        <el-button
          v-if="isDirectShareOwner(detailDirectShare)"
          type="danger"
          size="small"
          @click="revokeDirectShare(detailDirectShare)"
        >
          取消分享
        </el-button>
      </div>
    </div>

    <div class="detail-panel" v-else-if="detailMode === 'receivedShare' && detailReceivedShare">
      <div class="detail-grid">
        <div class="detail-row">
          <span class="detail-label">名称</span>
          <span class="detail-value">{{ detailReceivedShare.name }}</span>
        </div>
        <div class="detail-row">
          <span class="detail-label">类型</span>
          <span class="detail-value">{{ detailReceivedShare.isDir ? '目录' : '文件' }}</span>
        </div>
        <div class="detail-row">
          <span class="detail-label">路径</span>
          <span class="detail-value mono">{{ detailReceivedShare.path }}</span>
        </div>
        <div class="detail-row">
          <span class="detail-label">分享人</span>
          <span class="detail-value">{{ detailReceivedShare.ownerName || '-' }}</span>
        </div>
        <div class="detail-row">
          <span class="detail-label">源钱包</span>
          <span class="detail-value mono">{{ detailReceivedShare.ownerWallet || '-' }}</span>
        </div>
        <div class="detail-row">
          <span class="detail-label">有效授权</span>
          <span class="detail-value">{{ detailReceivedShare.grantCount }} 条</span>
        </div>
        <div class="detail-row">
          <span class="detail-label">权限</span>
          <span class="detail-value">
            <span v-if="!detailReceivedShare.permissions || !detailReceivedShare.permissions.length">-</span>
            <span v-else class="user-tags">
              <el-tag v-for="permission in detailReceivedShare.permissions" :key="permission" size="small" type="info">
                {{ formatSharePermission(permission) }}
              </el-tag>
            </span>
          </span>
        </div>
        <div class="detail-row">
          <span class="detail-label">创建时间</span>
          <span class="detail-value time-cell">{{ detailReceivedShare.createdAt ? formatTime(detailReceivedShare.createdAt) : '-' }}</span>
        </div>
      </div>
      <div class="detail-actions">
        <el-button v-if="detailReceivedShare.isDir" type="primary" size="small" @click="enterSharedRoot(detailReceivedShare)">
          进入目录
        </el-button>
        <el-button
          v-else-if="detailReceivedShare.permissions && detailReceivedShare.permissions.includes('read')"
          type="primary"
          size="small"
          @click="downloadSharedRoot(detailReceivedShare)"
        >
          下载
        </el-button>
      </div>
    </div>

    <div class="detail-panel" v-else-if="detailMode === 'sharedEntry' && detailSharedEntry">
      <div class="detail-grid">
        <div class="detail-row">
          <span class="detail-label">名称</span>
          <span class="detail-value">{{ detailSharedEntry.name }}</span>
        </div>
        <div class="detail-row">
          <span class="detail-label">类型</span>
          <span class="detail-value">{{ detailSharedEntry.isDir ? '目录' : '文件' }}</span>
        </div>
        <div class="detail-row">
          <span class="detail-label">路径</span>
          <span class="detail-value mono">{{ detailSharedEntry.path }}</span>
        </div>
        <div class="detail-row">
          <span class="detail-label">大小</span>
          <span class="detail-value">{{ detailSharedEntry.isDir ? '-' : formatSizeDetail(detailSharedEntry.size) }}</span>
        </div>
        <div class="detail-row">
          <span class="detail-label">修改时间</span>
          <span class="detail-value time-cell">{{ formatTime(detailSharedEntry.modified) }}</span>
        </div>
      </div>
      <div class="detail-actions">
        <template v-if="detailSharedEntry.isDir">
          <el-button type="primary" size="small" @click="enterSharedDirectory(detailSharedEntry)">
            进入目录
          </el-button>
        </template>
        <template v-else>
          <el-button v-if="sharedCanRead" type="primary" size="small" @click="downloadSharedFile(detailSharedEntry)">
            下载
          </el-button>
          <el-button
            v-if="sharedCanRead && getPreviewMode(detailSharedEntry) === 'text'"
            type="primary"
            size="small"
            @click="openFilePreview(detailSharedEntry)"
          >
            {{ sharedCanUpdate ? '打开编辑' : '预览' }}
          </el-button>
          <el-button
            v-else-if="sharedCanRead && getPreviewMode(detailSharedEntry)"
            type="primary"
            size="small"
            @click="openFilePreview(detailSharedEntry)"
          >
            预览
          </el-button>
          <el-button
            v-else-if="sharedCanRead"
            type="primary"
            size="small"
            @click="openFilePreview(detailSharedEntry)"
          >
            选择格式打开
          </el-button>
        </template>
      </div>
    </div>

    <div v-else class="detail-empty">暂无详情</div>
  </el-drawer>

  <el-dialog
    v-model="shareLinkDialogModel"
    title="创建分享链接"
    width="420px"
  >
    <el-form label-width="72px" label-position="left" class="share-user-form">
      <el-form-item label="分享对象">
        <span class="share-user-value">{{ shareLinkTarget?.name || '-' }}</span>
      </el-form-item>
      <el-form-item label="有效期">
        <div class="share-expiry-field">
          <el-input v-model="shareLinkForm.expiresValue" placeholder="0" />
          <el-select v-model="shareLinkForm.expiresUnit" class="share-expiry-unit">
            <el-option
              v-for="item in shareExpiryUnits"
              :key="item.value"
              :label="item.label"
              :value="item.value"
            />
          </el-select>
        </div>
        <div class="share-group-meta">输入 0 表示永不过期</div>
      </el-form-item>
      <el-form-item label="打开方式">
        <div class="share-mode-field">
          <el-radio-group v-model="shareLinkForm.mode" size="small">
            <el-radio-button
              v-for="item in shareModeOptions"
              :key="item.value"
              :value="item.value"
            >
              {{ item.label }}
            </el-radio-button>
          </el-radio-group>
          <div class="share-mode-note">
            <div class="share-group-meta">
              {{ shareModeOptions.find(item => item.value === shareLinkForm.mode)?.description || '' }}
            </div>
            <div class="share-group-meta share-mode-hint">
              {{ shareModeOptions.find(item => item.value === shareLinkForm.mode)?.hint || '' }}
            </div>
          </div>
        </div>
      </el-form-item>
    </el-form>
    <template #footer>
      <el-button @click="shareLinkDialogModel = false">取消</el-button>
      <el-button type="primary" :loading="shareLinkSubmitting" @click="submitShareLink">创建并复制</el-button>
    </template>
  </el-dialog>

  <el-dialog
    v-model="shareUserDialogModel"
    title="共享"
    width="420px"
  >
    <el-form label-width="72px" label-position="left" class="share-user-form">
      <el-form-item label="共享对象">
        <span class="share-user-value">{{ shareUserTarget?.name || '-' }}</span>
      </el-form-item>
      <el-form-item label="共享方式">
        <el-radio-group v-model="shareUserForm.targetMode" size="small">
          <el-radio-button value="addresses">指定地址</el-radio-button>
          <el-radio-button value="groups">共享分组</el-radio-button>
          <el-radio-button value="all_users">所有用户</el-radio-button>
        </el-radio-group>
      </el-form-item>
      <el-form-item v-if="shareUserForm.targetMode === 'addresses'" label="目标地址">
        <el-select
          v-model="shareUserForm.targetAddresses"
          multiple
          collapse-tags
          collapse-tags-tooltip
          max-collapse-tags="2"
          placeholder="选择或输入一个或多个钱包地址"
          filterable
          allow-create
          default-first-option
          fit-input-width
          clearable
          style="width: 100%"
        >
          <el-option
            v-for="member in groupMembers"
            :key="member.id"
            :label="[member.alias, member.name, member.walletAddress].filter(Boolean).join(' ')"
            :value="member.walletAddress"
          >
            <div class="member-option" :title="member.walletAddress">
              <span v-if="member.alias || member.name" class="member-name">{{ member.alias || member.name }}</span>
              <span class="member-address mono">{{ shortenAddress(member.walletAddress) }}</span>
            </div>
          </el-option>
        </el-select>
      </el-form-item>
      <el-form-item v-else-if="shareUserForm.targetMode === 'groups'" label="目标分组">
        <el-select
          v-model="shareUserForm.groupIds"
          multiple
          collapse-tags
          collapse-tags-tooltip
          max-collapse-tags="2"
          placeholder="选择一个或多个共享分组"
          style="width: 100%"
        >
          <el-option v-for="group in managedGroups" :key="group.id" :label="group.name" :value="group.id" />
        </el-select>
        <div class="share-group-meta">分组成员：{{ selectedGroupMembers.length }} 个</div>
      </el-form-item>
      <el-form-item v-if="shareUserForm.targetMode === 'all_users'" label="共享范围">
        <div class="share-inline-meta">当前账号体系下的所有已登录用户</div>
      </el-form-item>
      <el-form-item label="权限">
        <el-checkbox-group v-model="shareUserForm.permissions" class="share-user-permissions">
          <el-checkbox label="read">读取</el-checkbox>
          <el-checkbox label="create">上传</el-checkbox>
          <el-checkbox label="update">重命名</el-checkbox>
          <el-checkbox label="delete">删除</el-checkbox>
        </el-checkbox-group>
      </el-form-item>
      <el-form-item label="有效期">
        <div class="share-expiry-field">
          <el-input v-model="shareUserForm.expiresValue" placeholder="0" />
          <el-select v-model="shareUserForm.expiresUnit" class="share-expiry-unit">
            <el-option
              v-for="item in shareExpiryUnits"
              :key="item.value"
              :label="item.label"
              :value="item.value"
            />
          </el-select>
        </div>
        <div class="share-group-meta">输入 0 表示永不过期</div>
      </el-form-item>
    </el-form>
    <template #footer>
      <el-button @click="shareUserDialogModel = false">取消</el-button>
      <el-button type="primary" :loading="shareUserSubmitting" @click="submitShareUser">确认共享</el-button>
    </template>
  </el-dialog>

  <el-dialog
    v-model="createFolderDialogModel"
    title="新建文件夹"
    width="420px"
  >
    <el-form class="create-folder-form" label-position="top">
      <el-form-item label="文件夹名称">
        <el-input
          v-model="createFolderForm.name"
          placeholder="请输入文件夹名称"
          @keydown.enter.prevent="submitCreateFolder"
        />
      </el-form-item>
      <el-form-item label="目录选项">
        <el-checkbox v-model="createFolderForm.encrypted">创建为加密目录</el-checkbox>
        <div class="share-group-meta">加密目录内的文件会在浏览器端加密后再上传到服务端。</div>
      </el-form-item>
      <template v-if="createFolderForm.encrypted">
        <el-form-item label="加密算法">
          <el-select
            v-model="createFolderForm.cipherSuite"
            class="create-folder-cipher-select"
            placeholder="请选择加密算法"
          >
            <el-option
              v-for="suite in cipherSuiteOptions"
              :key="suite.name"
              :label="suite.description || suite.name"
              :value="suite.name"
            >
              <div class="cipher-suite-option">
                <span>{{ suite.description || suite.name }}</span>
              </div>
            </el-option>
          </el-select>
        </el-form-item>
        <el-form-item label="密钥来源">
          <el-radio-group v-model="createFolderForm.passwordSource">
            <el-radio-button label="wallet">钱包密钥</el-radio-button>
            <el-radio-button label="wallet+password">钱包密钥 + 额外密码</el-radio-button>
          </el-radio-group>
          <div class="share-group-meta">
            钱包密钥模式无需记忆目录密码；增加额外密码后，需要钱包和额外密码一起解密。
          </div>
        </el-form-item>
        <el-form-item v-if="createFolderForm.passwordSource === 'wallet+password'" label="额外密码">
          <el-input
            v-model="createFolderForm.password"
            type="password"
            show-password
            placeholder="请输入额外密码"
          />
        </el-form-item>
        <el-form-item v-if="createFolderForm.passwordSource === 'wallet+password'" label="确认额外密码">
          <el-input
            v-model="createFolderForm.confirmPassword"
            type="password"
            show-password
            placeholder="请再次输入额外密码"
          />
        </el-form-item>
      </template>
    </el-form>
    <template #footer>
      <el-button @click="createFolderDialogModel = false">取消</el-button>
      <el-button type="primary" :loading="createFolderSubmitting" @click="submitCreateFolder">创建</el-button>
    </template>
  </el-dialog>

  <el-dialog
    v-model="renameDialogModel"
    title="重命名"
    width="420px"
  >
    <div class="rename-field">
      <span class="rename-label">旧名称</span>
      <el-input :model-value="renameTarget?.name || ''" disabled />
    </div>
    <div class="rename-field">
      <span class="rename-label">新名称</span>
      <el-input v-model="renameForm.name" placeholder="请输入新的名称" />
    </div>
    <template #footer>
      <el-button @click="renameDialogModel = false">取消</el-button>
      <el-button type="primary" :loading="renameSubmitting" @click="submitRename">保存</el-button>
    </template>
  </el-dialog>

  <el-dialog
    v-model="passwordDialogModel"
    title="设置登录密码"
    width="420px"
  >
    <el-form label-width="90px">
      <el-form-item v-if="userProfile.hasPassword" label="旧密码">
        <el-input v-model="passwordForm.oldPassword" type="password" show-password />
      </el-form-item>
      <el-form-item label="新密码">
        <el-input v-model="passwordForm.newPassword" type="password" show-password />
      </el-form-item>
      <el-form-item label="确认密码">
        <el-input v-model="passwordForm.confirmPassword" type="password" show-password />
      </el-form-item>
    </el-form>
    <template #footer>
      <el-button @click="passwordDialogModel = false">取消</el-button>
      <el-button type="primary" :loading="passwordSubmitting" @click="submitPassword">保存</el-button>
    </template>
  </el-dialog>
</template>

<style scoped src="./homeShared.scss"></style>
<style scoped>
.share-user-value {
  word-break: break-all;
}

.share-user-form :deep(.el-form-item__label) {
  padding-right: 8px;
}

.share-user-form :deep(.el-form-item__content) {
  margin-left: 0;
}

.share-user-permissions {
  display: flex;
  flex-wrap: nowrap;
  gap: 6px;
}

.share-user-permissions :deep(.el-checkbox) {
  margin-right: 6px;
}

.share-expiry-field {
  display: flex;
  align-items: center;
  gap: 10px;
  width: 100%;
}

.share-expiry-field :deep(.el-input) {
  flex: 1;
}

.share-expiry-unit {
  width: 112px;
  flex: 0 0 112px;
}

.share-mode-field {
  display: flex;
  flex-direction: column;
  align-items: flex-start;
  gap: 6px;
  width: 100%;
}

.share-mode-note {
  display: flex;
  flex-direction: column;
  align-items: flex-start;
  width: 100%;
}

.rename-field {
  display: grid;
  grid-template-columns: 72px minmax(0, 1fr);
  gap: 10px;
  align-items: center;
  margin-bottom: 12px;
}

.rename-label {
  font-size: 12px;
  color: #909399;
}

.rename-value {
  font-size: 13px;
  color: #1f2d3d;
  font-weight: 500;
  word-break: break-all;
}

.share-group-meta {
  margin-top: 6px;
  font-size: 12px;
  color: #909399;
}

.share-mode-hint {
  margin-top: 2px;
}

.share-inline-meta {
  margin-top: 0;
  min-height: 32px;
  display: flex;
  align-items: center;
  font-size: 12px;
  color: #909399;
}

.member-option {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
}

.member-name {
  font-size: 13px;
  color: #1f2d3d;
  font-weight: 500;
}

.member-address {
  font-size: 12px;
  color: #909399;
}

.user-tags {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
}

.detail-panel {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.detail-grid {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.detail-row {
  display: grid;
  grid-template-columns: 72px minmax(0, 1fr);
  gap: 10px;
  align-items: start;
}

.detail-label {
  font-size: 12px;
  color: #909399;
}

.detail-value {
  font-size: 13px;
  color: #1f2d3d;
  word-break: break-all;
}

.detail-value.mono {
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, "Liberation Mono", "Courier New", monospace;
  font-size: 12px;
}

.detail-note {
  padding: 10px 12px;
  border-radius: 8px;
  background: #f5f7fa;
  color: #606266;
  font-size: 12px;
  line-height: 1.5;
}

.detail-actions {
  display: flex;
  gap: 10px;
  flex-wrap: wrap;
}

.detail-empty {
  font-size: 13px;
  color: #909399;
}

.detail-drawer :deep(.el-drawer) {
  max-width: calc(100vw - 12px);
}

.detail-drawer :deep(.el-drawer__body) {
  position: relative;
}

.drawer-resize-handle {
  position: absolute;
  top: 0;
  bottom: 0;
  left: 0;
  width: 10px;
  cursor: col-resize;
  z-index: 20;
}

.drawer-resize-handle::after {
  content: '';
  position: absolute;
  top: 14px;
  bottom: 14px;
  left: 4px;
  width: 2px;
  border-radius: 2px;
  background: rgba(96, 98, 102, 0.14);
  transition: background 0.2s ease;
}

.drawer-resize-handle:hover::after,
.drawer-resize-handle.is-active::after {
  background: rgba(64, 158, 255, 0.45);
}
</style>
