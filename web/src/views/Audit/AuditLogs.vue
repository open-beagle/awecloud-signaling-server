<template>
  <div class="audit-page">
    <PageHeader title="平台审计" eyebrow="Platform Governance" description="统一查询平台、Provider 与 Tenant 管理操作，并保留真实操作者、实际身份、Scope 和权限 revision 证据。">
      <template #actions><el-button :icon="Refresh" :loading="loading" @click="load">刷新</el-button></template>
    </PageHeader>

    <el-alert title="用户模拟记录同时展示真实操作者和实际 User；平台审计读取不会授予目标 Scope 的业务权限。" type="info" show-icon :closable="false" />
    <el-alert v-if="errorMessage" class="state-alert" title="平台审计加载失败" :description="errorMessage" type="error" show-icon :closable="false" />

    <section class="data-surface">
      <div class="toolbar">
        <el-input v-model="filters.search" clearable :prefix-icon="Search" placeholder="搜索身份、目标、Scope 或 Request ID" @keyup.enter="applyFilters" @clear="applyFilters" />
        <el-select v-model="filters.scope_type" clearable placeholder="全部 Scope" @change="applyFilters">
          <el-option label="Platform" value="platform" />
          <el-option label="Provider" value="provider" />
          <el-option label="Tenant" value="tenant" />
        </el-select>
        <el-select v-model="filters.simulation" clearable placeholder="全部身份模式" @change="applyFilters">
          <el-option label="真实身份" value="false" />
          <el-option label="用户模拟" value="true" />
        </el-select>
        <el-input v-model="filters.action_type" class="action-input" clearable placeholder="操作类型" @keyup.enter="applyFilters" @clear="applyFilters" />
        <span>{{ pagination.total }} 条记录</span>
      </div>

      <el-table v-loading="loading" :data="items" :empty-text="errorMessage ? ' ' : '当前筛选条件下没有审计记录'" stripe>
        <el-table-column label="时间 / Request ID" width="205"><template #default="{ row }"><span>{{ formatTime(row.created_at) }}</span><span class="secondary">{{ row.request_id || '—' }}</span></template></el-table-column>
        <el-table-column label="身份" min-width="210">
          <template #default="{ row }">
            <strong>{{ actorLabel(row) }}</strong>
            <span class="secondary" :class="{ simulated: row.simulation_session_id }">{{ identityEvidence(row) }}</span>
          </template>
        </el-table-column>
        <el-table-column label="Scope" min-width="155"><template #default="{ row }"><strong>{{ scopeLabel(row.scope_type) }}</strong><span class="secondary">{{ row.scope_id || '平台全局' }}</span></template></el-table-column>
        <el-table-column prop="action_type" label="操作" min-width="180" show-overflow-tooltip />
        <el-table-column label="目标" min-width="210"><template #default="{ row }"><span>{{ row.target_name || row.target_id }}</span><span class="secondary">{{ row.target_type }} · {{ row.target_id }}</span></template></el-table-column>
        <el-table-column label="权限证据" min-width="220"><template #default="{ row }"><span>{{ row.required_permission || '兼容记录' }}</span><span class="secondary">revision {{ row.permission_revision || '—' }}</span></template></el-table-column>
      </el-table>
      <div class="pagination"><el-pagination v-model:current-page="pagination.page" v-model:page-size="pagination.size" :total="pagination.total" :page-sizes="[20, 50, 100]" layout="total, sizes, prev, pager, next" @size-change="load" @current-change="load" /></div>
    </section>
  </div>
</template>

<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { Refresh, Search } from '@element-plus/icons-vue'
import PageHeader from '@/components/Common/PageHeader.vue'
import { getPlatformAuditLogs, type PlatformAuditLog, type PlatformAuditParams } from '@/api/platformGovernance'

const loading = ref(false)
const errorMessage = ref('')
const items = ref<PlatformAuditLog[]>([])
const filters = reactive<{ search: string; scope_type: PlatformAuditParams['scope_type'] | ''; simulation: PlatformAuditParams['simulation'] | ''; action_type: string }>({ search: '', scope_type: '', simulation: '', action_type: '' })
const pagination = reactive({ page: 1, size: 20, total: 0 })

const load = async () => {
  loading.value = true
  errorMessage.value = ''
  try {
    const response = await getPlatformAuditLogs({
      search: filters.search.trim() || undefined,
      scope_type: filters.scope_type || undefined,
      simulation: filters.simulation || undefined,
      action_type: filters.action_type.trim() || undefined,
      page: pagination.page,
      size: pagination.size
    })
    items.value = response.success && response.data ? response.data : []
    pagination.total = response.total || 0
  } catch {
    items.value = []
    pagination.total = 0
    errorMessage.value = '请确认平台审计读取权限和服务状态后重试。'
  } finally {
    loading.value = false
  }
}
const applyFilters = () => { pagination.page = 1; load() }
const actorLabel = (row: PlatformAuditLog) => row.actor_user_name || row.actor_username || `User #${row.actor_user_id || row.actor_admin_id}`
const identityEvidence = (row: PlatformAuditLog) => row.simulation_session_id
  ? `用户模拟 · 实际身份 ${row.effective_user_name || `User #${row.effective_user_id}`}`
  : `真实身份 · User #${row.actor_user_id || row.effective_user_id || '—'}`
const scopeLabel = (scope: PlatformAuditLog['scope_type']) => scope === 'provider' ? 'Provider' : scope === 'tenant' ? 'Tenant' : 'Platform'
const formatTime = (value: string) => new Date(value).toLocaleString('zh-CN', { hour12: false })

onMounted(load)
</script>

<style scoped>
.audit-page { width: 100%; }
.state-alert { margin-top: 12px; }
.data-surface { margin-top: 14px; overflow: hidden; border: 1px solid var(--border-light); border-radius: 6px; background: #fff; }
.toolbar { display: flex; align-items: center; gap: 10px; padding: 14px 16px; border-bottom: 1px solid var(--border-light); }
.toolbar .el-input { width: 300px; }
.toolbar .el-select { width: 150px; }
.toolbar .action-input { width: 190px; }
.toolbar > span { margin-left: auto; color: var(--text-secondary); font-size: 12px; white-space: nowrap; }
.secondary { display: block; margin-top: 3px; color: var(--text-secondary); font-size: 12px; }
.secondary.simulated { color: var(--el-color-warning-dark-2); }
.pagination { display: flex; justify-content: flex-end; padding: 16px; }
</style>
