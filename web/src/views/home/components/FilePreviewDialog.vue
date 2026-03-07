<script setup lang="ts">
import { computed, ref, watch, onMounted, onBeforeUnmount, nextTick, shallowRef, markRaw } from 'vue'
import { renderAsync } from 'docx-preview'
import { GlobalWorkerOptions, getDocument } from 'pdfjs-dist/legacy/build/pdf.min.mjs'

type PreviewMode = 'text' | 'pdf' | 'word' | 'image'

let pdfWorkerReady = false

async function ensurePdfWorker() {
  if (pdfWorkerReady) return
  const { default: workerSrc } = await import('pdfjs-dist/legacy/build/pdf.worker.min.mjs?url')
  GlobalWorkerOptions.workerSrc = workerSrc
  pdfWorkerReady = true
}

const props = defineProps<{
  modelValue: boolean
  title: string
  mode: PreviewMode
  content: string
  blob: Blob | null
  fileName: string
  loading: boolean
  saving: boolean
  dirty: boolean
  readOnly: boolean
  imagePosition: number
  imageTotal: number
  canPrevImage: boolean
  canNextImage: boolean
}>()

const emit = defineEmits<{
  (event: 'update:modelValue', value: boolean): void
  (event: 'update:content', value: string): void
  (event: 'request-close', done: () => void): void
  (event: 'save'): void
  (event: 'download'): void
  (event: 'prev-image'): void
  (event: 'next-image'): void
}>()

const dialogModel = computed({
  get: () => props.modelValue,
  set: value => emit('update:modelValue', value)
})

const contentModel = computed({
  get: () => props.content,
  set: value => emit('update:content', value)
})

const canSave = computed(() => props.mode === 'text' && !props.readOnly)
const canDownload = computed(() => props.mode !== 'text')
const isDocx = computed(() => props.fileName.toLowerCase().endsWith('.docx'))
const imageUrl = ref('')
const imageScale = ref(1)
const imageOffsetX = ref(0)
const imageOffsetY = ref(0)
const imageDragging = ref(false)
const showImageNavigator = computed(() => props.mode === 'image' && props.imageTotal > 1)
const showImageMeta = computed(() => props.mode === 'image' && !!imageUrl.value)
const imageScalePercent = computed(() => `${Math.round(imageScale.value * 100)}%`)
const canResetImageView = computed(() => (
  props.mode === 'image'
  && !!imageUrl.value
  && (
    Math.abs(imageScale.value - 1) > 0.001
    || Math.abs(imageOffsetX.value) > 0.5
    || Math.abs(imageOffsetY.value) > 0.5
  )
))
const imageCursor = computed(() => {
  if (imageDragging.value) return 'grabbing'
  if (imageScale.value > 1.05) return 'grab'
  return 'zoom-in'
})

const pdfCanvas = ref<HTMLCanvasElement | null>(null)
const previewFrame = ref<HTMLDivElement | null>(null)
const imageFrame = ref<HTMLDivElement | null>(null)
const imageElement = ref<HTMLImageElement | null>(null)
const wordContainer = ref<HTMLDivElement | null>(null)
const wordStyleContainer = ref<HTMLDivElement | null>(null)
const pdfDoc = shallowRef<any>(null)
const pdfPage = ref(1)
const pdfPageCount = ref(0)
const pdfScale = ref(1)
const lastScrollTop = ref(0)
const touchStartX = ref(0)
const touchStartY = ref(0)
const touchTracking = ref(false)
const imageTouchMode = ref<'idle' | 'swipe' | 'pan' | 'pinch'>('idle')
const imagePinchStartDistance = ref(0)
const imagePinchStartScale = ref(1)
const imageTouchPanStartX = ref(0)
const imageTouchPanStartY = ref(0)
const imageTouchPanOriginX = ref(0)
const imageTouchPanOriginY = ref(0)
const imageDragStartX = ref(0)
const imageDragStartY = ref(0)
const imageDragOriginX = ref(0)
const imageDragOriginY = ref(0)
let renderTask: any = null
let scrollLock = false
let pendingScroll: 'next' | 'prev' | null = null

