<template>
  <div class="resource-page">
    <div class="page-header">
      <div>
        <div class="eyebrow">资源访问</div>
        <h1 class="page-title">资源目录</h1>
        <p class="page-subtitle">按稳定资源管理访问入口、授权和会话。运行地址与底层端口仅在资源诊断中展示。</p>
      </div>
      <div class="header-actions">
        <el-button :icon="Refresh" :loading="loading" @click="fetchResources">刷新</el-button>
        <el-button type="primary" :icon="Plus" :disabled="!tenantStore.tenantId || !authStore.canWrite" @click="showCreate = true">登记 ContainerSSH</el-button>
      </div>
    </div>

    <div class="summary-strip">
      <div class="summary-item"><span class="summary-label">当前资源</span><strong>{{ summary.total }}</strong></div>
      <div class="summary-item"><span class="summary-label">可用</span><strong class="success">{{ summary.available }}</strong></div>
      <div class="summary-item"><span class="summary-label">ContainerSSH</span><strong>{{ summary.by_type.container_ssh || 0 }}</strong></div>
      <div class="summary-item"><span class="summary-label">活动会话</span><strong>{{ summary.active_sessions }}</strong></div>
      <div class="summary-item"><span class="summary-label">异常目标</span><strong class="danger">{{ summary.degraded }}</strong></div>
    </div>

    <section v-if="showLegacyInventory" class="legacy-band">
      <div class="legacy-copy">
        <div class="legacy-title"><el-icon><Warning /></el-icon><strong>存量接入（兼容模式）</strong><el-tag size="small" type="warning" effect="plain">待归属</el-tag></div>
        <p>这些节点继续使用旧数据和授权链路，尚未绑定 Tenant，也不会自动转换为统一 Resource。</p>
      </div>
      <div class="legacy-metrics">
        <button @click="router.push('/nodes/agents')"><strong>{{ legacyInventory.agents }}</strong><span>Agent</span></button>
        <button @click="router.push('/nodes/desktops')"><strong>{{ legacyInventory.desktops }}</strong><span>访问设备</span></button>
        <button @click="router.push('/endpoints')"><strong>{{ legacyInventory.endpoints }}</strong><span>Endpoint</span></button>
      </div>
      <el-button :icon="ArrowRight" @click="router.push('/legacy-inventory')">核验与认领</el-button>
    </section>

    <el-card class="resource-surface" shadow="never">
      <div class="resource-tabs">
        <button v-for="tab in tabs" :key="tab.value" class="resource-tab" :class="{ active: activeType === tab.value }" @click="selectType(tab.value)">
          {{ tab.label }} <span>{{ tab.count }}</span>
        </button>
      </div>
      <div class="toolbar">
        <el-input v-model="filters.search" clearable class="search-input" placeholder="搜索资源、Owner 或 Workspace" :prefix-icon="Search" @keyup.enter="handleSearch" />
        <el-select v-model="filters.state" clearable placeholder="全部状态" class="filter-select" @change="handleSearch">
          <el-option label="可用" value="available" />
          <el-option label="异常" value="degraded" />
          <el-option label="排空中" value="draining" />
          <el-option label="已停止" value="stopped" />
        </el-select>
        <span class="toolbar-spacer" />
        <span class="result-count">当前 {{ resources.length }} 条</span>
      </div>

      <el-table v-loading="loading" :data="resources" stripe class="resource-table" @row-click="goDetail">
        <el-table-column label="资源" min-width="250">
          <template #default="{ row }">
            <div class="resource-cell">
              <span class="type-icon" :class="`type-${row.type}`"><el-icon><component :is="resourceIcon(row.type)" /></el-icon></span>
              <div class="resource-copy"><el-link :underline="false" type="primary" @click.stop="goDetail(row)">{{ row.display_name }}</el-link><span>{{ typeLabel(row.type) }}<template v-if="row.external_workspace_id"> · {{ row.external_workspace_id }}</template></span></div>
            </div>
          </template>
        </el-table-column>
        <el-table-column label="客户 / Owner" min-width="190">
          <template #default="{ row }"><strong>{{ row.tenant_name || row.tenant_id }}</strong><span class="cell-secondary">{{ row.owner_name || '未指定 Owner' }}</span></template>
        </el-table-column>
        <el-table-column label="位置 / 提供者" min-width="230">
          <template #default="{ row }"><strong>{{ row.cluster_id || row.provider_id || '-' }}</strong><span class="cell-secondary">{{ locationLabel(row) }}</span></template>
        </el-table-column>
        <el-table-column label="状态" width="130">
          <template #default="{ row }"><el-tag size="small" :type="stateTag(row.state)">{{ stateLabel(row.state) }}</el-tag></template>
        </el-table-column>
        <el-table-column label="授权" width="90"><template #default="{ row }"><strong>{{ row.grant_count || 0 }}</strong></template></el-table-column>
        <el-table-column label="会话" width="90"><template #default="{ row }"><strong>{{ row.session_count || 0 }}</strong></template></el-table-column>
        <el-table-column label="更新时间" width="150"><template #default="{ row }"><span>{{ formatTime(row.updated_at) }}</span><span class="cell-secondary">rev {{ row.target_revision }}</span></template></el-table-column>
        <el-table-column label="" width="64" fixed="right" align="center"><template #default><el-button text :icon="MoreFilled" @click.stop="showActionMessage" /></template></el-table-column>
      </el-table>

      <el-empty v-if="!loading && resources.length === 0" :description="tenantStore.tenantId ? '当前客户还没有已登记资源' : '尚未建立统一资源目录'">
        <el-button v-if="authStore.isPlatformAdmin" type="primary" :icon="OfficeBuilding" @click="router.push('/tenants')">初始化客户</el-button>
      </el-empty>
      <div class="pagination-wrapper"><el-pagination v-model:current-page="pagination.page" v-model:page-size="pagination.size" :total="pagination.total" :page-sizes="[20, 50, 100]" layout="total, sizes, prev, pager, next" @size-change="fetchResources" @current-change="fetchResources" /></div>
    </el-card>

    <el-dialog v-model="showCreate" title="登记 ContainerSSH" width="520px">
      <el-alert title="HostSSH、Kubernetes、数据库和 TCP 仍使用兼容入口；完成各自 Target/Action 契约前不能登记为统一资源。" type="warning" :closable="false" show-icon class="dialog-alert" />
      <el-form label-position="top" :model="createForm">
        <el-form-item label="资源名称" required><el-input v-model="createForm.display_name" placeholder="例如：算法开发 IDE" /></el-form-item>
        <el-form-item label="资源类型"><el-input model-value="ContainerSSH" disabled /></el-form-item>
        <el-form-item label="Provider"><el-input v-model="createForm.provider_id" placeholder="例如：beagle-ide" /></el-form-item>
        <el-form-item label="Workspace ID"><el-input v-model="createForm.external_workspace_id" placeholder="受信任 Provider 的稳定业务 ID" /></el-form-item>
      </el-form>
      <template #footer><el-button @click="showCreate = false">取消</el-button><el-button type="primary" :loading="creating" @click="createResource">创建</el-button></template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import { ArrowRight, Coin, Connection, Monitor, MoreFilled, OfficeBuilding, Plus, Refresh, Search, Ship, TakeawayBox, Warning } from '@element-plus/icons-vue'
