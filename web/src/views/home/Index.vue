<script setup lang="ts">
import { ref, onMounted, onBeforeUnmount, computed, watch, defineAsyncComponent } from 'vue'
import { storeToRefs } from 'pinia'
import QRCode from 'qrcode'
import { ArrowLeft, ArrowRight, ArrowUp, Delete, Expand, Fold, Folder, FolderAdd, FolderOpened, Grid, Refresh, Upload, DocumentCopy, Share, Search, MoreFilled, Notebook, User, Lock, Unlock, Wallet } from '@element-plus/icons-vue'
import { ElMessageBox } from 'element-plus'
import { getSupportedCipherSuites, type CipherSuiteInfo } from '@yeying-community/web3-bs'
import { quotaApi, userApi, recycleApi, shareApi, directShareApi, assetsApi, webdavAccessKeyApi, s3CredentialApi, warehouseToolCredentialApi, adminUserApi, type RecycleItem, type ShareItem, type DirectShareItem, type ReceivedSharedResource, type AssetSpaceInfo, type ShareExpiryUnit, type ShareMode, type AccessKeyPermission, type WebDAVAccessKeyItem, type CreateWebDAVAccessKeyResult, type S3CredentialItem, type CreateS3CredentialResult, type WarehouseToolCredentialItem, type WarehouseToolAuditItem, type AdminUserItem, type GroupMember } from '@/api'
import { AUTH_CHANGED_EVENT, isLoggedIn, getUsername, getWalletName, getCurrentAccount, getUserPermissions, getUserCreatedAt, loginWithWallet, focusPendingWalletApproval, createIdentityLoginSession, pollIdentityLoginStatus, watchWalletProvider } from '@/plugins/auth'
import { decryptBlobContent, encryptFileContent, encryptTextContent } from '@/utils/crypto'
import {
  buildEncryptedDirectoryPasswordContext,
  buildEncryptedDirectoryMetadata,
  buildEncryptedDirectoryMetadataPath,
  DEFAULT_ENCRYPTED_DIRECTORY_CIPHER_SUITE,
  DEFAULT_ENCRYPTED_DIRECTORY_PASSWORD_SOURCE,
  type EncryptedDirectoryMetadata,
  type EncryptedDirectoryPasswordSource,
  getEncryptedDirectoryPassword,
  isEncryptedDirectoryMetadataFileName,
  normalizeEncryptedDirectoryPasswordSource,
  removeEncryptedRootsForDeletedPath,
  resolveEncryptedRoot,
  setEncryptedDirectoryPassword
} from '@/utils/encryptedDirectory'
import { parsePropfindResponse } from '@/utils/webdav'
import { copyText } from '@/utils/clipboard'
import { shortenAddress } from '@/utils/address'
import { showInfo, showSuccess } from '@/utils/toast'
import { useGroupStore } from '@/stores/groupStore'
import { useUploadTaskStore } from '@/stores/uploadTaskStore'
import GroupView from './components/GroupView.vue'
import HomeOverlays from './components/HomeOverlays.vue'
import FileTableView from './components/FileTableView.vue'
import ShareTableView from './components/ShareTableView.vue'
import SharedWithMeTableView from './components/SharedWithMeTableView.vue'
import RecycleTableView from './components/RecycleTableView.vue'
import type { CipherSuiteOption, DropEntry, FileItem, UploadItem, UploadTask } from './types'
import userGuideMarkdown from '../../../../docs/产品概览/用户指南.md?raw'

const FilePreviewDialog = defineAsyncComponent(() => import('./components/FilePreviewDialog.vue'))
const DAV_PREFIX = normalizeDavPrefix((import.meta as any)?.env?.VITE_WEBDAV_PREFIX || '/dav')

// 状态
const loading = ref(false)
const fileList = ref<FileItem[]>([])
const currentPath = ref('/')
const quota = ref({ quota: 0, used: 0, available: 0, percentage: 0, unlimited: true })
const userInfo = ref<{
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
} | null>(null)
type LoginMode = 'identity' | 'wallet'
const loginMode = ref<LoginMode>('wallet')
const identityLoading = ref(false)
const identityPolling = ref(false)
const identitySessionId = ref('')
const identityQrcodeUrl = ref('')
const identityQrcodeImage = ref('')
const identityStatusText = ref('')
const identityPollInterval = ref(2)
let identityTimer: number | null = null
let identityRequestSeq = 0
const isMobileViewport = ref(false)
const walletPresent = ref(false)
const walletLoginSubmitting = ref(false)
const loggedIn = ref(isLoggedIn())
let stopWalletProviderWatch: (() => void) | null = null

// 回收站相关状态
const showRecycle = ref(false)
const recycleList = ref<RecycleItem[]>([])
const recycleLoading = ref(false)
const recycleClearing = ref(false)
const recyclePage = ref(1)
const recyclePageSize = 50
const recycleTotal = ref(0)
const showShare = ref(false)
const shareList = ref<ShareItem[]>([])
const shareLoading = ref(false)
const shareTab = ref<'link' | 'direct'>('link')
const directShareList = ref<DirectShareItem[]>([])
const directShareLoading = ref(false)
const showSharedWithMe = ref(false)
const showHelp = ref(false)
const sharedWithMeList = ref<ReceivedSharedResource[]>([])
const sharedWithMeLoading = ref(false)
const sharedActive = ref<ReceivedSharedResource | null>(null)
const sharedPath = ref('/')
const sharedEntries = ref<FileItem[]>([])
const sharedEntriesLoading = ref(false)
const showQuotaManage = ref(false)
const quotaManageLoading = ref(false)
const adminUsersLoading = ref(false)
const canManageUsers = ref(false)
const adminUsersError = ref('')
const adminUsers = ref<AdminUserItem[]>([])
const adminUsersFilter = ref<'all' | 'unlimited' | 'near_limit' | 'over_quota'>('all')
const adminUsersDialogVisible = ref(false)
const adminUsersSubmitting = ref(false)
const adminUsersEditTarget = ref<AdminUserItem | null>(null)
const adminUsersEditValue = ref('')
const adminUsersEditUnit = ref<'B' | 'KB' | 'MB' | 'GB' | 'TB'>('GB')
const showGroupView = ref(false)
const managementSection = ref<ManagementSection>('account')
const credentialTab = ref<CredentialTab>('webdav')
const manualRefresh = ref(false)
const detailDrawerVisible = ref(false)
const detailMode = ref<'file' | 'recycle' | 'share' | 'directShare' | 'receivedShare' | 'sharedEntry' | null>(null)
const detailItem = ref<FileItem | RecycleItem | ShareItem | DirectShareItem | ReceivedSharedResource | null>(null)
const selectedFileRows = ref<FileItem[]>([])
const fileSelectionMode = ref(false)
const fileSelectionNonce = ref(0)
const dragActive = ref(false)
const dragCounter = ref(0)
const draggingFileItem = ref<FileItem | null>(null)
const dragMoveTargetPath = ref('')
const movingByDrag = ref(false)
const draggingSharedItem = ref<FileItem | null>(null)
const sharedDragMoveTargetPath = ref('')
const movingSharedByDrag = ref(false)
const shareLinkDialogVisible = ref(false)
const shareLinkSubmitting = ref(false)
const shareLinkTarget = ref<FileItem | null>(null)
const shareLinkResourceContext = ref<{ resourceId: string; relativePath: string } | null>(null)
const shareLinkForm = ref(createDefaultShareLinkForm())
const shareUserDialogVisible = ref(false)
const shareUserSubmitting = ref(false)
const shareUserTarget = ref<FileItem | null>(null)
const shareUserForm = ref({
  targetMode: 'addresses' as 'addresses' | 'groups' | 'all_users',
  targetAddresses: [] as string[],
  groupIds: [] as string[],
  permissions: ['read'] as string[],
  ...createDefaultShareExpiryForm()
})
const accessKeyLoading = ref(false)
const accessKeySubmitting = ref(false)
const accessKeyBinding = ref(false)
const accessKeyDialogVisible = ref(false)
const accessKeyScopePath = ref('/')
const accessKeyScopeLocked = ref(false)
const accessKeyBindTargetID = ref('')
const accessKeys = ref<WebDAVAccessKeyItem[]>([])
const accessKeyCreateResult = ref<CreateWebDAVAccessKeyResult | null>(null)
const accessKeyDirectoryPickerVisible = ref(false)
const accessKeyDirectoryPickerLoading = ref(false)
const accessKeyDirectoryPickerPath = ref('/personal')
const accessKeyDirectoryPickerItems = ref<FileItem[]>([])
const directoryPickerTarget = ref<'accessKey' | 'tool'>('accessKey')
const directoryPickerCreateMode = ref(false)
const directoryPickerNewFolderName = ref('')
const directoryPickerCreateSubmitting = ref(false)
const s3CredentialLoading = ref(false)
const s3CredentialSubmitting = ref(false)
const s3CredentialDialogVisible = ref(false)
const s3Credentials = ref<S3CredentialItem[]>([])
const s3CredentialCreateResult = ref<CreateS3CredentialResult | null>(null)
const s3CredentialName = ref('')
const s3CredentialBucket = ref<'personal' | 'apps' | 'services'>('personal')
const s3CredentialDirectory = ref('')
const toolCredentialLoading = ref(false)
const toolCredentialSubmitting = ref(false)
const toolCredentialDialogVisible = ref(false)
const toolCredentialName = ref('')
const toolCredentialPath = ref('')
const toolCredentialScopes = ref<string[]>(['asset:read', 'asset:write'])
const toolCredentialExpiresAt = ref('')
const toolCredentials = ref<WarehouseToolCredentialItem[]>([])
const toolCredentialCreateResult = ref<{ id: string; secret: string; expiresAt: string; warning: string } | null>(null)
const toolAuditLoading = ref(false)
const toolAudits = ref<WarehouseToolAuditItem[]>([])
const accessKeyForm = ref(createDefaultAccessKeyForm('/'))
const groupStore = useGroupStore()
const { groupLoading, managedGroups, activeGroupMembers } = storeToRefs(groupStore)
const shareAddressMembers = computed(() => dedupeGroupMembersByWalletAddress(activeGroupMembers.value))
const uploadTaskStore = useUploadTaskStore()
const passwordDialogVisible = ref(false)
const passwordSubmitting = ref(false)
const passwordForm = ref({
  oldPassword: '',
  newPassword: '',
  confirmPassword: ''
})
const createFolderDialogVisible = ref(false)
const createFolderSubmitting = ref(false)
const createFolderMode = ref<'file' | 'shared' | null>(null)
const createFolderForm = ref({
  name: '',
  encrypted: false,
  cipherSuite: DEFAULT_ENCRYPTED_DIRECTORY_CIPHER_SUITE,
  passwordSource: DEFAULT_ENCRYPTED_DIRECTORY_PASSWORD_SOURCE as EncryptedDirectoryPasswordSource,
  password: '',
  confirmPassword: ''
})
const fileSearch = ref('')
const recycleSearch = ref('')
const shareLinkSearch = ref('')
const shareDirectSearch = ref('')
const sharedSearch = ref('')
const renameDialogVisible = ref(false)
const renameSubmitting = ref(false)
const renameTarget = ref<FileItem | null>(null)
const renameMode = ref<'file' | 'shared' | null>(null)
const renameForm = ref({
  name: ''
})
type PreviewMode = 'text' | 'pdf' | 'word' | 'image' | 'audio' | 'video'
type PreviewFormatOption = {
  mode: PreviewMode
  label: string
  extensions: string
}
const previewVisible = ref(false)
const previewMode = ref<PreviewMode | null>(null)
const previewLoading = ref(false)
const previewSaving = ref(false)
const previewContent = ref('')
const previewOrigin = ref('')
const previewTarget = ref<FileItem | null>(null)
const previewBlob = ref<Blob | null>(null)
const previewSourceUrl = ref('')
const previewReadOnly = ref(false)
const previewFormatDialogVisible = ref(false)
const previewFormatTarget = ref<FileItem | null>(null)
const previewFormatSelection = ref<PreviewMode | null>(null)
let previewRequestSeq = 0
let identityBroadcastChannel: BroadcastChannel | null = null
const encryptedDirectoryRoots = ref<string[]>([])
const encryptedDirectoryMetadata = ref<Record<string, EncryptedDirectoryMetadata>>({})
const cipherSuiteOptions = ref<CipherSuiteOption[]>([
  {
    name: DEFAULT_ENCRYPTED_DIRECTORY_CIPHER_SUITE,
    description: '国际默认',
    mode: 'symmetric'
  }
])
const currentEncryptedRoot = computed(() => {
  if (showRecycle.value || showShare.value || showSharedWithMe.value) return null
  return resolveEncryptedRootForPath(currentPath.value)
})
const currentEncryptedDirectoryPasswordCached = computed(() => {
  const root = currentEncryptedRoot.value
  return root ? isEncryptedDirectoryPasswordCachedForRoot(root) : false
})
const currentEncryptedDirectoryRequiresPassword = computed(() => {
  const root = currentEncryptedRoot.value
  return root ? requiresEncryptedDirectoryExtraPassword(root) : false
})
const currentEncryptedDirectoryStatusText = computed(() => {
  const root = currentEncryptedRoot.value
  if (!root) return ''
  const normalizedCurrent = normalizeEncryptedRootPath(currentPath.value)
  const scope = root === normalizedCurrent ? '当前目录' : `继承自 ${root}`
  if (!requiresEncryptedDirectoryExtraPassword(root)) {
    return `${scope}，${getEncryptedDirectoryProtectionLabelForRoot(root)}`
  }
  return currentEncryptedDirectoryPasswordCached.value ? `${scope}，已解锁` : `${scope}，未解锁`
})

function getEncryptedDirectoryRootForItem(item: FileItem | null | undefined): string | null {
  if (!item) return null
  if (item.encryptedRoot) return normalizeEncryptedRootPath(item.encryptedRoot)
  if (item.isDir && item.encrypted) return normalizeEncryptedRootPath(item.path)
  return resolveEncryptedRootForPath(item.path)
}

function isEncryptedDirectoryPasswordCachedForRoot(rootPath: string | null): boolean {
  if (!rootPath) return false
  const normalizedRoot = normalizeEncryptedRootPath(rootPath)
  if (!requiresEncryptedDirectoryExtraPassword(normalizedRoot)) return true
  return Boolean(getEncryptedDirectoryPassword(normalizedRoot))
}
const VIEW_STORAGE_KEY = 'warehouse:lastView'
const MANAGEMENT_SECTION_STORAGE_KEY = 'warehouse:lastManagementSection'
const FILE_PATH_STORAGE_KEY = 'warehouse:lastFilePath'
const SHARED_ACTIVE_STORAGE_KEY = 'warehouse:sharedActiveId'
const SHARED_PATH_STORAGE_KEY = 'warehouse:sharedPath'
const SIDEBAR_COLLAPSED_STORAGE_KEY = 'warehouse:sidebarCollapsed'
type ViewKey = 'files' | 'recycle' | 'shareLink' | 'shareDirect' | 'sharedWithMe' | 'quotaManage' | 'group' | 'help'
type ManagementSection = 'account' | 'keys' | 'adminUsers' | 'group'
type CredentialTab = 'webdav' | 's3' | 'tool'
type AssetSpace = AssetSpaceInfo
type ShareExpiryForm = {
  expiresValue: string
  expiresUnit: ShareExpiryUnit
}
type ShareLinkForm = ShareExpiryForm & {
  mode: ShareMode
}
type AccessKeyForm = ShareExpiryForm & {
  name: string
  rootPath: string
  permissions: AccessKeyPermission[]
}
type DirectShareRelation = 'owned' | 'received'
type DirectShareListItem = DirectShareItem & { relation?: DirectShareRelation }
const SHARE_EXPIRY_UNITS: Array<{ label: string; value: ShareExpiryUnit }> = [
  { label: '分钟', value: 'minute' },
  { label: '小时', value: 'hour' },
  { label: '天', value: 'day' },
  { label: '周', value: 'week' },
  { label: '月', value: 'month' },
  { label: '年', value: 'year' }
]
const SHARE_MODE_OPTIONS: Array<{ label: string; value: ShareMode; description: string; hint: string }> = [
  {
    label: '仅下载',
    value: 'download',
    description: '访问链接后直接下载文件',
    hint: '适合压缩包、安装包、二进制文件，或希望避免浏览器内联打开的场景'
  },
  {
    label: '浏览器打开',
    value: 'preview',
    description: '浏览器会尝试直接打开支持的文件类型，不支持时仍可能下载',
    hint: '更适合图片、PDF、文本等浏览器原生支持或较易渲染的格式'
  }
]
const ACCESS_KEY_PERMISSIONS: Array<{ label: string; value: AccessKeyPermission }> = [
  { label: '读取', value: 'read' },
  { label: '新增', value: 'create' },
  { label: '修改', value: 'update' },
  { label: '删除', value: 'delete' }
]
const TOOL_CREDENTIAL_SCOPES: Array<{ label: string; value: string; description: string }> = [
  { label: '读取', value: 'asset:read', description: '列出、查看授权目录中的对象' },
  { label: '写入', value: 'asset:write', description: '在授权目录中创建或更新对象' }
]
const ASSET_SPACE_NAME_BY_KEY: Record<string, string> = {
  personal: '个人资产',
  apps: '应用资产',
  services: '服务资产'
}
const ASSET_SPACE_PATH_BY_KEY: Record<string, string> = {
  personal: '/personal',
  apps: '/apps',
  services: '/services'
}
const DEFAULT_ASSET_SPACES: AssetSpace[] = [
  { key: 'personal', name: '个人资产', path: '/personal' },
  { key: 'apps', name: '应用资产', path: '/apps' },
  { key: 'services', name: '服务资产', path: '/services' }
]
const assetSpaces = ref<AssetSpace[]>(DEFAULT_ASSET_SPACES)
const defaultAssetSpaceKey = ref('personal')
const assetSpaceLoading = ref(false)
const sidePanelCollapsed = ref(false)

// 是否显示回收站列表
const isFileView = computed(() => !showRecycle.value && !showShare.value && !showQuotaManage.value && !showSharedWithMe.value && !showGroupView.value && !showHelp.value)
const canUpload = computed(() => {
  if (!isFileView.value && !showSharedWithMe.value) return false
  if (showSharedWithMe.value) return isSharedBrowse.value && sharedCanCreate.value
  return true
})
const isSharedBrowse = computed(() => showSharedWithMe.value && !!sharedActive.value)
const sharedPermissions = computed(() => sharedActive.value?.permissions || [])
const sharedCanRead = computed(() => sharedPermissions.value.includes('read'))
const sharedCanCreate = computed(() => sharedPermissions.value.includes('create'))
const sharedCanUpdate = computed(() => sharedPermissions.value.includes('update'))
const sharedCanDelete = computed(() => sharedPermissions.value.includes('delete'))
const userProfile = computed(() => {
  const username = userInfo.value?.username || getUsername() || '当前用户'
  const walletAddress = userInfo.value?.wallet_address || localStorage.getItem('walletAddress') || getCurrentAccount() || '-'
  const walletName = getWalletName()
  const permissions = userInfo.value?.permissions?.length ? userInfo.value.permissions : getUserPermissions()
  const createdAt = userInfo.value?.created_at || getUserCreatedAt()
  const hasPassword = Boolean(userInfo.value?.has_password)
  return { username, walletAddress, walletName, permissions, createdAt, hasPassword }
})
const nextLoginMode = computed<LoginMode>(() => {
  if (loginMode.value === 'identity') return 'wallet'
  return 'identity'
})
const nextLoginModeLabel = computed(() => {
  if (nextLoginMode.value === 'wallet') return '钱包登录'
  return '通行证登录'
})
const nextLoginModeIcon = computed(() => {
  if (nextLoginMode.value === 'wallet') return Wallet
  return Grid
})
const loginSubtitle = computed(() => {
  if (loginMode.value === 'identity') return '使用通行证完成身份验证后进入 Warehouse。'
  return '使用夜莺钱包授权钱包身份，Warehouse 将读取已验证邮箱后进入工作区。'
})
const showSearch = computed(() => !showQuotaManage.value && !showGroupView.value && !showHelp.value)
const showListHeader = computed(() => !showQuotaManage.value && !showGroupView.value && !showHelp.value)
const helpGuideHtml = computed(() => renderMarkdown(userGuideMarkdown))
type MobileAction = { command: string; label: string; disabled?: boolean }
type MobileActionGroup = { title: string; items: MobileAction[] }

const mobileActionGroups = computed<MobileActionGroup[]>(() => {
  if (showRecycle.value) {
    return [
      {
        title: '回收站',
        items: [
          { command: 'clearRecycle', label: '清空回收站', disabled: recycleLoading.value || recycleClearing.value }
        ]
      },
      {
        title: '列表',
        items: [
          { command: 'refresh', label: '刷新', disabled: recycleLoading.value || recycleClearing.value }
        ]
      }
    ]
  }
  if (showShare.value) {
    return [
      {
        title: '列表',
        items: [
          { command: 'refresh', label: '刷新', disabled: shareTab.value === 'link' ? shareLoading.value : directShareLoading.value }
        ]
      }
    ]
  }
  if (showSharedWithMe.value) {
    if (sharedActive.value) {
      const uploadItems: MobileAction[] = []
      if (sharedCanCreate.value) {
        uploadItems.push(
          { command: 'createFolder', label: '新建文件夹' },
          { command: 'uploadFile', label: '上传文件' },
          { command: 'uploadDir', label: '上传目录' }
        )
      }
      const groups: MobileActionGroup[] = []
      if (uploadItems.length) {
        groups.push({ title: '上传与新建', items: uploadItems })
      }
      groups.push({
        title: '列表',
        items: [
          { command: 'refresh', label: '刷新', disabled: sharedEntriesLoading.value }
        ]
      })
      return groups
    }
    return [
      {
        title: '列表',
        items: [{ command: 'refresh', label: '刷新', disabled: sharedWithMeLoading.value }]
      }
    ]
  }
  return [
    {
      title: '上传与新建',
      items: [
        { command: 'createFolder', label: '新建文件夹' },
        { command: 'uploadFile', label: '上传文件' },
        { command: 'uploadDir', label: '上传目录' }
      ]
    },
    {
      title: '列表',
      items: [
        { command: 'refresh', label: '刷新', disabled: loading.value }
      ]
    }
  ]
})
const flatMobileActions = computed(() => mobileActionGroups.value.flatMap(group => group.items))
const hasMobileActions = computed(() => flatMobileActions.value.length > 0)
const singleRefreshAction = computed<MobileAction | null>(() => {
  if (flatMobileActions.value.length !== 1) return null
  const only = flatMobileActions.value[0]
  return only.command === 'refresh' ? only : null
})
const showMobileMenu = computed(() => hasMobileActions.value && !singleRefreshAction.value)
const mobileActionIcons: Record<string, any> = {
  createFolder: FolderAdd,
  uploadFile: Upload,
  uploadDir: FolderOpened,
  clearRecycle: Delete,
  refresh: Refresh
}
function getMobileActionIcon(command: string) {
  return mobileActionIcons[command] || MoreFilled
}
const searchKeyword = computed({
  get: () => {
    if (showRecycle.value) return recycleSearch.value
    if (showShare.value) return shareTab.value === 'link' ? shareLinkSearch.value : shareDirectSearch.value
    if (showSharedWithMe.value) return sharedSearch.value
    return fileSearch.value
  },
  set: (value: string) => {
    if (showRecycle.value) {
      recycleSearch.value = value
      return
    }
    if (showShare.value) {
      if (shareTab.value === 'link') {
        shareLinkSearch.value = value
      } else {
        shareDirectSearch.value = value
      }
      return
    }
    if (showSharedWithMe.value) {
      sharedSearch.value = value
      return
    }
    fileSearch.value = value
  }
})
const searchPlaceholder = computed(() => {
  if (showRecycle.value) return '搜索回收站'
  if (showShare.value) return shareTab.value === 'link' ? '搜索分享链接' : '搜索分享对象'
  if (showSharedWithMe.value) return sharedActive.value ? '搜索共享内容' : '搜索分享给我'
  return '搜索文件或目录'
})
const currentAssetSpace = computed(() => resolveAssetSpaceByPath(currentPath.value))
const currentAssetSpaceKey = computed(() => currentAssetSpace.value?.key || '')
const fileParentRootPath = computed(() => normalizeDirectoryPath(currentAssetSpace.value?.path || '/'))
const accessKeyDirectoryPickerBreadcrumbs = computed(() => {
  const current = normalizeAccessKeyRootPath(accessKeyDirectoryPickerPath.value)
  const space = resolveAssetSpaceByPath(current)
  if (!space) return []
  const root = normalizeAccessKeyRootPath(space.path)
  const relative = current.slice(root.length).split('/').filter(Boolean)
  const breadcrumbs = [{ label: space.name, path: root }]
  let accumulated = root
  for (const segment of relative) {
    accumulated = normalizeAccessKeyRootPath(`${accumulated}/${segment}`)
    breadcrumbs.push({ label: segment, path: accumulated })
  }
  return breadcrumbs
})
const canGoFileParent = computed(() => {
  const current = normalizePathForSpaceMatch(currentPath.value)
  const root = normalizePathForSpaceMatch(fileParentRootPath.value)
  if (root === '/') return current !== '/'
  return current !== root
})
const fileParentTargetPath = computed(() => {
  const current = normalizeDirectoryPath(currentPath.value)
  const rootPath = fileParentRootPath.value
  const normalizedCurrent = normalizePathForSpaceMatch(current)
  const normalizedRoot = normalizePathForSpaceMatch(rootPath)
  if (normalizedCurrent === '/' || normalizedCurrent === normalizedRoot) {
    return rootPath
  }
  const parts = current.split('/').filter(Boolean)
  parts.pop()
  let parentPath = parts.length > 0 ? '/' + parts.join('/') + '/' : '/'
  if (normalizedRoot !== '/' && !isPathInSpace(parentPath, rootPath)) {
    parentPath = rootPath
  }
  return normalizeDirectoryPath(parentPath)
})
const fileBreadcrumbRoot = computed(() => {
  const space = currentAssetSpace.value
  if (space) {
    return {
      name: space.name,
      path: normalizeDirectoryPath(space.path)
    }
  }
  return { name: '根目录', path: '/' }
})
const managementSectionLabel = computed(() => {
  if (managementSection.value === 'keys') return '密钥管理'
  if (managementSection.value === 'adminUsers') return '用户管理'
  if (managementSection.value === 'group') return '分组管理'
  return '我的资料'
})
const isManagementView = computed(() => showQuotaManage.value || showGroupView.value)
const mobileLocationLabel = computed(() => {
  if (showRecycle.value) return '回收站'
  if (showShare.value) return '分享'
  if (showSharedWithMe.value) return sharedActive.value ? '共享内容' : '收到的分享'
  if (showHelp.value) return '使用指南'
  if (showQuotaManage.value) return managementSectionLabel.value
  if (showGroupView.value) return '分组管理'
  const normalizedPath = normalizeDirectoryPath(currentPath.value)
  if (currentAssetSpace.value && normalizedPath === normalizeDirectoryPath(currentAssetSpace.value.path)) {
    return currentAssetSpace.value.name
  }
  const parts = currentPath.value.split('/').filter(Boolean)
  if (parts.length) return parts[parts.length - 1]
  const defaultSpace = getDefaultAssetSpace()
  return defaultSpace?.name || '根目录'
})
const mobileLocationText = computed(() => `当前位置：${mobileLocationLabel.value}`)
const detailTitle = computed(() => {
  if (detailMode.value === 'recycle') return '回收站详情'
  if (detailMode.value === 'share') return '分享详情'
  if (detailMode.value === 'directShare') return '定向分享详情'
  if (detailMode.value === 'receivedShare') return '共享详情'
  if (detailMode.value === 'sharedEntry') {
    return detailSharedEntry.value?.isDir ? '共享目录详情' : '共享文件详情'
  }
  if (detailMode.value === 'file') {
    return detailFile.value?.isDir ? '目录详情' : '文件详情'
  }
  return '详情信息'
})
const detailFile = computed(() => (detailMode.value === 'file' ? (detailItem.value as FileItem | null) : null))
const detailRecycle = computed(() => (detailMode.value === 'recycle' ? (detailItem.value as RecycleItem | null) : null))
const detailShare = computed(() => (detailMode.value === 'share' ? (detailItem.value as ShareItem | null) : null))
const detailDirectShare = computed(() => (detailMode.value === 'directShare' ? (detailItem.value as DirectShareItem | null) : null))
const detailReceivedShare = computed(() => (detailMode.value === 'receivedShare' ? (detailItem.value as ReceivedSharedResource | null) : null))
const detailSharedEntry = computed(() => (detailMode.value === 'sharedEntry' ? (detailItem.value as FileItem | null) : null))
const previewTitle = computed(() => {
  if (!previewTarget.value) return '文件预览'
  if (previewMode.value === 'text') return `编辑：${previewTarget.value.name}`
  return `预览：${previewTarget.value.name}`
})
const previewDirty = computed(
  () => previewMode.value === 'text' && !previewReadOnly.value && previewContent.value !== previewOrigin.value
)
const selectedGroupMembers = computed(() => {
  if (shareUserForm.value.targetMode !== 'groups') return []
  const groupSet = new Set(
    shareUserForm.value.groupIds.map(item => String(item || '').trim())
  )
  if (!groupSet.size) return []
  return dedupeGroupMembersByWalletAddress(
    activeGroupMembers.value.filter(item => groupSet.has(String(item.groupId || '').trim()))
  )
})

function dedupeGroupMembersByWalletAddress(members: GroupMember[]): GroupMember[] {
  const uniqueMembers = new Map<string, GroupMember>()
  for (const member of members) {
    const walletAddress = normalizeWalletAddress(member.walletAddress)
    const key = walletAddress || `member:${member.id}`
    const existing = uniqueMembers.get(key)
    if (!existing || memberDisplayPriority(member) > memberDisplayPriority(existing)) {
      uniqueMembers.set(key, member)
    }
  }
  return Array.from(uniqueMembers.values())
}

function memberDisplayPriority(member: GroupMember): number {
  if (String(member.alias || '').trim()) return 2
  const name = String(member.name || '').trim()
  return name && normalizeWalletAddress(name) !== normalizeWalletAddress(member.walletAddress) ? 1 : 0
}
const quotaAvailable = computed(() => {
  if (quota.value.unlimited) return null
  const available = Number.isFinite(quota.value.available)
    ? quota.value.available
    : quota.value.quota - quota.value.used
  return Math.max(available, 0)
})
const quotaAlertState = computed(() => {
  if (quota.value.unlimited || quota.value.quota <= 0) {
    return {
      level: 'info' as 'info' | 'warning' | 'danger',
      label: '未设置上限',
      message: '当前账号未设置存储上限。'
    }
  }
  const usage = quota.value.used / quota.value.quota
  if (quota.value.used > quota.value.quota || usage > 1) {
    return {
      level: 'danger' as const,
      label: '已超额',
      message: '当前已超过存储额度上限，新增写入会被拒绝。'
    }
  }
  if (usage >= 0.8) {
    return {
      level: 'warning' as const,
      label: '接近上限',
      message: '当前使用率已达到 80% 以上，建议尽快清理文件或联系管理员扩容。'
    }
  }
  return {
    level: 'info' as const,
    label: '正常',
    message: '当前额度使用正常。'
  }
})
const adminUsersSummary = computed(() => {
  const items = adminUsers.value
  const limitedUsers = items.filter(item => adminUserQuotaStatusValue(item) !== 'unlimited')
  const overQuotaUsers = items.filter(item => adminUserQuotaStatusValue(item) === 'over_quota')
  const nearLimitUsers = items.filter(item => adminUserQuotaStatusValue(item) === 'near_limit')
  return {
    total: items.length,
    limited: limitedUsers.length,
    unlimited: items.length - limitedUsers.length,
    nearLimit: nearLimitUsers.length,
    overQuota: overQuotaUsers.length
  }
})
const adminUsersHeadline = computed(() => {
  if (adminUsersSummary.value.overQuota > 0) {
    return {
      type: 'error' as 'error' | 'warning' | 'info',
      title: `当前有 ${adminUsersSummary.value.overQuota} 个用户已超额，新增写入会被拒绝。`
    }
  }
  if (adminUsersSummary.value.nearLimit > 0) {
    return {
      type: 'warning' as const,
      title: `当前有 ${adminUsersSummary.value.nearLimit} 个用户使用率已达到 80% 以上。`
    }
  }
  return null
})
const filteredAdminUsers = computed(() => {
  const filter = adminUsersFilter.value
  const items = [...adminUsers.value]
  const filtered = items.filter(item => {
    const status = adminUserQuotaStatusValue(item)
    if (filter === 'unlimited') return status === 'unlimited'
    if (filter === 'near_limit') return status === 'near_limit'
    if (filter === 'over_quota') return status === 'over_quota'
    return true
  })
  filtered.sort((a, b) => {
    const aUsage = adminUserUsageRate(a)
    const bUsage = adminUserUsageRate(b)
    if (aUsage === null && bUsage === null) return a.username.localeCompare(b.username)
    if (aUsage === null) return 1
    if (bUsage === null) return -1
    if (bUsage !== aUsage) return bUsage - aUsage
    return a.username.localeCompare(b.username)
  })
  return filtered
})
const accessKeysForScope = computed(() => {
  const scope = normalizeAccessKeyRootPath(accessKeyScopePath.value)
  return accessKeys.value.filter(item => isAccessKeyBoundToScope(item, scope))
})
const activeAccessKeysForScope = computed(() =>
  accessKeysForScope.value.filter(item => item.status === 'active')
)
const activeUnboundKeysForScope = computed(() => {
  const scope = normalizeAccessKeyRootPath(accessKeyScopePath.value)
  return accessKeys.value.filter(item => item.status === 'active' && !isAccessKeyBoundToScope(item, scope))
})

function normalizeWalletAddress(address?: string | null): string {
  return String(address || '').trim().toLowerCase()
}

const currentWalletAddress = computed(() =>
  normalizeWalletAddress(userInfo.value?.wallet_address || localStorage.getItem('walletAddress') || getCurrentAccount() || '')
)

function resolveDirectShareRelation(item: DirectShareListItem): DirectShareRelation {
  if (item.relation === 'owned' || item.relation === 'received') {
    return item.relation
  }
  const ownerWallet = normalizeWalletAddress(item.ownerWallet)
  if (ownerWallet && currentWalletAddress.value) {
    return ownerWallet === currentWalletAddress.value ? 'owned' : 'received'
  }
  return directShareList.value.some(entry => entry.id === item.id) ? 'owned' : 'received'
}

function isDirectShareOwner(item: DirectShareItem): boolean {
  return resolveDirectShareRelation(item as DirectShareListItem) === 'owned'
}

const mergedDirectShareList = computed<DirectShareListItem[]>(() => {
  const ownedRows = directShareList.value.map(item => ({ ...item, relation: 'owned' as const }))
  return ownedRows.sort((a, b) => {
    const timeA = Date.parse(a.createdAt || '') || 0
    const timeB = Date.parse(b.createdAt || '') || 0
    return timeB - timeA
  })
})

const breadcrumbItems = computed(() => {
  if (showRecycle.value) return []
  const rootPath = normalizePathForSpaceMatch(fileBreadcrumbRoot.value.path)
  const normalizedCurrent = normalizePathForSpaceMatch(currentPath.value)
  let relative = normalizedCurrent
  if (rootPath !== '/' && isPathInSpace(normalizedCurrent, rootPath)) {
    relative = normalizedCurrent.slice(rootPath.length)
  }
  const parts = relative.split('/').filter(Boolean)
  const items: { name: string; path: string }[] = []
  let acc = rootPath === '/' ? '' : rootPath
  for (const part of parts) {
    acc += '/' + part
    items.push({ name: part, path: normalizeDirectoryPath(acc) })
  }
  return items
})
const sharedBreadcrumbItems = computed(() => {
  if (!sharedActive.value) return []
  const parts = sharedPath.value.split('/').filter(Boolean)
  const items: { name: string; path: string }[] = []
  let acc = ''
  for (const part of parts) {
    acc += '/' + part
    items.push({ name: part, path: acc + '/' })
  }
  return items
})
const sharedParentTargetPath = computed(() => {
  if (sharedPath.value === '/') return '/'
  const parts = sharedPath.value.split('/').filter(Boolean)
  parts.pop()
  return parts.length > 0 ? '/' + parts.join('/') + '/' : '/'
})
const sortedFileList = computed(() => [...fileList.value].sort(compareItemsByModifiedDesc))
const sortedRecycleList = computed(() => [...recycleList.value].sort(compareItemsByDeletedAtDesc))
const sortedSharedWithMeList = computed(() => [...sharedWithMeList.value].sort(compareItemsByCreatedAtDesc))
const sortedSharedEntries = computed(() => [...sharedEntries.value].sort(compareItemsByModifiedDesc))
const selectedFileCount = computed(() => selectedFileRows.value.length)
const fileSelectionSummaryText = computed(() => {
  const count = selectedFileCount.value
  if (count <= 0) return '勾选列表中的资产后，可以执行批量删除'
  return `已选中 ${count} 项资产，可以执行批量删除`
})
const shareViewSummary = computed(() => {
  if (showSharedWithMe.value && !sharedActive.value) {
    return {
      title: '分享给我',
      description: '这里展示其他人共享给你的目录或文件。点击目录可继续浏览，点击文件可查看详情。'
    }
  }
  return null
})
const searchToken = computed(() => searchKeyword.value.trim().toLowerCase())
const filteredFileList = computed(() => {
  const token = searchToken.value
  if (!token) return sortedFileList.value
  return sortedFileList.value.filter(item => item.name.toLowerCase().includes(token))
})
const filteredRecycleList = computed(() => sortedRecycleList.value)
const filteredShareList = computed(() => {
  const token = searchToken.value
  if (!token) return shareList.value
  return shareList.value.filter(item => {
    if (item.name.toLowerCase().includes(token)) return true
    if (item.path?.toLowerCase().includes(token)) return true
    return item.url?.toLowerCase().includes(token) || false
  })
})
const filteredDirectShareList = computed(() => {
  const token = searchToken.value
  if (!token) return mergedDirectShareList.value
  return mergedDirectShareList.value.filter(item => {
    if (item.name.toLowerCase().includes(token)) return true
    if (item.path?.toLowerCase().includes(token)) return true
    if (item.targetWallet?.toLowerCase().includes(token)) return true
    const relationLabel = isDirectShareOwner(item) ? '我分享的' : '分享我的'
    return relationLabel.includes(token)
  })
})
const filteredSharedWithMeList = computed(() => {
  const token = searchToken.value
  if (!token) return sortedSharedWithMeList.value
  return sortedSharedWithMeList.value.filter(item => {
    if (item.name.toLowerCase().includes(token)) return true
    if (item.ownerName?.toLowerCase().includes(token)) return true
    if (`${item.grantCount} 条有效授权`.includes(token)) return true
    return item.ownerWallet?.toLowerCase().includes(token) || false
  })
})
const filteredSharedEntries = computed(() => {
  const token = searchToken.value
  if (!token) return sortedSharedEntries.value
  return sortedSharedEntries.value.filter(item => item.name.toLowerCase().includes(token))
})
const previewImageItems = computed(() => {
  const items = isSharedBrowse.value ? sortedSharedEntries.value : sortedFileList.value
  return items.filter(item => isImagePreviewItem(item))
})
const previewImageIndex = computed(() => {
  if (previewMode.value !== 'image' || !previewTarget.value) return -1
  return previewImageItems.value.findIndex(item => item.path === previewTarget.value?.path)
})
const previewImageTotal = computed(() => previewImageItems.value.length)
const canPreviewPreviousImage = computed(() => previewImageIndex.value > 0)
const canPreviewNextImage = computed(
  () => previewImageIndex.value >= 0 && previewImageIndex.value < previewImageItems.value.length - 1
)
const API_BASE = import.meta.env.VITE_API_BASE || ''
const RESUMABLE_UPLOAD_THRESHOLD = 64 * 1024 * 1024
const RESUMABLE_UPLOAD_CHUNK_SIZE = 8 * 1024 * 1024
type UploadSessionPart = {
  partNumber: number
  size: number
  etag: string
  checksumSha256: string
  updatedAt: string
}
type UploadSessionResponse = {
  id: string
  scope: string
  path: string
  shareId?: string
  size: number
  chunkSize: number
  fileName: string
  contentType?: string
  lastModified?: number
  status: string
  createdAt: string
  updatedAt: string
  expiresAt: string
  parts: UploadSessionPart[]
}
type UploadSessionPartResponse = {
  session: UploadSessionResponse
  part: UploadSessionPart
}
const TEXT_EXTENSIONS = new Set([
  'txt', 'md', 'markdown', 'json', 'jsonl', 'yaml', 'yml', 'toml', 'ini', 'conf', 'cfg', 'env',
  'log', 'csv', 'tsv', 'xml', 'html', 'htm', 'css', 'scss', 'less',
  'js', 'jsx', 'ts', 'tsx', 'mjs', 'cjs',
  'py', 'go', 'rs', 'java', 'kt', 'swift', 'c', 'cc', 'cpp', 'h', 'hpp',
  'sh', 'bash', 'zsh', 'sql', 'graphql', 'gql', 'mdx', 'vue'
])
const TEXT_FILENAMES = new Set([
  'readme', 'readme.md', 'license', 'license.txt', 'makefile', 'dockerfile', '.gitignore', '.env'
])
const PDF_EXTENSIONS = new Set(['pdf'])
const WORD_EXTENSIONS = new Set(['docx'])
const IMAGE_EXTENSIONS = new Set(['png', 'jpg', 'jpeg', 'gif', 'webp', 'bmp', 'svg', 'avif', 'ico'])
const AUDIO_EXTENSIONS = new Set(['mp3', 'wav', 'ogg', 'm4a', 'aac', 'flac', 'opus', 'weba', 'oga'])
const VIDEO_EXTENSIONS = new Set(['mp4', 'webm', 'ogv', 'mov', 'm4v', 'mkv'])
const PREVIEW_FORMAT_OPTIONS: PreviewFormatOption[] = [
  { mode: 'text', label: '文本', extensions: 'TXT、MD、JSON、YAML 等' },
  { mode: 'pdf', label: 'PDF', extensions: 'PDF 文档' },
  { mode: 'word', label: 'Word', extensions: 'DOCX 文档' },
  { mode: 'image', label: '图片', extensions: 'PNG、JPG、WEBP 等' },
  { mode: 'audio', label: '音频', extensions: 'MP3、WAV、OGG 等' },
  { mode: 'video', label: '视频', extensions: 'MP4、WEBM、MOV 等' }
]

