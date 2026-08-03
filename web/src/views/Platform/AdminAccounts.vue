<template>
  <div class="page-container admin-page">
    <div class="page-header">
      <div><h1>平台管理账号</h1><p>管理登录 Web 控制面的真实 Admin 身份。租户管理范围需在“租户管理员授权”中单独配置。</p></div>
      <div class="header-actions">
        <el-button :icon="Refresh" :loading="loading" @click="load">刷新</el-button>
        <el-button v-if="canWrite" type="primary" :icon="Plus" @click="openCreate">新增账号</el-button>
      </div>
    </div>

    <el-alert title="Admin 与 Desktop 用户是两套身份；在此创建账号不会成为租户成员，也不会获得资源访问权限。" type="info" show-icon :closable="false" />
    <section class="list-surface">
      <div class="toolbar"><el-input v-model="search" clearable :prefix-icon="Search" placeholder="搜索平台管理账号" @keyup.enter="handleSearch" /><span>{{ pagination.total }} 个账号</span></div>
      <el-table v-loading="loading" :data="items" stripe>
        <el-table-column label="平台管理账号" min-width="240"><template #default="{ row }"><strong>{{ row.username }}</strong><span class="secondary">Admin #{{ row.id }}</span></template></el-table-column>
        <el-table-column label="平台角色" width="180"><template #default="{ row }">{{ roleLabel(row.platform_role) }}</template></el-table-column>
        <el-table-column label="状态" width="120"><template #default="{ row }"><el-tag size="small" :type="row.enabled ? 'success' : 'info'">{{ row.enabled ? '启用' : '停用' }}</el-tag></template></el-table-column>
        <el-table-column label="创建时间" width="180"><template #default="{ row }">{{ formatTime(row.created_at) }}</template></el-table-column>
        <el-table-column label="更新时间" width="180"><template #default="{ row }">{{ formatTime(row.updated_at) }}</template></el-table-column>
        <el-table-column label="操作" width="90" fixed="right" align="right"><template #default="{ row }"><el-button v-if="canWrite" link type="primary" @click="openEdit(row)">编辑</el-button><span v-else class="secondary">只读</span></template></el-table-column>
      </el-table>
      <el-empty v-if="!loading && !items.length" description="暂无平台管理账号" />
      <div class="pagination"><el-pagination v-model:current-page="pagination.page" layout="total, prev, pager, next" :total="pagination.total" :page-size="pagination.size" @current-change="load" /></div>
    </section>

    <el-dialog v-model="dialogVisible" :title="editing ? '编辑平台管理账号' : '新增平台管理账号'" width="520px">
      <el-form label-position="top">
        <el-form-item label="账号" required><el-input v-model="form.username" maxlength="50" :disabled="Boolean(editing)" autocomplete="off" /></el-form-item>
        <el-form-item v-if="!editing" label="初始密码" required><el-input v-model="form.password" type="password" show-password autocomplete="new-password" /><div class="form-hint">至少 8 个字符，首次交付后应由账号本人修改。</div></el-form-item>
        <el-form-item label="平台角色" required>
          <el-select v-model="form.platform_role" style="width: 100%"><el-option v-for="role in roles" :key="role.value" :label="role.label" :value="role.value" /></el-select>
          <div class="form-hint">{{ roleDescription(form.platform_role) }}</div>
        </el-form-item>
        <el-form-item label="账号状态"><el-switch v-model="form.enabled" active-text="启用" inactive-text="停用" /></el-form-item>
      </el-form>
      <template #footer><el-button @click="dialogVisible = false">取消</el-button><el-button type="primary" :loading="saving" @click="save">保存账号</el-button></template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { Plus, Refresh, Search } from '@element-plus/icons-vue'
import { createPlatformAdmin, getPlatformAdmins, updatePlatformAdmin, type PlatformAdminAccount, type PlatformRole } from '@/api/platformAdmin'
import { useWorkspaceStore } from '@/stores/workspace'

