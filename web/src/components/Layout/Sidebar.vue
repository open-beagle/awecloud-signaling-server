<template>
  <div class="sidebar">
    <el-menu :default-active="activeMenu" :collapse="appStore.sidebarCollapsed" :collapse-transition="false" router unique-opened>
      <div class="nav-section">资源</div>
      <el-menu-item index="/resources"><el-icon><Box /></el-icon><template #title>资源目录</template></el-menu-item>
      <el-menu-item index="/resource-candidates"><el-icon><Search /></el-icon><template #title>发现候选</template></el-menu-item>

      <div class="nav-section">客户与成员</div>
      <el-menu-item index="/users"><el-icon><User /></el-icon><template #title>成员</template></el-menu-item>
      <el-menu-item index="/groups"><el-icon><UserFilled /></el-icon><template #title>用户组</template></el-menu-item>
      <el-menu-item index="/nodes/desktops"><el-icon><Iphone /></el-icon><template #title>访问设备</template></el-menu-item>

      <div class="nav-section">基础设施</div>
      <el-menu-item index="/nodes/agents"><el-icon><Monitor /></el-icon><template #title>Agent</template></el-menu-item>
      <el-menu-item index="/endpoints"><el-icon><Connection /></el-icon><template #title>Endpoint</template></el-menu-item>

      <div class="nav-section">治理</div>
      <el-menu-item index="/audit-logs"><el-icon><Document /></el-icon><template #title>审计</template></el-menu-item>
      <el-menu-item index="/system/config"><el-icon><Setting /></el-icon><template #title>系统设置</template></el-menu-item>

      <el-sub-menu index="diagnostics">
        <template #title><el-icon><Tools /></el-icon><span>高级诊断</span></template>
        <el-menu-item index="/domains">连接入口</el-menu-item>
        <el-menu-item index="/acl/ssh">旧授权视图</el-menu-item>
        <el-menu-item index="/tunnel/acl">网络策略</el-menu-item>
      </el-sub-menu>
    </el-menu>
    <div class="sidebar-footer" @click="toggleSidebar"><el-icon class="collapse-icon"><Fold v-if="!appStore.sidebarCollapsed" /><Expand v-else /></el-icon></div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useRoute } from 'vue-router'
import { useAppStore } from '@/stores/app'

const route = useRoute()
const appStore = useAppStore()
const activeMenu = computed(() => {
  if (route.path.startsWith('/resources')) return '/resources'
  if (route.path.startsWith('/resource-candidates')) return '/resource-candidates'
  if (route.path === '/nodes/desktops' || (route.path.match(/^\/nodes\/\d+$/) && route.query.type === 'desktop')) return '/nodes/desktops'
  if (route.path.startsWith('/nodes')) return '/nodes/agents'
  if (route.path.startsWith('/acl') || route.path.startsWith('/tunnel')) return '/acl/ssh'
  return route.path
})
const toggleSidebar = () => appStore.toggleSidebar()
</script>

<style scoped>
.sidebar { height: 100%; display: flex; flex-direction: column; overflow: hidden; background: #fff; border-right: 1px solid var(--border-light); }
.el-menu { flex: 1; overflow-y: auto; overflow-x: hidden; border-right: 0; background: #fff !important; }
.el-menu::-webkit-scrollbar { width: 0; }
.nav-section { margin-top: 14px; padding: 0 20px 5px; color: var(--text-secondary); font-size: 11px; font-weight: 650; line-height: 18px; }
.nav-section:first-child { margin-top: 8px; }
:deep(.el-menu-item), :deep(.el-sub-menu__title) { height: 44px; line-height: 44px; margin: 2px 10px; padding: 0 12px !important; border-radius: 5px; background: #fff !important; color: var(--text-regular) !important; }
:deep(.el-menu-item .el-icon), :deep(.el-sub-menu__title .el-icon) { width: 18px; height: 18px; margin-right: 10px !important; font-size: 17px; }
:deep(.el-menu-item:hover), :deep(.el-sub-menu__title:hover) { background: #f0f6f3 !important; }
:deep(.el-menu-item.is-active) { background: #e1f1ea !important; color: var(--primary-color) !important; font-weight: 650; }
:deep(.el-sub-menu .el-menu-item) { height: 38px; line-height: 38px; margin-left: 30px; padding-left: 17px !important; background: #fafcfb !important; font-size: 13px; }
:deep(.el-menu--collapse .el-menu-item), :deep(.el-menu--collapse .el-sub-menu__title) { margin-right: 6px; margin-left: 6px; padding: 0 !important; justify-content: center; }
:deep(.el-menu--collapse .el-menu-item .el-icon), :deep(.el-menu--collapse .el-sub-menu__title .el-icon) { margin-right: 0 !important; }
.sidebar-footer { height: 48px; display: flex; align-items: center; justify-content: center; flex-shrink: 0; border-top: 1px solid var(--border-light); color: var(--text-secondary); cursor: pointer; }
.sidebar-footer:hover { background: var(--bg-page); color: var(--primary-color); }
.collapse-icon { font-size: 19px; }
</style>