function getFileExtension(name: string): string {
  if (!name) return ''
  const trimmed = name.toLowerCase()
  const parts = trimmed.split('.')
  if (parts.length <= 1) return ''
  return parts.pop() || ''
}

function isTextFile(item?: FileItem | null): boolean {
  if (!item || item.isDir) return false
  const name = (item.name || '').toLowerCase()
  if (TEXT_FILENAMES.has(name)) return true
  const ext = getFileExtension(name)
  return Boolean(ext && TEXT_EXTENSIONS.has(ext))
}

function getPreviewMode(item?: FileItem | null): PreviewMode | null {
  if (!item || item.isDir) return null
  if (isTextFile(item)) return 'text'
  const ext = getFileExtension(item.name || '')
  if (ext && PDF_EXTENSIONS.has(ext)) return 'pdf'
  if (ext && WORD_EXTENSIONS.has(ext)) return 'word'
  if (ext && IMAGE_EXTENSIONS.has(ext)) return 'image'
  if (ext && AUDIO_EXTENSIONS.has(ext)) return 'audio'
  if (ext && VIDEO_EXTENSIONS.has(ext)) return 'video'
  return null
}

function mimeTypeForPreviewMode(mode: PreviewMode): string {
  switch (mode) {
    case 'text':
      return 'text/plain;charset=utf-8'
    case 'pdf':
      return 'application/pdf'
    case 'word':
      return 'application/vnd.openxmlformats-officedocument.wordprocessingml.document'
    case 'image':
      return 'image/png'
    case 'audio':
      return 'audio/mpeg'
    case 'video':
      return 'video/mp4'
  }
}

function openPreviewFormatDialog(item: FileItem) {
  previewFormatTarget.value = item
  previewFormatSelection.value = null
  previewFormatDialogVisible.value = true
}

function closePreviewFormatDialog() {
  previewFormatDialogVisible.value = false
  previewFormatTarget.value = null
  previewFormatSelection.value = null
}

function confirmPreviewFormat() {
  const target = previewFormatTarget.value
  const mode = previewFormatSelection.value
  if (!target || !mode) {
    showInfo('请选择打开格式')
    return
  }
  closePreviewFormatDialog()
  void openFilePreview(target, mode)
}

function isImagePreviewItem(item?: FileItem | null): boolean {
  return getPreviewMode(item) === 'image'
}

function encodePath(path: string): string {
  if (!path) return '/'
  const hasTrailing = path.endsWith('/') && path.length > 1
  const trimmed = path.replace(/^\/+/, '').replace(/\/+$/, '')
  if (!trimmed) return '/'
  const encoded = trimmed.split('/').map(encodeURIComponent).join('/')
  return '/' + encoded + (hasTrailing ? '/' : '')
}

function normalizeDavPrefix(prefix: string): string {
  let normalized = (prefix || '').trim()
  if (!normalized) {
    return '/dav'
  }
  if (!normalized.startsWith('/')) {
    normalized = '/' + normalized
  }
  if (normalized.length > 1) {
    normalized = normalized.replace(/\/+$/, '')
    if (normalized === '') return '/'
  }
  return normalized
}

function stripUrlToPath(raw: string): string {
  if (!raw) return '/'
  if (raw.startsWith('http://') || raw.startsWith('https://')) {
    try {
      return new URL(raw).pathname || '/'
    } catch {
      return raw
    }
  }
  return raw
}

function ensureDavPrefixedPath(path: string): string {
  const cleanedInput = stripUrlToPath(path)
  const encodedPath = encodePath(cleanedInput)

  if (DAV_PREFIX === '/') return encodedPath
  if (encodedPath === '/') return DAV_PREFIX + '/'
  if (encodedPath === DAV_PREFIX) return DAV_PREFIX + '/'
  if (encodedPath.startsWith(DAV_PREFIX + '/')) return encodedPath
  return DAV_PREFIX + encodedPath
}

function buildDavPath(path: string): string {
  return ensureDavPrefixedPath(path)
}

function buildApiPath(path: string): string {
  const rawBase = String(API_BASE || '').trim().replace(/\/+$/, '')
  const normalized = path.startsWith('/') ? path : `/${path}`
  return `${rawBase}${normalized}`
}

function buildShareDavPath(shareID: string, path: string = ''): string {
  const relative = normalizeShareRelative(path)
  const suffix = relative ? `/${relative}` : '/'
  return buildDavPath(`/share/${shareID}${suffix}`)
}

function buildAbsoluteDavURL(path: string): string {
  const rawBase = String(API_BASE || '').trim()
  const normalizedBase = rawBase ? rawBase.replace(/\/+$/, '') : ''
  if (typeof window === 'undefined') {
    return `${normalizedBase}${buildDavPath(path)}`
  }
  try {
    const resolvedBase = normalizedBase
      ? new URL(normalizedBase, window.location.origin).toString().replace(/\/+$/, '')
      : window.location.origin.replace(/\/+$/, '')
    return `${resolvedBase}${buildDavPath(path)}`
  } catch {
    return `${window.location.origin.replace(/\/+$/, '')}${buildDavPath(path)}`
  }
}

function normalizeEncryptedRootPath(path: string): string {
  const raw = stripUrlToPath(path || '/').trim().replace(/\\/g, '/')
  if (!raw || raw === '/') return '/'
  const withLeadingSlash = raw.startsWith('/') ? raw : '/' + raw
  return withLeadingSlash.replace(/\/{2,}/g, '/').replace(/\/+$/, '') || '/'
}

function normalizeCipherSuite(value: unknown): string {
  const suite = String(value || '').trim()
  return suite || DEFAULT_ENCRYPTED_DIRECTORY_CIPHER_SUITE
}

function normalizeEncryptedDirectoryMetadata(value: unknown): EncryptedDirectoryMetadata {
  const payload = value && typeof value === 'object' ? value as Partial<EncryptedDirectoryMetadata> : {}
  return {
    version: 1,
    encrypted: true,
    cipherSuite: normalizeCipherSuite(payload.cipherSuite),
    passwordSource: normalizeEncryptedDirectoryPasswordSource(payload.passwordSource),
    createdAt: typeof payload.createdAt === 'string' && payload.createdAt ? payload.createdAt : new Date(0).toISOString()
  }
}

function registerEncryptedDirectoryRoot(path: string, metadata?: EncryptedDirectoryMetadata) {
  const normalized = normalizeEncryptedRootPath(path)
  if (!encryptedDirectoryRoots.value.includes(normalized)) {
    encryptedDirectoryRoots.value = [...encryptedDirectoryRoots.value, normalized]
  }
  if (metadata) {
    encryptedDirectoryMetadata.value = {
      ...encryptedDirectoryMetadata.value,
      [normalized]: normalizeEncryptedDirectoryMetadata(metadata)
    }
  }
}

function unregisterEncryptedDirectoryRootsForDeletedPath(path: string) {
  const before = encryptedDirectoryRoots.value.map(item => normalizeEncryptedRootPath(item))
  const after = removeEncryptedRootsForDeletedPath(before, path)
  if (after.length === before.length && after.every((item, index) => item === before[index])) {
    return
  }
  const afterSet = new Set(after)
  encryptedDirectoryRoots.value = after
  const nextMetadata: Record<string, EncryptedDirectoryMetadata> = {}
  for (const [root, metadata] of Object.entries(encryptedDirectoryMetadata.value)) {
    const normalizedRoot = normalizeEncryptedRootPath(root)
    if (afterSet.has(normalizedRoot)) {
      nextMetadata[normalizedRoot] = metadata
    } else {
      setEncryptedDirectoryPassword(normalizedRoot, '')
    }
  }
  for (const root of before) {
    if (!afterSet.has(root)) {
      setEncryptedDirectoryPassword(root, '')
    }
  }
  encryptedDirectoryMetadata.value = nextMetadata
}

function unregisterExactEncryptedDirectoryRoot(path: string) {
  const normalized = normalizeEncryptedRootPath(path)
  if (!encryptedDirectoryRoots.value.includes(normalized) && !encryptedDirectoryMetadata.value[normalized]) {
    setEncryptedDirectoryPassword(normalized, '')
    return
  }
  encryptedDirectoryRoots.value = encryptedDirectoryRoots.value.filter(root => normalizeEncryptedRootPath(root) !== normalized)
  const { [normalized]: _removed, ...rest } = encryptedDirectoryMetadata.value
  encryptedDirectoryMetadata.value = rest
  setEncryptedDirectoryPassword(normalized, '')
}

function resolveEncryptedRootForPath(path: string): string | null {
  return resolveEncryptedRoot(normalizeEncryptedRootPath(path), encryptedDirectoryRoots.value)
}

function getEncryptedDirectoryCipherSuite(rootPath: string | null | undefined): string {
  if (!rootPath) return DEFAULT_ENCRYPTED_DIRECTORY_CIPHER_SUITE
  const normalizedRoot = normalizeEncryptedRootPath(rootPath)
  return normalizeCipherSuite(encryptedDirectoryMetadata.value[normalizedRoot]?.cipherSuite)
}

function getEncryptedDirectoryPasswordSource(rootPath: string | null | undefined): EncryptedDirectoryPasswordSource {
  if (!rootPath) return DEFAULT_ENCRYPTED_DIRECTORY_PASSWORD_SOURCE
  const normalizedRoot = normalizeEncryptedRootPath(rootPath)
  const metadata = encryptedDirectoryMetadata.value[normalizedRoot]
  if (!metadata) return DEFAULT_ENCRYPTED_DIRECTORY_PASSWORD_SOURCE
  return normalizeEncryptedDirectoryPasswordSource(metadata.passwordSource)
}

function requiresEncryptedDirectoryExtraPassword(rootPath: string | null | undefined): boolean {
  return getEncryptedDirectoryPasswordSource(rootPath) !== 'wallet'
}

function getEncryptedDirectoryProtectionLabelForRoot(rootPath: string | null | undefined): string {
  const passwordSource = getEncryptedDirectoryPasswordSource(rootPath)
  if (passwordSource === 'wallet') return '钱包密钥'
  if (passwordSource === 'wallet+password') return '钱包密钥 + 额外密码'
  return '目录密码'
}

async function buildEncryptedDirectoryCryptoOptions(rootPath: string, suite?: string) {
  const normalizedRoot = normalizeEncryptedRootPath(rootPath)
  const passwordSource = getEncryptedDirectoryPasswordSource(normalizedRoot)
  const password = passwordSource === 'wallet' ? '' : await ensureEncryptedDirectoryPassword(normalizedRoot)
  return {
    password,
    passwordSource,
    passwordContext: buildEncryptedDirectoryPasswordContext(normalizedRoot),
    suite: suite || getEncryptedDirectoryCipherSuite(normalizedRoot)
  }
}

async function fetchEncryptedDirectoryMetadata(rootPath: string): Promise<EncryptedDirectoryMetadata | null> {
  const normalizedRoot = normalizeEncryptedRootPath(rootPath)
  const cached = encryptedDirectoryMetadata.value[normalizedRoot]
  if (cached) return cached

  const token = localStorage.getItem('authToken') || ''
  const response = await fetch(buildDavPath(buildEncryptedDirectoryMetadataPath(normalizedRoot)), {
    method: 'GET',
    headers: {
      'Authorization': `Bearer ${token}`,
      'Accept': 'application/json'
    }
  })
  if (!response.ok) return null

  try {
    const metadata = normalizeEncryptedDirectoryMetadata(await response.json())
    registerEncryptedDirectoryRoot(normalizedRoot, metadata)
    return metadata
  } catch (error) {
    console.warn('解析加密目录元数据失败:', normalizedRoot, error)
    registerEncryptedDirectoryRoot(normalizedRoot)
    return null
  }
}

async function loadCipherSuiteOptions(): Promise<void> {
  try {
    const suites = await getSupportedCipherSuites()
    const supportedSymmetricSuiteNames = new Set((suites || [])
      .filter((suite: CipherSuiteInfo) => suite?.mode === 'symmetric' && typeof suite.name === 'string' && suite.name.trim())
      .map((suite: CipherSuiteInfo) => suite.name.trim()))
    const simplifiedSuites: CipherSuiteOption[] = [
      {
        name: DEFAULT_ENCRYPTED_DIRECTORY_CIPHER_SUITE,
        description: '国际默认',
        mode: 'symmetric'
      }
    ]
    if (supportedSymmetricSuiteNames.has('sm4-cbc-hmac-sm3')) {
      simplifiedSuites.push({
        name: 'sm4-cbc-hmac-sm3',
        description: '国密对称',
        mode: 'symmetric'
      })
    }
    cipherSuiteOptions.value = simplifiedSuites
  } catch (error) {
    console.warn('获取钱包加密算法列表失败，使用默认算法:', error)
  }
}

function isEncryptedItem(item: FileItem | null | undefined): boolean {
  if (!item) return false
  return !!item.encryptedRoot || !!item.encrypted
}

function canShareFileItem(item: FileItem): boolean {
  return !isEncryptedItem(item)
}

function getEffectiveEncryptedRoot(path: string): string | null {
  return resolveEncryptedRootForPath(path)
}

function isEncryptedRootDirectory(item: FileItem | null | undefined): boolean {
  if (!item?.isDir) return false
  const itemPath = normalizeEncryptedRootPath(item.path)
  const root = getEffectiveEncryptedRoot(itemPath)
  return !!root && root === itemPath
}

function canTransferAcrossEncryptedBoundary(source: FileItem | null, targetPath: string): { allowed: boolean; message?: string } {
  if (!source) return { allowed: false, message: '无效的移动对象' }
  const sourceRoot = source.encryptedRoot ? normalizeEncryptedRootPath(source.encryptedRoot) : null
  const targetRoot = getEffectiveEncryptedRoot(targetPath)
  if (sourceRoot === targetRoot) {
    return { allowed: true }
  }
  if (!sourceRoot && targetRoot) {
    return { allowed: false, message: '暂不支持将普通文件或目录直接移动到加密目录' }
  }
  if (sourceRoot && !targetRoot) {
    return { allowed: false, message: '暂不支持将加密目录中的文件或目录移出到普通目录' }
  }
  return { allowed: false, message: '暂不支持在不同加密目录之间移动文件或目录' }
}

async function ensureEncryptedDirectoryPassword(rootPath: string): Promise<string> {
  const normalizedRoot = normalizeEncryptedRootPath(rootPath)
  const cached = getEncryptedDirectoryPassword(normalizedRoot)
  if (cached) return cached
  const passwordSource = getEncryptedDirectoryPasswordSource(normalizedRoot)
  if (passwordSource === 'wallet') return ''

  const label = passwordSource === 'wallet+password' ? '额外密码' : '目录密码'

  const { value } = await ElMessageBox.prompt(
    `请输入加密目录 ${normalizedRoot} 的${label}`,
    '解锁加密目录',
    {
      confirmButtonText: '确认',
      cancelButtonText: '取消',
      inputType: 'password',
      inputPlaceholder: label,
      inputPattern: /.+/,
      inputErrorMessage: `请输入${label}`
    }
  )
  const password = String(value || '')
  setEncryptedDirectoryPassword(normalizedRoot, password)
  return password
}

async function promptEncryptedDirectoryPassword(rootPath: string, forceReset = false): Promise<string> {
  const normalizedRoot = normalizeEncryptedRootPath(rootPath)
  if (forceReset) {
    setEncryptedDirectoryPassword(normalizedRoot, '')
  }
  return ensureEncryptedDirectoryPassword(normalizedRoot)
}

function isPromptCanceled(error: unknown): boolean {
  return error === 'cancel'
    || error === 'close'
    || (error instanceof Error && /cancel|close/i.test(error.message))
}

async function unlockCurrentEncryptedDirectory(forceReset = false) {
  const root = currentEncryptedRoot.value
  if (!root) return
  if (!requiresEncryptedDirectoryExtraPassword(root)) {
    showInfo('当前加密目录使用钱包密钥，无需额外解锁')
    return
  }
  try {
    await promptEncryptedDirectoryPassword(root, forceReset)
    showSuccess(forceReset ? '密码已重新输入' : '加密目录已解锁')
  } catch (error) {
    if (isPromptCanceled(error)) return
    throw error
  }
}

async function unlockEncryptedDirectoryByRoot(rootPath: string, forceReset = false) {
  const normalizedRoot = normalizeEncryptedRootPath(rootPath)
  if (!requiresEncryptedDirectoryExtraPassword(normalizedRoot)) {
    showInfo('当前加密目录使用钱包密钥，无需额外解锁')
    return
  }
  try {
    await promptEncryptedDirectoryPassword(normalizedRoot, forceReset)
    showSuccess(forceReset ? '密码已重新输入' : '加密目录已解锁')
  } catch (error) {
    if (isPromptCanceled(error)) return
    throw error
  }
}

function clearCurrentEncryptedDirectoryPassword() {
  const root = currentEncryptedRoot.value
  if (!root) return
  setEncryptedDirectoryPassword(root, '')
  showSuccess('已清除密码缓存')
}

function clearEncryptedDirectoryPasswordByRoot(rootPath: string) {
  const normalizedRoot = normalizeEncryptedRootPath(rootPath)
  setEncryptedDirectoryPassword(normalizedRoot, '')
  showSuccess('已清除密码缓存')
}

function collectAncestorDirectories(path: string): string[] {
  const normalized = normalizeEncryptedRootPath(path)
  if (normalized === '/') return []
  const segments = normalized.split('/').filter(Boolean)
  const directories: string[] = []
  for (let index = segments.length; index >= 1; index -= 1) {
    directories.push(`/${segments.slice(0, index).join('/')}`)
  }
  return directories
}

function getEncryptedDirectoryParentPath(path: string): string {
  const normalized = normalizeEncryptedRootPath(path)
  if (normalized === '/') return '/'
  const segments = normalized.split('/').filter(Boolean)
  segments.pop()
  return segments.length ? `/${segments.join('/')}` : '/'
}

async function discoverEncryptedRootForPath(path: string): Promise<string | null> {
  const knownRoot = resolveEncryptedRootForPath(path)
  if (knownRoot) return knownRoot

  const token = localStorage.getItem('authToken') || ''
  for (const candidate of collectAncestorDirectories(path)) {
    const davPath = buildDavPath(candidate)
    const response = await fetch(davPath, {
      method: 'PROPFIND',
      headers: {
        'Authorization': `Bearer ${token}`,
        'Depth': '1'
      }
    })
    if (!response.ok) continue
    const xml = await response.text()
    const items = parsePropfindResponse(xml, davPath, DAV_PREFIX)
    if (items.some(item => isEncryptedDirectoryMetadataFileName(item.name))) {
      registerEncryptedDirectoryRoot(candidate)
      void fetchEncryptedDirectoryMetadata(candidate)
      return normalizeEncryptedRootPath(candidate)
    }
  }

  return null
}

async function readEncryptedFileBytes(item: FileItem, rootPath: string): Promise<Uint8Array> {
  const token = localStorage.getItem('authToken') || ''
  const response = await fetch(buildDavPath(item.path), {
    method: 'GET',
    headers: {
      'Authorization': `Bearer ${token}`
    }
  })
  if (!response.ok) {
    throw new Error(`读取失败: ${response.status}`)
  }
  const blob = await response.blob()
  const normalizedRoot = normalizeEncryptedRootPath(rootPath)
  const cachedPassword = getEncryptedDirectoryPassword(normalizedRoot)
  if (cachedPassword && requiresEncryptedDirectoryExtraPassword(normalizedRoot)) {
    try {
      return await decryptBlobContent(blob, await buildEncryptedDirectoryCryptoOptions(normalizedRoot))
    } catch {
      setEncryptedDirectoryPassword(normalizedRoot, '')
    }
  }
  return decryptBlobContent(blob, await buildEncryptedDirectoryCryptoOptions(normalizedRoot))
}

function inferFileMimeType(fileName: string, fallbackType = 'application/octet-stream'): string {
  const extension = fileName.split('.').pop()?.toLowerCase() || ''
  return (
    extension === 'txt' || extension === 'md' || extension === 'json'
      ? 'text/plain;charset=utf-8'
      : extension === 'pdf'
        ? 'application/pdf'
        : extension === 'docx'
          ? 'application/vnd.openxmlformats-officedocument.wordprocessingml.document'
          : extension === 'png'
            ? 'image/png'
            : extension === 'jpg' || extension === 'jpeg'
              ? 'image/jpeg'
              : extension === 'gif'
                ? 'image/gif'
                : extension === 'webp'
                  ? 'image/webp'
                  : extension === 'mp3'
                    ? 'audio/mpeg'
                    : extension === 'wav'
                      ? 'audio/wav'
                  : extension === 'mp4'
                        ? 'video/mp4'
                        : fallbackType
  )
}

function createEncryptedDownloadURL(bytes: Uint8Array, fileName: string, fallbackType = 'application/octet-stream'): string {
  const type = inferFileMimeType(fileName, fallbackType)
  return URL.createObjectURL(new Blob([bytes], { type }))
}

function isInternalMoveDrag(event?: DragEvent | null): boolean {
  const types = event?.dataTransfer?.types
  if (!types) return false
  return Array.from(types).some(type => type.startsWith('application/x-warehouse-'))
}

function isExternalFileDrag(event?: DragEvent | null): boolean {
  const types = event?.dataTransfer?.types
  if (!types) return false
  const typeList = Array.from(types)
  return typeList.includes('Files') && !typeList.includes('application/x-warehouse-file-move')
}

function normalizeDirectoryPath(path: string): string {
  let normalized = stripUrlToPath(path || '/').trim()
  if (!normalized.startsWith('/')) {
    normalized = '/' + normalized
  }
  normalized = normalized.replace(/\/{2,}/g, '/')
  if (normalized.length > 1) {
    normalized = normalized.replace(/\/+$/, '')
  }
  if (!normalized) {
    return '/'
  }
  return normalized === '/' ? '/' : `${normalized}/`
}

function normalizeItemPath(item: FileItem): string {
  if (item.isDir) return normalizeDirectoryPath(item.path)
  const raw = stripUrlToPath(item.path || '/').trim()
  if (!raw.startsWith('/')) return '/' + raw
  return raw.replace(/\/{2,}/g, '/')
}

function normalizeSharedItemPath(item: FileItem): string {
  if (item.isDir) return normalizeDirectoryPath(item.path)
  const raw = String(item.path || '/').trim().replace(/\/{2,}/g, '/')
  if (!raw.startsWith('/')) return '/' + raw
  return raw
}

function normalizePathForSpaceMatch(path: string): string {
  const normalized = normalizeDirectoryPath(path)
  if (normalized === '/') return '/'
  return normalized.replace(/\/$/, '')
}

function parseModifiedTimestamp(modified?: string): number {
  const parsed = Date.parse(String(modified || '').trim())
  return Number.isFinite(parsed) ? parsed : 0
}

function compareItemsByModifiedDesc<T extends { name: string; modified?: string }>(a: T, b: T): number {
  const diff = parseModifiedTimestamp(b.modified) - parseModifiedTimestamp(a.modified)
  if (diff !== 0) return diff
  return a.name.localeCompare(b.name, 'zh-Hans-CN', { numeric: true, sensitivity: 'base' })
}

function parseOptionalTimestamp(value?: string): number {
  const parsed = Date.parse(String(value || '').trim())
  return Number.isFinite(parsed) ? parsed : 0
}

function compareItemsByCreatedAtDesc<T extends { name: string; createdAt?: string }>(a: T, b: T): number {
  const diff = parseOptionalTimestamp(b.createdAt) - parseOptionalTimestamp(a.createdAt)
  if (diff !== 0) return diff
  return a.name.localeCompare(b.name, 'zh-Hans-CN', { numeric: true, sensitivity: 'base' })
}

function compareItemsByDeletedAtDesc<T extends { name: string; deletedAt?: string }>(a: T, b: T): number {
  const diff = parseOptionalTimestamp(b.deletedAt) - parseOptionalTimestamp(a.deletedAt)
  if (diff !== 0) return diff
  return a.name.localeCompare(b.name, 'zh-Hans-CN', { numeric: true, sensitivity: 'base' })
}

function normalizeAssetSpaces(spaces: AssetSpace[]): AssetSpace[] {
  const normalized: AssetSpace[] = []
  const seen = new Set<string>()
  for (const item of spaces) {
    const key = String(item?.key || '').trim()
    const canonicalName = ASSET_SPACE_NAME_BY_KEY[key] || ''
    const canonicalPath = ASSET_SPACE_PATH_BY_KEY[key] || ''
    const name = canonicalName || String(item?.name || '').trim()
    const rawPath = String(item?.path || canonicalPath || '').trim()
    const path = normalizeDirectoryPath(rawPath)
    if (!key || !name || path === '/') continue
    if (seen.has(key)) continue
    seen.add(key)
    normalized.push({ key, name, path })
  }
  return normalized
}

function getDefaultAssetSpace(): AssetSpace | null {
  const normalized = normalizeAssetSpaces(assetSpaces.value)
  if (!normalized.length) return null
  const byKey = normalized.find(item => item.key === defaultAssetSpaceKey.value)
  return byKey || normalized[0]
}

function resolveAssetSpaceByPath(path: string): AssetSpace | null {
  const target = normalizePathForSpaceMatch(path)
  for (const item of normalizeAssetSpaces(assetSpaces.value)) {
    const root = normalizePathForSpaceMatch(item.path)
    if (target === root || target.startsWith(`${root}/`)) {
      return item
    }
  }
  return null
}

function isPathInSpace(path: string, spacePath: string): boolean {
  const normalizedPath = normalizePathForSpaceMatch(path)
  const normalizedSpace = normalizePathForSpaceMatch(spacePath)
  if (normalizedSpace === '/') return true
  return normalizedPath === normalizedSpace || normalizedPath.startsWith(`${normalizedSpace}/`)
}

function resolveInitialFilePath(storedPath: string): string {
  const normalizedStored = normalizeDirectoryPath(storedPath || '/')
  if (normalizedStored !== '/') return normalizedStored
  const defaultSpace = getDefaultAssetSpace()
  return defaultSpace ? defaultSpace.path : '/'
}

function ensureAuthCookie(token: string): void {
  if (!token) return
  document.cookie = `authToken=${token}; path=/; max-age=86400`
}

async function confirmAction(message: string, title = '提示'): Promise<boolean> {
  try {
    await ElMessageBox.confirm(message, title, {
      confirmButtonText: '确定',
      cancelButtonText: '取消',
      type: 'warning',
      closeOnClickModal: false
    })
    return true
  } catch {
    return false
  }
}

function showError(message: string, title = '错误'): void {
  void ElMessageBox.alert(message, title, {
    confirmButtonText: '确定',
    type: 'error',
    closeOnClickModal: false
  })
}

function quotaExceededMessage(): string {
  return [
    '当前操作会超过你的存储额度，系统已拒绝写入。',
    '你可以先删除不需要的文件，或清理回收站后重试。',
    '如果仍然需要更多空间，请联系管理员调整额度。'
  ].join('\n')
}

function isQuotaExceededErrorMessage(message: string): boolean {
  const normalized = String(message || '').toLowerCase()
  return normalized.includes('507') ||
    normalized.includes('insufficient storage') ||
    normalized.includes('storage quota exceeded') ||
    normalized.includes('quota exceeded') ||
    normalized.includes('quota')
}

function normalizeUserFacingErrorMessage(message: string, fallback: string): string {
  const trimmed = String(message || '').trim()
  if (isQuotaExceededErrorMessage(trimmed)) {
    return quotaExceededMessage()
  }
  return trimmed || fallback
}

function errorMessageFromUnknown(error: unknown, fallback: string): string {
  if (error instanceof Error) {
    return normalizeUserFacingErrorMessage(error.message, fallback)
  }
  return normalizeUserFacingErrorMessage(String(error || ''), fallback)
}

function createDefaultShareExpiryForm(): ShareExpiryForm {
  return {
    expiresValue: '0',
    expiresUnit: 'hour'
  }
}

function createDefaultShareLinkForm(): ShareLinkForm {
  return {
    ...createDefaultShareExpiryForm(),
    mode: 'download'
  }
}

function normalizeAccessKeyRootPath(rawPath: string): string {
  const normalized = String(rawPath || '').trim().replace(/\\/g, '/').replace(/\/+/g, '/')
  if (!normalized) return '/'
  const withLeading = normalized.startsWith('/') ? normalized : `/${normalized}`
  const withoutTrailing = withLeading.length > 1 ? withLeading.replace(/\/+$/, '') : withLeading
  return withoutTrailing || '/'
}

function createDefaultAccessKeyForm(rootPath = '/'): AccessKeyForm {
  return {
    name: '',
    rootPath: normalizeAccessKeyRootPath(rootPath),
    permissions: ['read'],
    ...createDefaultShareExpiryForm()
  }
}

function normalizeAccessKeyBindings(paths: string[] | undefined): string[] {
  if (!Array.isArray(paths)) return []
  return Array.from(new Set(paths.map(item => normalizeAccessKeyRootPath(item)).filter(Boolean)))
}

function isAccessKeyBoundToScope(item: WebDAVAccessKeyItem, scopePath: string): boolean {
  const scope = normalizeAccessKeyRootPath(scopePath)
  const bindings = normalizeAccessKeyBindings(item.bindingPaths)
  return bindings.includes(scope)
}

function parseShareExpiryValue(raw: string): number | null {
  const trimmed = raw.trim()
  if (!/^\d+$/.test(trimmed)) return null
  const value = Number.parseInt(trimmed, 10)
  return Number.isSafeInteger(value) ? value : null
}

function resolveShareExpiryPayload(form: ShareExpiryForm): { expiresValue: number; expiresUnit: ShareExpiryUnit } | null {
  const expiresValue = parseShareExpiryValue(form.expiresValue)
  if (expiresValue === null) {
    showError('有效期请输入非负整数')
    return null
  }
  return {
    expiresValue,
    expiresUnit: form.expiresUnit
  }
}

async function fetchAccessKeys(withLoading = false) {
  if (withLoading) {
    accessKeyLoading.value = true
  }
  try {
    const data = await webdavAccessKeyApi.list()
    accessKeys.value = Array.isArray(data.items)
      ? data.items.map(item => ({
        ...item,
        bindingPaths: normalizeAccessKeyBindings(item.bindingPaths)
      }))
      : []
  } catch (error) {
    console.error('获取访问密钥失败:', error)
    if (withLoading) {
      showError('获取访问密钥失败')
    }
  } finally {
    if (withLoading) {
      accessKeyLoading.value = false
    }
  }
}

async function fetchS3Credentials(withLoading = false) {
  if (withLoading) s3CredentialLoading.value = true
  try {
    const data = await s3CredentialApi.list()
    s3Credentials.value = Array.isArray(data.items) ? data.items : []
  } catch (error) {
    console.error('获取 S3 凭证失败:', error)
    if (withLoading) showError('获取 S3 凭证失败')
  } finally {
    if (withLoading) s3CredentialLoading.value = false
  }
}

async function fetchToolCredentials(withLoading = false) {
  if (withLoading) toolCredentialLoading.value = true
  try {
    const data = await warehouseToolCredentialApi.list()
    toolCredentials.value = Array.isArray(data.items) ? data.items : []
  } catch (error) {
    console.error('获取 Warehouse Tool 凭证失败:', error)
    if (withLoading) showError('获取 Warehouse Tool 凭证失败')
  } finally {
    if (withLoading) toolCredentialLoading.value = false
  }
}

async function fetchToolAudits() {
  toolAuditLoading.value = true
  try {
    const data = await warehouseToolCredentialApi.audits()
    toolAudits.value = Array.isArray(data.items) ? data.items : []
  } catch (error) {
    console.error('获取 Tool 审计失败:', error)
    showError('获取 Tool 审计失败')
  } finally {
    toolAuditLoading.value = false
  }
}

function openToolCredentialDialog() {
  toolCredentialName.value = ''
  toolCredentialPath.value = ''
  toolCredentialScopes.value = ['asset:read', 'asset:write']
  toolCredentialExpiresAt.value = ''
  toolCredentialCreateResult.value = null
  toolCredentialDialogVisible.value = true
}

async function submitToolCredential() {
  const name = toolCredentialName.value.trim()
  const path = normalizeAccessKeyRootPath(toolCredentialPath.value)
  if (!name || path === '/' || !toolCredentialExpiresAt.value || !toolCredentialScopes.value.length) {
    showError('请填写名称、选择授权目录、至少一项权限和过期时间')
    return
  }
  toolCredentialSubmitting.value = true
  try {
    toolCredentialCreateResult.value = await warehouseToolCredentialApi.create({
      name,
      scopes: [...toolCredentialScopes.value],
      pathPrefixes: [path],
      expiresAt: new Date(toolCredentialExpiresAt.value).toISOString()
    })
    await fetchToolCredentials()
    showSuccess('Tool 凭证已创建，请立即保存 Secret')
  } catch (error: any) { showError(error?.message || '创建 Tool 凭证失败') } finally { toolCredentialSubmitting.value = false }
}

async function rotateToolCredential(item: WarehouseToolCredentialItem) {
  if (!(await confirmAction(`确定轮换凭证 ${item.name} 吗？旧 Secret 将立即失效。`, '轮换 Tool 凭证'))) return
  try {
    toolCredentialCreateResult.value = await warehouseToolCredentialApi.rotate(item.id)
    toolCredentialDialogVisible.value = true
    await fetchToolCredentials()
    showSuccess('Tool 凭证已轮换，请立即保存新 Secret')
  } catch (error: any) { showError(error?.message || '轮换 Tool 凭证失败') }
}

async function revokeToolCredential(item: WarehouseToolCredentialItem) {
  if (item.status !== 'active' || !(await confirmAction(`确定撤销凭证 ${item.name} 吗？`, '撤销 Tool 凭证'))) return
  try { await warehouseToolCredentialApi.revoke(item.id); await fetchToolCredentials(); showSuccess('Tool 凭证已撤销') } catch (error: any) { showError(error?.message || '撤销 Tool 凭证失败') }
}

function openS3CredentialDialog() {
  s3CredentialName.value = ''
  s3CredentialBucket.value = 'personal'
  s3CredentialDirectory.value = ''
  s3CredentialCreateResult.value = null
  s3CredentialDialogVisible.value = true
}

function normalizeS3CredentialDirectory(value: string): string {
  return String(value || '')
    .trim()
    .replace(/\\/g, '/')
    .replace(/\/+/g, '/')
    .replace(/^\/+/, '')
    .replace(/\/+$/, '')
}

function hasInvalidS3CredentialDirectory(value: string): boolean {
  return value.split('/').some(segment => segment === '.' || segment === '..')
}

function buildS3CredentialRootPath(): string | null {
  const directory = normalizeS3CredentialDirectory(s3CredentialDirectory.value)
  if (hasInvalidS3CredentialDirectory(directory)) {
    showError('bucket 下目录不能包含 . 或 ..')
    return null
  }
  return directory ? `/${s3CredentialBucket.value}/${directory}` : `/${s3CredentialBucket.value}`
}

async function submitS3Credential() {
  const name = s3CredentialName.value.trim()
  if (!name) {
    showError('请输入 S3 凭证名称')
    return
  }
  const rootPath = buildS3CredentialRootPath()
  if (!rootPath) {
    return
  }
  if (s3Credentials.value.some(item => item.name === name)) {
    showError('凭证名称已存在，请更换名称')
    return
  }
  s3CredentialSubmitting.value = true
  try {
    s3CredentialCreateResult.value = await s3CredentialApi.create({
      name,
      rootPath,
      permissions: ['read', 'create', 'update', 'delete']
    })
    await fetchS3Credentials()
    showSuccess('S3 凭证已创建，请立即保存 Secret')
  } catch (error: any) {
    console.error('创建 S3 凭证失败:', error)
    showError(error?.message || '创建 S3 凭证失败')
  } finally {
    s3CredentialSubmitting.value = false
  }
}

