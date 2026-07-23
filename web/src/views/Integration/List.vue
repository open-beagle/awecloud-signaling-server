<template>
  <div class="integration-page">
    <div class="page-header">
      <div><div class="eyebrow">可信业务绑定</div><h1>集成</h1><p>将 Provider 的稳定客户和 Workspace 身份绑定到当前 Tenant。运行时标签只作为匹配证据。</p></div>
      <el-button :icon="Refresh" :loading="loading" @click="fetchAll">刷新</el-button>
    </div>

    <el-alert v-if="!tenantStore.tenantId" title="当前为全客户只读视图。创建或更新绑定前，请先在顶部选择明确客户。" type="warning" show-icon :closable="false" class="context-alert" />

    <div class="summary-strip">
      <div><span>Provider 客户绑定</span><strong>{{ providerPagination.total }}</strong></div>
      <div><span>Workspace 绑定</span><strong>{{ workspacePagination.total }}</strong></div>
      <div><span>当前客户</span><strong class="tenant-name">{{ currentTenantName }}</strong></div>
    </div>

    <div class="surface">
      <el-tabs v-model="activeTab">
        <el-tab-pane label="Provider 客户" name="providers">
          <div class="tab-toolbar"><div><h2>外部客户映射</h2><p>一个 Provider 外部客户只能绑定一个内部 Tenant。</p></div><el-button type="primary" :icon="Plus" :disabled="!canWrite" @click="showProvider = true">绑定外部客户</el-button></div>
          <el-table v-loading="loadingProviders" :data="providerBindings" stripe>
            <el-table-column label="Provider" min-width="180"><template #default="{ row }"><strong>{{ row.provider_id }}</strong></template></el-table-column>
            <el-table-column label="外部客户 ID" min-width="220"><template #default="{ row }"><code>{{ row.external_tenant_id }}</code></template></el-table-column>
            <el-table-column label="内部客户" min-width="200"><template #default="{ row }"><strong>{{ row.tenant_name || row.tenant_id }}</strong><code>{{ row.tenant_id }}</code></template></el-table-column>
            <el-table-column label="状态" width="110"><template #default="{ row }"><el-tag size="small" :type="row.status === 'active' ? 'success' : 'info'">{{ row.status === 'active' ? '有效' : '已撤销' }}</el-tag></template></el-table-column>
            <el-table-column label="更新时间" width="180"><template #default="{ row }">{{ formatTime(row.updated_at) }}</template></el-table-column>
          </el-table>
          <el-empty v-if="!loadingProviders && !providerBindings.length" description="还没有 Provider 客户绑定" />
          <div class="pagination"><el-pagination v-model:current-page="providerPagination.page" layout="total, prev, pager, next" :total="providerPagination.total" @current-change="fetchProviders" /></div>
        </el-tab-pane>

        <el-tab-pane label="Workspace" name="workspaces">
          <div class="tab-toolbar"><div><h2>Workspace 绑定</h2><p>保存后会自动重试匹配已有 Candidate，并创建或更新稳定 Resource。</p></div><el-button type="primary" :icon="Plus" :disabled="!canWrite" @click="openWorkspaceDialog">绑定 Workspace</el-button></div>
          <el-table v-loading="loadingWorkspaces" :data="workspaceBindings" stripe>
            <el-table-column label="Workspace" min-width="240"><template #default="{ row }"><strong>{{ row.external_workspace_id }}</strong><span class="secondary">{{ row.provider_id }} · {{ row.external_tenant_id }}</span></template></el-table-column>
            <el-table-column label="客户" min-width="170"><template #default="{ row }">{{ row.tenant_name || row.tenant_id }}</template></el-table-column>
            <el-table-column label="Owner" width="120"><template #default="{ row }">{{ ownerLabel(row.owner_user_id) }}</template></el-table-column>
            <el-table-column label="Generation" width="120"><template #default="{ row }">{{ row.generation }}</template></el-table-column>
            <el-table-column label="状态" width="110"><template #default="{ row }"><el-tag size="small" :type="workspaceStatusTag(row.status)">{{ workspaceStatusLabel(row.status) }}</el-tag></template></el-table-column>
            <el-table-column label="Resource" min-width="210"><template #default="{ row }"><el-link type="primary" :underline="false" @click="router.push(`/resources/${row.resource_id}`)">{{ row.resource_id }}</el-link></template></el-table-column>
            <el-table-column label="更新时间" width="180"><template #default="{ row }">{{ formatTime(row.updated_at) }}</template></el-table-column>
          </el-table>
          <el-empty v-if="!loadingWorkspaces && !workspaceBindings.length" description="当前范围还没有 Workspace 绑定" />
          <div class="pagination"><el-pagination v-model:current-page="workspacePagination.page" layout="total, prev, pager, next" :total="workspacePagination.total" @current-change="fetchWorkspaces" /></div>
        </el-tab-pane>
      </el-tabs>
    </div>

    <el-dialog v-model="showProvider" title="绑定 Provider 外部客户" width="520px">
      <el-alert :title="`绑定到：${currentTenantName}`" type="info" show-icon :closable="false" class="dialog-alert" />
      <el-form label-position="top"><el-form-item label="Provider ID" required><el-input v-model="providerForm.provider_id" placeholder="例如：beagle-ide" /></el-form-item><el-form-item label="外部客户 ID" required><el-input v-model="providerForm.external_tenant_id" placeholder="Provider 中的稳定客户 ID" /></el-form-item></el-form>
      <template #footer><el-button @click="showProvider = false">取消</el-button><el-button type="primary" :loading="savingProvider" @click="saveProvider">确认绑定</el-button></template>
    </el-dialog>

    <el-dialog v-model="showWorkspace" title="绑定 Workspace" width="560px">
      <el-alert title="Provider、外部客户和 Workspace ID 必须来自可信业务系统，不能使用 Pod 标签猜测客户归属。" type="warning" show-icon :closable="false" class="dialog-alert" />
      <el-form label-position="top">
        <div class="form-grid"><el-form-item label="Provider ID" required><el-input v-model="workspaceForm.provider_id" /></el-form-item><el-form-item label="外部客户 ID" required><el-input v-model="workspaceForm.external_tenant_id" /></el-form-item></div>
        <el-form-item label="Workspace ID" required><el-input v-model="workspaceForm.external_workspace_id" /></el-form-item>
        <el-form-item label="资源显示名称"><el-input v-model="workspaceForm.display_name" placeholder="留空则使用 Workspace ID" /></el-form-item>
        <div class="form-grid"><el-form-item label="Owner"><el-select v-model="workspaceForm.owner_user_id" clearable filterable style="width:100%"><el-option v-for="member in members" :key="member.user_id" :label="member.alias || member.name" :value="member.user_id" :disabled="!member.enabled" /></el-select></el-form-item><el-form-item label="Generation"><el-input-number v-model="workspaceForm.generation" :min="1" :step="1" style="width:100%" /></el-form-item></div>
        <el-form-item label="生命周期"><el-radio-group v-model="workspaceForm.status"><el-radio-button label="active">运行</el-radio-button><el-radio-button label="stopped">停止</el-radio-button><el-radio-button label="revoked">撤销</el-radio-button></el-radio-group></el-form-item>
      </el-form>
      <template #footer><el-button @click="showWorkspace = false">取消</el-button><el-button type="primary" :loading="savingWorkspace" @click="saveWorkspace">保存绑定</el-button></template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import { Plus, Refresh } from '@element-plus/icons-vue'
