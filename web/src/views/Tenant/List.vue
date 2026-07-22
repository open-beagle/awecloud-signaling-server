<template>
  <div class="tenant-page">
    <div class="page-header">
      <div>
        <div class="eyebrow">客户安全边界</div>
        <h1>客户</h1>
        <p>先建立客户并加入现有人员，再登记和授权统一资源。旧 Agent、设备和授权不会被修改。</p>
      </div>
      <div class="header-actions">
        <el-button :icon="Refresh" :loading="loading" @click="fetchTenants">刷新</el-button>
        <el-button type="primary" :icon="Plus" :disabled="!authStore.isPlatformAdmin" @click="showCreate = true">创建客户</el-button>
      </div>
    </div>

    <div class="tenant-surface">
      <div class="toolbar">
        <el-input v-model="search" clearable :prefix-icon="Search" placeholder="搜索客户名称或标识" @keyup.enter="handleSearch" />
        <span>{{ pagination.total }} 个客户</span>
      </div>
      <el-table v-loading="loading" :data="tenants" stripe>
        <el-table-column label="客户" min-width="260">
          <template #default="{ row }"><strong>{{ row.name }}</strong><span class="secondary">{{ row.key }}</span></template>
        </el-table-column>
        <el-table-column label="状态" width="120"><template #default="{ row }"><el-tag size="small" :type="row.status === 'active' ? 'success' : 'warning'">{{ row.status === 'active' ? '正常' : '已暂停' }}</el-tag></template></el-table-column>
        <el-table-column label="成员" width="110" align="center"><template #default="{ row }"><strong>{{ row.member_count || 0 }}</strong></template></el-table-column>
        <el-table-column label="资源" width="110" align="center"><template #default="{ row }"><strong>{{ row.resource_count || 0 }}</strong></template></el-table-column>
        <el-table-column label="创建时间" width="180"><template #default="{ row }">{{ formatTime(row.created_at) }}</template></el-table-column>
        <el-table-column label="操作" width="210" fixed="right" align="right">
          <template #default="{ row }"><el-button link type="primary" @click="openMembers(row)">管理成员</el-button><el-button link type="primary" @click="enterTenant(row)">进入客户</el-button></template>
        </el-table-column>
      </el-table>
      <el-empty v-if="!loading && !tenants.length" description="还没有客户。创建第一个客户后即可登记统一资源。" />
      <div class="pagination"><el-pagination v-model:current-page="pagination.page" v-model:page-size="pagination.size" layout="total, prev, pager, next" :total="pagination.total" @current-change="fetchTenants" /></div>
    </div>

    <el-dialog v-model="showCreate" title="创建客户" width="480px">
      <el-form label-position="top">
        <el-form-item label="客户名称" required><el-input v-model="createForm.name" placeholder="例如：深圳智翼" /></el-form-item>
        <el-form-item label="稳定标识" required><el-input v-model="createForm.key" placeholder="例如：shenzhen-zhiyi" /><div class="form-hint">创建后用于业务绑定，不使用显示名称作为安全主键。</div></el-form-item>
      </el-form>
      <template #footer><el-button @click="showCreate = false">取消</el-button><el-button type="primary" :loading="creating" @click="handleCreate">创建</el-button></template>
    </el-dialog>

    <el-drawer v-model="showMembers" :title="`${selectedTenant?.name || ''} · 成员`" size="560px">
      <div class="member-toolbar"><el-button type="primary" :icon="Plus" :disabled="!authStore.canWrite" @click="openAddMember">加入现有人员</el-button></div>
      <el-table v-loading="loadingMembers" :data="members">
        <el-table-column label="人员" min-width="220"><template #default="{ row }"><strong>{{ row.alias || row.name }}</strong><span class="secondary">{{ row.name }}</span></template></el-table-column>
        <el-table-column prop="role" label="客户角色" width="140" />
        <el-table-column label="状态" width="100"><template #default="{ row }"><el-tag size="small" :type="row.enabled ? 'success' : 'info'">{{ row.enabled ? '有效' : '停用' }}</el-tag></template></el-table-column>
        <el-table-column label="操作" width="90" align="right"><template #default="{ row }"><el-button link :type="row.enabled ? 'danger' : 'primary'" :disabled="!authStore.canWrite" @click="toggleMember(row)">{{ row.enabled ? '停用' : '恢复' }}</el-button></template></el-table-column>
      </el-table>
      <el-empty v-if="!loadingMembers && !members.length" description="该客户还没有成员" />
    </el-drawer>

    <el-dialog v-model="showAddMember" title="加入现有人员" width="500px">
      <el-form label-position="top">
        <el-form-item label="人员" required><el-select v-model="memberForm.user_id" filterable style="width: 100%" placeholder="选择已启用的 Desktop 用户"><el-option v-for="user in users" :key="user.id" :value="user.id" :label="user.alias ? `${user.alias} (${user.name})` : user.name" /></el-select></el-form-item>
        <el-form-item label="客户角色"><el-select v-model="memberForm.role" style="width: 100%"><el-option label="成员" value="member" /><el-option label="客户管理员" value="tenant_admin" /><el-option label="只读成员" value="viewer" /></el-select></el-form-item>
      </el-form>
      <template #footer><el-button @click="showAddMember = false">取消</el-button><el-button type="primary" :loading="addingMember" :disabled="!authStore.canWrite" @click="handleAddMember">确认加入</el-button></template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Plus, Refresh, Search } from '@element-plus/icons-vue'