function resetPdf() {
  if (renderTask?.cancel) {
    renderTask.cancel()
  }
  renderTask = null
  if (pdfDoc.value?.destroy) {
    pdfDoc.value.destroy()
  }
  pdfDoc.value = null
  pdfPage.value = 1
  pdfPageCount.value = 0
  pdfScale.value = 1
  lastScrollTop.value = 0
  scrollLock = false
  pendingScroll = null
}

function revokeImageUrl() {
  if (!imageUrl.value) return
  URL.revokeObjectURL(imageUrl.value)
  imageUrl.value = ''
}

function clampImageScale(scale: number): number {
  return Math.min(4, Math.max(0.5, scale))
}

function resetImageOffset() {
  stopImageDrag()
  imageOffsetX.value = 0
  imageOffsetY.value = 0
}

function resetImageScale() {
  imageScale.value = 1
  resetImageOffset()
  resetImageTouch()
}

function getTouchDistance(touches: TouchList): number {
  if (touches.length < 2) return 0
  const dx = touches[0].clientX - touches[1].clientX
  const dy = touches[0].clientY - touches[1].clientY
  return Math.hypot(dx, dy)
}

function beginPinchZoom(touches: TouchList) {
  const distance = getTouchDistance(touches)
  if (distance <= 0) return
  imageTouchMode.value = 'pinch'
  imagePinchStartDistance.value = distance
  imagePinchStartScale.value = imageScale.value
  touchTracking.value = false
}

function beginTouchPan(touch: Touch) {
  imageTouchMode.value = 'pan'
  imageTouchPanStartX.value = touch.clientX
  imageTouchPanStartY.value = touch.clientY
  imageTouchPanOriginX.value = imageOffsetX.value
  imageTouchPanOriginY.value = imageOffsetY.value
  touchTracking.value = false
}

function stopImageDrag() {
  if (!imageDragging.value) return
  imageDragging.value = false
  if (typeof document !== 'undefined') {
    document.body.style.removeProperty('cursor')
    document.body.style.removeProperty('user-select')
  }
}

function getImagePanBounds() {
  const frame = imageFrame.value
  const image = imageElement.value
  if (!frame || !image || typeof window === 'undefined') {
    return { maxX: 0, maxY: 0 }
  }
  const style = window.getComputedStyle(frame)
  const paddingX = parseFloat(style.paddingLeft) + parseFloat(style.paddingRight)
  const paddingY = parseFloat(style.paddingTop) + parseFloat(style.paddingBottom)
  const availableWidth = Math.max(0, frame.clientWidth - paddingX)
  const availableHeight = Math.max(0, frame.clientHeight - paddingY)
  const baseWidth = image.offsetWidth
  const baseHeight = image.offsetHeight
  const scaledWidth = baseWidth * imageScale.value
  const scaledHeight = baseHeight * imageScale.value
  return {
    maxX: Math.max(0, (scaledWidth - availableWidth) / 2),
    maxY: Math.max(0, (scaledHeight - availableHeight) / 2)
  }
}

function clampImageOffsets() {
  const { maxX, maxY } = getImagePanBounds()
  imageOffsetX.value = maxX > 0 ? Math.min(maxX, Math.max(-maxX, imageOffsetX.value)) : 0
  imageOffsetY.value = maxY > 0 ? Math.min(maxY, Math.max(-maxY, imageOffsetY.value)) : 0
}