import { createProviderTenantBinding, createWorkspaceBinding, getProviderTenantBindings, getTenantMembers, getTenants, getWorkspaceBindings, type ProviderTenantBinding, type Tenant, type TenantMember, type WorkspaceBinding } from '@/api/resource'
import { useAuthStore } from '@/stores/auth'
import { useTenantStore } from '@/stores/tenant'

const router = useRouter(), authStore = useAuthStore(), tenantStore = useTenantStore()
const activeTab = ref('providers'), loadingProviders = ref(false), loadingWorkspaces = ref(false)
const loading = computed(() => loadingProviders.value || loadingWorkspaces.value)
const showProvider = ref(false), showWorkspace = ref(false), savingProvider = ref(false), savingWorkspace = ref(false)
const providerBindings = ref<ProviderTenantBinding[]>([]), workspaceBindings = ref<WorkspaceBinding[]>([]), tenants = ref<Tenant[]>([]), members = ref<TenantMember[]>([])
const providerPagination = reactive({ page: 1, size: 20, total: 0 }), workspacePagination = reactive({ page: 1, size: 20, total: 0 })
const providerForm = reactive({ provider_id: 'beagle-ide', external_tenant_id: '' })
const workspaceForm = reactive<{ provider_id: string; external_tenant_id: string; external_workspace_id: string; display_name: string; owner_user_id?: number; generation: number; status: 'active' | 'stopped' | 'revoked' }>({ provider_id: 'beagle-ide', external_tenant_id: '', external_workspace_id: '', display_name: '', owner_user_id: undefined, generation: 1, status: 'active' })
const currentTenant = computed(() => tenants.value.find(item => item.id === tenantStore.tenantId))
const currentTenantName = computed(() => currentTenant.value?.name || '未选择客户')
const canWrite = computed(() => authStore.isPlatformAdmin && !!tenantStore.tenantId)
const fetchProviders = async () => { loadingProviders.value = true; try { const res = await getProviderTenantBindings({ page: providerPagination.page, size: providerPagination.size }); if (res.success && res.data) { providerBindings.value = res.data; providerPagination.total = res.total } } finally { loadingProviders.value = false } }
const fetchWorkspaces = async () => { loadingWorkspaces.value = true; try { const res = await getWorkspaceBindings({ tenant_id: tenantStore.tenantId || undefined, page: workspacePagination.page, size: workspacePagination.size }); if (res.success && res.data) { workspaceBindings.value = res.data; workspacePagination.total = res.total } } finally { loadingWorkspaces.value = false } }
const fetchAll = () => Promise.all([fetchProviders(), fetchWorkspaces()])
const saveProvider = async () => { if (!canWrite.value || !providerForm.provider_id.trim() || !providerForm.external_tenant_id.trim()) return ElMessage.warning('请选择客户并填写完整绑定'); savingProvider.value = true; try { const res = await createProviderTenantBinding({ provider_id: providerForm.provider_id.trim(), external_tenant_id: providerForm.external_tenant_id.trim(), tenant_id: tenantStore.tenantId }); if (res.success) { ElMessage.success('Provider 客户绑定已保存'); showProvider.value = false; providerForm.external_tenant_id = ''; await fetchProviders() } } finally { savingProvider.value = false } }
const openWorkspaceDialog = async () => { if (!canWrite.value) return; const res = await getTenantMembers(tenantStore.tenantId); members.value = res.success && res.data ? res.data : []; showWorkspace.value = true }
const saveWorkspace = async () => { if (!canWrite.value || !workspaceForm.provider_id.trim() || !workspaceForm.external_tenant_id.trim() || !workspaceForm.external_workspace_id.trim()) return ElMessage.warning('请填写 Provider、外部客户和 Workspace ID'); savingWorkspace.value = true; try { const res = await createWorkspaceBinding({ ...workspaceForm, owner_user_id: workspaceForm.owner_user_id }); if (res.success) { ElMessage.success('Workspace 绑定已保存并触发匹配'); showWorkspace.value = false; workspaceForm.external_workspace_id = ''; workspaceForm.display_name = ''; workspaceForm.owner_user_id = undefined; await fetchWorkspaces() } } finally { savingWorkspace.value = false } }
const ownerLabel = (id?: number) => id ? members.value.find(item => item.user_id === id)?.alias || `User ${id}` : '-'
const workspaceStatusLabel = (status: string) => ({ active: '运行', stopped: '停止', revoked: '撤销' }[status] || status)
const workspaceStatusTag = (status: string) => ({ active: 'success', stopped: 'warning', revoked: 'info' }[status] || 'info') as any
const formatTime = (value: string) => new Date(value).toLocaleString('zh-CN', { hour12: false })
watch(() => tenantStore.tenantId, async () => { workspacePagination.page = 1; if (tenantStore.tenantId) { const res = await getTenantMembers(tenantStore.tenantId); members.value = res.success && res.data ? res.data : [] } else members.value = []; await fetchWorkspaces() })
onMounted(async () => { const tenantRes = await getTenants({ page: 1, size: 100, status: 'active' }); tenants.value = tenantRes.success && tenantRes.data ? tenantRes.data : []; if (tenantStore.tenantId) { const memberRes = await getTenantMembers(tenantStore.tenantId); members.value = memberRes.success && memberRes.data ? memberRes.data : [] } await fetchAll() })
</script>

