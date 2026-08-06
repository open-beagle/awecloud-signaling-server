<template>
  <div class="provider-page">
    <PageHeader :title="resourceTitle" :description="resourceDescription">
      <template #actions>
        <el-button :icon="ArrowLeft" @click="returnFromDetail">{{ resource?.type === 'endpoint' ? '返回 Agent' : '返回列表' }}</el-button>
        <el-button :icon="Refresh" :loading="loading" @click="load">刷新</el-button>
		<el-button v-if="resource && ['registered', 'disabled'].includes(resource.lifecycle_state)" :icon="Edit" :disabled="!canWrite || !hasBinding" @click="openCapabilities">编辑能力</el-button>
		<el-button v-if="resource && ['registered', 'disabled'].includes(resource.lifecycle_state)" type="primary" :icon="Upload" :disabled="!canWrite || !hasBinding || resource.updater_protocol !== 'v1'" @click="openUpdate">更新</el-button>
		<el-dropdown v-if="resource && resource.lifecycle_state !== 'deleted'" trigger="click" @command="handleLifecycleCommand">
			<el-button :icon="MoreFilled" :disabled="!canWrite">更多操作</el-button>
			<template #dropdown><el-dropdown-menu>
				<el-dropdown-item v-if="resource.lifecycle_state === 'registered'" command="maintenance">进入维护</el-dropdown-item>
				<el-dropdown-item v-if="resource.lifecycle_state === 'disabled'" command="resume">恢复服务</el-dropdown-item>
				<el-dropdown-item v-if="resource.lifecycle_state !== 'retired'" command="retire" divided>退役</el-dropdown-item>
				<el-dropdown-item v-else command="delete-check" divided>删除检查</el-dropdown-item>
			</el-dropdown-menu></template>
		</el-dropdown>
      </template>
    </PageHeader>

    <el-alert v-if="errorMessage" class="state-alert" title="技术资源详情加载失败" :description="errorMessage" type="error" show-icon :closable="false" />

    <template v-if="resource">
      <section class="summary-surface">
        <div class="summary-identity">
          <span class="resource-icon"><el-icon><Cpu v-if="resource.type === 'agent'" /><Connection v-else /></el-icon></span>
          <div><h2>{{ resourceTitle }}</h2><p>{{ typeLabel(resource.type) }}<template v-if="resource.parent_hostname"> · 父 Agent {{ resource.parent_hostname }}</template></p></div>
        </div>
        <div class="summary-item"><span>生命周期</span><el-tag size="small" :type="lifecycleTag(resource.lifecycle_state)">{{ lifecycleLabel(resource.lifecycle_state) }}</el-tag></div>
        <div class="summary-item"><span>健康</span><el-tag size="small" effect="plain" :type="healthTag(resource.health_state)">{{ healthLabel(resource.health_state) }}</el-tag></div>
        <div class="summary-item"><span>当前版本</span><strong>{{ resource.version || '-' }}</strong></div>
      </section>

      <el-tabs v-model="activeTab" class="detail-tabs">
        <el-tab-pane label="概览" name="overview">
          <section class="detail-surface">
            <h3>运行概况</h3>
            <el-descriptions :column="4" border>
              <el-descriptions-item v-if="resource.type === 'agent'" label="Agent 名称"><span>{{ resource.display_name || resource.domain_label }}</span><el-button v-if="canWrite" link type="primary" @click="editAgentDisplayName">编辑</el-button></el-descriptions-item>
              <el-descriptions-item v-else label="Endpoint 名称"><span>{{ resource.display_name || resource.hostname || '-' }}</span></el-descriptions-item>
              <el-descriptions-item v-if="resource.type === 'agent'" label="SSH 主机域名标识"><span class="mono">{{ resource.host_domain_label || '待配置' }}</span></el-descriptions-item>
              <el-descriptions-item label="SSH 域名"><span class="mono">{{ sshHostname }}</span></el-descriptions-item>
              <el-descriptions-item label="类型">{{ typeLabel(resource.type) }}</el-descriptions-item>
              <el-descriptions-item label="父 Agent">{{ resource.parent_hostname || '-' }}</el-descriptions-item>
			  <el-descriptions-item v-if="resource.type === 'agent'" label="域名命名空间"><span class="mono">*.{{ resource.domain_namespace }}.beagle</span><el-button v-if="canWrite" link type="primary" @click="editAgentDomainLabel">编辑</el-button></el-descriptions-item>
              <el-descriptions-item label="最后上报">{{ formatTime(resource.last_received_at) }}</el-descriptions-item>
              <el-descriptions-item label="租约到期">{{ formatTime(resource.lease_expires_at) }}</el-descriptions-item>
              <el-descriptions-item label="库存进度">seq {{ resource.last_sequence }}</el-descriptions-item>
              <el-descriptions-item label="观测 Revision">{{ resource.observed_revision }}</el-descriptions-item>
            </el-descriptions>
          </section>
		  <section class="detail-surface">
			<h3>部署实例</h3>
			<el-table v-if="bindings.length" :data="bindings" stripe>
				<el-table-column label="实例类型" width="150"><template #default="{ row }">{{ row.source_type === 'legacy_node' ? 'Agent Node' : 'Endpoint' }}</template></el-table-column>
				<el-table-column label="实例 ID" prop="source_id" min-width="220"><template #default="{ row }"><span class="mono">{{ row.source_id }}</span></template></el-table-column>
				<el-table-column label="状态" width="120"><template #default="{ row }"><el-tag size="small" :type="row.enabled ? 'success' : 'info'">{{ row.enabled ? '生效' : '停用' }}</el-tag></template></el-table-column>
				<el-table-column label="凭据 Revision" prop="credential_revision" width="150" />
				<el-table-column label="绑定时间" width="190"><template #default="{ row }">{{ formatTime(row.created_at) }}</template></el-table-column>
			</el-table>
			<el-empty v-else description="暂无部署实例" />
		  </section>
        </el-tab-pane>
        <el-tab-pane v-if="resource.type === 'agent'" :label="`Endpoints ${endpoints.length}`" name="endpoints">
          <section class="detail-surface">
            <h3>所属 Endpoints</h3>
            <el-table v-if="endpoints.length" :data="endpoints" stripe>
              <el-table-column label="Endpoint" min-width="210">
                <template #default="{ row }">
                  <el-link class="endpoint-name" type="primary" :underline="false" @click="openEndpoint(row.id)">{{ endpointName(row) }}</el-link>
                  <span class="secondary mono">{{ row.stable_key }}</span>
                </template>
              </el-table-column>
              <el-table-column label="SSH 域名" min-width="260"><template #default="{ row }"><span class="mono">{{ endpointDomain(row) }}</span></template></el-table-column>
              <el-table-column label="健康" width="110"><template #default="{ row }"><el-tag size="small" effect="plain" :type="healthTag(row.health_state)">{{ healthLabel(row.health_state) }}</el-tag></template></el-table-column>
              <el-table-column label="开放能力" min-width="190"><template #default="{ row }"><div class="endpoint-capabilities"><el-tag v-for="capability in endpointCapabilityLabels(row)" :key="capability" size="small" effect="plain" type="info">{{ capability }}</el-tag><span v-if="endpointCapabilityLabels(row).length === 0" class="secondary inline">未开放</span></div></template></el-table-column>
              <el-table-column label="版本" width="130"><template #default="{ row }"><strong>{{ row.version || '-' }}</strong><span class="secondary">{{ row.updater_protocol ? `Updater ${row.updater_protocol}` : '不支持远程更新' }}</span></template></el-table-column>
              <el-table-column label="最后上报" width="180"><template #default="{ row }">{{ formatTime(row.last_received_at) }}</template></el-table-column>
              <el-table-column label="" width="72" align="center"><template #default="{ row }"><el-button link type="primary" @click="openEndpoint(row.id)">查看</el-button></template></el-table-column>
            </el-table>
            <el-empty v-else description="当前 Agent 没有 Endpoint" />
          </section>
        </el-tab-pane>
        <el-tab-pane label="能力" name="capabilities">
          <section class="detail-surface">
            <h3>当前开放能力</h3>
            <div class="capability-list">
              <div v-for="item in capabilityRows" :key="item.label" class="capability-row"><div><strong>{{ item.label }}</strong><span>{{ item.description }}</span></div><el-tag size="small" :type="item.enabled ? 'success' : 'info'">{{ item.enabled ? '已启用' : '未启用' }}</el-tag></div>
            </div>
          </section>
        </el-tab-pane>
		<el-tab-pane label="更新记录" name="events"><section class="detail-surface">
			<el-table v-if="updateTasks.length" :data="updateTasks" stripe><el-table-column label="目标版本" prop="desired_version" width="150" /><el-table-column label="状态" width="140"><template #default="{ row }"><el-tag size="small" :type="updateStatusTag(row.status)">{{ updateStatusLabel(row.status) }}</el-tag></template></el-table-column><el-table-column label="发起时间" width="190"><template #default="{ row }">{{ formatTime(row.created_at) }}</template></el-table-column><el-table-column label="结果"><template #default="{ row }">{{ row.last_error_message || '-' }}</template></el-table-column></el-table>
			<el-empty v-else description="暂无更新任务" />
		</section></el-tab-pane>
        <el-tab-pane label="诊断" name="diagnostics">
          <section class="detail-surface">
            <el-alert title="以下字段仅用于身份对账和故障诊断，不作为日常资源名称。" type="warning" :closable="false" show-icon />
            <el-descriptions class="diagnostic-grid" :column="2" border>
              <el-descriptions-item label="TechnicalResource ID"><span class="mono">{{ resource.id }}</span></el-descriptions-item>
              <el-descriptions-item label="Stable key"><span class="mono">{{ resource.stable_key }}</span></el-descriptions-item>
              <el-descriptions-item label="Agent 上报主机名"><span class="mono">{{ resource.hostname || '-' }}</span></el-descriptions-item>
              <el-descriptions-item label="上报名称来源">{{ hostnameSourceLabel(resource.hostname_source) }}</el-descriptions-item>
              <el-descriptions-item label="Credential Revision">{{ resource.credential_revision }}</el-descriptions-item>
              <el-descriptions-item label="Updater 协议">{{ resource.updater_protocol || '-' }}</el-descriptions-item>
              <el-descriptions-item label="Config Revision">{{ resource.config_revision }}</el-descriptions-item>
              <el-descriptions-item label="Row Version">{{ resource.row_version }}</el-descriptions-item>
            </el-descriptions>
          </section>
        </el-tab-pane>
      </el-tabs>
    </template>

	<el-dialog v-model="capabilityDialog" title="编辑开放能力" width="680px" destroy-on-close>
		<el-form v-if="capabilityForm" label-position="top" class="capability-form">
			<div class="switch-grid">
				<el-form-item label="SSH"><el-switch v-model="capabilityForm.ssh_enabled" /></el-form-item>
				<el-form-item label="Kubernetes API"><el-switch v-model="capabilityForm.k8s_enabled" /></el-form-item>
				<el-form-item label="Kubernetes Service"><el-switch v-model="capabilityForm.svc_enabled" /></el-form-item>
				<el-form-item v-if="resource?.type === 'agent'" label="Endpoint 接入"><el-switch v-model="capabilityForm.endpoint_access_enabled" /></el-form-item>
			</div>
			<el-form-item v-if="resource?.type === 'endpoint' && capabilityForm.ssh_enabled" label="SSH 登录用户"><el-select v-model="capabilityForm.ssh_users" multiple allow-create filterable default-first-option placeholder="输入系统用户名后回车" /></el-form-item>
			<el-form-item v-if="capabilityForm.k8s_enabled" label="API Server 地址"><el-input v-model="capabilityForm.k8s_api_address" placeholder="https://127.0.0.1:6443" /></el-form-item>
			<el-form-item v-if="capabilityForm.svc_enabled" label="Namespace"><el-select v-model="capabilityForm.svc_namespaces" multiple allow-create filterable default-first-option placeholder="输入 Namespace 后回车" /></el-form-item>
			<el-form-item v-if="capabilityForm.svc_enabled" label="标签选择器"><el-input v-model="capabilityForm.svc_label_selector" placeholder="app.kubernetes.io/name=example" /></el-form-item>
		</el-form>
		<template #footer><el-button @click="capabilityDialog = false">取消</el-button><el-button type="primary" :loading="submitting" @click="saveCapabilities">保存配置</el-button></template>
	</el-dialog>

	<el-dialog v-model="updateDialog" title="创建更新任务" width="560px" destroy-on-close>
		<el-form label-position="top">
			<el-form-item label="目标版本" required><el-select v-model="updateForm.releaseId" style="width: 100%" placeholder="选择已发布版本"><el-option v-for="release in releases" :key="release.id" :label="`${release.version} · ${release.channel}`" :value="release.id" /></el-select></el-form-item>
			<el-form-item label="更新原因" required><el-input v-model="updateForm.reason" type="textarea" :rows="3" maxlength="500" show-word-limit /></el-form-item>
			<el-form-item><el-checkbox v-model="updateForm.force">忽略版本顺序并强制更新</el-checkbox></el-form-item>
		</el-form>
		<el-alert v-if="releases.length === 0" title="当前组件没有可用的已发布版本" type="warning" :closable="false" show-icon />
		<template #footer><el-button @click="updateDialog = false">取消</el-button><el-button type="primary" :disabled="!updateForm.releaseId || !updateForm.reason.trim()" :loading="submitting" @click="createUpdateTask">创建任务</el-button></template>
	</el-dialog>

	<el-dialog v-model="deleteCheckDialog" title="删除检查" width="600px">
		<el-result v-if="deleteCheck?.allowed" icon="success" title="可以删除" sub-title="资源依赖已清理，删除后只保留审计墓碑且不能恢复。" />
		<template v-else><el-alert title="当前不能删除" description="请先处理以下依赖，再重新检查。" type="warning" show-icon :closable="false" /><div class="blocker-list"><div v-for="blocker in deleteCheck?.blockers" :key="blocker.code"><strong>{{ blocker.message }}</strong><span>{{ blocker.count }} 项 · {{ blocker.code }}</span></div></div></template>
		<template #footer><el-button @click="deleteCheckDialog = false">关闭</el-button><el-button :loading="submitting" @click="runDeleteCheck">重新检查</el-button><el-button v-if="deleteCheck?.allowed" type="danger" :loading="submitting" @click="deleteResource">确认删除</el-button></template>
	</el-dialog>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { ArrowLeft, Connection, Cpu, Edit, MoreFilled, Refresh, Upload } from '@element-plus/icons-vue'