async function renderPdfPage() {
  if (!pdfDoc.value || !pdfCanvas.value) return
  const page = await pdfDoc.value.getPage(pdfPage.value)
  const viewport = page.getViewport({ scale: pdfScale.value })
  const ratio = window.devicePixelRatio || 1
  const canvas = pdfCanvas.value
  const context = canvas.getContext('2d')
  if (!context) return
  canvas.width = Math.floor(viewport.width * ratio)
  canvas.height = Math.floor(viewport.height * ratio)
  canvas.style.width = `${viewport.width}px`
  canvas.style.height = `${viewport.height}px`
  const transform = ratio !== 1 ? [ratio, 0, 0, ratio, 0, 0] : null
  if (renderTask?.cancel) {
    renderTask.cancel()
  }
  renderTask = page.render({ canvasContext: context, viewport, transform })
  await renderTask.promise
  if (pendingScroll && previewFrame.value) {
    if (pendingScroll === 'next') {
      previewFrame.value.scrollTop = 0
    } else {
      previewFrame.value.scrollTop = previewFrame.value.scrollHeight
    }
  }
  pendingScroll = null
  scrollLock = false
  if (previewFrame.value) {
    lastScrollTop.value = previewFrame.value.scrollTop
  }
}

async function loadPdf(blob: Blob) {
  resetPdf()
  await ensurePdfWorker()
  const data = await blob.arrayBuffer()
  pdfDoc.value = markRaw(await getDocument({ data }).promise)
  pdfPageCount.value = pdfDoc.value.numPages || 1
  pdfPage.value = 1
  pdfScale.value = 1
  await nextTick()
  await renderPdfPage()
}

async function waitForContainerReady(el: HTMLElement, tries = 12) {
  for (let i = 0; i < tries; i += 1) {
    const rect = el.getBoundingClientRect()
    if (rect.width > 0 && rect.height > 0) return
    await new Promise(requestAnimationFrame)
  }
}

async function renderWord(blob: Blob) {
  if (!wordContainer.value) return
  wordContainer.value.innerHTML = ''
  await nextTick()
  await waitForContainerReady(wordContainer.value)
  const data = await blob.arrayBuffer()
  const styleTarget = wordStyleContainer.value || wordContainer.value
  await renderAsync(data, wordContainer.value, styleTarget, {
    inWrapper: true,
    ignoreWidth: false,
    ignoreHeight: false
  })
}

watch(
  () => [props.mode, props.blob, props.modelValue] as const,
  async ([mode, blob, visible]) => {
    if (!visible) {
      resetPdf()
      revokeImageUrl()
      resetImageScale()
      if (wordContainer.value) wordContainer.value.innerHTML = ''
      if (wordStyleContainer.value) wordStyleContainer.value.innerHTML = ''
      return
    }
    if (mode === 'pdf') {
      if (!blob) {
        resetPdf()
        return
      }
      try {
        await loadPdf(blob)
      } catch (error) {
        console.error('PDF 预览失败:', error)
        resetPdf()
      }
      return
    }
    if (mode === 'image') {
      revokeImageUrl()
      resetImageScale()
      if (!blob) {
        return
      }
      imageUrl.value = URL.createObjectURL(blob)
      return
    }
    if (mode === 'word' && blob && isDocx.value) {
      try {
        await renderWord(blob)
      } catch (error) {
        console.error('Word 预览失败:', error)
        if (wordContainer.value) wordContainer.value.innerHTML = ''
        if (wordStyleContainer.value) wordStyleContainer.value.innerHTML = ''
      }
      return
    }
    resetPdf()
    revokeImageUrl()
    resetImageScale()
    if (wordContainer.value) wordContainer.value.innerHTML = ''
    if (wordStyleContainer.value) wordStyleContainer.value.innerHTML = ''
  }
)

watch([pdfScale, pdfPage], async () => {
  if (props.mode === 'pdf' && pdfDoc.value) {
    await renderPdfPage()
  }
})

watch(imageScale, async scale => {
  if (scale <= 1.001) {
    resetImageOffset()
    return
  }
  await nextTick()
  clampImageOffsets()
})

function isEditableTarget(target: EventTarget | null): boolean {
  if (!(target instanceof HTMLElement)) return false
  const tag = target.tagName
  return target.isContentEditable || tag === 'INPUT' || tag === 'TEXTAREA' || tag === 'SELECT'
}

