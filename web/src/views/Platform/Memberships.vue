<template>
  <div class="membership-page">
    <PageHeader title="授权管理" eyebrow="Platform Governance" description="统一查看用户对租户与资源供应商的正式管理关系；管理授权不会授予业务资源访问资格。">
      <template #actions>
        <el-button :icon="Refresh" :loading="loading" @click="load">刷新</el-button>
        <el-button v-if="canWrite" type="primary" :icon="Plus" @click="openCreate">新建管理授权</el-button>
      </template>
    </PageHeader>

    <el-alert
      :title="canWrite ? '可在此维护租户与资源供应商的正式管理授权；兼容期旧 Admin 关系不在此扩展。' : '当前页面只读。两类授权均以统一用户为主体，兼容期旧 Admin 关系不在此扩展。'"
      type="info"
      show-icon
      :closable="false"
    />
    <el-alert v-if="errorMessage" class="state-alert" title="管理授权加载失败" :description="errorMessage" type="error" show-icon :closable="false" />

    <section class="list-surface">
      <div class="scope-tabs" role="group" aria-label="管理授权对象类型">
        <el-radio-group v-model="filters.scope_type" @change="search">
          <el-radio-button value="">全部授权</el-radio-button>
          <el-radio-button value="provider">资源授权</el-radio-button>
          <el-radio-button value="tenant">租户授权</el-radio-button>
        </el-radio-group>
      </div>
      <div class="toolbar">
        <el-input v-model="filters.search" clearable :prefix-icon="Search" placeholder="搜索 User 或目标对象" @keyup.enter="search" />
        <el-select v-model="filters.role" clearable placeholder="全部角色" @change="search">
          <el-option v-for="option in roleOptions" :key="option.value" :label="option.label" :value="option.value" />
        </el-select>
        <el-select v-model="filters.state" clearable placeholder="全部状态" @change="search">
          <el-option label="有效" value="active" />
          <el-option label="未生效" value="scheduled" />
          <el-option label="已过期" value="expired" />
          <el-option label="已停用" value="disabled" />
        </el-select>
        <span>{{ pagination.total }} 条授权</span>
      </div>

      <el-table v-loading="loading" :data="items" :empty-text="errorMessage ? ' ' : '当前筛选条件下没有管理授权'" stripe>
        <el-table-column label="User" min-width="210">
          <template #default="{ row }"><strong>{{ row.display_name || row.username }}</strong><span class="secondary">{{ row.username }} · #{{ row.user_id }}</span></template>
        </el-table-column>
        <el-table-column label="目标对象" min-width="230">
          <template #default="{ row }"><strong>{{ row.scope_name }}</strong><span class="secondary">{{ scopeLabel(row.scope_type) }} · {{ row.scope_key }}</span></template>
        </el-table-column>
        <el-table-column label="角色" width="150"><template #default="{ row }">{{ roleLabel(row.role) }}</template></el-table-column>
        <el-table-column label="状态" width="120"><template #default="{ row }"><el-tag size="small" :type="statusType(row)">{{ statusLabel(row) }}</el-tag></template></el-table-column>
        <el-table-column label="有效期" width="190"><template #default="{ row }">{{ formatTime(row.valid_from) }}<span class="secondary">至 {{ row.expires_at ? formatTime(row.expires_at) : '长期有效' }}</span></template></el-table-column>
        <el-table-column label="权限版本" width="100" align="center"><template #default="{ row }">r{{ row.permission_revision }}</template></el-table-column>
        <el-table-column prop="reason" label="授权原因" min-width="190" show-overflow-tooltip />
        <el-table-column v-if="canWrite" label="操作" width="90" fixed="right"><template #default="{ row }"><el-button link type="primary" :icon="Edit" @click="openEdit(row)">编辑</el-button></template></el-table-column>
      </el-table>
      <div class="pagination"><el-pagination v-model:current-page="pagination.page" v-model:page-size="pagination.size" :total="pagination.total" :page-sizes="[20, 50, 100]" layout="total, sizes, prev, pager, next" @size-change="load" @current-change="load" /></div>
    </section>

    <el-dialog v-model="dialog.visible" :title="dialog.mode === 'create' ? '新建管理授权' : '编辑管理授权'" width="620px" destroy-on-close>
      <el-alert title="授权只建立目标组织的管理关系；完成后仍需切换到对应工作域。自授权必须设置到期时间并完整记录原因。" type="warning" show-icon :closable="false" />
      <el-form class="membership-form" label-position="top">
        <el-form-item label="授权类型" required>
          <el-radio-group v-model="form.scopeType" :disabled="dialog.mode === 'edit'" @change="loadOrganizations"><el-radio-button value="provider">资源授权</el-radio-button><el-radio-button value="tenant">租户授权</el-radio-button></el-radio-group>
        </el-form-item>
        <el-form-item label="目标对象" required>
          <el-select v-model="form.scopeId" filterable :disabled="dialog.mode === 'edit'" placeholder="选择目标组织" style="width: 100%">
            <el-option v-for="organization in organizationOptions" :key="organization.id" :label="`${organization.name} · ${organization.key}`" :value="organization.id" />
          </el-select>
        </el-form-item>
        <el-form-item label="User" required>
          <el-select v-model="form.userId" filterable remote :remote-method="searchUsers" :loading="userLoading" :disabled="dialog.mode === 'edit'" placeholder="搜索并选择统一 User" style="width: 100%">
            <el-option v-for="user in userOptions" :key="user.id" :label="`${user.alias || user.name} · ${user.name} · #${user.id}`" :value="user.id" />
          </el-select>
        </el-form-item>
        <div class="form-grid">
          <el-form-item label="角色" required><el-select v-model="form.role" style="width: 100%"><el-option v-for="option in rolesForScope(form.scopeType)" :key="option.value" :label="option.label" :value="option.value" /></el-select></el-form-item>
          <el-form-item v-if="dialog.mode === 'edit'" label="授权状态" required><el-select v-model="form.enabled" style="width: 100%"><el-option label="启用" :value="true" /><el-option label="停用" :value="false" /></el-select></el-form-item>
          <el-form-item v-else label="生效时间" required><el-date-picker v-model="form.validFrom" type="datetime" style="width: 100%" /></el-form-item>
        </div>
        <el-form-item label="到期时间" required><el-date-picker v-model="form.expiresAt" type="datetime" :disabled-date="disablePastDate" style="width: 100%" /></el-form-item>
        <el-form-item label="变更原因" required><el-input v-model="form.reason" type="textarea" :rows="3" maxlength="500" show-word-limit placeholder="说明授权目的、工单或轮换原因" /></el-form-item>
      </el-form>
      <template #footer><el-button @click="dialog.visible = false">取消</el-button><el-button type="primary" :loading="saving" :disabled="!formValid" @click="saveMembership">{{ dialog.mode === 'create' ? '创建授权' : '保存变更' }}</el-button></template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { Edit, Plus, Refresh, Search } from '@element-plus/icons-vue'
