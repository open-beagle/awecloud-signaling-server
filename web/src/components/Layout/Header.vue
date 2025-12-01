<template>
  <div class="header">
    <div class="header-left">
      <Logo :collapsed="false" class="header-logo" />
    </div>
    <div class="header-right">
      <span v-if="clientDownloadUrl" class="client-download" @click="handleDownloadClient">
        <el-icon><Download /></el-icon>
        客户端
      </span>
      <el-dropdown @command="handleLanguageChange">
        <span class="language-selector">
          <el-icon><Globe /></el-icon>
          {{ currentLanguage }}
        </span>
        <template #dropdown>
          <el-dropdown-menu>
            <el-dropdown-item command="zh-CN">中文</el-dropdown-item>
            <el-dropdown-item command="en-US">English</el-dropdown-item>
          </el-dropdown-menu>
        </template>
      </el-dropdown>
      <el-dropdown @command="handleCommand">
        <span class="user-info">
          <el-icon><User /></el-icon>
          {{ authStore.username }}
        </span>
        <template #dropdown>
          <el-dropdown-menu>
            <el-dropdown-item command="logout">
              <el-icon><SwitchButton /></el-icon>
              {{ t('common.logout') }}
            </el-dropdown-item>
          </el-dropdown-menu>
        </template>
      </el-dropdown>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, ref, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { useAuthStore } from '@/stores/auth'
import { useAppStore } from '@/stores/app'
import { ElMessage } from 'element-plus'
import Logo from '@/components/Common/Logo.vue'
import { getPublicSystemConfig } from '@/api/system'

const router = useRouter()
const { t, locale } = useI18n()
const authStore = useAuthStore()
const appStore = useAppStore()
const clientDownloadUrl = ref('')

const currentLanguage = computed(() => {
  return locale.value === 'zh-CN' ? '中文' : 'English'
})

const toggleSidebar = () => {
  appStore.toggleSidebar()
}

const handleLanguageChange = (lang: string) => {
  locale.value = lang
  localStorage.setItem('locale', lang)
  ElMessage.success(t('common.success'))
}

const handleCommand = async (command: string) => {
  if (command === 'logout') {
    await authStore.logout()
    router.push('/login')
  }
}

const handleDownloadClient = () => {
  if (clientDownloadUrl.value) {
    window.open(clientDownloadUrl.value, '_blank')
  }
}

const loadSystemConfig = async () => {
  try {
    const res = await getPublicSystemConfig()
    if (res.success && res.data) {
      clientDownloadUrl.value = res.data.client_download_url || ''
    }
  } catch (error) {
    console.error('加载系统配置失败:', error)
  }
}

onMounted(() => {
  loadSystemConfig()
})
</script>

<style scoped>
.header {
  width: 100%;
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.header-left {
  display: flex;
  align-items: center;
}

.header-logo {
  padding: 0;
}

.header-right {
  display: flex;
  align-items: center;
  gap: 20px;
}

.client-download,
.language-selector,
.user-info {
  display: flex;
  align-items: center;
  gap: 5px;
  cursor: pointer;
  color: var(--text-primary);
  font-size: 14px;
  padding: 8px 12px;
  border-radius: 4px;
  transition: all 0.3s;
}

.client-download:hover,
.language-selector:hover,
.user-info:hover {
  color: var(--primary-color);
  background-color: #ecf5ff;
}
</style>
