<template>
  <div class="provider-page" v-loading="loading">
    <PageHeader :title="resourceTitle" :description="resourceDescription">
      <template #actions>
        <el-button :icon="ArrowLeft" @click="returnToList">返回列表</el-button>
        <el-button :icon="Refresh" :loading="loading" @click="load">刷新</el-button>
        <el-button v-if="resource?.type === 'agent'" :icon="Operation" :disabled="!canWrite || !hasBinding" @click="openCapabilities">编辑能力</el-button>
        <el-button v-if="resource" type="primary" :icon="Upload" :disabled="!canWrite || !hasBinding || !updaterSupported(resource)" @click="openUpdate(resource)">更新</el-button>
        <el-dropdown v-if="resource && resource.lifecycle_state !== 'deleted'" trigger="click" @command="handleLifecycleCommand">
          <el-button :icon="MoreFilled" :disabled="!canWrite" aria-label="更多操作" />
          <template #dropdown>
            <el-dropdown-menu>
              <el-dropdown-item v-if="resource.lifecycle_state === 'registered'" command="maintenance">进入维护</el-dropdown-item>
              <el-dropdown-item v-if="resource.lifecycle_state === 'disabled'" command="resume">恢复服务</el-dropdown-item>
              <el-dropdown-item v-if="resource.lifecycle_state !== 'retired'" command="retire" divided>退役</el-dropdown-item>
              <el-dropdown-item v-else command="delete-check" divided>删除检查</el-dropdown-item>
            </el-dropdown-menu>
          </template>
        </el-dropdown>
      </template>
    </PageHeader>

    <el-alert v-if="errorMessage" class="state-alert" title="技术资源详情加载失败" :description="errorMessage" type="error" show-icon :closable="false">
      <template #default><el-button link type="primary" @click="load">重新加载</el-button></template>
    </el-alert>

    <template v-if="resource">
      <section class="summary-surface">
        <div class="summary-identity">
          <span class="resource-icon"><el-icon><Cpu /></el-icon></span>
          <div><h2>{{ resourceTitle }}</h2><p>Agent · {{ workspaceStore.currentContext?.scope_name || resource.provider_id }}</p></div>
        </div>
        <div class="summary-item"><span>生命周期</span><el-tag size="small" :type="lifecycleTag(resource.lifecycle_state)">{{ lifecycleLabel(resource.lifecycle_state) }}</el-tag></div>
        <div class="summary-item"><span>健康</span><el-tag size="small" effect="plain" :type="healthTag(resource.health_state)">{{ healthLabel(resource.health_state) }}</el-tag></div>
        <div class="summary-item"><span>配置状态</span><el-tag size="small" :type="configStatus.type">{{ configStatus.label }}</el-tag></div>
        <div class="summary-item"><span>当前版本</span><strong>{{ resource.version || '-' }}</strong></div>
      </section>

      <el-tabs v-model="activeTab" class="detail-tabs">
        <el-tab-pane label="概览" name="overview">
          <div class="overview-stack">
            <el-alert :type="configStatus.type" :title="configStatus.title" :description="configStatus.description" show-icon :closable="false" />

            <section class="detail-section">
              <div class="section-head"><div><h3>开放能力</h3><p>展示当前有效参数、配置来源和启用状态。</p></div><el-button v-if="canWrite" :icon="Operation" @click="openCapabilities">配置</el-button></div>
              <div class="capability-list">
                <div v-for="item in capabilityRows" :key="item.label" class="capability-row">
                  <div class="capability-name"><el-icon><component :is="item.icon" /></el-icon><div><strong>{{ item.label }}</strong><span>{{ item.description }}</span></div></div>
                  <div class="capability-value"><strong class="mono">{{ item.value }}</strong><span>{{ item.valueLabel }}</span></div>
                  <span class="capability-source">{{ item.source }}</span>
                  <el-tag size="small" :type="item.enabled ? 'success' : 'info'">{{ item.enabled ? '已启用' : '未启用' }}</el-tag>
                </div>
              </div>
            </section>

            <section class="detail-section endpoint-section">
              <div v-if="capabilities?.endpoint_access_enabled" class="access-strip">
                <div><el-icon><Connection /></el-icon><span><strong>Endpoint 接入服务{{ endpointAccessReady ? '已配置' : '缺少可达地址' }}</strong><small>隔离网络节点主动连接此 Agent</small></span></div>
                <div><small>有效接入地址</small><strong class="mono">{{ endpointAccessAddress }}</strong></div>
                <div><small>认证策略</small><strong>Agent 共享令牌 · 自动注册</strong></div>
                <div><small>网络方向</small><strong>Endpoint → Agent TCP</strong></div>
              </div>
              <el-alert v-else title="Endpoint 接入未启用" description="已注册 Endpoint 仍会保留；启用能力并配置内网可达地址后才能生成安装命令。" type="info" show-icon :closable="false" />

              <div v-if="capabilities?.endpoint_access_enabled && canWrite" class="install-command">
                <div class="section-head compact"><div><h3>在目标主机安装 Endpoint</h3><p>安装时生成稳定 endpoint_id，首次认证成功后自动注册，默认仅开启 SSH。</p></div><el-button type="primary" :icon="CopyDocument" :disabled="!endpointAccess?.install_command" @click="copyInstallCommand">复制安装命令</el-button></div>
                <pre v-if="endpointAccess?.install_command">{{ endpointAccess.install_command }}</pre>
                <el-empty v-else :image-size="34" description="请先配置 Agent 内网地址，再生成安装命令" />
              </div>

              <div class="section-head endpoint-head"><div><h3>Endpoints <el-tag size="small" type="info">{{ endpoints.length }}</el-tag></h3><p>能力、域名、更新和生命周期均在当前 Agent 内维护。</p></div></div>
              <div class="table-surface">
                <div class="endpoint-toolbar">
                  <el-input v-model="endpointSearch" clearable :prefix-icon="Search" placeholder="搜索名称、域名或稳定 ID" />
                  <el-segmented v-model="endpointFilter" :options="['全部', '在线', '离线', '已停用']" />
                  <span>共 {{ filteredEndpoints.length }} 个 Endpoint</span>
                </div>
                <el-table v-if="filteredEndpoints.length" :data="filteredEndpoints" stripe>
                  <el-table-column label="Endpoint" min-width="210"><template #default="{ row }"><el-button link type="primary" class="endpoint-name" @click="openEndpoint(row)">{{ endpointName(row) }}</el-button><span class="secondary mono">{{ endpointDomain(row) }}</span></template></el-table-column>
                  <el-table-column label="状态" width="120"><template #default="{ row }"><el-tag size="small" :type="row.lifecycle_state === 'disabled' ? 'warning' : healthTag(row.health_state)">{{ row.lifecycle_state === 'disabled' ? '已停用' : healthLabel(row.health_state) }}</el-tag><span class="secondary">{{ relativeTime(row.last_received_at) }}</span></template></el-table-column>
                  <el-table-column label="开放能力" min-width="210"><template #default="{ row }"><div class="endpoint-capabilities"><el-tag v-for="capability in endpointCapabilityLabels(row)" :key="capability" size="small" effect="plain" type="info">{{ capability }}</el-tag><span v-if="endpointCapabilityLabels(row).length === 0" class="secondary inline">未开放</span></div></template></el-table-column>
                  <el-table-column label="接入信息" min-width="220"><template #default="{ row }"><span class="mono">{{ row.hostname || '-' }}</span><span class="secondary mono">{{ row.stable_key }}</span></template></el-table-column>
                  <el-table-column label="版本" width="140"><template #default="{ row }"><strong>{{ row.version || '-' }}</strong><span class="secondary">{{ endpointConfigLabel(row) }}</span></template></el-table-column>
                  <el-table-column label="操作" width="112" fixed="right"><template #default="{ row }"><el-button link :icon="Upload" :disabled="!canWrite || !updaterSupported(row)" title="更新 Endpoint" @click="openUpdate(row)" /><el-button link type="danger" :icon="Delete" :disabled="!canWrite" title="删除 Endpoint" @click="openDeleteCheck(row)" /></template></el-table-column>
                </el-table>
                <el-empty v-else :description="endpoints.length ? '没有符合条件的 Endpoint' : '当前 Agent 没有 Endpoint'" />
              </div>
            </section>

            <section class="detail-section"><div class="section-head"><div><h3>运行概况</h3><p>最近一次 Agent 上报的运行事实。</p></div></div><div class="metric-grid"><div><span>最后上报</span><strong>{{ formatTime(resource.last_received_at) }}</strong></div><div><span>租约到期</span><strong>{{ formatTime(resource.lease_expires_at) }}</strong></div><div><span>库存进度</span><strong>sequence {{ resource.last_sequence }}</strong></div><div><span>域名命名空间</span><strong class="mono">*.{{ resource.domain_namespace }}.beagle</strong></div></div></section>
          </div>
        </el-tab-pane>

        <el-tab-pane label="变更记录" name="events">
          <section class="detail-section">
            <div class="section-head"><div><h3>配置与更新变更</h3><p>按时间倒序展示当前 Agent 的配置确认和组件更新任务。</p></div></div>
            <el-timeline>
              <el-timeline-item :timestamp="formatTime(resource.updated_at)" placement="top" :type="configStatus.type"><strong>{{ configStatus.title }}</strong><p>期望 revision {{ resource.config_revision }}，已应用 revision {{ resource.observed_revision }}</p></el-timeline-item>
              <el-timeline-item v-for="task in updateTasks" :key="task.id" :timestamp="formatTime(task.created_at)" placement="top" :type="updateStatusTag(task.status)"><strong>创建 {{ task.desired_version }} 更新任务</strong><p>{{ updateStatusLabel(task.status) }}{{ task.last_error_message ? ` · ${task.last_error_message}` : '' }}</p></el-timeline-item>
            </el-timeline>
            <el-empty v-if="updateTasks.length === 0" description="暂无组件更新记录" />
          </section>
        </el-tab-pane>

        <el-tab-pane label="诊断" name="diagnostics">
          <section class="detail-section"><el-alert title="以下字段仅用于身份对账、配置排查和审计定位，不作为日常资源名称。" type="warning" show-icon :closable="false" /><el-descriptions class="diagnostic-grid" :column="2" border>
            <el-descriptions-item v-for="item in diagnosticRows" :key="item.label" :label="item.label"><span class="mono">{{ item.value }}</span><el-button link :icon="CopyDocument" title="复制诊断值" @click="copyText(item.value)" /></el-descriptions-item>
          </el-descriptions></section>
        </el-tab-pane>
      </el-tabs>
    </template>

    <el-drawer v-model="capabilityDrawer" title="编辑开放能力" size="760px" destroy-on-close>
      <template v-if="capabilityForm">
        <el-tabs v-model="capabilityTab" tab-position="left" class="capability-editor">
          <el-tab-pane label="SSH" name="ssh"><CapabilitySwitch title="SSH" description="主机终端访问" v-model="capabilityForm.ssh_enabled" /><el-alert title="SSH 域名由 Agent 域名配置生成，不配置监听端口。" type="info" :closable="false" /></el-tab-pane>
          <el-tab-pane label="Kubernetes API" name="k8s"><CapabilitySwitch title="Kubernetes API" description="使用 Agent 本机 /root/.kube/config 的 current-context" v-model="capabilityForm.k8s_enabled" /><el-descriptions :column="1" border><el-descriptions-item label="配置文件"><span class="mono">/root/.kube/config</span></el-descriptions-item><el-descriptions-item label="实际 API Server"><span class="mono">{{ capabilityForm.k8s_api_address || '等待 Agent 上报' }}</span></el-descriptions-item></el-descriptions></el-tab-pane>
          <el-tab-pane label="Kubernetes Pods" name="pods"><CapabilitySwitch title="Kubernetes Pods" description="依赖 SSH 与 Kubernetes API；关闭依赖时会同步关闭" :model-value="resource?.container_ssh_enabled || false" :disabled="true" /><el-alert title="此能力由 SSH 与 Kubernetes API 共同决定。" type="info" :closable="false" /></el-tab-pane>
          <el-tab-pane label="Kubernetes Service" name="service"><CapabilitySwitch title="Kubernetes Service" description="发现匹配条件的 Service" v-model="capabilityForm.svc_enabled" /><el-form label-position="top"><el-form-item label="Namespace"><el-select v-model="capabilityForm.svc_namespaces" multiple allow-create filterable default-first-option placeholder="留空表示全部 Namespace" /></el-form-item><el-form-item label="标签选择器" required><el-input v-model="capabilityForm.svc_label_selector" placeholder="signal.beagle.io/expose=true" /></el-form-item><el-form-item label="代理监听端口"><el-input-number v-model="capabilityForm.svc_listen_port_base" :min="1" :max="65535" placeholder="50051" /></el-form-item></el-form></el-tab-pane>
          <el-tab-pane label="Endpoint 接入" name="endpoint"><CapabilitySwitch title="Endpoint 接入" description="接受隔离网络 Endpoint 主动建立长连接" v-model="capabilityForm.endpoint_access_enabled" /><el-form label-position="top"><el-form-item label="Agent 内网地址" :required="capabilityForm.endpoint_access_enabled"><el-input v-model="capabilityForm.endpoint_address" placeholder="Endpoint 所在网络可达的 IP 或内网域名" /></el-form-item><el-form-item label="监听端口"><el-input-number v-model="capabilityForm.endpoint_listen_port" :min="1" :max="65535" placeholder="50052" /></el-form-item></el-form><el-alert title="关闭能力、修改地址或轮换令牌会影响 Endpoint 重连。请先放通新的 TCP 网络策略。" type="warning" :closable="false" /><el-button class="rotate-button" type="danger" plain :disabled="!capabilityForm.endpoint_token_exists" @click="rotateEndpointToken">轮换共享令牌</el-button></el-tab-pane>
        </el-tabs>
      </template>
      <template #footer><el-button @click="capabilityDrawer = false">取消</el-button><el-button type="primary" :loading="submitting" :disabled="!capabilityChanged" @click="saveCapabilities">保存配置</el-button></template>
    </el-drawer>

    <el-dialog v-model="endpointDialog" :title="endpointName(selectedEndpoint || emptyResource)" width="860px" destroy-on-close @closed="selectedEndpoint = undefined">
      <template v-if="selectedEndpoint">
        <div class="endpoint-summary"><div><span>生命周期</span><el-tag :type="lifecycleTag(selectedEndpoint.lifecycle_state)">{{ lifecycleLabel(selectedEndpoint.lifecycle_state) }}</el-tag></div><div><span>健康</span><el-tag :type="healthTag(selectedEndpoint.health_state)">{{ healthLabel(selectedEndpoint.health_state) }}</el-tag></div><div><span>稳定 Endpoint ID</span><strong class="mono">{{ selectedEndpoint.stable_key }}</strong></div></div>
        <el-alert title="稳定身份保存在 Endpoint 本机" description="停用和恢复不改变 endpoint_id；删除后原 ID 不得再次注册。" type="info" show-icon :closable="false" />
        <section class="dialog-section"><h3>访问域名</h3><div class="domain-editor"><el-input v-model="endpointDomainLabel" placeholder="DNS 单标签" /><span class="mono">.{{ selectedEndpoint.domain_namespace }}.beagle</span><el-button :disabled="!canWrite" @click="saveEndpointDomain">保存域名</el-button></div></section>
        <section class="dialog-section"><h3>开放能力</h3><el-form v-if="endpointCapabilityForm" label-position="top"><div class="endpoint-switch-grid"><el-form-item label="SSH"><el-switch v-model="endpointCapabilityForm.ssh_enabled" /></el-form-item><el-form-item label="Kubernetes API"><el-switch v-model="endpointCapabilityForm.k8s_enabled" /></el-form-item><el-form-item label="Kubernetes Service"><el-switch v-model="endpointCapabilityForm.svc_enabled" /></el-form-item></div><el-form-item v-if="endpointCapabilityForm.ssh_enabled" label="SSH 登录用户"><el-select v-model="endpointCapabilityForm.ssh_users" multiple allow-create filterable default-first-option /></el-form-item><el-form-item v-if="endpointCapabilityForm.svc_enabled" label="Service Namespace"><el-select v-model="endpointCapabilityForm.svc_namespaces" multiple allow-create filterable default-first-option /></el-form-item><el-form-item v-if="endpointCapabilityForm.svc_enabled" label="Service 标签选择器"><el-input v-model="endpointCapabilityForm.svc_label_selector" /></el-form-item></el-form></section>
        <section class="dialog-section danger-zone"><div><h3>生命周期</h3><p>停用保留配置并拒绝业务访问；删除前必须通过依赖检查。</p></div><div><el-button v-if="selectedEndpoint.lifecycle_state === 'registered'" :disabled="!canWrite" @click="setEndpointLifecycle('maintenance')">停用</el-button><el-button v-else-if="selectedEndpoint.lifecycle_state === 'disabled'" :disabled="!canWrite" @click="setEndpointLifecycle('resume')">恢复</el-button><el-button type="danger" :disabled="!canWrite" @click="openDeleteCheck(selectedEndpoint)">删除</el-button></div></section>
      </template>
      <template #footer><el-button @click="endpointDialog = false">取消</el-button><el-button type="primary" :disabled="!canWrite || !endpointCapabilityForm" :loading="submitting" @click="saveEndpointCapabilities">保存能力配置</el-button></template>
    </el-dialog>

    <el-dialog v-model="updateDialog" :title="`更新 ${updateTarget ? endpointName(updateTarget) : ''}`" width="560px" destroy-on-close>
      <el-form label-position="top"><el-form-item label="目标构建" required><el-select v-model="updateForm.releaseId" style="width:100%" placeholder="选择已发布构建"><el-option v-for="release in releases" :key="release.id" :label="`${release.version} @ ${shortValue(release.commit_id, 8)} · ${formatTime(release.published_at)}`" :value="release.id" /></el-select></el-form-item><el-form-item><el-checkbox v-model="updateForm.force">重新校验并重启同一构建</el-checkbox></el-form-item></el-form><el-alert v-if="releases.length === 0" title="当前组件没有可用的已发布版本" type="warning" show-icon :closable="false" /><template #footer><el-button @click="updateDialog = false">取消</el-button><el-button type="primary" :disabled="!updateForm.releaseId" :loading="submitting" @click="createUpdateTask">创建任务</el-button></template>
    </el-dialog>

    <el-dialog v-model="deleteCheckDialog" :title="`删除检查 · ${deleteTarget ? endpointName(deleteTarget) : ''}`" width="620px">
      <template v-if="deleteCheck?.allowed"><el-result icon="success" title="可以删除" sub-title="配置、授权和域名将被移除，稳定 ID 将写入拒绝再次注册的墓碑。" /><el-form label-position="top"><el-form-item label="删除原因" required><el-input v-model="deleteReason" type="textarea" placeholder="说明删除原因，内容将写入审计" /></el-form-item><el-form-item :label="`输入 ${deleteTarget ? endpointName(deleteTarget) : ''} 以确认`" required><el-input v-model="deleteConfirmName" /></el-form-item></el-form></template>
      <template v-else><el-alert title="当前不能删除" description="请先处理以下依赖，再重新检查。" type="warning" show-icon :closable="false" /><div class="blocker-list"><div v-for="blocker in deleteCheck?.blockers" :key="blocker.code"><strong>{{ blocker.message }}</strong><span>{{ blocker.count }} 项 · {{ blocker.code }}</span></div></div></template>
      <template #footer><el-button @click="deleteCheckDialog = false">关闭</el-button><el-button :loading="submitting" @click="runDeleteCheck">重新检查</el-button><el-button v-if="deleteCheck?.allowed" type="danger" :disabled="!deleteReady" :loading="submitting" @click="deleteResource">确认删除</el-button></template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { ArrowLeft, Box, Connection, CopyDocument, Cpu, Delete, Monitor, MoreFilled, Operation, Refresh, Search, Service, Upload } from '@element-plus/icons-vue'
