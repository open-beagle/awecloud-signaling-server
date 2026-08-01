<template>
  <div class="page-container unavailable-page">
    <PageHeader :title="`${workspaceLabel(requestedWorkspace)}业务不可用`" :description="`当前身份没有可进入的${workspaceLabel(requestedWorkspace)}业务空间。`" />
    <div class="unavailable-surface">
      <el-empty :description="`当前身份没有可进入的${workspaceLabel(requestedWorkspace)}业务空间`">
        <div class="available-actions">
          <el-button
            v-for="workspace in availableWorkspaces"
            :key="workspace"
            :type="workspace === availableWorkspaces[0] ? 'primary' : 'default'"
            @click="enter(workspace)"
          >
            进入{{ workspaceLabel(workspace) }}
          </el-button>
        </div>
      </el-empty>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import type { ManagementWorkspace } from '@/api/managementContext'
import { useWorkspaceStore, workspaceHome, workspaceLabel } from '@/stores/workspace'
import PageHeader from '@/components/Common/PageHeader.vue'

const route = useRoute()
const router = useRouter()
const workspaceStore = useWorkspaceStore()
const requestedWorkspace = computed<ManagementWorkspace>(() => {
  const value = route.query.workspace
  return value === 'tenant' || value === 'provider' || value === 'platform' ? value : workspaceStore.currentWorkspace
})
const availableWorkspaces = computed<ManagementWorkspace[]>(() =>
  (['tenant', 'provider', 'platform'] as ManagementWorkspace[]).filter(workspaceStore.hasContext))

const enter = async (workspace: ManagementWorkspace) => {
  workspaceStore.activateWorkspace(workspace)
  await router.push(workspaceHome(workspace))
}
</script>

<style scoped>
.unavailable-page { width: 100%; }
.unavailable-surface { min-height: calc(100vh - 240px); display: grid; place-items: center; border: 1px solid var(--border-light); border-radius: 6px; background: #fff; }
.available-actions { display: flex; flex-wrap: wrap; justify-content: center; gap: 8px; }
.available-actions :deep(.el-button + .el-button) { margin-left: 0; }
</style>
