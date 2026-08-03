<template>
  <div class="page-container membership-page">
    <div class="page-header">
      <div>
        <h1>租户管理员授权</h1>
        <p>授权平台管理账号进入指定租户的管理空间。这里不会创建租户成员，也不会授予 Desktop 资源访问。</p>
      </div>
      <div class="header-actions">
        <el-button :icon="Refresh" :loading="loading" @click="load">刷新</el-button>
        <el-button v-if="canWrite" type="primary" :icon="Plus" @click="openCreate">新增授权</el-button>
      </div>
    </div>

    <section class="list-surface">
      <div class="toolbar">
        <el-input v-model="filters.search" clearable :prefix-icon="Search" placeholder="搜索管理账号或租户" @keyup.enter="search" />
        <el-select v-model="filters.role" clearable placeholder="全部管理角色" @change="search">
          <el-option v-for="option in roleOptions" :key="option.value" :label="option.label" :value="option.value" />
        </el-select>
        <span>{{ pagination.total }} 条授权</span>
      </div>
      <el-table v-loading="loading" :data="items" stripe>
        <el-table-column label="管理账号" min-width="190">
          <template #default="{ row }"><strong>{{ row.admin_username }}</strong><span class="secondary">Admin #{{ row.admin_id }}</span></template>
        </el-table-column>
        <el-table-column label="租户" min-width="220">
          <template #default="{ row }"><strong>{{ row.tenant_name }}</strong><span class="secondary">{{ row.tenant_key }}</span></template>
        </el-table-column>
        <el-table-column label="管理角色" width="150"><template #default="{ row }">{{ roleLabel(row.role) }}</template></el-table-column>
        <el-table-column label="有效期" width="185"><template #default="{ row }">{{ row.expires_at ? formatTime(row.expires_at) : '长期有效' }}</template></el-table-column>
        <el-table-column label="状态" width="125">
          <template #default="{ row }"><el-tag size="small" :type="statusType(row)">{{ statusLabel(row) }}</el-tag></template>
        </el-table-column>
        <el-table-column label="权限版本" width="100" align="center"><template #default="{ row }">r{{ row.permission_revision }}</template></el-table-column>
        <el-table-column label="操作" width="90" fixed="right" align="right">
          <template #default="{ row }"><el-button v-if="canWrite" link type="primary" @click="openEdit(row)">编辑</el-button><span v-else class="secondary">只读</span></template>
        </el-table-column>
      </el-table>
      <el-empty v-if="!loading && !items.length" description="暂无租户管理员授权" />
      <div class="pagination"><el-pagination v-model:current-page="pagination.page" layout="total, prev, pager, next" :total="pagination.total" :page-size="pagination.size" @current-change="load" /></div>
    </section>

    <el-dialog v-model="dialogVisible" :title="editing ? '编辑租户管理员授权' : '新增租户管理员授权'" width="540px">
      <el-alert title="管理授权与租户成员是两套独立关系，不会产生业务成员或资源访问权限。" type="info" show-icon :closable="false" />
      <el-form label-position="top" class="membership-form">
        <el-form-item label="平台管理账号" required>
          <el-select v-model="form.admin_id" filterable :disabled="Boolean(editing)" style="width: 100%" placeholder="选择管理账号">
            <el-option v-for="admin in activeAdminOptions" :key="admin.id" :value="admin.id" :label="`${admin.username} · ${platformRoleLabel(admin.role)}`" />
          </el-select>
        </el-form-item>
        <el-form-item label="租户" required>
          <el-select v-model="form.tenant_id" filterable :disabled="Boolean(editing)" style="width: 100%" placeholder="选择租户">
            <el-option v-for="tenant in tenants" :key="tenant.id" :value="tenant.id" :label="`${tenant.name} (${tenant.key})`" />
          </el-select>
        </el-form-item>
        <el-form-item label="租户管理角色" required>
          <el-select v-model="form.role" style="width: 100%"><el-option v-for="option in roleOptions" :key="option.value" :label="option.label" :value="option.value" /></el-select>
          <div class="role-hint">{{ roleDescription(form.role) }}</div>
        </el-form-item>
        <el-form-item label="授权有效期">
          <el-date-picker v-model="form.expires_at" type="datetime" value-format="YYYY-MM-DDTHH:mm:ssZ" clearable style="width: 100%" placeholder="不填写表示长期有效" />
        </el-form-item>
        <el-form-item label="授权状态"><el-switch v-model="form.enabled" active-text="启用" inactive-text="停用" /></el-form-item>
      </el-form>
      <template #footer><el-button @click="dialogVisible = false">取消</el-button><el-button type="primary" :loading="saving" @click="save">保存授权</el-button></template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { Plus, Refresh, Search } from '@element-plus/icons-vue'
import { getTenants, type Tenant } from '@/api/resource'
import {
  createTenantAdminMembership, getTenantAdminMemberships, getTenantAdminOptions, updateTenantAdminMembership,
  type TenantAdminMembership, type TenantAdminMembershipInput, type TenantAdminOption
} from '@/api/tenantManagement'
import { useWorkspaceStore } from '@/stores/workspace'