import PageHeader from '@/components/Common/PageHeader.vue'
import CapabilitySwitch from '@/views/Provider/components/CapabilitySwitch.vue'
import {
  checkProviderTechnicalResourceDelete, createProviderTechnicalResourceUpdateTask, deleteProviderTechnicalResource,
  getProviderTechnicalResource, getProviderTechnicalResourceCapabilities, getProviderTechnicalResourceEndpointAccess,
  getProviderTechnicalResourceReleases, getProviderTechnicalResourceUpdateTasks, rotateProviderTechnicalResourceEndpointToken,
  setProviderTechnicalResourceLifecycle, updateProviderAgentHostDomainLabel, updateProviderTechnicalResourceCapabilities,
  type ProviderRelease, type ProviderUpdateTask, type TechnicalResource, type TechnicalResourceCapabilities,
  type TechnicalResourceDeleteCheck, type TechnicalResourceEndpointAccess, type TechnicalResourceState,
} from '@/api/providerSupply'
import { useWorkspaceStore } from '@/stores/workspace'

const route = useRoute()
const router = useRouter()
const workspaceStore = useWorkspaceStore()
const loading = ref(false)
const submitting = ref(false)
const errorMessage = ref('')
const resource = ref<TechnicalResource>()
const endpoints = ref<TechnicalResource[]>([])
const capabilities = ref<TechnicalResourceCapabilities>()
const endpointAccess = ref<TechnicalResourceEndpointAccess>()
const updateTasks = ref<ProviderUpdateTask[]>([])
const activeTab = ref('overview')
const endpointSearch = ref('')
const endpointFilter = ref('全部')
const capabilityDrawer = ref(false)
const capabilityTab = ref('endpoint')
const capabilityForm = ref<TechnicalResourceCapabilities>()
const capabilitySnapshot = ref('')
const endpointDialog = ref(false)
const selectedEndpoint = ref<TechnicalResource>()
const endpointCapabilityForm = ref<TechnicalResourceCapabilities>()
const endpointDomainLabel = ref('')
const updateDialog = ref(false)
const updateTarget = ref<TechnicalResource>()
const releases = ref<ProviderRelease[]>([])
const updateForm = reactive({ releaseId: '', force: false })
const deleteCheckDialog = ref(false)
const deleteTarget = ref<TechnicalResource>()
const deleteCheck = ref<TechnicalResourceDeleteCheck>()
const deleteReason = ref('')
const deleteConfirmName = ref('')
const emptyResource = { display_name: '', domain_label: '', hostname: '' } as TechnicalResource

