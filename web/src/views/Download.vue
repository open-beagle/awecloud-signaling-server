<template>
  <div class="download-page">
    <header class="page-header">
      <div class="header-inner">
        <div class="brand">
          <img :src="logo" alt="" />
          <span>Beagle Signal</span>
        </div>
      </div>
    </header>

    <main class="page-main">
      <section class="intro">
        <div class="eyebrow">
          <el-icon><Monitor /></el-icon>
          <span>Beagle Signal Desktop</span>
        </div>
        <h1>下载 Desktop Launcher</h1>
        <p>面向研发、测试和运维人员的零信任资源访问客户端，使用熟悉的原生工具访问明确授权的业务资源。</p>
      </section>

      <section class="download-tool" aria-live="polite">
        <div v-if="loading" class="state-panel">
          <el-icon class="is-loading state-icon"><Loading /></el-icon>
          <h2>正在获取 Launcher</h2>
          <p>正在读取当前发布版本和可用平台。</p>
        </div>

        <div v-else-if="error" class="state-panel error-state">
          <el-icon class="state-icon"><Warning /></el-icon>
          <h2>暂时无法获取下载信息</h2>
          <p>{{ error }}</p>
          <el-button :icon="Refresh" @click="loadDownloads">重新加载</el-button>
        </div>

        <template v-else-if="selectedDownload">
          <div class="primary-download">
            <span class="platform-mark">
              <el-icon><component :is="platformIcon(selectedDownload.os)" /></el-icon>
            </span>

            <div class="primary-info">
              <div class="primary-title">
                <strong>{{ platformName(selectedDownload.os) }} Desktop Launcher</strong>
                <span class="recommended-tag">适合当前设备</span>
              </div>
              <div class="version-line">
                <b>Launcher {{ displayVersion }}</b>
                <span>· {{ archName(selectedDownload) }}</span>
              </div>
              <div class="filename">{{ selectedDownload.filename }}</div>

              <div v-if="showMacArchitecture" class="architecture" aria-label="macOS 架构">
                <button
                  v-for="item in macDownloads"
                  :key="downloadKey(item)"
                  type="button"
                  :class="{ active: downloadKey(item) === selectedKey }"
                  @click="selectedKey = downloadKey(item)"
                >
                  {{ item.arch === 'arm64' ? 'Apple 芯片' : 'Intel 芯片' }}
                </button>
              </div>
            </div>

            <div class="primary-action">
              <el-button type="primary" :icon="Download" @click="download(selectedDownload)">
                下载 Launcher
              </el-button>
              <span>{{ formatSize(selectedDownload.size) }} · Stable</span>
            </div>
          </div>

          <div class="resource-note">
            <el-icon><Grid /></el-icon>
            <span>服务器 · Kubernetes · 数据库 · 边缘 GPU 工作空间</span>
          </div>

          <div class="other-platforms">
            <h2>其他平台</h2>
            <div class="platform-list">
              <div v-for="item in otherDownloads" :key="downloadKey(item)" class="platform-row">
                <span class="small-mark">
                  <el-icon><component :is="platformIcon(item.os)" /></el-icon>
                </span>
                <div>
                  <div class="platform-name">{{ platformName(item.os) }}</div>
                  <div class="platform-meta">{{ archName(item) }} · Launcher {{ displayVersion }}</div>
                </div>
                <el-button :icon="Download" @click="download(item)">下载</el-button>
              </div>
            </div>
          </div>
        </template>
      </section>

      <section class="features" aria-label="产品特性">
        <div class="feature">
          <div class="feature-title"><el-icon><Lock /></el-icon><strong>零信任访问</strong></div>
          <p>以用户、设备、资源和会话为边界，仅访问明确授权的资源。</p>
        </div>
        <div class="feature">
          <div class="feature-title"><el-icon><Grid /></el-icon><strong>多类资源统一入口</strong></div>
          <p>统一访问服务器、Kubernetes、数据库及边缘 GPU 工作空间。</p>
        </div>
        <div class="feature">
          <div class="feature-title"><el-icon><Connection /></el-icon><strong>原生工具访问</strong></div>
          <p>继续使用 SSH、kubectl 和数据库客户端。</p>
        </div>
      </section>
    </main>
  </div>
</template>

<script setup lang="ts">
import { computed, markRaw, onMounted, ref } from 'vue'
import {
  Connection,
  Download,
  Grid,
  Iphone,
  Loading,
  Lock,
  Monitor,
  Platform,
  Refresh,
  Warning
} from '@element-plus/icons-vue'
import logo from '@/assets/logo.png'
import { getDesktopLaunchers } from '@/api/download'
import type { DesktopLauncherDownload } from '@/api/download'

const loading = ref(true)
const error = ref('')
const version = ref('')
const downloads = ref<DesktopLauncherDownload[]>([])
const selectedKey = ref('')

