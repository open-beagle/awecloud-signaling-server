<template>
  <div class="provider-page">
    <PageHeader title="技术资源" description="管理当前资源方的 Agent 部署位置、运行能力和所属 Endpoint。">
      <template #actions>
        <el-button :icon="Refresh" :loading="loading" @click="load">刷新</el-button>
		<el-button type="primary" :icon="Plus" :disabled="!canWrite" @click="openCreate">创建 Agent</el-button>
      </template>
    </PageHeader>

    <el-alert
      v-if="errorMessage"
      class="state-alert"
      title="技术资源加载失败"
      :description="errorMessage"
      type="error"
      show-icon
      :closable="false"
    >
      <template #default><el-button link type="primary" @click="load">重新加载</el-button></template>
    </el-alert>

    <section class="data-surface">
      <div class="toolbar">
        <el-input v-model="filters.search" class="search-input" clearable :prefix-icon="Search" placeholder="搜索 Agent 名称或稳定标识" @keyup.enter="applyFilters" @clear="applyFilters" />
        <el-select v-model="filters.state" class="filter-select" clearable placeholder="全部生命周期" @change="applyFilters">
          <el-option label="待注册" value="pending" />
          <el-option label="已注册" value="registered" />
          <el-option label="维护中" value="disabled" />
          <el-option label="已退役" value="retired" />
        </el-select>
        <span class="result-count">{{ pagination.total }} 个 Agent</span>
      </div>

      <el-table v-loading="loading" :data="items" stripe>
        <el-table-column label="Agent" min-width="250">
          <template #default="{ row }">
              <el-link class="resource-name" type="primary" :underline="false" @click="openDetail(row.id)">{{ row.display_name || row.domain_label }}</el-link>
            <span class="secondary"><span class="mono">{{ row.stable_key }}</span><template v-if="row.host_domain_label"> · SSH {{ row.host_domain_label }}</template></span>
          </template>
        </el-table-column>
		<el-table-column label="域名命名空间" min-width="210"><template #default="{ row }"><span class="mono">*.{{ row.domain_namespace }}.beagle</span></template></el-table-column>
		<el-table-column label="Endpoints" width="120" align="center"><template #default="{ row }"><el-button link type="primary" @click="openDetail(row.id, 'endpoints')">{{ row.endpoint_count }}</el-button></template></el-table-column>
        <el-table-column label="生命周期" width="120"><template #default="{ row }"><el-tag size="small" :type="lifecycleTag(row.lifecycle_state)">{{ lifecycleLabel(row.lifecycle_state) }}</el-tag></template></el-table-column>
        <el-table-column label="健康" width="110"><template #default="{ row }"><el-tag size="small" effect="plain" :type="healthTag(row.health_state)">{{ healthLabel(row.health_state) }}</el-tag></template></el-table-column>
        <el-table-column label="开放能力" min-width="190"><template #default="{ row }"><div class="capabilities"><el-tag v-for="capability in capabilityLabels(row)" :key="capability" size="small" effect="plain" type="info">{{ capability }}</el-tag><span v-if="capabilityLabels(row).length === 0" class="secondary inline">未开放</span></div></template></el-table-column>
        <el-table-column label="版本" width="130"><template #default="{ row }"><strong>{{ row.version || '-' }}</strong></template></el-table-column>
        <el-table-column label="最后上报" width="180"><template #default="{ row }">{{ formatTime(row.last_received_at) }}</template></el-table-column>
        <el-table-column label="" width="62" fixed="right" align="center">
          <template #default="{ row }">
            <el-dropdown trigger="click" @command="command => handleRowCommand(command, row)">
              <el-button class="row-actions-button" text :loading="deletingResourceId === row.id" aria-label="更多操作" @click.stop>
                <el-icon v-if="deletingResourceId !== row.id" class="row-actions-icon"><MoreFilled /></el-icon>
              </el-button>
              <template #dropdown>
                <el-dropdown-menu>
                  <el-dropdown-item command="detail">查看详情</el-dropdown-item>
                  <el-dropdown-item v-if="canDelete(row)" command="delete" :disabled="!canWrite" divided>删除</el-dropdown-item>
                </el-dropdown-menu>
              </template>
            </el-dropdown>
          </template>
        </el-table-column>
      </el-table>

      <el-empty v-if="!loading && !errorMessage && items.length === 0" description="当前资源方没有符合条件的 Agent" />
      <div class="pagination"><el-pagination v-model:current-page="pagination.page" v-model:page-size="pagination.size" :total="pagination.total" :page-sizes="[20, 50, 100]" layout="total, sizes, prev, pager, next" @size-change="load" @current-change="load" /></div>
    </section>

	<el-dialog v-model="createDialog" title="创建 Agent" width="620px" destroy-on-close>
		<el-form label-position="top">
			<el-form-item label="Agent 名称" required><el-input v-model="createForm.name" maxlength="100" placeholder="例如 beijing" /></el-form-item>
			<el-form-item label="Agent 域名标识" required><el-input v-model="createForm.domainLabel" maxlength="63" placeholder="例如 beijing" /><span class="field-help">逻辑集群命名空间，创建后与主机名独立。</span></el-form-item>
			<el-form-item label="首台 Node 名称" required><el-input v-model="createForm.nodeName" maxlength="63" placeholder="例如 beagle-242" /><span class="field-help">用于本次部署凭据和默认 SSH 主机标签。</span></el-form-item>
			<el-form-item label="Token 有效期"><el-input-number v-model="createForm.ttlMinutes" :min="1" :max="1440" :step="10" /> <span class="form-suffix">分钟</span></el-form-item>
			<el-form-item label="创建原因" required><el-input v-model="createForm.reason" type="textarea" :rows="3" maxlength="500" show-word-limit /></el-form-item>
		</el-form>
		<template #footer><el-button @click="createDialog = false">取消</el-button><el-button type="primary" :loading="creating" :disabled="!createFormValid" @click="createAgent">创建并生成命令</el-button></template>
	</el-dialog>

	<el-dialog v-model="commandDialog" title="部署 Agent" width="760px" :close-on-click-modal="false">
		<el-alert title="部署 Token 仅可成功注册一次，请妥善保管。" type="warning" show-icon :closable="false" />
		<div class="command-box"><code>{{ installCommand }}</code><el-button :icon="DocumentCopy" circle title="复制安装命令" @click="copyCommand" /></div>
		<template #footer><el-button type="primary" @click="commandDialog = false">完成</el-button></template>
	</el-dialog>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { DocumentCopy, MoreFilled, Plus, Refresh, Search } from '@element-plus/icons-vue'
