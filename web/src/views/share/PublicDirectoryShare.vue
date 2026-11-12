<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { ArrowLeft, Download, Document, Folder, Refresh } from '@element-plus/icons-vue'
import { shareApi, type ShareMode } from '@/api'

interface ShareEntry {
  name: string
  path: string
  isDir: boolean
  size: number
  modified: string
}

const route = useRoute()
const token = computed(() => String(route.params.token || ''))
const rootName = ref('共享目录')
const shareMode = ref<ShareMode>('download')
const currentPath = ref('')
const entries = ref<ShareEntry[]>([])
const offset = ref(0)
const hasMore = ref(false)
const loading = ref(false)
const error = ref(false)

const breadcrumbs = computed(() => {
  const parts = currentPath.value.split('/').filter(Boolean)
  return [
    { label: rootName.value, path: '' },
    ...parts.map((part, index) => ({ label: part, path: parts.slice(0, index + 1).join('/') }))
  ]
})

async function loadDirectory(path = '', append = false): Promise<void> {
  if (!token.value) {
    error.value = true
    return
  }
  loading.value = true
  error.value = false
  try {
    const nextOffset = append ? offset.value : 0
    const response = await shareApi.publicDirectoryEntries(token.value, path, nextOffset)
    rootName.value = response.name || '共享目录'
    shareMode.value = response.mode === 'preview' ? 'preview' : 'download'
    currentPath.value = response.path
    entries.value = append ? [...entries.value, ...response.entries] : response.entries
    offset.value = entries.value.length
    hasMore.value = response.hasMore
  } catch {
    error.value = true
    entries.value = []
    hasMore.value = false
  } finally {
    loading.value = false
  }
}

function openEntry(entry: ShareEntry): void {
  if (entry.isDir) {
    void loadDirectory(entry.path)
  }
}

function navigateTo(path: string): void {
  void loadDirectory(path)
}

function goBack(): void {
  const parts = currentPath.value.split('/').filter(Boolean)
  parts.pop()
  void loadDirectory(parts.join('/'))
}

function formatSize(size: number): string {
  if (size < 1024) return `${size} B`
  const units = ['KB', 'MB', 'GB', 'TB']
  let value = size / 1024
  let unit = 0
  while (value >= 1024 && unit < units.length - 1) {
    value /= 1024
    unit += 1
  }
  return `${value.toFixed(value >= 10 ? 0 : 1)} ${units[unit]}`
}

function formatModified(value: string): string {
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return ''
  return new Intl.DateTimeFormat(undefined, { year: 'numeric', month: 'short', day: 'numeric' }).format(date)
}

watch(token, value => {
  if (value) void loadDirectory('')
})

onMounted(() => void loadDirectory(''))
</script>

<template>
  <main class="public-share-page">
    <header class="share-header">
      <a class="brand" href="/" aria-label="Warehouse 首页">
        <img src="/logo.svg" alt="" />
        <span>Warehouse</span>
      </a>
      <span class="access-note">公开分享 · 只读访问</span>
    </header>

    <section class="share-content" aria-labelledby="folder-title">
      <div v-if="error" class="status-panel" role="alert">
        <div class="status-mark">!</div>
        <h1>这个分享暂时无法访问</h1>
        <p>链接可能已过期、已撤销，或目录已不可用。</p>
      </div>

      <template v-else>
        <div class="folder-heading">
          <div class="folder-emblem"><el-icon><Folder /></el-icon></div>
          <div class="folder-copy">
            <p class="eyebrow">共享文件夹</p>
            <h1 id="folder-title">{{ rootName }}</h1>
            <p class="folder-description">{{ shareMode === 'preview' ? '点击文件名可在浏览器中打开，也可以下载文件。' : '此链接允许查看目录内容并下载文件。' }}</p>
          </div>
        </div>

        <nav class="breadcrumbs" aria-label="目录位置">
          <button
            v-if="currentPath"
            class="back-button"
            type="button"
            aria-label="返回上一级目录"
            @click="goBack"
          >
            <el-icon><ArrowLeft /></el-icon>
          </button>
          <template v-for="(crumb, index) in breadcrumbs" :key="crumb.path">
            <button
              v-if="index < breadcrumbs.length - 1"
              class="crumb-link"
              type="button"
              @click="navigateTo(crumb.path)"
            >
              {{ crumb.label }}
            </button>
            <span v-else class="crumb-current" aria-current="location">{{ crumb.label }}</span>
            <span v-if="index < breadcrumbs.length - 1" class="crumb-separator" aria-hidden="true">/</span>
          </template>
        </nav>

        <div class="entry-panel" :aria-busy="loading">
          <div class="entry-heading">
            <span>{{ entries.length }} 个项目</span>
            <span class="column-label">修改日期 <span>大小</span></span>
          </div>

          <div v-if="loading && !entries.length" class="list-state">正在读取目录…</div>
          <div v-else-if="!entries.length" class="list-state empty-state">此目录为空</div>
          <ul v-else class="entry-list">
            <li v-for="entry in entries" :key="entry.path" class="entry-row">
              <button
                v-if="entry.isDir"
                class="entry-main entry-open"
                type="button"
                @click="openEntry(entry)"
              >
                <span class="entry-icon folder-icon"><el-icon><Folder /></el-icon></span>
                <span class="entry-name">{{ entry.name }}</span>
              </button>
              <a
                v-else-if="shareMode === 'preview'"
                class="entry-main file-open"
                :href="shareApi.publicDirectoryDownloadURL(token, entry.path)"
                target="_blank"
                rel="noopener"
                :aria-label="`在浏览器中打开 ${entry.name}`"
              >
                <span class="entry-icon file-icon"><el-icon><Document /></el-icon></span>
                <span class="entry-name">{{ entry.name }}</span>
              </a>
              <div v-else class="entry-main">
                <span class="entry-icon file-icon"><el-icon><Document /></el-icon></span>
                <span class="entry-name">{{ entry.name }}</span>
              </div>
              <span class="entry-date">{{ formatModified(entry.modified) }}</span>
              <span class="entry-size">{{ entry.isDir ? '—' : formatSize(entry.size) }}</span>
              <a
                v-if="!entry.isDir"
                class="download-link"
                :href="shareApi.publicDirectoryDownloadURL(token, entry.path)"
                :download="entry.name"
                :aria-label="`下载 ${entry.name}`"
              >
                <el-icon><Download /></el-icon>
                <span>下载</span>
              </a>
              <button v-else class="open-hint" type="button" :aria-label="`打开 ${entry.name}`" @click="openEntry(entry)">
                <el-icon><ArrowLeft /></el-icon>
              </button>
            </li>
          </ul>

          <button v-if="hasMore" class="load-more" type="button" :disabled="loading" @click="loadDirectory(currentPath, true)">
            <el-icon><Refresh /></el-icon>
            {{ loading ? '正在读取…' : '继续加载' }}
          </button>
        </div>
        <p class="share-footer">由 Warehouse 提供文件分享</p>
      </template>
    </section>
  </main>