import PageHeader from '@/components/Common/PageHeader.vue'
import {
	checkProviderTechnicalResourceDelete, createProviderTechnicalResourceUpdateTask, deleteProviderTechnicalResource,
	getProviderTechnicalResource, getProviderTechnicalResourceCapabilities, getProviderTechnicalResourceReleases,
	getProviderTechnicalResourceUpdateTasks, setProviderTechnicalResourceLifecycle, updateProviderAgentDisplayName, updateProviderAgentDomainLabel, updateProviderTechnicalResourceCapabilities,
	type ProviderRelease, type ProviderUpdateTask, type TechnicalResource, type TechnicalResourceBinding, type TechnicalResourceCapabilities,
	type TechnicalResourceDeleteCheck, type TechnicalResourceState,
} from '@/api/providerSupply'
import { useWorkspaceStore } from '@/stores/workspace'

const route = useRoute()
const router = useRouter()
const workspaceStore = useWorkspaceStore()
const loading = ref(false)
const errorMessage = ref('')
const resource = ref<TechnicalResource>()
const bindings = ref<TechnicalResourceBinding[]>([])
const endpoints = ref<TechnicalResource[]>([])
const activeTab = ref(typeof route.query.tab === 'string' ? route.query.tab : 'overview')
const submitting = ref(false)
const capabilityDialog = ref(false)
const updateDialog = ref(false)
const deleteCheckDialog = ref(false)
const capabilityForm = ref<TechnicalResourceCapabilities>()
const releases = ref<ProviderRelease[]>([])
const updateTasks = ref<ProviderUpdateTask[]>([])
const deleteCheck = ref<TechnicalResourceDeleteCheck>()
const updateForm = reactive({ releaseId: '', reason: '', force: false })
const canWrite = computed(() => workspaceStore.can('provider.technical_resources.write'))
const hasBinding = computed(() => !!resource.value && resource.value.lifecycle_state !== 'pending')
const resourceTitle = computed(() => resource.value?.display_name || resource.value?.domain_label || resource.value?.hostname || '等待主机注册')
const sshHostname = computed(() => resource.value
	? resource.value.type === 'agent' && resource.value.host_domain_label
		? `${resource.value.host_domain_label}.${resource.value.domain_namespace}.beagle`
		: '-'
	: '-')