import { useRouter } from 'vue-router'
import PageHeader from '@/components/Common/PageHeader.vue'
import { checkProviderTechnicalResourceDelete, createProviderAgent, createProviderDeploymentCredential, deleteProviderTechnicalResource, getProviderTechnicalResources, type TechnicalResource, type TechnicalResourceState } from '@/api/providerSupply'
import { useWorkspaceStore } from '@/stores/workspace'

const workspaceStore = useWorkspaceStore()
const router = useRouter()
const loading = ref(false)
const errorMessage = ref('')
const items = ref<TechnicalResource[]>([])
const filters = reactive({ search: '', state: '' })
const pagination = reactive({ page: 1, size: 20, total: 0 })
const createDialog = ref(false)
const commandDialog = ref(false)
const creating = ref(false)
const deletingResourceId = ref('')
const installCommand = ref('')
const createForm = reactive({ name: '', domainLabel: '', nodeName: '', ttlMinutes: 30, reason: '' })
const canWrite = computed(() => workspaceStore.can('provider.technical_resources.write'))
const agentNameValid = computed(() => /^[a-z0-9](?:[a-z0-9-]{0,98}[a-z0-9])?$/.test(createForm.name.trim()))
const domainLabelValid = computed(() => /^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$/.test(createForm.domainLabel.trim()))
const nodeNameValid = computed(() => /^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$/.test(createForm.nodeName.trim()))
const createFormValid = computed(() => agentNameValid.value && domainLabelValid.value && nodeNameValid.value && !!createForm.reason.trim())

const createdResourceId = (response: Awaited<ReturnType<typeof createProviderAgent>>) => {
	if (!response.success || !response.data) return ''
	if ('result' in response.data) return response.data.result?.id || ''
	return response.data.id || ''
}

const load = async () => {
  const providerId = workspaceStore.providerId
  if (!providerId) {
    items.value = []
    pagination.total = 0
    errorMessage.value = '当前没有有效的资源方上下文。'
    return
  }
  loading.value = true
  errorMessage.value = ''
  try {
    const response = await getProviderTechnicalResources(providerId, {
      search: filters.search.trim() || undefined,
      state: filters.state || undefined,
      page: pagination.page,
      size: pagination.size,
    })
    items.value = response.success && response.data ? response.data : []
    pagination.total = response.total || 0
  } catch {
    items.value = []
    pagination.total = 0
    errorMessage.value = '请确认当前资源方权限和服务状态后重试。'
  } finally {
    loading.value = false
  }
}

const openCreate = async () => {
	if (!workspaceStore.providerId) return
	createForm.name = ''
	createForm.domainLabel = ''
	createForm.nodeName = ''
	createForm.reason = ''
	createForm.ttlMinutes = 30
	createDialog.value = true
}

const createAgent = async () => {
	if (!workspaceStore.providerId || !agentNameValid.value) return
	creating.value = true
	try {
		const created = await createProviderAgent(workspaceStore.providerId, createForm.name.trim(), createForm.domainLabel.trim(), createForm.reason.trim())
		const resourceId = createdResourceId(created)
		if (!resourceId) {
			ElMessage.error('Agent 已创建，但服务端未返回资源 ID，请刷新列表后重试生成部署命令。')
			await load()
			return
		}
		const credential = await createProviderDeploymentCredential(workspaceStore.providerId, resourceId, createForm.nodeName.trim(), createForm.ttlMinutes)
		if (!credential.success || !credential.data?.install_command) {
			ElMessage.error('部署命令生成失败，请刷新列表后重试。')
			await load()
			return
		}
		installCommand.value = credential.data.install_command
		createDialog.value = false
		commandDialog.value = true
		await load()
	} finally { creating.value = false }
}

