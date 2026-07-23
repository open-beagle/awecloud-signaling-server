<template>
  <div class="page-container">
    <div class="page-header">
      <div>
        <h1>租户审计</h1>
        <p>记录真实管理员在当前租户上下文中执行的管理操作和当时生效的权限。</p>
      </div>
      <el-button :loading="loading" @click="fetchLogs">刷新</el-button>
    </div>
    <div class="filters">
      <el-input v-model="search" clearable placeholder="搜索管理员、目标或 Request ID" @keyup.enter="applySearch" @clear="applySearch" />
      <el-input v-model="actionType" clearable placeholder="操作类型" @keyup.enter="applySearch" @clear="applySearch" />
    </div>
    <el-table v-loading="loading" :data="logs" empty-text="当前租户还没有审计记录">
      <el-table-column prop="created_at" label="时间" min-width="180"><template #default="scope">{{ formatTime(scope.row.created_at) }}</template></el-table-column>
      <el-table-column label="真实管理员" min-width="160"><template #default="scope"><strong>{{ scope.row.actor_username || `Admin #${scope.row.actor_admin_id}` }}</strong><div class="secondary">{{ roleLabel(scope.row.tenant_role) }}</div></template></el-table-column>
      <el-table-column prop="action_type" label="操作" min-width="190" show-overflow-tooltip />
      <el-table-column label="目标" min-width="220"><template #default="scope"><span>{{ scope.row.target_name || scope.row.target_id }}</span><div class="secondary">{{ scope.row.target_type }}</div></template></el-table-column>
      <el-table-column prop="required_permission" label="通过权限" min-width="210" show-overflow-tooltip />
      <el-table-column label="权限版本" width="100"><template #default="scope">r{{ scope.row.permission_revision }}</template></el-table-column>
      <el-table-column prop="request_id" label="Request ID" min-width="220" show-overflow-tooltip />
      <el-table-column prop="source_ip" label="来源 IP" width="150" />
    </el-table>
    <div class="pagination"><el-pagination v-model:current-page="page" v-model:page-size="size" layout="total, sizes, prev, pager, next" :total="total" @current-change="fetchLogs" @size-change="resetAndFetch" /></div>
  </div>
</template>

<script setup lang="ts">
import { onMounted, ref, watch } from 'vue'
import { getTenantAuditLogs, type TenantAuditLog } from '@/api/tenantBusiness'
import { useTenantStore } from '@/stores/tenant'

const tenantStore = useTenantStore()
const loading = ref(false)
const logs = ref<TenantAuditLog[]>([])
const search = ref('')
const actionType = ref('')
const page = ref(1)
const size = ref(20)
const total = ref(0)
const formatTime = (value: string) => new Date(value).toLocaleString()
const roleLabel = (role: string) => ({ tenant_admin: '租户管理员', security_auditor: '安全审计员', tenant_viewer: '租户观察员' }[role] || role || '平台操作')
const fetchLogs = async () => {
  if (!tenantStore.tenantId) return
  loading.value = true
  try {
    const response = await getTenantAuditLogs(tenantStore.tenantId, { search: search.value || undefined, action_type: actionType.value || undefined, page: page.value, size: size.value })
    logs.value = response.success && response.data ? response.data : []
    total.value = response.total || 0
  } finally { loading.value = false }
}
const applySearch = () => { page.value = 1; fetchLogs() }
const resetAndFetch = () => { page.value = 1; fetchLogs() }
onMounted(fetchLogs)
watch(() => tenantStore.contextRevision, () => { search.value = ''; actionType.value = ''; page.value = 1; fetchLogs() })
</script>

<style scoped>
.page-header { display: flex; align-items: flex-start; justify-content: space-between; gap: 24px; margin-bottom: 18px; }
h1 { margin: 0; color: var(--text-primary); font-size: 24px; line-height: 32px; }
.page-header p { margin: 5px 0 0; color: var(--text-secondary); font-size: 13px; }
.filters { display: grid; grid-template-columns: minmax(260px, 420px) minmax(180px, 260px); gap: 10px; margin-bottom: 12px; }
.secondary { margin-top: 2px; color: var(--text-secondary); font-size: 12px; }
.pagination { display: flex; justify-content: flex-end; padding-top: 14px; }
@media (max-width: 700px) { .filters { grid-template-columns: 1fr; } }
</style>