async function revokeS3Credential(item: S3CredentialItem) {
  if (item.status !== 'active') return
  if (!(await confirmAction(`确定撤销 S3 凭证 ${item.name} 吗？`, '撤销 S3 凭证'))) return
  try {
    await s3CredentialApi.revoke(item.id)
    await fetchS3Credentials()
    showSuccess('S3 凭证已撤销')
  } catch (error: any) {
    showError(error?.message || '撤销 S3 凭证失败')
  }
}

async function deleteS3Credential(item: S3CredentialItem) {
  if (item.status !== 'revoked') return
  if (!(await confirmAction(`确定删除已撤销的 S3 凭证 ${item.name} 吗？`, '删除 S3 凭证'))) return
  try {
    await s3CredentialApi.remove(item.id)
    await fetchS3Credentials()
    showSuccess('S3 凭证已删除')
  } catch (error: any) {
    showError(error?.message || '删除 S3 凭证失败')
  }
}

async function handleWalletLogin() {
  if (walletLoginSubmitting.value) {
    await focusPendingWalletApproval()
    return
  }
  walletLoginSubmitting.value = true
  try {
    await loginWithWallet()
    window.location.reload()
  } catch (error: any) {
    showError(error?.message || '钱包登录失败')
  } finally {
    walletLoginSubmitting.value = false
  }
}

function clearIdentityTimer() {
  if (identityTimer !== null) {
    window.clearInterval(identityTimer)
    identityTimer = null
  }
}

async function refreshIdentityLogin() {
  identityRequestSeq += 1
  const requestSeq = identityRequestSeq
  clearIdentityTimer()
  identityLoading.value = true
  identityPolling.value = false
  identitySessionId.value = ''
  identityQrcodeUrl.value = ''
  identityQrcodeImage.value = ''
  identityStatusText.value = ''
  try {
    const session = await createIdentityLoginSession()
    if (requestSeq !== identityRequestSeq) return
    identitySessionId.value = session.sessionId
    identityQrcodeUrl.value = session.qrcodeUrl
    identityPollInterval.value = Math.max(1, session.pollInterval || 2)
    identityQrcodeImage.value = await QRCode.toDataURL(session.qrcodeUrl, {
      width: 220,
      margin: 2,
      color: {
        dark: '#162033',
        light: '#ffffff'
      }
    })
    startIdentityPolling()
  } catch (error: any) {
    if (requestSeq !== identityRequestSeq) return
    identityStatusText.value = error?.message || '通行证登录暂不可用'
  } finally {
    if (requestSeq === identityRequestSeq) {
      identityLoading.value = false
    }
  }
}

function startIdentityPolling() {
  clearIdentityTimer()
  if (!identitySessionId.value) return
  void checkIdentityStatus()
  identityTimer = window.setInterval(() => {
    void checkIdentityStatus()
  }, identityPollInterval.value * 1000)
}

async function checkIdentityStatus() {
  if (!identitySessionId.value || identityPolling.value) return
  identityPolling.value = true
  const sessionId = identitySessionId.value
  try {
    const status = await pollIdentityLoginStatus(sessionId)
    if (sessionId !== identitySessionId.value) return
    if (status.token || status.status === 'approved') {
      clearIdentityTimer()
      identityStatusText.value = '登录成功，正在进入资产仓库'
      window.location.reload()
      return
    }
    if (status.status === 'scanned') {
      identityStatusText.value = '已扫码，请在手机上确认登录'
    } else if (status.status === 'rejected') {
      identityStatusText.value = '已拒绝登录，请刷新二维码'
      clearIdentityTimer()
    } else if (status.status === 'expired') {
      identityStatusText.value = '二维码已过期，请刷新'
      clearIdentityTimer()
    } else {
      identityStatusText.value = status.message || ''
    }
  } catch (error: any) {
    if (error?.code === 'expired') {
      identityStatusText.value = '二维码已过期，请刷新'
      clearIdentityTimer()
    } else {
      identityStatusText.value = error?.message || '通行证登录状态异常'
    }
  } finally {
    identityPolling.value = false
  }
}

function switchLoginMode(mode: LoginMode) {
  loginMode.value = mode
  if (mode !== 'identity') {
    clearIdentityTimer()
  }
  if (mode === 'identity' && !identitySessionId.value && !identityLoading.value) {
    void refreshIdentityLogin()
  }
}

function openIdentityAuthorize() {
  if (!identityQrcodeUrl.value) return
  window.open(identityQrcodeUrl.value, '_blank', 'noopener,noreferrer')
}

function handleIdentityCallbackEvent(payload: unknown) {
  let data = payload as any
  if (typeof payload === 'string') {
    try {
      data = JSON.parse(payload)
    } catch {
      return
    }
  }
  if (data?.action !== 'warehouse-identity-callback') return
  if (!identitySessionId.value || data?.sessionId !== identitySessionId.value) return
  identityStatusText.value = '已确认，正在完成登录'
  void checkIdentityStatus()
}

function handleIdentityStorage(event: StorageEvent) {
  if (event.key !== '__warehouse_identity_callback__' || !event.newValue) return
  handleIdentityCallbackEvent(event.newValue)
}

function handleIdentityMessage(event: MessageEvent) {
  if (event.origin !== window.location.origin) return
  handleIdentityCallbackEvent(event.data)
}

// 获取文件列表 (WebDAV PROPFIND)
async function fetchFiles(path: string = '/') {
  loading.value = true
  const apiPath = buildDavPath(path)
  console.log('fetchFiles:', path, '→', apiPath)
  try {
    const token = localStorage.getItem('authToken') || ''
    const response = await fetch(apiPath, {
      method: 'PROPFIND',
      headers: {
        'Authorization': `Bearer ${token}`,
        'Content-Type': 'application/xml',
        'Depth': '1'
      }
    })

    console.log('PROPFIND response:', response.status, response.statusText)

    if (!response.ok) {
      throw new Error('获取文件列表失败')
    }

    const text = await response.text()
    console.log('PROPFIND response text length:', text.length)

    // 先更新 currentPath，再解析
    currentPath.value = path
    localStorage.setItem(FILE_PATH_STORAGE_KEY, currentPath.value)
    const parsedItems = parsePropfindResponse(text, currentPath.value, DAV_PREFIX)
    const currentDirRoot = normalizeEncryptedRootPath(currentPath.value)
    const hasEncryptionMarker = parsedItems.some(item => isEncryptedDirectoryMetadataFileName(item.name))
    if (hasEncryptionMarker) {
      registerEncryptedDirectoryRoot(currentDirRoot)
      void fetchEncryptedDirectoryMetadata(currentDirRoot)
    }
    const encryptedRoot = hasEncryptionMarker
      ? currentDirRoot
      : (await discoverEncryptedRootForPath(getEncryptedDirectoryParentPath(currentDirRoot)))
    fileList.value = parsedItems
      .filter(item => !isEncryptedDirectoryMetadataFileName(item.name))
      .map(item => {
        const itemEncryptedRoot = resolveEncryptedRootForPath(item.path) || encryptedRoot
        return {
          ...item,
          encrypted: !!itemEncryptedRoot,
          encryptedRoot: itemEncryptedRoot || undefined
        }
      })
    console.log('parsed items:', fileList.value)
  } catch (error) {
    console.error('获取文件列表失败:', error)
  } finally {
    loading.value = false
  }
}

// 获取配额
async function fetchQuota(withLoading = false) {
  if (withLoading) {
    quotaManageLoading.value = true
  }
  try {
    const data = await quotaApi.get()
    quota.value = data
  } catch (error) {
    console.error('获取配额失败:', error)
  } finally {
    if (withLoading) {
      quotaManageLoading.value = false
    }
  }
}

// 获取用户信息
async function fetchUserInfo() {
  try {
    const data = await userApi.getInfo()
    userInfo.value = data
    if (data.username) {
      localStorage.setItem('username', data.username)
    }
    if (data.wallet_address) {
      localStorage.setItem('walletAddress', data.wallet_address)
    } else {
      localStorage.removeItem('walletAddress')
    }
    if (Array.isArray(data.permissions)) {
      localStorage.setItem('permissions', JSON.stringify(data.permissions))
    }
    canManageUsers.value = Boolean(data.capabilities?.manageUsers)
    if (!canManageUsers.value && managementSection.value === 'adminUsers') {
      managementSection.value = 'account'
      persistManagementSection('account')
    }
    if (data.created_at) {
      localStorage.setItem('createdAt', data.created_at)
    }
  } catch (error) {
    console.error('获取用户信息失败:', error)
  }
}

async function fetchAssetSpaces() {
  assetSpaceLoading.value = true
  try {
    const data = await assetsApi.getSpaces()
    const spaces = normalizeAssetSpaces(data.spaces || [])
    if (spaces.length) {
      assetSpaces.value = spaces
      const hasDefault = spaces.some(item => item.key === data.defaultSpace)
      defaultAssetSpaceKey.value = hasDefault ? data.defaultSpace : spaces[0].key
      return
    }
  } catch (error) {
    console.error('获取资产空间失败:', error)
  } finally {
    assetSpaceLoading.value = false
  }

  const fallback = normalizeAssetSpaces(DEFAULT_ASSET_SPACES)
  assetSpaces.value = fallback
  defaultAssetSpaceKey.value = fallback[0]?.key || 'personal'
}

async function fetchUserCenter() {
  quotaManageLoading.value = true
  try {
    await fetchUserInfo()
    const tasks: Array<Promise<unknown>> = [fetchQuota(), fetchAccessKeys(), fetchS3Credentials()]
    if (managementSection.value === 'adminUsers' && canManageUsers.value) {
      tasks.push(fetchAdminUsers())
    }
    await Promise.all(tasks)
  } finally {
    quotaManageLoading.value = false
  }
}

function extractApiErrorMessage(error: unknown): string {
  if (error instanceof Error) {
    const message = error.message?.trim()
    if (!message) return ''
    try {
      const parsed = JSON.parse(message) as { error?: string; code?: number }
      if (parsed?.error) return parsed.error
      if (parsed?.code) return `HTTP ${parsed.code}`
    } catch {
      return message
    }
    return message
  }
  return String(error || '')
}

function isAdminPermissionError(error: unknown): boolean {
  const message = extractApiErrorMessage(error).toLowerCase()
  return message.includes('403') || message.includes('admin permission required') || message.includes('permission denied') || message.includes('forbidden')
}

async function fetchAdminUsers() {
  if (!canManageUsers.value) return
  adminUsersLoading.value = true
  adminUsersError.value = ''
  try {
    const data = await adminUserApi.list()
    adminUsers.value = Array.isArray(data.items) ? data.items : []
    canManageUsers.value = true
  } catch (error) {
    adminUsers.value = []
    if (isAdminPermissionError(error)) {
      canManageUsers.value = false
      return
    }
    canManageUsers.value = true
    adminUsersError.value = extractApiErrorMessage(error) || '加载失败'
    console.error('获取用户管理列表失败:', error)
  } finally {
    adminUsersLoading.value = false
  }
}

function openAccessKeyDialog(scopePath = '/', lockScope = false) {
  const normalizedScope = normalizeAccessKeyRootPath(scopePath)
  accessKeyScopePath.value = normalizedScope
  accessKeyScopeLocked.value = lockScope
  accessKeyBindTargetID.value = ''
  accessKeyCreateResult.value = null
  accessKeyForm.value = createDefaultAccessKeyForm(normalizedScope)
  accessKeyDialogVisible.value = true
  void fetchAccessKeys(true)
}

function openAccessKeyDialogFromDirectory(item: FileItem) {
  if (!item.isDir) return
  openAccessKeyDialog(item.path, true)
}

function openAccessKeyDialogFromUserCenter() {
  const current = normalizeAccessKeyRootPath(currentPath.value)
  const defaultScope = resolveAssetSpaceByPath(current)?.path || getDefaultAssetSpace()?.path || '/personal'
  openAccessKeyDialog(current !== '/' ? current : defaultScope, false)
}

function openAccessKeyCreateForCurrentScope() {
  openAccessKeyDialog(accessKeyScopePath.value, false)
}

async function loadAccessKeyDirectoryPicker(path: string) {
  const normalized = normalizeAccessKeyRootPath(path)
  if (!resolveAssetSpaceByPath(normalized)) {
    showError('只能选择资产空间内的目录')
    return
  }
  accessKeyDirectoryPickerLoading.value = true
  try {
    const token = localStorage.getItem('authToken') || ''
    const response = await fetch(buildDavPath(normalized), {
      method: 'PROPFIND',
      headers: {
        'Authorization': `Bearer ${token}`,
        'Content-Type': 'application/xml',
        'Depth': '1'
      }
    })
    if (!response.ok) {
      throw new Error(`目录读取失败（HTTP ${response.status}）`)
    }
    const xml = await response.text()
    accessKeyDirectoryPickerPath.value = normalized
    accessKeyDirectoryPickerItems.value = parsePropfindResponse(xml, normalized, DAV_PREFIX)
      .filter(item => item.isDir && !isEncryptedDirectoryMetadataFileName(item.name))
      .sort((a, b) => a.name.localeCompare(b.name, 'zh-CN'))
  } catch (error: any) {
    console.error('读取密钥授权目录失败:', error)
    showError(error?.message || '读取目录失败')
  } finally {
    accessKeyDirectoryPickerLoading.value = false
  }
}

function openAccessKeyDirectoryPicker() {
  directoryPickerTarget.value = 'accessKey'
  directoryPickerCreateMode.value = false
  directoryPickerNewFolderName.value = ''
  const selected = normalizeAccessKeyRootPath(accessKeyForm.value.rootPath)
  const initial = resolveAssetSpaceByPath(selected)?.path
    ? selected
    : (getDefaultAssetSpace()?.path || '/personal')
  accessKeyDirectoryPickerVisible.value = true
  void loadAccessKeyDirectoryPicker(initial)
}

function openToolCredentialDirectoryPicker() {
  directoryPickerTarget.value = 'tool'
  directoryPickerCreateMode.value = false
  directoryPickerNewFolderName.value = ''
  const selected = normalizeAccessKeyRootPath(toolCredentialPath.value)
  const initial = resolveAssetSpaceByPath(selected)?.path
    ? selected
    : (getDefaultAssetSpace()?.path || '/personal')
  accessKeyDirectoryPickerVisible.value = true
  void loadAccessKeyDirectoryPicker(initial)
}

function beginDirectoryPickerCreateFolder() {
  directoryPickerNewFolderName.value = ''
  directoryPickerCreateMode.value = true
}

function cancelDirectoryPickerCreateFolder() {
  directoryPickerCreateMode.value = false
  directoryPickerNewFolderName.value = ''
}

async function createDirectoryPickerFolder() {
  const name = directoryPickerNewFolderName.value.trim()
  if (!name) {
    showError('请输入目录名称')
    return
  }
  if (name === '.' || name === '..' || name.includes('/') || name.includes('\\')) {
    showError('目录名称不能包含路径分隔符或 .、..')
    return
  }
  const parentPath = normalizeAccessKeyRootPath(accessKeyDirectoryPickerPath.value)
  if (!resolveAssetSpaceByPath(parentPath)) {
    showError('只能在资产空间内创建目录')
    return
  }
  const targetPath = normalizeAccessKeyRootPath(`${parentPath}/${name}`)
  const token = localStorage.getItem('authToken') || ''
  directoryPickerCreateSubmitting.value = true
  try {
    const response = await fetch(buildDavPath(targetPath), {
      method: 'MKCOL',
      headers: {
        'Authorization': `Bearer ${token}`
      }
    })
    if (response.status === 405) {
      throw new Error('目录已存在')
    }
    if (!response.ok) {
      const text = (await response.text()).trim()
      throw new Error(normalizeUserFacingErrorMessage(text, `创建目录失败: ${response.status}`))
    }
    cancelDirectoryPickerCreateFolder()
    await loadAccessKeyDirectoryPicker(parentPath)
    showSuccess(`目录已创建：${name}`)
  } catch (error: any) {
    console.error('创建授权目录失败:', error)
    showError(errorMessageFromUnknown(error, '创建目录失败'))
  } finally {
    directoryPickerCreateSubmitting.value = false
  }
}

function enterAccessKeyDirectoryPickerItem(item: FileItem) {
  if (item.isDir) void loadAccessKeyDirectoryPicker(item.path)
}

function selectAccessKeyDirectoryPickerPath() {
  const selected = normalizeAccessKeyRootPath(accessKeyDirectoryPickerPath.value)
  if (directoryPickerTarget.value === 'tool') {
    toolCredentialPath.value = selected
    accessKeyDirectoryPickerVisible.value = false
    return
  }
  accessKeyForm.value.rootPath = selected
  accessKeyDirectoryPickerVisible.value = false
}

function adminUserUsageRate(item: AdminUserItem): number | null {
  if (!item || item.quota <= 0) return null
  if (typeof item.quota_usage_percent === 'number') {
    return item.quota_usage_percent / 100
  }
  return item.used_space / item.quota
}

function formatAdminUserUsage(item: AdminUserItem): string {
  const rate = adminUserUsageRate(item)
  if (rate === null) return '-'
  return `${(rate * 100).toFixed(2)}%`
}

function adminUserQuotaStatus(item: AdminUserItem): { label: string; type: 'info' | 'warning' | 'danger' | 'success' } {
  switch (adminUserQuotaStatusValue(item)) {
    case 'unlimited':
      return { label: '不限额', type: 'info' }
    case 'over_quota':
      return { label: '已超额', type: 'danger' }
    case 'near_limit':
      return { label: '接近上限', type: 'warning' }
    default:
      return { label: '正常', type: 'success' }
  }
}

function adminUserQuotaStatusValue(item: AdminUserItem): 'unlimited' | 'over_quota' | 'near_limit' | 'ok' {
  if (item?.quota_status) return item.quota_status
  const rate = adminUserUsageRate(item)
  if (rate === null) return 'unlimited'
  if (item.used_space > item.quota) return 'over_quota'
  if (rate >= 0.8) return 'near_limit'
  return 'ok'
}

function quotaUnitMultiplier(unit: 'B' | 'KB' | 'MB' | 'GB' | 'TB'): number {
  switch (unit) {
    case 'KB':
      return 1024
    case 'MB':
      return 1024 * 1024
    case 'GB':
      return 1024 * 1024 * 1024
    case 'TB':
      return 1024 * 1024 * 1024 * 1024
    default:
      return 1
  }
}

function normalizeQuotaEditor(quota: number): { value: string; unit: 'B' | 'KB' | 'MB' | 'GB' | 'TB' } {
  if (quota <= 0) {
    return { value: '0', unit: 'GB' }
  }
  const units: Array<'TB' | 'GB' | 'MB' | 'KB' | 'B'> = ['TB', 'GB', 'MB', 'KB', 'B']
  for (const unit of units) {
    const multiplier = quotaUnitMultiplier(unit)
    if (quota >= multiplier && quota % multiplier === 0) {
      return { value: String(quota / multiplier), unit }
    }
  }
  return { value: String(quota), unit: 'B' }
}

const adminUsersPreviewBytes = computed(() => {
  const raw = adminUsersEditValue.value.trim()
  if (raw === '' || !/^\d+$/.test(raw)) return null
  const value = Number(raw)
  if (!Number.isSafeInteger(value) || value < 0) return null
  const bytes = value * quotaUnitMultiplier(adminUsersEditUnit.value)
  if (!Number.isSafeInteger(bytes) || bytes < 0) return null
  return bytes
})

function openAdminUsersDialog(item: AdminUserItem) {
  adminUsersEditTarget.value = item
  const normalized = normalizeQuotaEditor(item.quota)
  adminUsersEditValue.value = normalized.value
  adminUsersEditUnit.value = normalized.unit
  adminUsersDialogVisible.value = true
}

function closeAdminUsersDialog() {
  adminUsersDialogVisible.value = false
  adminUsersSubmitting.value = false
  adminUsersEditTarget.value = null
  adminUsersEditValue.value = ''
  adminUsersEditUnit.value = 'GB'
}

async function submitAdminUsersUpdate() {
  const target = adminUsersEditTarget.value
  if (!target) return
  const raw = adminUsersEditValue.value.trim()
  if (raw === '') {
    showError('请输入额度值')
    return
  }
  if (!/^\d+$/.test(raw)) {
    showError('额度必须是大于等于 0 的整数')
    return
  }
  const value = Number(raw)
  if (!Number.isSafeInteger(value) || value < 0) {
    showError('额度值无效，请输入安全整数')
    return
  }
  const multiplier = quotaUnitMultiplier(adminUsersEditUnit.value)
  const quota = value * multiplier
  if (!Number.isSafeInteger(quota) || quota < 0) {
    showError('换算后的额度值过大，请调整后重试')
    return
  }

  adminUsersSubmitting.value = true
  try {
    await adminUserApi.updateQuota(target.username, quota)
    showSuccess('用户额度已更新')
    closeAdminUsersDialog()
    await fetchAdminUsers()
  } catch (error) {
    showError(extractApiErrorMessage(error) || '更新用户额度失败')
  } finally {
    adminUsersSubmitting.value = false
  }
}

async function submitAccessKey() {
  const name = accessKeyForm.value.name.trim()
  if (!name) {
    showError('请输入密钥名称')
    return
  }
  if (accessKeys.value.some(item => item.name === name)) {
    showError('密钥名称已存在，请更换名称')
    return
  }
  if (!accessKeyForm.value.permissions.length) {
    showError('请至少选择一个权限')
    return
  }
  const rootPath = normalizeAccessKeyRootPath(accessKeyForm.value.rootPath)
  if (!rootPath) {
    showError('请输入目录范围')
    return
  }
  const expiryPayload = resolveShareExpiryPayload(accessKeyForm.value)
  if (!expiryPayload) return

  accessKeySubmitting.value = true
  try {
    const data = await webdavAccessKeyApi.create({
      name,
      rootPath,
      permissions: accessKeyForm.value.permissions,
      ...expiryPayload
    })
    accessKeyCreateResult.value = data
    await fetchAccessKeys()
    if (accessKeyScopeLocked.value) {
      accessKeyBindTargetID.value = data.id
    }
    showSuccess('访问密钥已创建，请立即保存密钥')
  } catch (error: any) {
    console.error('创建访问密钥失败:', error)
    showError(error?.message || '创建访问密钥失败')
  } finally {
    accessKeySubmitting.value = false
  }
}

async function bindAccessKeyToScope(id?: string) {
  const targetID = String(id || accessKeyBindTargetID.value || '').trim()
  if (!targetID) {
    showError('请选择要绑定的密钥')
    return
  }
  const scope = normalizeAccessKeyRootPath(accessKeyScopePath.value)
  accessKeyBinding.value = true
  try {
    await webdavAccessKeyApi.bind(targetID, scope)
    await fetchAccessKeys()
    showSuccess('目录绑定成功')
  } catch (error: any) {
    console.error('绑定目录失败:', error)
    showError(error?.message || '绑定目录失败')
  } finally {
    accessKeyBinding.value = false
  }
}

async function revokeAccessKey(item: WebDAVAccessKeyItem) {
  if (item.status !== 'active') return
  if (!(await confirmAction(`确定撤销密钥 ${item.name} 吗？`, '撤销密钥'))) return
  try {
    await webdavAccessKeyApi.revoke(item.id)
    await fetchAccessKeys()
    showSuccess('密钥已撤销')
  } catch (error: any) {
    console.error('撤销访问密钥失败:', error)
    showError(error?.message || '撤销访问密钥失败')
  }
}

async function deleteAccessKey(item: WebDAVAccessKeyItem) {
  if (item.status !== 'revoked') return
  if (!(await confirmAction(`确定删除已撤销的密钥 ${item.name} 吗？`, '删除密钥'))) return
  try {
    await webdavAccessKeyApi.remove(item.id)
    await fetchAccessKeys()
    showSuccess('密钥已删除')
  } catch (error: any) {
    console.error('删除访问密钥失败:', error)
    showError(error?.message || '删除访问密钥失败')
  }
}

async function copyAccessKeyValue(value: string, label: string) {
  await copyText(value, `${label}已复制`)
}

function closeAccessKeyDialog() {
  accessKeyDialogVisible.value = false
}


function openPasswordDialog() {
  passwordForm.value = {
    oldPassword: '',
    newPassword: '',
    confirmPassword: ''
  }
  passwordDialogVisible.value = true
}

async function submitPassword() {
  const oldPassword = passwordForm.value.oldPassword
  const newPassword = passwordForm.value.newPassword
  const confirmPassword = passwordForm.value.confirmPassword
  if (!newPassword || newPassword.length < 6) {
    showError('新密码至少 6 位')
    return
  }
  if (newPassword !== confirmPassword) {
    showError('两次输入的密码不一致')
    return
  }
  if (userProfile.value.hasPassword && !oldPassword) {
    showError('请输入旧密码')
    return
  }
  passwordSubmitting.value = true
  try {
    await userApi.updatePassword(userProfile.value.hasPassword ? oldPassword : null, newPassword)
    if (userInfo.value) {
      userInfo.value.has_password = true
    }
    showSuccess('密码已更新')
    passwordDialogVisible.value = false
  } catch (error: any) {
    console.error('更新密码失败:', error)
    showError(error?.message || '更新密码失败')
  } finally {
    passwordSubmitting.value = false
  }
}

// 进入目录
function enterDirectory(item: FileItem) {
  if (item.isDir) {
    // 确保路径格式正确：/test/ 而不是 /test
    let path = item.path
    if (!path.endsWith('/')) {
      path += '/'
    }
    fetchFiles(path)
  }
}

function enterSharedDirectory(item: FileItem) {
  if (!item.isDir || !sharedActive.value) return
  let path = item.path
  if (!path.endsWith('/')) {
    path += '/'
  }
  detailDrawerVisible.value = false
  sharedPath.value = path
  persistSharedState()
  fetchSharedEntries(path)
}

function setSharedPath(path: string) {
  detailDrawerVisible.value = false
  sharedPath.value = path
  persistSharedState()
  fetchSharedEntries(path)
}

// 单击行进入目录（回收站模式不响应）
function openDetailDrawer(mode: 'file' | 'recycle', item: FileItem | RecycleItem) {
  detailItem.value = item
  detailMode.value = mode
  detailDrawerVisible.value = true
}

function openShareDetail(mode: 'share' | 'directShare', item: ShareItem | DirectShareItem): void
function openShareDetail(mode: 'receivedShare', item: ReceivedSharedResource): void
function openShareDetail(mode: 'share' | 'directShare' | 'receivedShare', item: ShareItem | DirectShareItem | ReceivedSharedResource) {
  detailItem.value = item
  detailMode.value = mode
  detailDrawerVisible.value = true
}

function openDirectShareDetail(item: DirectShareItem) {
  openShareDetail('directShare', item)
}

// Owner-managed direct shares remain on their existing management surface.
// They are intentionally kept separate from received V3 resource browsing.
function enterDirectShareRoot(item: DirectShareItem) {
  openDirectShareDetail(item)
}

function downloadDirectShareRoot(_item: DirectShareItem) {
  showInfo('定向分享目录请从资产列表下载')
}

function openSharedEntryDetail(item: FileItem) {
  detailItem.value = item
  detailMode.value = 'sharedEntry'
  detailDrawerVisible.value = true
}

async function openFilePreview(item: FileItem, forcedMode?: PreviewMode) {
  const detectedMode = getPreviewMode(item)
  const mode = forcedMode || detectedMode
  if (!mode) {
    openPreviewFormatDialog(item)
    return
  }
  const forcedFormat = Boolean(forcedMode && !detectedMode)
  const requestSeq = ++previewRequestSeq
  detailDrawerVisible.value = false
  previewTarget.value = item
  previewMode.value = mode
  previewContent.value = ''
  previewOrigin.value = ''
  previewBlob.value = null
  previewSourceUrl.value = ''
  previewReadOnly.value = isSharedBrowse.value && mode === 'text' && !sharedCanUpdate.value
  previewVisible.value = true
  previewLoading.value = true
  try {
    const token = localStorage.getItem('authToken') || ''
    const previewURL = isSharedBrowse.value && sharedActive.value
      ? `/api/v1/public/share/resource/download?${new URLSearchParams({ resourceId: sharedActive.value.resourceId, path: normalizeShareRelative(item.path) })}`
      : buildDavPath(item.path)
    if (item.encryptedRoot) {
      if (isSharedBrowse.value) {
        throw new Error('加密目录文件暂不支持分享预览')
      }
      const bytes = await readEncryptedFileBytes(item, item.encryptedRoot)
      if (requestSeq !== previewRequestSeq) return
      if (mode === 'text') {
        const text = new TextDecoder().decode(bytes)
        previewContent.value = text
        previewOrigin.value = text
      } else if (mode === 'audio' || mode === 'video') {
        previewSourceUrl.value = createEncryptedDownloadURL(bytes, item.name, mimeTypeForPreviewMode(mode))
      } else {
        previewBlob.value = new Blob([bytes], {
          type: forcedFormat ? mimeTypeForPreviewMode(mode) : inferFileMimeType(item.name)
        })
      }
    } else if (mode === 'text') {
      const response = await fetch(
        previewURL,
        {
          method: 'GET',
          headers: {
            'Authorization': `Bearer ${token}`
          }
        }
      )
      if (!response.ok) {
        throw new Error(`读取失败: ${response.status}`)
      }
      const text = await response.text()
      if (requestSeq !== previewRequestSeq) return
      previewContent.value = text
      previewOrigin.value = text
    } else if (mode === 'audio' || mode === 'video') {
      if (forcedFormat) {
        const response = await fetch(previewURL, {
          method: 'GET',
          headers: {
            'Authorization': `Bearer ${token}`
          }
        })
        if (!response.ok) {
          throw new Error(`读取失败: ${response.status}`)
        }
        if (requestSeq !== previewRequestSeq) return
        const blob = await response.blob()
        previewSourceUrl.value = URL.createObjectURL(new Blob([blob], { type: mimeTypeForPreviewMode(mode) }))
      } else {
        ensureAuthCookie(token)
        if (requestSeq !== previewRequestSeq) return
        previewSourceUrl.value = previewURL
      }
    } else if (mode === 'pdf' || mode === 'word' || mode === 'image') {
      const response = await fetch(
        previewURL,
        {
          method: 'GET',
          headers: {
            'Authorization': `Bearer ${token}`
          }
        }
      )
      if (!response.ok) {
        throw new Error(`读取失败: ${response.status}`)
      }
      if (requestSeq !== previewRequestSeq) return
      const blob = await response.blob()
      previewBlob.value = forcedFormat
        ? new Blob([blob], { type: mimeTypeForPreviewMode(mode) })
        : blob
    }
  } catch (error: any) {
    if (requestSeq !== previewRequestSeq) return
    console.error('预览文件失败:', error)
    showError(error?.message || '预览失败')
    previewVisible.value = false
    resetPreview()
  } finally {
    if (requestSeq !== previewRequestSeq) return
    previewLoading.value = false
  }
}

function resetPreview() {
  previewRequestSeq += 1
  if (previewSourceUrl.value.startsWith('blob:')) {
    URL.revokeObjectURL(previewSourceUrl.value)
  }
  previewTarget.value = null
  previewMode.value = null
  previewContent.value = ''
  previewOrigin.value = ''
  previewBlob.value = null
  previewSourceUrl.value = ''
  previewReadOnly.value = false
  previewLoading.value = false
  previewSaving.value = false
}

async function handlePreviewBeforeClose(done: () => void) {
  if (!previewDirty.value) {
    resetPreview()
    done()
    return
  }
  const confirmed = await confirmAction('内容尚未保存，确定关闭吗？', '关闭确认')
  if (!confirmed) return
  resetPreview()
  done()
}

async function savePreview() {
  if (!previewTarget.value || previewMode.value !== 'text') return
  if (previewReadOnly.value) {
    showError('没有修改权限')
    return
  }
  previewSaving.value = true
  try {
    const token = localStorage.getItem('authToken') || ''
    if (isSharedBrowse.value && sharedActive.value) {
      const relativePath = normalizeShareRelative(previewTarget.value.path)
      if (!relativePath) throw new Error('不能编辑共享根目录')
      const file = new File([previewContent.value], previewTarget.value.name, {
        type: 'text/plain;charset=utf-8',
        lastModified: Date.now()
      })
      const task = createUploadTask(
        { file, relativePath },
        { basePath: '', isShared: true, resourceId: sharedActive.value.resourceId }
      )
      // Reuse upload sessions so text editing observes the same quota, atomic
      // write, effective-grant and mutation-recording rules as file uploads.
      addUploadTasks([task])
      await performUploadTask(task)
      const completedTask = uploadTaskStore.tasks.find(item => item.id === task.id)
      if (completedTask?.status !== 'success') throw new Error(completedTask?.error || '保存失败')
      previewOrigin.value = previewContent.value
      showSuccess('已保存')
      await fetchSharedEntries(sharedPath.value)
    } else {
      const body = previewTarget.value.encryptedRoot
        ? await encryptTextContent(
          previewContent.value,
          await buildEncryptedDirectoryCryptoOptions(previewTarget.value.encryptedRoot)
        )
        : previewContent.value
      const response = await fetch(buildDavPath(previewTarget.value.path), {
        method: 'PUT',
        headers: {
          'Authorization': `Bearer ${token}`,
          'Content-Type': previewTarget.value.encryptedRoot ? 'text/plain; charset=utf-8' : 'text/plain; charset=utf-8'
        },
        body
      })
      if (!response.ok) {
        const text = (await response.text()).trim()
        throw new Error(normalizeUserFacingErrorMessage(text, `保存失败: ${response.status}`))
      }
      previewOrigin.value = previewContent.value
      showSuccess('已保存')
      fetchFiles(currentPath.value)
    }
  } catch (error: any) {
    console.error('保存文件失败:', error)
    showError(errorMessageFromUnknown(error, '保存失败'))
  } finally {
    previewSaving.value = false
  }
}

function downloadPreview() {
  if (!previewTarget.value) return
  downloadFile(previewTarget.value)
}

function previewAdjacentImage(direction: 'prev' | 'next') {
  if (previewMode.value !== 'image') return
  const currentIndex = previewImageIndex.value
  if (currentIndex < 0) return
  const offset = direction === 'next' ? 1 : -1
  const target = previewImageItems.value[currentIndex + offset]
  if (!target) return
  openFilePreview(target)
}

function handleRowClick(row: FileItem | RecycleItem | ShareItem | DirectShareItem | ReceivedSharedResource) {
  if (showQuotaManage.value || showGroupView.value) return
  if (showRecycle.value) {
    openDetailDrawer('recycle', row as RecycleItem)
    return
  }
  if (showShare.value) {
    if (shareTab.value === 'link') {
      openShareDetail('share', row as ShareItem)
    } else {
      const item = row as DirectShareItem
      if (item.isDir) {
        // Owner-side directory browsing remains on its legacy route until its
        // separate state is migrated; do not send it into received resources.
        openDirectShareDetail(item)
      } else {
        openDirectShareDetail(item)
      }
    }
    return
  }
  if (showSharedWithMe.value) {
    if (!sharedActive.value) {
      const item = row as ReceivedSharedResource
      if (item.isDir) {
        enterSharedRoot(item)
      } else {
        openShareDetail('receivedShare', item)
      }
      return
    }
    const entry = row as FileItem
    if (entry.isDir) {
      enterSharedDirectory(entry)
    } else {
      openSharedEntryDetail(entry)
    }
    return
  }
  const item = row as FileItem
  if (item.isDir) {
    detailDrawerVisible.value = false
    enterDirectory(item)
    return
  }
  openDetailDrawer('file', item)
}

// 刷新当前视图
async function refreshCurrentView() {
  if (manualRefresh.value) return
  manualRefresh.value = true
  try {
    if (showRecycle.value) {
      await fetchRecycle()
    } else if (showShare.value) {
      if (shareTab.value === 'link') {
        await fetchShare()
      } else {
        await fetchDirectShareList()
      }
    } else if (showSharedWithMe.value) {
      if (sharedActive.value) {
        await fetchSharedEntries(sharedPath.value)
      } else {
        await fetchSharedWithMe()
      }
    } else if (showQuotaManage.value) {
      if (managementSection.value === 'group') {
        await groupStore.fetchGroups()
      } else {
        await fetchUserCenter()
      }
    } else if (showGroupView.value) {
      await groupStore.fetchGroups()
    } else if (showHelp.value) {
      return
    } else {
      await fetchFiles(currentPath.value)
    }
  } finally {
    manualRefresh.value = false
  }
}

async function copyCurrentPath() {
  let text = currentPath.value
  if (showRecycle.value) {
    text = '回收站'
  } else if (showSharedWithMe.value) {
    if (sharedActive.value) {
      text = `${sharedActive.value.name}${sharedPath.value}`
    } else {
      text = '定向分享'
    }
  } else if (showGroupView.value) {
    text = '分组管理'
  }
  await copyText(text, '已复制当前路径')
}

// 返回上级目录
function goParent() {
  if (!canGoFileParent.value) return
  fetchFiles(fileParentTargetPath.value)
}

function goSharedParent() {
  if (sharedPath.value === '/') return
  const parentPath = sharedParentTargetPath.value
  detailDrawerVisible.value = false
  sharedPath.value = parentPath
  persistSharedState()
  fetchSharedEntries(parentPath)
}

// 下载文件
async function downloadFile(item: FileItem) {
  if (isSharedBrowse.value) {
    await downloadSharedFile(item)
    return
  }
  if (item.encryptedRoot) {
    showInfo('正在解密下载...')
    try {
      const bytes = await readEncryptedFileBytes(item, item.encryptedRoot)
      const url = createEncryptedDownloadURL(bytes, item.name)
      triggerBrowserDownload(url, item.name)
      setTimeout(() => URL.revokeObjectURL(url), 1000)
    } catch (error) {
      showError(`下载失败: ${String(error)}`)
    }
    return
  }
  const apiPath = buildDavPath(item.path)
  showInfo('下载中...')

  try {
    const token = localStorage.getItem('authToken') || ''
    ensureAuthCookie(token)
    triggerBrowserDownload(apiPath, item.name)
  } catch (error) {
    showError(`下载失败: ${String(error)}`)
  }
}

function normalizeShareRelative(path: string) {
  return path.replace(/^\/+/, '').replace(/\/$/, '')
}

function stripDavPrefix(path: string): string {
  if (DAV_PREFIX === '/') return path
  if (path === DAV_PREFIX) return '/'
  if (path.startsWith(DAV_PREFIX + '/')) {
    const trimmed = path.slice(DAV_PREFIX.length)
    return trimmed || '/'
  }
  return path
}

function formatRecycleFullPath(path: string): string {
  const trimmed = String(path || '').replace(/^\/+/, '').replace(/\/+$/, '')
  if (!trimmed) return '/'
  return '/' + trimmed
}

function formatRecycleLocation(path: string): string {
  const trimmed = String(path || '').replace(/^\/+/, '').replace(/\/+$/, '')
  if (!trimmed) return '/'
  const parts = trimmed.split('/').filter(Boolean)
  if (parts.length <= 1) return '/'
  return '/' + parts.slice(0, -1).join('/')
}

function normalizeSharePath(path: string): string {
  if (!path) return '/'
  let cleaned = stripUrlToPath(path).trim()
  if (!cleaned.startsWith('/')) cleaned = '/' + cleaned
  cleaned = stripDavPrefix(cleaned)
  cleaned = cleaned.replace(/\/+$/, '')
  return cleaned || '/'
}

