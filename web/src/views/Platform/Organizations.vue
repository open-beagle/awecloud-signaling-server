<template>
  <div class="organization-page">
    <PageHeader :title="pageTitle" eyebrow="Platform Governance" :description="pageDescription">
      <template #actions><el-button :icon="Refresh" :loading="loading" @click="load">刷新</el-button><el-button v-if="canWrite" type="primary" :icon="Plus" @click="openCreate">{{ createLabel }}</el-button></template>
    </PageHeader>

    <el-alert :title="pageNotice" type="info" show-icon :closable="false" />
    <el-alert v-if="errorMessage" class="state-alert" title="组织目录加载失败" :description="errorMessage" type="error" show-icon :closable="false" />

    <section class="list-surface">
      <div class="toolbar">
        <el-input v-model="filters.search" clearable :prefix-icon="Search" :placeholder="searchPlaceholder" @keyup.enter="search" />
        <el-select v-model="filters.status" clearable placeholder="全部状态" @change="search">
          <el-option label="正常" value="active" />
          <el-option label="已暂停" value="suspended" />
          <el-option v-if="scopeType === 'provider'" label="已退役" value="retired" />
        </el-select>
        <span>{{ pagination.total }} 个{{ scopeType === 'provider' ? '资源供应商' : '租户' }}</span>
      </div>

      <el-table v-loading="loading" :data="items" :empty-text="errorMessage ? ' ' : `当前筛选条件下没有${scopeType === 'provider' ? '资源供应商' : '租户'}`" stripe>
        <el-table-column :label="scopeType === 'provider' ? '资源供应商' : '租户'" min-width="250">
          <template #default="{ row }"><strong>{{ row.name }}</strong><span class="secondary">{{ scopeLabel(row.scope_type) }} · {{ row.key }}</span></template>
        </el-table-column>
        <el-table-column v-if="scopeType === 'provider'" label="域名命名空间" min-width="190"><template #default="{ row }"><strong class="domain-label">{{ row.domain_scope === 'root' ? '根域名' : row.domain_label }}</strong><span class="secondary">{{ row.domain_scope === 'root' ? '*.beagle' : `*.${row.domain_label}.beagle` }}</span></template></el-table-column>
        <el-table-column label="状态" width="110"><template #default="{ row }"><el-tag size="small" :type="statusType(row.status)">{{ statusLabel(row.status) }}</el-tag></template></el-table-column>
        <el-table-column label="有效管理员" width="120" align="right"><template #default="{ row }">{{ row.management_membership_count }}</template></el-table-column>
        <el-table-column label="业务规模" min-width="230">
          <template #default="{ row }">
            <template v-if="row.scope_type === 'provider'">
              <strong>{{ row.resource_count }} 个平台资源</strong>
              <span class="secondary">{{ row.technical_resource_count }} 个技术资源 · {{ row.scope_count }} 个资源 Scope</span>
            </template>
            <template v-else>
              <strong>{{ row.business_member_count }} 名业务成员</strong>
              <span class="secondary">{{ row.resource_count }} 个租户资源</span>
            </template>
          </template>
        </el-table-column>
        <el-table-column label="版本" width="90" align="center"><template #default="{ row }">{{ row.scope_type === 'provider' ? `r${row.revision}` : '—' }}</template></el-table-column>
        <el-table-column label="最近更新" width="180"><template #default="{ row }">{{ formatTime(row.updated_at) }}</template></el-table-column>
        <el-table-column v-if="canWrite" label="操作" width="170" fixed="right"><template #default="{ row }"><el-button link type="primary" :icon="Edit" @click="openEdit(row)">编辑</el-button><el-button v-if="row.status !== 'retired'" link :type="row.status === 'active' ? 'danger' : 'success'" @click="openTransition(row)">{{ row.status === 'active' ? '暂停' : '恢复' }}</el-button></template></el-table-column>
      </el-table>
      <div class="pagination"><el-pagination v-model:current-page="pagination.page" v-model:page-size="pagination.size" :total="pagination.total" :page-sizes="[20, 50, 100]" layout="total, sizes, prev, pager, next" @size-change="load" @current-change="load" /></div>
    </section>

    <el-dialog v-model="formDialog.visible" :title="formDialog.mode === 'create' ? createLabel : `编辑${scopeType === 'provider' ? '资源供应商' : '租户'}`" width="620px" destroy-on-close>
      <el-form class="organization-form" label-position="top">
        <el-form-item label="稳定标识" required><el-input v-model="form.key" :disabled="formDialog.mode === 'edit'" maxlength="100" placeholder="小写字母、数字和连字符，例如 north-provider" /></el-form-item>
        <el-form-item label="显示名称" required><el-input v-model="form.name" maxlength="200" placeholder="便于治理人员识别的组织名称" /></el-form-item>
        <el-form-item v-if="scopeType === 'provider' && formDialog.mode === 'create'" label="命名空间类型" required>
          <el-radio-group v-model="form.domainScope"><el-radio-button value="named">普通命名空间</el-radio-button><el-radio-button value="root">根命名空间</el-radio-button></el-radio-group>
        </el-form-item>
        <el-form-item v-if="scopeType === 'provider' && form.domainScope === 'named'" label="域名标识" required>
          <el-input v-model="form.domainLabel" maxlength="63" placeholder="例如 beagle-bj" />
          <span class="field-help">全局唯一；资源域名形如 &lt;resource&gt;.{{ form.domainLabel || 'domain-label' }}.beagle</span>
        </el-form-item>
        <template v-if="domainLabelChanged">
          <el-alert class="domain-alert" title="旧域名将在提交后立即下线" description="旧域名不能继续解析或建立新连接，不保留兼容期。请先确认客户端配置和自动化脚本影响。" type="error" show-icon :closable="false" />
          <div class="domain-change-preview"><strong>域名切换</strong><span>*.{{ formDialog.organization?.domain_label }}.beagle → *.{{ normalizedDomainLabel }}.beagle</span><span>影响摘要：{{ formDialog.organization?.resource_count || 0 }} 个平台资源 · {{ formDialog.organization?.technical_resource_count || 0 }} 个技术资源</span></div>
          <el-form-item label="确认新域名标识" required><el-input v-model="form.domainConfirmation" maxlength="63" placeholder="再次输入新的域名标识" /></el-form-item>
        </template>
        <el-form-item label="变更原因" required><el-input v-model="form.reason" type="textarea" :rows="3" maxlength="500" show-word-limit placeholder="说明创建来源、工单或名称调整原因" /></el-form-item>
      </el-form>
      <template #footer><el-button @click="formDialog.visible = false">取消</el-button><el-button type="primary" :loading="saving" :disabled="!organizationFormValid" @click="saveOrganization">{{ formDialog.mode === 'create' ? createLabel : '保存变更' }}</el-button></template>
    </el-dialog>

    <el-dialog v-model="transitionDialog.visible" :title="transitionDialog.action === 'suspend' ? '暂停组织' : '恢复组织'" width="580px" destroy-on-close>
      <el-alert :title="transitionDialog.action === 'suspend' ? '暂停会阻止该组织继续进入正常治理流程，请先核对影响范围。' : '恢复后该组织将重新进入正常治理流程。'" :type="transitionDialog.action === 'suspend' ? 'warning' : 'info'" show-icon :closable="false" />
      <div v-if="transitionDialog.organization" class="impact-preview"><strong>{{ transitionDialog.organization.name }}</strong><span>{{ scopeLabel(transitionDialog.organization.scope_type) }} · {{ transitionDialog.organization.key }}</span><span v-if="transitionDialog.organization.scope_type === 'provider'">影响摘要：{{ transitionDialog.organization.management_membership_count }} 个有效管理授权、{{ transitionDialog.organization.resource_count }} 个平台资源、{{ transitionDialog.organization.scope_count }} 个资源 Scope</span><span v-else>影响摘要：{{ transitionDialog.organization.management_membership_count }} 个有效管理授权、{{ transitionDialog.organization.business_member_count }} 名业务成员、{{ transitionDialog.organization.resource_count }} 个租户资源</span></div>
      <el-form class="organization-form" label-position="top"><el-form-item label="操作原因" required><el-input v-model="transitionDialog.reason" type="textarea" :rows="3" maxlength="500" show-word-limit placeholder="填写工单、风险处置或恢复依据" /></el-form-item></el-form>
      <template #footer><el-button @click="transitionDialog.visible = false">取消</el-button><el-button :type="transitionDialog.action === 'suspend' ? 'danger' : 'primary'" :loading="saving" :disabled="!transitionDialog.reason.trim()" @click="saveTransition">确认{{ transitionDialog.action === 'suspend' ? '暂停' : '恢复' }}</el-button></template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { Edit, Plus, Refresh, Search } from '@element-plus/icons-vue'