const resourceDescription = computed(() => resource.value
  ? `${typeLabel(resource.value.type)} 部署位置、运行能力、库存租约和身份诊断。`
  : '查看技术资源运行详情。')
const capabilityRows = computed(() => resource.value ? [
  { label: 'SSH', description: '主机终端访问', enabled: resource.value.ssh_enabled },
  ...(resource.value.type === 'agent' ? [{ label: 'ContainerSSH', description: '通过 Kubernetes Exec 进入容器', enabled: resource.value.container_ssh_enabled }] : []),
  { label: 'Kubernetes API', description: '代理原生 Kubernetes API', enabled: resource.value.k8s_enabled },
  { label: 'Kubernetes Service', description: '发现并代理集群服务', enabled: resource.value.svc_enabled },
  ...(resource.value.type === 'agent' ? [{ label: 'Endpoint 接入', description: '接受隔离网络 Endpoint 反向连接', enabled: resource.value.endpoint_access_enabled }] : []),
] : [])
const endpointName = (endpoint: TechnicalResource) => endpoint.display_name || endpoint.host_domain_label || endpoint.hostname || '等待主机注册'
const endpointDomain = (endpoint: TechnicalResource) => endpoint.host_domain_label && endpoint.domain_namespace
	? `${endpoint.host_domain_label}.${endpoint.domain_namespace}.beagle`
	: '-'
