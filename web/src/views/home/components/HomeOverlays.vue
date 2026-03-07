<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import type { AddressContact, AddressGroup, DirectShareItem, RecycleItem, ShareItem } from '@/api'
import type { FileItem } from '../types'
import { shortenAddress } from '@/utils/address'

const props = defineProps<{
  detailDrawerVisible: boolean
  detailTitle: string
  detailMode: 'file' | 'recycle' | 'share' | 'directShare' | 'receivedShare' | 'sharedEntry' | null
  detailFile: FileItem | null
  detailRecycle: RecycleItem | null
  detailShare: ShareItem | null
  detailDirectShare: DirectShareItem | null
  detailReceivedShare: DirectShareItem | null
  detailSharedEntry: FileItem | null
  sharedCanRead: boolean
  sharedCanUpdate: boolean
  getPreviewMode: (item: FileItem) => 'text' | 'pdf' | 'word' | 'image' | null
  openFilePreview: (item: FileItem) => void
  formatTime: (time: string | number) => string
  formatDeletedTime: (time: string) => string
  formatSizeDetail: (size: number) => string
  formatSharePermission: (permission: string) => string
  getShareLink: (item: ShareItem) => string
  copyShareLink: (item: ShareItem) => void
  revokeShare: (item: ShareItem) => void
  revokeDirectShare: (item: DirectShareItem) => void
  isDirectShareOwner: (item: DirectShareItem) => boolean
  enterDirectory: (item: FileItem) => void
  enterSharedRoot: (item: DirectShareItem) => void
  enterSharedDirectory: (item: FileItem) => void
  downloadSharedRoot: (item: DirectShareItem) => void
  downloadSharedFile: (item: FileItem) => void
  shareUserDialogVisible: boolean
  shareUserSubmitting: boolean
  shareUserTarget: FileItem | null
  shareUserForm: {
    targetMode: 'single' | 'group'
    targetAddress: string
    groupId: string
    permissions: string[]
    expiresIn: string
  }
  addressContacts: AddressContact[]
  addressGroups: AddressGroup[]
  groupedContacts: AddressContact[]
  submitShareUser: () => void
  createFolderDialogVisible: boolean
  createFolderSubmitting: boolean
  createFolderForm: {
    name: string
  }
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
  (event: 'update:shareUserDialogVisible', value: boolean): void
  (event: 'update:createFolderDialogVisible', value: boolean): void
  (event: 'update:renameDialogVisible', value: boolean): void
  (event: 'update:passwordDialogVisible', value: boolean): void
}>()

const detailDrawerModel = computed({
  get: () => props.detailDrawerVisible,
  set: value => emit('update:detailDrawerVisible', value)
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
          <span class="detail-label">修改时间</span>
          <span class="detail-value time-cell">{{ formatTime(detailFile.modified) }}</span>
        </div>
      </div>
      <div class="detail-actions" v-if="detailFile.isDir">
        <el-button type="primary" size="small" @click="handleEnterDirectory(detailFile)">
          进入目录
        </el-button>
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
          <span class="detail-label">目标地址</span>
          <span class="detail-value mono">{{ detailDirectShare.targetWallet || '-' }}</span>
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
          @click="enterSharedRoot(detailDirectShare)"
        >
          进入目录
        </el-button>
        <el-button
          v-else-if="detailDirectShare.permissions && detailDirectShare.permissions.includes('read')"
          type="primary"
          size="small"
          @click="downloadSharedRoot(detailDirectShare)"
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
          <span class="detail-label">目标地址</span>
          <span class="detail-value mono">{{ detailReceivedShare.targetWallet || '-' }}</span>
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
          <span class="detail-label">过期时间</span>
          <span class="detail-value time-cell">{{ detailReceivedShare.expiresAt ? formatTime(detailReceivedShare.expiresAt) : '永不过期' }}</span>
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
        </template>
      </div>
    </div>

    <div v-else class="detail-empty">暂无详情</div>
  </el-drawer>

  <el-dialog
    v-model="shareUserDialogModel"
    title="共享给用户"
    width="420px"
  >
    <el-form label-width="72px" label-position="left" class="share-user-form">
      <el-form-item label="共享对象">
        <span class="share-user-value">{{ shareUserTarget?.name || '-' }}</span>
      </el-form-item>
      <el-form-item label="共享方式">
        <el-radio-group v-model="shareUserForm.targetMode" size="small">
          <el-radio-button value="single">单个地址</el-radio-button>
          <el-radio-button value="group">分组</el-radio-button>
        </el-radio-group>
      </el-form-item>
      <el-form-item v-if="shareUserForm.targetMode === 'single'" label="目标钱包">
        <el-select
          v-model="shareUserForm.targetAddress"
          placeholder="选择或输入钱包地址"
          filterable
          allow-create
          default-first-option
          clearable
          style="width: 100%"
        >
          <el-option
            v-for="contact in addressContacts"
            :key="contact.id"
            :label="contact.walletAddress"
            :value="contact.walletAddress"
          >
            <div class="contact-option" :title="contact.walletAddress">
              <span v-if="contact.name" class="contact-name">{{ contact.name }}</span>
              <span class="contact-address mono">{{ shortenAddress(contact.walletAddress) }}</span>
            </div>
          </el-option>
        </el-select>
      </el-form-item>
      <el-form-item v-else label="目标分组">
        <el-select v-model="shareUserForm.groupId" placeholder="选择分组" style="width: 100%">
          <el-option label="未分组" value="" />
          <el-option v-for="group in addressGroups" :key="group.id" :label="group.name" :value="group.id" />
        </el-select>
        <div class="share-group-meta">分组地址：{{ groupedContacts.length }} 个</div>
      </el-form-item>
      <el-form-item v-if="shareUserForm.targetMode === 'group' && groupedContacts.length">
        <div class="share-group-list">
          <span v-for="item in groupedContacts" :key="item.id" class="mono">
            {{ shortenAddress(item.walletAddress) }}
          </span>
        </div>
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
        <el-input v-model="shareUserForm.expiresIn" placeholder="小时（0 表示永不过期）" />
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
    <el-form label-width="90px">
      <el-form-item label="文件夹名称">
        <el-input
          v-model="createFolderForm.name"
          placeholder="请输入文件夹名称"
          @keydown.enter.prevent="submitCreateFolder"
        />
      </el-form-item>
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

.contact-option {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
}

.contact-name {
  font-size: 13px;
  color: #1f2d3d;
  font-weight: 500;
}

.contact-address {
  font-size: 12px;
  color: #909399;
}

.share-group-list {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
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

.detail-actions {
  display: flex;
  gap: 10px;
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