function handleWindowKeydown(event: KeyboardEvent) {
  if (!props.modelValue || props.mode !== 'image' || !showImageNavigator.value || props.loading) return
  if (isEditableTarget(event.target)) return
  if (event.key === 'ArrowLeft' && props.canPrevImage) {
    event.preventDefault()
    emit('prev-image')
    return
  }
  if (event.key === 'ArrowRight' && props.canNextImage) {
    event.preventDefault()
    emit('next-image')
  }
}

function resetImageTouch() {
  imageTouchMode.value = 'idle'
  touchTracking.value = false
  touchStartX.value = 0
  touchStartY.value = 0
  imagePinchStartDistance.value = 0
  imagePinchStartScale.value = imageScale.value
  imageTouchPanStartX.value = 0
  imageTouchPanStartY.value = 0
  imageTouchPanOriginX.value = 0
  imageTouchPanOriginY.value = 0
}

function handleImageTouchStart(event: TouchEvent) {
  if (props.mode !== 'image' || !imageUrl.value || props.loading) return
  if (event.touches.length >= 2) {
    beginPinchZoom(event.touches)
    return
  }
  if (event.touches.length !== 1) {
    resetImageTouch()
    return
  }
  const touch = event.touches[0]
  if (imageScale.value > 1.05) {
    beginTouchPan(touch)
    return
  }
  if (!showImageNavigator.value) {
    resetImageTouch()
    return
  }
  imageTouchMode.value = 'swipe'
  touchTracking.value = true
  touchStartX.value = touch.clientX
  touchStartY.value = touch.clientY
}

function handleImageTouchMove(event: TouchEvent) {
  if (props.mode !== 'image' || !imageUrl.value || props.loading) return
  if (event.touches.length >= 2) {
    if (imageTouchMode.value !== 'pinch') {
      beginPinchZoom(event.touches)
    }
    if (imagePinchStartDistance.value <= 0) return
    if (event.cancelable) event.preventDefault()
    const distance = getTouchDistance(event.touches)
    if (distance <= 0) return
    imageScale.value = clampImageScale(Number((imagePinchStartScale.value * (distance / imagePinchStartDistance.value)).toFixed(3)))
    clampImageOffsets()
    return
  }
  if (imageTouchMode.value === 'pan' && event.touches.length === 1) {
    if (event.cancelable) event.preventDefault()
    const touch = event.touches[0]
    const { maxX, maxY } = getImagePanBounds()
    const nextX = imageTouchPanOriginX.value + (touch.clientX - imageTouchPanStartX.value)
    const nextY = imageTouchPanOriginY.value + (touch.clientY - imageTouchPanStartY.value)
    imageOffsetX.value = maxX > 0 ? Math.min(maxX, Math.max(-maxX, nextX)) : 0
    imageOffsetY.value = maxY > 0 ? Math.min(maxY, Math.max(-maxY, nextY)) : 0
  }
}

function handleImageTouchEnd(event: TouchEvent) {
  if (props.mode !== 'image' || !imageUrl.value || props.loading) {
    resetImageTouch()
    return
  }
  if (imageTouchMode.value === 'pinch' || imageTouchMode.value === 'pan') {
    if (imageScale.value > 1.05 && event.touches.length === 1) {
      beginTouchPan(event.touches[0])
      return
    }
    resetImageTouch()
    return
  }
  if (!touchTracking.value || imageTouchMode.value !== 'swipe' || !showImageNavigator.value) {
    resetImageTouch()
    return
  }
  const touch = event.changedTouches[0]
  if (!touch) {
    resetImageTouch()
    return
  }
  const deltaX = touch.clientX - touchStartX.value
  const deltaY = touch.clientY - touchStartY.value
  resetImageTouch()
  if (Math.abs(deltaX) < 48) return
  if (Math.abs(deltaX) <= Math.abs(deltaY) * 1.2) return
  if (deltaX > 0 && props.canPrevImage) {
    emit('prev-image')
    return
  }
  if (deltaX < 0 && props.canNextImage) {
    emit('next-image')
  }
}

