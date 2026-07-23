<template>
  <div class="candidate-page">
    <div class="page-header">
      <div>
        <div class="eyebrow">运行时观测</div>
        <h1 class="page-title">发现候选</h1>
        <p class="page-subtitle">Agent 发现只提供运行时证据，匹配可信 Workspace 前不会进入资源目录。</p>
      </div>
      <div class="header-actions"><el-button :icon="Link" @click="router.push('/infrastructure/integrations')">可信绑定</el-button><el-button :icon="Refresh" :loading="loading" @click="fetchCandidates">刷新</el-button></div>
    </div>

    <el-alert class="boundary-alert" title="候选没有客户归属，也不能直接授权。只有受信任 Provider 完成 Workspace 绑定后，才会发布为 Resource。" type="info" show-icon :closable="false" />

    <div class="summary-strip">
      <div class="summary-item"><span class="summary-label">全部观测</span><strong>{{ pagination.total }}</strong></div>
      <div class="summary-item"><span class="summary-label">当前页待处理</span><strong class="warning">{{ countBy('observed') + countBy('pending_claim') }}</strong></div>
      <div class="summary-item"><span class="summary-label">当前页冲突</span><strong class="danger">{{ countBy('conflict') }}</strong></div>
      <div class="summary-item"><span class="summary-label">当前页已拒绝</span><strong>{{ countBy('rejected') }}</strong></div>
    </div>

    <el-card shadow="never" class="candidate-surface">
      <div class="toolbar">
        <el-input v-model="filters.search" class="search-input" clearable placeholder="搜索 Namespace、Pod UID 或 Workspace Hint" :prefix-icon="Search" @keyup.enter="handleSearch" />
        <el-select v-model="filters.status" class="status-select" clearable placeholder="全部状态" @change="handleSearch">
          <el-option label="待观察" value="observed" />
          <el-option label="待认领" value="pending_claim" />
          <el-option label="冲突" value="conflict" />
          <el-option label="过期" value="stale" />
          <el-option label="已拒绝" value="rejected" />
          <el-option label="已发布" value="published" />
        </el-select>
        <div class="toolbar-spacer" />
        <span class="result-count">{{ pagination.total }} 条候选</span>
      </div>

      <el-table v-loading="loading" :data="candidates" stripe>
        <el-table-column label="运行目标" min-width="270">
          <template #default="{ row }">
            <div class="target-cell"><strong>{{ row.pod_name || row.pod_uid }}</strong><span>{{ row.namespace }} · {{ row.container_name }}</span><code>{{ row.pod_uid }}</code></div>
          </template>
        </el-table-column>
        <el-table-column label="Workspace Hint" min-width="210">
          <template #default="{ row }"><span class="mono">{{ row.workspace_hint || '-' }}</span><span class="cell-secondary">{{ row.provider_hint || '未知 Provider' }} · 非可信字段</span></template>
        </el-table-column>
        <el-table-column label="Agent / Cluster" min-width="170">
          <template #default="{ row }"><strong>{{ row.agent_name || `Agent ${row.agent_node_id}` }}</strong><span class="cell-secondary">{{ row.cluster_id || '-' }}</span></template>
        </el-table-column>
        <el-table-column label="状态" width="125"><template #default="{ row }"><el-tag size="small" :type="stateTag(row.status)">{{ stateLabel(row.status) }}</el-tag><span v-if="row.conflict_reason" class="cell-secondary">{{ row.conflict_reason }}</span></template></el-table-column>
        <el-table-column label="最近观测" width="165"><template #default="{ row }">{{ formatTime(row.observed_at) }}<span class="cell-secondary">{{ row.ready ? 'Ready' : 'Not Ready' }}</span></template></el-table-column>
        <el-table-column label="操作" width="160" fixed="right"><template #default="{ row }"><el-button v-if="['observed', 'pending_claim', 'conflict'].includes(row.status)" link type="primary" :disabled="!authStore.canWrite" @click="handleReconcile(row)">重新匹配</el-button><el-button v-if="!['rejected', 'published'].includes(row.status)" link type="danger" :disabled="!authStore.canWrite" @click="handleReject(row)">拒绝</el-button><span v-if="['rejected', 'published', 'stale'].includes(row.status)" class="muted">只读</span></template></el-table-column>
      </el-table>
      <el-empty v-if="!loading && !candidates.length" description="暂无发现候选" />
      <div class="pagination-wrapper"><el-pagination v-model:current-page="pagination.page" v-model:page-size="pagination.size" layout="total, prev, pager, next" :total="pagination.total" @current-change="fetchCandidates" /></div>
    </el-card>
  </div>
</template>

<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Link, Refresh, Search } from '@element-plus/icons-vue'
import { getResourceCandidates, reconcileResourceCandidate, rejectResourceCandidate, type DiscoveryCandidate, type DiscoveryCandidateStatus } from '@/api/resource'
import { useAuthStore } from '@/stores/auth'

