<template>
  <div class="sidebar">
    <div v-if="workspaceStore.currentWorkspace !== 'platform'" class="scope-area" :class="{ collapsed: appStore.sidebarCollapsed }">
      <template v-if="workspaceStore.currentContext">
        <el-icon><OfficeBuilding v-if="workspaceStore.currentWorkspace === 'tenant'" /><SetUp v-else /></el-icon>
        <el-select
          v-if="!appStore.sidebarCollapsed"
          :model-value="workspaceStore.selectedContextId(workspaceStore.currentWorkspace)"
          class="scope-select"
          size="small"
          :disabled="workspaceStore.isSimulationActive"
          :aria-label="workspaceStore.currentWorkspace === 'tenant' ? '当前租户' : '当前资源方'"
          @change="selectScope"
        >
          <el-option
            v-for="context in selectableContexts"
            :key="context.scope_id"
            :label="context.scope_name || context.scope_key || context.scope_id"
            :value="context.scope_id"
          />
        </el-select>
      </template>
      <div v-else-if="!workspaceStore.loading && !appStore.sidebarCollapsed" class="scope-empty">
        <strong>无{{ workspaceLabel(workspaceStore.currentWorkspace) }}权限</strong>
        <span>当前身份没有可进入的业务空间。</span>
      </div>
    </div>

    <el-menu :default-active="activeMenu" :collapse="appStore.sidebarCollapsed" :collapse-transition="false" router unique-opened>
      <template v-if="workspaceStore.currentWorkspace === 'tenant' && workspaceStore.currentContext">
        <el-menu-item v-if="workspaceStore.can('tenant.overview.read')" index="/tenant-overview"><el-icon><DataAnalysis /></el-icon><template #title>概览</template></el-menu-item>
        <div v-if="!appStore.sidebarCollapsed && hasTenantOrganizationMenu" class="nav-section">组织</div>
        <el-menu-item v-if="workspaceStore.can('tenant.members.read')" index="/tenant-members"><el-icon><UserFilled /></el-icon><template #title>成员</template></el-menu-item>
        <el-menu-item v-if="workspaceStore.can('tenant.groups.read')" index="/groups"><el-icon><Collection /></el-icon><template #title>成员分组</template></el-menu-item>
        <el-menu-item v-if="workspaceStore.can('tenant.devices.read')" index="/tenant-member-devices"><el-icon><Monitor /></el-icon><template #title>成员设备</template></el-menu-item>
        <el-menu-item v-if="workspaceStore.can('tenant.admins.read')" index="/tenant-management-memberships"><el-icon><Avatar /></el-icon><template #title>管理员</template></el-menu-item>
        <div v-if="!appStore.sidebarCollapsed && hasTenantResourceMenu" class="nav-section">资源与访问</div>
        <el-menu-item v-if="workspaceStore.can('tenant.resources.read')" index="/resources"><el-icon><Box /></el-icon><template #title>资源目录</template></el-menu-item>
        <el-menu-item v-if="workspaceStore.can('tenant.grants.read')" index="/access-policies"><el-icon><Key /></el-icon><template #title>访问授权</template></el-menu-item>
        <el-menu-item v-if="workspaceStore.can('tenant.sessions.read')" index="/sessions"><el-icon><Clock /></el-icon><template #title>访问会话</template></el-menu-item>
        <div v-if="!appStore.sidebarCollapsed && hasTenantGovernanceMenu" class="nav-section">租户治理</div>
        <el-menu-item v-if="workspaceStore.can('tenant.audit.read')" index="/tenant-audit"><el-icon><DocumentChecked /></el-icon><template #title>审计日志</template></el-menu-item>
        <el-menu-item v-if="workspaceStore.can('tenant.settings.read')" index="/tenant-settings"><el-icon><Setting /></el-icon><template #title>租户设置</template></el-menu-item>
      </template>

      <template v-else-if="workspaceStore.currentWorkspace === 'provider' && workspaceStore.currentContext">
        <el-menu-item v-if="workspaceStore.can('provider.overview.read')" index="/provider-overview"><el-icon><DataAnalysis /></el-icon><template #title>概览</template></el-menu-item>
        <div v-if="!appStore.sidebarCollapsed" class="nav-section">资源供给</div>
        <el-menu-item v-if="workspaceStore.can('provider.technical_resources.read')" index="/provider-technical-resources"><el-icon><Cpu /></el-icon><template #title>技术资源</template></el-menu-item>
        <el-menu-item v-if="workspaceStore.can('provider.resources.read')" index="/provider-supply-candidates"><el-icon><Search /></el-icon><template #title>供给候选</template></el-menu-item>
        <el-menu-item v-if="workspaceStore.can('provider.resources.read')" index="/provider-hosts"><el-icon><Monitor /></el-icon><template #title>主机</template></el-menu-item>
        <el-menu-item v-if="workspaceStore.can('provider.resources.read')" index="/provider-kubernetes"><el-icon><Connection /></el-icon><template #title>Kubernetes</template></el-menu-item>
        <el-menu-item v-if="workspaceStore.can('provider.resources.read')" index="/provider-namespaces"><el-icon><Grid /></el-icon><template #title>Namespace</template></el-menu-item>
        <div v-if="!appStore.sidebarCollapsed" class="nav-section">资源治理</div>
        <el-menu-item v-if="workspaceStore.can('provider.memberships.read')" index="/provider-memberships"><el-icon><Avatar /></el-icon><template #title>管理员</template></el-menu-item>
        <el-menu-item v-if="workspaceStore.can('provider.audit.read')" index="/provider-audit"><el-icon><DocumentChecked /></el-icon><template #title>审计日志</template></el-menu-item>
      </template>

      <template v-else-if="workspaceStore.currentWorkspace === 'platform' && workspaceStore.currentContext">
        <el-menu-item v-if="workspaceStore.can('platform.overview.read')" index="/platform-overview"><el-icon><DataAnalysis /></el-icon><template #title>概览</template></el-menu-item>
        <div v-if="!appStore.sidebarCollapsed" class="nav-section">组织治理</div>
        <el-menu-item v-if="workspaceStore.can('platform.organizations.read')" index="/tenants"><el-icon><OfficeBuilding /></el-icon><template #title>组织管理</template></el-menu-item>
        <el-menu-item v-if="workspaceStore.can('platform.memberships.read')" index="/tenant-admin-memberships"><el-icon><Avatar /></el-icon><template #title>管理授权</template></el-menu-item>
        <div v-if="!appStore.sidebarCollapsed" class="nav-section">资源治理</div>
        <el-menu-item v-if="workspaceStore.can('platform.resources.read')" index="/platform-resources"><el-icon><Box /></el-icon><template #title>资源目录</template></el-menu-item>
        <el-menu-item v-if="workspaceStore.can('platform.allocations.read')" index="/platform-allocations"><el-icon><Connection /></el-icon><template #title>资源分配</template></el-menu-item>
        <div v-if="!appStore.sidebarCollapsed" class="nav-section">平台治理</div>
        <el-menu-item v-if="workspaceStore.can('platform.identities.read')" index="/platform-identities"><el-icon><User /></el-icon><template #title>主体目录</template></el-menu-item>
        <el-menu-item v-if="workspaceStore.can('platform.memberships.read')" index="/platform-admins"><el-icon><UserFilled /></el-icon><template #title>平台管理账号</template></el-menu-item>
        <el-menu-item v-if="workspaceStore.can('platform.user_simulations.read')" index="/platform-user-simulations"><el-icon><Switch /></el-icon><template #title>用户模拟</template></el-menu-item>
        <el-menu-item v-if="workspaceStore.can('platform.audit.read')" index="/platform-audit"><el-icon><Document /></el-icon><template #title>审计日志</template></el-menu-item>
        <el-menu-item v-if="workspaceStore.can('platform.settings.read')" index="/system/config"><el-icon><Setting /></el-icon><template #title>系统配置</template></el-menu-item>
        <el-sub-menu index="diagnostics">
          <template #title><el-icon><Tools /></el-icon><span>高级诊断</span></template>
          <el-menu-item index="/domains">域名管理</el-menu-item>
          <el-menu-item index="/diagnostics/desktop-nodes">Desktop 节点</el-menu-item>
          <el-menu-item index="/diagnostics/nodes">全部节点</el-menu-item>
          <el-menu-item index="/diagnostics/operation-audit">连接操作审计</el-menu-item>
          <el-menu-item index="/acl/ssh">SSH 授权</el-menu-item>
          <el-menu-item index="/tunnel/acl">ACL 管理</el-menu-item>
        </el-sub-menu>
      </template>
    </el-menu>

    <button class="sidebar-footer" type="button" :aria-label="appStore.sidebarCollapsed ? '展开导航' : '折叠导航'" @click="appStore.toggleSidebar">
      <el-icon class="collapse-icon"><Fold v-if="!appStore.sidebarCollapsed" /><Expand v-else /></el-icon>
    </button>
  </div>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, watch } from 'vue'