const canWrite = computed(() => workspaceStore.can('provider.technical_resources.write'))
const hasBinding = computed(() => !!resource.value && resource.value.lifecycle_state !== 'pending')
const resourceTitle = computed(() => resource.value?.display_name || resource.value?.domain_label || resource.value?.hostname || '等待主机注册')
const resourceDescription = computed(() => 'Agent 部署位置、开放能力、Endpoint 接入与运行诊断。')
const endpointName = (item: TechnicalResource) => item.display_name || item.host_domain_label || item.domain_label || item.hostname || '等待主机注册'
const endpointDomain = (item: TechnicalResource) => item.host_domain_label && item.domain_namespace ? `${item.host_domain_label}.${item.domain_namespace}.beagle` : '-'
const endpointCapabilityLabels = (item: TechnicalResource) => [item.ssh_enabled ? 'SSH' : '', item.k8s_enabled ? 'Kubernetes API' : '', item.svc_enabled ? 'Kubernetes Service' : ''].filter(Boolean)
const updaterSupported = (item: TechnicalResource) => item.type === 'agent' ? item.updater_protocol === 'v2' : ['v1', 'v2'].includes(item.updater_protocol || '')
const filteredEndpoints = computed(() => endpoints.value.filter(item => {
  const state = item.lifecycle_state === 'disabled' ? '已停用' : item.health_state === 'online' ? '在线' : '离线'
  const matchesState = endpointFilter.value === '全部' || endpointFilter.value === state
  const query = endpointSearch.value.trim().toLowerCase()
  return matchesState && (!query || `${endpointName(item)} ${endpointDomain(item)} ${item.stable_key} ${item.hostname}`.toLowerCase().includes(query))
}))
const configStatus = computed(() => {
  if (!resource.value) return { type: 'info' as const, label: '-', title: '', description: '' }
  const expected = resource.value.config_revision
  const observed = resource.value.observed_revision
  if (observed > expected) return { type: 'error' as const, label: '异常', title: '配置 revision 异常', description: `已应用 revision ${observed} 超过期望 revision ${expected}，请检查上报协议。` }
  if (observed === expected) return { type: 'success' as const, label: `已生效 · r${expected}`, title: '配置已全部生效', description: `期望配置 revision ${expected} 与 Agent 已应用 revision ${observed} 一致。` }
  if (resource.value.health_state === 'online') return { type: 'warning' as const, label: `应用中 · r${observed}/${expected}`, title: '配置正在应用', description: `Agent 在线，等待确认期望配置 revision ${expected}。当前已应用 revision ${observed}。` }
  return { type: 'warning' as const, label: `等待上线 · r${observed}/${expected}`, title: '等待 Agent 上线应用配置', description: `Agent 当前离线，期望配置 revision ${expected} 尚未生效。` }
})
const endpointAccessAddress = computed(() => endpointAccess.value?.address ? `${endpointAccess.value.address}:${endpointAccess.value.port}` : capabilities.value?.endpoint_address ? `${capabilities.value.endpoint_address}:${capabilities.value.endpoint_listen_port || 50052}` : '待配置')
const endpointAccessReady = computed(() => capabilities.value?.endpoint_access_enabled && !!(endpointAccess.value?.address || capabilities.value?.endpoint_address))
const capabilityRows = computed(() => resource.value && capabilities.value ? [
  { label: 'SSH', description: '主机终端访问', value: resource.value.host_domain_label ? `${resource.value.host_domain_label}.${resource.value.domain_namespace}.beagle` : '待配置域名', valueLabel: '访问域名', source: 'Agent 域名配置', enabled: capabilities.value.ssh_enabled, icon: Connection },
  { label: 'Kubernetes API', description: '代理原生 Kubernetes API', value: capabilities.value.k8s_api_address || '等待 Agent 上报', valueLabel: '本机 current-context API Server', source: '本机 kubeconfig', enabled: capabilities.value.k8s_enabled, icon: Monitor },
  { label: 'Kubernetes Pods', description: '发现 Pod 并提供终端访问', value: '自动发现 Pod 与容器', valueLabel: '通过 pods/exec 连接', source: '能力依赖', enabled: resource.value.container_ssh_enabled, icon: Box },
  { label: 'Kubernetes Service', description: '发现并代理集群 Service', value: `${capabilities.value.svc_label_selector || '未配置选择器'} · :${capabilities.value.svc_listen_port_base || 50051}`, valueLabel: '标签选择器 · 代理端口', source: '远程配置', enabled: capabilities.value.svc_enabled, icon: Service },
  { label: 'Endpoint 接入', description: '接受隔离网络 Endpoint 反向连接', value: endpointAccessAddress.value, valueLabel: 'Agent 内网地址 · 监听端口', source: '远程配置', enabled: capabilities.value.endpoint_access_enabled, icon: Connection },
] : [])
const diagnosticRows = computed(() => resource.value ? [
  { label: 'TechnicalResource ID', value: resource.value.id }, { label: 'Stable key', value: resource.value.stable_key },
  { label: 'Agent 上报主机名', value: resource.value.hostname || '-' }, { label: 'Endpoint 内网地址', value: capabilities.value?.endpoint_address || '-' },
  { label: 'Credential Revision', value: String(resource.value.credential_revision) }, { label: 'Config Revision', value: String(resource.value.config_revision) },
  { label: 'Observed Revision', value: String(resource.value.observed_revision) }, { label: 'Updater 协议', value: resource.value.updater_protocol || '-' },
  { label: 'Inventory Sequence', value: String(resource.value.last_sequence) }, { label: 'Source Epoch', value: resource.value.source_epoch || '-' },
] : [])
const capabilityChanged = computed(() => !!capabilityForm.value && JSON.stringify(capabilityForm.value) !== capabilitySnapshot.value)
const deleteReady = computed(() => !!deleteTarget.value && !!deleteReason.value.trim() && deleteConfirmName.value.trim() === endpointName(deleteTarget.value))

