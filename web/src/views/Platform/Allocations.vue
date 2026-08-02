<template>
  <div class="platform-page">
    <PageHeader title="资源分配" description="跨租户查看 Namespace Scope 的分配状态、有效窗口和对象 revision。当前页面只读。">
      <template #actions><el-button :icon="Refresh" :loading="loading" @click="load">刷新</el-button></template>
    </PageHeader>

    <el-alert v-if="errorMessage" class="state-alert" title="资源分配加载失败" :description="errorMessage" type="error" show-icon :closable="false" />

    <section class="data-surface">
      <div class="toolbar">
        <el-input v-model="filters.search" class="search-input" clearable :prefix-icon="Search" placeholder="搜索 Allocation、合同或租户" @keyup.enter="applyFilters" @clear="applyFilters" />
        <el-select v-model="filters.state" class="filter-select" clearable placeholder="全部状态" @change="applyFilters">
          <el-option v-for="option in stateOptions" :key="option.value" :label="option.label" :value="option.value" />
        </el-select>
        <el-select v-model="filters.mode" class="filter-select" clearable placeholder="全部方式" @change="applyFilters">
          <el-option v-for="option in modeOptions" :key="option.value" :label="option.label" :value="option.value" />
        </el-select>
        <span class="result-count">{{ pagination.total }} 项分配</span>
      </div>

      <el-table v-loading="loading" :data="items" stripe>
        <el-table-column label="Allocation" min-width="250">
          <template #default="{ row }"><router-link class="allocation-link mono" :to="`/platform-allocations/${row.id}`">{{ row.id }}</router-link><span class="secondary">{{ row.contract_ref || '无合同引用' }}</span></template>
        </el-table-column>
        <el-table-column label="Tenant" min-width="220"><template #default="{ row }"><span class="mono">{{ row.tenant_id }}</span></template></el-table-column>
        <el-table-column label="Namespace Scope" min-width="220"><template #default="{ row }"><span class="mono">{{ scopeIds(row) }}</span><span class="secondary">{{ row.items.length }} 个分配项</span></template></el-table-column>
        <el-table-column label="方式" width="110"><template #default="{ row }">{{ modeLabel(row.mode) }}</template></el-table-column>
        <el-table-column label="状态" width="110"><template #default="{ row }"><el-tag size="small" :type="stateTag(row.state)">{{ stateLabel(row.state) }}</el-tag></template></el-table-column>
        <el-table-column label="有效窗口" width="210"><template #default="{ row }">{{ formatTime(row.valid_from) }}<span class="secondary">至 {{ formatTime(row.expires_at) }}</span></template></el-table-column>
        <el-table-column label="Revision" width="100" align="center"><template #default="{ row }">{{ row.row_version }}</template></el-table-column>
      </el-table>

      <el-empty v-if="!loading && !errorMessage && items.length === 0" description="当前筛选条件下没有资源分配" />
      <div class="pagination"><el-pagination v-model:current-page="pagination.page" v-model:page-size="pagination.size" :total="pagination.total" :page-sizes="[20, 50, 100]" layout="total, sizes, prev, pager, next" @size-change="load" @current-change="load" /></div>
    </section>
  </div>
</template>

<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { Refresh, Search } from '@element-plus/icons-vue'
import PageHeader from '@/components/Common/PageHeader.vue'
import { getPlatformAllocations, type ResourceAllocation, type ResourceAllocationMode, type ResourceAllocationState } from '@/api/platformAllocations'

const loading = ref(false)
const errorMessage = ref('')
const items = ref<ResourceAllocation[]>([])
const filters = reactive({ search: '', state: '', mode: '' })
const pagination = reactive({ page: 1, size: 20, total: 0 })
const stateOptions = [
  { label: '草稿', value: 'draft' }, { label: '待生效', value: 'scheduled' }, { label: '生效中', value: 'active' },
  { label: '已暂停', value: 'suspended' }, { label: '已到期', value: 'expired' }, { label: '已撤销', value: 'revoked' },
]
const modeOptions = [{ label: '独占分配', value: 'assigned' }, { label: '租约分配', value: 'leased' }, { label: '共享分配', value: 'shared' }]

const load = async () => {
  loading.value = true
  errorMessage.value = ''
  try {
    const response = await getPlatformAllocations({
      search: filters.search.trim() || undefined,
      state: filters.state || undefined,
      mode: filters.mode || undefined,
      page: pagination.page,
      size: pagination.size,
    })
    items.value = response.success && response.data ? response.data : []
    pagination.total = response.total || 0
  } catch {
    items.value = []
    pagination.total = 0
    errorMessage.value = '请确认平台资源分配读取权限和服务状态后重试。'
  } finally {
    loading.value = false
  }
}

const applyFilters = () => { pagination.page = 1; load() }
const scopeIds = (item: ResourceAllocation) => item.items.map(entry => entry.scope_id).join('、') || '-'
const modeLabel = (mode: ResourceAllocationMode) => ({ assigned: '独占', leased: '租约', shared: '共享' }[mode])
const stateLabel = (state: ResourceAllocationState) => ({ draft: '草稿', scheduled: '待生效', active: '生效中', suspended: '已暂停', expired: '已到期', revoked: '已撤销' }[state])
const stateTag = (state: ResourceAllocationState) => ({ draft: 'info', scheduled: 'warning', active: 'success', suspended: 'warning', expired: 'info', revoked: 'danger' }[state] as any)
const formatTime = (value?: string) => value ? new Date(value).toLocaleString('zh-CN', { hour12: false }) : '长期'

onMounted(load)
</script>

<style scoped>
.platform-page { width: 100%; }
.state-alert { margin-bottom: 14px; }
.data-surface { overflow: hidden; border: 1px solid var(--border-light); border-radius: 6px; background: #fff; }
.toolbar { display: flex; align-items: center; gap: 10px; padding: 14px 16px; border-bottom: 1px solid var(--border-light); }
.search-input { width: 320px; }
.filter-select { width: 150px; }
.result-count { margin-left: auto; color: var(--text-secondary); font-size: 12px; }
.allocation-link { color: var(--primary-color); font-weight: 600; text-decoration: none; }
.allocation-link:hover { text-decoration: underline; }
.secondary { display: block; margin-top: 3px; color: var(--text-secondary); font-size: 12px; }
.mono { font-family: 'SFMono-Regular', Consolas, 'Liberation Mono', monospace; font-size: 12px; }
.pagination { display: flex; justify-content: flex-end; padding: 16px; }
</style>