import PageHeader from '@/components/Common/PageHeader.vue'
import {
  createPlatformManagementMembership,
  getPlatformOrganizations,
  getPlatformManagementMemberships,
  updatePlatformManagementMembership,
  type PlatformManagementMembership,
  type PlatformMembershipScopeType,
  type PlatformMembershipState,
  type PlatformOrganization
} from '@/api/platformGovernance'
import { getUsers, type User } from '@/api/user'
import { useWorkspaceStore } from '@/stores/workspace'
import { createIdempotencyKey } from '@/utils/idempotency'

const workspaceStore = useWorkspaceStore()
const canWrite = computed(() => workspaceStore.can('platform.memberships.write'))
const loading = ref(false)
const errorMessage = ref('')
const items = ref<PlatformManagementMembership[]>([])
const filters = reactive<{ scope_type: PlatformMembershipScopeType | ''; role: string; state: PlatformMembershipState | ''; search: string }>({ scope_type: '', role: '', state: '', search: '' })
const pagination = reactive({ page: 1, size: 20, total: 0 })
const saving = ref(false)
const userLoading = ref(false)
const userOptions = ref<User[]>([])
const organizationOptions = ref<PlatformOrganization[]>([])
const dialog = reactive<{ visible: boolean; mode: 'create' | 'edit'; membership?: PlatformManagementMembership }>({ visible: false, mode: 'create' })
const form = reactive<{ scopeType: PlatformMembershipScopeType; scopeId: string; userId?: number; role: string; enabled: boolean; validFrom: Date; expiresAt: Date | null; reason: string }>({ scopeType: 'provider', scopeId: '', userId: undefined, role: 'provider_viewer', enabled: true, validFrom: new Date(), expiresAt: null, reason: '' })
const allRoles = [
  { label: '资源管理员', value: 'provider_admin', scope: 'provider' },
  { label: '资源运维员', value: 'provider_operator', scope: 'provider' },
  { label: '资源观察员', value: 'provider_viewer', scope: 'provider' },
  { label: '租户管理员', value: 'tenant_admin', scope: 'tenant' },
  { label: '安全审计员', value: 'security_auditor', scope: 'tenant' },
  { label: '租户观察员', value: 'tenant_viewer', scope: 'tenant' }
]
const roleOptions = computed(() => filters.scope_type ? allRoles.filter(option => option.scope === filters.scope_type) : allRoles)
const rolesForScope = (scope: PlatformMembershipScopeType) => allRoles.filter(option => option.scope === scope)
const formValid = computed(() => !!form.scopeId && !!form.userId && !!form.role && !!form.expiresAt && form.expiresAt.getTime() > form.validFrom.getTime() && !!form.reason.trim())