const endpointCapabilityLabels = (endpoint: TechnicalResource) => [
	endpoint.ssh_enabled ? 'SSH' : '',
	endpoint.k8s_enabled ? 'Kubernetes API' : '',
	endpoint.svc_enabled ? 'Kubernetes Service' : '',
].filter(Boolean)
const openEndpoint = async (endpointId: string) => {
	activeTab.value = 'overview'
	await router.push(`/provider-technical-resources/${endpointId}`)
}
const returnFromDetail = async () => {
	if (resource.value?.type === 'endpoint' && resource.value.parent_id) {
		await router.push({ path: `/provider-technical-resources/${resource.value.parent_id}`, query: { tab: 'endpoints' } })
		return
	}
	await router.push('/provider-technical-resources')
}

const editAgentDomainLabel = async () => {
	if (!resource.value || resource.value.type !== 'agent' || !workspaceStore.providerId) return
	try {
		const label = await ElMessageBox.prompt('修改会同时切换该 Agent 下全部资源域名，旧域名立即失效。', '编辑 Agent 域名标识', {
			inputValue: resource.value.domain_label,
			inputPattern: /^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$/,
			inputErrorMessage: '请输入有效的 DNS 单标签',
			confirmButtonText: '下一步', cancelButtonText: '取消', type: 'warning',
		})
		const reason = await ElMessageBox.prompt(`确认切换为 ${label.value.trim().toLowerCase()}，请输入变更原因。`, '确认域名切换', {
			inputPattern: /\S+/, inputErrorMessage: '请输入变更原因', confirmButtonText: '确认切换', cancelButtonText: '取消', type: 'warning',
		})
		await updateProviderAgentDomainLabel(workspaceStore.providerId, resource.value, label.value.trim().toLowerCase(), reason.value.trim())
		ElMessage.success('Agent 域名标识已更新')
		await load()
	} catch (error) {
		if (error !== 'cancel' && error !== 'close') throw error
	}
}