const workspaceStore = useWorkspaceStore()
const canWrite = computed(() => workspaceStore.can('platform.memberships.write'))
const loading = ref(false)
const saving = ref(false)
const dialogVisible = ref(false)
const editing = ref<TenantAdminMembership>()
const items = ref<TenantAdminMembership[]>([])
const admins = ref<TenantAdminOption[]>([])
const tenants = ref<Tenant[]>([])
const filters = reactive({ search: '', role: '' })
const pagination = reactive({ page: 1, size: 20, total: 0 })
const form = reactive<TenantAdminMembershipInput>({ admin_id: undefined, tenant_id: '', role: 'tenant_viewer', enabled: true, expires_at: null })
const roleOptions = [
  { label: '租户管理员', value: 'tenant_admin' },
  { label: '安全审计员', value: 'security_auditor' },
  { label: '租户观察员', value: 'tenant_viewer' }
] as const
const activeAdminOptions = computed(() => admins.value.filter(admin => admin.enabled || admin.id === editing.value?.admin_id))

const load = async () => {
  loading.value = true
  try {
    const response = await getTenantAdminMemberships({ search: filters.search || undefined, role: filters.role || undefined, page: pagination.page, size: pagination.size })
    items.value = response.success && response.data ? response.data : []
    pagination.total = response.total || 0
  } finally { loading.value = false }
}
const loadOptions = async () => {
  const [adminResponse, tenantResponse] = await Promise.all([getTenantAdminOptions(), getTenants({ page: 1, size: 100 })])
  admins.value = adminResponse.success && adminResponse.data ? adminResponse.data : []
  tenants.value = tenantResponse.success && tenantResponse.data ? tenantResponse.data : []
}
const search = () => { pagination.page = 1; load() }
const resetForm = () => Object.assign(form, { admin_id: undefined, tenant_id: '', role: 'tenant_viewer', enabled: true, expires_at: null })
const openCreate = async () => { if (!canWrite.value) return; editing.value = undefined; resetForm(); await loadOptions(); dialogVisible.value = true }
const openEdit = async (row: TenantAdminMembership) => {
  if (!canWrite.value) return
  editing.value = row
  await loadOptions()
  Object.assign(form, { admin_id: row.admin_id, tenant_id: row.tenant_id, role: row.role, enabled: row.enabled, expires_at: row.expires_at || null })
  dialogVisible.value = true
}
const save = async () => {
  if (!canWrite.value) return
  if (!form.admin_id || !form.tenant_id) return ElMessage.warning('请选择平台管理账号和租户')
  saving.value = true
  try {
    const response = editing.value
      ? await updateTenantAdminMembership(editing.value.id, form)
      : await createTenantAdminMembership(form)
    if (response.success) {
      ElMessage.success(editing.value ? '租户管理员授权已更新，权限版本已刷新' : '租户管理员授权已创建')
      dialogVisible.value = false
      window.dispatchEvent(new Event('tenant-catalog-changed'))
      await load()
    }
  } finally { saving.value = false }
}
const roleLabel = (role: string) => roleOptions.find(option => option.value === role)?.label || role
const roleDescription = (role: string) => ({ tenant_admin: '拥有当前租户全部管理权限。', security_auditor: '只读查看资源、会话与租户审计。', tenant_viewer: '只读查看租户业务信息和设置。' }[role] || '')
const platformRoleLabel = (role: string) => ({ admin: '平台管理员', platform_admin: '平台管理员', viewer: '平台观察员', platform_viewer: '平台观察员', tenant_admin: '仅租户身份' }[role] || role)
const expired = (row: TenantAdminMembership) => Boolean(row.expires_at && new Date(row.expires_at).getTime() <= Date.now())
const statusLabel = (row: TenantAdminMembership) => !row.admin_enabled ? '账号停用' : !row.enabled ? '授权停用' : expired(row) ? '已过期' : row.tenant_status === 'suspended' ? '租户暂停' : '有效'
const statusType = (row: TenantAdminMembership) => statusLabel(row) === '有效' ? 'success' : statusLabel(row) === '租户暂停' ? 'warning' : 'info'
const formatTime = (value: string) => new Date(value).toLocaleString('zh-CN', { hour12: false })

onMounted(load)
</script>

<style scoped>
.membership-page { max-width: none; }
.page-header, .header-actions, .toolbar { display: flex; align-items: center; }
.page-header { justify-content: space-between; align-items: flex-start; gap: 24px; margin-bottom: 18px; }
.header-actions { gap: 8px; }
h1 { margin: 0; color: var(--text-primary); font-size: 24px; line-height: 32px; }
.page-header p { margin: 5px 0 0; color: var(--text-secondary); font-size: 13px; }
.list-surface { overflow: hidden; border: 1px solid var(--border-light); border-radius: 7px; background: #fff; }
.toolbar { gap: 10px; padding: 14px 16px; border-bottom: 1px solid var(--border-light); }
.toolbar .el-input { width: 300px; }
.toolbar .el-select { width: 170px; }
.toolbar > span { margin-left: auto; color: var(--text-secondary); font-size: 12px; }
.secondary { display: block; margin-top: 3px; color: var(--text-secondary); font-size: 12px; }
.pagination { display: flex; justify-content: flex-end; padding: 16px; }
.membership-form { margin-top: 16px; }
.role-hint { margin-top: 6px; color: var(--text-secondary); font-size: 12px; line-height: 18px; }
@media (max-width: 760px) { .page-header, .toolbar { align-items: stretch; flex-direction: column; } .toolbar .el-input, .toolbar .el-select { width: 100%; } .toolbar > span { margin-left: 0; } }
</style>