import PageHeader from '@/components/Common/PageHeader.vue'
import {
  createPlatformOrganization,
  getPlatformOrganizations,
  transitionPlatformOrganization,
  updatePlatformOrganization,
  type PlatformOrganization,
  type PlatformOrganizationStatus,
  type PlatformMembershipScopeType
} from '@/api/platformGovernance'
import { useWorkspaceStore } from '@/stores/workspace'
import { createIdempotencyKey } from '@/utils/idempotency'

const workspaceStore = useWorkspaceStore()
const props = defineProps<{ scopeType: PlatformMembershipScopeType }>()
const scopeType = computed(() => props.scopeType)
const canWrite = computed(() => workspaceStore.can('platform.organizations.write'))
const pageTitle = computed(() => scopeType.value === 'provider' ? '资源供应商管理' : '租户管理')
const pageDescription = computed(() => scopeType.value === 'provider' ? '维护资源供应商的基本信息、域名标识和生命周期。' : '维护消费租户的基本信息和生命周期。')
const createLabel = computed(() => scopeType.value === 'provider' ? '新建资源供应商' : '新建租户')
const searchPlaceholder = computed(() => scopeType.value === 'provider' ? '搜索供应商名称、标识或域名' : '搜索租户名称或标识')
const pageNotice = computed(() => scopeType.value === 'provider'
  ? '域名标识全局唯一且允许修改；修改提交后旧域名立即下线，不保留兼容别名。资源实例仍需切换到资源工作域维护。'
  : '租户是成员、资源和访问授权的业务边界。暂停租户前，请先核对管理员、成员和资源影响。')