import { addTenantMember, createTenant, disableTenantMember, getTenantMembers, getTenants, type Tenant, type TenantMember } from '@/api/resource'
import { getUsers, type User } from '@/api/user'
import { useAuthStore } from '@/stores/auth'
import { useTenantStore } from '@/stores/tenant'

const router = useRouter()
const authStore = useAuthStore()
const tenantStore = useTenantStore()
const loading = ref(false)
const creating = ref(false)
const loadingMembers = ref(false)
const addingMember = ref(false)
const showCreate = ref(false)
const showMembers = ref(false)
const showAddMember = ref(false)
const search = ref('')
const tenants = ref<Tenant[]>([])
const members = ref<TenantMember[]>([])
const users = ref<User[]>([])
const selectedTenant = ref<Tenant | null>(null)
const pagination = reactive({ page: 1, size: 20, total: 0 })
const createForm = reactive({ name: '', key: '' })
const memberForm = reactive<{ user_id?: number; role: 'member' | 'tenant_admin' | 'viewer' }>({ user_id: undefined, role: 'member' })

const fetchTenants = async () => {
  loading.value = true
  try {
    const res = await getTenants({ search: search.value || undefined, page: pagination.page, size: pagination.size })
    if (res.success && res.data) { tenants.value = res.data; pagination.total = res.total }
  } finally { loading.value = false }
}
const handleSearch = () => { pagination.page = 1; fetchTenants() }
const handleCreate = async () => {
  if (!createForm.name.trim() || !createForm.key.trim()) return ElMessage.warning('请输入客户名称和稳定标识')
  creating.value = true
  try {
    const res = await createTenant({ name: createForm.name.trim(), key: createForm.key.trim() })
    if (res.success && res.data) {
      ElMessage.success('客户已创建')
      showCreate.value = false
      createForm.name = ''; createForm.key = ''
      window.dispatchEvent(new Event('tenant-catalog-changed'))
      await fetchTenants()
    }
  } finally { creating.value = false }
}
const openMembers = async (tenant: Tenant) => {
  selectedTenant.value = tenant; showMembers.value = true; loadingMembers.value = true
  try { const res = await getTenantMembers(tenant.id); members.value = res.success && res.data ? res.data : [] } finally { loadingMembers.value = false }
}
const openAddMember = async () => {
  if (!authStore.canWrite) return
  showAddMember.value = true
  const res = await getUsers({ role: 'client', enabled: 'true', page: 1, size: 100 })
  users.value = res.success && res.data ? res.data : []
}
const handleAddMember = async () => {
  if (!authStore.canWrite) return
  if (!selectedTenant.value || !memberForm.user_id) return ElMessage.warning('请选择人员')
  addingMember.value = true
  try {
    const res = await addTenantMember(selectedTenant.value.id, { user_id: memberForm.user_id, role: memberForm.role })
    if (res.success) { ElMessage.success('成员已加入客户'); showAddMember.value = false; memberForm.user_id = undefined; await openMembers(selectedTenant.value); await fetchTenants() }
  } finally { addingMember.value = false }
}
const toggleMember = async (member: TenantMember) => {
  if (!selectedTenant.value || !authStore.canWrite) return
  if (!member.enabled) {
    const res = await addTenantMember(selectedTenant.value.id, { user_id: member.user_id, role: member.role as 'member' | 'tenant_admin' | 'viewer' })
    if (res.success) { ElMessage.success('成员已恢复'); await openMembers(selectedTenant.value); await fetchTenants() }
    return
  }
  try {
    await ElMessageBox.confirm(`停用 ${member.alias || member.name} 后，该人员将不能通过此客户访问统一资源。`, '停用成员', { type: 'warning', confirmButtonText: '确认停用' })
    const res = await disableTenantMember(selectedTenant.value.id, member.user_id)
    if (res.success) { ElMessage.success('成员已停用'); await openMembers(selectedTenant.value); await fetchTenants() }
  } catch (error) {
    if (error !== 'cancel' && error !== 'close') throw error
  }
}
const enterTenant = (tenant: Tenant) => { tenantStore.setTenant(tenant.id); router.push('/resources') }
const formatTime = (value: string) => new Date(value).toLocaleString('zh-CN', { hour12: false })
onMounted(fetchTenants)
</script>

<style scoped>
.tenant-page { width: 100%; }
.page-header, .header-actions, .toolbar, .member-toolbar { display: flex; align-items: center; }
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
.member-toolbar { justify-content: flex-end; margin-bottom: 12px; }
.form-hint { margin-top: 6px; line-height: 18px; }
@media (max-width: 700px) { .page-header { flex-direction: column; } .toolbar .el-input { width: 100%; } }
</style>
