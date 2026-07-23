<template>
  <div class="page-container platform-resource-page">
    <div class="page-header">
      <div><h1>全局资源目录</h1><p>跨租户只读查看统一资源及运行状态；授权与会话操作仍需进入对应租户上下文。</p></div>
      <el-button :icon="Refresh" :loading="loading" @click="load">刷新</el-button>
    </div>
    <div class="summary-strip">
      <div><span>全局资源</span><strong>{{ summary.total }}</strong></div>
      <div><span>可用</span><strong class="success">{{ summary.available }}</strong></div>
      <div><span>ContainerSSH</span><strong>{{ summary.by_type.container_ssh || 0 }}</strong></div>
      <div><span>活动会话</span><strong>{{ summary.active_sessions }}</strong></div>
      <div><span>异常目标</span><strong class="danger">{{ summary.degraded }}</strong></div>
    </div>
    <el-alert title="平台视图不授予任何租户操作权限。查看详情或处理授权前，请先通过租户切换建立有效租户上下文。" type="info" show-icon :closable="false" />
    <section class="resource-surface">
      <div class="toolbar">
        <el-input v-model="filters.search" clearable :prefix-icon="Search" placeholder="搜索资源、Owner 或 Workspace" @keyup.enter="search" />
        <el-select v-model="filters.type" clearable placeholder="全部类型" @change="search"><el-option v-for="option in typeOptions" :key="option.value" :label="option.label" :value="option.value" /></el-select>
        <el-select v-model="filters.state" clearable placeholder="全部状态" @change="search"><el-option label="可用" value="available" /><el-option label="异常" value="degraded" /><el-option label="待发布" value="pending" /><el-option label="已停止" value="stopped" /></el-select>
        <span>{{ pagination.total }} 条资源</span>
      </div>
      <el-table v-loading="loading" :data="items" stripe>
        <el-table-column label="资源" min-width="240"><template #default="{ row }"><strong>{{ row.display_name }}</strong><span class="secondary">{{ typeLabel(row.type) }}<template v-if="row.external_workspace_id"> · {{ row.external_workspace_id }}</template></span></template></el-table-column>
        <el-table-column label="租户" min-width="190"><template #default="{ row }"><strong>{{ row.tenant_name || row.tenant_id }}</strong><span class="secondary">{{ row.tenant_id }}</span></template></el-table-column>
        <el-table-column label="Owner" min-width="160"><template #default="{ row }">{{ row.owner_name || '未指定' }}</template></el-table-column>
        <el-table-column label="状态" width="120"><template #default="{ row }"><el-tag size="small" :type="stateType(row.state)">{{ stateLabel(row.state) }}</el-tag></template></el-table-column>
        <el-table-column label="授权" width="90" align="center"><template #default="{ row }">{{ row.grant_count || 0 }}</template></el-table-column>
        <el-table-column label="会话" width="90" align="center"><template #default="{ row }">{{ row.session_count || 0 }}</template></el-table-column>
        <el-table-column label="更新时间" width="180"><template #default="{ row }">{{ formatTime(row.updated_at) }}</template></el-table-column>
      </el-table>
      <el-empty v-if="!loading && !items.length" description="当前筛选条件下没有统一资源" />
      <div class="pagination"><el-pagination v-model:current-page="pagination.page" v-model:page-size="pagination.size" :total="pagination.total" :page-sizes="[20, 50, 100]" layout="total, sizes, prev, pager, next" @size-change="load" @current-change="load" /></div>
    </section>
  </div>
</template>

<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { Refresh, Search } from '@element-plus/icons-vue'
import { getPlatformResources, getPlatformResourceSummary, type Resource, type ResourceState, type ResourceSummary, type ResourceType } from '@/api/resource'

const loading = ref(false)
const items = ref<Resource[]>([])
const filters = reactive<{ search: string; type: ResourceType | ''; state: ResourceState | '' }>({ search: '', type: '', state: '' })
const pagination = reactive({ page: 1, size: 20, total: 0 })
const summary = reactive<ResourceSummary>({ total: 0, available: 0, degraded: 0, active_sessions: 0, by_type: {} })
const typeOptions: { label: string; value: ResourceType }[] = [{ label: 'ContainerSSH', value: 'container_ssh' }, { label: 'SSH 主机', value: 'host_ssh' }, { label: 'Kubernetes API', value: 'kubernetes_api' }, { label: '数据库服务', value: 'database_service' }, { label: 'TCP 服务', value: 'tcp_service' }]
const load = async () => {
  loading.value = true
  try {
    const [listResponse, summaryResponse] = await Promise.all([
      getPlatformResources({ search: filters.search || undefined, type: filters.type || undefined, state: filters.state || undefined, page: pagination.page, size: pagination.size }),
      getPlatformResourceSummary({ search: filters.search || undefined, state: filters.state || undefined })
    ])
    items.value = listResponse.success && listResponse.data ? listResponse.data : []
    pagination.total = listResponse.total || 0
    if (summaryResponse.success && summaryResponse.data) Object.assign(summary, summaryResponse.data)
  } finally { loading.value = false }
}
const search = () => { pagination.page = 1; load() }
const typeLabel = (type: ResourceType) => typeOptions.find(option => option.value === type)?.label || type
const stateLabel = (state: string) => ({ pending: '待发布', available: '可用', degraded: '异常', draining: '排空中', stopped: '已停止', revoked: '已撤销' }[state] || state)
const stateType = (state: string) => ({ available: 'success', degraded: 'danger', pending: 'warning', draining: 'warning', stopped: 'info', revoked: 'info' }[state] || 'info') as any
const formatTime = (value?: string) => value ? new Date(value).toLocaleString('zh-CN', { hour12: false }) : '-'
onMounted(load)
</script>

<style scoped>
.platform-resource-page { max-width: none; }
.page-header { display: flex; align-items: flex-start; justify-content: space-between; gap: 24px; margin-bottom: 18px; }
h1 { margin: 0; color: var(--text-primary); font-size: 24px; line-height: 32px; }
.page-header p { margin: 5px 0 0; color: var(--text-secondary); font-size: 13px; }
.summary-strip { display: grid; grid-template-columns: repeat(5, 1fr); margin-bottom: 14px; overflow: hidden; border: 1px solid var(--border-light); border-radius: 6px; background: #fff; }
.summary-strip > div { padding: 14px 16px; border-right: 1px solid var(--border-light); }
.summary-strip > div:last-child { border-right: 0; }
.summary-strip span, .summary-strip strong { display: block; }
.summary-strip span, .secondary, .toolbar > span { color: var(--text-secondary); font-size: 12px; }
.summary-strip strong { margin-top: 4px; font-size: 21px; }
.summary-strip strong.success { color: var(--success-color); }.summary-strip strong.danger { color: var(--danger-color); }
.resource-surface { margin-top: 14px; overflow: hidden; border: 1px solid var(--border-light); border-radius: 6px; background: #fff; }
.toolbar { display: flex; align-items: center; gap: 10px; padding: 14px 16px; border-bottom: 1px solid var(--border-light); }
.toolbar .el-input { width: 300px; }.toolbar .el-select { width: 150px; }.toolbar > span { margin-left: auto; }
.secondary { display: block; margin-top: 3px; }
.pagination { display: flex; justify-content: flex-end; padding: 16px; }
@media (max-width: 800px) { .summary-strip { grid-template-columns: repeat(2, 1fr); }.summary-strip > div { border-bottom: 1px solid var(--border-light); }.toolbar { align-items: stretch; flex-direction: column; }.toolbar .el-input,.toolbar .el-select { width: 100%; }.toolbar > span { margin-left: 0; } }
</style>
