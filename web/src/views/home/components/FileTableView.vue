<script setup lang="ts">
import { ref, watch } from 'vue'
import { Download, MoreFilled, View } from '@element-plus/icons-vue'
import type { FileItem } from '../types'

const props = defineProps<{
  isMobile: boolean
  rows: FileItem[]
  loading: boolean
  selectionEnabled: boolean
  onRowClick: (...args: any[]) => void
  onSelectionChange: (rows: FileItem[]) => void
  canDragItem: (item: FileItem) => boolean
  isDraggingItem: (item: FileItem) => boolean
  isMoveTarget: (item: FileItem) => boolean
  handleItemDragStart: (event: DragEvent, item: FileItem) => void
  handleItemDragEnd: () => void
  handleDirectoryDragEnter: (event: DragEvent, item: FileItem) => void
  handleDirectoryDragOver: (event: DragEvent, item: FileItem) => void
  handleDirectoryDragLeave: (event: DragEvent, item: FileItem) => void
  handleDirectoryDrop: (event: DragEvent, item: FileItem) => void
  formatSize: (size: number) => string
  formatTime: (time: string | number) => string
  getPreviewMode: (item: FileItem) => 'text' | 'pdf' | 'word' | 'image' | 'audio' | 'video' | null
  openFilePreview: (item: FileItem) => void
  openDetailDrawer: (mode: 'file' | 'recycle', item: FileItem) => void
  downloadFile: (item: FileItem) => void
  canShareItem: (item: FileItem) => boolean
  shareFile: (item: FileItem) => void
  openShareUserDialog: (item: FileItem) => void
  openAccessKeyDialog: (item: FileItem) => void
  renameItem: (item: FileItem) => void
  deleteFile: (item: FileItem) => void
  selectionNonce: number
}>()

const tableRef = ref()

function canPreview(row: FileItem): boolean {
  return !row.isDir
}

function getPreviewActionLabel(row: FileItem): string {
  return props.getPreviewMode(row) ? '预览' : '选择格式打开'
}

function canShare(row: FileItem): boolean {
  return props.canShareItem(row)
}

function handleCommand(row: FileItem, command: string) {
  switch (command) {
    case 'detail':
      props.openDetailDrawer('file', row)
      break
    case 'preview':
      props.openFilePreview(row)
      break
    case 'download':
      props.downloadFile(row)
      break
    case 'share':
      if (!canShare(row)) return
      props.shareFile(row)
      break
    case 'shareUser':
      if (!canShare(row)) return
      props.openShareUserDialog(row)
      break
    case 'accessKey':
      props.openAccessKeyDialog(row)
      break
    case 'rename':
      props.renameItem(row)
      break
    case 'delete':
      props.deleteFile(row)
      break
    default:
      break
  }
}

function handleDropdownCommand(row: FileItem, command: string | number) {
  handleCommand(row, String(command))
}

function getRowClassName({ row }: { row: FileItem }) {
  return props.isMoveTarget(row) ? 'move-target-row' : ''
}

function handleSelectionChange(rows: FileItem[]) {
  props.onSelectionChange(rows)
}

function handleRowClick(row: FileItem, _column: unknown, event: MouseEvent) {
  const target = event?.target as HTMLElement | null
  if (target?.closest('.el-table-column--selection')) {
    return
  }
  props.onRowClick(row)
}

watch(() => props.selectionNonce, () => {
  tableRef.value?.clearSelection?.()
})

watch(() => props.selectionEnabled, enabled => {
  if (!enabled) {
    tableRef.value?.clearSelection?.()
  }
})
</script>

