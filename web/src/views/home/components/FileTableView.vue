<script setup lang="ts">
import { Delete, Download, Edit, MoreFilled, Share, User, View } from '@element-plus/icons-vue'
import type { FileItem } from '../types'

const props = defineProps<{
  rows: FileItem[]
  loading: boolean
  onRowClick: (...args: any[]) => void
  formatSize: (size: number) => string
  formatTime: (time: string | number) => string
  getPreviewMode: (item: FileItem) => 'text' | 'pdf' | 'word' | 'image' | null
  openFilePreview: (item: FileItem) => void
  openDetailDrawer: (mode: 'file' | 'recycle', item: FileItem) => void
  downloadFile: (item: FileItem) => void
  shareFile: (item: FileItem) => void
  openShareUserDialog: (item: FileItem) => void
  renameItem: (item: FileItem) => void
  deleteFile: (item: FileItem) => void
}>()

function canPreview(row: FileItem): boolean {
  return !row.isDir && !!props.getPreviewMode(row)
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
      props.shareFile(row)
      break
    case 'shareUser':
      props.openShareUserDialog(row)
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
</script>

<template>
  <el-table
    class="desktop-only"
    :data="rows"
    v-loading="loading"
    style="width: 100%"
    height="100%"
    @row-click="onRowClick"
  >
    <el-table-column label="名称" min-width="280">
      <template #default="{ row }">
        <div class="file-name">
          <span class="iconfont" :class="row.isDir ? 'icon-wenjianjia' : 'icon-wenjian1'"></span>
          <span class="name" :title="row.name">{{ row.name }}</span>
        </div>
      </template>
    </el-table-column>
    <el-table-column label="大小" width="120">
      <template #default="{ row }">
        <span class="size-cell">{{ row.isDir ? '-' : formatSize(row.size) }}</span>
      </template>
    </el-table-column>
    <el-table-column label="修改时间" width="180">
      <template #default="{ row }">
        <span class="time-cell">{{ formatTime(row.modified) }}</span>
      </template>
    </el-table-column>
    <el-table-column label="操作" width="280" fixed="right">
      <template #default="{ row }">
        <div class="actions" @click.stop>
          <el-tooltip v-if="row.isDir" content="详情" placement="top">
            <el-button type="primary" link :icon="View" @click="openDetailDrawer('file', row)" />
          </el-tooltip>
          <el-tooltip v-if="canPreview(row)" content="预览" placement="top">
            <el-button type="primary" link :icon="View" @click="openFilePreview(row)" />
          </el-tooltip>
          <el-tooltip v-if="!row.isDir" content="下载" placement="top">
            <el-button type="primary" link :icon="Download" @click="downloadFile(row)" />
          </el-tooltip>
          <el-tooltip v-if="!row.isDir" content="分享" placement="top">
            <el-button type="primary" link :icon="Share" @click="shareFile(row)" />
          </el-tooltip>
          <el-tooltip content="共享给用户" placement="top">
            <el-button type="primary" link :icon="User" @click="openShareUserDialog(row)" />
          </el-tooltip>
          <el-tooltip content="重命名" placement="top">
            <el-button type="primary" link :icon="Edit" @click="renameItem(row)" />
          </el-tooltip>
          <el-tooltip content="删除" placement="top">
            <el-button type="danger" link :icon="Delete" @click="deleteFile(row)" />
          </el-tooltip>
        </div>
      </template>
    </el-table-column>
  </el-table>

  <div class="mobile-only card-list" v-loading="loading">
    <el-empty v-if="!loading && !rows.length" description="暂无数据" />
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
          <el-button v-else size="small" :icon="Download" circle @click="downloadFile(row)" />
          <el-dropdown @command="handleDropdownCommand(row, $event)">
            <el-button size="small" :icon="MoreFilled" circle />
            <template #dropdown>
              <el-dropdown-menu>
                <el-dropdown-item command="detail">详情</el-dropdown-item>
                <el-dropdown-item v-if="canPreview(row)" command="preview">预览</el-dropdown-item>
                <el-dropdown-item v-if="!row.isDir" command="download">下载</el-dropdown-item>
                <el-dropdown-item v-if="!row.isDir" command="share">分享</el-dropdown-item>
                <el-dropdown-item command="shareUser">共享给用户</el-dropdown-item>
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