const load = async () => {
  loading.value = true
  errorMessage.value = ''
  try {
    const response = await getPlatformManagementMemberships({
      scope_type: filters.scope_type || undefined,
      role: filters.role || undefined,
      state: filters.state || undefined,
      search: filters.search.trim() || undefined,
      page: pagination.page,
      size: pagination.size
    })
    items.value = response.success && response.data ? response.data : []
    pagination.total = response.total || 0
  } catch {
    items.value = []
    pagination.total = 0
    errorMessage.value = '请确认平台管理授权读取权限和服务状态后重试。'
  } finally {
    loading.value = false
  }
}
const search = () => { pagination.page = 1; load() }
watch(() => filters.scope_type, () => {
  if (filters.role && !roleOptions.value.some(option => option.value === filters.role)) filters.role = ''
})
const scopeLabel = (scope: PlatformMembershipScopeType) => scope === 'provider' ? '资源供应商' : '租户'
const roleLabel = (role: string) => allRoles.find(option => option.value === role)?.label || role
const statusLabel = (row: PlatformManagementMembership) => !row.user_enabled ? '账号停用' : !row.enabled ? '授权停用' : new Date(row.valid_from).getTime() > Date.now() ? '未生效' : row.expires_at && new Date(row.expires_at).getTime() <= Date.now() ? '已过期' : row.scope_status !== 'active' ? '对象暂停' : '有效'
const statusType = (row: PlatformManagementMembership) => statusLabel(row) === '有效' ? 'success' : statusLabel(row) === '对象暂停' ? 'warning' : 'info'
const formatTime = (value: string) => new Date(value).toLocaleString('zh-CN', { hour12: false })

