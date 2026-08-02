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
      <div v-if="workspaceStore.isSimulationActive && workspaceStore.simulationSession" class="simulation-state" role="status">
        <span class="simulation-copy"><strong>用户模拟</strong><span>真实 {{ authStore.username }} → 实际 {{ simulatedUserLabel }} · {{ simulatedScopeLabel }} · 至 {{ simulationExpiry }}</span></span>
        <el-button class="simulation-exit" size="small" type="warning" plain :loading="exitingSimulation" @click="exitSimulation">退出模拟</el-button>
      </div>
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
            <el-dropdown-item v-if="canOpenSimulations && !workspaceStore.isSimulationActive" command="user-simulations">
              <el-icon><Switch /></el-icon>
              用户模拟
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
import { ElMessage, ElMessageBox } from 'element-plus'
import { Menu } from '@element-plus/icons-vue'
import type { ManagementWorkspace } from '@/api/managementContext'
import Logo from '@/components/Common/Logo.vue'
import { useAuthStore } from '@/stores/auth'
import { useAppStore } from '@/stores/app'
import { useWorkspaceStore, workspaceHome } from '@/stores/workspace'
import { useTenantStore } from '@/stores/tenant'
import { revokeUserSimulation } from '@/api/userSimulation'

const router = useRouter()
const { t, locale } = useI18n()
const authStore = useAuthStore()
const appStore = useAppStore()
const workspaceStore = useWorkspaceStore()
const tenantStore = useTenantStore()
const switching = ref(false)
const exitingSimulation = ref(false)
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
const canOpenSimulations = computed(() => workspaceStore.platformContext?.permissions.includes('platform.user_simulations.read') || false)
const simulatedUserLabel = computed(() => workspaceStore.simulationSession?.effective_user_name || `User ${workspaceStore.simulationSession?.effective_user_id || '-'}`)
const simulatedScopeLabel = computed(() => {
  const session = workspaceStore.simulationSession
  if (!session) return '-'
  const context = workspaceStore.contexts.find(item => item.scope_type === session.scope_type && item.scope_id === session.scope_id)
  return `${session.scope_type === 'tenant' ? '租户' : '资源'} ${context?.scope_name || context?.scope_key || session.scope_id}`
})
const simulationExpiry = computed(() => workspaceStore.simulationSession?.expires_at
  ? new Date(workspaceStore.simulationSession.expires_at).toLocaleString('zh-CN', { hour12: false })
  : '-')

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
  if (command === 'user-simulations') {
    await router.push('/platform-user-simulations')
    return
  }
  if (command === 'logout') {
    await authStore.logout()
    router.push('/login')
  }
}

const exitSimulation = async () => {
  const session = workspaceStore.simulationSession
  if (!session || exitingSimulation.value) return
  try {
    await ElMessageBox.confirm('退出后恢复真实身份与平台工作域。当前模拟会话将立即撤销。', '退出用户模拟', { type: 'warning', confirmButtonText: '确认退出', cancelButtonText: '继续模拟' })
  } catch {
    return
  }
  exitingSimulation.value = true
  try {
    await revokeUserSimulation(session.id, session.row_version, '管理员主动退出用户模拟')
    ElMessage.success('已退出用户模拟')
  } catch {
    ElMessage.warning('模拟会话可能已结束，本地模拟状态已清除。')
  } finally {
    workspaceStore.clearSimulation()
    await workspaceStore.loadContexts(true).catch(() => undefined)
    await tenantStore.loadContexts(true).catch(() => undefined)
    await router.push('/platform-overview')
    exitingSimulation.value = false
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
.simulation-state { display: flex; max-width: 560px; align-items: center; gap: 10px; margin-right: 6px; padding: 6px 8px 6px 10px; border: 1px solid #e6a23c; border-radius: 5px; background: #fdf6ec; color: #8a5a12; }
.simulation-copy { display: flex; min-width: 0; flex-direction: column; line-height: 16px; }
.simulation-copy strong { font-size: 12px; }
.simulation-copy span { overflow: hidden; max-width: 430px; font-size: 11px; text-overflow: ellipsis; white-space: nowrap; }
.simulation-exit { flex: 0 0 auto; }

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