const copyCommand = async () => {
	await navigator.clipboard.writeText(installCommand.value)
	ElMessage.success('安装命令已复制')
}

const applyFilters = () => { pagination.page = 1; load() }
const openDetail = (resourceId: string, tab?: string) => router.push({ path: `/provider-technical-resources/${resourceId}`, query: tab ? { tab } : undefined })
const canDelete = (resource: TechnicalResource) =>
  (resource.lifecycle_state === 'retired' || resource.health_state === 'offline' || (resource.lifecycle_state === 'pending' && (resource.endpoint_count || 0) === 0)) &&
  resource.lifecycle_state !== 'deleted'
const handleRowCommand = async (command: string, resource: TechnicalResource) => {
  if (command === 'detail') {
    await openDetail(resource.id)
    return
  }
  if (command !== 'delete' || !canWrite.value || !canDelete(resource) || !workspaceStore.providerId) return

  deletingResourceId.value = resource.id
  try {
    const check = await checkProviderTechnicalResourceDelete(workspaceStore.providerId, resource.id)
    if (!check.success || !check.data.allowed) {
      const blockers = check.data?.blockers.map(item => `${item.message}${item.count > 1 ? `（${item.count} 项）` : ''}`).join('\n') || '当前资源不允许删除。'
      await ElMessageBox.alert(blockers, '无法删除技术资源', { confirmButtonText: '知道了', type: 'warning' })
      return
    }
    const result = await ElMessageBox.prompt(`删除 ${resource.hostname || '该资源'} 后不可恢复，请输入删除原因。`, '确认删除技术资源', {
      confirmButtonText: '确认删除',
      cancelButtonText: '取消',
      inputPattern: /\S+/,
      inputErrorMessage: '请输入删除原因',
      type: 'warning',
    })
    await deleteProviderTechnicalResource(workspaceStore.providerId, resource, result.value.trim())
    ElMessage.success('技术资源已删除')
    await load()
  } catch (error) {
    if (error !== 'cancel' && error !== 'close') throw error
  } finally {
    deletingResourceId.value = ''
  }
}
const capabilityLabels = (row: TechnicalResource) => [
  row.ssh_enabled ? 'SSH' : '',
  row.container_ssh_enabled ? 'ContainerSSH' : '',
  row.k8s_enabled ? 'Kubernetes API' : '',
  row.svc_enabled ? 'Kubernetes Service' : '',
  row.endpoint_access_enabled ? 'Endpoint 接入' : '',
].filter(Boolean)
const lifecycleLabel = (state: TechnicalResourceState) => ({ pending: '待部署', registered: '已注册', disabled: '维护中', retired: '已退役', deleted: '已删除' }[state])
const lifecycleTag = (state: TechnicalResourceState) => ({ pending: 'warning', registered: 'success', disabled: 'warning', retired: 'info', deleted: 'info' }[state] as any)
const healthLabel = (state: string) => ({ unknown: '未知', online: '在线', degraded: '异常', offline: '离线' }[state] || state)
const healthTag = (state: string) => ({ online: 'success', degraded: 'warning', offline: 'danger', unknown: 'info' }[state] || 'info') as any
const formatTime = (value?: string) => value ? new Date(value).toLocaleString('zh-CN', { hour12: false }) : '-'

watch(() => workspaceStore.providerId, () => { pagination.page = 1; load() })
onMounted(load)
</script>

<style scoped>
.provider-page { width: 100%; }
.state-alert { margin-bottom: 14px; }
.data-surface { overflow: hidden; border: 1px solid var(--border-light); border-radius: 6px; background: #fff; }
.toolbar { display: flex; align-items: center; gap: 10px; padding: 14px 16px; border-bottom: 1px solid var(--border-light); }
.search-input { width: 300px; }
.filter-select { width: 160px; }
.result-count { margin-left: auto; color: var(--text-secondary); font-size: 12px; }
.field-help { display: block; margin-top: 6px; color: var(--text-secondary); font-size: 12px; }
.secondary { display: block; margin-top: 3px; color: var(--text-secondary); font-size: 12px; }
.secondary.inline { display: inline; margin-top: 0; }
.mono { font-family: 'SFMono-Regular', Consolas, 'Liberation Mono', monospace; }
.resource-name { max-width: 100%; font-weight: 650; }
.capabilities { display: flex; flex-wrap: wrap; gap: 4px; }
.row-actions-button { width: 32px; height: 32px; padding: 0; }
.row-actions-icon { transform: rotate(90deg); }
.pagination { display: flex; justify-content: flex-end; padding: 16px; }
.form-suffix { margin-left: 8px; color: var(--text-secondary); }
.command-box { display: flex; align-items: flex-start; gap: 12px; margin-top: 14px; padding: 14px; border: 1px solid var(--border-light); border-radius: 5px; background: #f7f8fa; }
.command-box code { flex: 1; overflow-wrap: anywhere; line-height: 1.7; font-family: 'SFMono-Regular', Consolas, 'Liberation Mono', monospace; }
</style>
