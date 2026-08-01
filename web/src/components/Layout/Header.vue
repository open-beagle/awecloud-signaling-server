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
          <el-icon><Compass /></el-icon>
          <span>{{ currentLanguage }}</span>
        </button>
        <template #dropdown>
          <el-dropdown-menu>
            <el-dropdown-item command="zh-CN">中文</el-dropdown-item>
            <el-dropdown-item command="en-US">English</el-dropdown-item>
          </el-dropdown-menu>
        </template>
      </el-dropdown>
      <el-dropdown trigger="click" @command="handleCommand">
        <button class="header-action user-info" type="button" aria-label="用户菜单">
          <span class="user-avatar">{{ userInitial }}</span>
          <span class="identity-copy">
            <strong class="username">{{ authStore.username }}</strong>
            <span class="role-text">{{ roleLabel }}</span>
          </span>
          <span class="dropdown-caret">▼</span>
        </button>
        <template #dropdown>
          <el-dropdown-menu>
            <el-dropdown-item v-if="authStore.isPlatformAdmin" disabled>
              <el-icon><Switch /></el-icon>
              切换用户
              <el-tag class="pending-tag" size="small" type="info" effect="plain">待开放</el-tag>
            </el-dropdown-item>
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
const userInitial = computed(() => authStore.username.trim().slice(0, 1).toUpperCase() || 'U')
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
.header { width: 100%; height: 100%; display: grid; grid-template-columns: auto minmax(240px, 1fr) auto; align-items: stretch; }
.header-left, .header-right { display: flex; align-items: center; min-width: 0; }
.header-left { gap: 12px; padding-right: 18px; border-right: 1px solid var(--border-lighter); }
.header-right { justify-content: flex-end; gap: 4px; }
.header-logo { padding: 0; }
.mobile-menu { display: none; width: 36px; height: 36px; flex: 0 0 36px; font-size: 19px; }
.workspace-switcher { display: flex; min-width: 0; height: 100%; align-items: stretch; justify-content: flex-start; }
.workspace-switcher button { position: relative; min-width: 82px; padding: 0 20px; border: 0; background: transparent; color: var(--text-regular); cursor: pointer; font: inherit; font-size: 14px; font-weight: 600; letter-spacing: 0; }
.workspace-switcher button:hover:not(:disabled) { color: var(--primary-color); background: var(--primary-lighter); }
.workspace-switcher button.active { color: var(--primary-color); }
.workspace-switcher button.active::after { position: absolute; right: 18px; bottom: 0; left: 18px; height: 3px; background: var(--primary-color); content: ''; }
.workspace-switcher button:focus-visible { outline: 2px solid var(--primary-color); outline-offset: 1px; }
.workspace-switcher button:disabled { cursor: not-allowed; opacity: 0.48; }
.header-action { display: flex; align-items: center; gap: 5px; min-width: 36px; min-height: 36px; padding: 7px 10px; border: 0; border-radius: 4px; background: transparent; color: var(--text-primary); cursor: pointer; font: inherit; font-size: 14px; letter-spacing: 0; }
.header-action:hover { color: var(--primary-color); background: var(--primary-lighter); }
.header-action:focus-visible { outline: 2px solid var(--primary-color); outline-offset: 1px; }
.user-info { max-width: 230px; text-align: left; }
.user-avatar { display: inline-grid; width: 26px; height: 26px; flex: 0 0 26px; place-items: center; border-radius: 50%; background: #e3e8f0; color: #354052; font-size: 12px; font-weight: 700; }
.identity-copy { display: flex; min-width: 0; flex-direction: column; align-items: flex-start; line-height: 16px; }
.username { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.identity-copy strong { max-width: 130px; font-size: 13px; }
.role-text { color: var(--text-secondary); font-size: 11px; white-space: nowrap; }
.dropdown-caret { color: var(--text-secondary); font-size: 9px; }
.pending-tag { margin-left: 10px; }

@media (max-width: 1040px) {
  .header { grid-template-columns: auto minmax(220px, 1fr) auto; }
  .client-download span, .language-selector span, .role-text { display: none; }
  .identity-copy { line-height: 20px; }
}

@media (max-width: 768px) {
  .header { grid-template-columns: minmax(0, 1fr) 44px; grid-template-rows: 54px 50px; }
  .mobile-menu { display: inline-flex; }
  .header-logo :deep(.logo-icon) { width: 30px; height: 30px; }
  .header-left { gap: 3px; }
  .workspace-switcher { grid-column: 1 / -1; grid-row: 2; width: 100%; border-top: 1px solid var(--border-lighter); }
  .workspace-switcher button { min-width: 0; flex: 1; padding: 0 8px; }
  .header-right { grid-column: 2; grid-row: 1; }
  .client-download, .language-selector, .identity-copy, .dropdown-caret { display: none; }
  .header-action { width: 36px; padding: 7px; justify-content: center; }
  .user-info { width: 36px; }
}

@media (max-width: 390px) {
  .header-logo :deep(.logo-text) { font-size: 15px; }
}
</style>