import { useRoute } from 'vue-router'
import {
  Avatar, Box, Clock, Collection, Connection, Cpu, DataAnalysis, Document, DocumentChecked,
  Expand, Fold, Grid, Key, Monitor, OfficeBuilding, Search, SetUp, Setting, Switch, Tools, User, UserFilled
} from '@element-plus/icons-vue'
import type { ManagementWorkspace } from '@/api/managementContext'
import { useAppStore } from '@/stores/app'
import { useTenantStore } from '@/stores/tenant'
import { useWorkspaceStore, workspaceLabel } from '@/stores/workspace'

const route = useRoute()
const appStore = useAppStore()
const tenantStore = useTenantStore()
const workspaceStore = useWorkspaceStore()

const selectableContexts = computed(() => {
  const contexts = workspaceStore.currentWorkspace === 'tenant' ? workspaceStore.tenantContexts : workspaceStore.providerContexts
  if (!workspaceStore.isSimulationActive || !workspaceStore.simulationSession) return contexts
  return contexts.filter(item => item.scope_type === workspaceStore.simulationSession?.scope_type && item.scope_id === workspaceStore.simulationSession.scope_id)
})
const hasTenantOrganizationMenu = computed(() => ['tenant.members.read', 'tenant.groups.read', 'tenant.devices.read', 'tenant.admins.read'].some(workspaceStore.can))
const hasTenantResourceMenu = computed(() => ['tenant.resources.read', 'tenant.grants.read', 'tenant.sessions.read'].some(workspaceStore.can))
const hasTenantGovernanceMenu = computed(() => ['tenant.audit.read', 'tenant.settings.read'].some(workspaceStore.can))
const activeMenu = computed(() => {
  if (route.path.startsWith('/tenant-overview')) return '/tenant-overview'
  if (route.path.startsWith('/provider-overview')) return '/provider-overview'
  if (route.path.startsWith('/provider-technical-resources')) return '/provider-technical-resources'
  if (route.path.startsWith('/provider-supply-candidates')) return '/provider-supply-candidates'
  if (route.path.startsWith('/provider-hosts')) return '/provider-hosts'
  if (route.path.startsWith('/provider-kubernetes')) return '/provider-kubernetes'
  if (route.path.startsWith('/provider-namespaces')) return '/provider-namespaces'
  if (route.path.startsWith('/provider-memberships')) return '/provider-memberships'
  if (route.path.startsWith('/provider-audit')) return '/provider-audit'
  if (route.path.startsWith('/platform-overview')) return '/platform-overview'
  if (route.path.startsWith('/platform-admins')) return '/platform-admins'
  if (route.path.startsWith('/platform-identities')) return '/platform-identities'
  if (route.path.startsWith('/platform-user-simulations')) return '/platform-user-simulations'
  if (route.path.startsWith('/diagnostics/desktop-nodes')) return '/diagnostics/desktop-nodes'
  if (route.path.startsWith('/diagnostics/nodes')) return '/diagnostics/nodes'
  if (route.path.startsWith('/diagnostics/operation-audit')) return '/diagnostics/operation-audit'
  if (route.path.startsWith('/platform-audit')) return '/platform-audit'
  if (route.path.startsWith('/platform-resources')) return '/platform-resources'
  if (route.path.startsWith('/platform-allocations')) return '/platform-allocations'
  if (route.path.startsWith('/resources')) return '/resources'
  if (route.path.startsWith('/groups')) return '/groups'
  if (route.path.startsWith('/tenant-members')) return '/tenant-members'
  if (route.path.startsWith('/tenant-member-devices')) return '/tenant-member-devices'
  if (route.path.startsWith('/tenant-management-memberships')) return '/tenant-management-memberships'
  if (route.path.startsWith('/tenant-audit')) return '/tenant-audit'
  if (route.path.startsWith('/tenant-settings')) return '/tenant-settings'
  if (route.path.startsWith('/nodes/')) {
    if (route.query.source === 'desktop') return '/diagnostics/desktop-nodes'
    if (route.query.source === 'all') return '/diagnostics/nodes'
  }
  if (route.path.startsWith('/tunnel/acl')) return '/tunnel/acl'
  if (route.path.startsWith('/acl/ssh')) return '/acl/ssh'
  return route.path
})