</template>

<style scoped lang="scss">
.public-share-page {
  --share-ink: #182943;
  --share-muted: #718097;
  --share-blue: #2762cb;
  --share-blue-soft: #edf4ff;
  --share-line: #e1e7ef;
  --share-paper: #ffffff;
  min-height: 100vh;
  color: var(--share-ink);
  background: #f4f7fb;
  font-family: "Noto Sans", "SF Pro Text", "PingFang SC", sans-serif;
}

.share-header {
  height: 68px;
  padding: 0 clamp(20px, 6vw, 84px);
  display: flex;
  align-items: center;
  justify-content: space-between;
  border-bottom: 1px solid #e6ebf2;
  background: var(--share-paper);
}

.brand {
  display: inline-flex;
  align-items: center;
  gap: 10px;
  color: var(--share-ink);
  font-size: 16px;
  font-weight: 650;
  text-decoration: none;
}

.brand img { width: 28px; height: 28px; }
.access-note { color: var(--share-muted); font-size: 13px; }

.share-content {
  width: min(880px, calc(100% - 36px));
  margin: 58px auto 44px;
}

.folder-heading {
  display: flex;
  align-items: center;
  gap: 20px;
  margin-bottom: 32px;
}

.folder-emblem {
  width: 62px;
  height: 62px;
  flex: 0 0 auto;
  display: grid;
  place-items: center;
  color: var(--share-blue);
  background: var(--share-blue-soft);
  border-radius: 17px 17px 17px 5px;
  font-size: 28px;
}

.folder-copy { min-width: 0; }
.eyebrow { margin-bottom: 5px; color: var(--share-blue); font-size: 12px; font-weight: 650; }
.folder-copy h1 { overflow: hidden; color: var(--share-ink); font-size: clamp(23px, 4vw, 31px); font-weight: 650; text-overflow: ellipsis; white-space: nowrap; }
.folder-description { margin-top: 7px; color: var(--share-muted); font-size: 14px; }

.breadcrumbs {
  display: flex;
  align-items: center;
  gap: 10px;
  min-height: 48px;
  padding: 0 16px;
  color: var(--share-muted);
  background: #eaf0f7;
  border-radius: 8px 8px 0 0;
  overflow: hidden;
}

.crumb-link, .back-button, .open-hint {
  border: 0;
  padding: 0;
  color: var(--share-blue);
  background: transparent;
  cursor: pointer;
}

