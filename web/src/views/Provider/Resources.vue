<template>
  <div class="provider-page">
    <PageHeader :title="pageTitle" :description="pageDescription">
      <template #actions><el-button :icon="Refresh" :loading="loading" @click="load">刷新</el-button></template>
    </PageHeader>

    <el-alert v-if="errorMessage" class="state-alert" :title="`${pageTitle}加载失败`" :description="errorMessage" type="error" show-icon :closable="false" />

    <section class="data-surface">
      <div class="toolbar">
        <el-input v-model="filters.search" class="search-input" clearable :prefix-icon="Search" :placeholder="searchPlaceholder" @keyup.enter="applyFilters" @clear="applyFilters" />
        <el-select v-model="filters.state" class="filter-select" clearable placeholder="全部生命周期" @change="applyFilters">
          <el-option label="草稿" value="draft" />
          <el-option label="生效中" value="active" />
          <el-option label="已暂停" value="suspended" />
          <el-option label="已退役" value="retired" />
        </el-select>
        <span class="result-count">{{ pagination.total }} 项资源</span>
      </div>

      <el-table v-loading="loading" :data="items" stripe>
        <el-table-column :label="resourceType === 'kubernetes' ? 'Kubernetes' : '主机'" min-width="250">
          <template #default="{ row }">
            <strong class="mono">{{ resourcePrimaryName(row) }}</strong>
            <span class="secondary">{{ resourceSecondaryName(row) }}</span>
          </template>
        </el-table-column>
        <el-table-column label="健康" width="110"><template #default="{ row }"><el-tag size="small" effect="plain" :type="healthTag(row.health_state)">{{ healthLabel(row.health_state) }}</el-tag></template></el-table-column>
        <el-table-column label="生命周期" width="120"><template #default="{ row }"><el-tag size="small" :type="lifecycleTag(row.lifecycle_state)">{{ lifecycleLabel(row.lifecycle_state) }}</el-tag></template></el-table-column>
        <el-table-column :label="resourceType === 'kubernetes' ? '可分配 Namespace' : '可分配 Scope'" width="150" align="center"><template #default="{ row }"><strong class="scope-count">{{ row.allocatable_scope_count }}</strong></template></el-table-column>
        <el-table-column label="能力 Revision" width="140"><template #default="{ row }">{{ row.capability_revision }}</template></el-table-column>
        <el-table-column label="对象 Revision" width="140"><template #default="{ row }">{{ row.row_version }}</template></el-table-column>
        <el-table-column label="更新时间" width="180"><template #default="{ row }">{{ formatTime(row.updated_at) }}</template></el-table-column>
        <el-table-column v-if="resourceType === 'host'" label="操作" width="150" fixed="right" align="right">
          <template #default="{ row }">
            <el-tooltip :disabled="!!row.source_node_id" content="仅 legacy Agent 主机支持编辑 SSH 主机域名标识" placement="top">
              <span><el-button link type="primary" :icon="Edit" :disabled="!canWrite || !row.source_node_id" @click="editHostDomainLabel(row)">编辑</el-button></span>
            </el-tooltip>
            <el-button link type="danger" :icon="Delete" :loading="deletingResourceId === row.id" :disabled="!canWrite || row.lifecycle_state === 'retired'" @click="deleteHost(row)">删除</el-button>
          </template>
        </el-table-column>
      </el-table>

      <el-empty v-if="!loading && !errorMessage && items.length === 0" :description="`当前资源方没有符合条件的${pageTitle}`" />
      <div class="pagination"><el-pagination v-model:current-page="pagination.page" v-model:page-size="pagination.size" :total="pagination.total" :page-sizes="[20, 50, 100]" layout="total, sizes, prev, pager, next" @size-change="load" @current-change="load" /></div>
    </section>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Delete, Edit, Refresh, Search } from '@element-plus/icons-vue'
import PageHeader from '@/components/Common/PageHeader.vue'
import { deleteProviderPlatformResource, getProviderPlatformResources, updateProviderPlatformHostDomainLabel, type PlatformResource, type PlatformResourceState, type SupplyResourceType } from '@/api/providerSupply'
import { useWorkspaceStore } from '@/stores/workspace'

const props = defineProps<{ resourceType: SupplyResourceType }>()
const workspaceStore = useWorkspaceStore()
const loading = ref(false)
const deletingResourceId = ref('')
const errorMessage = ref('')
const items = ref<PlatformResource[]>([])
const filters = reactive({ search: '', state: '' })
const pagination = reactive({ page: 1, size: 20, total: 0 })
const canWrite = computed(() => workspaceStore.can('provider.resources.write'))
const pageTitle = computed(() => props.resourceType === 'kubernetes' ? 'Kubernetes 资源' : '主机资源')
const pageDescription = computed(() => props.resourceType === 'kubernetes'
  ? '查看当前资源方确认的 Kubernetes Cluster、健康状态和可分配 Namespace 数量。'
  : '查看当前资源方确认的 Host、健康状态和 whole-host Scope 可分配情况。')
