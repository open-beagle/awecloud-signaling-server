<template>
  <div class="download-container">
    <el-card class="download-card">
      <h1 class="download-title">下载客户端</h1>
      
      <div v-if="loading" class="loading-container">
        <el-icon class="is-loading"><Loading /></el-icon>
        <p>加载中...</p>
      </div>
      
      <div v-else-if="error" class="error-container">
        <el-alert type="error" :title="error" :closable="false" />
      </div>
      
      <div v-else class="download-content">
        <div v-if="version" class="version-info">
          <el-tag type="info" size="large">版本: {{ version }}</el-tag>
        </div>
        
        <div class="download-list">
          <div
            v-for="item in downloads"
            :key="`${item.os}-${item.arch}`"
            class="download-item"
            :class="{ 'recommended': isRecommended(item) }"
          >
            <div class="item-header">
              <el-icon class="platform-icon" :size="32">
                <Monitor v-if="item.os === 'windows'" />
                <Platform v-else-if="item.os === 'linux'" />
                <Iphone v-else />
              </el-icon>
              <div class="item-info">
                <div class="item-title">
                  <h3>{{ getPlatformName(item.os) }}</h3>
                  <el-tag v-if="isRecommended(item)" type="success" size="small">
                    推荐
                  </el-tag>
                </div>
                <p class="item-meta">{{ getArchName(item.arch) }}</p>
                <p class="item-filename">{{ item.filename }}</p>
              </div>
            </div>
            <el-button
              :type="isRecommended(item) ? 'primary' : 'default'"
              :icon="Download"
              @click="handleDownload(item)"
            >
              下载
            </el-button>
          </div>
        </div>
        
        <div class="download-footer">
          <el-button text @click="goToLogin">
            <el-icon><ArrowLeft /></el-icon>
            返回
          </el-button>
        </div>
      </div>
    </el-card>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import {
  Download,
  Loading,
  Monitor,
  Platform,
  Iphone,
  ArrowLeft
} from '@element-plus/icons-vue'
import { getDownloads } from '@/api/download'

const router = useRouter()

const loading = ref(true)
const error = ref('')
const version = ref('')
const downloads = ref<any[]>([])
const recommendedOS = ref('')

interface DownloadItem {
  os: string
  arch: string
  filename: string
  download_url: string
  version: string
}

const detectUserOS = (): string => {
  const userAgent = navigator.userAgent.toLowerCase()
  if (userAgent.includes('win')) return 'windows'
  if (userAgent.includes('mac')) return 'darwin'
  if (userAgent.includes('linux')) return 'linux'
  return 'windows'
}

const getPlatformName = (platform: string) => {
  const names: Record<string, string> = {
    windows: 'Windows',
    linux: 'Linux',
    darwin: 'macOS',
    macos: 'macOS'
  }
  return names[platform] || platform
}

const getArchName = (arch: string) => {
  const names: Record<string, string> = {
    amd64: '64位',
    arm64: 'ARM64'
  }
  return names[arch] || arch
}

const loadDownloads = async () => {
  loading.value = true
  error.value = ''
  
  try {
    // 检测用户操作系统
    recommendedOS.value = detectUserOS()
    
    // 获取所有平台的下载信息
    const res = await getDownloads()
    if (res.success && res.downloads) {
      version.value = res.version || ''
      
      // 转换为数组格式，并按推荐顺序排序
      const downloadMap = res.downloads
      const downloadList: DownloadItem[] = []
      
      // 优先添加推荐的系统
      if (downloadMap[recommendedOS.value]) {
        downloadList.push(downloadMap[recommendedOS.value])
      }
      
      // 添加其他系统
      const otherSystems = ['windows', 'darwin', 'linux'].filter(
        os => os !== recommendedOS.value && downloadMap[os]
      )
      otherSystems.forEach(os => {
        if (downloadMap[os]) {
          downloadList.push(downloadMap[os])
        }
      })
      
      downloads.value = downloadList
      
      if (downloads.value.length === 0) {
        error.value = '暂无可用下载'
      }
    } else {
      error.value = res.message || '加载失败'
    }
  } catch (err: any) {
    error.value = err.message || '加载失败'
  } finally {
    loading.value = false
  }
}

const handleDownload = (item: DownloadItem) => {
  // 直接下载文件
  window.location.href = item.download_url
  ElMessage.success('开始下载...')
}

const isRecommended = (item: DownloadItem): boolean => {
  return item.os === recommendedOS.value
}

const goToLogin = () => {
  router.push('/login')
}

onMounted(() => {
  loadDownloads()
})
</script>

<style scoped>
.download-container {
  width: 100%;
  height: 100%;
  display: flex;
  align-items: center;
  justify-content: center;
  background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
  padding: 20px;
}

.download-card {
  width: 100%;
  max-width: 600px;
  padding: 20px;
}

.download-title {
  text-align: center;
  margin-bottom: 30px;
  color: var(--text-primary);
  font-size: 24px;
}

.loading-container,
.error-container {
  text-align: center;
  padding: 40px 20px;
}

.loading-container .is-loading {
  font-size: 32px;
  margin-bottom: 16px;
}

.download-content {
  display: flex;
  flex-direction: column;
  gap: 24px;
}

.version-info {
  text-align: center;
}

.download-list {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.download-item {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 20px;
  border: 1px solid var(--el-border-color);
  border-radius: 8px;
  transition: all 0.3s;
}

.download-item:hover {
  border-color: var(--el-color-primary);
  box-shadow: 0 2px 12px rgba(0, 0, 0, 0.1);
}

.download-item.recommended {
  border-color: var(--el-color-success);
  background-color: var(--el-color-success-light-9);
}

.item-header {
  display: flex;
  align-items: center;
  gap: 16px;
  flex: 1;
}

.platform-icon {
  color: var(--el-color-primary);
}

.item-title {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 4px;
}

.item-info h3 {
  margin: 0;
  font-size: 18px;
  font-weight: 500;
}

.item-meta {
  margin: 0 0 4px 0;
  font-size: 14px;
  color: var(--el-text-color-secondary);
}

.item-filename {
  margin: 0;
  font-size: 12px;
  color: var(--el-text-color-placeholder);
  word-break: break-all;
}

.download-footer {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding-top: 16px;
  border-top: 1px solid var(--el-border-color);
}
</style>