const selectScope = async (scopeId: string) => {
  const workspace = workspaceStore.currentWorkspace as 'tenant' | 'provider'
  await workspaceStore.selectContext(workspace, scopeId)
}

const handleContextInvalid = () => {
  workspaceStore.loadContexts(true).catch(() => undefined)
  if (workspaceStore.currentWorkspace === 'tenant') tenantStore.loadContexts(true).catch(() => undefined)
}
const handleSessionCleared = () => {
  workspaceStore.reset()
  tenantStore.reset()
}

onMounted(() => {
  workspaceStore.loadContexts().catch(() => undefined)
  if (workspaceStore.currentWorkspace === 'tenant') tenantStore.loadContexts().catch(() => undefined)
  window.addEventListener('tenant-context-invalid', handleContextInvalid)
  window.addEventListener('admin-session-cleared', handleSessionCleared)
})
onBeforeUnmount(() => {
  window.removeEventListener('tenant-context-invalid', handleContextInvalid)
  window.removeEventListener('admin-session-cleared', handleSessionCleared)
})
watch(() => route.fullPath, () => appStore.closeMobileSidebar())
watch(() => workspaceStore.currentWorkspace, (workspace: ManagementWorkspace) => {
  if (workspace === 'tenant') tenantStore.loadContexts().catch(() => undefined)
})
</script>

