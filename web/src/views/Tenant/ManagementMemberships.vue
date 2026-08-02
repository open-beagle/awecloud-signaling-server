<template>
  <div class="tenant-page">
    <PageHeader title="管理员" description="查看当前租户的管理成员、职责、生效区间和权限版本。管理成员与 Desktop 业务成员相互独立。">
      <template #actions><el-button :icon="Refresh" :loading="loading" @click="load">刷新</el-button></template>
    </PageHeader>

    <el-alert v-if="errorMessage" class="state-alert" title="管理员加载失败" :description="errorMessage" type="error" show-icon :closable="false">
      <template #default><el-button link type="primary" @click="load">重新加载</el-button></template>
    </el-alert>

    <section class="data-surface">
      <div class="toolbar">
        <el-input v-model="filters.search" class="search-input" clearable :prefix-icon="Search" placeholder="搜索账号或显示名称" @keyup.enter="applyFilters" @clear="applyFilters" />
        <el-select v-model="filters.role" class="filter-select" clearable placeholder="全部职责" @change="applyFilters">
          <el-option label="租户管理员" value="tenant_admin" />
          <el-option label="安全审计员" value="security_auditor" />
          <el-option label="租户观察员" value="tenant_viewer" />
        </el-select>
        <el-select v-model="filters.state" class="filter-select" clearable placeholder="全部状态" @change="applyFilters">
          <el-option label="生效中" value="active" />
          <el-option label="待生效" value="scheduled" />
          <el-option label="已过期" value="expired" />
          <el-option label="已停用" value="disabled" />
        </el-select>
        <span class="result-count">{{ pagination.total }} 个管理成员</span>
      </div>

      <el-table v-loading="loading" :data="items" stripe>
        <el-table-column label="管理成员" min-width="230">
          <template #default="{ row }"><strong>{{ row.display_name || row.username }}</strong><span class="secondary">{{ row.username }} · User #{{ row.user_id }}</span></template>
        </el-table-column>
        <el-table-column label="职责" width="140"><template #default="{ row }">{{ roleLabel(row.role) }}</template></el-table-column>
        <el-table-column label="状态" width="110"><template #default="{ row }"><el-tag size="small" :type="stateTag(row)">{{ stateLabel(row) }}</el-tag></template></el-table-column>
        <el-table-column label="生效时间" width="180"><template #default="{ row }">{{ formatTime(row.valid_from) }}</template></el-table-column>
        <el-table-column label="到期时间" width="180"><template #default="{ row }">{{ formatTime(row.expires_at) }}</template></el-table-column>
        <el-table-column label="权限版本" width="105"><template #default="{ row }">r{{ row.permission_revision }}</template></el-table-column>
        <el-table-column prop="reason" label="授权原因" min-width="220" show-overflow-tooltip />
      </el-table>

      <el-empty v-if="!loading && !errorMessage && items.length === 0" description="当前租户没有符合条件的管理成员" />
      <div class="pagination"><el-pagination v-model:current-page="pagination.page" v-model:page-size="pagination.size" :total="pagination.total" :page-sizes="[20, 50, 100]" layout="total, sizes, prev, pager, next" @size-change="load" @current-change="load" /></div>
    </section>
  </div>
</template>

<script setup lang="ts">
import { onMounted, reactive, ref, watch } from 'vue'
import { Refresh, Search } from '@element-plus/icons-vue'
import PageHeader from '@/components/Common/PageHeader.vue'
import { getTenantManagementMemberships, type TenantManagementMembership, type TenantManagementRole } from '@/api/tenantBusiness'
import { useTenantStore } from '@/stores/tenant'

const tenantStore = useTenantStore()
const loading = ref(false)
const errorMessage = ref('')
const items = ref<TenantManagementMembership[]>([])
const filters = reactive({ search: '', role: '', state: '' })
const pagination = reactive({ page: 1, size: 20, total: 0 })

const load = async () => {
  const tenantId = tenantStore.tenantId
  if (!tenantId) { items.value = []; pagination.total = 0; errorMessage.value = '当前没有有效的租户上下文。'; return }
  loading.value = true
  errorMessage.value = ''
  try {
    const response = await getTenantManagementMemberships(tenantId, {
      search: filters.search.trim() || undefined, role: filters.role || undefined,
      state: filters.state || undefined, page: pagination.page, size: pagination.size,
    })
    items.value = response.success && response.data ? response.data : []
    pagination.total = response.total || 0
  } catch {
    items.value = []; pagination.total = 0; errorMessage.value = '请确认当前租户权限和服务状态后重试。'
  } finally { loading.value = false }
}

const applyFilters = () => { pagination.page = 1; load() }
const roleLabel = (role: TenantManagementRole) => ({ tenant_admin: '租户管理员', security_auditor: '安全审计员', tenant_viewer: '租户观察员' }[role])
const membershipState = (item: TenantManagementMembership) => {
  const now = Date.now()
  if (!item.enabled || !item.user_enabled) return 'disabled'
  if (new Date(item.valid_from).getTime() > now) return 'scheduled'
  if (item.expires_at && new Date(item.expires_at).getTime() <= now) return 'expired'
  return 'active'
}
const stateLabel = (item: TenantManagementMembership) => ({ active: '生效中', scheduled: '待生效', expired: '已过期', disabled: '已停用' }[membershipState(item)])
const stateTag = (item: TenantManagementMembership) => ({ active: 'success', scheduled: 'warning', expired: 'info', disabled: 'danger' }[membershipState(item)] as any)
const formatTime = (value?: string) => value ? new Date(value).toLocaleString('zh-CN', { hour12: false }) : '长期有效'

watch(() => tenantStore.contextRevision, () => { filters.search = ''; filters.role = ''; filters.state = ''; pagination.page = 1; load() })
onMounted(load)
</script>

<style scoped>
.tenant-page { width: 100%; }
.state-alert { margin-bottom: 14px; }
.data-surface { overflow: hidden; border: 1px solid var(--border-light); border-radius: 6px; background: #fff; }
.toolbar { display: flex; align-items: center; gap: 10px; padding: 14px 16px; border-bottom: 1px solid var(--border-light); }
.search-input { width: 300px; }
.filter-select { width: 160px; }
.result-count { margin-left: auto; color: var(--text-secondary); font-size: 12px; }
.secondary { display: block; margin-top: 3px; color: var(--text-secondary); font-size: 12px; }
.pagination { display: flex; justify-content: flex-end; padding: 16px; }
</style>
