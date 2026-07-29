<template>
  <div class="header">
    <div class="header-left">
      <el-button class="mobile-menu" text :icon="Menu" aria-label="打开导航" @click="appStore.toggleMobileSidebar" />
      <Logo :collapsed="false" class="header-logo" />
    </div>

    <nav class="workspace-switcher" aria-label="核心业务工作域">
      <button
        v-for="item in workspaces"
        :key="item.value"
        type="button"
        :class="{ active: workspaceStore.currentWorkspace === item.value }"
        :aria-current="workspaceStore.currentWorkspace === item.value ? 'page' : undefined"
        :disabled="switching || (workspaceStore.isSimulationActive && workspaceStore.currentWorkspace !== item.value)"
        @click="switchWorkspace(item.value)"
      >
        {{ item.label }}
      </button>
    </nav>

    <div class="header-right">
      <button class="header-action client-download" type="button" @click="handleDownloadClient">
        <el-icon><Download /></el-icon>
        <span>客户端</span>
      </button>
      <el-dropdown @command="handleLanguageChange">
        <button class="header-action language-selector" type="button">
          <el-icon><Globe /></el-icon>
          <span>{{ currentLanguage }}</span>
        </button>
        <template #dropdown>
          <el-dropdown-menu>
            <el-dropdown-item command="zh-CN">中文</el-dropdown-item>
            <el-dropdown-item command="en-US">English</el-dropdown-item>
          </el-dropdown-menu>
        </template>
      </el-dropdown>
      <el-dropdown @command="handleCommand">
        <button class="header-action user-info" type="button" aria-label="用户菜单">
          <el-icon><User /></el-icon>
          <span class="username">{{ authStore.username }}</span>
          <el-tag v-if="authStore.role" class="role-tag" size="small" effect="plain">{{ roleLabel }}</el-tag>
        </button>
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
import { computed, ref } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { ElMessage } from 'element-plus'
import { Menu } from '@element-plus/icons-vue'
import type { ManagementWorkspace } from '@/api/managementContext'
import Logo from '@/components/Common/Logo.vue'
import { useAuthStore } from '@/stores/auth'
import { useAppStore } from '@/stores/app'
import { useWorkspaceStore, workspaceHome } from '@/stores/workspace'

const router = useRouter()
const { t, locale } = useI18n()
const authStore = useAuthStore()
const appStore = useAppStore()
const workspaceStore = useWorkspaceStore()
const switching = ref(false)
const workspaces: Array<{ value: ManagementWorkspace; label: string }> = [
  { value: 'tenant', label: '租户' },
  { value: 'provider', label: '资源' },
  { value: 'platform', label: '平台' }
]

const currentLanguage = computed(() => locale.value === 'zh-CN' ? '中文' : 'English')
const roleLabel = computed(() => ({
  admin: '平台管理员',
  platform_admin: '平台管理员',
  viewer: '平台观察员',
  platform_viewer: '平台观察员',
  tenant_admin: '租户管理员',
  none: '无平台角色'
}[authStore.role] || authStore.role))

const switchWorkspace = async (workspace: ManagementWorkspace) => {
  if (workspace === workspaceStore.currentWorkspace || switching.value) return
  switching.value = true
  try {
    await workspaceStore.loadContexts()
    if (!workspaceStore.activateWorkspace(workspace)) return
    await router.push(workspaceStore.hasContext(workspace)
      ? workspaceHome(workspace)
      : { name: 'WorkspaceUnavailable', query: { workspace } })
  } finally {
    switching.value = false
  }
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

const handleDownloadClient = () => window.open('/download', '_blank')
</script>

<style scoped>
.header { width: 100%; display: grid; grid-template-columns: minmax(180px, 1fr) auto minmax(180px, 1fr); align-items: center; gap: 16px; }
.header-left, .header-right { display: flex; align-items: center; min-width: 0; }
.header-left { gap: 18px; }
.header-right { justify-content: flex-end; gap: 4px; }
.header-logo { padding: 0; }
.mobile-menu { display: none; width: 36px; height: 36px; flex: 0 0 36px; font-size: 19px; }
.workspace-switcher { display: grid; grid-template-columns: repeat(3, 64px); height: 34px; padding: 3px; border: 1px solid var(--border-base); border-radius: 6px; background: var(--bg-subtle); }
.workspace-switcher button { border: 0; border-radius: 4px; background: transparent; color: var(--text-regular); cursor: pointer; font: inherit; font-size: 13px; font-weight: 600; letter-spacing: 0; }
.workspace-switcher button:hover:not(:disabled) { color: var(--primary-color); background: var(--primary-lighter); }
.workspace-switcher button.active { background: #fff; color: var(--primary-color); box-shadow: 0 0 0 1px var(--border-light); }
.workspace-switcher button:focus-visible { outline: 2px solid var(--primary-color); outline-offset: 1px; }
.workspace-switcher button:disabled { cursor: not-allowed; opacity: 0.48; }
.header-action { display: flex; align-items: center; gap: 5px; min-width: 36px; min-height: 36px; padding: 7px 10px; border: 0; border-radius: 4px; background: transparent; color: var(--text-primary); cursor: pointer; font: inherit; font-size: 14px; letter-spacing: 0; }
.header-action:hover { color: var(--primary-color); background: var(--primary-lighter); }
.header-action:focus-visible { outline: 2px solid var(--primary-color); outline-offset: 1px; }
.user-info { max-width: 260px; }
.username { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }

@media (max-width: 1040px) {
  .header { grid-template-columns: minmax(100px, 1fr) auto minmax(100px, 1fr); gap: 10px; }
  .client-download span, .language-selector span, .role-tag { display: none; }
}

@media (max-width: 768px) {
  .header { grid-template-columns: 72px minmax(168px, 1fr) 40px; gap: 6px; }
  .mobile-menu { display: inline-flex; }
  .header-logo :deep(.logo-text) { display: none; }
  .header-logo :deep(.logo-icon) { width: 30px; height: 30px; }
  .header-left { gap: 3px; }
  .workspace-switcher { width: 100%; grid-template-columns: repeat(3, minmax(50px, 1fr)); }
  .client-download, .language-selector, .username, .role-tag { display: none; }
  .header-action { width: 36px; padding: 7px; justify-content: center; }
}

@media (max-width: 390px) {
  .header { grid-template-columns: 42px minmax(162px, 1fr) 36px; }
  .header-logo { display: none; }
}
</style>
