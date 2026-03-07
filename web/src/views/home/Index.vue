<script setup lang="ts">
import { ref, onMounted, onBeforeUnmount, computed, watch, defineAsyncComponent, nextTick } from 'vue'
import { storeToRefs } from 'pinia'
import { ArrowLeft, ArrowUp, Delete, Expand, Fold, FolderAdd, FolderOpened, Grid, Refresh, Upload, DocumentCopy, Share, Search, MoreFilled, Notebook, User } from '@element-plus/icons-vue'
import { ElMessageBox } from 'element-plus'
import { quotaApi, userApi, recycleApi, shareApi, directShareApi, assetsApi, type RecycleItem, type ShareItem, type DirectShareItem, type AssetSpaceInfo } from '@/api'
import { isLoggedIn, hasWallet, getUsername, getWalletName, getCurrentAccount, getUserPermissions, getUserCreatedAt, loginWithWallet, loginWithPassword, sendEmailCode, loginWithEmailCode, getAccountHistory, watchWalletAccounts, consumeAccountChanged } from '@/plugins/auth'
import { parsePropfindResponse } from '@/utils/webdav'
import { copyText } from '@/utils/clipboard'
import { shortenAddress } from '@/utils/address'
import { showInfo, showSuccess } from '@/utils/toast'
import { useAddressBookStore } from '@/stores/addressBookStore'
import AddressBookView from './components/AddressBookView.vue'
import HomeOverlays from './components/HomeOverlays.vue'
import FileTableView from './components/FileTableView.vue'
import ShareTableView from './components/ShareTableView.vue'
import SharedWithMeTableView from './components/SharedWithMeTableView.vue'
import RecycleTableView from './components/RecycleTableView.vue'
import UploadTaskListView from './components/UploadTaskListView.vue'
import type { DropEntry, FileItem, UploadItem, UploadTask } from './types'

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
  created_at?: string
  updated_at?: string
  has_password?: boolean
} | null>(null)
const uploadTasks = ref<UploadTask[]>([])
const loginSubmitting = ref(false)
const showPasswordLoginForm = ref(false)
const passwordLoginForm = ref({
  username: '',
  password: ''
})
const showEmailLoginForm = ref(false)
const emailLoginForm = ref({
  email: '',
  code: ''
})
const emailCodeSending = ref(false)
const emailLoginSubmitting = ref(false)
const emailCodeCountdown = ref(0)
let emailCodeTimer: number | null = null
const walletHistory = ref<string[]>([])
const selectedWalletAccount = ref('')
const walletRowRef = ref<HTMLElement | null>(null)
let stopAccountWatch: (() => void) | null = null

// 回收站相关状态
const showRecycle = ref(false)
const recycleList = ref<RecycleItem[]>([])
const recycleLoading = ref(false)
const recycleClearing = ref(false)
const showShare = ref(false)
const shareList = ref<ShareItem[]>([])
const shareLoading = ref(false)
const shareTab = ref<'link' | 'direct'>('link')
const directShareList = ref<DirectShareItem[]>([])
const directShareLoading = ref(false)
const showSharedWithMe = ref(false)
const sharedWithMeList = ref<DirectShareItem[]>([])
const sharedWithMeLoading = ref(false)
const sharedActive = ref<DirectShareItem | null>(null)
const sharedPath = ref('/')
const sharedEntries = ref<FileItem[]>([])
const sharedEntriesLoading = ref(false)
const showQuotaManage = ref(false)
const quotaManageLoading = ref(false)
const showAddressBook = ref(false)
const showUploadTasks = ref(false)
const uploadTasksReturnView = ref<ViewKey | null>(null)
const uploadTasksReturnPath = ref('/')
const manualRefresh = ref(false)
const detailDrawerVisible = ref(false)
const detailMode = ref<'file' | 'recycle' | 'share' | 'directShare' | 'receivedShare' | 'sharedEntry' | null>(null)
const detailItem = ref<FileItem | RecycleItem | ShareItem | DirectShareItem | null>(null)
const dragActive = ref(false)
const dragCounter = ref(0)
const shareUserDialogVisible = ref(false)
const shareUserSubmitting = ref(false)
const shareUserTarget = ref<FileItem | null>(null)
const shareUserForm = ref({
  targetMode: 'single' as 'single' | 'group',
  targetAddress: '',
  groupId: '',
  permissions: ['read'] as string[],
  expiresIn: '0'
})
const addressBookStore = useAddressBookStore()
const { addressBookLoading, addressGroups, addressContacts } = storeToRefs(addressBookStore)
const editingUsername = ref(false)
const usernameDraft = ref('')
const usernameSaving = ref(false)
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
  name: ''
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
type PreviewMode = 'text' | 'pdf' | 'word' | 'image'
const previewVisible = ref(false)
const previewMode = ref<PreviewMode | null>(null)
const previewLoading = ref(false)
const previewSaving = ref(false)
const previewContent = ref('')
const previewOrigin = ref('')
const previewTarget = ref<FileItem | null>(null)
const previewBlob = ref<Blob | null>(null)
const previewReadOnly = ref(false)
let previewRequestSeq = 0
const VIEW_STORAGE_KEY = 'warehouse:lastView'
const FILE_PATH_STORAGE_KEY = 'warehouse:lastFilePath'
const SHARED_ACTIVE_STORAGE_KEY = 'warehouse:sharedActiveId'
const SHARED_PATH_STORAGE_KEY = 'warehouse:sharedPath'
const SIDEBAR_COLLAPSED_STORAGE_KEY = 'warehouse:sidebarCollapsed'
type ViewKey = 'files' | 'recycle' | 'shareLink' | 'shareDirect' | 'sharedWithMe' | 'quotaManage' | 'addressBook' | 'uploadTasks'
type AssetSpace = AssetSpaceInfo
type DirectShareRelation = 'owned' | 'received'
type DirectShareListItem = DirectShareItem & { relation?: DirectShareRelation }
const ASSET_SPACE_NAME_BY_KEY: Record<string, string> = {
  personal: '个人资产',
  apps: '应用资产'
}
const ASSET_SPACE_PATH_BY_KEY: Record<string, string> = {
  personal: '/personal',
  apps: '/apps'
}
const DEFAULT_ASSET_SPACES: AssetSpace[] = [
  { key: 'personal', name: '个人资产', path: '/personal' },
  { key: 'apps', name: '应用资产', path: '/apps' }
]
const assetSpaces = ref<AssetSpace[]>(DEFAULT_ASSET_SPACES)
const defaultAssetSpaceKey = ref('personal')
const assetSpaceLoading = ref(false)
const appsRootDirectories = ref<Array<{ name: string; path: string }>>([])
const sidePanelCollapsed = ref(false)

// 是否显示回收站列表
const isFileView = computed(() => !showRecycle.value && !showShare.value && !showQuotaManage.value && !showSharedWithMe.value && !showAddressBook.value && !showUploadTasks.value)
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
const showSearch = computed(() => !showQuotaManage.value && !showAddressBook.value && !showUploadTasks.value)
const showListHeader = computed(() => !showQuotaManage.value && !showAddressBook.value)
type MobileAction = { command: string; label: string; disabled?: boolean }
type MobileActionGroup = { title: string; items: MobileAction[] }