<template>
  <el-table
    v-if="!isMobile"
    ref="tableRef"
    :data="rows"
    v-loading="loading"
    style="width: 100%"
    height="100%"
    :row-class-name="getRowClassName"
    row-key="path"
    empty-text="当前目录暂无资产"
    @row-click="handleRowClick"
    @selection-change="handleSelectionChange"
  >
    <el-table-column v-if="selectionEnabled" type="selection" width="48" />
    <el-table-column prop="name" label="名称" min-width="280" sortable :sort-method="(a: FileItem, b: FileItem) => a.name.localeCompare(b.name, 'zh-CN')">
      <template #default="{ row }">
        <div
          class="file-name"
          :class="{
            'is-draggable': canDragItem(row),
            'is-dragging': isDraggingItem(row),
            'is-drop-target': isMoveTarget(row)
          }"
          :draggable="canDragItem(row)"
          @dragstart="handleItemDragStart($event, row)"
          @dragend="handleItemDragEnd"
          @dragenter="handleDirectoryDragEnter($event, row)"
          @dragover="handleDirectoryDragOver($event, row)"
          @dragleave="handleDirectoryDragLeave($event, row)"
          @drop="handleDirectoryDrop($event, row)"
        >
          <span class="iconfont" :class="row.isDir ? 'icon-wenjianjia' : 'icon-wenjian1'"></span>
          <span class="name" :title="row.name">{{ row.name }}</span>
          <el-tag v-if="row.encrypted" size="small" type="warning" effect="plain">加密</el-tag>
          <span v-if="isMoveTarget(row)" class="drop-tip">移动到此目录</span>
        </div>
      </template>
    </el-table-column>
    <el-table-column label="大小" width="120">
      <template #default="{ row }">
        <span class="size-cell">{{ row.isDir ? '-' : formatSize(row.size) }}</span>
      </template>
    </el-table-column>
    <el-table-column prop="modified" label="修改时间" width="180" sortable :sort-method="(a: FileItem, b: FileItem) => new Date(a.modified).getTime() - new Date(b.modified).getTime()">
      <template #default="{ row }">
        <span class="time-cell">{{ formatTime(row.modified) }}</span>
      </template>
    </el-table-column>
    <el-table-column label="操作" width="140" fixed="right" align="left" header-align="left">
      <template #default="{ row }">
        <div class="actions" @click.stop>
          <el-tooltip v-if="canPreview(row)" :content="getPreviewActionLabel(row)" placement="top">
            <el-button type="primary" link :icon="View" @click="openFilePreview(row)" />
          </el-tooltip>
          <el-tooltip v-if="!row.isDir" content="下载" placement="top">
            <el-button type="primary" link :icon="Download" @click="downloadFile(row)" />
          </el-tooltip>
          <el-dropdown @command="handleDropdownCommand(row, $event)">
            <el-button type="primary" link :icon="MoreFilled" />
            <template #dropdown>
              <el-dropdown-menu>
                <el-dropdown-item command="detail">详情</el-dropdown-item>
                <el-dropdown-item v-if="row.isDir" command="accessKey">授权密钥</el-dropdown-item>
                <el-dropdown-item v-if="canPreview(row)" command="preview">{{ getPreviewActionLabel(row) }}</el-dropdown-item>
                <el-dropdown-item v-if="!row.isDir" command="download">下载</el-dropdown-item>
                <el-dropdown-item v-if="!row.isDir && canShare(row)" command="share">创建链接</el-dropdown-item>
                <el-dropdown-item v-if="canShare(row)" command="shareUser">分享</el-dropdown-item>
                <el-dropdown-item v-if="row.encrypted" disabled>加密项暂不支持分享</el-dropdown-item>
                <el-dropdown-item command="rename">重命名</el-dropdown-item>
                <el-dropdown-item command="delete">删除</el-dropdown-item>
              </el-dropdown-menu>
            </template>
          </el-dropdown>
        </div>
      </template>
    </el-table-column>
  </el-table>

  <div v-else class="card-list" v-loading="loading">
    <el-empty v-if="!loading && !rows.length" description="当前目录暂无资产" />
    <div
      v-for="row in rows"
      :key="row.path"
      class="card-item"
      @click="onRowClick(row)"
    >
      <div class="card-header">
        <div class="file-name">
          <span class="iconfont" :class="row.isDir ? 'icon-wenjianjia' : 'icon-wenjian1'"></span>
          <span class="name" :title="row.name">{{ row.name }}</span>
          <el-tag v-if="row.encrypted" size="small" type="warning" effect="plain">加密</el-tag>
        </div>
      </div>
      <div class="card-footer" @click.stop>
        <div class="card-meta card-meta-compact">
          <span class="card-meta-value">{{ formatTime(row.modified) }}</span>
          <span v-if="!row.isDir" class="card-meta-sep">·</span>
          <span v-if="!row.isDir" class="card-meta-value">{{ formatSize(row.size) }}</span>
        </div>
        <div class="card-actions card-actions-inline">
          <el-button v-if="row.isDir" size="small" circle :icon="View" @click="openDetailDrawer('file', row)" />
          <template v-else>
            <el-button size="small" circle :icon="View" @click="openFilePreview(row)" />
            <el-button size="small" :icon="Download" circle @click="downloadFile(row)" />
          </template>
          <el-dropdown @command="handleDropdownCommand(row, $event)">
            <el-button size="small" :icon="MoreFilled" circle />
            <template #dropdown>
              <el-dropdown-menu>
                <el-dropdown-item command="detail">详情</el-dropdown-item>
                <el-dropdown-item v-if="row.isDir" command="accessKey">授权密钥</el-dropdown-item>
                <el-dropdown-item v-if="canPreview(row)" command="preview">{{ getPreviewActionLabel(row) }}</el-dropdown-item>
                <el-dropdown-item v-if="!row.isDir" command="download">下载</el-dropdown-item>
                <el-dropdown-item v-if="!row.isDir && canShare(row)" command="share">创建链接</el-dropdown-item>
                <el-dropdown-item v-if="canShare(row)" command="shareUser">分享</el-dropdown-item>
                <el-dropdown-item v-if="row.encrypted" disabled>加密项暂不支持分享</el-dropdown-item>
                <el-dropdown-item command="rename">重命名</el-dropdown-item>
                <el-dropdown-item command="delete">删除</el-dropdown-item>
              </el-dropdown-menu>
            </template>
          </el-dropdown>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped src="./homeShared.scss"></style>
<style scoped>
:deep(.move-target-row td) {
  background: rgba(64, 158, 255, 0.08) !important;
}

:deep(.move-target-row .el-table__cell) {
  box-shadow: inset 0 -1px 0 rgba(64, 158, 255, 0.18);
}

.file-name.is-draggable {
  cursor: grab;
}

.file-name.is-draggable:active {
  cursor: grabbing;
}

.file-name.is-dragging {
  opacity: 0.48;
  transform: scale(0.985);
}

.file-name.is-drop-target {
  background: rgba(64, 158, 255, 0.12);
  border-radius: 10px;
  box-shadow: inset 0 0 0 1px rgba(64, 158, 255, 0.45);
  padding: 6px 8px;
  margin: -6px -8px;
}

.drop-tip {
  margin-left: 8px;
  font-size: 12px;
  color: #409eff;
  flex-shrink: 0;
}
</style>