const editAgentDisplayName = async () => {
	if (!resource.value || resource.value.type !== 'agent' || !workspaceStore.providerId) return
	try {
		const result = await ElMessageBox.prompt('请输入 Agent 名称', '编辑 Agent 名称', {
			inputValue: resource.value.display_name || resource.value.domain_label,
			inputPattern: /\S+/,
			inputErrorMessage: '请输入 Agent 名称',
			confirmButtonText: '保存', cancelButtonText: '取消',
		})
		await updateProviderAgentDisplayName(workspaceStore.providerId, resource.value, result.value.trim())
		ElMessage.success('Agent 名称已更新')
		await load()
	} catch (error) {
		if (error !== 'cancel' && error !== 'close') throw error
	}
}

const load = async () => {
  const providerId = workspaceStore.providerId
  if (!providerId) return
  loading.value = true
  errorMessage.value = ''
  try {
    const response = await getProviderTechnicalResource(providerId, String(route.params.id))
	resource.value = response.success ? response.data.resource : undefined
	bindings.value = response.success ? response.data.bindings || [] : []
	endpoints.value = response.success ? response.data.endpoints || [] : []
	if (resource.value?.type === 'agent' && route.query.tab === 'endpoints') activeTab.value = 'endpoints'
	else if (resource.value?.type !== 'agent' && activeTab.value === 'endpoints') activeTab.value = 'overview'
	if (resource.value && response.data.bindings?.length) {
		const tasks = await getProviderTechnicalResourceUpdateTasks(providerId, resource.value.id)
		updateTasks.value = tasks.success ? tasks.data : []
	} else updateTasks.value = []
  } catch {
    resource.value = undefined
	bindings.value = []
	endpoints.value = []
    errorMessage.value = '请确认技术资源仍属于当前资源方且当前账号具有查看权限。'
  } finally {
    loading.value = false
  }
}