const loading = ref(false)
const errorMessage = ref('')
const items = ref<PlatformOrganization[]>([])
const filters = reactive<{ status: PlatformOrganizationStatus | ''; search: string }>({ status: '', search: '' })
const pagination = reactive({ page: 1, size: 20, total: 0 })
const saving = ref(false)
const formDialog = reactive<{ visible: boolean; mode: 'create' | 'edit'; organization?: PlatformOrganization }>({ visible: false, mode: 'create' })
const form = reactive<{ key: string; name: string; domainScope: 'root' | 'named'; domainLabel: string; domainConfirmation: string; reason: string }>({ key: '', name: '', domainScope: 'named', domainLabel: '', domainConfirmation: '', reason: '' })
const transitionDialog = reactive<{ visible: boolean; action: 'suspend' | 'resume'; organization?: PlatformOrganization; reason: string }>({ visible: false, action: 'suspend', reason: '' })
const normalizedDomainLabel = computed(() => form.domainLabel.trim().toLowerCase())
const domainLabelChanged = computed(() => scopeType.value === 'provider' && form.domainScope === 'named' && formDialog.mode === 'edit' && normalizedDomainLabel.value !== (formDialog.organization?.domain_label || ''))
const domainLabelValid = computed(() => scopeType.value !== 'provider' || form.domainScope === 'root' || /^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$/.test(normalizedDomainLabel.value))
const organizationFormValid = computed(() => /^[a-z0-9](?:[a-z0-9-]{0,98}[a-z0-9])?$/.test(form.key) && !!form.name.trim() && !!form.reason.trim() && domainLabelValid.value && (!domainLabelChanged.value || form.domainConfirmation.trim().toLowerCase() === normalizedDomainLabel.value))

