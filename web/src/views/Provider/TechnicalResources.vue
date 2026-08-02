<template>
  <div class="provider-page">
    <PageHeader title="技术资源" description="查看当前资源方注册的 Agent 与 Endpoint，以及库存租约和健康状态。">
      <template #actions>
        <el-button :icon="Refresh" :loading="loading" @click="load">刷新</el-button>
      </template>
    </PageHeader>

    <el-alert
      v-if="errorMessage"
      class="state-alert"
      title="技术资源加载失败"
      :description="errorMessage"
      type="error"
      show-icon
      :closable="false"
    >
      <template #default><el-button link type="primary" @click="load">重新加载</el-button></template>
    </el-alert>

    <section class="data-surface">
      <div class="toolbar">
        <el-input v-model="filters.search" class="search-input" clearable :prefix-icon="Search" placeholder="搜索稳定标识" @keyup.enter="applyFilters" @clear="applyFilters" />
        <el-select v-model="filters.type" class="filter-select" clearable placeholder="全部类型" @change="applyFilters">
          <el-option label="Agent" value="agent" />
          <el-option label="Endpoint" value="endpoint" />
        </el-select>
        <el-select v-model="filters.state" class="filter-select" clearable placeholder="全部生命周期" @change="applyFilters">
          <el-option label="待注册" value="pending" />
          <el-option label="已注册" value="registered" />
          <el-option label="已禁用" value="disabled" />
          <el-option label="已退役" value="retired" />
        </el-select>
        <span class="result-count">{{ pagination.total }} 项技术资源</span>
      </div>

      <el-table v-loading="loading" :data="items" stripe>
        <el-table-column label="技术资源" min-width="250">
          <template #default="{ row }">
            <strong>{{ row.stable_key }}</strong>
            <span class="secondary mono">{{ row.id }}</span>
          </template>
        </el-table-column>
        <el-table-column label="类型" width="110"><template #default="{ row }">{{ typeLabel(row.type) }}</template></el-table-column>
        <el-table-column label="生命周期" width="120"><template #default="{ row }"><el-tag size="small" :type="lifecycleTag(row.lifecycle_state)">{{ lifecycleLabel(row.lifecycle_state) }}</el-tag></template></el-table-column>
        <el-table-column label="健康" width="110"><template #default="{ row }"><el-tag size="small" effect="plain" :type="healthTag(row.health_state)">{{ healthLabel(row.health_state) }}</el-tag></template></el-table-column>
        <el-table-column label="库存进度" min-width="150"><template #default="{ row }"><strong>seq {{ row.last_sequence }}</strong><span class="secondary">观测 rev {{ row.observed_revision }}</span></template></el-table-column>
        <el-table-column label="租约到期" width="180"><template #default="{ row }">{{ formatTime(row.lease_expires_at) }}</template></el-table-column>
        <el-table-column label="最后上报" width="180"><template #default="{ row }">{{ formatTime(row.last_received_at) }}</template></el-table-column>
      </el-table>

      <el-empty v-if="!loading && !errorMessage && items.length === 0" description="当前资源方没有符合条件的技术资源" />
      <div class="pagination"><el-pagination v-model:current-page="pagination.page" v-model:page-size="pagination.size" :total="pagination.total" :page-sizes="[20, 50, 100]" layout="total, sizes, prev, pager, next" @size-change="load" @current-change="load" /></div>
    </section>
  </div>
</template>

<script setup lang="ts">
import { onMounted, reactive, ref, watch } from 'vue'
import { Refresh, Search } from '@element-plus/icons-vue'
import PageHeader from '@/components/Common/PageHeader.vue'
import { getProviderTechnicalResources, type TechnicalResource, type TechnicalResourceState } from '@/api/providerSupply'
import { useWorkspaceStore } from '@/stores/workspace'

const workspaceStore = useWorkspaceStore()
const loading = ref(false)
const errorMessage = ref('')
const items = ref<TechnicalResource[]>([])
const filters = reactive({ search: '', type: '', state: '' })
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
    const response = await getProviderTechnicalResources(providerId, {
      search: filters.search.trim() || undefined,
      type: filters.type || undefined,
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
const typeLabel = (type: string) => type === 'agent' ? 'Agent' : type === 'endpoint' ? 'Endpoint' : type
const lifecycleLabel = (state: TechnicalResourceState) => ({ pending: '待注册', registered: '已注册', disabled: '已禁用', retired: '已退役' }[state])
const lifecycleTag = (state: TechnicalResourceState) => ({ pending: 'warning', registered: 'success', disabled: 'info', retired: 'info' }[state] as any)
const healthLabel = (state: string) => ({ unknown: '未知', online: '在线', degraded: '异常', offline: '离线' }[state] || state)
const healthTag = (state: string) => ({ online: 'success', degraded: 'warning', offline: 'danger', unknown: 'info' }[state] || 'info') as any
const formatTime = (value?: string) => value ? new Date(value).toLocaleString('zh-CN', { hour12: false }) : '-'

watch(() => workspaceStore.providerId, () => { pagination.page = 1; load() })
onMounted(load)
</script>

<style scoped>
.provider-page { width: 100%; }
.state-alert { margin-bottom: 14px; }
.data-surface { overflow: hidden; border: 1px solid var(--border-light); border-radius: 6px; background: #fff; }
.toolbar { display: flex; align-items: center; gap: 10px; padding: 14px 16px; border-bottom: 1px solid var(--border-light); }
.search-input { width: 300px; }
.filter-select { width: 160px; }
.result-count { margin-left: auto; color: var(--text-secondary); font-size: 12px; }
.secondary { display: block; margin-top: 3px; color: var(--text-secondary); font-size: 12px; }
.mono { font-family: 'SFMono-Regular', Consolas, 'Liberation Mono', monospace; }
.pagination { display: flex; justify-content: flex-end; padding: 16px; }
</style>