const workspaceStore = useWorkspaceStore()
const canWrite = computed(() => workspaceStore.can('platform.memberships.write'))
const loading = ref(false)
const saving = ref(false)
const dialogVisible = ref(false)
const editing = ref<PlatformAdminAccount>()
const search = ref('')
const items = ref<PlatformAdminAccount[]>([])
const pagination = reactive({ page: 1, size: 20, total: 0 })
const form = reactive<{ username: string; password: string; platform_role: PlatformRole; enabled: boolean }>({ username: '', password: '', platform_role: 'none', enabled: true })
const roles: { label: string; value: PlatformRole }[] = [
  { label: '平台管理员', value: 'platform_admin' },
  { label: '平台观察员', value: 'platform_viewer' },
  { label: '无平台权限', value: 'none' }
]
const load = async () => {
  loading.value = true
  try { const response = await getPlatformAdmins({ search: search.value || undefined, page: pagination.page, size: pagination.size }); items.value = response.success && response.data ? response.data : []; pagination.total = response.total || 0 }
  finally { loading.value = false }
}
const handleSearch = () => { pagination.page = 1; load() }
const openCreate = () => { if (!canWrite.value) return; editing.value = undefined; Object.assign(form, { username: '', password: '', platform_role: 'none', enabled: true }); dialogVisible.value = true }
const openEdit = (row: PlatformAdminAccount) => { if (!canWrite.value) return; editing.value = row; Object.assign(form, { username: row.username, password: '', platform_role: row.platform_role, enabled: row.enabled }); dialogVisible.value = true }
const save = async () => {
  if (!canWrite.value) return
  if (!form.username.trim()) return ElMessage.warning('请输入平台管理账号')
  if (!editing.value && form.password.length < 8) return ElMessage.warning('初始密码至少需要 8 个字符')
  saving.value = true
  try {
    const response = editing.value
      ? await updatePlatformAdmin(editing.value.id, { platform_role: form.platform_role, enabled: form.enabled })
      : await createPlatformAdmin({ username: form.username.trim(), password: form.password, platform_role: form.platform_role, enabled: form.enabled })
    if (response.success) { ElMessage.success(editing.value ? '平台管理账号已更新' : '平台管理账号已创建'); dialogVisible.value = false; await load() }
  } finally { saving.value = false }
}
const roleLabel = (role: PlatformRole) => roles.find(item => item.value === role)?.label || role
const roleDescription = (role: PlatformRole) => ({ platform_admin: '管理平台治理、基础设施和全局安全边界。', platform_viewer: '只读查看获准的平台治理信息。', none: '不显示管理员侧菜单，只能依赖独立的租户管理员授权。' }[role])
const formatTime = (value: string) => new Date(value).toLocaleString('zh-CN', { hour12: false })
onMounted(load)
</script>

<style scoped>
.admin-page { max-width: none; }
.page-header, .header-actions, .toolbar { display: flex; align-items: center; }
.page-header { align-items: flex-start; justify-content: space-between; gap: 24px; margin-bottom: 18px; }
.header-actions { gap: 8px; }
h1 { margin: 0; color: var(--text-primary); font-size: 24px; line-height: 32px; }
.page-header p { margin: 5px 0 0; color: var(--text-secondary); font-size: 13px; }
.list-surface { margin-top: 14px; overflow: hidden; border: 1px solid var(--border-light); border-radius: 6px; background: #fff; }
.toolbar { justify-content: space-between; gap: 16px; padding: 14px 16px; border-bottom: 1px solid var(--border-light); }
.toolbar .el-input { width: 320px; }
.toolbar > span, .secondary, .form-hint { color: var(--text-secondary); font-size: 12px; }
.secondary { display: block; margin-top: 3px; }
.form-hint { margin-top: 6px; line-height: 18px; }
.pagination { display: flex; justify-content: flex-end; padding: 16px; }
@media (max-width: 650px) { .page-header, .toolbar { align-items: stretch; flex-direction: column; } .toolbar .el-input { width: 100%; } }
</style>