function isSharePathAffected(targetPath: string, isDir: boolean, sharePath: string): boolean {
  const target = normalizeSharePath(targetPath)
  const share = normalizeSharePath(sharePath)
  if (target === '/') return true
  if (isDir) {
    return share === target || share.startsWith(target + '/')
  }
  return share === target
}

async function revokeSharesBeforeDelete(item: FileItem): Promise<{ proceed: boolean; skipConfirm: boolean }> {
  try {
    const [linkShares, directShares] = await Promise.all([
      shareApi.list(),
      directShareApi.listMine()
    ])
    const affectedLink = (linkShares.items || []).filter(share => isSharePathAffected(item.path, item.isDir, share.path))
    const affectedDirect = (directShares.items || []).filter(share => isSharePathAffected(item.path, item.isDir, share.path))

    if (!affectedLink.length && !affectedDirect.length) {
      return { proceed: true, skipConfirm: false }
    }

    const label = item.isDir ? '目录' : '文件'
    const parts: string[] = []
    if (affectedLink.length) parts.push(`分享链接 ${affectedLink.length} 个`)
    if (affectedDirect.length) parts.push(`定向分享 ${affectedDirect.length} 个`)
    const message = `检测到该${label}存在分享（${parts.join('，')}），删除后分享将失效。是否撤销分享并删除？`

    if (!(await confirmAction(message, '删除确认'))) return { proceed: false, skipConfirm: true }

    const revokeTasks: Promise<unknown>[] = []
    for (const share of affectedLink) {
      revokeTasks.push(shareApi.revoke(share.token))
    }
    for (const share of affectedDirect) {
      revokeTasks.push(directShareApi.revoke(share.id))
    }

    const results = await Promise.allSettled(revokeTasks)
    const failed = results.some(result => result.status === 'rejected')
    if (failed) {
      showError('撤销分享失败，已取消删除')
      return { proceed: false, skipConfirm: true }
    }
    return { proceed: true, skipConfirm: true }
  } catch (error) {
    console.error('检测分享失败:', error)
    if (!(await confirmAction('检测分享失败，是否继续删除？', '删除确认'))) return { proceed: false, skipConfirm: true }
    return { proceed: true, skipConfirm: true }
  }
}

async function downloadSharedRoot(item: ReceivedSharedResource) {
	if (!item || item.isDir) return
	const url = `/api/v1/public/share/resource/download?${new URLSearchParams({ resourceId: item.resourceId })}`
  showInfo('下载中...')
  try {
    const token = localStorage.getItem('authToken') || ''
    ensureAuthCookie(token)
    triggerBrowserDownload(url, item.name)
  } catch (error) {
    showError(`下载失败: ${String(error)}`)
  }
}

async function downloadSharedFile(item: FileItem) {
  if (!sharedActive.value) return
  if (item.encryptedRoot || item.encrypted) {
    showError('加密目录文件暂不支持分享下载')
    return
  }
  const relPath = normalizeShareRelative(item.path)
  const url = `/api/v1/public/share/resource/download?${new URLSearchParams({ resourceId: sharedActive.value.resourceId, path: relPath })}`
  showInfo('下载中...')
  try {
    const token = localStorage.getItem('authToken') || ''
    ensureAuthCookie(token)
    triggerBrowserDownload(url, item.name)
  } catch (error) {
    showError(`下载失败: ${String(error)}`)
  }
}

function getRenameContext(item: FileItem) {
  const rawPath = item.path.startsWith('/') ? item.path : '/' + item.path
  const isDir = item.isDir
  const normalized = isDir ? rawPath.replace(/\/$/, '') : rawPath
  const segments = normalized.split('/').filter(Boolean)
  const oldName = segments.pop() || item.name || ''
  const parentPath = segments.length ? '/' + segments.join('/') + '/' : '/'
  return { rawPath, isDir, normalized, oldName, parentPath }
}

function openRenameDialog(item: FileItem, mode: 'file' | 'shared') {
  if (item.path === '/') return
  const context = getRenameContext(item)
  renameTarget.value = item
  renameMode.value = mode
  renameForm.value = { name: context.oldName }
  renameDialogVisible.value = true
}

function renameItem(item: FileItem) {
  if (isSharedBrowse.value) {
    renameSharedItem(item)
    return
  }
  if (isEncryptedRootDirectory(item)) {
    showError('暂不支持直接重命名加密目录根，请新建目录后迁移内容')
    return
  }
  openRenameDialog(item, 'file')
}

function renameSharedItem(item: FileItem) {
  if (!sharedActive.value || !sharedCanUpdate.value) return
  if (isEncryptedRootDirectory(item)) {
    showError('暂不支持直接重命名加密目录根，请新建目录后迁移内容')
    return
  }
  openRenameDialog(item, 'shared')
}

async function submitRename() {
  if (!renameTarget.value || !renameMode.value) return
  const context = getRenameContext(renameTarget.value)
  const newName = renameForm.value.name.trim()
  if (!newName) {
    showError('请输入新的名称')
    return
  }
  if (newName === context.oldName) {
    renameDialogVisible.value = false
    return
  }
  if (newName.includes('/')) {
    showError('名称不能包含 "/"')
    return
  }
  const peerList = renameMode.value === 'shared' ? sharedEntries.value : fileList.value
  const hasSameName = peerList.some(item => item.name === newName && item.path !== renameTarget.value?.path)
  if (hasSameName) {
    showError('同名文件或目录已存在')
    return
  }

  renameSubmitting.value = true
  try {
    if (renameMode.value === 'file') {
      const sourcePath = context.isDir ? context.normalized + '/' : context.normalized
      const destinationPath = (context.parentPath === '/' ? '/' + newName : context.parentPath + newName) + (context.isDir ? '/' : '')
      const token = localStorage.getItem('authToken') || ''
      const response = await fetch(buildDavPath(sourcePath), {
        method: 'MOVE',
        headers: {
          'Authorization': `Bearer ${token}`,
          'Destination': buildDavPath(destinationPath),
          'Overwrite': 'F'
        }
      })

      if (!response.ok) {
        const text = (await response.text()).trim()
        throw new Error(normalizeUserFacingErrorMessage(text, `重命名失败: ${response.status}`))
      }
      await fetchFiles(currentPath.value)
    } else {
      if (!sharedActive.value || !sharedCanUpdate.value) return
      const fromPath = context.normalized.replace(/^\/+/, '')
      const toPath = (context.parentPath === '/' ? '/' + newName : context.parentPath + newName).replace(/^\/+/, '')
      const token = localStorage.getItem('authToken') || ''
      const response = await fetch('/api/v1/public/share/resource/rename', {
        method: 'POST',
        headers: {
          'Authorization': `Bearer ${token}`,
          'Content-Type': 'application/json'
        },
        body: JSON.stringify({ resourceId: sharedActive.value.resourceId, fromPath, toPath })
      })
      if (!response.ok) {
        const text = (await response.text()).trim()
        throw new Error(normalizeUserFacingErrorMessage(text, `重命名失败: ${response.status}`))
      }
      fetchSharedEntries(sharedPath.value)
    }
    renameDialogVisible.value = false
  } catch (error: any) {
    console.error('重命名失败:', error)
    showError(errorMessageFromUnknown(error, '重命名失败'))
  } finally {
    renameSubmitting.value = false
  }
}

async function shareFile(item: FileItem) {
  if (item.isDir) return
  if (isEncryptedItem(item)) {
    showError('加密目录文件暂不支持公开分享')
    return
  }
  shareLinkTarget.value = item
  shareLinkResourceContext.value = null
  shareLinkForm.value = createDefaultShareLinkForm()
  shareLinkDialogVisible.value = true
}

function shareReceivedFile(item: FileItem) {
  if (!sharedActive.value || item.isDir || !sharedCanRead.value) return
  shareLinkTarget.value = item
  shareLinkResourceContext.value = {
    resourceId: sharedActive.value.resourceId,
    relativePath: normalizeShareRelative(item.path)
  }
  shareLinkForm.value = createDefaultShareLinkForm()
  shareLinkDialogVisible.value = true
}

function shareReceivedRoot(item: ReceivedSharedResource) {
	if (item.isDir || !item.permissions.includes('read')) return
	shareLinkTarget.value = { name: item.name, path: item.path, isDir: false, size: 0, modified: item.createdAt }
	shareLinkResourceContext.value = { resourceId: item.resourceId, relativePath: '' }
	shareLinkForm.value = createDefaultShareLinkForm()
	shareLinkDialogVisible.value = true
}

async function submitShareLink() {
  if (!shareLinkTarget.value) return
  const expiryPayload = resolveShareExpiryPayload(shareLinkForm.value)
  if (!expiryPayload) return

  shareLinkSubmitting.value = true
  try {
    const data = shareLinkResourceContext.value
      ? await shareApi.createFromReceivedResource({ ...shareLinkResourceContext.value, ...expiryPayload, mode: shareLinkForm.value.mode })
      : await shareApi.create(shareLinkTarget.value.path, { ...expiryPayload, mode: shareLinkForm.value.mode })
    const url = data.url || `${window.location.origin}/api/v1/public/share/${data.token}`
    await copyText(url, '分享链接已复制')
    shareLinkDialogVisible.value = false
    shareLinkResourceContext.value = null
  } catch (error: any) {
    console.error('创建分享失败:', error)
    showError(error?.message || '创建分享失败')
  } finally {
    shareLinkSubmitting.value = false
  }
}

// 上传文件/目录（含空目录）
const fileInput = ref<HTMLInputElement | null>(null)
const directoryInput = ref<HTMLInputElement | null>(null)

function triggerUpload() {
  fileInput.value?.click()
}

async function triggerDirectoryUpload() {
  const picker = (window as Window & { showDirectoryPicker?: () => Promise<any> }).showDirectoryPicker
  if (picker) {
    try {
      const handle = await picker()
      const files: UploadItem[] = []
      const directories = new Set<string>()
      await walkDirectoryHandle(handle, '', files, directories)
      if (files.length || directories.size) {
        if (isSharedBrowse.value) {
          await uploadSharedFilesWithDirectories(files, directories)
        } else {
          await uploadFilesWithDirectories(files, directories)
        }
      }
      return
    } catch (error: any) {
      if (error?.name === 'AbortError') return
      console.warn('目录选择失败，切换为浏览器目录选择:', error)
    }
  }
  directoryInput.value?.click()
}

function normalizeRelativePath(path: string): string {
  return path.replace(/^\/+/, '').replace(/\\/g, '/').replace(/\/+/g, '/')
}

async function ensureDirectories(path: string, ensured: Set<string>, token: string) {
  const trimmed = normalizeRelativePath(path)
  if (!trimmed) return
  const segments = trimmed.split('/').filter(Boolean)
  let current = ''
  for (const segment of segments) {
    current += '/' + segment
    if (ensured.has(current)) continue
    const apiPath = buildDavPath(current)
    const response = await fetch(apiPath, {
      method: 'MKCOL',
      headers: {
        'Authorization': `Bearer ${token}`
      }
    })
    // 201 = 创建成功, 405 = 已存在
    if (response.ok || response.status === 405) {
      ensured.add(current)
      continue
    }
    const text = (await response.text()).trim()
    throw new Error(normalizeUserFacingErrorMessage(text, `创建目录失败: ${response.status}`))
  }
}

async function readAllDirectoryEntries(reader: {
  readEntries: (success: (entries: DropEntry[]) => void, error?: (error: Error) => void) => void
}): Promise<DropEntry[]> {
  const entries: DropEntry[] = []
  return new Promise((resolve, reject) => {
    const readBatch = () => {
      reader.readEntries(
        batch => {
          if (!batch.length) {
            resolve(entries)
            return
          }
          entries.push(...batch)
          readBatch()
        },
        error => reject(error)
      )
    }
    readBatch()
  })
}

async function walkDirectoryHandle(
  handle: any,
  parentPath: string,
  files: UploadItem[],
  directories: Set<string>
): Promise<void> {
  const dirPath = parentPath ? `${parentPath}/${handle.name}` : handle.name
  if (dirPath) directories.add(normalizeRelativePath(dirPath))
  const entries = handle.entries?.()
  if (!entries) return
  for await (const [name, entry] of entries) {
    if (entry.kind === 'directory') {
      await walkDirectoryHandle(entry, dirPath, files, directories)
    } else if (entry.kind === 'file') {
      const file = await entry.getFile()
      const relativePath = dirPath ? `${dirPath}/${name}` : name
      files.push({ file, relativePath })
    }
  }
}

async function walkEntry(entry: DropEntry, files: UploadItem[], directories: Set<string>): Promise<void> {
  const rawPath = entry.fullPath || entry.name || ''
  const relativePath = normalizeRelativePath(rawPath)
  if (entry.isDirectory) {
    if (relativePath) directories.add(relativePath)
    const reader = entry.createReader?.()
    if (!reader) return
    const children = await readAllDirectoryEntries(reader)
    if (!children.length) return
    for (const child of children) {
      await walkEntry(child, files, directories)
    }
    return
  }
  if (entry.isFile) {
    if (!entry.file) return
    const file = await new Promise<File>((resolve, reject) => {
      entry.file?.(resolve, reject)
    })
    files.push({ file, relativePath: relativePath || file.name })
  }
}

function createUploadTaskId(): string {
  if (typeof crypto !== 'undefined' && 'randomUUID' in crypto) {
    return (crypto as Crypto).randomUUID()
  }
  return `task_${Date.now()}_${Math.random().toString(16).slice(2)}`
}

function buildTargetPath(basePath: string, relativePath: string): string {
  const cleanBase = basePath.replace(/^\/+/, '').replace(/\/$/, '')
  const cleanRelative = normalizeRelativePath(relativePath)
  if (!cleanBase) return '/' + cleanRelative
  if (!cleanRelative) return '/' + cleanBase
  return '/' + cleanBase + '/' + cleanRelative
}

function normalizeUploadTargetPath(path: string): string {
  const normalized = '/' + normalizeRelativePath(path)
  return normalized.length > 1 ? normalized.replace(/\/+$/, '') : '/'
}

function getSharedUploadPrecheckError(task: UploadTask): string {
  if (!task.isShared || sharedCanUpdate.value) return ''
  const targetPath = normalizeUploadTargetPath(task.sharePath || task.relativePath || task.name)
  const existing = sharedEntries.value.find(item => normalizeUploadTargetPath(item.path) === targetPath)
  if (!existing) return ''
  return `同名文件已存在，当前共享没有修改权限，不能覆盖：${existing.name}`
}

function createUploadTask(item: UploadItem, options: { basePath: string; isShared: boolean; shareId?: string; resourceId?: string; encryptedRoot?: string | null; cipherSuite?: string }): UploadTask {
  const relativePath = normalizeRelativePath(item.relativePath || item.file.name) || item.file.name
  const targetPath = buildTargetPath(options.basePath, relativePath)
  const now = Date.now()
  return {
    id: createUploadTaskId(),
    name: item.file.name,
    relativePath,
    size: item.file.size,
    status: 'queued',
    progress: 0,
    createdAt: now,
    updatedAt: now,
    file: item.file,
    targetPath: options.isShared ? undefined : targetPath,
    isShared: options.isShared,
    shareId: options.shareId,
    resourceId: options.resourceId,
    sharePath: options.isShared ? normalizeRelativePath(targetPath) : undefined,
    encryptedRoot: options.encryptedRoot || undefined,
    cipherSuite: options.cipherSuite || undefined,
    originalSize: item.file.size,
    originalLastModified: item.file.lastModified
  }
}

function triggerBrowserDownload(url: string, filename: string) {
  const a = document.createElement('a')
  a.href = url
  a.download = filename
  a.rel = 'noopener'
  document.body.appendChild(a)
  a.click()
  document.body.removeChild(a)
}

function addUploadTasks(tasks: UploadTask[]) {
  uploadTaskStore.addTasks(tasks)
}

function updateUploadTask(task: UploadTask, patch: Partial<UploadTask>) {
  uploadTaskStore.updateTask(task, patch)
}

function getUploadUrlForTask(task: UploadTask): string | null {
  if (task.isShared) {
    if (task.resourceId) return null
    if (!task.shareId) return null
    const path = normalizeRelativePath(task.sharePath || task.relativePath || task.name)
    return buildShareDavPath(task.shareId, path)
  }
  const targetPath = task.targetPath || buildTargetPath('', task.relativePath || task.name)
  return buildDavPath(targetPath)
}

function uploadFileWithProgress(
  url: string,
  file: Blob,
  token: string,
  useMultipart: boolean,
  onProgress: (progress: number, loaded: number, total: number, speed: number) => void
): Promise<void> {
  return new Promise((resolve, reject) => {
    const xhr = new XMLHttpRequest()
    let lastLoaded = 0
    let lastTime = performance.now()
    xhr.open('PUT', url, true)
    if (token) {
      xhr.setRequestHeader('Authorization', `Bearer ${token}`)
    }
    xhr.upload.onprogress = event => {
      if (!event.lengthComputable) return
      const now = performance.now()
      const elapsedSeconds = Math.max((now - lastTime) / 1000, 0.001)
      const speed = Math.max(0, (event.loaded - lastLoaded) / elapsedSeconds)
      lastLoaded = event.loaded
      lastTime = now
      const percent = Math.min(99, Math.round((event.loaded / event.total) * 100))
      onProgress(percent, event.loaded, event.total, speed)
    }
    xhr.onload = () => {
      if (xhr.status >= 200 && xhr.status < 300) {
        resolve()
      } else {
        const rawMessage = xhr.responseText?.trim() || `上传失败: ${xhr.status}`
        reject(new Error(normalizeUserFacingErrorMessage(rawMessage, `上传失败: ${xhr.status}`)))
      }
    }
    xhr.onerror = () => reject(new Error(normalizeUserFacingErrorMessage('上传失败', '上传失败')))
    if (useMultipart) {
      const formData = new FormData()
      formData.append('file', file)
      xhr.send(formData)
      return
    }
    // WebDAV PUT must upload raw file bytes, not multipart/form-data wrapper.
    if (file.type) {
      xhr.setRequestHeader('Content-Type', file.type)
    }
    xhr.send(file)
  })
}

async function uploadSessionRequest<T>(path: string, options: { method?: string; body?: Record<string, unknown> } = {}): Promise<T> {
  const token = localStorage.getItem('authToken') || ''
  const response = await fetch(buildApiPath(path), {
    method: options.method || 'GET',
    headers: {
      'Accept': 'application/json',
      'Content-Type': 'application/json',
      ...(token ? { 'Authorization': `Bearer ${token}` } : {})
    },
    body: options.body ? JSON.stringify(options.body) : undefined
  })
  if (!response.ok) {
    const raw = (await response.text()).trim()
    throw new Error(normalizeUserFacingErrorMessage(raw, `上传会话失败: ${response.status}`))
  }
  const text = await response.text()
  return text.trim() ? JSON.parse(text) as T : undefined as T
}

function createUploadSession(task: UploadTask, file: Blob): Promise<UploadSessionResponse> {
  const path = task.isShared
    ? normalizeRelativePath(task.sharePath || task.relativePath || task.name)
    : (task.targetPath || buildTargetPath('', task.relativePath || task.name))
  return uploadSessionRequest<UploadSessionResponse>('/api/v1/public/uploads/sessions', {
    method: 'POST',
    body: {
      path,
      shareId: task.isShared ? task.shareId : undefined,
      resourceId: task.isShared ? task.resourceId : undefined,
      size: file.size,
      chunkSize: RESUMABLE_UPLOAD_CHUNK_SIZE,
      fileName: task.name,
      contentType: file.type || task.file?.type || 'application/octet-stream',
      lastModified: task.file?.lastModified || task.originalLastModified || 0
    }
  })
}

function getUploadSession(sessionId: string): Promise<UploadSessionResponse> {
  return uploadSessionRequest<UploadSessionResponse>(`/api/v1/public/uploads/sessions/${encodeURIComponent(sessionId)}`)
}

function completeUploadSession(sessionId: string): Promise<UploadSessionResponse> {
  return uploadSessionRequest<UploadSessionResponse>(`/api/v1/public/uploads/sessions/${encodeURIComponent(sessionId)}/complete`, {
    method: 'POST'
  })
}

function expectedChunkSize(totalSize: number, chunkSize: number, partNumber: number): number {
  const start = (partNumber - 1) * chunkSize
  return Math.max(0, Math.min(chunkSize, totalSize - start))
}

function normalizeChecksumSHA256(value: unknown): string {
  const checksum = String(value || '').trim().toLowerCase()
  return /^[0-9a-f]{64}$/.test(checksum) ? checksum : ''
}

function bytesToHex(buffer: ArrayBuffer): string {
  return Array.from(new Uint8Array(buffer), item => item.toString(16).padStart(2, '0')).join('')
}

async function blobSHA256Hex(blob: Blob): Promise<string> {
  const subtle = globalThis.crypto?.subtle
  if (!subtle) {
    throw new Error('当前浏览器不支持上传校验')
  }
  return bytesToHex(await subtle.digest('SHA-256', await blob.arrayBuffer()))
}

async function verifiedUploadedPartMap(session: UploadSessionResponse, file: Blob, chunkSize: number): Promise<Map<number, UploadSessionPart>> {
  const result = new Map<number, UploadSessionPart>()
  const partCount = Math.ceil(file.size / chunkSize)
  for (const part of session.parts || []) {
    const partNumber = Number(part.partNumber)
    if (!Number.isInteger(partNumber) || partNumber < 1 || partNumber > partCount) continue
    if (Number(part.size) !== expectedChunkSize(file.size, chunkSize, partNumber)) continue
    const expectedChecksum = normalizeChecksumSHA256(part.checksumSha256)
    if (!expectedChecksum) continue
    const start = (partNumber - 1) * chunkSize
    const end = Math.min(file.size, start + chunkSize)
    const actualChecksum = await blobSHA256Hex(file.slice(start, end, file.type || 'application/octet-stream'))
    if (actualChecksum !== expectedChecksum) continue
    result.set(partNumber, part)
  }
  return result
}

function uploadedBytesFromParts(parts: Map<number, UploadSessionPart>): number {
  let total = 0
  for (const part of parts.values()) {
    total += Number(part.size || 0)
  }
  return total
}

function uploadSessionPartWithProgress(
  sessionId: string,
  partNumber: number,
  file: Blob,
  checksumSHA256: string,
  token: string,
  onProgress: (loaded: number, speed: number) => void
): Promise<UploadSessionPartResponse> {
  return new Promise((resolve, reject) => {
    const xhr = new XMLHttpRequest()
    let lastLoaded = 0
    let lastTime = performance.now()
    xhr.open('PUT', buildApiPath(`/api/v1/public/uploads/sessions/${encodeURIComponent(sessionId)}/parts/${partNumber}`), true)
    xhr.setRequestHeader('Accept', 'application/json')
    xhr.setRequestHeader('Content-Type', file.type || 'application/octet-stream')
    xhr.setRequestHeader('X-Warehouse-Checksum-SHA256', checksumSHA256)
    if (token) {
      xhr.setRequestHeader('Authorization', `Bearer ${token}`)
    }
    xhr.upload.onprogress = event => {
      const loaded = event.lengthComputable ? event.loaded : Math.min(file.size, lastLoaded)
      const now = performance.now()
      const elapsedSeconds = Math.max((now - lastTime) / 1000, 0.001)
      const speed = Math.max(0, (loaded - lastLoaded) / elapsedSeconds)
      lastLoaded = loaded
      lastTime = now
      onProgress(loaded, speed)
    }
    xhr.onload = () => {
      if (xhr.status >= 200 && xhr.status < 300) {
        try {
          const response = JSON.parse(xhr.responseText || '{}') as UploadSessionPartResponse
          if (normalizeChecksumSHA256(response.part?.checksumSha256) !== checksumSHA256) {
            reject(new Error('上传分片校验失败'))
            return
          }
          resolve(response)
        } catch {
          reject(new Error('上传分片响应无效'))
        }
      } else {
        const rawMessage = xhr.responseText?.trim() || `上传分片失败: ${xhr.status}`
        reject(new Error(normalizeUserFacingErrorMessage(rawMessage, `上传分片失败: ${xhr.status}`)))
      }
    }
    xhr.onerror = () => reject(new Error(normalizeUserFacingErrorMessage('上传分片失败', '上传分片失败')))
    xhr.send(file)
  })
}

function shouldUseResumableUpload(task: UploadTask, file: Blob): boolean {
  // Received V3 resources have no legacy WebDAV write route. Always use the
  // resource-authorized upload-session protocol, including for small files.
  return !!task.resourceId || file.size >= RESUMABLE_UPLOAD_THRESHOLD
}

async function uploadFileResumable(
  task: UploadTask,
  file: Blob,
  token: string,
  onProgress: (progress: number, loaded: number, total: number, speed: number) => void
): Promise<{ transmittedBytes: number }> {
  let session: UploadSessionResponse | null = null
  if (task.uploadSessionId) {
    try {
      session = await getUploadSession(task.uploadSessionId)
    } catch {
      updateUploadTask(task, { uploadSessionId: undefined, uploadSessionExpiresAt: undefined })
    }
  }
  if (!session) {
    session = await createUploadSession(task, file)
  }
  const chunkSize = Number(session.chunkSize || RESUMABLE_UPLOAD_CHUNK_SIZE)
  updateUploadTask(task, {
    uploadSessionId: session.id,
    uploadChunkSize: chunkSize,
    uploadSessionExpiresAt: session.expiresAt,
    resumable: true,
    size: file.size,
    originalSize: task.file?.size || task.originalSize,
    originalLastModified: task.file?.lastModified || task.originalLastModified
  })
  await uploadTaskStore.flushPersistedTasks()

  const validParts = await verifiedUploadedPartMap(session, file, chunkSize)
  let uploadedBytes = uploadedBytesFromParts(validParts)
  let transmittedBytes = 0
  onProgress(Math.min(99, Math.round((uploadedBytes / file.size) * 100)), uploadedBytes, file.size, 0)

  const partCount = Math.ceil(file.size / chunkSize)
  for (let partNumber = 1; partNumber <= partCount; partNumber++) {
    if (validParts.has(partNumber)) continue
    const start = (partNumber - 1) * chunkSize
    const end = Math.min(file.size, start + chunkSize)
    const chunk = file.slice(start, end, file.type || 'application/octet-stream')
    const checksumSHA256 = await blobSHA256Hex(chunk)
    const baseLoaded = uploadedBytes
    const result = await uploadSessionPartWithProgress(session.id, partNumber, chunk, checksumSHA256, token, (partLoaded, speed) => {
      const loaded = Math.min(file.size, baseLoaded + partLoaded)
      const progress = Math.min(99, Math.round((loaded / file.size) * 100))
      onProgress(progress, loaded, file.size, speed)
    })
    validParts.set(partNumber, result.part)
    uploadedBytes += Number(result.part.size || chunk.size)
    transmittedBytes += chunk.size
    onProgress(Math.min(99, Math.round((uploadedBytes / file.size) * 100)), uploadedBytes, file.size, 0)
  }

  await completeUploadSession(session.id)
  return { transmittedBytes }
}

async function getStoredUploadPayload(task: UploadTask): Promise<Blob | null> {
  if (!task.uploadPayloadStorageKey) return null
  try {
    const payload = await uploadTaskStore.getUploadPayload(task.uploadPayloadStorageKey)
    if (payload && (!task.uploadPayloadSize || payload.size === task.uploadPayloadSize)) {
      return payload
    }
  } catch (error) {
    console.warn('读取上传缓存失败:', error)
  }
  await uploadTaskStore.removeUploadPayload(task.uploadPayloadStorageKey)
  updateUploadTask(task, {
    uploadPayloadStorageKey: undefined,
    uploadPayloadSize: undefined,
    uploadSessionId: undefined,
    uploadSessionExpiresAt: undefined
  })
  return null
}

async function resolveEncryptedUploadBody(task: UploadTask): Promise<Blob> {
  const storedPayload = await getStoredUploadPayload(task)
  if (storedPayload) return storedPayload
  if (!task.file) {
    throw new Error('加密上传缓存已失效，请重新选择文件')
  }
  const resetSession = Boolean(task.uploadSessionId)
  const encryptedBody = await encryptFileContent(
    task.file,
    await buildEncryptedDirectoryCryptoOptions(task.encryptedRoot || '', task.cipherSuite || getEncryptedDirectoryCipherSuite(task.encryptedRoot))
  )
  if (encryptedBody.size >= RESUMABLE_UPLOAD_THRESHOLD) {
    let payloadKey = ''
    try {
      payloadKey = await uploadTaskStore.saveUploadPayload(task.id, encryptedBody)
    } catch {
      throw new Error('浏览器本地空间不足，无法准备加密断点续传')
    }
    updateUploadTask(task, {
      uploadPayloadStorageKey: payloadKey,
      uploadPayloadSize: encryptedBody.size,
      size: encryptedBody.size,
      uploadSessionId: resetSession ? undefined : task.uploadSessionId,
      uploadSessionExpiresAt: resetSession ? undefined : task.uploadSessionExpiresAt
    })
    await uploadTaskStore.flushPersistedTasks()
  }
  return encryptedBody
}

async function resolveUploadBody(task: UploadTask): Promise<Blob> {
  if (!task.isShared && task.encryptedRoot) {
    return resolveEncryptedUploadBody(task)
  }
  if (!task.file) {
    throw new Error('文件已失效')
  }
  return task.file
}

async function clearUploadPayload(task: UploadTask) {
  const key = task.uploadPayloadStorageKey
  if (!key) return
  try {
    await uploadTaskStore.removeUploadPayload(key)
  } catch (error) {
    console.warn('删除上传缓存失败:', error)
  }
}

async function performUploadTask(task: UploadTask) {
  if (!task.file && !(task.encryptedRoot && task.uploadPayloadStorageKey)) {
    updateUploadTask(task, { status: 'failed', error: '文件已失效' })
    return
  }
  const useResumable = !!task.resourceId || (!!task.file && shouldUseResumableUpload(task, task.file))
  const url = useResumable ? null : getUploadUrlForTask(task)
  if (!useResumable && !url) {
    updateUploadTask(task, { status: 'failed', error: '上传目标无效' })
    return
  }
  const precheckError = getSharedUploadPrecheckError(task)
  if (precheckError) {
    updateUploadTask(task, { status: 'failed', progress: 0, error: precheckError })
    return
  }
  const token = localStorage.getItem('authToken') || ''
  const useMultipart = false
  updateUploadTask(task, { status: 'uploading', progress: 0, uploadedBytes: 0, uploadSpeed: 0, error: undefined })
  try {
    const uploadBody = await resolveUploadBody(task)
    const uploadStartedAt = performance.now()
    let averageBytes = uploadBody.size
    if (shouldUseResumableUpload(task, uploadBody)) {
      const result = await uploadFileResumable(task, uploadBody, token, (progress, loaded, total, speed) => {
        updateUploadTask(task, { progress, uploadedBytes: loaded, size: total || task.size, uploadSpeed: speed })
      })
      averageBytes = result.transmittedBytes > 0 ? result.transmittedBytes : uploadBody.size
    } else {
      await uploadFileWithProgress(url!, uploadBody, token, useMultipart, (progress, loaded, total, speed) => {
        updateUploadTask(task, { progress, uploadedBytes: loaded, size: total || task.size, uploadSpeed: speed })
      })
    }
    const elapsedSeconds = Math.max((performance.now() - uploadStartedAt) / 1000, 0.001)
    const averageSpeed = averageBytes / elapsedSeconds
    await clearUploadPayload(task)
    updateUploadTask(task, {
      status: 'success',
      progress: 100,
      uploadedBytes: uploadBody.size,
      uploadSpeed: averageSpeed,
      uploadSessionId: undefined,
      uploadSessionExpiresAt: undefined,
      uploadPayloadStorageKey: undefined,
      uploadPayloadSize: undefined
    })
  } catch (error: any) {
    updateUploadTask(task, { status: 'failed', error: errorMessageFromUnknown(error, '上传失败') })
  }
}

