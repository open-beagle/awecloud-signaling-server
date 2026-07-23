<template>
  <div class="header">
    <div class="header-left">
      <el-button class="mobile-menu" text :icon="Menu" aria-label="打开导航" @click="appStore.toggleMobileSidebar" />
      <Logo :collapsed="false" class="header-logo" />
    </div>
    <div class="header-right">
      <span class="client-download" @click="handleDownloadClient">
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
          <el-tag v-if="authStore.role" size="small" effect="plain">{{ roleLabel }}</el-tag>
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
import { computed } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { useAuthStore } from '@/stores/auth'
import { useAppStore } from '@/stores/app'
import { ElMessage } from 'element-plus'
import { Menu } from '@element-plus/icons-vue'
import Logo from '@/components/Common/Logo.vue'

const router = useRouter()
const { t, locale } = useI18n()
const authStore = useAuthStore()
const appStore = useAppStore()

const currentLanguage = computed(() => {
  return locale.value === 'zh-CN' ? '中文' : 'English'
})
const roleLabel = computed(() => ({ admin: 'Platform Admin', tenant_admin: 'Tenant Admin', viewer: 'Viewer' }[authStore.role] || authStore.role))

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
  window.open('/download', '_blank')
}

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
  gap: 18px;
}

.header-logo {
  padding: 0;
}

.header-right {
  display: flex;
  align-items: center;
  gap: 8px;
}

.mobile-menu { display: none; width: 36px; height: 36px; font-size: 19px; }

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
  transition: color 0.15s, background-color 0.15s;
}

.client-download:hover,
.language-selector:hover,
.user-info:hover {
  color: var(--primary-color);
  background-color: var(--primary-lighter);
}

@media (max-width: 768px) {
  .mobile-menu { display: inline-flex; }
  .header-logo :deep(.logo-text) { display: none; }
  .header-logo :deep(.logo-icon) { width: 34px; height: 34px; }
  .header-left { gap: 6px; }
  .client-download, .language-selector { display: none; }
  .user-info { padding: 7px; }
  .user-info .el-tag { display: none; }
}
</style>
