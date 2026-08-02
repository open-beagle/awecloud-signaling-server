<template>
  <div class="provider-page">
    <PageHeader title="供给候选" description="查看当前资源方可信发现的 Host 与 Kubernetes 候选，以及身份质量和审核状态。">
      <template #actions>
        <el-button :icon="Refresh" :loading="loading" @click="load">刷新</el-button>
      </template>
    </PageHeader>

    <el-alert
      v-if="errorMessage"
      class="state-alert"
      title="供给候选加载失败"
      :description="errorMessage"
      type="error"
      show-icon
      :closable="false"
    >
      <template #default><el-button link type="primary" @click="load">重新加载</el-button></template>
    </el-alert>

    <section class="data-surface">
      <div class="toolbar">
        <el-input v-model="filters.search" class="search-input" clearable :prefix-icon="Search" placeholder="搜索稳定标识或冲突码" @keyup.enter="applyFilters" @clear="applyFilters" />
        <el-select v-model="filters.type" class="filter-select" clearable placeholder="全部资源类型" @change="applyFilters">
          <el-option label="Kubernetes" value="kubernetes" />
          <el-option label="Host" value="host" />
        </el-select>
        <el-select v-model="filters.state" class="filter-select" clearable placeholder="全部审核状态" @change="applyFilters">
          <el-option v-for="option in stateOptions" :key="option.value" :label="option.label" :value="option.value" />
        </el-select>
        <span class="result-count">{{ pagination.total }} 项候选</span>
      </div>

      <el-table v-loading="loading" :data="items" stripe>
        <el-table-column label="候选资源" min-width="260">
          <template #default="{ row }">
            <strong>{{ row.stable_key }}</strong>
            <span class="secondary mono">{{ row.id }}</span>
          </template>
        </el-table-column>
        <el-table-column label="类型" width="120"><template #default="{ row }">{{ resourceTypeLabel(row.resource_type) }}</template></el-table-column>
        <el-table-column label="身份质量" width="130"><template #default="{ row }"><el-tag size="small" effect="plain" :type="identityTag(row.identity_quality)">{{ identityLabel(row.identity_quality) }}</el-tag></template></el-table-column>
        <el-table-column label="审核状态" width="130"><template #default="{ row }"><el-tag size="small" :type="reviewTag(row.review_state)">{{ reviewLabel(row.review_state) }}</el-tag></template></el-table-column>
        <el-table-column label="冲突" min-width="170"><template #default="{ row }"><span v-if="row.conflict_code" class="danger-copy">{{ row.conflict_code }}</span><span v-else class="secondary">无</span></template></el-table-column>
        <el-table-column label="最后发现" width="180"><template #default="{ row }">{{ formatTime(row.last_observed_at) }}</template></el-table-column>
        <el-table-column label="租约到期" width="180"><template #default="{ row }">{{ formatTime(row.lease_expires_at) }}</template></el-table-column>
      </el-table>

      <el-empty v-if="!loading && !errorMessage && items.length === 0" description="当前资源方没有符合条件的供给候选" />
      <div class="pagination"><el-pagination v-model:current-page="pagination.page" v-model:page-size="pagination.size" :total="pagination.total" :page-sizes="[20, 50, 100]" layout="total, sizes, prev, pager, next" @size-change="load" @current-change="load" /></div>
    </section>
  </div>
</template>

<script setup lang="ts">
import { onMounted, reactive, ref, watch } from 'vue'
import { Refresh, Search } from '@element-plus/icons-vue'
import PageHeader from '@/components/Common/PageHeader.vue'
import { getProviderSupplyCandidates, type IdentityQuality, type SupplyCandidate, type SupplyCandidateState } from '@/api/providerSupply'
import { useWorkspaceStore } from '@/stores/workspace'

const workspaceStore = useWorkspaceStore()
const loading = ref(false)
const errorMessage = ref('')
const items = ref<SupplyCandidate[]>([])
const filters = reactive({ search: '', type: '', state: '' })
const pagination = reactive({ page: 1, size: 20, total: 0 })
const stateOptions: Array<{ label: string; value: SupplyCandidateState }> = [
  { label: '已发现', value: 'observed' },
  { label: '待审核', value: 'pending_review' },
  { label: '已接受', value: 'accepted' },
  { label: '已关联', value: 'linked' },
  { label: '冲突', value: 'conflict' },
  { label: '已拒绝', value: 'rejected' },
]

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
    const response = await getProviderSupplyCandidates(providerId, {
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
const resourceTypeLabel = (type: string) => type === 'kubernetes' ? 'Kubernetes' : type === 'host' ? 'Host' : type
const identityLabel = (quality: IdentityQuality) => ({ strong: '强身份', insufficient: '证据不足', collision: '身份冲突' }[quality])
const identityTag = (quality: IdentityQuality) => ({ strong: 'success', insufficient: 'warning', collision: 'danger' }[quality] as any)
const reviewLabel = (state: SupplyCandidateState) => stateOptions.find(option => option.value === state)?.label || state
const reviewTag = (state: SupplyCandidateState) => ({ observed: 'info', pending_review: 'warning', accepted: 'success', linked: 'success', conflict: 'danger', rejected: 'info' }[state] as any)
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
.filter-select { width: 170px; }
.result-count { margin-left: auto; color: var(--text-secondary); font-size: 12px; }
.secondary { display: block; margin-top: 3px; color: var(--text-secondary); font-size: 12px; }
.mono { font-family: 'SFMono-Regular', Consolas, 'Liberation Mono', monospace; }
.danger-copy { color: var(--danger-color); font-size: 12px; font-weight: 600; }
.pagination { display: flex; justify-content: flex-end; padding: 16px; }
</style>
