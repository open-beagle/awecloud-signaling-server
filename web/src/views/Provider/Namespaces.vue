<template>
  <div class="provider-page">
    <PageHeader title="Namespace" description="查看当前资源方 Kubernetes Namespace Scope 的稳定身份、隔离模式和可分配状态。">
      <template #actions><el-button :icon="Refresh" :loading="loading" @click="load">刷新</el-button></template>
    </PageHeader>

    <el-alert v-if="errorMessage" class="state-alert" title="Namespace 加载失败" :description="errorMessage" type="error" show-icon :closable="false" />

    <section class="data-surface">
      <div class="toolbar">
        <el-input v-model="filters.search" class="search-input" clearable :prefix-icon="Search" placeholder="搜索 Namespace 稳定标识" @keyup.enter="applyFilters" @clear="applyFilters" />
        <el-select v-model="filters.state" class="filter-select" clearable placeholder="全部 Scope 状态" @change="applyFilters">
          <el-option label="草稿" value="draft" />
          <el-option label="生效中" value="active" />
          <el-option label="可分配" value="allocatable" />
          <el-option label="已暂停" value="suspended" />
          <el-option label="已退役" value="retired" />
        </el-select>
        <span class="result-count">{{ pagination.total }} 个 Namespace Scope</span>
      </div>

      <el-table v-loading="loading" :data="items" stripe>
        <el-table-column label="Namespace 稳定标识" min-width="270"><template #default="{ row }"><strong class="mono">{{ row.stable_key }}</strong><span class="secondary mono">{{ row.id }}</span></template></el-table-column>
        <el-table-column label="Kubernetes" min-width="220"><template #default="{ row }"><strong class="mono">{{ resourceName(row) }}</strong><span class="secondary">{{ resourceSummary(row) }}</span></template></el-table-column>
        <el-table-column label="隔离模式" width="150"><template #default="{ row }">{{ isolationLabel(row.isolation_mode) }}</template></el-table-column>
        <el-table-column label="Scope 状态" width="125"><template #default="{ row }"><el-tag size="small" :type="scopeTag(row.lifecycle_state)">{{ scopeLabel(row.lifecycle_state) }}</el-tag></template></el-table-column>
        <el-table-column label="证据 Revision" width="140"><template #default="{ row }">{{ row.evidence_revision }}</template></el-table-column>
        <el-table-column label="配置 Revision" width="140"><template #default="{ row }">{{ row.config_revision }}</template></el-table-column>
        <el-table-column label="更新时间" width="180"><template #default="{ row }">{{ formatTime(row.updated_at) }}</template></el-table-column>
      </el-table>

      <el-empty v-if="!loading && !errorMessage && items.length === 0" description="当前资源方没有符合条件的 Namespace Scope" />
      <div class="pagination"><el-pagination v-model:current-page="pagination.page" v-model:page-size="pagination.size" :total="pagination.total" :page-sizes="[20, 50, 100]" layout="total, sizes, prev, pager, next" @size-change="load" @current-change="load" /></div>
    </section>
  </div>
</template>

<script setup lang="ts">
import { onMounted, reactive, ref, watch } from 'vue'
import { Refresh, Search } from '@element-plus/icons-vue'
import PageHeader from '@/components/Common/PageHeader.vue'
import { getProviderResourceScopes, type ResourceScope, type ResourceScopeIsolationMode, type ResourceScopeState } from '@/api/providerSupply'
import { useWorkspaceStore } from '@/stores/workspace'

const workspaceStore = useWorkspaceStore()
const loading = ref(false)
const errorMessage = ref('')
const items = ref<ResourceScope[]>([])
const filters = reactive({ search: '', state: '' })
const pagination = reactive({ page: 1, size: 20, total: 0 })

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
    const scopeResponse = await getProviderResourceScopes(providerId, { search: filters.search.trim() || undefined, type: 'namespace', state: filters.state || undefined, page: pagination.page, size: pagination.size })
    items.value = scopeResponse.success && scopeResponse.data ? scopeResponse.data : []
    pagination.total = scopeResponse.total || 0
  } catch {
    items.value = []
    pagination.total = 0
    errorMessage.value = '请确认当前资源方权限和服务状态后重试。'
  } finally {
    loading.value = false
  }
}

const applyFilters = () => { pagination.page = 1; load() }
const resourceName = (scope: ResourceScope) => scope.platform_resource_access_domain || scope.platform_resource_display_name || scope.platform_resource_id
const resourceSummary = (scope: ResourceScope) => {
  const primary = resourceName(scope)
  const names = [scope.platform_resource_display_name, scope.platform_resource_stable_key].filter(value => value && value !== primary)
  return names.join(' · ') || scope.platform_resource_id
}
const isolationLabel = (mode: ResourceScopeIsolationMode) => ({ namespace_isolated: 'Namespace 隔离', reviewed_shared: '已审核共享', '': '-' }[mode])
const scopeLabel = (state: ResourceScopeState) => ({ draft: '草稿', active: '生效中', allocatable: '可分配', suspended: '已暂停', retired: '已退役' }[state])
const scopeTag = (state: ResourceScopeState) => ({ draft: 'warning', active: 'success', allocatable: 'success', suspended: 'warning', retired: 'info' }[state] as any)
const formatTime = (value?: string) => value ? new Date(value).toLocaleString('zh-CN', { hour12: false }) : '-'

watch(() => workspaceStore.providerId, () => { pagination.page = 1; load() })
onMounted(load)
</script>

<style scoped>
.provider-page { width: 100%; }
.state-alert { margin-bottom: 14px; }
.data-surface { overflow: hidden; border: 1px solid var(--border-light); border-radius: 6px; background: #fff; }
.toolbar { display: flex; align-items: center; gap: 10px; padding: 14px 16px; border-bottom: 1px solid var(--border-light); }
.search-input { width: 320px; }
.filter-select { width: 170px; }
.result-count { margin-left: auto; color: var(--text-secondary); font-size: 12px; }
.secondary { display: block; margin-top: 3px; color: var(--text-secondary); font-size: 12px; }
.mono { font-family: 'SFMono-Regular', Consolas, 'Liberation Mono', monospace; }
.pagination { display: flex; justify-content: flex-end; padding: 16px; }
</style>