async function uploadFilesWithDirectories(files: UploadItem[], extraDirectories?: Set<string>) {
  const cleanPath = currentPath.value.replace(/^\//, '').replace(/\/$/, '')
  const ensuredDirs = new Set<string>()
  const token = localStorage.getItem('authToken') || ''
  const encryptedRoot = resolveEncryptedRootForPath(currentPath.value)
  const cipherSuite = getEncryptedDirectoryCipherSuite(encryptedRoot)

  const directories = new Set<string>()
  if (extraDirectories) {
    for (const dir of extraDirectories) {
      const safeDir = normalizeRelativePath(dir)
      if (safeDir) directories.add(safeDir)
    }
  }
  for (const item of files) {
    const safeRelative = normalizeRelativePath(item.relativePath || item.file.name)
    item.relativePath = safeRelative
    const relativeDir = safeRelative.includes('/') ? safeRelative.slice(0, safeRelative.lastIndexOf('/')) : ''
    if (relativeDir) directories.add(relativeDir)
  }

  const tasks = files.map(item => createUploadTask(item, { basePath: cleanPath, isShared: false, encryptedRoot, cipherSuite }))
  addUploadTasks(tasks)

  const dirsToCreate = Array.from(directories).filter(Boolean).sort((a, b) => a.split('/').length - b.split('/').length)
  for (const dir of dirsToCreate) {
    const targetDir = cleanPath ? '/' + cleanPath + '/' + dir : '/' + dir
    try {
      await ensureDirectories(targetDir, ensuredDirs, token)
    } catch (error) {
      console.error('创建目录失败:', dir, error)
    }
  }

  for (const task of tasks) {
    await performUploadTask(task)
  }

  if (tasks.length || dirsToCreate.length) {
    // 等待文件完全写入后再刷新列表
    await new Promise(resolve => setTimeout(resolve, 500))
    await fetchFiles(currentPath.value)
  }
}

async function ensureSharedDirectories(path: string, ensured: Set<string>, resourceId: string) {
  const trimmed = normalizeRelativePath(path)
  if (!trimmed) return
  const segments = trimmed.split('/').filter(Boolean)
  let current = ''
  for (const segment of segments) {
    current = current ? `${current}/${segment}` : segment
    if (ensured.has(current)) continue
    try {
      const token = localStorage.getItem('authToken') || ''
      const response = await fetch('/api/v1/public/share/resource/folder', {
        method: 'POST',
        headers: {
          'Authorization': `Bearer ${token}`,
          'Content-Type': 'application/json'
        },
        body: JSON.stringify({ resourceId, path: current })
      })
      if (!response.ok && response.status !== 405) {
        const text = (await response.text()).trim()
        throw new Error(normalizeUserFacingErrorMessage(text, `创建目录失败: ${response.status}`))
      }
      ensured.add(current)
    } catch (error) {
      throw error
    }
  }
}

async function uploadSharedFilesWithDirectories(files: UploadItem[], extraDirectories?: Set<string>) {
  if (!sharedActive.value) return
  const resourceId = sharedActive.value.resourceId
  const cleanSharedBase = sharedPath.value.replace(/^\/+/, '').replace(/\/$/, '')
  const ensuredDirs = new Set<string>()

  const directories = new Set<string>()
  if (extraDirectories) {
    for (const dir of extraDirectories) {
      const safeDir = normalizeRelativePath(dir)
      if (safeDir) directories.add(safeDir)
    }
  }
  for (const item of files) {
    const safeRelative = normalizeRelativePath(item.relativePath || item.file.name)
    item.relativePath = safeRelative
    const relativeDir = safeRelative.includes('/') ? safeRelative.slice(0, safeRelative.lastIndexOf('/')) : ''
    if (relativeDir) directories.add(relativeDir)
  }

  const tasks = files.map(item => createUploadTask(item, { basePath: cleanSharedBase, isShared: true, resourceId }))
  addUploadTasks(tasks)

  const dirsToCreate = Array.from(directories).filter(Boolean).sort((a, b) => a.split('/').length - b.split('/').length)
  for (const dir of dirsToCreate) {
    const targetDir = cleanSharedBase ? `${cleanSharedBase}/${dir}` : dir
    try {
      await ensureSharedDirectories(targetDir, ensuredDirs, resourceId)
    } catch (error) {
      console.error('创建目录失败:', dir, error)
    }
  }

  for (const task of tasks) {
    await performUploadTask(task)
  }

  if (tasks.length || dirsToCreate.length) {
    await new Promise(resolve => setTimeout(resolve, 500))
    await fetchSharedEntries(sharedPath.value)
  }
}

function chooseFileForUploadTask(): Promise<File | null> {
  return new Promise(resolve => {
    const input = document.createElement('input')
    input.type = 'file'
    input.style.display = 'none'
    input.onchange = () => {
      const file = input.files?.[0] || null
      input.remove()
      resolve(file)
    }
    input.oncancel = () => {
      input.remove()
      resolve(null)
    }
    document.body.appendChild(input)
    input.click()
  })
}

async function ensureRetryTaskFile(task: UploadTask): Promise<boolean> {
  if (task.file) return true
  if (task.encryptedRoot && task.uploadPayloadStorageKey) {
    const payload = await getStoredUploadPayload(task)
    if (payload) return true
  }
  if (!task.uploadSessionId && !task.encryptedRoot) {
    showError('文件已失效，无法重试')
    return false
  }
  const file = await chooseFileForUploadTask()
  if (!file) return false
  const expectedSize = Number(task.originalSize || task.size || 0)
  if (file.name !== task.name || (expectedSize > 0 && file.size !== expectedSize)) {
    showError('请选择同一个文件继续上传')
    return false
  }
  if (task.originalLastModified && file.lastModified !== task.originalLastModified) {
    showError('文件修改时间不一致，请选择原始文件继续上传')
    return false
  }
  updateUploadTask(task, {
    file,
    error: undefined,
    originalSize: file.size,
    originalLastModified: file.lastModified,
    uploadSessionId: task.encryptedRoot ? undefined : task.uploadSessionId,
    uploadSessionExpiresAt: task.encryptedRoot ? undefined : task.uploadSessionExpiresAt,
    uploadPayloadStorageKey: undefined,
    uploadPayloadSize: undefined
  })
  return true
}

async function retryUploadTask(task: UploadTask) {
  if (task.status === 'uploading') return
  if (!await ensureRetryTaskFile(task)) return
  try {
    if (task.isShared) {
      if (!task.resourceId) {
        showError('共享信息已失效，无法重试')
        return
      }
      const relative = normalizeRelativePath(task.sharePath || task.relativePath || task.name)
      const dir = relative.includes('/') ? relative.slice(0, relative.lastIndexOf('/')) : ''
      if (dir) {
        await ensureSharedDirectories(dir, new Set<string>(), task.resourceId)
      }
    } else {
      const token = localStorage.getItem('authToken') || ''
      const targetPath = task.targetPath || buildTargetPath('', task.relativePath || task.name)
      const dirPath = targetPath.includes('/') ? targetPath.slice(0, targetPath.lastIndexOf('/')) : ''
      if (dirPath) {
        await ensureDirectories(dirPath, new Set<string>(), token)
      }
    }
  } catch (error) {
    console.error('重试创建目录失败:', error)
  }

  await performUploadTask(task)

  if (task.status !== 'success') return
  if (task.isShared) {
    if (showSharedWithMe.value && sharedActive.value?.resourceId === task.resourceId) {
      fetchSharedEntries(sharedPath.value)
    }
    return
  }
  if (!showRecycle.value && !showShare.value && !showSharedWithMe.value && !showQuotaManage.value && !showGroupView.value && !showHelp.value) {
    fetchFiles(currentPath.value)
  }
}

async function handleFileSelect(event: Event) {
  const input = event.target as HTMLInputElement
  const fileList = input.files ? Array.from(input.files) : []
  if (!fileList.length) {
    if (input === directoryInput.value) {
      showInfo('目录内没有可上传的文件，空目录请使用拖拽上传')
    }
    return
  }

  if (isSharedBrowse.value) {
    const items = fileList.map(file => ({
      file,
      relativePath: (file as File & { webkitRelativePath?: string }).webkitRelativePath || file.name
    }))
    await uploadSharedFilesWithDirectories(items)
    input.value = ''
    return
  }

  const items = fileList.map(file => ({
    file,
    relativePath: (file as File & { webkitRelativePath?: string }).webkitRelativePath || file.name
  }))
  await uploadFilesWithDirectories(items)

  // 清空 input，允许重复上传同一文件
  input.value = ''
}

function handleDragEnter(event: DragEvent) {
  event.preventDefault()
  if (isInternalMoveDrag(event)) return
  if (!isExternalFileDrag(event)) return
  if (!canUpload.value) return
  dragCounter.value += 1
  dragActive.value = true
}

function handleDragOver(event: DragEvent) {
  if (isInternalMoveDrag(event)) return
  if (!isExternalFileDrag(event)) return
  event.preventDefault()
}

function handleDragLeave(event: DragEvent) {
  event.preventDefault()
  if (isInternalMoveDrag(event)) return
  if (!isExternalFileDrag(event)) return
  if (!canUpload.value) return
  dragCounter.value = Math.max(dragCounter.value - 1, 0)
  if (dragCounter.value === 0) {
    dragActive.value = false
  }
}

async function handleDrop(event: DragEvent) {
  event.preventDefault()
  if (isInternalMoveDrag(event)) return
  if (!isExternalFileDrag(event)) return
  dragCounter.value = 0
  dragActive.value = false
  if (!canUpload.value) return

  const dataTransfer = event.dataTransfer
  if (!dataTransfer) return

  const files: UploadItem[] = []
  const directories = new Set<string>()
  const items = dataTransfer.items ? Array.from(dataTransfer.items) : []

  const entries: DropEntry[] = []
  for (const item of items) {
    if (item.kind !== 'file') continue
    const entry = (item as unknown as { webkitGetAsEntry?: () => DropEntry | null }).webkitGetAsEntry?.()
    if (entry) entries.push(entry)
  }

  if (entries.length > 0) {
    for (const entry of entries) {
      try {
        await walkEntry(entry, files, directories)
      } catch (error) {
        console.error('读取拖拽条目失败:', error)
      }
    }
  } else {
    const droppedFiles = dataTransfer.files ? Array.from(dataTransfer.files) : []
    for (const file of droppedFiles) {
      const relativePath = (file as File & { webkitRelativePath?: string }).webkitRelativePath || file.name
      files.push({ file, relativePath })
    }
  }

  if (!files.length && directories.size === 0) return
  if (isSharedBrowse.value) {
    await uploadSharedFilesWithDirectories(files, directories)
  } else {
    await uploadFilesWithDirectories(files, directories)
  }
}

function canDragItem(item: FileItem): boolean {
  return isFileView.value && !loading.value && !movingByDrag.value && item.path !== '/'
}

function isDraggingFile(item: FileItem): boolean {
  return draggingFileItem.value?.path === item.path
}

function canMoveItemToDirectoryPath(source: FileItem | null, targetPath: string): boolean {
  if (!source) return false
  const sourcePath = normalizeItemPath(source)
  const normalizedTargetPath = normalizeDirectoryPath(targetPath)
  if (sourcePath === normalizedTargetPath) return false
  if (source.isDir && normalizedTargetPath.startsWith(sourcePath)) return false
  if (!canTransferAcrossEncryptedBoundary(source, normalizedTargetPath).allowed) return false
  const destinationPath = `${normalizedTargetPath}${source.name}${source.isDir ? '/' : ''}`
  return destinationPath !== sourcePath
}

function canMoveToDirectory(source: FileItem | null, target: FileItem): boolean {
  return target.isDir && canMoveItemToDirectoryPath(source, target.path)
}

function isMoveTarget(item: FileItem): boolean {
  return item.isDir && dragMoveTargetPath.value !== '' && normalizeDirectoryPath(item.path) === dragMoveTargetPath.value
}

function handleItemDragStart(event: DragEvent, item: FileItem) {
  if (!canDragItem(item) || !event.dataTransfer) return
  draggingFileItem.value = item
  dragMoveTargetPath.value = ''
  event.dataTransfer.effectAllowed = 'move'
  event.dataTransfer.setData('application/x-warehouse-file-move', normalizeItemPath(item))
  event.dataTransfer.setData('text/plain', item.name)
}

function handleItemDragEnd() {
  draggingFileItem.value = null
  dragMoveTargetPath.value = ''
}

function handleDirectoryDragEnter(event: DragEvent, item: FileItem) {
  if (!isInternalMoveDrag(event)) return
  if (!canMoveToDirectory(draggingFileItem.value, item)) return
  event.preventDefault()
  dragMoveTargetPath.value = normalizeDirectoryPath(item.path)
}

function handleDirectoryDragOver(event: DragEvent, item: FileItem) {
  if (!isInternalMoveDrag(event)) return
  if (!canMoveToDirectory(draggingFileItem.value, item)) return
  event.preventDefault()
  if (event.dataTransfer) {
    event.dataTransfer.dropEffect = 'move'
  }
  dragMoveTargetPath.value = normalizeDirectoryPath(item.path)
}

function handleDirectoryDragLeave(_event: DragEvent, item: FileItem) {
  if (!isMoveTarget(item)) return
  dragMoveTargetPath.value = ''
}

async function moveFileByDrag(source: FileItem, targetDir: FileItem) {
  await moveFileToDirectoryPath(source, targetDir.path)
}

async function moveFileToDirectoryPath(source: FileItem, targetPath: string) {
  const sourcePath = normalizeItemPath(source)
  const targetDirPath = normalizeDirectoryPath(targetPath)
  const transferCheck = canTransferAcrossEncryptedBoundary(source, targetDirPath)
  if (!transferCheck.allowed) {
    showError(transferCheck.message || '移动失败')
    return
  }
  const destinationPath = `${targetDirPath}${source.name}${source.isDir ? '/' : ''}`
  if (destinationPath === sourcePath) {
    return
  }

  movingByDrag.value = true
  try {
    const token = localStorage.getItem('authToken') || ''
    const response = await fetch(buildDavPath(sourcePath), {
      method: 'MOVE',
      headers: {
        'Authorization': `Bearer ${token}`,
        'Destination': buildDavPath(destinationPath),
        'Overwrite': 'F'
      }
    })

    if (!response.ok) {
      if (response.status === 412) {
        throw new Error('目标目录中已存在同名文件或目录')
      }
      const text = (await response.text()).trim()
      throw new Error(normalizeUserFacingErrorMessage(text, `移动失败: ${response.status}`))
    }

    await fetchFiles(currentPath.value)
    showSuccess(`已移动到 ${targetDirPath}`)
  } catch (error: any) {
    console.error('拖拽移动失败:', error)
    showError(errorMessageFromUnknown(error, '移动失败'))
  } finally {
    movingByDrag.value = false
  }
}

async function handleDirectoryDrop(event: DragEvent, item: FileItem) {
  if (!isInternalMoveDrag(event)) return
  event.preventDefault()
  const source = draggingFileItem.value
  handleItemDragEnd()
  if (!source) return
  if (!canMoveToDirectory(source, item)) return
  await moveFileByDrag(source, item)
}

function isBreadcrumbMoveTarget(path: string): boolean {
  return dragMoveTargetPath.value !== '' && normalizeDirectoryPath(path) === dragMoveTargetPath.value
}

function isParentButtonMoveTarget(): boolean {
  return canGoFileParent.value && isBreadcrumbMoveTarget(fileParentTargetPath.value)
}

function handleBreadcrumbDragEnter(event: DragEvent, path: string) {
  if (!isInternalMoveDrag(event)) return
  if (!canMoveItemToDirectoryPath(draggingFileItem.value, path)) return
  event.preventDefault()
  dragMoveTargetPath.value = normalizeDirectoryPath(path)
}

function handleBreadcrumbDragOver(event: DragEvent, path: string) {
  if (!isInternalMoveDrag(event)) return
  if (!canMoveItemToDirectoryPath(draggingFileItem.value, path)) return
  event.preventDefault()
  if (event.dataTransfer) {
    event.dataTransfer.dropEffect = 'move'
  }
  dragMoveTargetPath.value = normalizeDirectoryPath(path)
}

function handleBreadcrumbDragLeave(_event: DragEvent, path: string) {
  if (!isBreadcrumbMoveTarget(path)) return
  dragMoveTargetPath.value = ''
}

async function handleBreadcrumbDrop(event: DragEvent, path: string) {
  if (!isInternalMoveDrag(event)) return
  event.preventDefault()
  const source = draggingFileItem.value
  handleItemDragEnd()
  if (!source) return
  if (!canMoveItemToDirectoryPath(source, path)) return
  await moveFileToDirectoryPath(source, path)
}

function handleParentButtonDragEnter(event: DragEvent) {
  handleBreadcrumbDragEnter(event, fileParentTargetPath.value)
}

function handleParentButtonDragOver(event: DragEvent) {
  handleBreadcrumbDragOver(event, fileParentTargetPath.value)
}

function handleParentButtonDragLeave(event: DragEvent) {
  handleBreadcrumbDragLeave(event, fileParentTargetPath.value)
}

async function handleParentButtonDrop(event: DragEvent) {
  await handleBreadcrumbDrop(event, fileParentTargetPath.value)
}

function canDragSharedItem(item: FileItem): boolean {
  return isSharedBrowse.value && sharedCanUpdate.value && !sharedEntriesLoading.value && !movingSharedByDrag.value && item.path !== '/'
}

function isDraggingSharedItem(item: FileItem): boolean {
  return draggingSharedItem.value?.path === item.path
}

function canMoveSharedItemToDirectoryPath(source: FileItem | null, targetPath: string): boolean {
  if (!source) return false
  const sourcePath = normalizeSharedItemPath(source)
  const normalizedTargetPath = normalizeDirectoryPath(targetPath)
  if (sourcePath === normalizedTargetPath) return false
  if (source.isDir && normalizedTargetPath.startsWith(sourcePath)) return false
  if (!canTransferAcrossEncryptedBoundary(source, normalizedTargetPath).allowed) return false
  const destinationPath = `${normalizedTargetPath}${source.name}${source.isDir ? '/' : ''}`
  return destinationPath !== sourcePath
}

function canMoveSharedToDirectory(source: FileItem | null, target: FileItem): boolean {
  return target.isDir && canMoveSharedItemToDirectoryPath(source, target.path)
}

function isSharedMoveTarget(item: FileItem): boolean {
  return item.isDir && sharedDragMoveTargetPath.value !== '' && normalizeDirectoryPath(item.path) === sharedDragMoveTargetPath.value
}

function handleSharedItemDragStart(event: DragEvent, item: FileItem) {
  if (!canDragSharedItem(item) || !event.dataTransfer) return
  draggingSharedItem.value = item
  sharedDragMoveTargetPath.value = ''
  event.dataTransfer.effectAllowed = 'move'
  event.dataTransfer.setData('application/x-warehouse-shared-move', normalizeSharedItemPath(item))
  event.dataTransfer.setData('text/plain', item.name)
}

function handleSharedItemDragEnd() {
  draggingSharedItem.value = null
  sharedDragMoveTargetPath.value = ''
}

function handleSharedDirectoryDragEnter(event: DragEvent, item: FileItem) {
  if (!isInternalMoveDrag(event)) return
  if (!canMoveSharedToDirectory(draggingSharedItem.value, item)) return
  event.preventDefault()
  sharedDragMoveTargetPath.value = normalizeDirectoryPath(item.path)
}

function handleSharedDirectoryDragOver(event: DragEvent, item: FileItem) {
  if (!isInternalMoveDrag(event)) return
  if (!canMoveSharedToDirectory(draggingSharedItem.value, item)) return
  event.preventDefault()
  if (event.dataTransfer) {
    event.dataTransfer.dropEffect = 'move'
  }
  sharedDragMoveTargetPath.value = normalizeDirectoryPath(item.path)
}

function handleSharedDirectoryDragLeave(_event: DragEvent, item: FileItem) {
  if (!isSharedMoveTarget(item)) return
  sharedDragMoveTargetPath.value = ''
}

async function moveSharedItemToDirectoryPath(source: FileItem, targetPath: string) {
  if (!sharedActive.value || !sharedCanUpdate.value) return
  const sourcePath = normalizeSharedItemPath(source)
  const targetDirPath = normalizeDirectoryPath(targetPath)
  const transferCheck = canTransferAcrossEncryptedBoundary(source, targetDirPath)
  if (!transferCheck.allowed) {
    showError(transferCheck.message || '移动失败')
    return
  }
  const destinationPath = `${targetDirPath}${source.name}${source.isDir ? '/' : ''}`
  if (destinationPath === sourcePath) return
  movingSharedByDrag.value = true
  try {
    const token = localStorage.getItem('authToken') || ''
    const response = await fetch('/api/v1/public/share/resource/rename', {
      method: 'POST',
      headers: {
        'Authorization': `Bearer ${token}`,
        'Content-Type': 'application/json'
      },
      body: JSON.stringify({
        resourceId: sharedActive.value.resourceId,
        fromPath: sourcePath,
        toPath: destinationPath
      })
    })
    if (!response.ok) {
      const text = (await response.text()).trim()
      throw new Error(normalizeUserFacingErrorMessage(text, `移动失败: ${response.status}`))
    }
    await fetchSharedEntries(sharedPath.value)
    showSuccess(`已移动到 ${targetDirPath}`)
  } catch (error: any) {
    console.error('共享拖拽移动失败:', error)
    showError(error?.message || '移动失败')
  } finally {
    movingSharedByDrag.value = false
  }
}

async function handleSharedDirectoryDrop(event: DragEvent, item: FileItem) {
  if (!isInternalMoveDrag(event)) return
  event.preventDefault()
  const source = draggingSharedItem.value
  handleSharedItemDragEnd()
  if (!source) return
  if (!canMoveSharedToDirectory(source, item)) return
  await moveSharedItemToDirectoryPath(source, item.path)
}

function isSharedBreadcrumbMoveTarget(path: string): boolean {
  return sharedDragMoveTargetPath.value !== '' && normalizeDirectoryPath(path) === sharedDragMoveTargetPath.value
}

function isSharedParentButtonMoveTarget(): boolean {
  return sharedPath.value !== '/' && isSharedBreadcrumbMoveTarget(sharedParentTargetPath.value)
}

function handleSharedBreadcrumbDragEnter(event: DragEvent, path: string) {
  if (!isInternalMoveDrag(event)) return
  if (!canMoveSharedItemToDirectoryPath(draggingSharedItem.value, path)) return
  event.preventDefault()
  sharedDragMoveTargetPath.value = normalizeDirectoryPath(path)
}

function handleSharedBreadcrumbDragOver(event: DragEvent, path: string) {
  if (!isInternalMoveDrag(event)) return
  if (!canMoveSharedItemToDirectoryPath(draggingSharedItem.value, path)) return
  event.preventDefault()
  if (event.dataTransfer) {
    event.dataTransfer.dropEffect = 'move'
  }
  sharedDragMoveTargetPath.value = normalizeDirectoryPath(path)
}

function handleSharedBreadcrumbDragLeave(_event: DragEvent, path: string) {
  if (!isSharedBreadcrumbMoveTarget(path)) return
  sharedDragMoveTargetPath.value = ''
}

async function handleSharedBreadcrumbDrop(event: DragEvent, path: string) {
  if (!isInternalMoveDrag(event)) return
  event.preventDefault()
  const source = draggingSharedItem.value
  handleSharedItemDragEnd()
  if (!source) return
  if (!canMoveSharedItemToDirectoryPath(source, path)) return
  await moveSharedItemToDirectoryPath(source, path)
}

function handleSharedParentButtonDragEnter(event: DragEvent) {
  handleSharedBreadcrumbDragEnter(event, sharedParentTargetPath.value)
}

function handleSharedParentButtonDragOver(event: DragEvent) {
  handleSharedBreadcrumbDragOver(event, sharedParentTargetPath.value)
}

function handleSharedParentButtonDragLeave(event: DragEvent) {
  handleSharedBreadcrumbDragLeave(event, sharedParentTargetPath.value)
}

async function handleSharedParentButtonDrop(event: DragEvent) {
  await handleSharedBreadcrumbDrop(event, sharedParentTargetPath.value)
}

function handleFileSelectionChange(rows: FileItem[]) {
  selectedFileRows.value = rows
}

function clearSelectedFiles() {
  selectedFileRows.value = []
  fileSelectionNonce.value += 1
}

function enterFileSelectionMode() {
  if (isSharedBrowse.value) return
  fileSelectionMode.value = true
}

function exitFileSelectionMode() {
  fileSelectionMode.value = false
  clearSelectedFiles()
}

function handleBatchDeleteButtonClick() {
  if (!fileSelectionMode.value) {
    enterFileSelectionMode()
    return
  }
  void deleteSelectedFiles()
}

function buildBatchShareRevokePlan(
  items: FileItem[],
  linkShares: ShareItem[],
  directShares: DirectShareItem[]
): { link: ShareItem[]; direct: DirectShareItem[] } {
  const linkMap = new Map<string, ShareItem>()
  const directMap = new Map<string, DirectShareItem>()
  for (const item of items) {
    for (const share of linkShares) {
      if (!isSharePathAffected(item.path, item.isDir, share.path)) continue
      linkMap.set(share.token, share)
    }
    for (const share of directShares) {
      if (!isSharePathAffected(item.path, item.isDir, share.path)) continue
      directMap.set(share.id, share)
    }
  }
  return {
    link: Array.from(linkMap.values()),
    direct: Array.from(directMap.values())
  }
}

async function revokeSharesBeforeBatchDelete(items: FileItem[]): Promise<{ proceed: boolean }> {
  try {
    const [linkShares, directShares] = await Promise.all([
      shareApi.list(),
      directShareApi.listMine()
    ])
    const plan = buildBatchShareRevokePlan(items, linkShares.items || [], directShares.items || [])
    if (!plan.link.length && !plan.direct.length) {
      const confirmed = await confirmAction(`确定删除选中的 ${items.length} 项吗？删除后可在回收站恢复`, '删除确认')
      return { proceed: confirmed }
    }
    const parts: string[] = []
    if (plan.link.length) parts.push(`分享链接 ${plan.link.length} 个`)
    if (plan.direct.length) parts.push(`定向分享 ${plan.direct.length} 个`)
    const message = `检测到选中项存在分享（${parts.join('，')}），删除后分享将失效。是否撤销分享并删除选中的 ${items.length} 项？`
    if (!(await confirmAction(message, '删除确认'))) return { proceed: false }

    const revokeTasks: Promise<unknown>[] = []
    for (const share of plan.link) {
      revokeTasks.push(shareApi.revoke(share.token))
    }
    for (const share of plan.direct) {
      revokeTasks.push(directShareApi.revoke(share.id))
    }
    const results = await Promise.allSettled(revokeTasks)
    const failed = results.some(result => result.status === 'rejected')
    if (failed) {
      showError('撤销分享失败，已取消删除')
      return { proceed: false }
    }
    return { proceed: true }
  } catch (error) {
    console.error('检测分享失败:', error)
    const confirmed = await confirmAction(`检测分享失败，是否继续删除选中的 ${items.length} 项？`, '删除确认')
    return { proceed: confirmed }
  }
}

async function deleteSelectedFiles() {
  if (isSharedBrowse.value) return
  const selectedMap = new Map<string, FileItem>()
  for (const item of selectedFileRows.value) {
    selectedMap.set(item.path, item)
  }
  const items = Array.from(selectedMap.values())
  if (!items.length) {
    showInfo('请先选择要删除的资产')
    return
  }
  const { proceed } = await revokeSharesBeforeBatchDelete(items)
  if (!proceed) return

  const token = localStorage.getItem('authToken') || ''
  let deletedCount = 0
  let failedCount = 0
  for (const item of items) {
    const apiPath = buildDavPath(item.path)
    try {
      const response = await fetch(apiPath, {
        method: 'DELETE',
        headers: {
          'Authorization': `Bearer ${token}`
        }
      })
      if (!response.ok) {
        failedCount += 1
        continue
      }
      unregisterEncryptedDirectoryRootsForDeletedPath(item.path)
      deletedCount += 1
    } catch {
      failedCount += 1
    }
  }
  selectedFileRows.value = []
  fileSelectionMode.value = false
  fileSelectionNonce.value += 1
  fetchFiles(currentPath.value)
  if (failedCount > 0) {
    showError(`批量删除完成：成功 ${deletedCount} 项，失败 ${failedCount} 项`)
    return
  }
  showSuccess(`已删除 ${deletedCount} 项`)
}

// 删除文件
async function deleteFile(item: FileItem) {
  if (isSharedBrowse.value) {
    await deleteSharedItem(item)
    return
  }
  const { proceed, skipConfirm } = await revokeSharesBeforeDelete(item)
  if (!proceed) return
  if (!skipConfirm && !(await confirmAction(`确定删除 ${item.name} 吗？删除后可在回收站恢复`, '删除确认'))) return

  const apiPath = buildDavPath(item.path)
  try {
    const token = localStorage.getItem('authToken') || ''
    const response = await fetch(apiPath, {
      method: 'DELETE',
      headers: {
        'Authorization': `Bearer ${token}`
      }
    })

    if (!response.ok) {
      throw new Error('删除失败')
    }

    unregisterEncryptedDirectoryRootsForDeletedPath(item.path)
    fetchFiles(currentPath.value)
  } catch (error) {
    showError(`删除失败: ${String(error)}`)
  }
}

async function deleteSharedItem(item: FileItem) {
  if (!sharedActive.value || !sharedCanDelete.value) return
  if (!(await confirmAction(`确定删除 ${item.name} 吗？`, '删除确认'))) return
  const relPath = normalizeShareRelative(item.path)
  try {
    const token = localStorage.getItem('authToken') || ''
    const response = await fetch('/api/v1/public/share/resource/delete', {
      method: 'POST',
      headers: {
        'Authorization': `Bearer ${token}`,
        'Content-Type': 'application/json'
      },
      body: JSON.stringify({ resourceId: sharedActive.value.resourceId, path: relPath })
    })
    if (!response.ok) {
      const text = (await response.text()).trim()
      throw new Error(normalizeUserFacingErrorMessage(text, `删除失败: ${response.status}`))
    }
    fetchSharedEntries(sharedPath.value)
  } catch (error) {
    showError(`删除失败: ${String(error)}`)
  }
}

// 获取回收站列表
async function fetchRecycle() {
  recycleLoading.value = true
  try {
    const data = await recycleApi.list({
      page: recyclePage.value,
      pageSize: recyclePageSize,
      search: recycleSearch.value
    })
    recycleList.value = Array.isArray(data.items) ? data.items : []
    recycleTotal.value = Number(data.total || 0)
  } catch (error) {
    console.error('获取回收站失败:', error)
    recycleList.value = []
    recycleTotal.value = 0
  } finally {
    recycleLoading.value = false
  }
}

// 进入回收站
function enterRecycle() {
  detailDrawerVisible.value = false
  recyclePage.value = 1
  showRecycle.value = true
  showShare.value = false
  showSharedWithMe.value = false
  showQuotaManage.value = false
  showGroupView.value = false
  showHelp.value = false
  sharedActive.value = null
  sharedPath.value = '/'
  persistView('recycle')
  fetchRecycle()
}

// 返回文件列表
function enterFiles(path: string = currentPath.value) {
  detailDrawerVisible.value = false
  showRecycle.value = false
  showShare.value = false
  showSharedWithMe.value = false
  showQuotaManage.value = false
  showGroupView.value = false
  showHelp.value = false
  sharedActive.value = null
  sharedPath.value = '/'
  persistView('files')
  fetchFiles(path)
}

function enterAssetSpace(spaceKey: string) {
  const target = normalizeAssetSpaces(assetSpaces.value).find(item => item.key === spaceKey)
  if (!target) return
  enterFiles(target.path)
}

function getAssetSpaceIcon(spaceKey: string) {
  if (spaceKey === 'apps') return Grid
  if (spaceKey === 'services') return Notebook
  return FolderOpened
}

function toggleSidePanel() {
  sidePanelCollapsed.value = !sidePanelCollapsed.value
  try {
    localStorage.setItem(SIDEBAR_COLLAPSED_STORAGE_KEY, sidePanelCollapsed.value ? '1' : '0')
  } catch {
    // ignore storage failure
  }
}

function backToFiles() {
  enterFiles(resolveInitialFilePath(currentPath.value))
}

function syncViewportMode() {
  if (typeof window === 'undefined') return
  isMobileViewport.value = window.innerWidth <= 768
}

// 恢复文件
async function recoverFile(item: RecycleItem) {
  if (!(await confirmAction(`确定恢复 ${item.name} 吗？`, '恢复文件'))) return
  try {
    await recycleApi.recover(item.hash)
    fetchRecycle()
  } catch (error) {
    showError(`恢复失败: ${String(error)}`)
  }
}

// 永久删除文件
async function permanentlyDelete(item: RecycleItem) {
  if (!(await confirmAction(`确定永久删除 ${item.name} 吗？此操作不可恢复！`, '永久删除'))) return
  try {
    await recycleApi.remove(item.hash)
    fetchRecycle()
  } catch (error) {
    showError(`删除失败: ${String(error)}`)
  }
}

async function clearRecycle() {
  if (!recycleList.value.length) {
    showInfo('回收站为空')
    return
  }
  if (!(await confirmAction('确定清空回收站吗？此操作不可恢复！', '清空回收站'))) return
  recycleClearing.value = true
  try {
    await recycleApi.clear()
    showSuccess('回收站已清空')
    await fetchRecycle()
  } catch (error) {
    console.error('清空回收站失败:', error)
    showError('清空回收站失败')
  } finally {
    recycleClearing.value = false
  }
}

// 获取分享列表
async function fetchShare() {
  shareLoading.value = true
  try {
    const data = await shareApi.list()
    shareList.value = data.items
  } catch (error) {
    console.error('获取分享列表失败:', error)
  } finally {
    shareLoading.value = false
  }
}

// 获取我分享的（定向）列表
async function fetchDirectShareList() {
  directShareLoading.value = true
  sharedWithMeLoading.value = true
  try {
    const [mineResult, receivedResult] = await Promise.allSettled([
      directShareApi.listMine(),
      directShareApi.listReceived()
    ])
    if (mineResult.status === 'fulfilled') {
      directShareList.value = mineResult.value.items
    } else {
      console.error('获取我发起的定向分享失败:', mineResult.reason)
    }
    if (receivedResult.status === 'fulfilled') {
      sharedWithMeList.value = receivedResult.value.items
    } else {
      console.error('获取分享给我的定向分享失败:', receivedResult.reason)
    }
  } catch (error) {
    console.error('获取定向分享列表失败:', error)
  } finally {
    directShareLoading.value = false
    sharedWithMeLoading.value = false
  }
}

// 获取分享给我的列表
async function fetchSharedWithMe() {
  sharedWithMeLoading.value = true
  try {
    const data = await directShareApi.listReceived()
    sharedWithMeList.value = data.items
  } catch (error) {
    console.error('获取共享给我列表失败:', error)
  } finally {
    sharedWithMeLoading.value = false
  }
}

async function fetchSharedEntries(path: string = sharedPath.value) {
  if (!sharedActive.value) return
  sharedEntriesLoading.value = true
  try {
    const data = await directShareApi.listReceivedResourceEntries(sharedActive.value.resourceId, path)
    sharedEntries.value = data.items
  } catch (error: any) {
    console.error('获取共享目录失败:', error)
    showError(error?.message || '获取共享目录失败')
  } finally {
    sharedEntriesLoading.value = false
  }
}

// 进入分享管理
function enterShare(type: 'link' | 'direct' = shareTab.value) {
  detailDrawerVisible.value = false
  showShare.value = true
  showRecycle.value = false
  showSharedWithMe.value = false
  showQuotaManage.value = false
  showGroupView.value = false
  showHelp.value = false
  sharedActive.value = null
  sharedPath.value = '/'
  shareTab.value = type
  persistView(type === 'link' ? 'shareLink' : 'shareDirect')
  if (shareTab.value === 'link') {
    fetchShare()
  } else {
    fetchDirectShareList()
  }
}

function switchShareTab(type: 'link' | 'direct') {
  if (shareTab.value === type && showShare.value) return
  enterShare(type)
}

// 进入共享的目录
function enterSharedRoot(item: ReceivedSharedResource) {
  if (!item.isDir) return
  detailDrawerVisible.value = false
  showSharedWithMe.value = true
  showShare.value = false
  showRecycle.value = false
  showQuotaManage.value = false
  showGroupView.value = false
  showHelp.value = false
  sharedActive.value = item
  sharedPath.value = '/'
  sharedEntries.value = []
  persistView('sharedWithMe')
  persistSharedState()
  fetchSharedEntries('/')
}


function backToSharedList() {
  detailDrawerVisible.value = false
  showSharedWithMe.value = false
  showShare.value = true
  showRecycle.value = false
  showQuotaManage.value = false
  showGroupView.value = false
  showHelp.value = false
  shareTab.value = 'direct'
  sharedActive.value = null
  sharedPath.value = '/'
  sharedEntries.value = []
  persistView('shareDirect')
  clearSharedState()
  fetchDirectShareList()
}

function enterSharedWithMeList() {
  detailDrawerVisible.value = false
  showSharedWithMe.value = true
  showShare.value = false
  showRecycle.value = false
  showQuotaManage.value = false
  showGroupView.value = false
  showHelp.value = false
  sharedActive.value = null
  sharedPath.value = '/'
  persistView('sharedWithMe')
  clearSharedState()
  fetchSharedWithMe()
}

// Side navigation must restore the last received-resource context rather than
// discarding it. Search text is scoped to this view, so keeping the resource
// and path together prevents a stale keyword being applied to the root.
async function returnToSharedWithMe() {
  if (showSharedWithMe.value && sharedActive.value) return
  await restoreSharedWithMeView()
}

// 取消分享
async function revokeShare(item: ShareItem) {
  if (!(await confirmAction(`确定取消分享 ${item.name} 吗？`, '取消分享'))) return
  try {
    await shareApi.revoke(item.token)
    fetchShare()
  } catch (error) {
    showError(`取消分享失败: ${String(error)}`)
  }
}

async function revokeDirectShare(item: DirectShareItem) {
  if (!isDirectShareOwner(item)) {
    showError('仅分享者可以取消该定向分享')
    return
  }
  if (!(await confirmAction(`确定取消分享 ${item.name} 吗？`, '取消分享'))) return
  try {
    await directShareApi.revoke(item.id)
    fetchDirectShareList()
  } catch (error) {
    showError(`取消分享失败: ${String(error)}`)
  }
}

async function copyShareLink(item: ShareItem) {
  const url = item.url || `${window.location.origin}/api/v1/public/share/${item.token}`
  await copyText(url, '分享链接已复制')
}

function getShareLink(item: ShareItem): string {
  return item.url || `${window.location.origin}/api/v1/public/share/${item.token}`
}

function formatShareMode(mode?: ShareMode | string): string {
  return mode === 'preview' ? '浏览器打开' : '仅下载'
}

function openShareUserDialog(item: FileItem) {
  if (isEncryptedItem(item)) {
    showError('加密目录文件暂不支持定向分享')
    return
  }
  shareUserTarget.value = item
  shareUserForm.value = {
    targetMode: 'addresses',
    targetAddresses: [],
    groupIds: [],
    permissions: ['read'],
    ...createDefaultShareExpiryForm()
  }
  shareUserDialogVisible.value = true
  groupStore.fetchGroups()
}

async function submitShareUser() {
  if (!shareUserTarget.value) return
  if (!shareUserForm.value.permissions.length) {
    showError('请至少选择一个权限')
    return
  }

  const expiryPayload = resolveShareExpiryPayload(shareUserForm.value)
  if (!expiryPayload) return
  const rawPath = shareUserTarget.value.isDir
    ? shareUserTarget.value.path.replace(/\/$/, '')
    : shareUserTarget.value.path

  shareUserSubmitting.value = true
  try {
    if (shareUserForm.value.targetMode === 'groups') {
      const groupIds = Array.from(
        new Set(shareUserForm.value.groupIds.map(item => String(item || '').trim()))
      )
      if (!groupIds.length) {
        showError('请至少选择一个分组')
        return
      }
      if (!selectedGroupMembers.value.length) {
        showError('所选分组没有可用地址')
        return
      }
      await directShareApi.create({
        path: rawPath,
        targetMode: 'groups',
        groupIds,
        permissions: shareUserForm.value.permissions,
        ...expiryPayload
      })
      showSuccess(`已共享给所选分组内 ${selectedGroupMembers.value.length} 位用户`)
    } else if (shareUserForm.value.targetMode === 'all_users') {
      await directShareApi.create({
        path: rawPath,
        targetMode: 'all_users',
        permissions: shareUserForm.value.permissions,
        ...expiryPayload
      })
      showSuccess('已共享给所有用户')
    } else {
      const targetAddresses = Array.from(
        new Set(
          shareUserForm.value.targetAddresses
            .map(item => item.trim())
            .filter(Boolean)
            .map(item => item.toLowerCase())
        )
      )
      if (!targetAddresses.length) {
        showError('请至少输入一个目标钱包地址')
        return
      }
      await directShareApi.create({
        path: rawPath,
        targetMode: 'addresses',
        targetAddresses,
        permissions: shareUserForm.value.permissions,
        ...expiryPayload
      })
      showSuccess(`已共享给 ${targetAddresses.length} 个地址`)
    }
    shareUserDialogVisible.value = false
    if (showShare.value && shareTab.value === 'direct') {
      fetchDirectShareList()
    }
  } catch (error) {
    console.error('定向分享失败:', error)
    showError('定向分享失败')
  } finally {
    shareUserSubmitting.value = false
  }
}

function enterGroupView() {
  detailDrawerVisible.value = false
  showGroupView.value = true
  showShare.value = false
  showRecycle.value = false
  showSharedWithMe.value = false
  showQuotaManage.value = false
  showHelp.value = false
  sharedActive.value = null
  sharedPath.value = '/'
  persistView('group')
  groupStore.fetchGroups()
}

// 进入账户管理
function enterQuotaManage(section: ManagementSection = 'account') {
  const nextSection = !canManageUsers.value && section === 'adminUsers' ? 'account' : normalizeManagementSection(section)
  detailDrawerVisible.value = false
  managementSection.value = nextSection
  showQuotaManage.value = true
  showShare.value = false
  showRecycle.value = false
  showSharedWithMe.value = false
  showGroupView.value = false
  showHelp.value = false
  sharedActive.value = null
  sharedPath.value = '/'
  persistView('quotaManage')
  persistManagementSection(nextSection)
  if (nextSection === 'group') {
    groupStore.fetchGroups()
    return
  }
  fetchUserCenter()
}

function enterHelpGuide() {
  detailDrawerVisible.value = false
  showHelp.value = true
  showQuotaManage.value = false
  showGroupView.value = false
  showShare.value = false
  showRecycle.value = false
  showSharedWithMe.value = false
  sharedActive.value = null
  sharedPath.value = '/'
  persistView('help')
}

function openUploadTaskLocation(task: UploadTask) {
  if (task.isShared) return
  const rawTargetPath = String(task.targetPath || '').trim()
  if (!rawTargetPath) return
  const normalizedTargetPath = normalizeDirectoryPath(rawTargetPath)
  const parentPath = normalizedTargetPath === '/'
    ? '/'
    : normalizeDirectoryPath(normalizedTargetPath.replace(/\/[^/]+\/?$/, '') || '/')
  uploadTaskStore.closeDialog()
  enterFiles(parentPath)
}

function handleUploadTaskRetryEvent(event: Event) {
  const task = (event as CustomEvent<{ task?: UploadTask }>).detail?.task
  if (!task) return
  retryUploadTask(task)
}

function handleUploadTaskOpenEvent(event: Event) {
  const task = (event as CustomEvent<{ task?: UploadTask }>).detail?.task
  if (!task) return
  openUploadTaskLocation(task)
}

function openShareLocation(item: ShareItem) {
  const rawPath = stripUrlToPath(String(item.path || '').trim())
  if (!rawPath) return
  const normalizedPath = rawPath.startsWith('/') ? rawPath : `/${rawPath}`
  const compactPath = normalizedPath.replace(/\/{2,}/g, '/')
  const parentPath = compactPath.includes('/')
    ? normalizeDirectoryPath(compactPath.replace(/\/[^/]+\/?$/, '') || '/')
    : '/'
  enterFiles(parentPath)
}

function openCreateFolderDialog(mode: 'file' | 'shared') {
  if (mode === 'shared' && (!sharedActive.value || !sharedCanCreate.value)) return
  createFolderMode.value = mode
  createFolderForm.value = {
    name: '',
    encrypted: false,
    cipherSuite: DEFAULT_ENCRYPTED_DIRECTORY_CIPHER_SUITE,
    passwordSource: DEFAULT_ENCRYPTED_DIRECTORY_PASSWORD_SOURCE,
    password: '',
    confirmPassword: ''
  }
  createFolderDialogVisible.value = true
}

function createFolder() {
  if (isSharedBrowse.value) {
    openCreateFolderDialog('shared')
    return
  }
  openCreateFolderDialog('file')
}

async function createEncryptedDirectoryMarker(
  directoryPath: string,
  password: string,
  cipherSuite: string,
  passwordSource: EncryptedDirectoryPasswordSource
) {
  const token = localStorage.getItem('authToken') || ''
  const metadataPath = buildEncryptedDirectoryMetadataPath(directoryPath)
  const metadata = buildEncryptedDirectoryMetadata(cipherSuite, passwordSource)
  const response = await fetch(buildDavPath(metadataPath), {
    method: 'PUT',
    headers: {
      'Authorization': `Bearer ${token}`,
      'Content-Type': 'application/json; charset=utf-8'
    },
    body: JSON.stringify(metadata)
  })

  if (!response.ok) {
    const text = (await response.text()).trim()
    throw new Error(normalizeUserFacingErrorMessage(text, `创建加密目录失败: ${response.status}`))
  }

  registerEncryptedDirectoryRoot(directoryPath, metadata)
  if (passwordSource === 'wallet') {
    setEncryptedDirectoryPassword(directoryPath, '')
  } else {
    setEncryptedDirectoryPassword(directoryPath, password)
  }
}

async function createFolderWithName(
  name: string,
  encrypted = false,
  password = '',
  cipherSuite = DEFAULT_ENCRYPTED_DIRECTORY_CIPHER_SUITE,
  passwordSource: EncryptedDirectoryPasswordSource = DEFAULT_ENCRYPTED_DIRECTORY_PASSWORD_SOURCE
) {
  const cleanPath = currentPath.value.replace(/^\/+/, '').replace(/\/$/, '')
  const targetPath = cleanPath ? '/' + cleanPath + '/' + name : '/' + name
  const apiPath = buildDavPath(targetPath)
  const token = localStorage.getItem('authToken') || ''
  const response = await fetch(apiPath, {
    method: 'MKCOL',
    headers: {
      'Authorization': `Bearer ${token}`
    }
  })

  if (response.ok || response.status === 405) {
    if (response.ok && encrypted) {
      await createEncryptedDirectoryMarker(targetPath, password, cipherSuite, passwordSource)
    } else if (response.ok) {
      unregisterExactEncryptedDirectoryRoot(targetPath)
    }
    fetchFiles(currentPath.value)
    if (response.status === 405) {
      showError('文件夹已存在')
    }
    return
  }
  const text = (await response.text()).trim()
  throw new Error(normalizeUserFacingErrorMessage(text, `创建失败: ${response.status}`))
}

async function createSharedFolderWithName(name: string) {
  if (!sharedActive.value || !sharedCanCreate.value) return
  const basePath = sharedPath.value.replace(/^\/+/, '').replace(/\/$/, '')
  const targetPath = basePath ? `${basePath}/${name}` : name
  const token = localStorage.getItem('authToken') || ''
  const response = await fetch('/api/v1/public/share/resource/folder', {
    method: 'POST',
    headers: {
      'Authorization': `Bearer ${token}`,
      'Content-Type': 'application/json'
    },
    body: JSON.stringify({ resourceId: sharedActive.value.resourceId, path: targetPath })
  })
  if (!response.ok) {
    const text = (await response.text()).trim()
    throw new Error(normalizeUserFacingErrorMessage(text, `创建失败: ${response.status}`))
  }
  fetchSharedEntries(sharedPath.value)
}

async function submitCreateFolder() {
  const mode = createFolderMode.value
  if (!mode) return
  const name = createFolderForm.value.name.trim()
  const encrypted = mode === 'file' && createFolderForm.value.encrypted
  const cipherSuite = normalizeCipherSuite(createFolderForm.value.cipherSuite)
  const passwordSource = normalizeEncryptedDirectoryPasswordSource(createFolderForm.value.passwordSource)
  const password = createFolderForm.value.password
  const confirmPassword = createFolderForm.value.confirmPassword
  if (!name) {
    showError('请输入文件夹名称')
    return
  }
  if (name.includes('/')) {
    showError('名称不能包含 "/"')
    return
  }
  if (encrypted && passwordSource !== 'wallet' && !password) {
    showError(passwordSource === 'wallet+password' ? '请输入额外密码' : '请输入目录密码')
    return
  }
  if (encrypted && passwordSource !== 'wallet' && password !== confirmPassword) {
    showError(passwordSource === 'wallet+password' ? '两次输入的额外密码不一致' : '两次输入的目录密码不一致')
    return
  }
  createFolderSubmitting.value = true
  try {
    if (mode === 'shared') {
      await createSharedFolderWithName(name)
    } else {
      await createFolderWithName(name, encrypted, password, cipherSuite, passwordSource)
    }
    createFolderDialogVisible.value = false
  } catch (error: any) {
    console.error('创建失败:', error)
    showError(errorMessageFromUnknown(error, '创建失败'))
  } finally {
    createFolderSubmitting.value = false
  }
}

// 格式化文件大小
function formatSize(bytes: number): string {
  const value = Number(bytes)
  if (!Number.isFinite(value) || value < 0) return '-'
  const units = ['B', 'K', 'M', 'G', 'T', 'P']
  let size = value
  let index = 0
  while (size >= 1024 && index < units.length - 1) {
    size /= 1024
    index += 1
  }
  return `${Math.round(size)} ${units[index]}`
}

function formatSizeDetail(bytes: number): string {
  const value = Number(bytes)
  if (!Number.isFinite(value) || value < 0) return '-'
  const raw = Math.trunc(value)
  const units = ['B', 'K', 'M', 'G', 'T', 'P']
  let size = value
  let index = 0
  while (size >= 1024 && index < units.length - 1) {
    size /= 1024
    index += 1
  }
  if (index === 0) return `${raw} B`
  return `${raw} B (${size.toFixed(2)} ${units[index]})`
}

// 格式化时间
function formatDateTime(value: string | number): string {
  if (value === null || value === undefined || value === '') return '-'
  try {
    let raw: number | string = value
    if (typeof value === 'string') {
      const trimmed = value.trim()
      if (/^\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}$/.test(trimmed)) {
        return trimmed
      }
      if (/^\d+$/.test(trimmed)) {
        const asNumber = Number(trimmed)
        raw = trimmed.length <= 10 ? asNumber * 1000 : asNumber
      } else {
        raw = trimmed
      }
    }
    const date = new Date(raw)
    if (Number.isNaN(date.getTime())) return '-'
    const pad = (num: number) => String(num).padStart(2, '0')
    const year = date.getFullYear()
    const month = pad(date.getMonth() + 1)
    const day = pad(date.getDate())
    const hour = pad(date.getHours())
    const minute = pad(date.getMinutes())
    const second = pad(date.getSeconds())
    return `${year}-${month}-${day} ${hour}:${minute}:${second}`
  } catch {
    return '-'
  }
}