const loading = ref(false)
const router = useRouter()
const authStore = useAuthStore()
const candidates = ref<DiscoveryCandidate[]>([])
const filters = reactive<{ search: string; status?: DiscoveryCandidateStatus }>({ search: '', status: undefined })
const pagination = reactive({ page: 1, size: 20, total: 0 })
const counts = reactive<Record<string, number>>({})

const fetchCandidates = async () => {
  loading.value = true
  Object.keys(counts).forEach(key => delete counts[key])
  try {
    const res = await getResourceCandidates({ search: filters.search || undefined, status: filters.status, page: pagination.page, size: pagination.size })
    if (res.success && res.data) { candidates.value = res.data; pagination.total = res.total; candidates.value.forEach(item => { counts[item.status] = (counts[item.status] || 0) + 1 }) }
  } finally { loading.value = false }
}
const handleSearch = () => { pagination.page = 1; Object.keys(counts).forEach(key => delete counts[key]); fetchCandidates() }
const countBy = (status: string) => counts[status] || 0
const stateLabel = (status: string) => ({ observed: '待处理', pending_claim: '待认领', conflict: '冲突', stale: '已过期', rejected: '已拒绝', published: '已发布' }[status] || status)
const stateTag = (status: string) => ({ observed: 'warning', pending_claim: 'warning', conflict: 'danger', stale: 'info', rejected: 'info', published: 'success' }[status] || 'info') as any
const formatTime = (value?: string) => value ? new Date(value).toLocaleString('zh-CN', { hour12: false }) : '-'
const handleReject = async (candidate: DiscoveryCandidate) => {
  if (!authStore.canWrite) return
  try {
    await ElMessageBox.confirm(`拒绝 ${candidate.pod_name || candidate.pod_uid}？该候选仍会保留在审计记录中。`, '拒绝发现候选', { type: 'warning', confirmButtonText: '拒绝', cancelButtonText: '取消' })
    const res = await rejectResourceCandidate(candidate.id, '管理员拒绝，等待可信 Workspace 绑定')
    if (res.success) { ElMessage.success('候选已拒绝'); await fetchCandidates() }
  } catch { /* user cancelled */ }
}
const handleReconcile = async (candidate: DiscoveryCandidate) => {
  if (!authStore.canWrite) return
  try {
    const res = await reconcileResourceCandidate(candidate.id)
    if (res.success) {
      ElMessage.success(res.data?.status === 'published' ? '候选已发布为资源' : '候选已进入待认领状态')
      await fetchCandidates()
    }
  } catch { /* request interceptor reports the boundary error */ }
}
onMounted(fetchCandidates)
</script>

<style scoped>
.candidate-page { width: 100%; }
.page-header, .header-actions { display: flex; }
.page-header { justify-content: space-between; align-items: flex-start; gap: 24px; margin-bottom: 18px; }
.header-actions { gap: 8px; }
.eyebrow, .summary-label, .cell-secondary, .target-cell span { color: var(--text-secondary); font-size: 12px; }
.page-title { margin: 0; color: var(--text-primary); font-size: 24px; line-height: 32px; }
.page-subtitle { margin: 5px 0 0; color: var(--text-regular); font-size: 13px; }
.boundary-alert { margin-bottom: 16px; }
.summary-strip { display: grid; grid-template-columns: repeat(4, 1fr); margin-bottom: 16px; background: #fff; border: 1px solid var(--border-light); border-radius: 6px; }
.summary-item { padding: 14px 16px; border-right: 1px solid var(--border-light); }
.summary-item:last-child { border-right: 0; }
.summary-label { display: block; margin-bottom: 4px; }
.summary-item strong { font-size: 21px; color: var(--text-primary); }
.summary-item strong.warning { color: var(--warning-color); }
.summary-item strong.danger { color: var(--danger-color); }
.candidate-surface { border-radius: 6px; }
.toolbar { display: flex; align-items: center; gap: 10px; padding-bottom: 14px; }
.search-input { width: 360px; }
.status-select { width: 145px; }
.toolbar-spacer { flex: 1; }
.result-count, .muted { color: var(--text-secondary); font-size: 12px; }
.target-cell strong, .target-cell span, .target-cell code { display: block; }
.target-cell span, .target-cell code { margin-top: 3px; }
.target-cell code, .mono { color: var(--text-regular); font-family: Consolas, monospace; font-size: 11px; }
.pagination-wrapper { display: flex; justify-content: flex-end; padding-top: 18px; }
@media (max-width: 700px) { .page-header { flex-direction: column; } .summary-strip { grid-template-columns: repeat(2, 1fr); } .summary-item:nth-child(odd) { border-right: 1px solid var(--border-light); } .summary-item:nth-child(n+3) { border-top: 1px solid var(--border-light); } .summary-item:nth-child(even) { border-right: 0; } .toolbar { flex-wrap: wrap; } .search-input { width: 100%; } .status-select { flex: 1; } }
</style>