function handleImageWheel(event: WheelEvent) {
  if (props.mode !== 'image' || !imageUrl.value || props.loading) return
  const delta = Math.sign(event.deltaY)
  if (!delta) return
  event.preventDefault()
  const stepCount = Math.max(1, Math.min(4, Math.round(Math.abs(event.deltaY) / 120)))
  const step = delta < 0 ? 0.1 : -0.1
  const nextScale = imageScale.value + step * stepCount
  imageScale.value = clampImageScale(Number(nextScale.toFixed(2)))
}

function handleImageDoubleClick() {
  if (props.mode !== 'image' || !imageUrl.value || props.loading) return
  imageScale.value = imageScale.value > 1.05 ? 1 : 2
}

function handleImageMouseDown(event: MouseEvent) {
  if (props.mode !== 'image' || !imageUrl.value || props.loading) return
  if (imageScale.value <= 1.05 || event.button !== 0) return
  event.preventDefault()
  imageDragging.value = true
  imageDragStartX.value = event.clientX
  imageDragStartY.value = event.clientY
  imageDragOriginX.value = imageOffsetX.value
  imageDragOriginY.value = imageOffsetY.value
  if (typeof document !== 'undefined') {
    document.body.style.cursor = 'grabbing'
    document.body.style.userSelect = 'none'
  }
}

function handleWindowMouseMove(event: MouseEvent) {
  if (!imageDragging.value) return
  const { maxX, maxY } = getImagePanBounds()
  const nextX = imageDragOriginX.value + (event.clientX - imageDragStartX.value)
  const nextY = imageDragOriginY.value + (event.clientY - imageDragStartY.value)
  imageOffsetX.value = maxX > 0 ? Math.min(maxX, Math.max(-maxX, nextX)) : 0
  imageOffsetY.value = maxY > 0 ? Math.min(maxY, Math.max(-maxY, nextY)) : 0
}

function handleImageLoad() {
  clampImageOffsets()
}

function handleResetImageView() {
  if (props.mode !== 'image' || !imageUrl.value) return
  resetImageScale()
}

function handleWindowResize() {
  clampImageOffsets()
}

onMounted(() => {
  if (typeof window === 'undefined') return
  window.addEventListener('keydown', handleWindowKeydown)
  window.addEventListener('mousemove', handleWindowMouseMove)
  window.addEventListener('mouseup', stopImageDrag)
  window.addEventListener('resize', handleWindowResize)
})

onBeforeUnmount(() => {
  resetPdf()
  revokeImageUrl()
  resetImageScale()
  stopImageDrag()
  if (typeof window !== 'undefined') {
    window.removeEventListener('keydown', handleWindowKeydown)
    window.removeEventListener('mousemove', handleWindowMouseMove)
    window.removeEventListener('mouseup', stopImageDrag)
    window.removeEventListener('resize', handleWindowResize)
  }
})

function handleBeforeClose(done: () => void) {
  emit('request-close', done)
}

function handleCloseClick() {
  emit('request-close', () => emit('update:modelValue', false))
}

function handleDialogOpened() {
  if (props.mode === 'word' && props.blob && isDocx.value) {
    renderWord(props.blob)
  }
}

function queuePdfPageChange(direction: 'next' | 'prev') {
  if (scrollLock) return
  if (direction === 'next' && pdfPage.value >= pdfPageCount.value) return
  if (direction === 'prev' && pdfPage.value <= 1) return
  scrollLock = true
  pendingScroll = direction
  pdfPage.value += direction === 'next' ? 1 : -1
}

function handlePdfScroll() {
  if (props.mode !== 'pdf') return
  const frame = previewFrame.value
  if (!frame || scrollLock) return
  const { scrollTop, scrollHeight, clientHeight } = frame
  const maxScrollTop = Math.max(0, scrollHeight - clientHeight)
  const goingDown = scrollTop > lastScrollTop.value
  const goingUp = scrollTop < lastScrollTop.value
  lastScrollTop.value = scrollTop
  if (maxScrollTop <= 0) return
  if (goingDown && scrollTop >= maxScrollTop - 32) {
    queuePdfPageChange('next')
  } else if (goingUp && scrollTop <= 32) {
    queuePdfPageChange('prev')
  }
}