// 格式化时间
function formatTime(timeStr: string | number): string {
  return formatDateTime(timeStr)
}

// 格式化删除时间
function formatDeletedTime(timeStr: string): string {
  return formatDateTime(timeStr)
}

function formatSharePermission(permission: string): string {
  const key = permission.toLowerCase()
  if (key === 'read') return '读取'
  if (key === 'create') return '上传'
  if (key === 'update') return '重命名'
  if (key === 'delete') return '删除'
  return permission
}

function escapeHtml(value: string): string {
  return String(value)
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&#39;')
}

const HELP_DOC_BASE_URL = 'https://github.com/yeying-community/warehouse/blob/main/docs/'

function resolveHelpDocumentHref(href: string): string {
  const value = href.trim()
  if (/^https?:\/\//i.test(value)) return value
  if (value.startsWith('./') || value.startsWith('../')) {
    try {
      return new URL(value, HELP_DOC_BASE_URL).toString()
    } catch {
      return ''
    }
  }
  return ''
}

function renderInlineMarkdown(value: string): string {
  const escaped = escapeHtml(value)
  return escaped
    .replace(/`([^`]+)`/g, '<code>$1</code>')
    .replace(/\[([^\]]+)\]\(([^)]+)\)/g, (_match, label: string, href: string) => {
      const target = resolveHelpDocumentHref(href)
      if (!target) return `<span>${label}</span>`
      return `<a href="${escapeHtml(target)}" target="_blank" rel="noopener noreferrer">${label}</a>`
    })
    .replace(/\*\*([^*]+)\*\*/g, '<strong>$1</strong>')
}

function renderMarkdown(markdown: string): string {
  const lines = markdown.replace(/\r\n/g, '\n').split('\n')
  const html: string[] = []
  let inCode = false
  let codeLang = ''
  let codeLines: string[] = []
  let inList = false
  let inOrderedList = false
  let inTable = false

  const closeList = () => {
    if (!inList) return
    html.push('</ul>')
    inList = false
  }
  const closeOrderedList = () => {
    if (!inOrderedList) return
    html.push('</ol>')
    inOrderedList = false
  }
  const closeTable = () => {
    if (!inTable) return
    html.push('</tbody></table>')
    inTable = false
  }
  const closeCode = () => {
    if (!inCode) return
    html.push(`<pre><code${codeLang ? ` class="language-${escapeHtml(codeLang)}"` : ''}>${escapeHtml(codeLines.join('\n'))}</code></pre>`)
    inCode = false
    codeLang = ''
    codeLines = []
  }
  const closeBlocks = () => {
    closeList()
    closeOrderedList()
    closeTable()
  }

  for (const rawLine of lines) {
    const line = rawLine.replace(/\s+$/, '')
    if (line.startsWith('```')) {
      if (inCode) {
        closeCode()
      } else {
        closeBlocks()
        inCode = true
        codeLang = line.slice(3).trim()
        codeLines = []
      }
      continue
    }
    if (inCode) {
      codeLines.push(rawLine)
      continue
    }
    if (!line.trim()) {
      closeBlocks()
      continue
    }
    const headingMatch = line.match(/^(#{1,4})\s+(.+)$/)
    if (headingMatch) {
      closeBlocks()
      const level = headingMatch[1].length
      html.push(`<h${level}>${renderInlineMarkdown(headingMatch[2])}</h${level}>`)
      continue
    }
    if (/^\|.+\|$/.test(line)) {
      const cells = line.slice(1, -1).split('|').map(item => item.trim())
      const isSeparator = cells.every(cell => /^:?-{3,}:?$/.test(cell))
      if (isSeparator) continue
      closeList()
      if (!inTable) {
        html.push('<table><tbody>')
        inTable = true
      }
      html.push(`<tr>${cells.map(cell => `<td>${renderInlineMarkdown(cell)}</td>`).join('')}</tr>`)
      continue
    }
    const listMatch = line.match(/^[-*]\s+(.+)$/)
    if (listMatch) {
      closeOrderedList()
      closeTable()
      if (!inList) {
        html.push('<ul>')
        inList = true
      }
      html.push(`<li>${renderInlineMarkdown(listMatch[1])}</li>`)
      continue
    }
    const orderedListMatch = line.match(/^\d+\.\s+(.+)$/)
    if (orderedListMatch) {
      closeList()
      closeTable()
      if (!inOrderedList) {
        html.push('<ol>')
        inOrderedList = true
      }
      html.push(`<li>${renderInlineMarkdown(orderedListMatch[1])}</li>`)
      continue
    }
    closeBlocks()
    html.push(`<p>${renderInlineMarkdown(line)}</p>`)
  }
  closeCode()
  closeBlocks()
  return html.join('\n')
}

function handleMobileAction(command: string) {
  switch (command) {
    case 'createFolder':
      createFolder()
      break
    case 'uploadFile':
      triggerUpload()
      break
    case 'uploadDir':
      triggerDirectoryUpload()
      break
    case 'clearRecycle':
      clearRecycle()
      break
    default:
      refreshCurrentView()
  }
}


function persistView(view: ViewKey) {
  localStorage.setItem(VIEW_STORAGE_KEY, view)
  window.dispatchEvent(new CustomEvent('warehouse:view-changed', { detail: { view } }))
}

function normalizeManagementSection(value: unknown): ManagementSection {
  switch (value) {
    case 'keys':
    case 'adminUsers':
    case 'group':
      return value
    default:
      return 'account'
  }
}

function persistManagementSection(section: ManagementSection) {
  localStorage.setItem(MANAGEMENT_SECTION_STORAGE_KEY, section)
}

function restoreManagementSection() {
  const storedSection = normalizeManagementSection(localStorage.getItem(MANAGEMENT_SECTION_STORAGE_KEY))
  return !canManageUsers.value && storedSection === 'adminUsers' ? 'account' : storedSection
}

function clearSharedState() {
  localStorage.removeItem(SHARED_ACTIVE_STORAGE_KEY)
  localStorage.removeItem(SHARED_PATH_STORAGE_KEY)
}

function persistSharedState() {
  if (!sharedActive.value) return
  localStorage.setItem(SHARED_ACTIVE_STORAGE_KEY, sharedActive.value.resourceId)
  localStorage.setItem(SHARED_PATH_STORAGE_KEY, sharedPath.value)
}

async function restoreSharedWithMeView() {
  const activeId = localStorage.getItem(SHARED_ACTIVE_STORAGE_KEY) || ''
  const storedPath = normalizeDirectoryPath(localStorage.getItem(SHARED_PATH_STORAGE_KEY) || '/')
  if (!activeId) {
    enterSharedWithMeList()
    return
  }

  detailDrawerVisible.value = false
  showSharedWithMe.value = true
  showShare.value = false
  showRecycle.value = false
  showQuotaManage.value = false
  showGroupView.value = false
  sharedActive.value = null
  sharedPath.value = storedPath
  sharedEntries.value = []
  persistView('sharedWithMe')

  sharedWithMeLoading.value = true
  try {
    const [, receivedResult] = await Promise.allSettled([
      directShareApi.listMine(),
      directShareApi.listReceived()
    ])
    const receivedItems = receivedResult.status === 'fulfilled' ? receivedResult.value.items : []
    if (receivedResult.status === 'fulfilled') sharedWithMeList.value = receivedItems

    const active = receivedItems.find(item => item.resourceId === activeId && item.isDir)
    if (!active) {
      clearSharedState()
      sharedPath.value = '/'
      await fetchSharedWithMe()
      return
    }
    sharedActive.value = active
    persistSharedState()
    await fetchSharedEntries(sharedPath.value)
  } catch (error) {
    console.error('恢复共享目录失败:', error)
    clearSharedState()
    sharedPath.value = '/'
    await fetchSharedWithMe()
  } finally {
    sharedWithMeLoading.value = false
  }
}

async function restoreView() {
  const storedView = localStorage.getItem(VIEW_STORAGE_KEY) as ViewKey | null
  if (storedView === 'recycle') {
    enterRecycle()
    return
  }
  if (storedView === 'shareLink') {
    enterShare('link')
    return
  }
  if (storedView === 'shareDirect') {
    enterShare('direct')
    return
  }
  if (storedView === 'sharedWithMe') {
    await restoreSharedWithMeView()
    return
  }
  if (storedView === 'quotaManage') {
    enterQuotaManage(restoreManagementSection())
    return
  }
  if (storedView === 'group') {
    enterGroupView()
    return
  }
  if (storedView === 'help') {
    enterHelpGuide()
    return
  }
  const storedPath = localStorage.getItem(FILE_PATH_STORAGE_KEY) || '/'
  enterFiles(resolveInitialFilePath(storedPath))
}

watch(filteredFileList, rows => {
  const visiblePaths = new Set(rows.map(item => item.path))
  selectedFileRows.value = selectedFileRows.value.filter(item => visiblePaths.has(item.path))
})

watch(recyclePage, () => {
  if (showRecycle.value) {
    void fetchRecycle()
  }
})

watch(recycleSearch, () => {
  if (recyclePage.value !== 1) {
    recyclePage.value = 1
    return
  }
  if (showRecycle.value) {
    void fetchRecycle()
  }
})

watch(currentPath, () => {
  selectedFileRows.value = []
  fileSelectionMode.value = false
})

watch(isFileView, visible => {
  if (visible) return
  selectedFileRows.value = []
  fileSelectionMode.value = false
})

watch(renameDialogVisible, visible => {
  if (visible) return
  renameTarget.value = null
  renameMode.value = null
  renameForm.value = { name: '' }
})

watch(createFolderDialogVisible, visible => {
  if (visible) return
  createFolderMode.value = null
  createFolderForm.value = {
    name: '',
    encrypted: false,
    cipherSuite: DEFAULT_ENCRYPTED_DIRECTORY_CIPHER_SUITE,
    passwordSource: DEFAULT_ENCRYPTED_DIRECTORY_PASSWORD_SOURCE,
    password: '',
    confirmPassword: ''
  }
})

watch(accessKeyDialogVisible, visible => {
  if (visible) return
  accessKeyScopeLocked.value = false
  accessKeyBindTargetID.value = ''
  accessKeyBinding.value = false
  accessKeyCreateResult.value = null
  accessKeyForm.value = createDefaultAccessKeyForm('/')
})

watch(credentialTab, tab => {
  if (tab === 'tool' && !toolCredentials.value.length) fetchToolCredentials(true)
})

watch(managedGroups, groups => {
  const validIDs = new Set(groups.map(group => group.id))
  const normalized = Array.from(
    new Set(shareUserForm.value.groupIds.map(item => String(item || '').trim()))
  ).filter(id => id === '' || validIDs.has(id))
  if (normalized.length !== shareUserForm.value.groupIds.length ||
      normalized.some((id, index) => id !== shareUserForm.value.groupIds[index])) {
    shareUserForm.value.groupIds = normalized
  }
})

onMounted(() => {
  try {
    sidePanelCollapsed.value = localStorage.getItem(SIDEBAR_COLLAPSED_STORAGE_KEY) === '1'
  } catch {
    sidePanelCollapsed.value = false
  }
  syncViewportMode()
  window.addEventListener('resize', syncViewportMode, { passive: true })
})

onMounted(() => {
  if (loggedIn.value) {
    fetchQuota()
    fetchUserInfo()
    void (async () => {
      await fetchAssetSpaces()
      await restoreView()
    })()
  }
})

function handleGroupsRefresh() {
  groupStore.fetchGroups()
}

function handleExternalNavigate(event: Event) {
  const customEvent = event as CustomEvent<{
    view?: ViewKey
    section?: ManagementSection
  }>
  const view = customEvent?.detail?.view
  if (!view) return
  if (view === 'quotaManage') {
    enterQuotaManage(customEvent.detail?.section || 'account')
    return
  }
  if (view === 'sharedWithMe') {
    void returnToSharedWithMe()
    return
  }
  if (view === 'group') {
    enterGroupView()
    return
  }
  if (view === 'help') {
    enterHelpGuide()
    return
  }
}

function handleAuthChanged(): void {
  loggedIn.value = isLoggedIn()
  if (!loggedIn.value) {
    loading.value = false
    fileList.value = []
    selectedFileRows.value = []
    fileSelectionNonce.value += 1
    userInfo.value = null
    quota.value = { quota: 0, used: 0, available: 0, percentage: 0, unlimited: true }
    showRecycle.value = false
    showShare.value = false
    showSharedWithMe.value = false
    showQuotaManage.value = false
    showGroupView.value = false
    showHelp.value = false
    detailDrawerVisible.value = false
    walletLoginSubmitting.value = false
  }
}

onMounted(() => {
  window.addEventListener('warehouse:navigate', handleExternalNavigate as EventListener)
  window.addEventListener('warehouse:groups-refresh', handleGroupsRefresh as EventListener)
  window.addEventListener('warehouse:upload-task-retry', handleUploadTaskRetryEvent as EventListener)
  window.addEventListener('warehouse:upload-task-open', handleUploadTaskOpenEvent as EventListener)
  window.addEventListener(AUTH_CHANGED_EVENT, handleAuthChanged as EventListener)
})

onBeforeUnmount(() => {
  window.removeEventListener('warehouse:navigate', handleExternalNavigate as EventListener)
  window.removeEventListener('warehouse:groups-refresh', handleGroupsRefresh as EventListener)
  window.removeEventListener('warehouse:upload-task-retry', handleUploadTaskRetryEvent as EventListener)
  window.removeEventListener('warehouse:upload-task-open', handleUploadTaskOpenEvent as EventListener)
  window.removeEventListener(AUTH_CHANGED_EVENT, handleAuthChanged as EventListener)
})

onMounted(() => {
  stopWalletProviderWatch = watchWalletProvider((present) => {
    walletPresent.value = present
  })
  void loadCipherSuiteOptions()
  if (!loggedIn.value && loginMode.value === 'identity') {
    void refreshIdentityLogin()
  }
  window.addEventListener('storage', handleIdentityStorage)
  window.addEventListener('message', handleIdentityMessage)
  if ('BroadcastChannel' in window) {
    identityBroadcastChannel = new BroadcastChannel('warehouse-identity-login')
    identityBroadcastChannel.onmessage = event => handleIdentityCallbackEvent(event.data)
  }
})

onBeforeUnmount(() => {
  window.removeEventListener('resize', syncViewportMode)
  window.removeEventListener('storage', handleIdentityStorage)
  window.removeEventListener('message', handleIdentityMessage)
  identityBroadcastChannel?.close()
  identityBroadcastChannel = null
  clearIdentityTimer()
  stopWalletProviderWatch?.()
})
</script>