<style scoped>
.sidebar { height: 100%; display: flex; flex-direction: column; overflow: hidden; background: #fff; border-right: 1px solid var(--border-light); }
.scope-area { display: flex; align-items: center; gap: 8px; min-height: 54px; margin: 9px 10px 4px; padding: 8px 9px; border: 1px solid var(--border-light); border-radius: 6px; background: var(--bg-subtle); }
.scope-area.collapsed { justify-content: center; margin: 8px 6px 4px; padding: 9px 0; }
.scope-area > .el-icon { flex: 0 0 auto; color: var(--primary-color); font-size: 18px; }
.scope-select { min-width: 0; flex: 1; }
.scope-select :deep(.el-select__wrapper) { background: #fff; }
.scope-empty { min-width: 0; display: flex; flex-direction: column; gap: 2px; }
.scope-empty strong { color: var(--text-primary); font-size: 12px; }
.scope-empty span { color: var(--text-secondary); font-size: 11px; line-height: 16px; }
.el-menu { flex: 1; overflow-y: auto; overflow-x: hidden; border-right: 0; background: #fff !important; }
.el-menu::-webkit-scrollbar { width: 0; }
.nav-section { margin-top: 10px; padding: 0 20px 4px; color: var(--text-secondary); font-size: 11px; font-weight: 650; line-height: 18px; }
:deep(.el-menu-item), :deep(.el-sub-menu__title) { height: 40px; line-height: 40px; margin: 2px 10px; padding: 0 12px !important; border-radius: 5px; background: #fff !important; color: var(--text-regular) !important; }
:deep(.el-menu-item .el-icon), :deep(.el-sub-menu__title .el-icon) { width: 18px; height: 18px; margin-right: 10px !important; font-size: 17px; }
:deep(.el-menu-item:hover), :deep(.el-sub-menu__title:hover) { background: var(--sidebar-hover-bg) !important; }
:deep(.el-menu-item.is-active) { background: var(--sidebar-active-bg) !important; color: var(--primary-color) !important; font-weight: 650; }
:deep(.el-menu-item.is-disabled) { color: var(--text-placeholder) !important; cursor: not-allowed; opacity: 0.72; }
:deep(.el-sub-menu .el-menu-item) { height: 38px; line-height: 38px; margin-left: 30px; padding-left: 17px !important; background: #fafcfb !important; font-size: 13px; }
:deep(.el-menu--collapse .el-menu-item), :deep(.el-menu--collapse .el-sub-menu__title) { margin-right: 6px; margin-left: 6px; padding: 0 !important; justify-content: center; }
:deep(.el-menu--collapse .el-menu-item .el-icon), :deep(.el-menu--collapse .el-sub-menu__title .el-icon) { margin-right: 0 !important; }
.sidebar-footer { height: 48px; display: flex; align-items: center; justify-content: center; flex-shrink: 0; width: 100%; border: 0; border-top: 1px solid var(--border-light); background: #fff; color: var(--text-secondary); cursor: pointer; }
.sidebar-footer:hover { background: var(--bg-page); color: var(--primary-color); }
.sidebar-footer:focus-visible { outline: 2px solid var(--primary-color); outline-offset: -2px; }
.collapse-icon { font-size: 19px; }
@media (max-width: 768px) { .sidebar-footer { display: none; } }
</style>