import { createManagedResource, getManagedResources, getResourceSummary, type Resource, type ResourceSummary, type ResourceType } from '@/api/resource'
import { getNodes } from '@/api/node'
import { getEndpoints } from '@/api/endpoint'
import { useTenantStore } from '@/stores/tenant'
import { useAuthStore } from '@/stores/auth'

const router = useRouter()
const tenantStore = useTenantStore()
const authStore = useAuthStore()
const loading = ref(false)
const creating = ref(false)
const showCreate = ref(false)
const resources = ref<Resource[]>([])
const activeType = ref<ResourceType | ''>('')
const filters = reactive<{ search: string; state: string }>({ search: '', state: '' })
const createForm = reactive({ display_name: '', type: 'container_ssh' as ResourceType, provider_id: 'beagle-ide', external_workspace_id: '' })
const pagination = reactive({ page: 1, size: 20, total: 0 })
const summary = reactive<ResourceSummary>({ total: 0, available: 0, degraded: 0, active_sessions: 0, by_type: {} })
const legacyInventory = reactive({ agents: 0, desktops: 0, endpoints: 0 })
const showLegacyInventory = computed(() => authStore.role !== 'tenant_admin' && legacyInventory.agents + legacyInventory.desktops + legacyInventory.endpoints > 0)
const tabs = computed(() => [
  { label: '全部', value: '' as ResourceType | '', count: summary.total },
  { label: '容器', value: 'container_ssh' as ResourceType, count: summary.by_type.container_ssh || 0 },
  { label: 'SSH 主机', value: 'host_ssh' as ResourceType, count: summary.by_type.host_ssh || 0 },
  { label: 'Kubernetes', value: 'kubernetes_api' as ResourceType, count: summary.by_type.kubernetes_api || 0 },
  { label: '数据库', value: 'database_service' as ResourceType, count: summary.by_type.database_service || 0 },
  { label: 'TCP', value: 'tcp_service' as ResourceType, count: summary.by_type.tcp_service || 0 }
])