const mobileActionGroups = computed<MobileActionGroup[]>(() => {
  if (showUploadTasks.value) {
    return []
  }
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
    if (showUploadTasks.value) return ''
    if (showRecycle.value) return recycleSearch.value
    if (showShare.value) return shareTab.value === 'link' ? shareLinkSearch.value : shareDirectSearch.value
    if (showSharedWithMe.value) return sharedSearch.value
    return fileSearch.value
  },
  set: (value: string) => {
    if (showUploadTasks.value) return
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
  if (showUploadTasks.value) return '搜索上传任务'
  if (showRecycle.value) return '搜索回收站'
  if (showShare.value) return shareTab.value === 'link' ? '搜索下载链接' : '搜索定向分享'
  if (showSharedWithMe.value) return sharedActive.value ? '搜索共享内容' : '搜索定向分享'
  return '搜索文件或目录'
})
const currentAssetSpace = computed(() => resolveAssetSpaceByPath(currentPath.value))
const currentAssetSpaceKey = computed(() => currentAssetSpace.value?.key || '')
const fileParentRootPath = computed(() => normalizeDirectoryPath(currentAssetSpace.value?.path || '/'))
const canGoFileParent = computed(() => {
  const current = normalizePathForSpaceMatch(currentPath.value)
  const root = normalizePathForSpaceMatch(fileParentRootPath.value)
  if (root === '/') return current !== '/'
  return current !== root
})
const appsSpacePath = computed(() => getAppsSpacePath())
const isInAppsSpace = computed(() => isPathInSpace(currentPath.value, appsSpacePath.value))
const currentTopLevelApp = computed(() => getTopLevelAppFromPath(currentPath.value))
const showAppQuickRow = computed(() => isFileView.value && isInAppsSpace.value && appsRootDirectories.value.length > 0)
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
const mobileLocationLabel = computed(() => {
  if (showRecycle.value) return '回收站'
  if (showShare.value) return shareTab.value === 'link' ? '下载链接' : '定向分享'
  if (showSharedWithMe.value) return sharedActive.value ? '共享内容' : '定向分享'
  if (showQuotaManage.value) return '个人中心'
  if (showAddressBook.value) return '我的好友'
  if (showUploadTasks.value) return '上传任务'
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
const detailReceivedShare = computed(() => (detailMode.value === 'receivedShare' ? (detailItem.value as DirectShareItem | null) : null))
const detailSharedEntry = computed(() => (detailMode.value === 'sharedEntry' ? (detailItem.value as FileItem | null) : null))
const previewTitle = computed(() => {
  if (!previewTarget.value) return '文件预览'
  if (previewMode.value === 'text') return `编辑：${previewTarget.value.name}`
  return `预览：${previewTarget.value.name}`
})
const previewDirty = computed(
  () => previewMode.value === 'text' && !previewReadOnly.value && previewContent.value !== previewOrigin.value
)
const groupedContacts = computed(() => {
  const groupId = shareUserForm.value.groupId
  if (shareUserForm.value.targetMode !== 'group') return []
  if (!groupId) {
    return addressContacts.value.filter(item => !item.groupId)
  }
  return addressContacts.value.filter(item => item.groupId === groupId)
})
const quotaAvailable = computed(() => {
  if (quota.value.unlimited) return null
  const available = Number.isFinite(quota.value.available)
    ? quota.value.available
    : quota.value.quota - quota.value.used
  return Math.max(available, 0)
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
  const seen = new Set(ownedRows.map(item => item.id))
  const receivedRows = sharedWithMeList.value
    .filter(item => !seen.has(item.id))
    .map(item => ({ ...item, relation: 'received' as const }))
  return [...ownedRows, ...receivedRows].sort((a, b) => {
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
const searchToken = computed(() => searchKeyword.value.trim().toLowerCase())
const filteredFileList = computed(() => {
  const token = searchToken.value
  if (!token) return fileList.value
  return fileList.value.filter(item => item.name.toLowerCase().includes(token))
})
const filteredRecycleList = computed(() => {
  const token = searchToken.value
  if (!token) return recycleList.value
  return recycleList.value.filter(item => {
    const nameMatch = item.name.toLowerCase().includes(token)
    if (nameMatch) return true
    return item.path?.toLowerCase().includes(token) || false
  })
})
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
    if (item.ownerName?.toLowerCase().includes(token)) return true
    if (item.ownerWallet?.toLowerCase().includes(token)) return true
    const relationLabel = isDirectShareOwner(item) ? '我分享的' : '分享我的'
    return relationLabel.includes(token)
  })
})
const filteredSharedWithMeList = computed(() => {
  const token = searchToken.value
  if (!token) return sharedWithMeList.value
  return sharedWithMeList.value.filter(item => {
    if (item.name.toLowerCase().includes(token)) return true
    if (item.ownerName?.toLowerCase().includes(token)) return true
    return item.ownerWallet?.toLowerCase().includes(token) || false
  })
})
const filteredSharedEntries = computed(() => {
  const token = searchToken.value
  if (!token) return sharedEntries.value
  return sharedEntries.value.filter(item => item.name.toLowerCase().includes(token))
})
const previewImageItems = computed(() => {
  const items = isSharedBrowse.value ? sharedEntries.value : fileList.value
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
  return null
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

function normalizePathForSpaceMatch(path: string): string {
  const normalized = normalizeDirectoryPath(path)
  if (normalized === '/') return '/'
  return normalized.replace(/\/$/, '')
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

function getAppsSpacePath(): string {
  const appsSpace = normalizeAssetSpaces(assetSpaces.value).find(item => item.key === 'apps')
  return normalizeDirectoryPath(appsSpace?.path || '/apps')
}

function isPathInSpace(path: string, spacePath: string): boolean {
  const normalizedPath = normalizePathForSpaceMatch(path)
  const normalizedSpace = normalizePathForSpaceMatch(spacePath)
  if (normalizedSpace === '/') return true
  return normalizedPath === normalizedSpace || normalizedPath.startsWith(`${normalizedSpace}/`)
}

function getTopLevelAppFromPath(path: string): string {
  const appsRoot = normalizePathForSpaceMatch(getAppsSpacePath())
  const normalizedPath = normalizePathForSpaceMatch(path)
  if (!isPathInSpace(normalizedPath, appsRoot)) return ''
  if (normalizedPath === appsRoot) return ''
  const relative = normalizedPath.slice(appsRoot.length).replace(/^\/+/, '')
  const [appName] = relative.split('/')
  return appName || ''
}

function updateAppsRootDirectories(path: string, items: FileItem[]) {
  const appsRootPath = getAppsSpacePath()
  if (normalizePathForSpaceMatch(path) !== normalizePathForSpaceMatch(appsRootPath)) {
    return
  }
  const dirs = items
    .filter(item => item.isDir)
    .map(item => ({
      name: item.name,
      path: normalizeDirectoryPath(item.path)
    }))
    .sort((a, b) => a.name.localeCompare(b.name))
  appsRootDirectories.value = dirs
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

async function handleWalletLogin() {
  try {
    const preferred = selectedWalletAccount.value.trim()
    await loginWithWallet(preferred || undefined)
    window.location.reload()
  } catch (error: any) {
    showError(error?.message || '钱包登录失败')
  }
}

async function handleWalletHistoryVisibleChange(visible: boolean) {
  if (!visible) return
  await nextTick()
  const rowWidth = walletRowRef.value?.offsetWidth
  if (!rowWidth) return
  const poppers = Array.from(document.querySelectorAll<HTMLElement>('.login-history-select-popper'))
  const popper = poppers.find((item) => item.offsetParent !== null) ?? poppers[poppers.length - 1]
  if (!popper) return
  popper.style.width = `${rowWidth}px`
  popper.style.minWidth = `${rowWidth}px`
}

async function handlePasswordLogin() {
  const username = passwordLoginForm.value.username.trim()
  const password = passwordLoginForm.value.password
  if (!username || !password) {
    showError('请输入用户名和密码')
    return
  }
  loginSubmitting.value = true
  try {
    await loginWithPassword(username, password)
    window.location.reload()
  } catch (error: any) {
    showError(error?.message || '登录失败')
  } finally {
    loginSubmitting.value = false
  }
}

function clearEmailCodeTimer() {
  if (emailCodeTimer !== null) {
    clearInterval(emailCodeTimer)
    emailCodeTimer = null
  }
}

function startEmailCountdown(seconds: number) {
  clearEmailCodeTimer()
  emailCodeCountdown.value = Math.max(0, Math.floor(seconds))
  if (emailCodeCountdown.value <= 0) return
  emailCodeTimer = window.setInterval(() => {
    if (emailCodeCountdown.value <= 1) {
      emailCodeCountdown.value = 0
      clearEmailCodeTimer()
      return
    }
    emailCodeCountdown.value -= 1
  }, 1000)
}

async function handleSendEmailCode() {
  const email = emailLoginForm.value.email.trim()
  if (!email) {
    showError('请输入邮箱')
    return
  }
  emailCodeSending.value = true
  try {
    const data = await sendEmailCode(email)
    const retryAfter = Number(data?.retryAfter || 60)
    startEmailCountdown(retryAfter)
    showSuccess('验证码已发送')
  } catch (error: any) {
    showError(error?.message || '发送验证码失败')
  } finally {
    emailCodeSending.value = false
  }
}

async function handleEmailLogin() {
  const email = emailLoginForm.value.email.trim()
  const code = emailLoginForm.value.code.trim()
  if (!email || !code) {
    showError('请输入邮箱和验证码')
    return
  }
  emailLoginSubmitting.value = true
  try {
    await loginWithEmailCode(email, code)
    window.location.reload()
  } catch (error: any) {
    showError(error?.message || '登录失败')
  } finally {
    emailLoginSubmitting.value = false
  }
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
    fileList.value = parsePropfindResponse(text, currentPath.value, DAV_PREFIX)
    updateAppsRootDirectories(currentPath.value, fileList.value)
    if (isPathInSpace(currentPath.value, appsSpacePath.value) && appsRootDirectories.value.length === 0) {
      void fetchAppsRootDirectories()
    }
    console.log('parsed items:', fileList.value)
  } catch (error) {
    console.error('获取文件列表失败:', error)
  } finally {
    loading.value = false
  }
}

async function fetchAppsRootDirectories() {
  const rootPath = appsSpacePath.value
  const apiPath = buildDavPath(rootPath)
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
    if (!response.ok) return
    const text = await response.text()
    const parsed = parsePropfindResponse(text, rootPath, DAV_PREFIX)
    updateAppsRootDirectories(rootPath, parsed)
  } catch (error) {
    console.error('获取应用目录失败:', error)
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
    await Promise.all([fetchUserInfo(), fetchQuota()])
  } finally {
    quotaManageLoading.value = false
  }
}


function startEditUsername() {
  usernameDraft.value = userProfile.value.username || ''
  editingUsername.value = true
}

function cancelEditUsername() {
  editingUsername.value = false
  usernameDraft.value = ''
}

async function submitUsername() {
  const nextName = usernameDraft.value.trim()
  if (!nextName) {
    showError('用户名不能为空')
    return
  }
  if (nextName === userProfile.value.username) {
    cancelEditUsername()
    return
  }
  usernameSaving.value = true
  try {
    const data = await userApi.updateUsername(nextName)
    const finalName = data?.username || nextName
    if (userInfo.value) {
      userInfo.value.username = finalName
    }
    localStorage.setItem('username', finalName)
    const currentAccount = localStorage.getItem('currentAccount')
    if (!userInfo.value?.wallet_address || currentAccount === userProfile.value.username) {
      localStorage.setItem('currentAccount', finalName)
    }
    showSuccess('用户名已更新')
    editingUsername.value = false
  } catch (error: any) {
    console.error('更新用户名失败:', error)
    showError(error?.message || '更新用户名失败')
  } finally {
    usernameSaving.value = false
  }
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

function openShareDetail(mode: 'share' | 'directShare' | 'receivedShare', item: ShareItem | DirectShareItem) {
  detailItem.value = item
  detailMode.value = mode
  detailDrawerVisible.value = true
}

function openDirectShareDetail(item: DirectShareItem) {
  openShareDetail('directShare', item)
}

function openSharedEntryDetail(item: FileItem) {
  detailItem.value = item
  detailMode.value = 'sharedEntry'
  detailDrawerVisible.value = true
}

async function openFilePreview(item: FileItem) {
  const mode = getPreviewMode(item)
  if (!mode) {
    showError('暂不支持预览该类型文件')
    return
  }
  const requestSeq = ++previewRequestSeq
  detailDrawerVisible.value = false
  previewTarget.value = item
  previewMode.value = mode
  previewContent.value = ''
  previewOrigin.value = ''
  previewBlob.value = null
  previewReadOnly.value = isSharedBrowse.value && mode === 'text' && !sharedCanUpdate.value
  previewVisible.value = true
  previewLoading.value = true
  try {
    const token = localStorage.getItem('authToken') || ''
    if (mode === 'text') {
      const response = await fetch(
        isSharedBrowse.value && sharedActive.value
          ? buildShareUserUrl('/api/v1/public/share/user/download', new URLSearchParams({
              shareId: sharedActive.value.id,
              path: normalizeShareRelative(item.path)
            }))
          : buildDavPath(item.path),
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
    } else if (mode === 'pdf' || mode === 'word' || mode === 'image') {
      const response = await fetch(
        isSharedBrowse.value && sharedActive.value
          ? buildShareUserUrl('/api/v1/public/share/user/download', new URLSearchParams({
              shareId: sharedActive.value.id,
              path: normalizeShareRelative(item.path)
            }))
          : buildDavPath(item.path),
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
      previewBlob.value = await response.blob()
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
  previewTarget.value = null
  previewMode.value = null
  previewContent.value = ''
  previewOrigin.value = ''
  previewBlob.value = null
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
      const relPath = normalizeShareRelative(previewTarget.value.path)
      const params = new URLSearchParams({ shareId: sharedActive.value.id, path: relPath })
      const apiPath = buildShareUserUrl('/api/v1/public/share/user/upload', params)
      const blob = new Blob([previewContent.value], { type: 'text/plain;charset=utf-8' })
      const file = new File([blob], previewTarget.value.name, { type: 'text/plain;charset=utf-8' })
      const formData = new FormData()
      formData.append('file', file)
      const response = await fetch(apiPath, {
        method: 'PUT',
        headers: {
          'Authorization': `Bearer ${token}`
        },
        body: formData
      })
      if (!response.ok) {
        throw new Error(`保存失败: ${response.status}`)
      }
      previewOrigin.value = previewContent.value
      showSuccess('已保存')
      fetchSharedEntries(sharedPath.value)
    } else {
      const response = await fetch(buildDavPath(previewTarget.value.path), {
        method: 'PUT',
        headers: {
          'Authorization': `Bearer ${token}`,
          'Content-Type': 'text/plain; charset=utf-8'
        },
        body: previewContent.value
      })
      if (!response.ok) {
        throw new Error(`保存失败: ${response.status}`)
      }
      previewOrigin.value = previewContent.value
      showSuccess('已保存')
      fetchFiles(currentPath.value)
    }
  } catch (error: any) {
    console.error('保存文件失败:', error)
    showError(error?.message || '保存失败')
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

function handleRowClick(row: FileItem | RecycleItem | ShareItem | DirectShareItem) {
  if (showQuotaManage.value || showAddressBook.value || showUploadTasks.value) return
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
        enterSharedRoot(item)
      } else {
        openDirectShareDetail(item)
      }
    }
    return
  }
  if (showSharedWithMe.value) {
    if (!sharedActive.value) {
      const item = row as DirectShareItem
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
  if (showUploadTasks.value || manualRefresh.value) return
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
      await fetchUserCenter()
    } else if (showAddressBook.value) {
      await addressBookStore.fetchAddressBook()
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
  } else if (showAddressBook.value) {
    text = '我的好友'
  }
  await copyText(text, '已复制当前路径')
}

// 返回上级目录
function goParent() {
  const current = normalizeDirectoryPath(currentPath.value)
  const rootPath = fileParentRootPath.value
  const normalizedCurrent = normalizePathForSpaceMatch(current)
  const normalizedRoot = normalizePathForSpaceMatch(rootPath)
  if (normalizedCurrent === '/' || normalizedCurrent === normalizedRoot) return
  const parts = current.split('/').filter(Boolean)
  parts.pop()
  let parentPath = parts.length > 0 ? '/' + parts.join('/') + '/' : '/'
  if (normalizedRoot !== '/' && !isPathInSpace(parentPath, rootPath)) {
    parentPath = rootPath
  }
  fetchFiles(parentPath)
}

function goSharedParent() {
  if (sharedPath.value === '/') return
  const parts = sharedPath.value.split('/').filter(Boolean)
  parts.pop()
  const parentPath = parts.length > 0 ? '/' + parts.join('/') + '/' : '/'
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
  const apiPath = buildDavPath(item.path)
  showInfo('下载中...')

  try {
    const token = localStorage.getItem('authToken') || ''
    ensureAuthCookie(token)
    const a = document.createElement('a')
    a.href = apiPath
    a.download = item.name
    a.rel = 'noopener'
    document.body.appendChild(a)
    a.click()
    document.body.removeChild(a)
  } catch (error) {
    showError(`下载失败: ${String(error)}`)
  }
}

function buildShareUserUrl(path: string, params?: URLSearchParams) {
  const base = API_BASE ? API_BASE.replace(/\/+$/, '') : ''
  const query = params ? `?${params.toString()}` : ''
  return `${base}${path}${query}`
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

async function downloadSharedRoot(item: DirectShareItem) {
  if (!item || item.isDir) return
  const params = new URLSearchParams({ shareId: item.id, path: '' })
  const url = buildShareUserUrl('/api/v1/public/share/user/download', params)
  showInfo('下载中...')
  try {
    const token = localStorage.getItem('authToken') || ''
    ensureAuthCookie(token)
    const a = document.createElement('a')
    a.href = url
    a.download = item.name
    a.rel = 'noopener'
    document.body.appendChild(a)
    a.click()
    document.body.removeChild(a)
  } catch (error) {
    showError(`下载失败: ${String(error)}`)
  }
}

async function downloadSharedFile(item: FileItem) {
  if (!sharedActive.value) return
  const relPath = normalizeShareRelative(item.path)
  const params = new URLSearchParams({ shareId: sharedActive.value.id, path: relPath })
  const url = buildShareUserUrl('/api/v1/public/share/user/download', params)
  showInfo('下载中...')
  try {
    const token = localStorage.getItem('authToken') || ''
    ensureAuthCookie(token)
    const a = document.createElement('a')
    a.href = url
    a.download = item.name
    a.rel = 'noopener'
    document.body.appendChild(a)
    a.click()
    document.body.removeChild(a)
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
  openRenameDialog(item, 'file')
}

function renameSharedItem(item: FileItem) {
  if (!sharedActive.value || !sharedCanUpdate.value) return
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
        throw new Error(`重命名失败: ${response.status}`)
      }
      await fetchFiles(currentPath.value)
    } else {
      if (!sharedActive.value || !sharedCanUpdate.value) return
      const fromPath = context.normalized.replace(/^\/+/, '')
      const toPath = (context.parentPath === '/' ? '/' + newName : context.parentPath + newName).replace(/^\/+/, '')
      await directShareApi.rename(sharedActive.value.id, fromPath, toPath)
      fetchSharedEntries(sharedPath.value)
    }
    renameDialogVisible.value = false
  } catch (error: any) {
    console.error('重命名失败:', error)
    showError(error?.message || '重命名失败')
  } finally {
    renameSubmitting.value = false
  }
}

async function shareFile(item: FileItem) {
  if (item.isDir) return
  try {
    let expiresIn: number | undefined
    try {
      const { value } = await ElMessageBox.prompt(
        '设置有效期（小时，0 表示永不过期）',
        '创建分享链接',
        {
          confirmButtonText: '创建',
          cancelButtonText: '取消',
          inputPattern: /^\d+$/,
          inputErrorMessage: '请输入非负整数',
          inputValue: '0'
        }
      )
      const hours = parseInt(value, 10)
      if (Number.isFinite(hours) && hours > 0) {
        expiresIn = hours * 3600
      }
    } catch {
      return
    }

    const data = await shareApi.create(item.path, expiresIn)
    const url = data.url || `${window.location.origin}/api/v1/public/share/${data.token}`
    await copyText(url, '分享链接已复制')
  } catch (error) {
    console.error('创建分享失败:', error)
    showError('创建分享失败')
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
        await uploadFilesWithDirectories(files, directories)
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
    throw new Error(`创建目录失败: ${response.status}`)
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

function createUploadTask(item: UploadItem, options: { basePath: string; isShared: boolean; shareId?: string }): UploadTask {
  const relativePath = normalizeRelativePath(item.relativePath || item.file.name) || item.file.name
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
    targetPath: options.isShared ? undefined : buildTargetPath(options.basePath, relativePath),
    isShared: options.isShared,
    shareId: options.shareId,
    sharePath: options.isShared ? relativePath : undefined
  }
}

function addUploadTasks(tasks: UploadTask[]) {
  if (!tasks.length) return
  uploadTasks.value = [...tasks, ...uploadTasks.value]
}

function updateUploadTask(task: UploadTask, patch: Partial<UploadTask>) {
  const index = uploadTasks.value.findIndex(item => item.id === task.id)
  const updatedAt = Date.now()
  if (index === -1) {
    Object.assign(task, patch, { updatedAt })
    return
  }
  const current = uploadTasks.value[index]
  uploadTasks.value[index] = {
    ...current,
    ...patch,
    updatedAt
  }
}

function getUploadUrlForTask(task: UploadTask): string | null {
  if (task.isShared) {
    if (!task.shareId) return null
    const path = normalizeRelativePath(task.sharePath || task.relativePath || task.name)
    const params = new URLSearchParams({ shareId: task.shareId, path })
    return buildShareUserUrl('/api/v1/public/share/user/upload', params)
  }
  const targetPath = task.targetPath || buildTargetPath('', task.relativePath || task.name)
  return buildDavPath(targetPath)
}

function uploadFileWithProgress(
  url: string,
  file: File,
  token: string,
  useMultipart: boolean,
  onProgress: (progress: number) => void
): Promise<void> {
  return new Promise((resolve, reject) => {
    const xhr = new XMLHttpRequest()
    xhr.open('PUT', url, true)
    if (token) {
      xhr.setRequestHeader('Authorization', `Bearer ${token}`)
    }
    xhr.upload.onprogress = event => {
      if (!event.lengthComputable) return
      const percent = Math.min(99, Math.round((event.loaded / event.total) * 100))
      onProgress(percent)
    }
    xhr.onload = () => {
      if (xhr.status >= 200 && xhr.status < 300) {
        resolve()
      } else {
        reject(new Error(`上传失败: ${xhr.status}`))
      }
    }
    xhr.onerror = () => reject(new Error('上传失败'))
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

async function performUploadTask(task: UploadTask) {
  if (!task.file) {
    updateUploadTask(task, { status: 'failed', error: '文件已失效' })
    return
  }
  const url = getUploadUrlForTask(task)
  if (!url) {
    updateUploadTask(task, { status: 'failed', error: '上传目标无效' })
    return
  }
  const token = localStorage.getItem('authToken') || ''
  const useMultipart = task.isShared
  updateUploadTask(task, { status: 'uploading', progress: 0, error: undefined })
  try {
    await uploadFileWithProgress(url, task.file, token, useMultipart, progress => {
      updateUploadTask(task, { progress })
    })
    updateUploadTask(task, { status: 'success', progress: 100 })
  } catch (error: any) {
    updateUploadTask(task, { status: 'failed', error: error?.message || '上传失败' })
  }
}

async function uploadFilesWithDirectories(files: UploadItem[], extraDirectories?: Set<string>) {
  const cleanPath = currentPath.value.replace(/^\//, '').replace(/\/$/, '')
  const ensuredDirs = new Set<string>()
  const token = localStorage.getItem('authToken') || ''

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

  const tasks = files.map(item => createUploadTask(item, { basePath: cleanPath, isShared: false }))
  addUploadTasks(tasks)
  if (!showRecycle.value && !showShare.value && !showSharedWithMe.value && !showQuotaManage.value && !showAddressBook.value && !showUploadTasks.value) {
    enterUploadTasks('files')
  }

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

async function ensureSharedDirectories(path: string, ensured: Set<string>, shareId: string) {
  const trimmed = normalizeRelativePath(path)
  if (!trimmed) return
  const segments = trimmed.split('/').filter(Boolean)
  let current = ''
  for (const segment of segments) {
    current = current ? `${current}/${segment}` : segment
    if (ensured.has(current)) continue
    try {
      await directShareApi.createFolder(shareId, current)
      ensured.add(current)
    } catch (error) {
      throw error
    }
  }
}

async function uploadSharedFilesWithDirectories(files: UploadItem[], extraDirectories?: Set<string>) {
  if (!sharedActive.value) return
  const shareId = sharedActive.value.id
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

  const tasks = files.map(item => createUploadTask(item, { basePath: '', isShared: true, shareId }))
  addUploadTasks(tasks)
  if (!showUploadTasks.value) {
    enterUploadTasks('shared')
  }

  const dirsToCreate = Array.from(directories).filter(Boolean).sort((a, b) => a.split('/').length - b.split('/').length)
  for (const dir of dirsToCreate) {
    try {
      await ensureSharedDirectories(dir, ensuredDirs, shareId)
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

async function retryUploadTask(task: UploadTask) {
  if (task.status === 'uploading') return
  if (!task.file) {
    showError('文件已失效，无法重试')
    return
  }
  try {
    if (task.isShared) {
      if (!task.shareId) {
        showError('共享信息已失效，无法重试')
        return
      }
      const relative = normalizeRelativePath(task.sharePath || task.relativePath || task.name)
      const dir = relative.includes('/') ? relative.slice(0, relative.lastIndexOf('/')) : ''
      if (dir) {
        await ensureSharedDirectories(dir, new Set<string>(), task.shareId)
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
    if (showSharedWithMe.value && sharedActive.value?.id === task.shareId) {
      fetchSharedEntries(sharedPath.value)
    }
    return
  }
  if (!showRecycle.value && !showShare.value && !showSharedWithMe.value && !showQuotaManage.value && !showAddressBook.value && !showUploadTasks.value) {
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
  if (!canUpload.value) return
  dragCounter.value += 1
  dragActive.value = true
}

function handleDragOver(event: DragEvent) {
  event.preventDefault()
}

function handleDragLeave(event: DragEvent) {
  event.preventDefault()
  if (!canUpload.value) return
  dragCounter.value = Math.max(dragCounter.value - 1, 0)
  if (dragCounter.value === 0) {
    dragActive.value = false
  }
}

async function handleDrop(event: DragEvent) {
  event.preventDefault()
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
    await directShareApi.remove(sharedActive.value.id, relPath)
    fetchSharedEntries(sharedPath.value)
  } catch (error) {
    showError(`删除失败: ${String(error)}`)
  }
}

// 获取回收站列表
async function fetchRecycle() {
  recycleLoading.value = true
  try {
    const data = await recycleApi.list()
    recycleList.value = data.items
  } catch (error) {
    console.error('获取回收站失败:', error)
  } finally {
    recycleLoading.value = false
  }
}

// 进入回收站
function enterRecycle() {
  detailDrawerVisible.value = false
  showRecycle.value = true
  showShare.value = false
  showSharedWithMe.value = false
  showQuotaManage.value = false
  showAddressBook.value = false
  showUploadTasks.value = false
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
  showAddressBook.value = false
  showUploadTasks.value = false
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
    const cleanPath = path.replace(/^\/+/, '').replace(/\/$/, '')
    const data = await directShareApi.listEntries(sharedActive.value.id, cleanPath)
    sharedEntries.value = data.items as FileItem[]
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
  showAddressBook.value = false
  showUploadTasks.value = false
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

// 进入共享的目录
function enterSharedRoot(item: DirectShareItem) {
  if (!item.isDir) return
  detailDrawerVisible.value = false
  showSharedWithMe.value = true
  showShare.value = false
  showRecycle.value = false
  showQuotaManage.value = false
  showAddressBook.value = false
  showUploadTasks.value = false
  sharedActive.value = item
  sharedPath.value = '/'
  sharedEntries.value = []
  persistView('shareDirect')
  persistSharedState()
  fetchSharedEntries('/')
}

function backToSharedList() {
  detailDrawerVisible.value = false
  showSharedWithMe.value = false
  showShare.value = true
  showRecycle.value = false
  showQuotaManage.value = false
  showAddressBook.value = false
  showUploadTasks.value = false
  shareTab.value = 'direct'
  sharedActive.value = null
  sharedPath.value = '/'
  sharedEntries.value = []
  persistView('shareDirect')
  clearSharedState()
  fetchDirectShareList()
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

function openShareUserDialog(item: FileItem) {
  shareUserTarget.value = item
  shareUserForm.value = {
    targetMode: 'single',
    targetAddress: '',
    groupId: '',
    permissions: ['read'],
    expiresIn: '0'
  }
  shareUserDialogVisible.value = true
  addressBookStore.fetchAddressBook()
}

async function submitShareUser() {
  if (!shareUserTarget.value) return
  if (!shareUserForm.value.permissions.length) {
    showError('请至少选择一个权限')
    return
  }

  const hours = parseInt(shareUserForm.value.expiresIn, 10)
  const expiresIn = Number.isFinite(hours) && hours > 0 ? hours * 3600 : 0
  const rawPath = shareUserTarget.value.isDir
    ? shareUserTarget.value.path.replace(/\/$/, '')
    : shareUserTarget.value.path

  shareUserSubmitting.value = true
  try {
    if (shareUserForm.value.targetMode === 'group') {
      const targets = groupedContacts.value.map(item => item.walletAddress).filter(Boolean)
      const uniqueTargets = Array.from(new Set(targets.map(addr => addr.trim()).filter(Boolean)))
      if (!uniqueTargets.length) {
        showError('该分组没有可用地址')
        return
      }
      const tasks = uniqueTargets.map(address => directShareApi.create({
        path: rawPath,
        targetAddress: address,
        permissions: shareUserForm.value.permissions,
        expiresIn
      }))
      const results = await Promise.allSettled(tasks)
      const successCount = results.filter(result => result.status === 'fulfilled').length
      const failCount = results.length - successCount
      if (successCount > 0) {
        showSuccess(`已共享给 ${successCount} 位用户${failCount ? `，失败 ${failCount} 位` : ''}`)
      } else {
        showError('共享失败')
        return
      }
    } else {
      const targetAddress = shareUserForm.value.targetAddress.trim()
      if (!targetAddress) {
        showError('请输入目标钱包地址')
        return
      }
      await directShareApi.create({
        path: rawPath,
        targetAddress,
        permissions: shareUserForm.value.permissions,
        expiresIn
      })
      showSuccess('已分享给指定用户')
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

function enterAddressBook() {
  detailDrawerVisible.value = false
  showAddressBook.value = true
  showShare.value = false
  showRecycle.value = false
  showSharedWithMe.value = false
  showQuotaManage.value = false
  showUploadTasks.value = false
  sharedActive.value = null
  sharedPath.value = '/'
  persistView('addressBook')
  addressBookStore.fetchAddressBook()
}

// 进入个人中心
function enterQuotaManage() {
  detailDrawerVisible.value = false
  showQuotaManage.value = true
  showShare.value = false
  showRecycle.value = false
  showSharedWithMe.value = false
  showAddressBook.value = false
  showUploadTasks.value = false
  sharedActive.value = null
  sharedPath.value = '/'
  persistView('quotaManage')
  fetchUserCenter()
}

function enterUploadTasks(source?: 'files' | 'shared') {
  const resolved = source || (showSharedWithMe.value ? 'shared' : 'files')
  if (resolved === 'shared') {
    uploadTasksReturnView.value = 'sharedWithMe'
    if (sharedActive.value) {
      persistSharedState()
    } else {
      clearSharedState()
    }
  } else {
    uploadTasksReturnView.value = 'files'
    uploadTasksReturnPath.value = currentPath.value
  }
  detailDrawerVisible.value = false
  showUploadTasks.value = true
  showShare.value = false
  showRecycle.value = false
  showSharedWithMe.value = false
  showQuotaManage.value = false
  showAddressBook.value = false
  persistView('uploadTasks')
}

async function exitUploadTasks() {
  const returnView = uploadTasksReturnView.value
  uploadTasksReturnView.value = null
  if (returnView === 'sharedWithMe') {
    await restoreSharedWithMeView()
    return
  }
  enterFiles(uploadTasksReturnPath.value || currentPath.value)
}

function openCreateFolderDialog(mode: 'file' | 'shared') {
  if (mode === 'shared' && (!sharedActive.value || !sharedCanCreate.value)) return
  createFolderMode.value = mode
  createFolderForm.value = { name: '' }
  createFolderDialogVisible.value = true
}

function createFolder() {
  if (isSharedBrowse.value) {
    openCreateFolderDialog('shared')
    return
  }
  openCreateFolderDialog('file')
}

async function createFolderWithName(name: string) {
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
    fetchFiles(currentPath.value)
    if (response.status === 405) {
      showError('文件夹已存在')
    }
    return
  }
  throw new Error(`创建失败: ${response.status}`)
}

async function createSharedFolderWithName(name: string) {
  if (!sharedActive.value || !sharedCanCreate.value) return
  const basePath = sharedPath.value.replace(/^\/+/, '').replace(/\/$/, '')
  const targetPath = basePath ? `${basePath}/${name}` : name
  await directShareApi.createFolder(sharedActive.value.id, targetPath)
  fetchSharedEntries(sharedPath.value)
}

async function submitCreateFolder() {
  const mode = createFolderMode.value
  if (!mode) return
  const name = createFolderForm.value.name.trim()
  if (!name) {
    showError('请输入文件夹名称')
    return
  }
  if (name.includes('/')) {
    showError('名称不能包含 "/"')
    return
  }
  createFolderSubmitting.value = true
  try {
    if (mode === 'shared') {
      await createSharedFolderWithName(name)
    } else {
      await createFolderWithName(name)
    }
    createFolderDialogVisible.value = false
  } catch (error: any) {
    console.error('创建失败:', error)
    showError(error?.message || '创建失败')
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

function clearSharedState() {
  localStorage.removeItem(SHARED_ACTIVE_STORAGE_KEY)
  localStorage.removeItem(SHARED_PATH_STORAGE_KEY)
}

function persistSharedState() {
  if (!sharedActive.value) return
  localStorage.setItem(SHARED_ACTIVE_STORAGE_KEY, sharedActive.value.id)
  localStorage.setItem(SHARED_PATH_STORAGE_KEY, sharedPath.value)
}

async function restoreSharedWithMeView() {
  enterShare('direct')
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
    enterQuotaManage()
    return
  }
  if (storedView === 'addressBook') {
    enterAddressBook()
    return
  }
  if (storedView === 'uploadTasks') {
    enterUploadTasks()
    return
  }
  const storedPath = localStorage.getItem(FILE_PATH_STORAGE_KEY) || '/'
  enterFiles(resolveInitialFilePath(storedPath))
}

watch(renameDialogVisible, visible => {
  if (visible) return
  renameTarget.value = null
  renameMode.value = null
  renameForm.value = { name: '' }
})

watch(createFolderDialogVisible, visible => {
  if (visible) return
  createFolderMode.value = null
  createFolderForm.value = { name: '' }
})

watch(addressGroups, groups => {
  const groupId = shareUserForm.value.groupId
  if (!groupId) return
  const exists = groups.some(group => group.id === groupId)
  if (!exists) {
    shareUserForm.value.groupId = ''
  }
})

onMounted(() => {
  try {
    sidePanelCollapsed.value = localStorage.getItem(SIDEBAR_COLLAPSED_STORAGE_KEY) === '1'
  } catch {
    sidePanelCollapsed.value = false
  }
})

onMounted(() => {
  if (isLoggedIn()) {
    fetchQuota()
    fetchUserInfo()
    void (async () => {
      await fetchAssetSpaces()
      await restoreView()
    })()
  }
})

function handleExternalNavigate(event: Event) {
  const customEvent = event as CustomEvent<{ view?: ViewKey }>
  const view = customEvent?.detail?.view
  if (!view) return
  if (view === 'quotaManage') {
    enterQuotaManage()
    return
  }
  if (view === 'addressBook') {
    enterAddressBook()
    return
  }
  if (view === 'uploadTasks') {
    enterUploadTasks()
    return
  }
}

onMounted(() => {
  window.addEventListener('warehouse:navigate', handleExternalNavigate as EventListener)
})

onBeforeUnmount(() => {
  window.removeEventListener('warehouse:navigate', handleExternalNavigate as EventListener)
})

function syncWalletHistory(next?: string) {
  walletHistory.value = getAccountHistory()
  if (next) {
    selectedWalletAccount.value = next
    return
  }
  if (!selectedWalletAccount.value && walletHistory.value.length > 0) {
    selectedWalletAccount.value = walletHistory.value[0]
  }
}

onMounted(() => {
  const changed = consumeAccountChanged()
  if (changed) {
    showInfo('钱包账户已切换，请重新登录')
  }
  syncWalletHistory()
  void (async () => {
    stopAccountWatch = await watchWalletAccounts(({ account }) => {
      syncWalletHistory(account || undefined)
    })
  })()
})

onBeforeUnmount(() => {
  stopAccountWatch?.()
  clearEmailCodeTimer()
})
</script>

<template>
  <div class="home-container">
    <!-- 未登录状态 -->
    <div v-if="!isLoggedIn()" class="login-page">
      <div class="login-floating-container">
        <div class="login-top-banner">
          <div class="login-top-banner-inner">
            <img src="/logo.svg" alt="资产仓库" class="login-top-banner-logo" />
            <span>
              资产仓库 · 连接钱包或账号后，安全管理文件、分享与回收站
              <a href="https://www.yeying.pub" target="_blank" rel="noopener noreferrer">
                了解夜莺社区
              </a>
            </span>
          </div>
        </div>
        <div class="login-hero">
          <div class="login-card">
            <div class="login-section">
              <div v-if="hasWallet() && walletHistory.length" ref="walletRowRef" class="login-wallet-row">
                <el-select
                  v-model="selectedWalletAccount"
                  placeholder="历史账户（可选）"
                  class="login-history-select"
                  popper-class="login-history-select-popper"
                  @visible-change="handleWalletHistoryVisibleChange"
                >
                  <el-option
                    v-for="accountItem in walletHistory"
                    :key="accountItem"
                    :value="accountItem"
                    :label="shortenAddress(accountItem)"
                  >
                    <span class="mono">{{ accountItem }}</span>
                  </el-option>
                </el-select>
                <el-button
                  type="primary"
                  class="login-main-btn login-wallet-btn login-wallet-action-btn"
                  @click="handleWalletLogin"
                >
                  钱包登陆
                </el-button>
              </div>
              <el-button
                v-else-if="hasWallet()"
                type="primary"
                class="login-main-btn login-wallet-btn"
                @click="handleWalletLogin"
              >
                钱包登陆
              </el-button>
              <div v-else class="login-warning">未检测到钱包插件</div>
            </div>
            <div class="login-divider"><span>或</span></div>
            <div class="login-section">
              <el-button
                type="primary"
                class="login-main-btn login-password-btn"
                @click="showPasswordLoginForm = !showPasswordLoginForm"
              >
                密码登陆
              </el-button>
              <div class="login-form-shell" :class="{ 'is-collapsed': !showPasswordLoginForm }">
                <el-form class="login-form" @submit.prevent="handlePasswordLogin">
                  <el-form-item>
                    <el-input
                      v-model="passwordLoginForm.username"
                      placeholder="用户名"
                      autocomplete="username"
                    />
                  </el-form-item>
                  <el-form-item>
                    <el-input
                      v-model="passwordLoginForm.password"
                      type="password"
                      show-password
                      placeholder="密码"
                      autocomplete="current-password"
                    />
                  </el-form-item>
                  <el-button
                    type="primary"
                    native-type="submit"
                    :loading="loginSubmitting"
                    class="login-submit login-password-submit-btn"
                  >
                    登录
                  </el-button>
                </el-form>
              </div>
            </div>
            <div class="login-divider"><span>或</span></div>
            <div class="login-section">
              <el-button
                type="primary"
                class="login-main-btn login-email-btn"
                @click="showEmailLoginForm = !showEmailLoginForm"
              >
                邮箱登陆
              </el-button>
              <div class="login-form-shell" :class="{ 'is-collapsed': !showEmailLoginForm }">
                <el-form class="login-form" @submit.prevent="handleEmailLogin">
                  <el-form-item>
                    <el-input
                      v-model="emailLoginForm.email"
                      placeholder="邮箱"
                      autocomplete="email"
                    />
                  </el-form-item>
                  <el-form-item>
                    <div class="email-code-row">
                      <el-input
                        v-model="emailLoginForm.code"
                        placeholder="验证码"
                        autocomplete="one-time-code"
                      />
                      <el-button
                        type="primary"
                        class="email-code-button login-code-btn"
                        native-type="button"
                        :loading="emailCodeSending"
                        :disabled="emailCodeCountdown > 0"
                        @click="handleSendEmailCode"
                      >
                        {{ emailCodeCountdown > 0 ? `${emailCodeCountdown}s` : '发送验证码' }}
                      </el-button>
                    </div>
                  </el-form-item>
                  <el-button
                    type="primary"
                    native-type="submit"
                    :loading="emailLoginSubmitting"
                    class="login-submit login-email-submit-btn"
                  >
                    登录
                  </el-button>
                </el-form>
              </div>
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
            <div class="nav-group-title" v-show="!sidePanelCollapsed">资产类型</div>
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
            </div>
          </div>

          <div class="nav-group">
            <div class="nav-group-title" v-show="!sidePanelCollapsed">共享协作</div>
            <div class="nav-list">
              <el-tooltip content="下载链接" placement="right" :disabled="!sidePanelCollapsed">
                <button
                  type="button"
                  class="nav-item"
                  :class="{ active: showShare && shareTab === 'link' }"
                  @click="enterShare('link')"
                >
                  <el-icon class="nav-icon"><DocumentCopy /></el-icon>
                  <span v-show="!sidePanelCollapsed">下载链接</span>
                </button>
              </el-tooltip>
              <el-tooltip content="定向分享" placement="right" :disabled="!sidePanelCollapsed">
                <button
                  type="button"
                  class="nav-item"
                  :class="{ active: showShare && shareTab === 'direct' }"
                  @click="enterShare('direct')"
                >
                  <el-icon class="nav-icon"><Share /></el-icon>
                  <span v-show="!sidePanelCollapsed">定向分享</span>
                </button>
              </el-tooltip>
            </div>
          </div>

          <div class="nav-group">
            <div class="nav-group-title" v-show="!sidePanelCollapsed">系统工具</div>
            <div class="nav-list">
              <el-tooltip content="上传任务" placement="right" :disabled="!sidePanelCollapsed">
                <button
                  type="button"
                  class="nav-item"
                  :class="{ active: showUploadTasks }"
                  @click="enterUploadTasks()"
                >
                  <el-icon class="nav-icon"><Upload /></el-icon>
                  <span v-show="!sidePanelCollapsed">上传任务</span>
                </button>
              </el-tooltip>
              <el-tooltip content="回收站" placement="right" :disabled="!sidePanelCollapsed">
                <button
                  type="button"
                  class="nav-item"
                  :class="{ active: showRecycle }"
                  @click="enterRecycle"
                >
                  <el-icon class="nav-icon"><Delete /></el-icon>
                  <span v-show="!sidePanelCollapsed">回收站</span>
                </button>
              </el-tooltip>
            </div>
          </div>

          <div class="nav-group">
            <div class="nav-group-title" v-show="!sidePanelCollapsed">账户管理</div>
            <div class="nav-list">
              <el-tooltip content="个人中心" placement="right" :disabled="!sidePanelCollapsed">
                <button
                  type="button"
                  class="nav-item"
                  :class="{ active: showQuotaManage }"
                  @click="enterQuotaManage"
                >
                  <el-icon class="nav-icon"><User /></el-icon>
                  <span v-show="!sidePanelCollapsed">个人中心</span>
                </button>
              </el-tooltip>
              <el-tooltip content="我的好友" placement="right" :disabled="!sidePanelCollapsed">
                <button
                  type="button"
                  class="nav-item"
                  :class="{ active: showAddressBook }"
                  @click="enterAddressBook"
                >
                  <el-icon class="nav-icon"><Notebook /></el-icon>
                  <span v-show="!sidePanelCollapsed">我的好友</span>
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
          <div v-if="showListHeader" class="list-header">
            <div class="list-header-left">
              <div class="path-row">
              <template v-if="showSharedWithMe && sharedActive">
                <el-tooltip content="返回共享列表" placement="top">
                  <el-button circle :icon="ArrowLeft" @click="backToSharedList" />
                </el-tooltip>
                <el-tooltip content="返回上级" placement="top">
                  <el-button circle :icon="ArrowUp" @click="goSharedParent" :disabled="sharedPath === '/'" />
                </el-tooltip>
              </template>
              <template v-else-if="showRecycle || showShare || showSharedWithMe || showUploadTasks">
                <el-tooltip :content="showUploadTasks ? '返回上一页' : '返回文件列表'" placement="top">
                  <el-button circle :icon="ArrowLeft" @click="showUploadTasks ? exitUploadTasks() : backToFiles()" />
                </el-tooltip>
              </template>
              <template v-else>
                <el-tooltip content="返回上级" placement="top">
                  <el-button circle :icon="ArrowUp" @click="goParent" :disabled="!canGoFileParent" />
                </el-tooltip>
              </template>
                <template v-if="showRecycle">
                  <div class="path-pill">
                    <span class="path-label">当前位置</span>
                    <span class="path-value">回收站</span>
                    <el-tooltip content="复制路径" placement="top">
                      <button type="button" class="copy-icon" @click="copyCurrentPath">
                        <el-icon><DocumentCopy /></el-icon>
                      </button>
                    </el-tooltip>
                  </div>
                </template>
                <template v-else-if="showShare">
                  <div class="path-pill">
                    <span class="path-label">当前位置</span>
                    <span class="path-value">{{ shareTab === 'link' ? '下载链接' : '定向分享' }}</span>
                  </div>
                </template>
                <template v-else-if="showSharedWithMe">
                  <template v-if="sharedActive">
                    <div class="breadcrumb">
                      <el-breadcrumb separator="/">
                        <el-breadcrumb-item>
                          <button class="breadcrumb-link" type="button" @click="setSharedPath('/')">
                            {{ sharedActive.name }}
                          </button>
                        </el-breadcrumb-item>
                        <el-breadcrumb-item v-for="crumb in sharedBreadcrumbItems" :key="crumb.path">
                          <button class="breadcrumb-link" type="button" @click="setSharedPath(crumb.path)">
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
                <template v-else-if="showUploadTasks">
                  <div class="path-pill">
                    <span class="path-label">当前位置</span>
                    <span class="path-value">上传任务</span>
                  </div>
                </template>
                <template v-else>
                  <div class="breadcrumb">
                    <el-breadcrumb separator="/">
                      <el-breadcrumb-item>
                        <button class="breadcrumb-link" type="button" @click="fetchFiles(fileBreadcrumbRoot.path)">
                          {{ fileBreadcrumbRoot.name }}
                        </button>
                      </el-breadcrumb-item>
                      <el-breadcrumb-item v-for="crumb in breadcrumbItems" :key="crumb.path">
                        <button class="breadcrumb-link" type="button" @click="fetchFiles(crumb.path)">
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
              </div>
              <div v-if="showAppQuickRow" class="app-quick-row">
                <button
                  type="button"
                  class="app-quick-item"
                  :class="{ active: !currentTopLevelApp }"
                  @click="enterAssetSpace('apps')"
                >
                  全部
                </button>
                <button
                  v-for="app in appsRootDirectories"
                  :key="app.path"
                  type="button"
                  class="app-quick-item"
                  :class="{ active: currentTopLevelApp === app.name }"
                  @click="fetchFiles(app.path)"
                >
                  {{ app.name }}
                </button>
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
                <template v-else-if="showUploadTasks">
                </template>
                <template v-else>
                  <el-tooltip content="新建文件夹" placement="top">
                    <el-button circle type="primary" :icon="FolderAdd" @click="createFolder" />
                  </el-tooltip>
                  <el-tooltip content="上传文件" placement="top">
                    <el-button circle type="primary" :icon="Upload" @click="triggerUpload" />
                  </el-tooltip>
                  <el-tooltip content="上传目录" placement="top">
                    <el-button circle type="primary" :icon="FolderOpened" @click="triggerDirectoryUpload" />
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
              <div class="user-card">
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
                    <div class="user-value user-inline">
                      <span v-if="!editingUsername" class="user-text">{{ userProfile.username }}</span>
                      <el-input
                        v-else
                        v-model="usernameDraft"
                        size="small"
                        class="user-input"
                        placeholder="请输入新用户名"
                      />
                      <div class="user-actions">
                        <el-button
                          v-if="!editingUsername"
                          size="small"
                          text
                          @click="startEditUsername"
                        >
                          修改
                        </el-button>
                        <template v-else>
                          <el-button
                            size="small"
                            type="primary"
                            :loading="usernameSaving"
                            @click="submitUsername"
                          >
                            保存
                          </el-button>
                          <el-button size="small" @click="cancelEditUsername">取消</el-button>
                        </template>
                      </div>
                    </div>
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
                        <el-button size="small" text @click="openPasswordDialog">
                          {{ userProfile.hasPassword ? '修改' : '设置' }}
                        </el-button>
                      </div>
                    </div>
                  </div>
                </div>
              </div>
              <div class="user-card">
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
                />
                <div class="quota-meta">
                  <span v-if="quota.unlimited">未设置上限</span>
                  <span v-else>已使用 {{ quota.percentage.toFixed(2) }}%</span>
                </div>
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
            </div>
          </div>
          <div v-else-if="showAddressBook" class="content-body content-scroll" v-loading="addressBookLoading && !manualRefresh">
            <AddressBookView @refresh="refreshCurrentView" />
          </div>
          <div v-else-if="showUploadTasks" class="content-body table-wrapper">
            <UploadTaskListView
              :tasks="uploadTasks"
              :format-size="formatSize"
              :format-time="formatTime"
              :retry-task="retryUploadTask"
            />
          </div>
          <div v-else class="content-body table-wrapper">
            <RecycleTableView
              v-if="showRecycle"
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
            <ShareTableView
              v-else-if="showShare"
              :share-tab="shareTab"
              :share-list="filteredShareList"
              :direct-share-list="filteredDirectShareList"
              :loading="(shareTab === 'link' ? shareLoading : directShareLoading) && !manualRefresh"
              :on-row-click="handleRowClick"
              :copy-share-link="copyShareLink"
              :revoke-share="revokeShare"
              :revoke-direct-share="revokeDirectShare"
              :open-direct-share-detail="openDirectShareDetail"
              :format-time="formatTime"
              :shorten-address="shortenAddress"
              :is-direct-share-owner="isDirectShareOwner"
            />
            <SharedWithMeTableView
              v-else-if="showSharedWithMe"
              :shared-active="sharedActive"
              :shared-with-me-list="filteredSharedWithMeList"
              :shared-entries="filteredSharedEntries"
              :loading="(sharedActive ? sharedEntriesLoading : sharedWithMeLoading) && !manualRefresh"
              :on-row-click="handleRowClick"
              :format-time="formatTime"
              :format-size="formatSize"
              :shorten-address="shortenAddress"
              :shared-can-read="sharedCanRead"
              :shared-can-update="sharedCanUpdate"
              :shared-can-delete="sharedCanDelete"
              :open-share-detail="openShareDetail"
              :download-shared-root="downloadSharedRoot"
              :get-preview-mode="getPreviewMode"
              :open-file-preview="openFilePreview"
              :open-shared-entry-detail="openSharedEntryDetail"
              :download-shared-file="downloadSharedFile"
              :rename-shared-item="renameSharedItem"
              :delete-shared-item="deleteSharedItem"
            />
            <FileTableView
              v-else
              :rows="filteredFileList"
              :loading="loading && !manualRefresh"
              :on-row-click="handleRowClick"
              :format-size="formatSize"
              :format-time="formatTime"
              :get-preview-mode="getPreviewMode"
              :open-file-preview="openFilePreview"
              :open-detail-drawer="openDetailDrawer"
              :download-file="downloadFile"
              :share-file="shareFile"
              :open-share-user-dialog="openShareUserDialog"
              :rename-item="renameItem"
              :delete-file="deleteFile"
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
        :get-share-link="getShareLink"
        :copy-share-link="copyShareLink"
        :revoke-share="revokeShare"
        :revoke-direct-share="revokeDirectShare"
        :is-direct-share-owner="isDirectShareOwner"
        :enter-directory="enterDirectory"
        :enter-shared-root="enterSharedRoot"
        :enter-shared-directory="enterSharedDirectory"
        :download-shared-root="downloadSharedRoot"
        :download-shared-file="downloadSharedFile"
        v-model:share-user-dialog-visible="shareUserDialogVisible"
        :share-user-submitting="shareUserSubmitting"
        :share-user-target="shareUserTarget"
        :share-user-form="shareUserForm"
        :address-contacts="addressContacts"
        :address-groups="addressGroups"
        :grouped-contacts="groupedContacts"
        :submit-share-user="submitShareUser"
        v-model:create-folder-dialog-visible="createFolderDialogVisible"
        :create-folder-submitting="createFolderSubmitting"
        :create-folder-form="createFolderForm"
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
      <FilePreviewDialog
        v-model="previewVisible"
        v-model:content="previewContent"
        :title="previewTitle"
        :mode="previewMode || 'text'"
        :blob="previewBlob"
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
          :class="{ active: showShare && shareTab === 'link' }"
          @click="enterShare('link')"
        >
          <el-icon><DocumentCopy /></el-icon>
          <span>下载</span>
        </button>
        <button
          type="button"
          class="mobile-nav-item"
          :class="{ active: showShare && shareTab === 'direct' }"
          @click="enterShare('direct')"
        >
          <el-icon><Share /></el-icon>
          <span>定向</span>
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
  display: flex;
  justify-content: center;
  align-items: center;
  width: 100%;
  height: 100%;
  padding: 24px;
  box-sizing: border-box;
  color: #606266;
}

.login-floating-container {
  width: min(1080px, 94vw);
  height: min(860px, 90vh);
  min-height: 620px;
  background: #ffffff;
  border: 0;
  border-radius: 20px;
  box-shadow: none;
  display: flex;
  flex-direction: column;
  overflow: hidden;
}

.login-top-banner {
  width: 100%;
  display: flex;
  justify-content: center;
  align-items: center;
  padding: 14px 24px;
  box-sizing: border-box;
  background: #eaf1fb;
  border-bottom: 1px solid #dce7f7;
}

.login-top-banner-inner {
  display: flex;
  align-items: center;
  gap: 10px;
  font-size: 14px;
  color: #2f3f57;

  span {
    display: inline-flex;
    align-items: center;
    gap: 8px;
    line-height: 1.5;
  }

  a {
    color: #2f7df6;
    text-decoration: none;
    white-space: nowrap;
  }

  a:hover {
    text-decoration: underline;
  }
}

.login-top-banner-logo {
  width: 22px;
  height: 22px;
  display: block;
  object-fit: contain;
}

.login-hero {
  flex: 1;
  width: 100%;
  padding: clamp(24px, 4vw, 44px);
  box-sizing: border-box;
  display: flex;
  align-items: center;
  justify-content: center;
  gap: clamp(20px, 3vw, 40px);
}

.login-warning {
  color: #cf8a1f;
  font-size: 14px;
  text-align: center;
  background: #fff8eb;
  border: 1px solid #f5dfb8;
  border-radius: 10px;
  padding: 8px 12px;
}

.login-card {
  width: min(430px, 100%);
  background: #fff;
  border: 0;
  border-radius: 14px;
  padding: 18px 18px;
  display: flex;
  flex-direction: column;
  gap: 14px;
  box-shadow: none;
}

.login-section {
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.login-main-btn {
  width: 100%;
  height: 40px;
  border-radius: 10px;
  font-weight: 500;
  align-self: stretch;
}

.login-history-select {
  width: 100%;
}

.login-wallet-row {
  width: 100%;
  display: flex;
  align-items: center;
  gap: 8px;
}

.login-wallet-row .login-history-select {
  flex: 1;
  min-width: 0;
}

.login-wallet-action-btn {
  width: 132px;
  flex: none;
}

:deep(.login-history-select .el-select__wrapper) {
  min-height: 40px;
  border-radius: 10px;
  box-shadow: inset 0 0 0 1.5px #9ab7e6;
  transition: box-shadow 0.16s ease;
}

:deep(.login-history-select .el-select__wrapper:hover) {
  box-shadow: inset 0 0 0 1.5px #6a97dd;
}

:deep(.login-history-select .el-select__wrapper.is-focused) {
  box-shadow: inset 0 0 0 2px #3f7fe0;
}

.login-divider {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 10px;
  color: #9aa5b5;
  font-size: 12px;
}

.login-divider::before,
.login-divider::after {
  content: '';
  height: 1px;
  flex: 1;
  background: #edf1f6;
}

.login-form :deep(.el-form-item) {
  margin-bottom: 12px;
}

.login-submit {
  width: 100%;
}

.email-code-row {
  display: flex;
  gap: 8px;
  align-items: center;
}

.email-code-row :deep(.el-input) {
  flex: 1;
}

.email-code-button {
  flex: none;
  white-space: nowrap;
}

:deep(.el-button.login-main-btn),
:deep(.el-button.login-submit),
:deep(.el-button.email-code-button) {
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
:deep(.el-button.login-submit:not(.is-disabled):hover),
:deep(.el-button.email-code-button:not(.is-disabled):hover) {
  background-color: #356fc7 !important;
}

:deep(.el-button.login-main-btn:not(.is-disabled):active),
:deep(.el-button.login-submit:not(.is-disabled):active),
:deep(.el-button.email-code-button:not(.is-disabled):active) {
  background-color: #2e62b2 !important;
  transform: translateY(0);
}

:deep(.el-button.login-main-btn.login-password-btn),
:deep(.el-button.login-main-btn.login-email-btn) {
  border: 1px solid #b7cae7 !important;
  color: #2f5fa8 !important;
  --el-button-text-color: #2f5fa8;
  --el-button-hover-text-color: #24508f;
  --el-button-active-text-color: #1f467d;
  --el-button-border-color: #b7cae7;
  --el-button-hover-border-color: #9fb8de;
  --el-button-active-border-color: #8caad7;
  background-color: #f4f8ff !important;
}

:deep(.el-button.login-main-btn.login-password-btn:not(.is-disabled):hover),
:deep(.el-button.login-main-btn.login-email-btn:not(.is-disabled):hover) {
  background-color: #ecf3ff !important;
}

:deep(.el-button.login-main-btn.login-password-btn:not(.is-disabled):active),
:deep(.el-button.login-main-btn.login-email-btn:not(.is-disabled):active) {
  background-color: #e4eeff !important;
}

:deep(.el-button.login-main-btn.is-disabled),
:deep(.el-button.login-submit.is-disabled),
:deep(.el-button.email-code-button.is-disabled) {
  border: 0 !important;
  color: #f3f7ff !important;
  background-image: none !important;
  background-color: #b7c7df !important;
  box-shadow: none !important;
}

.login-form-shell {
  width: 100%;
  overflow: hidden;
  max-height: 300px;
  opacity: 1;
  transform-origin: top;
  transition: max-height 0.28s ease, opacity 0.2s ease;
}

.login-form-shell.is-collapsed {
  max-height: 0;
  opacity: 0;
  pointer-events: none;
}

@media (max-width: 980px) {
  .login-hero {
    flex-direction: column;
    justify-content: flex-start;
  }

  .login-card {
    width: min(430px, 100%);
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
    border-radius: 0;
    border: 0;
    box-shadow: none;
  }

  .login-top-banner {
    padding: 10px 12px;
  }

  .login-top-banner-inner {
    font-size: 12px;
  }

  .login-hero {
    padding: 18px 14px;
    flex-direction: column;
    justify-content: flex-start;
    align-items: stretch;
    gap: 14px;
  }

  .login-card {
    width: 100%;
    padding: 14px;
  }

  .email-code-row {
    flex-wrap: wrap;
  }

  .email-code-button {
    width: 100%;
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

.app-quick-row {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-top: 8px;
  flex-wrap: wrap;
}

.app-quick-item {
  border: 1px solid #e4e9f2;
  background: #fff;
  color: #606266;
  border-radius: 999px;
  padding: 4px 10px;
  font-size: 12px;
  cursor: pointer;
  transition: all 0.2s ease;
}

.app-quick-item:hover {
  border-color: #bfd9ff;
  color: #409eff;
}

.app-quick-item.active {
  border-color: #409eff;
  background: #eef6ff;
  color: #1c4fb8;
  font-weight: 600;
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

.table-wrapper {
  display: flex;
  flex-direction: column;
  min-height: 0;
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

.quota-value {
  display: flex;
  align-items: baseline;
  gap: 6px;
  font-size: 18px;
  font-weight: 600;
  color: #1f2d3d;
}

.quota-sep {
  color: #c0c4cc;
  font-weight: 400;
}

.quota-meta {
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

  .app-quick-row {
    width: 100%;
    overflow-x: auto;
    flex-wrap: nowrap;
    -webkit-overflow-scrolling: touch;
    padding-bottom: 2px;
  }

  .app-quick-item {
    flex: 0 0 auto;
  }
}

</style>