const platformOrder = ['windows-amd64', 'darwin-arm64', 'darwin-amd64', 'linux-amd64']

const displayVersion = computed(() => {
  const value = version.value.trim()
  return value.startsWith('v') ? value : `v${value}`
})

const selectedDownload = computed(() =>
  downloads.value.find(item => downloadKey(item) === selectedKey.value) || null
)

const macDownloads = computed(() =>
  downloads.value.filter(item => item.os === 'darwin').sort(sortDownloads)
)

const showMacArchitecture = computed(() =>
  selectedDownload.value?.os === 'darwin' && macDownloads.value.length > 1
)

const otherDownloads = computed(() => {
  if (!selectedDownload.value) return []
  return downloads.value
    .filter(item => downloadKey(item) !== selectedKey.value)
    .filter(item => selectedDownload.value?.os !== 'darwin' || item.os !== 'darwin')
    .sort(sortDownloads)
})

function downloadKey(item: DesktopLauncherDownload) {
  return `${item.os}-${item.arch}`
}

function sortDownloads(left: DesktopLauncherDownload, right: DesktopLauncherDownload) {
  const leftIndex = platformOrder.indexOf(downloadKey(left))
  const rightIndex = platformOrder.indexOf(downloadKey(right))
  return (leftIndex < 0 ? platformOrder.length : leftIndex) - (rightIndex < 0 ? platformOrder.length : rightIndex)
}

function detectPlatform() {
  const userAgent = navigator.userAgent.toLowerCase()
  if (userAgent.includes('win')) return 'windows-amd64'
  if (userAgent.includes('mac')) return 'darwin-arm64'
  if (userAgent.includes('linux')) return 'linux-amd64'
  return 'windows-amd64'
}

function chooseDownload(items: DesktopLauncherDownload[]) {
  const preferred = detectPlatform()
  if (items.some(item => downloadKey(item) === preferred)) return preferred
  return [...items].sort(sortDownloads)[0] ? downloadKey([...items].sort(sortDownloads)[0]) : ''
}

function platformName(os: string) {
  return ({ windows: 'Windows', darwin: 'macOS', linux: 'Linux' } as Record<string, string>)[os] || os
}

function archName(item: DesktopLauncherDownload) {
  if (item.os === 'darwin' && item.arch === 'arm64') return 'Apple Silicon'
  if (item.os === 'darwin') return '64 位 (Intel)'
  return item.arch === 'arm64' ? 'ARM64' : '64 位 (Intel/AMD)'
}

function platformIcon(os: string) {
  if (os === 'windows') return markRaw(Monitor)
  if (os === 'linux') return markRaw(Platform)
  return markRaw(Iphone)
}

function formatSize(bytes: number) {
  if (!Number.isFinite(bytes) || bytes <= 0) return '大小未知'
  if (bytes < 1024 * 1024) return `${Math.max(1, Math.round(bytes / 1024))} KB`
  return `${(bytes / 1024 / 1024).toFixed(1)} MB`
}

function download(item: DesktopLauncherDownload) {
  window.location.assign(item.download_url)
}

async function loadDownloads() {
  loading.value = true
  error.value = ''
  try {
    const response = await getDesktopLaunchers()
    const launchers = response.downloads.filter(item => item.filename && item.download_url)
    if (!response.version || launchers.length === 0) {
      throw new Error('当前版本尚未发布可用的 Desktop Launcher')
    }
    version.value = response.version
    downloads.value = launchers
    selectedKey.value = chooseDownload(launchers)
  } catch (reason) {
    downloads.value = []
    selectedKey.value = ''
    error.value = reason instanceof Error ? reason.message : '下载服务暂时不可用，请稍后重试。'
  } finally {
    loading.value = false
  }
}

onMounted(loadDownloads)
</script>

<style scoped>
.download-page {
  min-width: 1600px;
  min-height: 900px;
  height: 100%;
  overflow: auto;
  color: var(--text-primary);
  background: var(--bg-page);
}

.page-header {
  height: 64px;
  display: flex;
  align-items: center;
  background: #fff;
  border-bottom: 1px solid var(--border-light);
}

.header-inner,
.page-main {
  width: 1080px;
  margin: 0 auto;
}

.brand {
  display: flex;
  align-items: center;
  gap: 10px;
  font-weight: 700;
}

.brand img {
  width: 32px;
  height: 32px;
  object-fit: contain;
}

.page-main {
  padding: 68px 0 56px;
}

.intro {
  max-width: 760px;
  margin-bottom: 30px;
}

.eyebrow {
  display: flex;
  align-items: center;
  gap: 7px;
  margin-bottom: 9px;
  color: var(--success-color);
  font-size: 12px;
  font-weight: 700;
}