<template>
  <div class="home-container">
    <!-- 未登录状态 -->
    <div v-if="!loggedIn" class="login-page">
      <div class="login-floating-container">
        <div class="login-hero">
          <div class="login-card" :class="{ 'is-wallet-mode': loginMode === 'wallet', 'is-identity-mode': loginMode === 'identity' }">
            <div class="login-card-switch">
              <el-tooltip :content="nextLoginModeLabel" placement="left">
                <el-button class="login-mode-button" :aria-label="nextLoginModeLabel" @click="switchLoginMode(nextLoginMode)">
                  <el-icon>
                    <component :is="nextLoginModeIcon" />
                  </el-icon>
                </el-button>
              </el-tooltip>
            </div>
            <div class="login-brand">
              <div>
                <h1>Warehouse</h1>
                <p>{{ loginSubtitle }}</p>
              </div>
            </div>

            <transition name="login-mode" mode="out-in">
              <div v-if="loginMode === 'identity'" key="identity" class="login-identity-panel">
                <button type="button" class="login-qrcode-frame" :disabled="identityLoading" @click="refreshIdentityLogin">
                  <img v-if="identityQrcodeImage" :src="identityQrcodeImage" alt="通行证登录二维码" />
                  <span v-else class="login-qrcode-placeholder">{{ identityLoading ? '加载中' : '刷新二维码' }}</span>
                </button>
                <div v-if="identityStatusText" class="login-status-line">
                  {{ identityStatusText }}
                </div>
                <div class="login-identity-actions">
                  <el-button text @click="openIdentityAuthorize">无法扫码？使用本机通行证登录</el-button>
                </div>
              </div>

              <div v-else key="wallet" class="login-wallet-panel">
                <template v-if="walletPresent">
                  <el-button
                    type="primary"
                    class="login-main-btn login-wallet-btn"
                    :loading="walletLoginSubmitting"
                    @click="handleWalletLogin"
                  >
                    钱包登录
                  </el-button>
                </template>
                <div v-else class="login-warning">未检测到钱包插件</div>
              </div>
            </transition>

            <div class="login-bottom-link">
              <a href="https://www.yeying.pub" target="_blank" rel="noopener noreferrer">了解夜莺社区</a>
            </div>
          </div>
        </div>
      </div>
    </div>

    <!-- 已登录状态 -->
    <div v-else class="app-shell" :class="{ 'is-side-collapsed': sidePanelCollapsed }">
      <aside class="side-panel">
        <div class="brand">
          <div class="brand-main">
            <div class="brand-text" v-show="!sidePanelCollapsed">
              <div class="brand-sub">资产管理中心</div>
            </div>
          </div>
          <el-tooltip :content="sidePanelCollapsed ? '展开侧栏' : '收起侧栏'" placement="right">
            <el-button class="side-toggle-btn" circle size="small" @click="toggleSidePanel">
              <el-icon>
                <component :is="sidePanelCollapsed ? Expand : Fold" />
              </el-icon>
            </el-button>
          </el-tooltip>
        </div>

        <div class="nav-block">
          <div class="nav-group">
            <div class="nav-group-title" v-show="!sidePanelCollapsed">我的资产</div>
            <div class="asset-nav-list" v-if="assetSpaces.length">
              <el-tooltip
                v-for="space in assetSpaces"
                :key="space.key"
                :content="space.name"
                placement="right"
                :disabled="!sidePanelCollapsed"
              >
                <button
                  type="button"
                  class="asset-nav-item"
                  :class="{ active: isFileView && currentAssetSpaceKey === space.key }"
                  @click="enterAssetSpace(space.key)"
                >
                  <div class="asset-nav-main">
                    <el-icon class="nav-icon">
                      <component :is="getAssetSpaceIcon(space.key)" />
                    </el-icon>
                    <span v-show="!sidePanelCollapsed">{{ space.name }}</span>
                  </div>
                </button>
              </el-tooltip>
              <el-tooltip content="回收站" placement="right" :disabled="!sidePanelCollapsed">
                <button
                  type="button"
                  class="asset-nav-item"
                  :class="{ active: showRecycle }"
                  @click="enterRecycle"
                >
                  <div class="asset-nav-main">
                    <el-icon class="nav-icon"><Delete /></el-icon>
                    <span v-show="!sidePanelCollapsed">回收站</span>
                  </div>
                </button>
              </el-tooltip>
            </div>
          </div>

          <div class="nav-group">
            <div class="nav-group-title" v-show="!sidePanelCollapsed">分享与协作</div>
            <div class="nav-list">
              <el-tooltip content="分享" placement="right" :disabled="!sidePanelCollapsed">
                <button
                  type="button"
                  class="nav-item"
                  :class="{ active: showShare }"
                  @click="enterShare()"
                >
                  <el-icon class="nav-icon"><Share /></el-icon>
                  <span v-show="!sidePanelCollapsed">分享</span>
                </button>
              </el-tooltip>
              <el-tooltip content="收到的分享" placement="right" :disabled="!sidePanelCollapsed">
                <button
                  type="button"
                  class="nav-item"
                  :class="{ active: showSharedWithMe }"
                  @click="returnToSharedWithMe"
                >
                  <el-icon class="nav-icon"><DocumentCopy /></el-icon>
                  <span v-show="!sidePanelCollapsed">收到的分享</span>
                </button>
              </el-tooltip>
            </div>
          </div>

          <div class="nav-group">
            <div class="nav-group-title" v-show="!sidePanelCollapsed">管理</div>
            <div class="nav-list">
              <el-tooltip content="我的资料" placement="right" :disabled="!sidePanelCollapsed">
                <button
                  type="button"
                  class="nav-item"
                  :class="{ active: showQuotaManage && managementSection === 'account' }"
                  @click="enterQuotaManage('account')"
                >
                  <el-icon class="nav-icon"><User /></el-icon>
                  <span v-show="!sidePanelCollapsed">我的资料</span>
                </button>
              </el-tooltip>
              <el-tooltip content="密钥管理" placement="right" :disabled="!sidePanelCollapsed">
                <button
                  type="button"
                  class="nav-item"
                  :class="{ active: showQuotaManage && managementSection === 'keys' }"
                  @click="enterQuotaManage('keys')"
                >
                  <el-icon class="nav-icon"><DocumentCopy /></el-icon>
                  <span v-show="!sidePanelCollapsed">密钥管理</span>
                </button>
              </el-tooltip>
              <el-tooltip v-if="canManageUsers" content="用户管理" placement="right" :disabled="!sidePanelCollapsed">
                <button
                  type="button"
                  class="nav-item"
                  :class="{ active: showQuotaManage && managementSection === 'adminUsers' }"
                  @click="enterQuotaManage('adminUsers')"
                >
                  <el-icon class="nav-icon"><Grid /></el-icon>
                  <span v-show="!sidePanelCollapsed">用户管理</span>
                </button>
              </el-tooltip>
              <el-tooltip content="分组管理" placement="right" :disabled="!sidePanelCollapsed">
                <button
                  type="button"
                  class="nav-item"
                  :class="{ active: showQuotaManage && managementSection === 'group' }"
                  @click="enterQuotaManage('group')"
                >
                  <el-icon class="nav-icon"><Notebook /></el-icon>
                  <span v-show="!sidePanelCollapsed">分组管理</span>
                </button>
              </el-tooltip>
            </div>
          </div>

          <div class="nav-group">
            <div class="nav-group-title" v-show="!sidePanelCollapsed">帮助</div>
            <div class="nav-list">
              <el-tooltip content="使用指南" placement="right" :disabled="!sidePanelCollapsed">
                <button
                  type="button"
                  class="nav-item"
                  :class="{ active: showHelp }"
                  @click="enterHelpGuide"
                >
                  <el-icon class="nav-icon"><Notebook /></el-icon>
                  <span v-show="!sidePanelCollapsed">使用指南</span>
                </button>
              </el-tooltip>
            </div>
          </div>
        </div>
      </aside>

      <main class="main-panel">
        <section
          class="content-card"
          @dragenter="handleDragEnter"
          @dragleave="handleDragLeave"
          @dragover="handleDragOver"
          @drop="handleDrop"
        >
          <div v-if="dragActive && canUpload" class="drop-mask">
            <div class="drop-hint">拖拽文件或文件夹到此处上传（支持空目录）</div>
          </div>
          <div v-if="showListHeader" class="list-header" :class="{ 'list-header-recycle': showRecycle }">
            <div class="list-header-left">
              <div class="path-row">
              <template v-if="showSharedWithMe && sharedActive">
                <el-tooltip content="返回共享列表" placement="top">
                  <el-button circle :icon="ArrowLeft" @click="backToSharedList" />
                </el-tooltip>
                <el-tooltip content="返回上级" placement="top">
                  <el-button
                    circle
                    :icon="ArrowUp"
                    :class="{ 'is-drop-target': isSharedParentButtonMoveTarget() }"
                    @click="goSharedParent"
                    :disabled="sharedPath === '/'"
                    @dragenter="handleSharedParentButtonDragEnter"
                    @dragover="handleSharedParentButtonDragOver"
                    @dragleave="handleSharedParentButtonDragLeave"
                    @drop="handleSharedParentButtonDrop"
                  />
                </el-tooltip>
              </template>
              <template v-else-if="showRecycle || showShare || showSharedWithMe">
                <el-tooltip content="返回文件列表" placement="top">
                  <el-button circle :icon="ArrowLeft" @click="backToFiles" />
                </el-tooltip>
              </template>
              <template v-else>
                <el-tooltip content="返回上级" placement="top">
                  <el-button
                    circle
                    :icon="ArrowUp"
                    :class="{ 'is-drop-target': isParentButtonMoveTarget() }"
                    @click="goParent"
                    :disabled="!canGoFileParent"
                    @dragenter="handleParentButtonDragEnter"
                    @dragover="handleParentButtonDragOver"
                    @dragleave="handleParentButtonDragLeave"
                    @drop="handleParentButtonDrop"
                  />
                </el-tooltip>
              </template>
                <template v-if="showRecycle">
                  <div class="section-title-row">
                    <span class="section-title">回收站</span>
                    <span class="section-subtitle">已删除资产可在此恢复或彻底清理</span>
                  </div>
                </template>
                <template v-else-if="showShare">
                  <div class="path-pill">
                    <span class="path-label">当前位置</span>
                    <span class="path-value">分享</span>
                  </div>
                  <div class="view-segment">
                    <el-tooltip
                      content="生成公开访问链接，适合临时下载或外部访问。创建后会直接得到可复制的链接地址。"
                      placement="bottom"
                      :show-after="300"
                    >
                      <button
                        type="button"
                        class="segment-button"
                        :class="{ active: shareTab === 'link' }"
                        @click="switchShareTab('link')"
                      >
                        分享链接
                      </button>
                    </el-tooltip>
                    <el-tooltip
                      content="把目录或文件共享给当前账号体系下的用户、地址或分组，适合协作访问。"
                      placement="bottom"
                      :show-after="300"
                    >
                      <button
                        type="button"
                        class="segment-button"
                        :class="{ active: shareTab === 'direct' }"
                        @click="switchShareTab('direct')"
                      >
                        分享对象
                      </button>
                    </el-tooltip>
                  </div>
                </template>
                <template v-else-if="showSharedWithMe">
                  <template v-if="sharedActive">
                    <div class="breadcrumb">
                      <el-breadcrumb separator="/">
                        <el-breadcrumb-item>
                          <button
                            class="breadcrumb-link"
                            :class="{ 'is-drop-target': isSharedBreadcrumbMoveTarget('/') }"
                            type="button"
                            @click="setSharedPath('/')"
                            @dragenter="handleSharedBreadcrumbDragEnter($event, '/')"
                            @dragover="handleSharedBreadcrumbDragOver($event, '/')"
                            @dragleave="handleSharedBreadcrumbDragLeave($event, '/')"
                            @drop="handleSharedBreadcrumbDrop($event, '/')"
                          >
                            {{ sharedActive.name }}
                          </button>
                        </el-breadcrumb-item>
                        <el-breadcrumb-item v-for="crumb in sharedBreadcrumbItems" :key="crumb.path">
                          <button
                            class="breadcrumb-link"
                            :class="{ 'is-drop-target': isSharedBreadcrumbMoveTarget(crumb.path) }"
                            type="button"
                            @click="setSharedPath(crumb.path)"
                            @dragenter="handleSharedBreadcrumbDragEnter($event, crumb.path)"
                            @dragover="handleSharedBreadcrumbDragOver($event, crumb.path)"
                            @dragleave="handleSharedBreadcrumbDragLeave($event, crumb.path)"
                            @drop="handleSharedBreadcrumbDrop($event, crumb.path)"
                          >
                            {{ crumb.name }}
                          </button>
                        </el-breadcrumb-item>
                      </el-breadcrumb>
                      <el-tooltip content="复制路径" placement="top">
                        <button type="button" class="copy-icon" @click="copyCurrentPath">
                          <el-icon><DocumentCopy /></el-icon>
                        </button>
                      </el-tooltip>
                    </div>
                  </template>
                  <template v-else>
                    <div class="path-pill">
                      <span class="path-label">当前位置</span>
                      <span class="path-value">定向分享</span>
                    </div>
                  </template>
                </template>
                <template v-else>
                  <div class="breadcrumb">
                    <el-breadcrumb separator="/">
                      <el-breadcrumb-item>
                        <button
                          class="breadcrumb-link"
                          :class="{ 'is-drop-target': isBreadcrumbMoveTarget(fileBreadcrumbRoot.path) }"
                          type="button"
                          @click="fetchFiles(fileBreadcrumbRoot.path)"
                          @dragenter="handleBreadcrumbDragEnter($event, fileBreadcrumbRoot.path)"
                          @dragover="handleBreadcrumbDragOver($event, fileBreadcrumbRoot.path)"
                          @dragleave="handleBreadcrumbDragLeave($event, fileBreadcrumbRoot.path)"
                          @drop="handleBreadcrumbDrop($event, fileBreadcrumbRoot.path)"
                        >
                          {{ fileBreadcrumbRoot.name }}
                        </button>
                      </el-breadcrumb-item>
                      <el-breadcrumb-item v-for="crumb in breadcrumbItems" :key="crumb.path">
                        <button
                          class="breadcrumb-link"
                          :class="{ 'is-drop-target': isBreadcrumbMoveTarget(crumb.path) }"
                          type="button"
                          @click="fetchFiles(crumb.path)"
                          @dragenter="handleBreadcrumbDragEnter($event, crumb.path)"
                          @dragover="handleBreadcrumbDragOver($event, crumb.path)"
                          @dragleave="handleBreadcrumbDragLeave($event, crumb.path)"
                          @drop="handleBreadcrumbDrop($event, crumb.path)"
                        >
                          {{ crumb.name }}
                        </button>
                      </el-breadcrumb-item>
                    </el-breadcrumb>
                    <el-tooltip content="复制路径" placement="top">
                      <button type="button" class="copy-icon" @click="copyCurrentPath">
                        <el-icon><DocumentCopy /></el-icon>
                      </button>
                    </el-tooltip>
                  </div>
                  <div v-if="currentEncryptedRoot" class="crypto-pill">
                    <el-icon class="crypto-pill-icon">
                      <component :is="currentEncryptedDirectoryPasswordCached ? Unlock : Lock" />
                    </el-icon>
                    <span class="crypto-pill-label">加密目录</span>
                    <span class="crypto-pill-text">{{ currentEncryptedDirectoryStatusText }}</span>
                    <div class="crypto-pill-actions">
                      <el-button
                        v-if="currentEncryptedDirectoryRequiresPassword"
                        size="small"
                        text
                        type="primary"
                        @click="unlockCurrentEncryptedDirectory(currentEncryptedDirectoryPasswordCached)"
                      >
                        {{ currentEncryptedDirectoryPasswordCached ? '重新输入密码' : '解锁' }}
                      </el-button>
                      <el-button
                        v-if="currentEncryptedDirectoryRequiresPassword && currentEncryptedDirectoryPasswordCached"
                        size="small"
                        text
                        @click="clearCurrentEncryptedDirectoryPassword"
                      >
                        清除缓存
                      </el-button>
                    </div>
                  </div>
                </template>
              </div>
            </div>
            <div class="list-header-right">
              <div v-if="showSearch" class="header-search">
                <el-input
                  v-model="searchKeyword"
                  clearable
                  :placeholder="searchPlaceholder"
                >
                  <template #prefix>
                    <el-icon><Search /></el-icon>
                  </template>
                </el-input>
              </div>
              <el-tooltip v-if="showListHeader && singleRefreshAction" content="刷新" placement="top">
                <el-button
                  class="mobile-only refresh-button"
                  circle
                  :icon="Refresh"
                  :disabled="singleRefreshAction?.disabled"
                  :class="{ 'is-refreshing': singleRefreshAction?.disabled }"
                  @click="refreshCurrentView"
                />
              </el-tooltip>
              <el-dropdown
                v-if="showListHeader && showMobileMenu"
                class="mobile-only mobile-menu-button"
                trigger="click"
                @command="handleMobileAction"
              >
                <el-button circle :icon="MoreFilled" />
                <template #dropdown>
                  <el-dropdown-menu>
                    <template v-for="(group, groupIndex) in mobileActionGroups" :key="group.title">
                      <el-dropdown-item
                        v-for="(item, itemIndex) in group.items"
                        :key="item.command"
                        :command="item.command"
                        :disabled="item.disabled"
                        :divided="groupIndex > 0 && itemIndex === 0"
                        class="mobile-menu-item"
                        :title="item.label"
                      >
                        <el-icon>
                          <component :is="getMobileActionIcon(item.command)" />
                        </el-icon>
                      </el-dropdown-item>
                    </template>
                  </el-dropdown-menu>
                </template>
              </el-dropdown>
              <div class="list-actions desktop-only">
                <template v-if="showRecycle">
                  <el-tooltip content="清空回收站" placement="top">
                    <el-button
                      type="danger"
                      circle
                      :loading="recycleClearing"
                      :disabled="recycleLoading || recycleClearing"
                      @click="clearRecycle"
                    >
                      <el-icon><Delete /></el-icon>
                    </el-button>
                  </el-tooltip>
                  <el-tooltip content="刷新" placement="top">
                    <el-button
                      class="refresh-button"
                      circle
                      :disabled="recycleLoading || recycleClearing"
                      :class="{ 'is-refreshing': recycleLoading }"
                      @click="refreshCurrentView"
                    >
                      <el-icon><Refresh /></el-icon>
                    </el-button>
                  </el-tooltip>
                </template>
                <template v-else-if="showShare">
                  <el-tooltip content="刷新" placement="top">
                    <el-button
                      class="refresh-button"
                      circle
                      :disabled="shareTab === 'link' ? shareLoading : directShareLoading"
                      :class="{ 'is-refreshing': shareTab === 'link' ? shareLoading : directShareLoading }"
                      @click="refreshCurrentView"
                    >
                      <el-icon><Refresh /></el-icon>
                    </el-button>
                  </el-tooltip>
                </template>
                <template v-else-if="showSharedWithMe">
                  <template v-if="sharedActive">
                    <el-tooltip v-if="sharedCanCreate" content="新建文件夹" placement="top">
                      <el-button circle type="primary" :icon="FolderAdd" @click="createFolder" />
                    </el-tooltip>
                    <el-tooltip v-if="sharedCanCreate" content="上传文件" placement="top">
                      <el-button circle type="primary" :icon="Upload" @click="triggerUpload" />
                    </el-tooltip>
                    <el-tooltip v-if="sharedCanCreate" content="上传目录" placement="top">
                      <el-button circle type="primary" :icon="FolderOpened" @click="triggerDirectoryUpload" />
                    </el-tooltip>
                    <el-tooltip content="刷新" placement="top">
                      <el-button
                        class="refresh-button"
                        circle
                        :disabled="sharedEntriesLoading"
                        :class="{ 'is-refreshing': sharedEntriesLoading }"
                        @click="refreshCurrentView"
                      >
                        <el-icon><Refresh /></el-icon>
                      </el-button>
                    </el-tooltip>
                    <input
                      ref="fileInput"
                      type="file"
                      multiple
                      style="display:none"
                      @change="handleFileSelect"
                    />
                    <input
                      ref="directoryInput"
                      type="file"
                      webkitdirectory
                      directory
                      multiple
                      style="display:none"
                      @change="handleFileSelect"
                    />
                  </template>
                  <template v-else>
                    <el-tooltip content="刷新" placement="top">
                      <el-button
                        class="refresh-button"
                        circle
                        :disabled="sharedWithMeLoading"
                        :class="{ 'is-refreshing': sharedWithMeLoading }"
                        @click="refreshCurrentView"
                      >
                        <el-icon><Refresh /></el-icon>
                      </el-button>
                    </el-tooltip>
                  </template>
                </template>
                <template v-else>
                  <div class="action-cluster">
                    <el-tooltip content="新建文件夹" placement="top">
                      <el-button circle type="primary" :icon="FolderAdd" @click="createFolder" />
                    </el-tooltip>
                    <el-tooltip content="上传文件" placement="top">
                      <el-button circle type="primary" :icon="Upload" @click="triggerUpload" />
                    </el-tooltip>
                    <el-tooltip content="上传目录" placement="top">
                      <el-button circle type="primary" :icon="FolderOpened" @click="triggerDirectoryUpload" />
                    </el-tooltip>
                  </div>
                  <div class="action-cluster action-cluster-subtle">
                    <el-tooltip :content="fileSelectionMode ? (selectedFileCount > 0 ? `删除已选 ${selectedFileCount} 项` : '勾选要删除的资产') : '批量删除'" placement="top">
                      <el-button
                        type="danger"
                        circle
                        :disabled="loading || (fileSelectionMode && selectedFileCount === 0)"
                        @click="handleBatchDeleteButtonClick"
                      >
                        <el-icon><Delete /></el-icon>
                      </el-button>
                    </el-tooltip>
                    <el-tooltip content="刷新" placement="top">
                      <el-button
                        class="refresh-button"
                        circle
                        :disabled="loading"
                        :class="{ 'is-refreshing': loading }"
                        @click="refreshCurrentView"
                      >
                        <el-icon><Refresh /></el-icon>
                      </el-button>
                    </el-tooltip>
                  </div>
                  <input
                    ref="fileInput"
                    type="file"
                    multiple
                    style="display:none"
                    @change="handleFileSelect"
                  />
                  <input
                    ref="directoryInput"
                    type="file"
                    webkitdirectory
                    directory
                    multiple
                    style="display:none"
                    @change="handleFileSelect"
                  />
                </template>
              </div>
            </div>
          </div>
          <div v-if="showQuotaManage" class="content-body content-scroll" v-loading="quotaManageLoading && !manualRefresh">
            <div class="user-center">
              <div v-if="managementSection === 'account'" class="user-card">
                <div class="card-head">
                  <div class="card-title">基础信息</div>
                  <el-tooltip content="刷新" placement="top">
                    <el-button
                      class="refresh-button"
                      circle
                      size="small"
                      :icon="Refresh"
                      :disabled="quotaManageLoading"
                      :class="{ 'is-refreshing': quotaManageLoading }"
                      @click="refreshCurrentView"
                    />
                  </el-tooltip>
                </div>
                <div class="user-list">
                  <div class="user-row">
                    <span class="user-label">用户名</span>
                    <span class="user-value">{{ userProfile.username }}</span>
                  </div>
                  <div class="user-row">
                    <span class="user-label">钱包地址</span>
                    <span class="user-value mono">{{ userProfile.walletAddress }}</span>
                  </div>
                  <div class="user-row">
                    <span class="user-label">钱包类型</span>
                    <span class="user-value">{{ userProfile.walletName }}</span>
                  </div>
                  <div class="user-row">
                    <span class="user-label">权限</span>
                    <span class="user-value">
                      <span v-if="!userProfile.permissions.length">-</span>
                      <span v-else class="user-tags">
                        <el-tag v-for="permission in userProfile.permissions" :key="permission" size="small" type="info">
                          {{ permission }}
                        </el-tag>
                      </span>
                    </span>
                  </div>
                  <div class="user-row">
                    <span class="user-label">注册时间</span>
                    <span class="user-value">
                      {{ userProfile.createdAt ? formatTime(userProfile.createdAt) : '-' }}
                    </span>
                  </div>
                  <div class="user-row">
                    <span class="user-label">登录密码</span>
                    <div class="user-value user-inline">
                      <span class="user-text">{{ userProfile.hasPassword ? '已设置' : '未设置' }}</span>
                      <div class="user-actions">
                        <el-button size="small" class="access-key-ghost-button" @click="openPasswordDialog">
                          {{ userProfile.hasPassword ? '修改' : '设置' }}
                        </el-button>
                      </div>
                    </div>
                  </div>
                </div>
              </div>
              <div v-if="managementSection === 'account'" class="user-card" :class="`quota-card quota-card-${quotaAlertState.level}`">
                <div class="card-title">当前额度</div>
                <div class="quota-value">
                  <span>{{ formatSize(quota.used) }}</span>
                  <span class="quota-sep">/</span>
                  <span>{{ quota.unlimited ? '无限' : formatSize(quota.quota) }}</span>
                </div>
                <el-progress
                  v-if="!quota.unlimited"
                  :percentage="Math.min(Number(quota.percentage.toFixed(2)), 100)"
                  :stroke-width="8"
                  :status="quotaAlertState.level === 'danger' ? 'exception' : undefined"
                />
                <div class="quota-meta">
                  <span v-if="quota.unlimited">未设置上限</span>
                  <span v-else>已使用 {{ quota.percentage.toFixed(2) }}%</span>
                  <el-tag size="small" :type="quotaAlertState.level === 'danger' ? 'danger' : quotaAlertState.level === 'warning' ? 'warning' : 'info'">
                    {{ quotaAlertState.label }}
                  </el-tag>
                </div>
                <el-alert
                  :type="quotaAlertState.level === 'danger' ? 'error' : quotaAlertState.level"
                  :closable="false"
                  show-icon
                  :title="quotaAlertState.message"
                />
                <div class="quota-grid">
                  <div class="quota-item">
                    <span class="quota-label">已使用</span>
                    <span class="quota-amount">{{ formatSize(quota.used) }}</span>
                  </div>
                  <div class="quota-item">
                    <span class="quota-label">可用</span>
                    <span class="quota-amount">
                      {{ quota.unlimited ? '无限' : formatSize(quotaAvailable ?? 0) }}
                    </span>
                  </div>
                  <div class="quota-item">
                    <span class="quota-label">总额度</span>
                    <span class="quota-amount">{{ quota.unlimited ? '无限' : formatSize(quota.quota) }}</span>
                  </div>
                  <div class="quota-item">
                    <span class="quota-label">使用率</span>
                    <span class="quota-amount">{{ quota.unlimited ? '-' : `${quota.percentage.toFixed(2)}%` }}</span>
                  </div>
                </div>
              </div>
              <div
                v-if="canManageUsers && managementSection === 'adminUsers'"
                class="user-card user-card-full"
                v-loading="adminUsersLoading"
              >
                <div class="card-head">
                  <div class="card-title">用户管理概览</div>
                  <div class="user-actions">
                    <el-select v-model="adminUsersFilter" size="small" class="admin-quota-filter">
                      <el-option label="全部用户" value="all" />
                      <el-option label="不限额" value="unlimited" />
                      <el-option label="接近上限" value="near_limit" />
                      <el-option label="已超额" value="over_quota" />
                    </el-select>
                    <el-button size="small" @click="fetchAdminUsers">刷新</el-button>
                  </div>
                </div>
                <div class="key-summary">
                  <span>总用户：{{ adminUsersSummary.total }}</span>
                  <span>已设上限：{{ adminUsersSummary.limited }}</span>
                  <span>不限额：{{ adminUsersSummary.unlimited }}</span>
                  <span>接近上限：{{ adminUsersSummary.nearLimit }}</span>
                  <span>已超额：{{ adminUsersSummary.overQuota }}</span>
                </div>
                <el-alert
                  v-if="adminUsersHeadline"
                  :type="adminUsersHeadline.type"
                  :closable="false"
                  show-icon
                  :title="adminUsersHeadline.title"
                />
                <el-alert
                  v-if="adminUsersError"
                  type="warning"
                  :closable="false"
                  show-icon
                  :title="`用户管理列表加载失败：${adminUsersError}`"
                />
                <el-empty
                  v-else-if="!adminUsersLoading && !filteredAdminUsers.length"
                  description="当前筛选条件下暂无用户"
                />
                <el-table
                  v-else
                  :data="filteredAdminUsers"
                  class="admin-quota-table"
                  max-height="320"
                  size="small"
                >
                  <el-table-column prop="username" label="用户名" min-width="140" />
                  <el-table-column label="已使用" min-width="120">
                    <template #default="{ row }">
                      {{ formatSize(row.used_space) }}
                    </template>
                  </el-table-column>
                  <el-table-column label="总额度" min-width="120">
                    <template #default="{ row }">
                      {{ row.quota > 0 ? formatSize(row.quota) : '无限' }}
                    </template>
                  </el-table-column>
                  <el-table-column label="使用率" min-width="110">
                    <template #default="{ row }">
                      {{ formatAdminUserUsage(row) }}
                    </template>
                  </el-table-column>
                  <el-table-column label="状态" min-width="110">
                    <template #default="{ row }">
                      <el-tag size="small" :type="adminUserQuotaStatus(row).type">
                        {{ adminUserQuotaStatus(row).label }}
                      </el-tag>
                    </template>
                  </el-table-column>
                  <el-table-column label="操作" width="120" align="left" header-align="left">
                    <template #default="{ row }">
                      <el-button size="small" @click="openAdminUsersDialog(row)">修改额度</el-button>
                    </template>
                  </el-table-column>
                </el-table>
              </div>
              <div v-if="managementSection === 'keys'" class="user-card user-card-full">
                <div class="card-head">
                  <div>
                    <div class="card-title">密钥管理</div>
                    <div class="card-subtitle">按协议分别管理 WebDAV 密钥和 S3 凭证</div>
                  </div>
                </div>
                <el-tabs v-model="credentialTab" class="credential-tabs">
                  <el-tab-pane label="WebDAV 凭证" name="webdav">
                    <div class="credential-tab-head">
                      <div class="card-subtitle">用于 Finder、Windows 网络位置、rclone 等 WebDAV 客户端</div>
                      <div class="user-actions">
                        <el-button size="small" @click="fetchAccessKeys(true)">刷新</el-button>
                        <el-button size="small" type="primary" @click="openAccessKeyDialogFromUserCenter">新建</el-button>
                      </div>
                    </div>
                    <div class="credential-tab-body" v-loading="accessKeyLoading">
                      <el-empty v-if="!accessKeys.length && !accessKeyLoading" description="暂无 WebDAV 密钥" />
                      <el-table v-else :data="accessKeys" size="small">
                        <el-table-column prop="name" label="名称" min-width="160" />
                        <el-table-column prop="keyId" label="Key ID" min-width="230">
                          <template #default="{ row }"><span class="mono">{{ row.keyId }}</span></template>
                        </el-table-column>
                        <el-table-column label="绑定目录" min-width="220">
                          <template #default="{ row }">
                            <div v-if="(row.bindingPaths || []).length" class="key-meta-row">
                              <el-tag
                                v-for="path in (row.bindingPaths || []).slice(0, 3)"
                                :key="`${row.id}-${path}`"
                                size="small"
                                type="info"
                              >
                                {{ path }}
                              </el-tag>
                              <span v-if="(row.bindingPaths || []).length > 3" class="key-more-text">
                                +{{ (row.bindingPaths || []).length - 3 }}
                              </span>
                            </div>
                            <span v-else>-</span>
                          </template>
                        </el-table-column>
                        <el-table-column label="权限" min-width="220">
                          <template #default="{ row }">
                            <div class="key-meta-row">
                              <el-tag
                                v-for="permission in row.permissions"
                                :key="`${row.id}-${permission}`"
                                size="small"
                                type="info"
                              >
                                {{ formatSharePermission(permission) }}
                              </el-tag>
                            </div>
                          </template>
                        </el-table-column>
                        <el-table-column label="状态" width="100">
                          <template #default="{ row }">
                            <el-tag size="small" :type="row.status === 'active' ? 'success' : 'info'">{{ row.status === 'active' ? '生效中' : '已撤销' }}</el-tag>
                          </template>
                        </el-table-column>
                        <el-table-column label="操作" width="160">
                          <template #default="{ row }">
                            <el-button text size="small" @click="copyAccessKeyValue(row.keyId, 'Key ID')">复制 ID</el-button>
                            <el-button v-if="row.status === 'active'" text type="danger" size="small" @click="revokeAccessKey(row)">撤销</el-button>
                            <el-button v-else text type="danger" size="small" @click="deleteAccessKey(row)">删除</el-button>
                          </template>
                        </el-table-column>
                      </el-table>
                    </div>
                  </el-tab-pane>
                  <el-tab-pane label="S3 凭证" name="s3">
                    <div class="credential-tab-head">
                      <div class="card-subtitle">用于 mc、rclone、AWS CLI 等 S3 客户端</div>
                      <div class="user-actions">
                        <el-button size="small" @click="fetchS3Credentials(true)">刷新</el-button>
                        <el-button size="small" type="primary" @click="openS3CredentialDialog">新建</el-button>
                      </div>
                    </div>
                    <div class="credential-tab-body" v-loading="s3CredentialLoading">
                      <el-empty v-if="!s3Credentials.length && !s3CredentialLoading" description="暂无 S3 凭证" />
                      <el-table v-else :data="s3Credentials" size="small">
                        <el-table-column prop="name" label="名称" min-width="160" />
                        <el-table-column prop="accessKeyId" label="Access Key ID" min-width="230">
                          <template #default="{ row }"><span class="mono">{{ row.accessKeyId }}</span></template>
                        </el-table-column>
                        <el-table-column prop="rootPath" label="目录范围" min-width="140" />
                        <el-table-column prop="permissions" label="权限" min-width="220" />
                        <el-table-column label="状态" width="100">
                          <template #default="{ row }">
                            <el-tag size="small" :type="row.status === 'active' ? 'success' : 'info'">{{ row.status === 'active' ? '生效中' : '已撤销' }}</el-tag>
                          </template>
                        </el-table-column>
                        <el-table-column label="操作" width="120">
                          <template #default="{ row }">
                            <el-button v-if="row.status === 'active'" text type="danger" size="small" @click="revokeS3Credential(row)">撤销</el-button>
                            <el-button v-else text type="danger" size="small" @click="deleteS3Credential(row)">删除</el-button>
                          </template>
                        </el-table-column>
                      </el-table>
                    </div>
                  </el-tab-pane>
                  <el-tab-pane label="Warehouse Tool" name="tool" @click="fetchToolCredentials(true)">
                    <div class="credential-tab-head">
                      <div class="card-subtitle">用于 Chat、Agent 和 Skill 的受限资产 Tool 调用</div>
                      <div class="user-actions">
                        <el-button size="small" @click="fetchToolCredentials(true)">刷新</el-button>
                        <el-button size="small" @click="fetchToolAudits">审计</el-button>
                        <el-button size="small" type="primary" @click="openToolCredentialDialog">新建</el-button>
                      </div>
                    </div>
                    <div class="credential-tab-body" v-loading="toolCredentialLoading">
                      <el-empty v-if="!toolCredentials.length && !toolCredentialLoading" description="暂无 Warehouse Tool 凭证" />
                      <el-table v-else :data="toolCredentials" size="small">
                        <el-table-column prop="name" label="名称" min-width="150" />
                        <el-table-column label="授权范围" min-width="240"><template #default="{ row }"><div class="key-meta-row"><el-tag v-for="path in row.pathPrefixes" :key="path" size="small" type="info">{{ path }}</el-tag></div></template></el-table-column>
                        <el-table-column prop="expiresAt" label="过期时间" min-width="180" />
                        <el-table-column label="状态" width="100"><template #default="{ row }"><el-tag size="small" :type="row.status === 'active' ? 'success' : 'info'">{{ row.status === 'active' ? '生效中' : '已撤销' }}</el-tag></template></el-table-column>
                        <el-table-column label="操作" width="180"><template #default="{ row }"><el-button v-if="row.status === 'active'" text size="small" @click="rotateToolCredential(row)">轮换</el-button><el-button v-if="row.status === 'active'" text type="danger" size="small" @click="revokeToolCredential(row)">撤销</el-button></template></el-table-column>
                      </el-table>
                      <el-table v-if="toolAudits.length" :data="toolAudits" size="small" class="tool-audit-table" v-loading="toolAuditLoading">
                        <el-table-column prop="createdAt" label="时间" width="180" /><el-table-column prop="toolName" label="Tool" min-width="180" /><el-table-column prop="path" label="路径" min-width="240" /><el-table-column prop="outcome" label="结果" width="100" />
                      </el-table>
                    </div>
                  </el-tab-pane>
                </el-tabs>
              </div>
              <div v-if="managementSection === 'group'" class="user-card user-card-full" v-loading="groupLoading && !manualRefresh">
                <GroupView embedded @refresh="refreshCurrentView" />
              </div>
            </div>
          </div>
          <div v-else-if="showGroupView" class="content-body content-scroll" v-loading="groupLoading && !manualRefresh">
            <GroupView @refresh="refreshCurrentView" />
          </div>
          <div v-else-if="showHelp" class="content-body content-scroll">
            <div class="help-guide">
              <article class="help-guide-content" v-html="helpGuideHtml"></article>
            </div>
          </div>
          <div v-else class="content-body table-wrapper">
            <div v-if="shareViewSummary" class="view-summary-bar">
              <div class="view-summary-title">{{ shareViewSummary.title }}</div>
              <div class="view-summary-text">{{ shareViewSummary.description }}</div>
            </div>
            <div v-if="fileSelectionMode" class="selection-summary-bar">
              <div class="selection-summary-main">
                <span class="selection-summary-title">已进入批量操作模式</span>
                <span class="selection-summary-text">{{ fileSelectionSummaryText }}</span>
              </div>
              <div class="selection-summary-actions">
                <el-button size="small" @click="exitFileSelectionMode">退出批量</el-button>
                <el-button size="small" type="danger" :disabled="selectedFileCount === 0 || loading" @click="deleteSelectedFiles">删除已选</el-button>
              </div>
            </div>
            <template v-if="showRecycle">
              <section class="recycle-panel">
                <div class="recycle-summary-bar">
                  <div class="recycle-summary-main">
                    <span class="recycle-summary-title">删除记录</span>
                    <span class="recycle-summary-text">按删除时间倒序展示，可恢复或彻底删除。</span>
                  </div>
                  <div class="recycle-summary-meta">
                    <span class="recycle-summary-chip">当前 {{ filteredRecycleList.length }} 项</span>
                    <span class="recycle-summary-chip">总计 {{ recycleTotal }} 项</span>
                  </div>
                </div>
                <RecycleTableView
                  :is-mobile="isMobileViewport"
                  :rows="filteredRecycleList"
                  :loading="recycleLoading && !manualRefresh"
                  :on-row-click="handleRowClick"
                  :format-recycle-full-path="formatRecycleFullPath"
                  :format-recycle-location="formatRecycleLocation"
                  :format-size="formatSize"
                  :format-deleted-time="formatDeletedTime"
                  :recover-file="recoverFile"
                  :permanently-delete="permanentlyDelete"
                />
                <div v-if="recycleTotal > recyclePageSize" class="table-pagination recycle-pagination">
                  <el-pagination
                    v-model:current-page="recyclePage"
                    :page-size="recyclePageSize"
                    layout="prev, pager, next, total"
                    :total="recycleTotal"
                    small
                    background
                  />
                </div>
              </section>
            </template>
            <ShareTableView
              v-else-if="showShare"
              :is-mobile="isMobileViewport"
              :share-tab="shareTab"
              :share-list="filteredShareList"
              :direct-share-list="filteredDirectShareList"
              :loading="(shareTab === 'link' ? shareLoading : directShareLoading) && !manualRefresh"
              :on-row-click="handleRowClick"
              :copy-share-link="copyShareLink"
              :open-share-location="openShareLocation"
              :revoke-share="revokeShare"
              :revoke-direct-share="revokeDirectShare"
              :open-direct-share-detail="openDirectShareDetail"
              :format-time="formatTime"
              :shorten-address="shortenAddress"
              :is-direct-share-owner="isDirectShareOwner"
            />
            <SharedWithMeTableView
              v-else-if="showSharedWithMe"
              :is-mobile="isMobileViewport"
              :shared-active="sharedActive"
              :shared-with-me-list="filteredSharedWithMeList"
              :shared-entries="filteredSharedEntries"
              :loading="(sharedActive ? sharedEntriesLoading : sharedWithMeLoading) && !manualRefresh"
              :on-row-click="handleRowClick"
              :can-drag-shared-item="canDragSharedItem"
              :is-dragging-shared-item="isDraggingSharedItem"
              :is-shared-move-target="isSharedMoveTarget"
              :handle-shared-item-drag-start="handleSharedItemDragStart"
              :handle-shared-item-drag-end="handleSharedItemDragEnd"
              :handle-shared-directory-drag-enter="handleSharedDirectoryDragEnter"
              :handle-shared-directory-drag-over="handleSharedDirectoryDragOver"
              :handle-shared-directory-drag-leave="handleSharedDirectoryDragLeave"
              :handle-shared-directory-drop="handleSharedDirectoryDrop"
              :format-time="formatTime"
              :format-size="formatSize"
              :shorten-address="shortenAddress"
              :shared-can-read="sharedCanRead"
              :shared-can-update="sharedCanUpdate"
              :shared-can-delete="sharedCanDelete"
              :open-share-detail="openShareDetail"
              :download-shared-root="downloadSharedRoot"
              :share-received-root="shareReceivedRoot"
              :get-preview-mode="getPreviewMode"
              :open-file-preview="openFilePreview"
              :open-shared-entry-detail="openSharedEntryDetail"
              :download-shared-file="downloadSharedFile"
              :share-received-file="shareReceivedFile"
              :rename-shared-item="renameSharedItem"
              :delete-shared-item="deleteSharedItem"
            />
            <FileTableView
              v-else
              :is-mobile="isMobileViewport"
              :rows="filteredFileList"
              :loading="loading && !manualRefresh"
              :selection-enabled="fileSelectionMode"
              :on-row-click="handleRowClick"
              :on-selection-change="handleFileSelectionChange"
              :can-drag-item="canDragItem"
              :is-dragging-item="isDraggingFile"
              :is-move-target="isMoveTarget"
              :handle-item-drag-start="handleItemDragStart"
              :handle-item-drag-end="handleItemDragEnd"
              :handle-directory-drag-enter="handleDirectoryDragEnter"
              :handle-directory-drag-over="handleDirectoryDragOver"
              :handle-directory-drag-leave="handleDirectoryDragLeave"
              :handle-directory-drop="handleDirectoryDrop"
              :format-size="formatSize"
              :format-time="formatTime"
              :get-preview-mode="getPreviewMode"
              :open-file-preview="openFilePreview"
              :open-detail-drawer="openDetailDrawer"
              :download-file="downloadFile"
              :can-share-item="canShareFileItem"
              :share-file="shareFile"
              :open-share-user-dialog="openShareUserDialog"
              :open-access-key-dialog="openAccessKeyDialogFromDirectory"
              :rename-item="renameItem"
              :delete-file="deleteFile"
              :selection-nonce="fileSelectionNonce"
            />
          </div>
        </section>

      </main>

      <HomeOverlays
        v-model:detail-drawer-visible="detailDrawerVisible"
        :detail-title="detailTitle"
        :detail-mode="detailMode"
        :detail-file="detailFile"
        :detail-recycle="detailRecycle"
        :detail-share="detailShare"
        :detail-direct-share="detailDirectShare"
        :detail-received-share="detailReceivedShare"
        :detail-shared-entry="detailSharedEntry"
        :shared-can-read="sharedCanRead"
        :shared-can-update="sharedCanUpdate"
        :get-preview-mode="getPreviewMode"
        :open-file-preview="openFilePreview"
        :format-time="formatTime"
        :format-deleted-time="formatDeletedTime"
        :format-size-detail="formatSizeDetail"
        :format-share-permission="formatSharePermission"
        :format-share-mode="formatShareMode"
        :get-share-link="getShareLink"
        :copy-share-link="copyShareLink"
        :revoke-share="revokeShare"
        :revoke-direct-share="revokeDirectShare"
        :is-direct-share-owner="isDirectShareOwner"
        :enter-direct-share-root="enterDirectShareRoot"
        :download-direct-share-root="downloadDirectShareRoot"
        :enter-directory="enterDirectory"
        :open-access-key-dialog="openAccessKeyDialogFromDirectory"
        :get-encrypted-directory-root="getEncryptedDirectoryRootForItem"
        :is-encrypted-directory-password-cached="isEncryptedDirectoryPasswordCachedForRoot"
        :requires-encrypted-directory-password="requiresEncryptedDirectoryExtraPassword"
        :get-encrypted-directory-protection-label="getEncryptedDirectoryProtectionLabelForRoot"
        :unlock-encrypted-directory="unlockEncryptedDirectoryByRoot"
        :clear-encrypted-directory-password-cache="clearEncryptedDirectoryPasswordByRoot"
        :enter-shared-root="enterSharedRoot"
        :enter-shared-directory="enterSharedDirectory"
        :download-shared-root="downloadSharedRoot"
        :download-shared-file="downloadSharedFile"
        v-model:share-link-dialog-visible="shareLinkDialogVisible"
        :share-link-submitting="shareLinkSubmitting"
        :share-link-target="shareLinkTarget"
        :share-link-form="shareLinkForm"
        :share-expiry-units="SHARE_EXPIRY_UNITS"
        :share-mode-options="SHARE_MODE_OPTIONS"
        :submit-share-link="submitShareLink"
        v-model:share-user-dialog-visible="shareUserDialogVisible"
        :share-user-submitting="shareUserSubmitting"
        :share-user-target="shareUserTarget"
        :share-user-form="shareUserForm"
        :group-members="shareAddressMembers"
        :managed-groups="managedGroups"
        :selected-group-members="selectedGroupMembers"
        :submit-share-user="submitShareUser"
        v-model:create-folder-dialog-visible="createFolderDialogVisible"
        :create-folder-submitting="createFolderSubmitting"
        :create-folder-form="createFolderForm"
        :cipher-suite-options="cipherSuiteOptions"
        :submit-create-folder="submitCreateFolder"
        v-model:rename-dialog-visible="renameDialogVisible"
        :rename-submitting="renameSubmitting"
        :rename-form="renameForm"
        :rename-target="renameTarget"
        :submit-rename="submitRename"
        v-model:password-dialog-visible="passwordDialogVisible"
        :password-submitting="passwordSubmitting"
        :password-form="passwordForm"
        :user-profile="userProfile"
        :submit-password="submitPassword"
      />
      <el-dialog
        v-model="accessKeyDialogVisible"
        :title="accessKeyScopeLocked ? '授权密钥' : '新建密钥'"
        class="access-key-create-dialog"
        width="680px"
      >
        <div class="access-key-dialog">
          <template v-if="accessKeyScopeLocked">
            <div class="access-key-authorize-layout">
              <div class="access-key-scope-card">
                <div class="access-key-scope-header">
                  <span class="access-key-scope-label">当前目录</span>
                  <el-button
                    size="small"
                    class="access-key-ghost-button"
                    @click="copyAccessKeyValue(buildAbsoluteDavURL(accessKeyScopePath), '挂载URL')"
                  >
                    复制挂载 URL
                  </el-button>
                </div>
                <div class="access-key-scope-content">
                  <span class="access-key-scope-value mono">{{ accessKeyScopePath }}</span>
                  <span class="access-key-scope-count">生效中 {{ activeAccessKeysForScope.length }}</span>
                </div>
              </div>

              <div class="access-key-section">
                <div class="access-key-section-head">
                  <span class="access-key-section-title">授权列表</span>
                </div>
                <el-table
                  v-if="accessKeysForScope.length"
                  :data="accessKeysForScope"
                  size="small"
                  border
                  max-height="250"
                  class="access-key-bound-table"
                >
                  <el-table-column prop="name" label="名称" min-width="180" />
                  <el-table-column label="Key ID" min-width="190">
                    <template #default="{ row }">
                      <span class="mono">{{ row.keyId }}</span>
                    </template>
                  </el-table-column>
                  <el-table-column label="状态" width="100" align="center">
                    <template #default="{ row }">
                      <el-tag size="small" :type="row.status === 'active' ? 'success' : 'info'">
                        {{ row.status === 'active' ? '生效中' : '已撤销' }}
                      </el-tag>
                    </template>
                  </el-table-column>
                  <el-table-column label="操作" width="92" align="center">
                    <template #default="{ row }">
                      <el-button
                        size="small"
                        text
                        type="danger"
                        :disabled="row.status !== 'active'"
                        @click="revokeAccessKey(row)"
                      >
                        撤销
                      </el-button>
                    </template>
                  </el-table-column>
                </el-table>
                <div v-else class="access-key-empty">当前目录还没有授权记录</div>
              </div>

              <div class="access-key-section access-key-bind-box">
                <div class="access-key-section-head">
                  <span class="access-key-section-title">新增授权</span>
                </div>
                <div v-if="activeUnboundKeysForScope.length" class="access-key-bind-row">
                  <el-select
                    v-model="accessKeyBindTargetID"
                    placeholder="选择一个已创建的密钥"
                    style="width: 100%"
                  >
                    <el-option
                      v-for="item in activeUnboundKeysForScope"
                      :key="item.id"
                      :label="`${item.name} (${item.keyId})`"
                      :value="item.id"
                    />
                  </el-select>
                  <el-button type="primary" :loading="accessKeyBinding" @click="bindAccessKeyToScope()">
                    授权
                  </el-button>
                </div>
                <div v-else class="access-key-empty">
                  <span>没有可授权的生效密钥</span>
                  <el-button class="access-key-ghost-button" @click="openAccessKeyCreateForCurrentScope">为当前目录新建密钥</el-button>
                </div>
              </div>
            </div>
          </template>
          <template v-else>
            <el-form label-position="top" class="access-key-form">
              <el-form-item label="密钥名称">
                <el-input v-model="accessKeyForm.name" placeholder="例如：同步工具只读密钥" />
              </el-form-item>
              <el-form-item label="目录范围">
                <div class="access-key-directory-field">
                  <el-input v-model="accessKeyForm.rootPath" readonly />
                  <el-button @click="openAccessKeyDirectoryPicker">选择目录</el-button>
                </div>
                <div class="access-key-permission-help">
                  密钥覆盖所选目录及其全部子目录。请按最小权限原则选择实际需要的目录。
                </div>
              </el-form-item>
              <el-form-item label="权限">
                <el-checkbox-group v-model="accessKeyForm.permissions">
                  <el-checkbox
                    v-for="item in ACCESS_KEY_PERMISSIONS"
                    :key="item.value"
                    :label="item.value"
                  >
                    {{ item.label }}
                  </el-checkbox>
                </el-checkbox-group>
                <div class="access-key-permission-help">
                  <div>新增：上传新文件、创建目录。</div>
                  <div>修改：覆盖已有文件、重命名、移动；如果要通过 `davfs2` 挂载后直接写入，通常至少需要“读取 + 新增 + 修改”。</div>
                </div>
              </el-form-item>
              <el-form-item label="有效期">
                <div class="access-key-expiry">
                  <el-input v-model="accessKeyForm.expiresValue" placeholder="0" />
                  <el-select v-model="accessKeyForm.expiresUnit" class="access-key-expiry-unit">
                    <el-option
                      v-for="item in SHARE_EXPIRY_UNITS"
                      :key="item.value"
                      :label="item.label"
                      :value="item.value"
                    />
                  </el-select>
                </div>
                <div class="share-group-meta">输入 0 表示永不过期</div>
              </el-form-item>
            </el-form>

            <div v-if="accessKeyCreateResult" class="access-key-created">
              <div class="access-key-created-title">创建成功，请立即保存以下凭据（仅显示一次）</div>
              <div class="access-key-created-row">
                <span class="access-key-created-label">Key ID</span>
                <span class="access-key-created-value mono">{{ accessKeyCreateResult.keyId }}</span>
                <el-button size="small" class="access-key-ghost-button" @click="copyAccessKeyValue(accessKeyCreateResult.keyId, 'Key ID')">复制</el-button>
              </div>
              <div class="access-key-created-row">
                <span class="access-key-created-label">Key Secret</span>
                <span class="access-key-created-value mono">{{ accessKeyCreateResult.keySecret }}</span>
                <el-button size="small" class="access-key-ghost-button" @click="copyAccessKeyValue(accessKeyCreateResult.keySecret, 'Key Secret')">复制</el-button>
              </div>
            </div>
          </template>
        </div>
        <template #footer>
          <el-button @click="closeAccessKeyDialog">关闭</el-button>
          <el-button v-if="!accessKeyScopeLocked" type="primary" :loading="accessKeySubmitting" @click="submitAccessKey">创建密钥</el-button>
        </template>
      </el-dialog>
      <el-dialog
        v-model="accessKeyDirectoryPickerVisible"
        :title="directoryPickerTarget === 'tool' ? '选择 Tool 授权目录' : '选择 WebDAV 授权目录'"
        width="620px"
        append-to-body
      >
        <div class="access-key-directory-picker">
          <div class="access-key-directory-spaces">
            <el-button
              v-for="space in normalizeAssetSpaces(assetSpaces)"
              :key="space.key"
              size="small"
              :type="resolveAssetSpaceByPath(accessKeyDirectoryPickerPath)?.key === space.key ? 'primary' : 'default'"
              @click="loadAccessKeyDirectoryPicker(space.path)"
            >
              {{ space.name }}
            </el-button>
          </div>
          <el-breadcrumb separator="/" class="access-key-directory-breadcrumb">
            <el-breadcrumb-item
              v-for="crumb in accessKeyDirectoryPickerBreadcrumbs"
              :key="crumb.path"
            >
              <el-button text @click="loadAccessKeyDirectoryPicker(crumb.path)">{{ crumb.label }}</el-button>
            </el-breadcrumb-item>
          </el-breadcrumb>
          <div class="access-key-directory-create">
            <template v-if="directoryPickerCreateMode">
              <el-input
                v-model="directoryPickerNewFolderName"
                placeholder="输入新目录名称"
                :disabled="directoryPickerCreateSubmitting"
                @keyup.enter="createDirectoryPickerFolder"
              />
              <el-button type="primary" :loading="directoryPickerCreateSubmitting" @click="createDirectoryPickerFolder">创建</el-button>
              <el-button :disabled="directoryPickerCreateSubmitting" @click="cancelDirectoryPickerCreateFolder">取消</el-button>
            </template>
            <el-button v-else size="small" class="access-key-ghost-button" @click="beginDirectoryPickerCreateFolder">新建目录</el-button>
          </div>
          <div v-loading="accessKeyDirectoryPickerLoading" class="access-key-directory-list">
            <button
              v-for="item in accessKeyDirectoryPickerItems"
              :key="item.path"
              type="button"
              class="access-key-directory-item"
              @click="enterAccessKeyDirectoryPickerItem(item)"
            >
              <el-icon><Folder /></el-icon>
              <span>{{ item.name }}</span>
              <el-icon class="access-key-directory-arrow"><ArrowRight /></el-icon>
            </button>
            <el-empty
              v-if="!accessKeyDirectoryPickerLoading && !accessKeyDirectoryPickerItems.length"
              description="当前目录没有子目录"
              :image-size="72"
            />
          </div>
          <div class="access-key-directory-selected">
            当前选择：<span class="mono">{{ accessKeyDirectoryPickerPath }}</span>
          </div>
        </div>
        <template #footer>
          <el-button @click="accessKeyDirectoryPickerVisible = false">取消</el-button>
          <el-button type="primary" @click="selectAccessKeyDirectoryPickerPath">选择当前目录</el-button>
        </template>
      </el-dialog>
      <el-dialog v-model="s3CredentialDialogVisible" title="新建 S3 凭证" width="560px">
        <el-form label-position="top">
          <el-form-item label="凭证名称">
            <el-input v-model="s3CredentialName" placeholder="例如：本地 mc" :disabled="!!s3CredentialCreateResult" />
          </el-form-item>
          <el-form-item label="逻辑 bucket">
            <el-select v-model="s3CredentialBucket" class="s3-credential-bucket-select" :disabled="!!s3CredentialCreateResult">
              <el-option label="personal（个人存储）" value="personal" />
              <el-option label="apps（应用存储）" value="apps" />
              <el-option label="services（服务存储）" value="services" />
            </el-select>
          </el-form-item>
          <el-form-item label="bucket 下目录">
            <el-input v-model="s3CredentialDirectory" placeholder="留空表示整个 bucket；例如：backup/资料" :disabled="!!s3CredentialCreateResult">
              <template #prepend>/{{ s3CredentialBucket }}/</template>
            </el-input>
            <div class="access-key-permission-help">S3 客户端的 bucket 填上面选择的值，目录限制会作为 key 前缀生效。</div>
          </el-form-item>
        </el-form>
        <div v-if="s3CredentialCreateResult" class="access-key-created">
          <div class="access-key-created-title">创建成功，请立即保存以下凭据（Secret 仅显示一次）</div>
          <div class="access-key-created-row">
            <span class="access-key-created-label">目录范围</span>
            <span class="access-key-created-value mono">{{ s3CredentialCreateResult.rootPath }}</span>
            <span></span>
          </div>
          <div class="access-key-created-row">
            <span class="access-key-created-label">Access Key ID</span>
            <span class="access-key-created-value mono">{{ s3CredentialCreateResult.accessKeyId }}</span>
            <el-button size="small" class="access-key-ghost-button" @click="copyAccessKeyValue(s3CredentialCreateResult.accessKeyId, 'Access Key ID')">复制</el-button>
          </div>
          <div class="access-key-created-row">
            <span class="access-key-created-label">Secret</span>
            <span class="access-key-created-value mono">{{ s3CredentialCreateResult.secret }}</span>
            <el-button size="small" class="access-key-ghost-button" @click="copyAccessKeyValue(s3CredentialCreateResult.secret, 'Secret')">复制</el-button>
          </div>
          <div class="access-key-permission-help">关闭弹窗后 Secret 无法恢复，只能撤销凭证并重新创建。</div>
        </div>
        <template #footer>
          <el-button @click="s3CredentialDialogVisible = false">关闭</el-button>
          <el-button v-if="!s3CredentialCreateResult" type="primary" :loading="s3CredentialSubmitting" @click="submitS3Credential">创建凭证</el-button>
        </template>
      </el-dialog>
      <el-dialog v-model="toolCredentialDialogVisible" title="Warehouse Tool 凭证" width="560px">
        <el-form v-if="!toolCredentialCreateResult" label-position="top" class="tool-credential-form">
          <el-form-item label="凭证名称"><el-input v-model="toolCredentialName" placeholder="例如：conversation-review" /></el-form-item>
          <el-form-item label="授权目录">
            <div class="access-key-directory-field">
              <el-input v-model="toolCredentialPath" readonly placeholder="请选择 Warehouse 目录" />
              <el-button @click="openToolCredentialDirectoryPicker">选择</el-button>
            </div>
            <div class="access-key-permission-help">授权覆盖所选目录及其子目录，请按最小权限原则选择。</div>
          </el-form-item>
          <el-form-item label="Tool 权限">
            <el-checkbox-group v-model="toolCredentialScopes" class="tool-credential-scope-options">
              <el-checkbox v-for="scope in TOOL_CREDENTIAL_SCOPES" :key="scope.value" :label="scope.value">
                <span>{{ scope.label }}</span>
                <span class="tool-credential-scope-description">{{ scope.description }}</span>
              </el-checkbox>
            </el-checkbox-group>
          </el-form-item>
          <el-form-item label="过期时间"><el-date-picker v-model="toolCredentialExpiresAt" type="datetime" value-format="YYYY-MM-DDTHH:mm:ssZ" placeholder="选择过期时间" /></el-form-item>
        </el-form>
        <div v-else class="access-key-created">
          <div class="access-key-created-title">操作成功，请立即保存 Secret（仅显示一次）</div>
          <div class="access-key-created-row"><span class="access-key-created-label">Credential ID</span><span class="access-key-created-value mono">{{ toolCredentialCreateResult.id }}</span><el-button size="small" class="access-key-ghost-button" @click="copyAccessKeyValue(toolCredentialCreateResult.id, 'Credential ID')">复制</el-button></div>
          <div class="access-key-created-row"><span class="access-key-created-label">Secret</span><span class="access-key-created-value mono">{{ toolCredentialCreateResult.secret }}</span><el-button size="small" class="access-key-ghost-button" @click="copyAccessKeyValue(toolCredentialCreateResult.secret, 'Secret')">复制</el-button></div>
          <div class="access-key-permission-help">关闭弹窗后 Secret 无法恢复。生产 Agent 请使用 YEYING_WAREHOUSE_TOOL_TOKEN 保存该值。</div>
        </div>
        <template #footer><el-button @click="toolCredentialDialogVisible = false">关闭</el-button><el-button v-if="!toolCredentialCreateResult" type="primary" :loading="toolCredentialSubmitting" @click="submitToolCredential">创建凭证</el-button></template>
      </el-dialog>
      <el-dialog
        v-model="adminUsersDialogVisible"
        title="修改用户额度"
        width="420px"
        @closed="closeAdminUsersDialog"
      >
        <div class="admin-quota-dialog">
          <div v-if="adminUsersEditTarget" class="admin-quota-target">
            <div class="admin-quota-target-name">{{ adminUsersEditTarget.username }}</div>
            <div class="admin-quota-target-meta">
              已使用 {{ formatSize(adminUsersEditTarget.used_space) }}，当前上限
              {{ adminUsersEditTarget.quota > 0 ? formatSize(adminUsersEditTarget.quota) : '无限' }}
            </div>
          </div>
          <el-form label-position="top">
            <el-form-item label="额度">
              <div class="admin-quota-input-row">
                <el-input v-model="adminUsersEditValue" placeholder="输入 0 表示不限额" />
                <el-select v-model="adminUsersEditUnit" class="admin-quota-input-unit">
                  <el-option label="B" value="B" />
                  <el-option label="KB" value="KB" />
                  <el-option label="MB" value="MB" />
                  <el-option label="GB" value="GB" />
                  <el-option label="TB" value="TB" />
                </el-select>
              </div>
            </el-form-item>
          </el-form>
          <div v-if="adminUsersPreviewBytes !== null" class="admin-quota-preview">
            <span class="admin-quota-preview-label">换算预览</span>
            <span class="admin-quota-preview-value">
              {{ adminUsersPreviewBytes === 0 ? '不限额（保存为 0 字节）' : `${adminUsersPreviewBytes} 字节（约 ${formatSize(adminUsersPreviewBytes)}）` }}
            </span>
          </div>
          <div class="share-group-meta">请输入大于等于 0 的整数，`0` 表示不限额。保存时会自动换算为字节。</div>
        </div>
        <template #footer>
          <el-button @click="closeAdminUsersDialog">取消</el-button>
          <el-button type="primary" :loading="adminUsersSubmitting" @click="submitAdminUsersUpdate">保存</el-button>
        </template>
      </el-dialog>
      <el-dialog
        v-model="previewFormatDialogVisible"
        title="选择打开格式"
        width="480px"
        :close-on-click-modal="false"
        @closed="closePreviewFormatDialog"
      >
        <div class="preview-format-dialog">
          <div class="preview-format-target" :title="previewFormatTarget?.name || ''">
            {{ previewFormatTarget?.name || '当前文件' }}
          </div>
          <div class="preview-format-hint">文件后缀未识别，请选择内容格式打开。此选择仅影响本次预览，不会修改原文件。</div>
          <el-radio-group v-model="previewFormatSelection" class="preview-format-options">
            <el-radio
              v-for="option in PREVIEW_FORMAT_OPTIONS"
              :key="option.mode"
              :label="option.mode"
              border
              class="preview-format-option"
            >
              <span class="preview-format-option-label">{{ option.label }}</span>
              <span class="preview-format-option-extensions">{{ option.extensions }}</span>
            </el-radio>
          </el-radio-group>
        </div>
        <template #footer>
          <el-button @click="closePreviewFormatDialog">取消</el-button>
          <el-button type="primary" :disabled="!previewFormatSelection" @click="confirmPreviewFormat">
            打开
          </el-button>
        </template>
      </el-dialog>
      <FilePreviewDialog
        v-model="previewVisible"
        v-model:content="previewContent"
        :title="previewTitle"
        :mode="previewMode || 'text'"
        :blob="previewBlob"
        :source-url="previewSourceUrl"
        :file-name="previewTarget?.name || ''"
        :loading="previewLoading"
        :saving="previewSaving"
        :dirty="previewDirty"
        :read-only="previewReadOnly"
        :image-position="previewImageIndex + 1"
        :image-total="previewImageTotal"
        :can-prev-image="canPreviewPreviousImage"
        :can-next-image="canPreviewNextImage"
        @request-close="handlePreviewBeforeClose"
        @save="savePreview"
        @download="downloadPreview"
        @prev-image="previewAdjacentImage('prev')"
        @next-image="previewAdjacentImage('next')"
      />

      <nav class="mobile-only mobile-bottom-nav">
        <div class="mobile-nav-hint">
          <span class="mobile-nav-badge"></span>
          <span class="mobile-nav-text">{{ mobileLocationText }}</span>
        </div>
        <button
          type="button"
          class="mobile-nav-item"
          :class="{ active: isFileView }"
          @click="backToFiles"
        >
          <el-icon><FolderOpened /></el-icon>
          <span>文件</span>
        </button>
        <button
          type="button"
          class="mobile-nav-item"
          :class="{ active: showShare || showSharedWithMe }"
          @click="enterShare('link')"
        >
          <el-icon><Share /></el-icon>
          <span>分享</span>
        </button>
        <button
          type="button"
          class="mobile-nav-item"
          :class="{ active: showRecycle }"
          @click="enterRecycle"
        >
          <el-icon><Delete /></el-icon>
          <span>回收</span>
        </button>
        <button
          type="button"
          class="mobile-nav-item"
          :class="{ active: isManagementView }"
          @click="enterQuotaManage('account')"
        >
          <el-icon><User /></el-icon>
          <span>管理</span>
        </button>
      </nav>
    </div>
  </div>
