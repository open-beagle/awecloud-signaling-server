<template>
  <div class="provider-page">
    <PageHeader title="审计日志" description="仅展示当前资源方上下文中的管理操作，并保留真实操作者、生效身份和权限版本。">
      <template #actions><el-button :icon="Refresh" :loading="loading" @click="load">刷新</el-button></template>
    </PageHeader>

    <el-alert v-if="errorMessage" class="state-alert" title="审计日志加载失败" :description="errorMessage" type="error" show-icon :closable="false">
      <template #default><el-button link type="primary" @click="load">重新加载</el-button></template>
    </el-alert>

    <section class="data-surface">
      <div class="toolbar">
        <el-input v-model="filters.search" class="search-input" clearable :prefix-icon="Search" placeholder="搜索操作者、目标或 Request ID" @keyup.enter="applyFilters" @clear="applyFilters" />
        <el-input v-model="filters.actionType" class="action-input" clearable placeholder="操作类型" @keyup.enter="applyFilters" @clear="applyFilters" />
        <span class="result-count">{{ pagination.total }} 条审计记录</span>
      </div>

      <el-table v-loading="loading" :data="items" stripe>
        <el-table-column label="时间" width="180"><template #default="{ row }">{{ formatTime(row.created_at) }}</template></el-table-column>
        <el-table-column label="真实操作者" min-width="190"><template #default="{ row }"><strong>{{ row.actor_username || `User #${row.actor_user_id}` }}</strong><span class="secondary">实际身份 User #{{ row.effective_user_id }}</span></template></el-table-column>
        <el-table-column prop="action_type" label="操作" min-width="190" show-overflow-tooltip />
        <el-table-column label="目标" min-width="220"><template #default="{ row }"><span>{{ row.target_name || row.target_id }}</span><span class="secondary">{{ row.target_type }} · {{ row.target_id }}</span></template></el-table-column>
        <el-table-column prop="required_permission" label="通过权限" min-width="220" show-overflow-tooltip />
        <el-table-column label="权限版本" width="105"><template #default="{ row }">r{{ row.permission_revision }}</template></el-table-column>
        <el-table-column prop="request_id" label="Request ID" min-width="220" show-overflow-tooltip />
      </el-table>

      <el-empty v-if="!loading && !errorMessage && items.length === 0" description="当前资源方还没有符合条件的审计记录" />
      <div class="pagination"><el-pagination v-model:current-page="pagination.page" v-model:page-size="pagination.size" :total="pagination.total" :page-sizes="[20, 50, 100]" layout="total, sizes, prev, pager, next" @size-change="load" @current-change="load" /></div>
    </section>
  </div>
</template>

<script setup lang="ts">
import { onMounted, reactive, ref, watch } from 'vue'
import { Refresh, Search } from '@element-plus/icons-vue'
import PageHeader from '@/components/Common/PageHeader.vue'
import { getProviderAuditLogs, type ProviderAuditLog } from '@/api/providerSupply'
import { useWorkspaceStore } from '@/stores/workspace'

const workspaceStore = useWorkspaceStore()
const loading = ref(false)
const errorMessage = ref('')
const items = ref<ProviderAuditLog[]>([])
const filters = reactive({ search: '', actionType: '' })
const pagination = reactive({ page: 1, size: 20, total: 0 })

const load = async () => {
  const providerId = workspaceStore.providerId
  if (!providerId) { items.value = []; pagination.total = 0; errorMessage.value = '当前没有有效的资源方上下文。'; return }
  loading.value = true
  errorMessage.value = ''
  try {
    const response = await getProviderAuditLogs(providerId, {
      search: filters.search.trim() || undefined, action_type: filters.actionType.trim() || undefined,
      page: pagination.page, size: pagination.size,
    })
    items.value = response.success && response.data ? response.data : []
    pagination.total = response.total || 0
  } catch {
    items.value = []; pagination.total = 0; errorMessage.value = '请确认当前资源方权限和服务状态后重试。'
  } finally { loading.value = false }
}

const applyFilters = () => { pagination.page = 1; load() }
const formatTime = (value: string) => new Date(value).toLocaleString('zh-CN', { hour12: false })
watch(() => workspaceStore.providerId, () => { filters.search = ''; filters.actionType = ''; pagination.page = 1; load() })
onMounted(load)
</script>

<style scoped>
.provider-page { width: 100%; }
.state-alert { margin-bottom: 14px; }
.data-surface { overflow: hidden; border: 1px solid var(--border-light); border-radius: 6px; background: #fff; }
.toolbar { display: flex; align-items: center; gap: 10px; padding: 14px 16px; border-bottom: 1px solid var(--border-light); }
.search-input { width: 340px; }
.action-input { width: 220px; }
.result-count { margin-left: auto; color: var(--text-secondary); font-size: 12px; }
.secondary { display: block; margin-top: 3px; color: var(--text-secondary); font-size: 12px; }
.pagination { display: flex; justify-content: flex-end; padding: 16px; }
</style>
