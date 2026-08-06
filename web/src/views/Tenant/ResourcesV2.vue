<template>
  <div class="tenant-page">
    <PageHeader title="资源目录" description="查看当前租户发布的 HostSSH、ContainerSSH 与 ContainerService。运行地址和底层端口不作为授权标识。">
      <template #actions><el-button :icon="Refresh" :loading="loading" @click="load">刷新</el-button></template>
    </PageHeader>
    <el-alert v-if="errorMessage" class="state-alert" title="资源目录加载失败" :description="errorMessage" type="error" show-icon :closable="false" />
    <section class="surface">
      <div class="toolbar">
        <el-input v-model="filters.query" class="search-input" clearable :prefix-icon="Search" placeholder="搜索资源名称或描述" @keyup.enter="applyFilters" @clear="applyFilters" />
        <el-select v-model="filters.type" class="filter-select" clearable placeholder="全部类型" @change="applyFilters"><el-option label="SSH 主机" value="host_ssh" /><el-option label="ContainerSSH" value="container_ssh" /><el-option label="ContainerService" value="container_service" /></el-select>
        <el-select v-model="filters.availability" class="filter-select" clearable placeholder="全部可用性" @change="applyFilters"><el-option label="可用" value="available" /><el-option label="异常" value="degraded" /><el-option label="不可用" value="unavailable" /><el-option label="未知" value="unknown" /></el-select>
        <span class="result-count">当前 {{ items.length }} 项</span>
      </div>
      <el-table v-loading="loading" :data="items" stripe>
        <el-table-column label="资源" min-width="260"><template #default="{ row }"><el-link type="primary" :underline="false" @click="router.push(`/resources/${row.resource_id}`)">{{ row.display_name }}</el-link><span class="secondary mono">{{ row.resource_id }}</span></template></el-table-column>
        <el-table-column label="类型" width="155"><template #default="{ row }">{{ typeLabel(row.type) }}</template></el-table-column>
        <el-table-column label="位置" min-width="190"><template #default="{ row }"><span>{{ locationLabel(row) }}</span><span class="secondary mono">{{ row.type === 'host_ssh' ? `Node ${row.agent_node_id || '-'}` : (row.namespace_scope_id || '-') }}</span></template></el-table-column>
        <el-table-column label="目标" min-width="220"><template #default="{ row }"><strong>{{ targetLabel(row) }}</strong><span class="secondary">target rev {{ row.target_revision || 0 }}</span></template></el-table-column>
        <el-table-column label="可见性" width="105"><template #default="{ row }">{{ visibilityLabel(row.visibility_state) }}</template></el-table-column>
        <el-table-column label="可用性" width="110"><template #default="{ row }"><el-tag size="small" :type="availabilityTag(row.availability_state)">{{ availabilityLabel(row.availability_state) }}</el-tag></template></el-table-column>
        <el-table-column label="版本" width="110"><template #default="{ row }">r{{ row.revision }} / v{{ row.row_version }}</template></el-table-column>
        <el-table-column label="更新时间" width="180"><template #default="{ row }">{{ formatTime(row.updated_at) }}</template></el-table-column>
        <el-table-column label="操作" width="110" fixed="right" align="right"><template #default="{ row }"><el-button v-if="canGrant(row)" type="primary" link :icon="Key" @click.stop="openGrant(row)">授权</el-button></template></el-table-column>
      </el-table>
      <el-empty v-if="!loading && !errorMessage && items.length === 0" description="当前租户没有符合条件的已发布资源" />
    </section>
  </div>
</template>

<script setup lang="ts">
import { onMounted, reactive, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { Key, Refresh, Search } from '@element-plus/icons-vue'
import PageHeader from '@/components/Common/PageHeader.vue'
import { getTenantResourcesV2, type TenantResourceV2 } from '@/api/tenantResourcesV2'
import { useTenantStore } from '@/stores/tenant'

const tenantStore = useTenantStore()
const router = useRouter()
const loading = ref(false), errorMessage = ref(''), items = ref<TenantResourceV2[]>([])
const filters = reactive({ query: '', type: '', availability: '' })
const load = async () => {
  const tenantId = tenantStore.tenantId
  if (!tenantId) { items.value = []; errorMessage.value = '当前没有有效的租户上下文。'; return }
  loading.value = true; errorMessage.value = ''
  try {
    const response = await getTenantResourcesV2(tenantId, { query: filters.query.trim() || undefined, type: filters.type || undefined, availability: filters.availability || undefined, limit: 100 })
    items.value = response.success && response.data ? response.data.items : []
  } catch { items.value = []; errorMessage.value = '请确认当前租户权限、资源模型读取开关和服务状态后重试。' }
  finally { loading.value = false }
}
const applyFilters = () => load()
const typeLabel = (value: string) => value === 'host_ssh' ? 'SSH 主机' : value === 'container_ssh' ? 'ContainerSSH' : value === 'container_service' ? 'ContainerService' : value
const visibilityLabel = (value: string) => ({ visible: '可见', hidden: '已隐藏', retired: '已退役', pending: '待发布' }[value] || value)
const availabilityLabel = (value: string) => ({ available: '可用', degraded: '异常', unavailable: '不可用', unknown: '未知' }[value] || value)
const availabilityTag = (value: string) => ({ available: 'success', degraded: 'warning', unavailable: 'danger', unknown: 'info' }[value] || 'info') as any
const locationLabel = (row: TenantResourceV2) => row.type === 'host_ssh' ? (row.ssh_domain || row.display_name) : (row.namespace_name || '-')
const targetLabel = (row: TenantResourceV2) => row.type === 'host_ssh' ? `${row.target_ip || '-'}:${row.target_port || 22}` : row.type === 'container_service' ? `${row.service_name || '-'}:${row.port_number || '-'}` : `${row.pod_name || row.workload_name || '-'} / ${row.container_name || '-'}`
const formatTime = (value: string) => new Date(value).toLocaleString('zh-CN', { hour12: false })
const canGrant = (row: TenantResourceV2) => row.type === 'host_ssh' && tenantStore.canTenant('tenant.grants.write')
const openGrant = (row: TenantResourceV2) => router.push({ path: `/resources/${row.resource_id}`, query: { grant: '1' } })
watch(() => tenantStore.contextRevision, () => { filters.query = ''; filters.type = ''; filters.availability = ''; load() })
onMounted(load)
</script>

<style scoped>
.tenant-page{width:100%}.state-alert{margin-bottom:14px}.surface{overflow:hidden;border:1px solid var(--border-light);border-radius:6px;background:#fff}.toolbar{display:flex;align-items:center;gap:10px;padding:14px 16px;border-bottom:1px solid var(--border-light)}.search-input{width:320px}.filter-select{width:160px}.result-count{margin-left:auto;color:var(--text-secondary);font-size:12px}.secondary{display:block;margin-top:3px;color:var(--text-secondary);font-size:12px}.mono{font-family:'SFMono-Regular',Consolas,'Liberation Mono',monospace}
</style>