const openCapabilities = async () => {
	if (!resource.value || !workspaceStore.providerId) return
	const response = await getProviderTechnicalResourceCapabilities(workspaceStore.providerId, resource.value.id)
	if (!response.success) return
	capabilityForm.value = { ...response.data, ssh_users: [...(response.data.ssh_users || [])], svc_namespaces: [...(response.data.svc_namespaces || [])] }
	capabilityDialog.value = true
}

const saveCapabilities = async () => {
	if (!resource.value || !capabilityForm.value || !workspaceStore.providerId) return
	submitting.value = true
	try {
		await updateProviderTechnicalResourceCapabilities(workspaceStore.providerId, resource.value, capabilityForm.value)
		ElMessage.success('能力配置已更新')
		capabilityDialog.value = false
		await load()
	} finally { submitting.value = false }
}

const openUpdate = async () => {
	if (!resource.value || !workspaceStore.providerId) return
	const response = await getProviderTechnicalResourceReleases(workspaceStore.providerId, resource.value.id)
	releases.value = response.success ? response.data : []
	updateForm.releaseId = releases.value[0]?.id || ''
	updateForm.reason = ''
	updateForm.force = false
	updateDialog.value = true
}

const createUpdateTask = async () => {
	if (!resource.value || !workspaceStore.providerId || !updateForm.releaseId || !updateForm.reason.trim()) return
	submitting.value = true
	try {
		await createProviderTechnicalResourceUpdateTask(workspaceStore.providerId, resource.value.id, updateForm.releaseId, updateForm.force, updateForm.reason.trim())
		ElMessage.success('更新任务已创建')
		updateDialog.value = false
		activeTab.value = 'events'
		await load()
	} finally { submitting.value = false }
}