const loadOrganizations = async () => {
  form.scopeId = ''
  form.role = rolesForScope(form.scopeType)[2]?.value || rolesForScope(form.scopeType)[0]?.value || ''
  try {
    const response = await getPlatformOrganizations({ scope_type: form.scopeType, page: 1, size: 100 })
    organizationOptions.value = response.success && response.data ? response.data.filter(item => item.status !== 'retired') : []
  } catch { organizationOptions.value = [] }
}
const searchUsers = async (search = '') => {
  userLoading.value = true
  try {
    const response = await getUsers({ role: 'client', enabled: 'true', search: search || undefined, page: 1, size: 50 })
    userOptions.value = response.success && response.data ? response.data : []
  } catch { userOptions.value = [] } finally { userLoading.value = false }
}
const resetForm = () => {
  form.scopeType = 'provider'; form.scopeId = ''; form.userId = undefined; form.role = 'provider_viewer'; form.enabled = true
  form.validFrom = new Date(); form.expiresAt = null; form.reason = ''
}
const openCreate = async () => {
  resetForm(); dialog.mode = 'create'; dialog.membership = undefined; dialog.visible = true
  await Promise.all([loadOrganizations(), searchUsers()])
}
const openEdit = async (membership: PlatformManagementMembership) => {
  dialog.mode = 'edit'; dialog.membership = membership; form.scopeType = membership.scope_type; form.scopeId = membership.scope_id
  form.userId = membership.user_id; form.role = membership.role; form.enabled = membership.enabled
  form.validFrom = new Date(membership.valid_from); form.expiresAt = membership.expires_at ? new Date(membership.expires_at) : null; form.reason = ''
  organizationOptions.value = [{ id: membership.scope_id, scope_type: membership.scope_type, key: membership.scope_key, name: membership.scope_name, status: membership.scope_status as PlatformOrganization['status'], management_membership_count: 0, business_member_count: 0, technical_resource_count: 0, resource_count: 0, scope_count: 0, revision: 0, row_version: 0, created_at: membership.created_at, updated_at: membership.updated_at }]
  userOptions.value = [{ id: membership.user_id, name: membership.username, alias: membership.display_name, role: 'client', enabled: membership.user_enabled, created_at: membership.created_at, updated_at: membership.updated_at }]
  dialog.visible = true
}
const saveMembership = async () => {
  if (!canWrite.value || !formValid.value) return
  saving.value = true
  try {
    if (dialog.mode === 'create') {
      await createPlatformManagementMembership(form.scopeType, form.scopeId, { user_id: form.userId, role: form.role, valid_from: form.validFrom.toISOString(), expires_at: form.expiresAt!.toISOString(), reason: form.reason.trim() }, createIdempotencyKey())
    } else if (dialog.membership) {
      await updatePlatformManagementMembership(dialog.membership, { role: form.role, enabled: form.enabled, expires_at: form.expiresAt!.toISOString(), reason: form.reason.trim() })
    }
    ElMessage.success(dialog.mode === 'create' ? '管理授权已创建' : '管理授权已更新')
    dialog.visible = false; await load()
  } catch (error) {
    if (!(error as { isAxiosError?: boolean })?.isAxiosError) ElMessage.error('管理授权保存失败，请重试')
  } finally { saving.value = false }
}
const disablePastDate = (date: Date) => date.getTime() < new Date().setHours(0, 0, 0, 0)

onMounted(load)
</script>

<style scoped>
.membership-page { width: 100%; }
.state-alert { margin-top: 12px; }
.list-surface { margin-top: 14px; overflow: hidden; border: 1px solid var(--border-light); border-radius: 6px; background: #fff; }
.scope-tabs { padding: 14px 16px 0; }
.toolbar { display: flex; align-items: center; gap: 10px; padding: 14px 16px; border-bottom: 1px solid var(--border-light); }
.toolbar .el-input { width: 310px; }
.toolbar .el-select { width: 160px; }
.toolbar > span { margin-left: auto; color: var(--text-secondary); font-size: 12px; }
.secondary { display: block; margin-top: 3px; color: var(--text-secondary); font-size: 12px; }
.pagination { display: flex; justify-content: flex-end; padding: 16px; }
.membership-form { margin-top: 16px; }
.form-grid { display: grid; grid-template-columns: 1fr 1fr; gap: 14px; }
</style>