const fetchResources = async () => {
  loading.value = true
  try {
    const [res, summaryRes] = await Promise.all([
      getManagedResources({ tenant_id: tenantStore.tenantId || undefined, type: activeType.value || undefined, state: filters.state || undefined, search: filters.search || undefined, page: pagination.page, size: pagination.size }),
      getResourceSummary({ tenant_id: tenantStore.tenantId || undefined, state: filters.state || undefined, search: filters.search || undefined })
    ])
    if (res.success && res.data) {
      resources.value = res.data
      pagination.total = res.total
    }
    if (summaryRes.success && summaryRes.data) Object.assign(summary, summaryRes.data)
  } catch (error) {
    console.error('获取统一资源失败:', error)
  } finally {
    loading.value = false
  }
}

const fetchLegacyInventory = async () => {
  if (authStore.role === 'tenant_admin') return
  try {
    const [agents, desktops, endpoints] = await Promise.all([
      getNodes({ type: 'agent', page: 1, size: 1 }),
      getNodes({ type: 'desktop', page: 1, size: 1 }),
      getEndpoints({ page: 1, size: 1 })
    ])
    legacyInventory.agents = agents.total || 0
    legacyInventory.desktops = desktops.total || 0
    legacyInventory.endpoints = endpoints.total || 0
  } catch (error) {
    console.error('获取存量接入概览失败:', error)
  }
}

const handleSearch = () => {
  pagination.page = 1
  fetchResources()
}

const selectType = (type: ResourceType | '') => {
  activeType.value = type
  handleSearch()
}

const goDetail = (row: Resource) => router.push(`/resources/${row.id}`)

const createResource = async () => {
  if (!tenantStore.tenantId) return ElMessage.warning('请先选择一个客户上下文')
  if (!createForm.display_name.trim()) return ElMessage.warning('请输入资源名称')
  creating.value = true
  try {
    const res = await createManagedResource({ tenant_id: tenantStore.tenantId, ...createForm })
    if (res.success) {
      ElMessage.success('资源已创建')
      showCreate.value = false
      createForm.display_name = ''
      createForm.external_workspace_id = ''
      await fetchResources()
    }
  } finally {
    creating.value = false
  }
}

const showActionMessage = () => ElMessage.info('低频资源操作将在资源详情中处理')
const typeLabel = (type: ResourceType) => ({ container_ssh: 'ContainerSSH', host_ssh: 'HostSSH', kubernetes_api: 'KubernetesAPI', database_service: 'DatabaseService', tcp_service: 'TCPService' }[type] || type)
const stateLabel = (state: string) => ({ pending: '待发布', available: '可用', degraded: '异常', draining: '排空中', stopped: '已停止', revoked: '已撤销' }[state] || state)
const stateTag = (state: string) => ({ available: 'success', degraded: 'danger', draining: 'warning', stopped: 'info', revoked: 'info', pending: 'warning' }[state] || 'info') as any
const resourceIcon = (type: ResourceType) => ({ container_ssh: TakeawayBox, host_ssh: Monitor, kubernetes_api: Ship, database_service: Coin, tcp_service: Connection }[type] || TakeawayBox)
const locationLabel = (row: Resource) => row.type === 'container_ssh' ? `${row.namespace || '-'} · ${row.provider_id || '-'}` : `${row.provider_id || '未指定 Provider'}${row.namespace ? ` · ${row.namespace}` : ''}`
const formatTime = (value?: string) => value ? new Date(value).toLocaleString('zh-CN', { hour12: false }) : '-'

watch(() => tenantStore.tenantId, () => { pagination.page = 1; fetchResources() })
onMounted(() => { fetchResources(); fetchLegacyInventory() })
</script>