function handlePdfWheel(event: WheelEvent) {
  if (props.mode !== 'pdf') return
  const frame = previewFrame.value
  if (!frame || scrollLock) return
  const { scrollTop, scrollHeight, clientHeight } = frame
  const maxScrollTop = Math.max(0, scrollHeight - clientHeight)
  const atBottom = scrollTop >= maxScrollTop - 32
  const atTop = scrollTop <= 32
  const down = event.deltaY > 0
  const up = event.deltaY < 0
  if (down && (maxScrollTop <= 0 || atBottom)) {
    queuePdfPageChange('next')
    event.preventDefault()
  } else if (up && (maxScrollTop <= 0 || atTop)) {
    queuePdfPageChange('prev')
    event.preventDefault()
  }
}
</script>

<template>
  <el-dialog
    v-model="dialogModel"
    :title="title"
    width="760px"
    top="6vh"
    :close-on-click-modal="false"
    :before-close="handleBeforeClose"
    @opened="handleDialogOpened"
    class="file-preview-dialog"
  >
    <div class="preview-body" v-loading="loading">
      <template v-if="mode === 'text'">
        <el-input
          v-model="contentModel"
          type="textarea"
          :rows="18"
          resize="vertical"
          class="preview-textarea"
          :disabled="loading || readOnly"
        />
      </template>
      <template v-else-if="mode === 'pdf'">
        <div v-if="!blob" class="preview-placeholder">正在加载 PDF...</div>
        <template v-else>
          <div class="pdf-toolbar">
            <el-button size="small" :disabled="pdfPage <= 1" @click="pdfPage -= 1">上一页</el-button>
            <span class="pdf-meta">{{ pdfPage }} / {{ pdfPageCount || 1 }}</span>
            <el-button size="small" :disabled="pdfPage >= pdfPageCount" @click="pdfPage += 1">下一页</el-button>
            <div class="pdf-spacer"></div>
            <el-button size="small" @click="pdfScale = Math.max(0.2, pdfScale - 0.1)">缩小</el-button>
            <span class="pdf-meta">{{ Math.round(pdfScale * 100) }}%</span>
            <el-button size="small" @click="pdfScale = Math.min(2.2, pdfScale + 0.1)">放大</el-button>
          </div>
          <div
            ref="previewFrame"
            class="preview-frame"
            @scroll="handlePdfScroll"
            @wheel="handlePdfWheel"
          >
            <canvas ref="pdfCanvas"></canvas>
          </div>
        </template>
      </template>
      <template v-else-if="mode === 'image'">
        <div v-if="!imageUrl" class="preview-placeholder">正在加载图片...</div>
        <div
          v-else
          ref="imageFrame"
          class="preview-frame image-preview-frame"
          @wheel="handleImageWheel"
          @dblclick="handleImageDoubleClick"
          @mousedown="handleImageMouseDown"
          @touchstart.passive="handleImageTouchStart"
          @touchmove="handleImageTouchMove"
          @touchend="handleImageTouchEnd"
          @touchcancel="resetImageTouch"
        >
          <div
            class="preview-image-stage"
            :style="{ transform: `translate(${imageOffsetX}px, ${imageOffsetY}px)` }"
          >
            <img
              ref="imageElement"
              :src="imageUrl"
              :alt="fileName"
              class="preview-image"
              :style="{ transform: `scale(${imageScale})`, cursor: imageCursor }"
              draggable="false"
              @load="handleImageLoad"
            />
          </div>
        </div>
      </template>
      <template v-else>
        <div v-if="isDocx && blob" class="preview-docx" ref="wordContainer"></div>
        <div v-if="isDocx && blob" ref="wordStyleContainer" class="docx-style-container"></div>
        <div v-else class="preview-placeholder">
          暂不支持在线预览此类型文件
        </div>
      </template>
    </div>
    <template #footer>
      <div class="preview-footer">
        <div v-if="showImageMeta" class="preview-footer-meta">
          <span class="preview-footer-zoom">{{ imageScalePercent }}</span>
          <span v-if="showImageNavigator" class="preview-footer-separator">·</span>
          <span v-if="showImageNavigator" class="preview-footer-count">{{ imagePosition }} / {{ imageTotal }}</span>
        </div>
        <div class="preview-footer-actions">
          <el-button
            v-if="showImageNavigator"
            :disabled="loading || !canPrevImage"
            @click="$emit('prev-image')"
          >
            上一张
          </el-button>
          <el-button
            v-if="showImageNavigator"
            :disabled="loading || !canNextImage"
            @click="$emit('next-image')"
          >
            下一张
          </el-button>
          <el-button
            v-if="showImageMeta"
            :disabled="loading || !canResetImageView"
            @click="handleResetImageView"
          >
            重置视图
          </el-button>
          <el-button @click="handleCloseClick">关闭</el-button>
          <el-button v-if="canDownload" @click="$emit('download')">
            下载
          </el-button>
          <el-button
            v-if="canSave"
            type="primary"
            :loading="saving"
            :disabled="loading || !dirty"
            @click="$emit('save')"
          >
            保存
          </el-button>
        </div>
      </div>
    </template>
  </el-dialog>