const load = async () => {
  const providerId = workspaceStore.providerId
  if (!providerId) return
  clearSensitiveAccess()
  loading.value = true
  errorMessage.value = ''
  try {
    const response = await getProviderTechnicalResource(providerId, String(route.params.id))
    const current = response.success ? response.data.resource : undefined
    if (current?.type === 'endpoint' && current.parent_id) {
      await router.replace({ path: `/provider-technical-resources/${current.parent_id}`, query: { endpoint: current.id } })
      return
    }
    resource.value = current
    endpoints.value = response.success ? response.data.endpoints || [] : []
    if (!current) return
    const [capabilityResponse, taskResponse] = await Promise.all([
      getProviderTechnicalResourceCapabilities(providerId, current.id),
      getProviderTechnicalResourceUpdateTasks(providerId, current.id),
    ])
    capabilities.value = capabilityResponse.success ? capabilityResponse.data : undefined
    updateTasks.value = taskResponse.success ? taskResponse.data : []
    if (canWrite.value && current.type === 'agent') await loadEndpointAccess()
    const requestedEndpoint = typeof route.query.endpoint === 'string' ? route.query.endpoint : ''
    if (requestedEndpoint) {
      const endpoint = endpoints.value.find(item => item.id === requestedEndpoint)
      if (endpoint) await openEndpoint(endpoint)
    }
  } catch {
    resource.value = undefined
    endpoints.value = []
    errorMessage.value = '请确认技术资源仍属于当前资源方且当前账号具有查看权限。'
  } finally { loading.value = false }
}
const loadEndpointAccess = async () => {
  if (!resource.value || !workspaceStore.providerId || !canWrite.value) return
  const response = await getProviderTechnicalResourceEndpointAccess(workspaceStore.providerId, resource.value.id)
  endpointAccess.value = response.success ? response.data : undefined
}
const clearSensitiveAccess = () => { endpointAccess.value = undefined }
const openCapabilities = async () => {
  if (!resource.value || !workspaceStore.providerId) return
  const response = await getProviderTechnicalResourceCapabilities(workspaceStore.providerId, resource.value.id)
  if (!response.success) return
  capabilityForm.value = { ...response.data, ssh_users: [...(response.data.ssh_users || [])], svc_namespaces: [...(response.data.svc_namespaces || [])] }
  capabilitySnapshot.value = JSON.stringify(capabilityForm.value)
  capabilityTab.value = 'endpoint'
  capabilityDrawer.value = true
}
const saveCapabilities = async () => {
  if (!resource.value || !capabilityForm.value || !workspaceStore.providerId) return
  if (capabilityForm.value.endpoint_access_enabled && !capabilityForm.value.endpoint_address?.trim()) { ElMessage.warning('启用 Endpoint 接入时必须填写 Agent 内网地址'); return }
  if (capabilityForm.value.svc_enabled && !capabilityForm.value.svc_label_selector?.trim()) { ElMessage.warning('启用 Kubernetes Service 时必须填写标签选择器'); return }
  submitting.value = true
  try {
    await updateProviderTechnicalResourceCapabilities(workspaceStore.providerId, resource.value, capabilityForm.value)
    ElMessage.success('期望配置已保存，等待 Agent 确认生效')
    capabilityDrawer.value = false
    await load()
  } finally { submitting.value = false }
}
const rotateEndpointToken = async () => {
  if (!resource.value || !workspaceStore.providerId) return
  await ElMessageBox.confirm('轮换后，仍持有旧令牌的 Endpoint 断线后将无法重连。', '轮换共享令牌', { type: 'warning', confirmButtonText: '确认轮换' })
  submitting.value = true
  try {
    const response = await rotateProviderTechnicalResourceEndpointToken(workspaceStore.providerId, resource.value)
    endpointAccess.value = response.success ? response.data : undefined
    ElMessage.success('共享令牌已轮换，新安装命令已生成')
    await load()
  } finally { submitting.value = false }
}
const openEndpoint = async (item: TechnicalResource) => {
  if (!workspaceStore.providerId) return
  selectedEndpoint.value = item
  endpointDomainLabel.value = item.host_domain_label || ''
  endpointDialog.value = true
  const response = await getProviderTechnicalResourceCapabilities(workspaceStore.providerId, item.id)
  endpointCapabilityForm.value = response.success ? { ...response.data, ssh_users: [...(response.data.ssh_users || [])], svc_namespaces: [...(response.data.svc_namespaces || [])] } : undefined
}
const saveEndpointDomain = async () => {
  if (!selectedEndpoint.value || !workspaceStore.providerId) return
  const label = endpointDomainLabel.value.trim().toLowerCase()
  if (!/^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$/.test(label)) { ElMessage.warning('请输入有效的 DNS 单标签'); return }
  await ElMessageBox.confirm(`旧域名将立即失效，新域名为 ${label}.${selectedEndpoint.value.domain_namespace}.beagle。`, '确认变更域名', { type: 'warning' })
  await updateProviderAgentHostDomainLabel(workspaceStore.providerId, selectedEndpoint.value, label)
  ElMessage.success('Endpoint 访问域名已更新')
  endpointDialog.value = false
  await load()
}
const saveEndpointCapabilities = async () => {
  if (!selectedEndpoint.value || !endpointCapabilityForm.value || !workspaceStore.providerId) return
  if (endpointCapabilityForm.value.svc_enabled && !endpointCapabilityForm.value.svc_label_selector?.trim()) { ElMessage.warning('启用 Kubernetes Service 时必须填写标签选择器'); return }
  submitting.value = true
  try { await updateProviderTechnicalResourceCapabilities(workspaceStore.providerId, selectedEndpoint.value, endpointCapabilityForm.value); ElMessage.success('Endpoint 期望配置已保存'); endpointDialog.value = false; await load() } finally { submitting.value = false }
}
const setEndpointLifecycle = async (action: 'maintenance' | 'resume') => {
  if (!selectedEndpoint.value) return
  await setLifecycle(selectedEndpoint.value, action)
  endpointDialog.value = false
}
const setLifecycle = async (target: TechnicalResource, action: 'maintenance' | 'resume' | 'retire') => {
  if (!workspaceStore.providerId) return
  const title = action === 'maintenance' ? '进入维护' : action === 'resume' ? '恢复服务' : '退役技术资源'
  try {
    const result = await ElMessageBox.prompt('请输入操作原因，原因将写入审计记录。', title, { inputPattern: /\S+/, inputErrorMessage: '请输入操作原因', type: action === 'retire' ? 'warning' : 'info' })
    await setProviderTechnicalResourceLifecycle(workspaceStore.providerId, target, action, result.value.trim())
    ElMessage.success(`${title}成功`)
    await load()
  } catch (error) { if (error !== 'cancel' && error !== 'close') throw error }
}
const handleLifecycleCommand = async (command: string) => {
  if (!resource.value) return
  if (command === 'delete-check') { await openDeleteCheck(resource.value); return }
  await setLifecycle(resource.value, command as 'maintenance' | 'resume' | 'retire')
}
const openUpdate = async (target: TechnicalResource) => {
  if (!workspaceStore.providerId) return
  updateTarget.value = target
  const response = await getProviderTechnicalResourceReleases(workspaceStore.providerId, target.id)
  releases.value = response.success ? response.data : []
  updateForm.releaseId = releases.value[0]?.id || ''
  updateForm.force = false
  updateDialog.value = true
}
const createUpdateTask = async () => {
  if (!updateTarget.value || !workspaceStore.providerId || !updateForm.releaseId) return
  submitting.value = true
  try { await createProviderTechnicalResourceUpdateTask(workspaceStore.providerId, updateTarget.value.id, updateForm.releaseId, updateForm.force); ElMessage.success('更新任务已创建'); updateDialog.value = false; activeTab.value = 'events'; await load() } finally { submitting.value = false }
}
const openDeleteCheck = async (target: TechnicalResource) => { deleteTarget.value = target; deleteCheck.value = undefined; deleteReason.value = ''; deleteConfirmName.value = ''; deleteCheckDialog.value = true; await runDeleteCheck() }
const runDeleteCheck = async () => {
  if (!deleteTarget.value || !workspaceStore.providerId) return
  submitting.value = true
  try { const response = await checkProviderTechnicalResourceDelete(workspaceStore.providerId, deleteTarget.value.id); deleteCheck.value = response.success ? response.data : undefined } finally { submitting.value = false }
}
const deleteResource = async () => {
  if (!deleteTarget.value || !workspaceStore.providerId || !deleteCheck.value?.allowed) return
  const target = deleteTarget.value
  if (!deleteReady.value) return
  try {
    await deleteProviderTechnicalResource(workspaceStore.providerId, target, deleteReason.value.trim())
    ElMessage.success('技术资源已删除')
    deleteCheckDialog.value = false
    endpointDialog.value = false
    if (target.id === resource.value?.id) await returnToList(); else await load()
  } catch (error) { if (error !== 'cancel' && error !== 'close') throw error }
}
const copyText = async (value: string) => { await navigator.clipboard.writeText(value); ElMessage.success('已复制到剪贴板') }
const copyInstallCommand = () => endpointAccess.value?.install_command && copyText(endpointAccess.value.install_command)
const returnToList = () => router.push('/provider-technical-resources')
const endpointConfigLabel = (item: TechnicalResource) => item.config_revision === item.observed_revision ? `配置 r${item.config_revision} 已生效` : `配置 r${item.observed_revision}/${item.config_revision}`
const lifecycleLabel = (state: TechnicalResourceState) => ({ pending: '待部署', registered: '已注册', disabled: '已停用', retired: '已退役', deleted: '已删除' }[state])
const lifecycleTag = (state: TechnicalResourceState) => ({ pending: 'warning', registered: 'success', disabled: 'warning', retired: 'info', deleted: 'info' }[state] as any)
const healthLabel = (state: string) => ({ unknown: '未知', online: '在线', degraded: '异常', offline: '离线' }[state] || state)
const healthTag = (state: string) => ({ online: 'success', degraded: 'warning', offline: 'danger', unknown: 'info' }[state] || 'info') as any
const formatTime = (value?: string) => value ? new Date(value).toLocaleString('zh-CN', { hour12: false }) : '-'
const relativeTime = (value?: string) => { if (!value) return '从未上报'; const seconds = Math.max(0, Math.floor((Date.now() - new Date(value).getTime()) / 1000)); return seconds < 60 ? `${seconds} 秒前` : seconds < 3600 ? `${Math.floor(seconds / 60)} 分钟前` : formatTime(value) }
const shortValue = (value: string | undefined, length: number) => value ? value.substring(0, length) : '-'
const updateStatusLabel = (status: string) => ({ pending: '等待下发', delivered: '已下发', accepted: '已接受', downloading: '下载中', verifying: '校验中', installing: '安装中', restarting: '重启中', succeeded: '成功', failed: '失败', rolled_back: '已回滚', cancelled: '已取消', expired: '已过期' }[status] || status)
const updateStatusTag = (status: string) => status === 'succeeded' ? 'success' : ['failed', 'expired'].includes(status) ? 'danger' : ['cancelled', 'rolled_back'].includes(status) ? 'info' : 'warning'
watch(() => [workspaceStore.providerId, route.params.id], load)
onMounted(load)
onBeforeUnmount(clearSensitiveAccess)
</script>

