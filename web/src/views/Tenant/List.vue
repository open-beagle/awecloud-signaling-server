<template>
  <div class="tenant-page">
    <div class="page-header">
      <div>
        <div class="eyebrow">租户安全边界</div>
        <h1>租户管理</h1>
        <p>建立和查看租户安全边界。业务成员统一在进入租户后管理，管理台授权由“租户管理员授权”维护。</p>
      </div>
      <div class="header-actions">
        <el-button :icon="Refresh" :loading="loading" @click="fetchTenants">刷新</el-button>
        <el-button type="primary" :icon="Plus" :disabled="!authStore.isPlatformAdmin" @click="showCreate = true">创建租户</el-button>
      </div>
    </div>

    <div class="tenant-surface">
      <div class="toolbar">
        <el-input v-model="search" clearable :prefix-icon="Search" placeholder="搜索租户名称或标识" @keyup.enter="handleSearch" />
        <span>{{ pagination.total }} 个租户</span>
      </div>
      <el-table v-loading="loading" :data="tenants" stripe>
        <el-table-column label="租户" min-width="260">
          <template #default="{ row }"><strong>{{ row.name }}</strong><span class="secondary">{{ row.key }}</span></template>
        </el-table-column>
        <el-table-column label="状态" width="120"><template #default="{ row }"><el-tag size="small" :type="row.status === 'active' ? 'success' : 'warning'">{{ row.status === 'active' ? '正常' : '已暂停' }}</el-tag></template></el-table-column>
        <el-table-column label="成员" width="110" align="center"><template #default="{ row }"><strong>{{ row.member_count || 0 }}</strong></template></el-table-column>
        <el-table-column label="资源" width="110" align="center"><template #default="{ row }"><strong>{{ row.resource_count || 0 }}</strong></template></el-table-column>
        <el-table-column label="创建时间" width="180"><template #default="{ row }">{{ formatTime(row.created_at) }}</template></el-table-column>
        <el-table-column label="操作" width="180" fixed="right" align="right">
          <template #default="{ row }">
            <el-button v-if="canEnterTenant(row.id)" link type="primary" @click="enterTenant(row)">进入租户</el-button>
            <el-button v-else link type="primary" @click="router.push('/tenant-admin-memberships')">配置管理授权</el-button>
          </template>
        </el-table-column>
      </el-table>
      <el-empty v-if="!loading && !tenants.length" description="还没有租户。创建第一个租户后即可登记统一资源。" />
      <div class="pagination"><el-pagination v-model:current-page="pagination.page" v-model:page-size="pagination.size" layout="total, prev, pager, next" :total="pagination.total" @current-change="fetchTenants" /></div>
    </div>

    <el-dialog v-model="showCreate" title="创建租户" width="480px">
      <el-form label-position="top">
        <el-form-item label="租户名称" required><el-input v-model="createForm.name" placeholder="例如：深圳智翼" /></el-form-item>
        <el-form-item label="稳定标识" required><el-input v-model="createForm.key" placeholder="例如：shenzhen-zhiyi" /><div class="form-hint">创建后用于业务绑定，不使用显示名称作为安全主键。</div></el-form-item>
      </el-form>
      <template #footer><el-button @click="showCreate = false">取消</el-button><el-button type="primary" :loading="creating" @click="handleCreate">创建</el-button></template>
    </el-dialog>

  </div>
</template>

<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import { Plus, Refresh, Search } from '@element-plus/icons-vue'
import { createTenant, getTenants, type Tenant } from '@/api/resource'
import { useAuthStore } from '@/stores/auth'
import { useTenantStore } from '@/stores/tenant'

const router = useRouter()
const authStore = useAuthStore()
const tenantStore = useTenantStore()
const loading = ref(false)
const creating = ref(false)
const showCreate = ref(false)
const search = ref('')
const tenants = ref<Tenant[]>([])
const pagination = reactive({ page: 1, size: 20, total: 0 })
const createForm = reactive({ name: '', key: '' })

const fetchTenants = async () => {
  loading.value = true
  try {
    const res = await getTenants({ search: search.value || undefined, page: pagination.page, size: pagination.size })
    if (res.success && res.data) { tenants.value = res.data; pagination.total = res.total }
  } finally { loading.value = false }
}
const handleSearch = () => { pagination.page = 1; fetchTenants() }
const handleCreate = async () => {
  if (!createForm.name.trim() || !createForm.key.trim()) return ElMessage.warning('请输入租户名称和稳定标识')
  creating.value = true
  try {
    const res = await createTenant({ name: createForm.name.trim(), key: createForm.key.trim() })
    if (res.success && res.data) {
      ElMessage.success('租户已创建')
      showCreate.value = false
      createForm.name = ''; createForm.key = ''
      window.dispatchEvent(new Event('tenant-catalog-changed'))
      await fetchTenants()
    }
  } finally { creating.value = false }
}
const canEnterTenant = (tenantId: string) => tenantStore.contexts.some(context => context.tenant_id === tenantId)
const enterTenant = async (tenant: Tenant) => {
  const changed = await tenantStore.selectTenant(tenant.id)
  if (!changed) return ElMessage.warning('当前账号没有该租户的管理授权')
  await router.push('/tenant-overview')
}
const formatTime = (value: string) => new Date(value).toLocaleString('zh-CN', { hour12: false })
onMounted(async () => { await Promise.all([fetchTenants(), tenantStore.loadContexts()]) })
</script>

<style scoped>
.tenant-page { width: 100%; }
.page-header, .header-actions, .toolbar { display: flex; align-items: center; }
.page-header { justify-content: space-between; align-items: flex-start; gap: 24px; margin-bottom: 18px; }
.header-actions { gap: 8px; }
.eyebrow, .secondary, .toolbar span, .form-hint { color: var(--text-secondary); font-size: 12px; }
h1 { margin: 0; color: var(--text-primary); font-size: 24px; line-height: 32px; }
.page-header p { margin: 5px 0 0; color: var(--text-regular); font-size: 13px; }
.tenant-surface { overflow: hidden; background: #fff; border: 1px solid var(--border-light); border-radius: 6px; }
.toolbar { justify-content: space-between; gap: 16px; padding: 14px 16px; border-bottom: 1px solid var(--border-light); }
.toolbar .el-input { width: 340px; }
.secondary { display: block; margin-top: 3px; }
.pagination { display: flex; justify-content: flex-end; padding: 16px; }
.form-hint { margin-top: 6px; line-height: 18px; }
@media (max-width: 700px) { .page-header { flex-direction: column; } .toolbar .el-input { width: 100%; } }
</style>