.intro h1 {
  margin: 0;
  font-size: 30px;
  line-height: 40px;
  font-weight: 700;
}

.intro p {
  margin: 10px 0 0;
  color: var(--text-regular);
  font-size: 15px;
}

.download-tool {
  overflow: hidden;
  background: #fff;
  border: 1px solid var(--border-base);
  border-radius: 8px;
  box-shadow: 0 16px 42px rgba(35, 49, 69, .1);
}

.primary-download {
  min-height: 184px;
  display: grid;
  grid-template-columns: 72px minmax(0, 1fr) 154px;
  gap: 20px;
  align-items: center;
  padding: 28px 30px;
}

.platform-mark {
  width: 64px;
  height: 64px;
  display: grid;
  place-items: center;
  color: var(--primary-color);
  background: var(--primary-lighter);
  border-radius: 8px;
  font-size: 30px;
}

.primary-title {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 5px;
}

.primary-title strong {
  font-size: 18px;
}

.recommended-tag {
  min-height: 23px;
  display: inline-flex;
  align-items: center;
  padding: 2px 7px;
  color: var(--success-color);
  background: #eaf5ef;
  border: 1px solid #bddfce;
  border-radius: 4px;
  font-size: 11px;
  font-weight: 600;
}

.version-line {
  display: flex;
  gap: 4px;
  color: var(--text-regular);
  font-size: 13px;
}

.version-line b {
  color: var(--text-primary);
}

.filename {
  max-width: 620px;
  overflow: hidden;
  margin-top: 7px;
  color: var(--text-secondary);
  font: 11px/1.4 "SFMono-Regular", Consolas, monospace;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.architecture {
  width: fit-content;
  display: flex;
  margin-top: 13px;
  padding: 3px;
  background: #eef1f5;
  border-radius: 6px;
}

.architecture button {
  height: 29px;
  padding: 0 10px;
  color: var(--text-regular);
  background: transparent;
  border: 0;
  border-radius: 4px;
  cursor: pointer;
}

.architecture button.active {
  color: var(--text-primary);
  background: #fff;
  box-shadow: 0 1px 4px rgba(30, 43, 60, .12);
  font-weight: 600;
}

.primary-action {
  text-align: center;
}

.primary-action .el-button {
  width: 100%;
  height: 40px;
}

.primary-action > span {
  display: block;
  margin-top: 8px;
  color: var(--text-secondary);
  font-size: 11px;
}

.resource-note {
  min-height: 58px;
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 11px 30px;
  color: var(--text-regular);
  background: #fafbfc;
  border-top: 1px solid var(--border-light);
  font-size: 12px;
}

.resource-note .el-icon {
  color: var(--primary-color);
}

.other-platforms {
  border-top: 1px solid var(--border-light);
}

.other-platforms h2 {
  margin: 0;
  padding: 20px 30px 9px;
  font-size: 13px;
}

.platform-list {
  padding: 0 30px 20px;
}

.platform-row {
  min-height: 76px;
  display: grid;
  grid-template-columns: 38px minmax(0, 1fr) auto;
  gap: 14px;
  align-items: center;
  border-bottom: 1px solid var(--border-lighter);
}

.platform-row:last-child {
  border-bottom: 0;
}

.small-mark {
  width: 36px;
  height: 36px;
  display: grid;
  place-items: center;
  color: var(--primary-color);
  background: var(--primary-lighter);
  border-radius: 6px;
}

.platform-name {
  font-weight: 600;
}

.platform-meta {
  margin-top: 2px;
  color: var(--text-secondary);
  font-size: 11px;
}

.features {
  display: grid;
  grid-template-columns: repeat(3, 1fr);
  margin-top: 28px;
  border-top: 1px solid var(--border-light);
  border-bottom: 1px solid var(--border-light);
}

.feature {
  min-height: 102px;
  padding: 20px 22px;
  border-right: 1px solid var(--border-light);
}

.feature:first-child {
  padding-left: 0;
}

.feature:last-child {
  padding-right: 0;
  border-right: 0;
}

.feature-title {
  display: flex;
  align-items: center;
  gap: 8px;
}

.feature-title .el-icon {
  color: var(--primary-color);
}

.feature p {
  margin: 5px 0 0 26px;
  color: var(--text-secondary);
  font-size: 11px;
}

.state-panel {
  min-height: 420px;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  padding: 42px 24px;
  text-align: center;
}

.state-panel .state-icon {
  margin-bottom: 14px;
  color: var(--primary-color);
  font-size: 38px;
}

.state-panel h2 {
  margin: 0;
  font-size: 17px;
}

.state-panel p {
  max-width: 460px;
  margin: 7px auto 18px;
  color: var(--text-secondary);
  font-size: 12px;
}

.error-state .state-icon {
  color: var(--danger-color);
}
</style>