<style scoped>
.resource-page { width: 100%; }
.page-header { display: flex; justify-content: space-between; align-items: flex-start; gap: 24px; margin-bottom: 20px; }
.eyebrow { color: var(--text-secondary); font-size: 12px; margin-bottom: 5px; }
.page-title { margin: 0; color: var(--text-primary); font-size: 24px; line-height: 32px; }
.page-subtitle { margin: 5px 0 0; color: var(--text-regular); font-size: 13px; }
.header-actions { display: flex; gap: 8px; }
.summary-strip { display: grid; grid-template-columns: repeat(5, 1fr); margin-bottom: 16px; background: #fff; border: 1px solid var(--border-light); border-radius: 6px; }
.summary-item { padding: 14px 16px; border-right: 1px solid var(--border-light); }
.summary-item:last-child { border-right: 0; }
.summary-label { display: block; color: var(--text-secondary); font-size: 12px; margin-bottom: 4px; }
.summary-item strong { font-size: 21px; color: var(--text-primary); }
.summary-item strong.success { color: var(--success-color); }
.summary-item strong.danger { color: var(--danger-color); }
.legacy-band { display: grid; grid-template-columns: minmax(280px, 1fr) auto auto; align-items: center; gap: 22px; margin-bottom: 16px; padding: 14px 16px; background: #fffaf0; border: 1px solid #ead7ad; border-radius: 6px; }
.legacy-title { display: flex; align-items: center; gap: 8px; color: #72521b; }
.legacy-copy p { margin: 4px 0 0; color: #7a6849; font-size: 12px; }
.legacy-metrics { display: flex; border-left: 1px solid #ead7ad; border-right: 1px solid #ead7ad; }
.legacy-metrics button { min-width: 88px; padding: 2px 14px; border: 0; background: transparent; cursor: pointer; }
.legacy-metrics strong, .legacy-metrics span { display: block; }
.legacy-metrics strong { color: var(--text-primary); font-size: 18px; }
.legacy-metrics span { margin-top: 1px; color: var(--text-secondary); font-size: 11px; }
.resource-surface { border-radius: 6px; }
.resource-tabs { display: flex; gap: 4px; overflow-x: auto; border-bottom: 1px solid var(--border-light); }
.resource-tab { position: relative; height: 44px; padding: 0 12px; color: var(--text-regular); background: transparent; border: 0; cursor: pointer; white-space: nowrap; }
.resource-tab.active { color: var(--primary-color); font-weight: 600; }
.resource-tab.active::after { position: absolute; right: 8px; bottom: -1px; left: 8px; height: 2px; background: var(--primary-color); content: ''; }
.resource-tab span { display: inline-block; min-width: 18px; margin-left: 5px; padding: 1px 5px; border-radius: 4px; color: var(--text-secondary); background: var(--bg-page); font-size: 11px; }
.toolbar { display: flex; align-items: center; gap: 10px; padding: 12px 0; }
.search-input { width: 340px; }
.filter-select { width: 140px; }
.toolbar-spacer { flex: 1; }
.result-count { color: var(--text-secondary); font-size: 12px; }
.resource-table :deep(.el-table__row) { cursor: pointer; }
.resource-cell { display: flex; align-items: center; min-width: 0; gap: 9px; }
.type-icon { display: inline-flex; width: 30px; height: 30px; align-items: center; justify-content: center; flex: 0 0 30px; border-radius: 5px; }
.type-container_ssh { color: #176b55; background: #e5f3ed; }
.type-host_ssh { color: #2f6fba; background: #eaf2fb; }
.type-kubernetes_api { color: #9b600e; background: #fff3dc; }
.type-database_service { color: #725096; background: #f1ebf8; }
.type-tcp_service { color: #7c5550; background: #f4eeee; }
.resource-copy { min-width: 0; }
.resource-copy .el-link { display: block; max-width: 100%; overflow: hidden; font-weight: 600; text-overflow: ellipsis; white-space: nowrap; }
.resource-copy > span, .cell-secondary { display: block; margin-top: 2px; color: var(--text-secondary); font-size: 11px; line-height: 16px; }
.resource-table strong { color: var(--text-primary); font-weight: 600; }
.pagination-wrapper { display: flex; justify-content: flex-end; padding-top: 18px; }
.dialog-alert { margin-bottom: 16px; }
@media (max-width: 1000px) { .legacy-band { grid-template-columns: 1fr auto; } .legacy-copy { grid-column: 1 / -1; } }
@media (max-width: 900px) { .summary-strip { grid-template-columns: repeat(3, 1fr); } .summary-item:nth-child(n+4) { border-top: 1px solid var(--border-light); } .search-input { width: 260px; } }
@media (max-width: 640px) { .page-header { flex-direction: column; } .summary-strip { grid-template-columns: repeat(2, 1fr); } .summary-item:nth-child(odd) { border-right: 1px solid var(--border-light); } .summary-item:nth-child(even) { border-right: 0; } .toolbar { flex-wrap: wrap; } .search-input { width: 100%; } .filter-select { flex: 1; min-width: 120px; } .legacy-band { grid-template-columns: 1fr; } .legacy-metrics { justify-content: space-around; border: 0; border-top: 1px solid #ead7ad; border-bottom: 1px solid #ead7ad; padding: 10px 0; } .legacy-metrics button { min-width: 0; padding: 0 8px; } }
</style>