</template>

<style scoped>
.preview-body {
  min-height: 380px;
}

.preview-footer {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  flex-wrap: wrap;
}

.preview-footer-meta {
  display: flex;
  align-items: center;
  min-height: 32px;
}

.preview-footer-count {
  font-size: 13px;
  color: #606266;
}

.preview-footer-zoom {
  font-size: 13px;
  color: #303133;
}

.preview-footer-separator {
  margin: 0 8px;
  color: #c0c4cc;
}

.preview-footer-actions {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: 8px;
  flex-wrap: wrap;
  margin-left: auto;
}

.preview-textarea :deep(.el-textarea__inner) {
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, "Liberation Mono", "Courier New", monospace;
  line-height: 1.5;
}

.preview-frame {
  width: 100%;
  height: 60vh;
  min-height: 360px;
  border-radius: 12px;
  overflow: auto;
  border: 1px solid #eef1f4;
  background: #f8fafc;
  display: block;
  padding: 12px;
  overscroll-behavior: contain;
}

.preview-frame canvas {
  display: block;
  margin: 0 auto;
}

.image-preview-frame {
  display: flex;
  align-items: center;
  justify-content: center;
  touch-action: none;
  background:
    linear-gradient(45deg, #eef2f7 25%, transparent 25%),
    linear-gradient(-45deg, #eef2f7 25%, transparent 25%),
    linear-gradient(45deg, transparent 75%, #eef2f7 75%),
    linear-gradient(-45deg, transparent 75%, #eef2f7 75%);
  background-size: 24px 24px;
  background-position: 0 0, 0 12px, 12px -12px, -12px 0;
}

.preview-image-stage {
  display: flex;
  align-items: center;
  justify-content: center;
  transform-origin: center center;
}

.preview-image {
  display: block;
  max-width: 100%;
  max-height: calc(60vh - 24px);
  object-fit: contain;
  border-radius: 8px;
  box-shadow: 0 12px 32px rgba(15, 23, 42, 0.08);
  transform-origin: center center;
  transition: transform 120ms ease-out;
  will-change: transform;
}

.pdf-toolbar {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 10px;
  flex-wrap: wrap;
}

.pdf-meta {
  font-size: 12px;
  color: #606266;
}

.pdf-spacer {
  flex: 1;
  min-width: 12px;
}

.preview-docx {
  min-height: 320px;
  padding: 12px;
  background: #fff;
  border: 1px solid #eef1f4;
  border-radius: 12px;
  overflow: auto;
}

.docx-style-container {
  display: none;
}

.preview-placeholder {
  height: 260px;
  display: flex;
  align-items: center;
  justify-content: center;
  color: #909399;
  font-size: 14px;
  background: #f8fafc;
  border: 1px dashed #dcdfe6;
  border-radius: 12px;
}
</style>