const handleLifecycleCommand = async (command: string) => {
	if (command === 'delete-check') { deleteCheckDialog.value = true; await runDeleteCheck(); return }
	if (!resource.value || !workspaceStore.providerId) return
	const title = command === 'maintenance' ? '进入维护' : command === 'resume' ? '恢复服务' : '退役技术资源'
	const prompt = command === 'retire' ? `退役 ${resource.value.hostname || '该资源'} 后不可恢复，请输入操作原因。` : '请输入操作原因，原因将写入审计记录。'
	try {
		const result = await ElMessageBox.prompt(prompt, title, { confirmButtonText: title, cancelButtonText: '取消', inputPattern: /\S+/, inputErrorMessage: '请输入操作原因', type: command === 'retire' ? 'warning' : 'info' })
		submitting.value = true
		await setProviderTechnicalResourceLifecycle(workspaceStore.providerId, resource.value, command as 'maintenance' | 'resume' | 'retire', result.value.trim())
		ElMessage.success(`${title}成功`)
		await load()
	} catch (error) {
		if (error !== 'cancel' && error !== 'close') throw error
	} finally { submitting.value = false }
}

const runDeleteCheck = async () => {
	if (!resource.value || !workspaceStore.providerId) return
	submitting.value = true
	try {
		const response = await checkProviderTechnicalResourceDelete(workspaceStore.providerId, resource.value.id)
		deleteCheck.value = response.success ? response.data : undefined
	} finally { submitting.value = false }
}

const deleteResource = async () => {
	if (!resource.value || !workspaceStore.providerId || !deleteCheck.value?.allowed) return
	try {
		const result = await ElMessageBox.prompt(`删除 ${resource.value.hostname || '该资源'} 后不可恢复，请输入删除原因。`, '确认删除技术资源', { confirmButtonText: '确认删除', cancelButtonText: '取消', inputPattern: /\S+/, inputErrorMessage: '请输入删除原因', type: 'warning' })
		submitting.value = true
		await deleteProviderTechnicalResource(workspaceStore.providerId, resource.value, result.value.trim())
		ElMessage.success('技术资源已删除')
		deleteCheckDialog.value = false
		await returnFromDetail()
	} catch (error) {
		if (error !== 'cancel' && error !== 'close') throw error
	} finally { submitting.value = false }
}