</template>

<style lang="scss" scoped>
.home-container {
  height: 100%;
  overflow: hidden;
  background: linear-gradient(180deg, #f6f8fb 0%, #f2f4f7 100%);
  padding: 0;
}

.login-page {
  display: grid;
  place-items: center;
  width: 100%;
  height: 100%;
  padding: 24px 16px;
  box-sizing: border-box;
  color: #1f2937;
  background:
    linear-gradient(180deg, #eef4fb 0%, #f8fafc 48%, #ffffff 100%);
}

.login-floating-container {
  width: min(480px, 100%);
}

.login-hero {
  width: 100%;
  display: grid;
  place-items: center;
}

.login-warning {
  width: 100%;
  box-sizing: border-box;
  color: #a16207;
  font-size: 14px;
  text-align: center;
  background: #fffbeb;
  border: 1px solid #fde68a;
  border-radius: 8px;
  padding: 10px 12px;
}

.login-card {
  position: relative;
  overflow: hidden;
  width: 100%;
  min-height: 470px;
  background: #fff;
  border: 1px solid #e5e7eb;
  border-radius: 8px;
  padding: 34px 40px 28px;
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 22px;
  box-shadow: 0 20px 60px rgba(15, 23, 42, 0.08);
}

.login-card.is-wallet-mode {
  min-height: 0;
  padding-top: 32px;
  padding-bottom: 26px;
  gap: 20px;
}

.login-card-switch {
  position: absolute;
  top: 0;
  right: 0;
}

.login-mode-button {
  width: 88px;
  min-width: 88px;
  height: 88px;
  padding: 12px 10px 40px 40px;
  color: #ffffff;
  background: #9dcc86;
  border: 0;
  border-radius: 0;
  clip-path: polygon(100% 0, 100% 100%, 0 0);
  box-shadow: none;
}

.login-mode-button:hover,
.login-mode-button:focus {
  color: #ffffff;
  background: #8cc474;
}

.login-mode-button :deep(.el-icon) {
  font-size: 30px;
}

.login-brand {
  width: min(360px, 100%);
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 14px;
  text-align: center;
}

.login-brand > div {
  width: 100%;
}

.login-brand h1 {
  margin: 0;
  font-size: 25px;
  line-height: 1.25;
  font-weight: 650;
  color: #111827;
}

.login-brand p {
  margin: 8px 0 0;
  font-size: 14px;
  line-height: 1.5;
  color: #64748b;
}

.login-identity-panel,
.login-wallet-panel {
  width: min(360px, 100%);
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 18px;
}

.login-wallet-panel {
  margin-top: 2px;
}

.login-qrcode-frame {
  width: 248px;
  height: 248px;
  padding: 13px;
  border: 1px solid #e5e7eb;
  border-radius: 8px;
  background: #ffffff;
  display: grid;
  place-items: center;
  cursor: pointer;
  box-shadow: 0 1px 2px rgba(15, 23, 42, 0.04);
}

.login-qrcode-frame:disabled {
  cursor: default;
}

.login-qrcode-frame img {
  width: 220px;
  height: 220px;
  display: block;
}

.login-qrcode-placeholder {
  font-size: 14px;
  color: #94a3b8;
}

.login-status-line {
  min-height: 22px;
  font-size: 14px;
  color: #475569;
  text-align: center;
  line-height: 1.5;
}

.login-identity-actions {
  display: flex;
  align-items: center;
  justify-content: center;
  flex-wrap: wrap;
  gap: 6px;
}

.login-main-btn {
  width: 100%;
  height: 44px;
  border-radius: 8px;
  font-weight: 500;
  align-self: stretch;
}

:deep(.el-button.login-main-btn),
:deep(.el-button.login-submit) {
  border: 0 !important;
  color: #ffffff !important;
  --el-button-border-color: transparent;
  --el-button-hover-border-color: transparent;
  --el-button-active-border-color: transparent;
  --el-button-disabled-border-color: transparent;
  --el-button-text-color: #ffffff;
  --el-button-hover-text-color: #ffffff;
  --el-button-active-text-color: #ffffff;
  background-color: #3f7fe0 !important;
  background-image: none !important;
  box-shadow: none;
  transition: background-color 0.16s ease, transform 0.12s ease;
}

:deep(.el-button.login-main-btn:not(.is-disabled):hover),
:deep(.el-button.login-submit:not(.is-disabled):hover) {
  background-color: #356fc7 !important;
}

:deep(.el-button.login-main-btn:not(.is-disabled):active),
:deep(.el-button.login-submit:not(.is-disabled):active) {
  background-color: #2e62b2 !important;
  transform: translateY(0);
}

:deep(.el-button.login-main-btn.is-disabled),
:deep(.el-button.login-submit.is-disabled) {
  border: 0 !important;
  color: #f3f7ff !important;
  background-image: none !important;
  background-color: #b7c7df !important;
  box-shadow: none !important;
}

.login-bottom-link {
  margin-top: auto;
  font-size: 13px;
}

.login-card.is-wallet-mode .login-bottom-link {
  margin-top: 2px;
}

.login-bottom-link a,
.login-identity-actions :deep(.el-button) {
  color: #2563eb;
}

.login-mode-enter-active,
.login-mode-leave-active {
  transition: opacity 0.18s ease, transform 0.18s ease;
}

.login-mode-enter-from,
.login-mode-leave-to {
  opacity: 0;
  transform: translateY(8px);
}

@media (max-width: 980px) {
  .login-hero {
    justify-content: center;
  }
}

@media (max-width: 700px) {
  .login-page {
    padding: 0;
  }

  .login-floating-container {
    width: 100%;
    height: 100%;
    min-height: 0;
  }

  .login-hero {
    height: 100%;
    align-items: center;
  }

  .login-card {
    width: 100%;
    min-height: min(560px, 100%);
    padding: 30px 18px 24px;
    border: 0;
    border-radius: 0;
    box-shadow: none;
  }

  .login-card.is-wallet-mode {
    min-height: 0;
    padding-top: 28px;
    padding-bottom: 22px;
  }

  .login-qrcode-frame {
    width: 232px;
    height: 232px;
  }

  .login-qrcode-frame img {
    width: 204px;
    height: 204px;
  }

}

.app-shell {
  display: grid;
  grid-template-columns: 240px minmax(0, 1fr);
  gap: 16px;
  padding: 0;
  height: 100%;
  box-sizing: border-box;
  min-height: 0;
}

.app-shell.is-side-collapsed {
  grid-template-columns: 92px minmax(0, 1fr);
}

.side-panel {
  background: #fff;
  border-radius: 16px;
  padding: 16px;
  border: 1px solid #eef1f4;
  box-shadow: 0 10px 24px rgba(15, 23, 42, 0.06);
  display: flex;
  flex-direction: column;
  gap: 16px;
  min-height: 0;
  overflow: auto;
}

.brand {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  padding-bottom: 12px;
  border-bottom: 1px solid #f0f2f5;
}

.brand-main {
  display: flex;
  align-items: center;
  gap: 12px;
  min-width: 0;
}

.brand-title {
  font-weight: 600;
  font-size: 16px;
  color: #1f2d3d;
}

.brand-sub {
  font-size: 14px;
  color: #98a2b3;
  margin-top: 2px;
  white-space: nowrap;
}

.side-toggle-btn {
  flex-shrink: 0;
}

.app-shell.is-side-collapsed .side-panel {
  padding-left: 10px;
  padding-right: 10px;
}

.app-shell.is-side-collapsed .brand {
  justify-content: center;
}

.app-shell.is-side-collapsed .brand-main {
  justify-content: center;
}

.app-shell.is-side-collapsed .nav-group {
  align-items: center;
}

.nav-block {
  display: flex;
  flex-direction: column;
  gap: 14px;
}

.nav-group {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.nav-group-title {
  font-size: 12px;
  color: #8a8f98;
  letter-spacing: 0.04em;
  text-transform: uppercase;
}

.nav-list {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.nav-item {
  width: 100%;
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 10px 12px;
  border-radius: 12px;
  border: 1px solid transparent;
  background: transparent;
  color: #2b2f36;
  font-size: 14px;
  cursor: pointer;
  transition: all 0.2s ease;
}

.app-shell.is-side-collapsed .nav-item {
  justify-content: center;
  padding: 10px 8px;
}

.nav-item:hover {
  background: #f5f7fa;
}

.nav-item.active {
  background: #eef5ff;
  border-color: #d6e6ff;
  color: #1c4fb8;
  font-weight: 600;
}

.nav-item.is-soon {
  cursor: not-allowed;
}

.nav-item:disabled {
  background: #fafafa;
  color: #9aa0a6;
  border-color: #f0f0f0;
}

.nav-icon {
  font-size: 16px;
}

.nav-item .el-tag {
  margin-left: auto;
}

.asset-nav-list {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.asset-nav-item {
  width: 100%;
  border: none;
  background: #fff;
  color: #606266;
  border-radius: 12px;
  padding: 10px 12px;
  text-align: left;
  font-size: 14px;
  cursor: pointer;
  transition: all 0.2s ease;
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.app-shell.is-side-collapsed .asset-nav-item {
  padding: 10px 8px;
  flex-direction: row;
  justify-content: center;
}

.asset-nav-main {
  display: flex;
  align-items: center;
  gap: 8px;
}

.app-shell.is-side-collapsed .asset-nav-main {
  justify-content: center;
  width: 100%;
}

.asset-nav-item:hover {
  background: #f7fbff;
  color: #2b2f36;
}

.asset-nav-item.active {
  background: #edf6ff;
  color: #1c4fb8;
  font-weight: 600;
}

.main-panel {
  display: flex;
  flex-direction: column;
  gap: 16px;
  min-width: 0;
  min-height: 0;
}

.path-row {
  display: flex;
  align-items: center;
  gap: 12px;
  flex-wrap: wrap;
}


.path-pill {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  padding: 6px 12px;
  border-radius: 999px;
  background: #f5f7fa;
  color: #606266;
  font-size: 12px;
}

.view-segment {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 4px;
  border-radius: 999px;
  background: #f5f7fa;
}

.segment-button {
  border: none;
  background: transparent;
  color: #606266;
  border-radius: 999px;
  padding: 7px 14px;
  font-size: 12px;
  font-weight: 500;
  cursor: pointer;
  transition: all 0.2s ease;
}

.segment-button:hover {
  color: #1f2d3d;
  background: rgba(64, 158, 255, 0.1);
}

.segment-button.active {
  background: #fff;
  color: #1c4fb8;
  box-shadow: 0 4px 12px rgba(64, 158, 255, 0.16);
}

.path-label {
  color: #909399;
}

.breadcrumb {
  display: flex;
  align-items: center;
  padding: 4px 10px;
  border-radius: 999px;
  background: #f5f7fa;
  color: #606266;
  gap: 8px;
}

.crypto-pill {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  padding: 5px 10px;
  border-radius: 999px;
  background: rgba(230, 162, 60, 0.1);
  color: #8a5200;
  font-size: 12px;
  min-height: 32px;
  max-width: 100%;
}

.crypto-pill-icon {
  color: #e6a23c;
  flex-shrink: 0;
}

.crypto-pill-label {
  font-weight: 600;
  flex-shrink: 0;
}

.crypto-pill-text {
  color: #7a4b00;
  white-space: nowrap;
}

.crypto-pill-actions {
  display: inline-flex;
  align-items: center;
  gap: 2px;
}

.crypto-pill-actions :deep(.el-button) {
  padding-inline: 4px;
}

.breadcrumb-link {
  border: none;
  background: transparent;
  padding: 0;
  font-size: 12px;
  color: #409eff;
  cursor: pointer;
}

.breadcrumb-link:hover {
  color: #1d73c7;
}

.breadcrumb-link.is-drop-target {
  padding: 2px 8px;
  border-radius: 8px;
  background: rgba(64, 158, 255, 0.12);
  box-shadow: inset 0 0 0 1px rgba(64, 158, 255, 0.35);
}

.path-row :deep(.el-button.is-drop-target) {
  background: rgba(64, 158, 255, 0.12);
  border-color: rgba(64, 158, 255, 0.35);
  color: #409eff;
}

.copy-icon {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 24px;
  height: 24px;
  border: none;
  background: transparent;
  color: #909399;
  cursor: pointer;
  border-radius: 999px;
  transition: all 0.2s ease;
}

.copy-icon:hover {
  background: rgba(64, 158, 255, 0.12);
  color: #409eff;
}

.header-search {
  display: flex;
  align-items: center;
}

.header-search :deep(.el-input) {
  width: 420px;
  max-width: 100%;
}

.list-header {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 16px;
  flex-wrap: wrap;
  padding: 4px 0 12px;
  border-bottom: 1px solid #eef1f4;
}

.list-header-recycle {
  align-items: center;
}

.list-header-left {
  flex: 1;
  min-width: 0;
}

.list-header-right {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: 12px;
  flex-wrap: wrap;
  margin-left: auto;
}

.list-actions {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}

.section-title-row {
  display: flex;
  align-items: baseline;
  gap: 10px;
  min-width: 0;
  flex-wrap: wrap;
}

.section-title {
  font-size: 18px;
  font-weight: 700;
  color: #1f2937;
  line-height: 1.2;
}

.section-subtitle {
  font-size: 12px;
  color: #909399;
  line-height: 1.5;
}

.action-cluster {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  padding: 6px 10px;
  border-radius: 999px;
  background: #f7f9fc;
  border: 1px solid #eef1f4;
}

.action-cluster-subtle {
  background: #fff;
}

.action-cluster-label {
  font-size: 12px;
  color: #909399;
  white-space: nowrap;
}

.card-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}

.content-card {
  background: #fff;
  border-radius: 16px;
  border: 1px solid #eef1f4;
  box-shadow: 0 10px 24px rgba(15, 23, 42, 0.06);
  padding: 8px 8px 12px;
  position: relative;
  display: flex;
  flex-direction: column;
  flex: 1;
  min-height: 0;
  overflow: hidden;
}

.content-body {
  flex: 1;
  min-height: 0;
}

.content-scroll {
  overflow: auto;
}

.help-guide {
  min-height: 100%;
  padding: 24px clamp(16px, 4vw, 40px) 36px;
  background: #ffffff;
}

.help-guide-content {
  max-width: 980px;
  margin: 0 auto;
  color: #1f2937;
  font-size: 14px;
  line-height: 1.72;
}

.help-guide-content :deep(h1),
.help-guide-content :deep(h2),
.help-guide-content :deep(h3),
.help-guide-content :deep(h4) {
  color: #111827;
  line-height: 1.35;
  margin: 24px 0 12px;
}

.help-guide-content :deep(h1) {
  font-size: 28px;
  margin-top: 0;
}

.help-guide-content :deep(h2) {
  padding-top: 10px;
  border-top: 1px solid #eef1f4;
  font-size: 22px;
}

.help-guide-content :deep(h3) {
  font-size: 17px;
}

.help-guide-content :deep(p),
.help-guide-content :deep(ul),
.help-guide-content :deep(ol),
.help-guide-content :deep(table),
.help-guide-content :deep(pre) {
  margin: 0 0 14px;
}

.help-guide-content :deep(ul),
.help-guide-content :deep(ol) {
  padding-left: 22px;
}

.help-guide-content :deep(li) {
  margin: 4px 0;
}

.help-guide-content :deep(table) {
  width: 100%;
  border-collapse: collapse;
  font-size: 13px;
}

.help-guide-content :deep(td) {
  padding: 9px 10px;
  border: 1px solid #e5e7eb;
  vertical-align: top;
}

.help-guide-content :deep(tr:first-child td) {
  background: #f7f9fc;
  color: #374151;
  font-weight: 600;
}

.help-guide-content :deep(pre) {
  overflow-x: auto;
  padding: 14px 16px;
  border-radius: 10px;
  background: #111827;
  color: #f9fafb;
  font-size: 13px;
  line-height: 1.6;
}

.help-guide-content :deep(code) {
  padding: 2px 5px;
  border-radius: 5px;
  background: #eef2f7;
  color: #334155;
  font-size: 0.92em;
}

.help-guide-content :deep(pre code) {
  padding: 0;
  background: transparent;
  color: inherit;
}

.help-guide-content :deep(a) {
  color: #2563eb;
  text-decoration: none;
}

.help-guide-content :deep(a:hover) {
  text-decoration: underline;
}

.table-wrapper {
  display: flex;
  flex-direction: column;
  min-height: 0;
}

.recycle-panel {
  display: flex;
  flex-direction: column;
  min-height: 0;
  border: 1px solid #eef3f8;
  border-radius: 14px;
  background: linear-gradient(180deg, #fbfcfe 0%, #ffffff 88%);
  overflow: hidden;
}

.recycle-summary-bar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  padding: 10px 14px;
  border-bottom: 1px solid #eef3f8;
  background: rgba(247, 249, 252, 0.8);
}

.recycle-summary-main {
  display: flex;
  align-items: baseline;
  gap: 10px;
  min-width: 0;
  flex-wrap: wrap;
}

.recycle-summary-title {
  font-size: 12px;
  font-weight: 700;
  color: #1f2937;
  text-transform: uppercase;
  letter-spacing: 0.04em;
}

.recycle-summary-text {
  font-size: 12px;
  color: #909399;
  min-width: 0;
}

.recycle-summary-meta {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
  justify-content: flex-end;
}

.recycle-summary-chip {
  display: inline-flex;
  align-items: center;
  padding: 4px 8px;
  border-radius: 999px;
  border: 1px solid #e6edf5;
  background: #fff;
  color: #606266;
  font-size: 11px;
  line-height: 1;
  white-space: nowrap;
}

.table-pagination {
  display: flex;
  justify-content: flex-end;
  padding: 12px 4px 0;
}

.recycle-pagination {
  padding: 10px 14px 12px;
  border-top: 1px solid #eef3f8;
  margin-top: 0;
  background: rgba(251, 252, 254, 0.9);
}

.selection-summary-bar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  padding: 10px 12px;
  margin-bottom: 12px;
  border-radius: 14px;
  border: 1px solid #d9ecff;
  background: #f5faff;
}

.selection-summary-main {
  display: flex;
  flex-direction: column;
  gap: 4px;
  min-width: 0;
}

.selection-summary-title {
  font-size: 13px;
  font-weight: 600;
  color: #1c4fb8;
}

.selection-summary-text {
  font-size: 12px;
  color: #606266;
}

.selection-summary-actions {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}

.view-summary-bar {
  display: flex;
  flex-direction: column;
  gap: 6px;
  padding: 12px 14px;
  margin-bottom: 12px;
  border-radius: 14px;
  background: #f7f9fc;
  border: 1px solid #eef1f4;
}

.view-summary-title {
  font-size: 13px;
  font-weight: 600;
  color: #1f2d3d;
}

.view-summary-text {
  font-size: 12px;
  line-height: 1.6;
  color: #606266;
}

.drop-mask {
  position: absolute;
  inset: 8px;
  border-radius: 12px;
  border: 2px dashed #409eff;
  background: rgba(64, 158, 255, 0.08);
  display: flex;
  align-items: center;
  justify-content: center;
  z-index: 6;
  pointer-events: none;
}

.drop-hint {
  padding: 10px 16px;
  border-radius: 999px;
  background: #fff;
  color: #1f2d3d;
  font-size: 14px;
  box-shadow: 0 8px 16px rgba(15, 23, 42, 0.08);
}

.user-center {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 16px;
  padding: 8px;
}

.user-card {
  background: #fff;
  border-radius: 16px;
  padding: 16px;
  border: 1px solid #eef1f4;
  box-shadow: 0 10px 24px rgba(15, 23, 42, 0.06);
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.card-title {
  font-size: 14px;
  font-weight: 600;
  color: #1f2d3d;
}

.card-subtitle {
  margin-top: 4px;
  font-size: 12px;
  line-height: 1.5;
  color: #909399;
  font-family: inherit;
}

.user-list {
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.user-row {
  display: grid;
  grid-template-columns: 96px minmax(0, 1fr);
  gap: 10px;
  align-items: center;
}

.user-label {
  font-size: 12px;
  color: #909399;
}

.user-value {
  font-size: 13px;
  color: #1f2d3d;
  word-break: break-all;
}

.user-inline {
  display: flex;
  align-items: center;
  gap: 10px;
  min-width: 0;
}

.user-text {
  flex: 1;
  min-width: 0;
  word-break: break-all;
}

.user-input {
  flex: 1;
  min-width: 0;
}

.user-actions {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-shrink: 0;
}

.user-tags {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
}

.user-value.mono {
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, "Liberation Mono", "Courier New", monospace;
  font-size: 12px;
}

.user-card-full {
  grid-column: 1 / -1;
}

.user-card-full:has(.group-management-page.embedded) {
  padding: 16px;
}

.key-summary {
  display: flex;
  flex-wrap: wrap;
  gap: 16px;
  font-size: 12px;
  color: #909399;
}

.credential-tabs {
  margin-top: 8px;
}

.credential-tab-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  margin-bottom: 12px;
}

.credential-tab-body {
  min-height: 120px;
}

.admin-quota-filter {
  width: 140px;
}

.s3-credential-bucket-select {
  width: 100%;
}

.tool-credential-form :deep(.el-form-item) {
  margin-bottom: 16px;
}

.access-key-directory-create {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-top: 8px;
}

.access-key-directory-create .el-input {
  flex: 1;
  min-width: 160px;
}

.tool-credential-scope-options {
  display: flex;
  flex-direction: column;
  align-items: flex-start;
  gap: 8px;
}

.tool-credential-scope-options :deep(.el-checkbox) {
  height: auto;
  margin-right: 0;
  line-height: 1.4;
}

.tool-credential-scope-description {
  margin-left: 4px;
  color: #909399;
  font-size: 12px;
}

.admin-quota-table {
  width: 100%;
}

.key-meta-row {
  display: flex;
  align-items: center;
  gap: 6px;
  flex-wrap: wrap;
  font-size: 12px;
  color: #606266;
}

.key-more-text {
  font-size: 12px;
  color: #909399;
}

.access-key-dialog {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.access-key-authorize-layout {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.access-key-scope-card {
  border: 1px solid #eef1f4;
  border-radius: 12px;
  padding: 10px;
  background: #fafbfd;
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.access-key-scope-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
}

.access-key-scope-label {
  font-size: 12px;
  font-weight: 600;
  color: #1f2d3d;
}

.access-key-ghost-button {
  --el-button-bg-color: #f4f8ff;
  --el-button-border-color: #d6e4ff;
  --el-button-text-color: #2f5fa8;
  --el-button-hover-bg-color: #e9f1ff;
  --el-button-hover-border-color: #bfd4ff;
  --el-button-hover-text-color: #24508f;
  --el-button-active-bg-color: #dfeaff;
  --el-button-active-border-color: #a9c5ff;
  --el-button-active-text-color: #1f467d;
  --el-button-disabled-bg-color: #f5f7fa;
  --el-button-disabled-border-color: #e4e7ed;
  --el-button-disabled-text-color: #c0c4cc;
  padding-inline: 10px;
}

.access-key-scope-count {
  font-size: 12px;
  color: #409eff;
  background: rgba(64, 158, 255, 0.12);
  border-radius: 999px;
  padding: 2px 8px;
  flex-shrink: 0;
}

.access-key-scope-content {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}

.access-key-scope-value {
  font-size: 12px;
  color: #1f2d3d;
  word-break: break-all;
  min-width: 0;
}

.access-key-section {
  border: 1px solid #eef1f4;
  border-radius: 12px;
  padding: 10px;
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.access-key-section-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
}

.access-key-section-title {
  font-size: 12px;
  font-weight: 600;
  color: #1f2d3d;
}

.access-key-empty {
  font-size: 12px;
  color: #909399;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
}

.access-key-bound-table :deep(.el-table__cell) {
  padding-top: 8px;
  padding-bottom: 8px;
}

.access-key-bind-box {
  background: #fff;
}

.access-key-bind-row {
  display: flex;
  align-items: center;
  gap: 8px;
}

.access-key-form :deep(.el-form-item) {
  margin-bottom: 12px;
}

.access-key-directory-field {
  display: flex;
  width: 100%;
  gap: 8px;
}

.access-key-directory-picker {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.access-key-directory-spaces {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}

.access-key-directory-breadcrumb {
  min-height: 32px;
  padding: 6px 10px;
  border-radius: 8px;
  background: #f5f7fa;
}

.access-key-directory-breadcrumb :deep(.el-button) {
  padding: 0;
  height: auto;
}

.access-key-directory-list {
  min-height: 220px;
  max-height: 360px;
  overflow-y: auto;
  border: 1px solid #e4e7ed;
  border-radius: 8px;
}

.access-key-directory-item {
  width: 100%;
  min-height: 44px;
  padding: 8px 12px;
  border: 0;
  border-bottom: 1px solid #f0f2f5;
  background: #fff;
  color: #303133;
  display: flex;
  align-items: center;
  gap: 10px;
  cursor: pointer;
  text-align: left;
}

.access-key-directory-item:hover {
  background: #f5f7fa;
}

.access-key-directory-arrow {
  margin-left: auto;
  color: #909399;
}

.access-key-directory-selected {
  padding: 10px 12px;
  border-radius: 8px;
  background: #ecf5ff;
  color: #606266;
  word-break: break-all;
}

.access-key-permission-help {
  margin-top: 8px;
  font-size: 12px;
  line-height: 1.6;
  color: #909399;
}

.access-key-expiry {
  width: 100%;
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: nowrap;
}

.access-key-expiry :deep(.el-input) {
  flex: 1;
  min-width: 0;
}

.access-key-expiry :deep(.el-select) {
  width: 120px;
  flex: 0 0 120px;
}

.access-key-created {
  border: 1px solid #d8e9ff;
  background: #f5faff;
  border-radius: 12px;
  padding: 12px;
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.access-key-created-title {
  font-size: 12px;
  color: #1f2d3d;
  font-weight: 600;
}

.access-key-created-row {
  display: grid;
  grid-template-columns: 72px minmax(0, 1fr) auto;
  align-items: center;
  gap: 8px;
}

.access-key-created-label {
  font-size: 12px;
  color: #909399;
}

.access-key-created-value {
  font-size: 12px;
  color: #1f2d3d;
  word-break: break-all;
}

.access-key-created-actions {
  display: flex;
  justify-content: flex-end;
}

.admin-quota-dialog {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.admin-quota-input-row {
  display: flex;
  align-items: center;
  gap: 8px;
}

.admin-quota-input-row :deep(.el-input) {
  flex: 1;
  min-width: 0;
}

.admin-quota-input-unit {
  width: 110px;
  flex: 0 0 110px;
}

.admin-quota-preview {
  display: flex;
  flex-direction: column;
  gap: 4px;
  padding: 10px 12px;
  border-radius: 12px;
  background: #f7f9fc;
  border: 1px solid #eef1f4;
}

.admin-quota-preview-label {
  font-size: 12px;
  color: #909399;
}

.admin-quota-preview-value {
  font-size: 12px;
  color: #1f2d3d;
  word-break: break-all;
}

.admin-quota-target {
  padding: 12px;
  border-radius: 12px;
  background: #f7f9fc;
  border: 1px solid #eef1f4;
}

.admin-quota-target-name {
  font-size: 14px;
  font-weight: 600;
  color: #1f2d3d;
}

.admin-quota-target-meta {
  margin-top: 4px;
  font-size: 12px;
  color: #606266;
}

.preview-format-dialog {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.preview-format-target {
  color: #303133;
  font-size: 14px;
  font-weight: 600;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.preview-format-hint {
  color: #909399;
  font-size: 13px;
}

.preview-format-options {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 10px;
}

.preview-format-option {
  height: auto;
  min-height: 58px;
  margin: 0 !important;
  display: flex;
  align-items: center;
}

.preview-format-option :deep(.el-radio__label) {
  display: flex;
  flex-direction: column;
  gap: 3px;
  min-width: 0;
}

.preview-format-option-label {
  color: #303133;
  font-size: 14px;
  font-weight: 600;
}

.preview-format-option-extensions {
  color: #909399;
  font-size: 12px;
  line-height: 1.35;
  white-space: normal;
}

.quota-value {
  display: flex;
  align-items: baseline;
  gap: 6px;
  font-size: 18px;
  font-weight: 600;
  color: #1f2d3d;
}

.quota-card {
  position: relative;
}

.quota-card-warning {
  border-color: #f3d19e;
  box-shadow: 0 10px 24px rgba(230, 162, 60, 0.12);
}

.quota-card-danger {
  border-color: #f5c2c7;
  box-shadow: 0 10px 24px rgba(245, 108, 108, 0.12);
}

.quota-sep {
  color: #c0c4cc;
  font-weight: 400;
}

.quota-meta {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
  font-size: 12px;
  color: #909399;
}

.quota-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 12px;
}

.quota-item {
  padding: 10px 12px;
  border-radius: 12px;
  background: #f7f9fc;
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.quota-label {
  font-size: 12px;
  color: #909399;
}

.quota-amount {
  font-size: 13px;
  font-weight: 600;
  color: #1f2d3d;
}

.mobile-bottom-nav {
  display: none;
}

.mobile-nav-hint {
  display: none;
}

@media (max-width: 1200px) {
  .app-shell {
    grid-template-columns: 220px minmax(0, 1fr);
  }

  .app-shell.is-side-collapsed {
    grid-template-columns: 84px minmax(0, 1fr);
  }

  .user-center {
    grid-template-columns: 1fr;
  }
}

@media (max-width: 900px) {
  .app-shell {
    grid-template-columns: 1fr;
    padding: 12px;
  }

  .side-panel {
    padding: 12px;
  }

  .brand {
    padding-bottom: 0;
    border-bottom: none;
  }

  .nav-group {
    gap: 6px;
  }

  .nav-item {
    width: 100%;
  }

  .asset-nav-list {
    width: 100%;
    gap: 6px;
  }

  .asset-nav-item {
    width: 100%;
  }

  .view-segment {
    width: 100%;
    overflow-x: auto;
  }

  .crypto-pill {
    width: 100%;
    justify-content: flex-start;
    flex-wrap: wrap;
    border-radius: 14px;
  }

  .crypto-pill-text {
    white-space: normal;
  }

  .selection-summary-bar {
    flex-direction: column;
    align-items: flex-start;
  }

  .access-key-created-row {
    grid-template-columns: 1fr;
    gap: 4px;
  }

  .access-key-bind-row {
    flex-direction: column;
    align-items: stretch;
  }

  .access-key-bound-item {
    flex-direction: column;
  }

  .access-key-bound-actions {
    width: 100%;
    justify-content: flex-end;
  }

  .access-key-empty {
    flex-direction: column;
    align-items: flex-start;
  }

  .access-key-expiry :deep(.el-select) {
    width: 110px;
    flex-basis: 110px;
  }

  .help-guide {
    padding: 18px 12px 28px;
  }

  .help-guide-content {
    font-size: 13px;
  }

  .help-guide-content :deep(h1) {
    font-size: 22px;
  }

  .help-guide-content :deep(h2) {
    font-size: 18px;
  }

  .mobile-bottom-nav {
    display: grid;
    grid-template-columns: repeat(4, minmax(0, 1fr));
    gap: 8px;
    align-items: center;
    padding: 10px 12px calc(10px + env(safe-area-inset-bottom));
    border-top: 1px solid #eef1f4;
    background: rgba(255, 255, 255, 0.96);
    backdrop-filter: blur(12px);
  }

  .mobile-nav-hint {
    display: flex;
    align-items: center;
    gap: 8px;
    grid-column: 1 / -1;
    font-size: 12px;
    color: #909399;
    padding: 0 4px 2px;
  }

  .mobile-nav-badge {
    width: 6px;
    height: 6px;
    border-radius: 999px;
    background: #409eff;
  }

  .mobile-nav-item {
    border: none;
    background: transparent;
    border-radius: 14px;
    color: #606266;
    padding: 10px 8px;
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 6px;
    font-size: 12px;
    cursor: pointer;
    transition: all 0.2s ease;
  }

  .mobile-nav-item.active {
    color: #1c4fb8;
    background: #edf6ff;
    font-weight: 600;
  }
}

</style>