.crumb-link { overflow: hidden; max-width: 180px; font: inherit; text-overflow: ellipsis; white-space: nowrap; }
.crumb-link:hover { text-decoration: underline; }
.crumb-current { overflow: hidden; color: var(--share-ink); font-size: 13px; font-weight: 600; text-overflow: ellipsis; white-space: nowrap; }
.crumb-separator { color: #9aa7b8; }
.back-button { display: grid; place-items: center; width: 26px; height: 26px; margin-right: 2px; }
.back-button:focus-visible, .crumb-link:focus-visible, .entry-open:focus-visible, .file-open:focus-visible, .download-link:focus-visible, .load-more:focus-visible, .open-hint:focus-visible { outline: 3px solid #7eabf2; outline-offset: 3px; }

.entry-panel {
  overflow: hidden;
  background: var(--share-paper);
  border: 1px solid var(--share-line);
  border-top: 0;
  border-radius: 0 0 9px 9px;
  box-shadow: 0 8px 24px rgba(32, 55, 86, .045);
}

.entry-heading, .entry-row {
  display: grid;
  grid-template-columns: minmax(0, 1fr) 132px 90px 86px;
  align-items: center;
  column-gap: 12px;
}

.entry-heading { min-height: 43px; padding: 0 20px; color: #8a96a7; background: #fbfcfe; border-bottom: 1px solid var(--share-line); font-size: 12px; }
.column-label { grid-column: 2 / 4; display: flex; justify-content: space-between; }
.entry-row { min-height: 62px; padding: 0 20px; border-bottom: 1px solid #edf0f4; }
.entry-row:last-child { border-bottom: 0; }
.entry-row:hover { background: #f8faff; }
.entry-main { min-width: 0; display: flex; align-items: center; gap: 12px; }
.entry-open { width: 100%; border: 0; padding: 0; color: inherit; background: transparent; text-align: left; cursor: pointer; }
.file-open { color: inherit; text-decoration: none; }
.entry-icon { display: grid; place-items: center; flex: 0 0 auto; width: 30px; height: 30px; border-radius: 8px; font-size: 17px; }
.folder-icon { color: #bd8a2b; background: #fff5d9; }
.file-icon { color: #6683a8; background: #eef3fa; }
.entry-name { overflow: hidden; color: #253750; font-size: 14px; text-overflow: ellipsis; white-space: nowrap; }
.entry-open:hover .entry-name, .file-open:hover .entry-name { color: var(--share-blue); }
.entry-date, .entry-size { color: var(--share-muted); font-size: 12px; }
.download-link { display: inline-flex; justify-self: end; align-items: center; gap: 6px; border: 1px solid #d8e4f7; border-radius: 6px; padding: 6px 9px; color: var(--share-blue); background: #f7faff; font-size: 12px; text-decoration: none; }
.download-link:hover { border-color: #a9c5f2; background: var(--share-blue-soft); }
.open-hint { justify-self: end; transform: rotate(180deg); }
.list-state { padding: 34px 20px; color: var(--share-muted); text-align: center; font-size: 14px; }
.empty-state { color: #8794a6; }
.load-more { width: 100%; display: flex; align-items: center; justify-content: center; gap: 8px; border: 0; border-top: 1px solid var(--share-line); padding: 13px; color: var(--share-blue); background: #fbfcfe; cursor: pointer; }
.load-more:disabled { color: #8a96a7; cursor: wait; }
.share-footer { margin-top: 18px; color: #9aa5b4; text-align: center; font-size: 12px; }

.status-panel { margin: 15vh auto 0; max-width: 460px; text-align: center; }
.status-mark { width: 46px; height: 46px; margin: 0 auto 18px; display: grid; place-items: center; border-radius: 50%; color: #a66233; background: #fff0e5; font-size: 22px; font-weight: 700; }
.status-panel h1 { margin-bottom: 9px; font-size: 21px; font-weight: 650; }
.status-panel p { color: var(--share-muted); font-size: 14px; }

@media (max-width: 620px) {
  .share-header { height: 58px; padding: 0 18px; }
  .share-content { width: calc(100% - 24px); margin: 30px auto 28px; }
  .folder-heading { gap: 14px; margin-bottom: 24px; }
  .folder-emblem { width: 50px; height: 50px; border-radius: 14px 14px 14px 4px; font-size: 23px; }
  .folder-description { font-size: 12px; }
  .entry-heading, .entry-row { grid-template-columns: minmax(0, 1fr) 68px 68px; column-gap: 8px; padding-left: 12px; padding-right: 12px; }
  .entry-heading { font-size: 11px; }
  .column-label { grid-column: 2 / 4; }
  .entry-row { position: relative; min-height: 66px; padding-right: 44px; }
  .entry-main { gap: 8px; }
  .entry-icon { width: 27px; height: 27px; font-size: 15px; }
  .entry-name { font-size: 13px; }
  .entry-date, .entry-size { font-size: 11px; }
  .download-link, .open-hint { position: absolute; right: 8px; }
  .download-link { width: 28px; height: 28px; justify-content: center; padding: 0; }
  .download-link span { position: absolute; width: 1px; height: 1px; overflow: hidden; clip: rect(0, 0, 0, 0); }
}
</style>