<style scoped>
.provider-page { width: 100%; }
.state-alert { margin-bottom: 14px; }
.summary-surface { display: grid; grid-template-columns: minmax(280px, 1.45fr) repeat(4, minmax(125px, .65fr)); margin-bottom: 14px; border: 1px solid var(--border-light); border-radius: 6px; background: #fff; }
.summary-identity, .summary-item { min-height: 76px; padding: 12px 14px; }
.summary-identity { display: flex; align-items: center; gap: 11px; }
.resource-icon { width: 38px; height: 38px; display: inline-flex; align-items: center; justify-content: center; border-radius: 6px; color: var(--primary-color); background: var(--primary-lighter); font-size: 20px; }
.summary-identity h2 { margin: 0; font-size: 17px; }
.summary-identity p { margin: 2px 0 0; color: var(--text-secondary); font-size: 11px; }
.summary-item { display: flex; flex-direction: column; justify-content: center; border-left: 1px solid var(--border-light); }
.summary-item > span { margin-bottom: 6px; color: var(--text-secondary); font-size: 11px; }
.detail-tabs { border: 1px solid var(--border-light); border-radius: 6px; background: #fff; }
.detail-tabs :deep(.el-tabs__header) { margin: 0; padding: 0 14px; }
.detail-tabs :deep(.el-tabs__content) { padding: 14px; }
.overview-stack { display: grid; gap: 18px; }
.detail-section { min-width: 0; }
.section-head { min-height: 34px; display: flex; align-items: flex-start; justify-content: space-between; gap: 16px; margin-bottom: 8px; }
.section-head.compact { align-items: center; }
.section-head h3, .dialog-section h3 { margin: 0; font-size: 14px; }
.section-head p, .dialog-section p { margin: 2px 0 0; color: var(--text-secondary); font-size: 11px; }
.capability-list, .table-surface { overflow: hidden; border: 1px solid var(--border-light); border-radius: 6px; }
.capability-row { min-height: 66px; display: grid; grid-template-columns: minmax(220px, 1.2fr) minmax(220px, 1fr) 130px 80px; align-items: center; gap: 12px; padding: 9px 12px; border-bottom: 1px solid var(--border-lighter); }
.capability-row:last-child { border-bottom: 0; }
.capability-name { display: flex; align-items: flex-start; gap: 9px; }
.capability-name .el-icon { margin-top: 2px; color: var(--primary-color); }
.capability-name strong, .capability-name span, .capability-value strong, .capability-value span { display: block; }
.capability-name span, .capability-value span, .capability-source { color: var(--text-secondary); font-size: 11px; }
.capability-value strong { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-size: 11px; }
.access-strip { display: grid; grid-template-columns: minmax(260px, 1.25fr) repeat(3, minmax(150px, .75fr)); margin-bottom: 12px; border: 1px solid #cbd9e6; border-radius: 6px; background: #eef4f8; }
.access-strip > div { min-height: 68px; display: flex; flex-direction: column; justify-content: center; padding: 10px 12px; border-left: 1px solid #d7e2ec; }
.access-strip > div:first-child { flex-direction: row; align-items: center; justify-content: flex-start; gap: 9px; border-left: 0; color: #4f6f8f; }
.access-strip span strong, .access-strip span small, .access-strip > div > small { display: block; }
.access-strip small { color: var(--text-secondary); font-size: 10px; }
.install-command { margin-bottom: 18px; padding: 12px; border: 1px solid var(--border-light); border-radius: 6px; }
.install-command pre { overflow: auto; margin: 0; padding: 12px; border-radius: 4px; background: #192332; color: #eff4fb; font: 11px/1.6 'SFMono-Regular', Consolas, monospace; white-space: pre-wrap; }
.install-command :deep(.el-empty) { padding: 12px 0; }
.endpoint-head { margin-top: 16px; }
.endpoint-toolbar { display: flex; align-items: center; gap: 10px; padding: 10px; border-bottom: 1px solid var(--border-lighter); }
.endpoint-toolbar .el-input { width: 280px; }
.endpoint-toolbar > span { margin-left: auto; color: var(--text-secondary); font-size: 11px; }
.endpoint-name { max-width: 100%; padding: 0; font-weight: 650; }
.endpoint-capabilities { display: flex; flex-wrap: wrap; gap: 4px; }
.secondary { display: block; margin-top: 3px; color: var(--text-secondary); font-size: 11px; }
.secondary.inline { display: inline; }
.metric-grid { display: grid; grid-template-columns: repeat(4, 1fr); border: 1px solid var(--border-light); border-radius: 6px; }
.metric-grid > div { min-height: 66px; padding: 11px 13px; border-left: 1px solid var(--border-lighter); }
.metric-grid > div:first-child { border-left: 0; }
.metric-grid span, .metric-grid strong { display: block; }
.metric-grid span { color: var(--text-secondary); font-size: 10px; }
.metric-grid strong { margin-top: 6px; font-size: 12px; }
.diagnostic-grid { margin-top: 14px; }
.diagnostic-grid .el-button { float: right; }
.mono { font-family: 'SFMono-Regular', Consolas, 'Liberation Mono', monospace; }
.capability-editor { height: 100%; }
.capability-editor :deep(.el-tabs__content) { height: 100%; padding: 4px 4px 4px 20px; }
.capability-editor :deep(.el-select) { width: 100%; }
.rotate-button { margin-top: 14px; }
.endpoint-summary { display: grid; grid-template-columns: 1fr 1fr 2fr; margin-bottom: 12px; border: 1px solid var(--border-light); border-radius: 5px; }
.endpoint-summary > div { min-height: 64px; display: flex; flex-direction: column; justify-content: center; padding: 10px 12px; border-left: 1px solid var(--border-lighter); }
.endpoint-summary > div:first-child { border-left: 0; }
.endpoint-summary span { margin-bottom: 5px; color: var(--text-secondary); font-size: 10px; }
.dialog-section { margin-top: 18px; }
.domain-editor { display: grid; grid-template-columns: 220px minmax(220px, 1fr) auto; align-items: center; gap: 8px; margin-top: 8px; padding: 10px; border: 1px solid var(--border-light); border-radius: 5px; }
.endpoint-switch-grid { display: grid; grid-template-columns: repeat(3, 1fr); gap: 12px; }
.danger-zone { display: flex; align-items: center; justify-content: space-between; gap: 16px; padding: 12px; border: 1px solid #e7b9b6; border-radius: 5px; background: #fff1f0; }
.blocker-list { margin-top: 14px; border: 1px solid var(--border-light); border-radius: 5px; }
.blocker-list > div { display: flex; align-items: center; justify-content: space-between; min-height: 56px; padding: 10px 14px; border-bottom: 1px solid var(--border-lighter); }
.blocker-list > div:last-child { border-bottom: 0; }
.blocker-list span { color: var(--text-secondary); font-size: 11px; }
@media (max-width: 1280px) { .summary-surface { grid-template-columns: minmax(250px, 1.3fr) repeat(4, minmax(110px, .6fr)); } .capability-row { grid-template-columns: minmax(190px, 1.1fr) minmax(190px, 1fr) 110px 74px; } }
</style>