const load = async () => {
  loading.value = true
  errorMessage.value = ''
  try {
    const response = await getPlatformOrganizations({
      scope_type: scopeType.value,
      status: filters.status || undefined,
      search: filters.search.trim() || undefined,
      page: pagination.page,
      size: pagination.size
    })
    items.value = response.success && response.data ? response.data : []
    pagination.total = response.total || 0
  } catch {
    items.value = []
    pagination.total = 0
    errorMessage.value = '请确认平台组织读取权限和服务状态后重试。'
  } finally {
    loading.value = false
  }
}
const search = () => { pagination.page = 1; load() }
const scopeLabel = (scope: PlatformMembershipScopeType) => scope === 'provider' ? 'Provider' : 'Tenant'
const statusLabel = (status: PlatformOrganizationStatus) => status === 'active' ? '正常' : status === 'suspended' ? '已暂停' : '已退役'
const statusType = (status: PlatformOrganizationStatus) => status === 'active' ? 'success' : status === 'suspended' ? 'warning' : 'info'
const formatTime = (value: string) => new Date(value).toLocaleString('zh-CN', { hour12: false })
const openCreate = () => { formDialog.mode = 'create'; formDialog.organization = undefined; form.key = ''; form.name = ''; form.domainScope = 'named'; form.domainLabel = ''; form.domainConfirmation = ''; form.reason = ''; formDialog.visible = true }
const openEdit = (organization: PlatformOrganization) => { formDialog.mode = 'edit'; formDialog.organization = organization; form.key = organization.key; form.name = organization.name; form.domainScope = organization.domain_scope || 'named'; form.domainLabel = organization.domain_label || ''; form.domainConfirmation = ''; form.reason = ''; formDialog.visible = true }
const openTransition = (organization: PlatformOrganization) => { transitionDialog.organization = organization; transitionDialog.action = organization.status === 'active' ? 'suspend' : 'resume'; transitionDialog.reason = ''; transitionDialog.visible = true }
const saveOrganization = async () => {
  if (!canWrite.value || !organizationFormValid.value) return
  saving.value = true
  try {
    const domainFields = scopeType.value === 'provider' ? { domain_scope: form.domainScope, domain_label: form.domainScope === 'root' ? '' : normalizedDomainLabel.value } : {}
    if (formDialog.mode === 'create') await createPlatformOrganization(scopeType.value, { key: form.key, name: form.name.trim(), ...domainFields, reason: form.reason.trim() }, createIdempotencyKey())
    else if (formDialog.organization) await updatePlatformOrganization(formDialog.organization, { name: form.name.trim(), ...domainFields, domain_change_confirmation: domainLabelChanged.value ? form.domainConfirmation.trim().toLowerCase() : undefined, reason: form.reason.trim() })
    ElMessage.success(formDialog.mode === 'create' ? '组织对象已创建' : '组织信息已更新'); formDialog.visible = false; await load()
  } catch (error) { if (!(error as { isAxiosError?: boolean })?.isAxiosError) ElMessage.error('组织保存失败，请重试') } finally { saving.value = false }
}
const saveTransition = async () => {
  if (!canWrite.value || !transitionDialog.organization || !transitionDialog.reason.trim()) return
  saving.value = true
  try {
    await transitionPlatformOrganization(transitionDialog.organization, transitionDialog.action, transitionDialog.reason.trim(), createIdempotencyKey())
    ElMessage.success(transitionDialog.action === 'suspend' ? '组织已暂停' : '组织已恢复'); transitionDialog.visible = false; await load()
  } catch (error) { if (!(error as { isAxiosError?: boolean })?.isAxiosError) ElMessage.error('组织状态变更失败，请重试') } finally { saving.value = false }
}

onMounted(load)
watch(scopeType, () => { filters.status = ''; filters.search = ''; pagination.page = 1; load() })
</script>

<style scoped>
.organization-page { width: 100%; }
.state-alert { margin-top: 12px; }
.list-surface { margin-top: 14px; overflow: hidden; border: 1px solid var(--border-light); border-radius: 6px; background: #fff; }
.toolbar { display: flex; align-items: center; gap: 10px; padding: 14px 16px; border-bottom: 1px solid var(--border-light); }
.toolbar .el-input { width: 310px; }
.toolbar .el-select { width: 160px; }
.toolbar > span { margin-left: auto; color: var(--text-secondary); font-size: 12px; }
.secondary { display: block; margin-top: 3px; color: var(--text-secondary); font-size: 12px; }
.pagination { display: flex; justify-content: flex-end; padding: 16px; }
.organization-form { margin-top: 16px; }
.domain-label { font-family: ui-monospace, SFMono-Regular, Consolas, monospace; }
.field-help { display: block; margin-top: 6px; color: var(--text-secondary); font-size: 12px; }
.domain-alert { margin-bottom: 12px; }
.domain-change-preview { display: grid; gap: 6px; margin-bottom: 14px; padding: 13px; border: 1px solid var(--danger-color, #d94b4b); border-radius: 6px; background: #fff4f4; color: var(--text-secondary); font-size: 12px; }
.domain-change-preview strong { color: var(--danger-color, #c53b3b); }
.impact-preview { display: grid; gap: 6px; margin-top: 16px; padding: 14px; border: 1px solid var(--border-light); border-radius: 6px; background: var(--bg-light); color: var(--text-secondary); font-size: 13px; }
.impact-preview strong { color: var(--text-primary); font-size: 15px; }
</style>