const typeLabel = (type: string) => type === 'agent' ? 'Agent' : type === 'endpoint' ? 'Endpoint' : type
const lifecycleLabel = (state: TechnicalResourceState) => ({ pending: '待部署', registered: '已注册', disabled: '维护中', retired: '已退役', deleted: '已删除' }[state])
const lifecycleTag = (state: TechnicalResourceState) => ({ pending: 'warning', registered: 'success', disabled: 'warning', retired: 'info', deleted: 'info' }[state] as any)
const healthLabel = (state: string) => ({ unknown: '未知', online: '在线', degraded: '异常', offline: '离线' }[state] || state)
const healthTag = (state: string) => ({ online: 'success', degraded: 'warning', offline: 'danger', unknown: 'info' }[state] || 'info') as any
const hostnameSourceLabel = (source?: string) => source === 'reported' ? '主机上报' : source === 'legacy_name' ? '存量名称（待升级确认）' : '等待首次注册'
const formatTime = (value?: string) => value ? new Date(value).toLocaleString('zh-CN', { hour12: false }) : '-'
const updateStatusLabel = (status: string) => ({ pending: '等待下发', delivered: '已下发', accepted: '已接受', downloading: '下载中', verifying: '校验中', installing: '安装中', restarting: '重启中', succeeded: '成功', failed: '失败', rolled_back: '已回滚', cancelled: '已取消', expired: '已过期' }[status] || status)
const updateStatusTag = (status: string) => status === 'succeeded' ? 'success' : ['failed', 'expired'].includes(status) ? 'danger' : ['cancelled', 'rolled_back'].includes(status) ? 'info' : 'warning'

watch(() => [workspaceStore.providerId, route.params.id], load)
onMounted(load)
</script>

<style scoped>
.provider-page { width: 100%; }
.state-alert { margin-bottom: 14px; }
.summary-surface { display: grid; grid-template-columns: minmax(320px, 1.5fr) repeat(3, minmax(140px, .6fr)); margin-bottom: 14px; border: 1px solid var(--border-light); border-radius: 6px; background: #fff; }
.summary-identity, .summary-item { min-height: 88px; padding: 14px 16px; }
.summary-identity { display: flex; align-items: center; gap: 12px; }
.resource-icon { width: 40px; height: 40px; display: inline-flex; align-items: center; justify-content: center; border-radius: 6px; color: var(--primary-color); background: var(--primary-lighter); font-size: 21px; }
.summary-identity h2 { margin: 0; font-size: 18px; }
.summary-identity p { margin: 3px 0 0; color: var(--text-secondary); font-size: 12px; }
.summary-item { display: flex; flex-direction: column; justify-content: center; border-left: 1px solid var(--border-light); }
.summary-item > span { margin-bottom: 7px; color: var(--text-secondary); font-size: 11px; }
.detail-tabs { border: 1px solid var(--border-light); border-radius: 6px; background: #fff; }
.detail-tabs :deep(.el-tabs__header) { margin: 0; padding: 0 16px; }
.detail-tabs :deep(.el-tabs__content) { padding: 16px; }
.detail-surface h3 { margin: 0 0 12px; font-size: 15px; }
.capability-list { border: 1px solid var(--border-light); border-radius: 5px; }
.capability-row { min-height: 62px; display: flex; align-items: center; justify-content: space-between; padding: 10px 14px; border-bottom: 1px solid var(--border-lighter); }
.capability-row:last-child { border-bottom: 0; }
.capability-row strong, .capability-row span { display: block; }
.capability-row span { margin-top: 2px; color: var(--text-secondary); font-size: 12px; }
.endpoint-name { max-width: 100%; font-weight: 650; }
.endpoint-capabilities { display: flex; flex-wrap: wrap; gap: 4px; }
.secondary { display: block; margin-top: 3px; color: var(--text-secondary); font-size: 12px; }
.secondary.inline { display: inline; margin-top: 0; }
.diagnostic-grid { margin-top: 14px; }
.mono { font-family: 'SFMono-Regular', Consolas, 'Liberation Mono', monospace; }
.switch-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 0 24px; }
.capability-form :deep(.el-select) { width: 100%; }
.blocker-list { margin-top: 14px; border: 1px solid var(--border-light); border-radius: 5px; }
.blocker-list > div { display: flex; align-items: center; justify-content: space-between; min-height: 58px; padding: 10px 14px; border-bottom: 1px solid var(--border-lighter); }
.blocker-list > div:last-child { border-bottom: 0; }
.blocker-list span { color: var(--text-secondary); font-size: 12px; }
</style>