const searchPlaceholder = computed(() => props.resourceType === 'kubernetes' ? '搜索访问域名、集群名称或稳定标识' : '搜索主机名称或稳定标识')

const load = async () => {
  const providerId = workspaceStore.providerId
  if (!providerId) {
    items.value = []
    pagination.total = 0
    errorMessage.value = '当前没有有效的资源方上下文。'
    return
  }
  loading.value = true
  errorMessage.value = ''
  try {
    const response = await getProviderPlatformResources(providerId, {
      search: filters.search.trim() || undefined,
      type: props.resourceType,
      state: filters.state || undefined,
      page: pagination.page,
      size: pagination.size,
    })
    items.value = response.success && response.data ? response.data : []
    pagination.total = response.total || 0
  } catch {
    items.value = []
    pagination.total = 0
    errorMessage.value = '请确认当前资源方权限和服务状态后重试。'
  } finally {
    loading.value = false
  }
}

const applyFilters = () => { pagination.page = 1; load() }
const lifecycleLabel = (state: PlatformResourceState) => ({ draft: '草稿', active: '生效中', suspended: '已暂停', retired: '已退役' }[state])
const lifecycleTag = (state: PlatformResourceState) => ({ draft: 'warning', active: 'success', suspended: 'warning', retired: 'info' }[state] as any)
const healthLabel = (state: string) => ({ unknown: '未知', online: '健康', degraded: '异常', offline: '离线' }[state] || state)
const healthTag = (state: string) => ({ online: 'success', degraded: 'warning', offline: 'danger', unknown: 'info' }[state] || 'info') as any
const formatTime = (value?: string) => value ? new Date(value).toLocaleString('zh-CN', { hour12: false }) : '-'
const resourcePrimaryName = (resource: PlatformResource) => resource.access_domain || resource.display_name || resource.stable_key
const resourceSecondaryName = (resource: PlatformResource) => {
  const names = [resource.host_domain_label, resource.display_name, resource.stable_key].filter(value => value && value !== resourcePrimaryName(resource))
  return names.join(' · ') || '-'
}

const editHostDomainLabel = async (resource: PlatformResource) => {
  if (!workspaceStore.providerId || resource.type !== 'host') return
  try {
    const result = await ElMessageBox.prompt('修改后 Desktop 展示和连接使用的新 SSH 域名会同步生效，旧域名立即失效。', '编辑 SSH 主机域名标识', {
      inputValue: resource.host_domain_label || '',
      inputPattern: /^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$/,
      inputErrorMessage: '请输入有效的 DNS 单标签',
      confirmButtonText: '保存', cancelButtonText: '取消', type: 'warning',
    })
    await updateProviderPlatformHostDomainLabel(workspaceStore.providerId, resource, result.value.trim().toLowerCase())
    ElMessage.success('SSH 主机域名标识已更新')
    await load()
  } catch (error) {
    if (error !== 'cancel' && error !== 'close') throw error
  }
}

const deleteHost = async (resource: PlatformResource) => {
  if (!workspaceStore.providerId || resource.type !== 'host' || resource.lifecycle_state === 'retired') return
  try {
    const result = await ElMessageBox.prompt(`删除 ${resourcePrimaryName(resource)} 后将停止供给且不可恢复，请输入删除原因。`, '确认删除主机', {
      confirmButtonText: '确认删除', cancelButtonText: '取消', inputPattern: /\S+/, inputErrorMessage: '请输入删除原因', type: 'warning',
    })
    deletingResourceId.value = resource.id
    await deleteProviderPlatformResource(workspaceStore.providerId, resource, result.value.trim())
    ElMessage.success('主机已删除')
    await load()
  } catch (error) {
    if (error !== 'cancel' && error !== 'close') throw error
  } finally {
    deletingResourceId.value = ''
  }
}

watch(() => [workspaceStore.providerId, props.resourceType], () => { pagination.page = 1; load() })
onMounted(load)
</script>

<style scoped>
.provider-page { width: 100%; }
.state-alert { margin-bottom: 14px; }
.data-surface { overflow: hidden; border: 1px solid var(--border-light); border-radius: 6px; background: #fff; }
.toolbar { display: flex; align-items: center; gap: 10px; padding: 14px 16px; border-bottom: 1px solid var(--border-light); }
.search-input { width: 320px; }
.filter-select { width: 160px; }
.result-count { margin-left: auto; color: var(--text-secondary); font-size: 12px; }
.secondary { display: block; margin-top: 3px; color: var(--text-secondary); font-size: 12px; }
.mono { font-family: 'SFMono-Regular', Consolas, 'Liberation Mono', monospace; }
.scope-count { color: var(--primary-dark); font-size: 16px; }
.pagination { display: flex; justify-content: flex-end; padding: 16px; }
</style>