<style scoped>
.integration-page{width:100%}.page-header,.tab-toolbar{display:flex;justify-content:space-between;align-items:flex-start;gap:24px}.page-header{margin-bottom:18px}.eyebrow,.secondary,.tab-toolbar p,.summary-strip span{color:var(--text-secondary);font-size:12px}.eyebrow{margin-bottom:5px}h1{margin:0;font-size:24px;line-height:32px}.page-header p{margin:5px 0 0;color:var(--text-regular);font-size:13px}.context-alert{margin-bottom:14px}.summary-strip{display:grid;grid-template-columns:repeat(3,1fr);margin-bottom:14px;background:#fff;border:1px solid var(--border-light);border-radius:6px}.summary-strip>div{padding:14px 16px;border-right:1px solid var(--border-light)}.summary-strip>div:last-child{border-right:0}.summary-strip span,.summary-strip strong{display:block}.summary-strip strong{margin-top:4px;font-size:20px}.summary-strip .tenant-name{font-size:15px;line-height:28px}.surface{padding:0 16px 16px;background:#fff;border:1px solid var(--border-light);border-radius:6px}.tab-toolbar{align-items:center;padding:8px 0 14px}.tab-toolbar h2{margin:0;font-size:16px}.tab-toolbar p{margin:4px 0 0}.secondary,code{display:block;margin-top:3px}.pagination{display:flex;justify-content:flex-end;padding-top:16px}.dialog-alert{margin-bottom:16px}.form-grid{display:grid;grid-template-columns:1fr 1fr;gap:12px}@media(max-width:700px){.page-header,.tab-toolbar{flex-direction:column}.summary-strip,.form-grid{grid-template-columns:1fr}.summary-strip>div{border-right:0;border-bottom:1px solid var(--border-light)}}
</style>
