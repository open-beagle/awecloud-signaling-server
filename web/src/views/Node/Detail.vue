<template>
  <div class="node-detail" v-loading="loading">
    <template v-if="node">
      <!-- 基本信息 -->
      <el-card shadow="never" class="info-card">
        <template #header>
          <div class="card-header">
            <span>{{ $t('node.basicInfo') }}</span>
            <el-tag :type="node.status === 'online' ? 'success' : 'info'" size="small">
              {{ node.status === 'online' ? $t('common.online') : $t('common.offline') }}
            </el-tag>
          </div>
        </template>
        <el-descriptions :column="2" border label-class-name="desc-label">
          <el-descriptions-item label="ID">{{ node.id }}</el-descriptions-item>
          <el-descriptions-item :label="$t('node.name')">{{ node.name }}</el-descriptions-item>
          <el-descriptions-item :label="$t('node.type')">
            <el-tag :type="node.type === 'agent' ? 'success' : 'primary'" size="small">
              {{ node.type === 'agent' ? $t('node.typeAgent') : $t('node.typeDesktop') }}
            </el-tag>
          </el-descriptions-item>
          <el-descriptions-item :label="$t('node.user')">
            <router-link v-if="node.user" :to="`/platform-identities/${node.user.id}`" class="user-link">
              {{ node.user.name }}
            </router-link>
            <span v-else>-</span>
          </el-descriptions-item>
          <el-descriptions-item :label="$t('node.ip')">{{ node.ip || '-' }}</el-descriptions-item>
          <el-descriptions-item :label="$t('node.hostname')">{{ node.hostname || '-' }}</el-descriptions-item>
          <el-descriptions-item v-if="node.type === 'agent'" label="SSH 域名标识">
            <span class="mono">{{ node.host_domain_label || '待配置' }}</span>
            <el-button link type="primary" @click="editHostDomainLabel">编辑</el-button>
          </el-descriptions-item>
          <el-descriptions-item :label="$t('node.version')">{{ node.version || '-' }}</el-descriptions-item>
          <el-descriptions-item :label="$t('node.lastHeartbeat')">
            {{ node.last_heartbeat ? formatTime(node.last_heartbeat) : '-' }}
          </el-descriptions-item>
          <el-descriptions-item :label="$t('common.createdAt')">
            {{ formatTime(node.created_at) }}
          </el-descriptions-item>
          <el-descriptions-item :label="$t('node.updatedAt')">
            {{ formatTime(node.updated_at) }}
          </el-descriptions-item>
        </el-descriptions>
      </el-card>

      <!-- Headscale 信息 -->
      <el-card shadow="never" class="info-card">
        <template #header>
          <div class="card-header">
            <span>Headscale {{ $t('node.nodeInfo') }}</span>
            <el-tag v-if="node.headscale" :type="node.headscale.online ? 'success' : 'info'" size="small">
              {{ node.headscale.online ? $t('common.online') : $t('common.offline') }}
            </el-tag>
          </div>
        </template>
        <template v-if="node.headscale">
          <el-descriptions :column="2" border label-class-name="desc-label">
            <el-descriptions-item label="Node ID">{{ node.headscale.id }}</el-descriptions-item>
            <el-descriptions-item :label="$t('node.name')">{{ node.headscale.name }}</el-descriptions-item>
            <el-descriptions-item :label="$t('node.givenName')">{{ node.headscale.given_name }}</el-descriptions-item>
            <el-descriptions-item :label="$t('node.hsUser')">{{ node.headscale.user_name || '-' }}</el-descriptions-item>
            <el-descriptions-item :label="$t('node.ipAddresses')">
              <el-tag v-for="ip in node.headscale.ip_addresses" :key="ip" size="small" class="ip-tag">
                {{ ip }}
              </el-tag>
            </el-descriptions-item>
            <el-descriptions-item :label="$t('node.forcedTags')">
              <el-tag v-for="tag in node.headscale.forced_tags" :key="tag" size="small" type="warning" class="tag-item">
                {{ tag }}
              </el-tag>
              <span v-if="!node.headscale.forced_tags?.length">-</span>
            </el-descriptions-item>
            <el-descriptions-item :label="$t('node.lastSeen')">
              {{ node.headscale.last_seen ? formatTime(node.headscale.last_seen) : '-' }}
            </el-descriptions-item>
            <el-descriptions-item :label="$t('node.expiry')">
              {{ node.headscale.expiry ? formatTime(node.headscale.expiry) : '-' }}
            </el-descriptions-item>
            <el-descriptions-item :label="$t('common.createdAt')">
              {{ node.headscale.created_at ? formatTime(node.headscale.created_at) : '-' }}
            </el-descriptions-item>
            <el-descriptions-item label="">&nbsp;</el-descriptions-item>
          </el-descriptions>
        </template>
        <template v-else>
          <el-empty :description="$t('node.noHeadscaleInfo')">
            <template #image><span></span></template>
          </el-empty>
        </template>
      </el-card>

      <!-- 系统信息 -->
      <el-card v-if="systemInfo" shadow="never" class="info-card">
        <template #header>
          <span>{{ $t('node.systemInfo') }}</span>
        </template>
        <el-descriptions :column="2" border label-class-name="desc-label">
          <el-descriptions-item :label="$t('node.os')">{{ systemInfo.os || '-' }}</el-descriptions-item>
          <el-descriptions-item :label="$t('node.osVersion')">{{ systemInfo.os_version || '-' }}</el-descriptions-item>
          <el-descriptions-item :label="$t('node.arch')">{{ systemInfo.arch || '-' }}</el-descriptions-item>
          <el-descriptions-item :label="$t('node.hostname')">{{ systemInfo.hostname || '-' }}</el-descriptions-item>
          <el-descriptions-item label="CPU">{{ systemInfo.cpu || '-' }}</el-descriptions-item>
          <el-descriptions-item :label="$t('node.cpuCores')">{{ systemInfo.cpu_cores || '-' }}</el-descriptions-item>
          <el-descriptions-item :label="$t('node.memory')">
            {{ systemInfo.memory_gb ? `${systemInfo.memory_gb} GB` : '-' }}
          </el-descriptions-item>
          <el-descriptions-item label="">&nbsp;</el-descriptions-item>
        </el-descriptions>
      </el-card>

      <!-- Desktop 远程桌面（3389）只读诊断、紧急熔断与会话审计（仅 Desktop 类型） -->
      <el-card v-if="node.type === 'desktop'" shadow="never" class="info-card" v-loading="rdpLoading">
        <template #header>
          <div class="card-header">
            <div class="rdp-header-left">
              <span>远程桌面（RDP / 3389）诊断与隐私治理</span>
              <el-tag v-if="rdpDiag?.rdp_admin_disabled" type="danger" size="small">
                平台紧急熔断中
              </el-tag>
              <el-tag v-else-if="rdpDiag?.rdp_tunnel_exposed" type="success" size="small">
                隧道入站已开放 (:{{ rdpDiag.rdp_local_port || 3389 }})
              </el-tag>
              <el-tag v-else type="info" size="small">
                入站未暴露
              </el-tag>
            </div>
            <div class="rdp-header-actions">
              <el-button size="small" @click="fetchDesktopRDPStatus" :loading="rdpLoading">刷新</el-button>
              <el-button
                v-if="rdpDiag?.rdp_admin_disabled"
                size="small"
                type="warning"
                :loading="rdpUpdating"
                @click="toggleRDPAdminDisable(false)"
              >
                解除紧急熔断
              </el-button>
              <el-button
                v-else
                size="small"
                type="danger"
                :loading="rdpUpdating"
                @click="toggleRDPAdminDisable(true)"
              >
                紧急熔断关闭
              </el-button>
            </div>
          </div>
        </template>

        <el-alert type="info" :closable="false" show-icon class="rdp-privacy-alert">
          <template #title>
            个人设备隐私边界：平台管理员仅拥有只读运行态诊断、连接审计与紧急熔断关闭权，不可代机主开启远程桌面或跨人共享许可。
          </template>
        </el-alert>

        <el-descriptions v-if="rdpDiag" :column="2" border label-class-name="desc-label" class="rdp-descriptions">
          <el-descriptions-item label="机主模式">
            <el-tag size="small" effect="plain" :type="rdpDiag.rdp_mode === 'disabled' ? 'info' : 'primary'">
              {{ formatRDPMode(rdpDiag.rdp_mode) }}
            </el-tag>
          </el-descriptions-item>
          <el-descriptions-item label="跨人共享许可">
            <el-tag size="small" effect="plain" :type="rdpDiag.rdp_allow_share ? 'warning' : 'info'">
              {{ rdpDiag.rdp_allow_share ? '机主已开启同租户临时共享许可' : '仅限机主本人设备（默认关闭）' }}
            </el-tag>
          </el-descriptions-item>
          <el-descriptions-item label="本机监听探测">
            <span>
              {{ rdpDiag.rdp_local_ready ? `已监听 (端口 ${rdpDiag.rdp_local_port || 3389})` : '未监听或设备离线' }}
            </span>
          </el-descriptions-item>
          <el-descriptions-item label="隧道暴露状态">
            <span>
              {{ rdpDiag.rdp_admin_disabled ? '已被平台强制熔断阻断' : rdpDiag.rdp_tunnel_exposed ? '允许授权节点访问 :3389' : '未开放' }}
            </span>
          </el-descriptions-item>
        </el-descriptions>

        <el-divider content-position="left">生效中的跨人临时授权 ({{ rdpDiag?.active_grants?.length || 0 }})</el-divider>
        <el-table
          :data="rdpDiag?.active_grants || []"
          size="small"
          empty-text="当前无跨用户临时远程桌面授权"
          class="rdp-sub-table"
        >
          <el-table-column prop="grantee_user_name" label="被授权用户" min-width="140" />
          <el-table-column prop="tenant_id" label="所属租户 ID" min-width="180" />
          <el-table-column prop="created_by_name" label="签发管理员" min-width="130" />
          <el-table-column label="到期时间" min-width="170">
            <template #default="{ row }">{{ formatTime(row.expires_at) }}</template>
          </el-table-column>
          <el-table-column prop="reason" label="授权原因" min-width="160" />
        </el-table>

        <el-divider content-position="left">最近远程桌面入站会话与拦截审计</el-divider>
        <el-table
          :data="rdpDiag?.recent_sessions || []"
          size="small"
          empty-text="暂无远程桌面连接或拦截审计记录"
        >
          <el-table-column label="发生时间" width="175">
            <template #default="{ row }">{{ formatTime(row.occurred_at) }}</template>
          </el-table-column>
          <el-table-column label="事件类型" width="115">
            <template #default="{ row }">
              <el-tag
                size="small"
                :type="row.event_type === 'started' ? 'success' : row.event_type === 'rejected' ? 'danger' : 'info'"
              >
                {{ row.event_type === 'started' ? '会话建立' : row.event_type === 'rejected' ? '零信任拦截' : '会话结束' }}
              </el-tag>
            </template>
          </el-table-column>
          <el-table-column label="来源身份 / 设备" min-width="200">
            <template #default="{ row }">
              <span>{{ row.source_user_name || '-' }}</span>
              <span v-if="row.source_device_name" class="session-sub"> / {{ row.source_device_name }}</span>
            </template>
          </el-table-column>
          <el-table-column prop="source_ip" label="来源隧道 IP" width="145" />
          <el-table-column label="持续时长 / 断开原因" min-width="180">
            <template #default="{ row }">
              <span v-if="row.duration_ms">{{ Math.round(row.duration_ms / 1000) }}s · </span>
              <span>{{ row.close_reason || (row.grant_id ? `临时授权 #${row.grant_id}` : '本人 Desktop 互访') }}</span>
            </template>
          </el-table-column>
        </el-table>
      </el-card>

      <!-- 能力配置（仅 Agent 类型） -->
      <el-card v-if="node.type === 'agent'" shadow="never" class="info-card" v-loading="capLoading">
        <template #header>
          <div class="card-header">
            <span>{{ $t('capability.title') }}</span>
            <div>
              <el-button type="primary" size="small" @click="saveCapabilities" :loading="capSaving">
                {{ $t('common.save') }}
              </el-button>
              <el-button size="small" @click="resetCapabilities">
                {{ $t('capability.reset') }}
              </el-button>
            </div>
          </div>
        </template>

        <el-form label-width="120px">
          <!-- SSH -->
          <el-divider content-position="left">{{ $t('capability.ssh') }}</el-divider>
          <el-form-item :label="$t('capability.sshEnabled')">
            <el-switch v-model="capForm.ssh_enabled" />
          </el-form-item>

          <!-- K8S API -->
          <el-divider content-position="left">{{ $t('capability.k8s') }}</el-divider>
          <el-form-item :label="$t('capability.k8sEnabled')">
            <el-switch v-model="capForm.k8s_enabled" />
            <el-tag :type="capOriginal.k8s_enabled !== null ? 'primary' : 'info'" size="small" class="source-tag">
              {{ capOriginal.k8s_enabled !== null ? $t('capability.sourceRemote') : $t('capability.sourceLocal') }}
            </el-tag>
          </el-form-item>
          <el-form-item :label="$t('capability.k8sListenPort')">
            <el-input-number v-model="capForm.k8s_listen_port" :min="1" :max="65535"
              :placeholder="$t('capability.k8sListenPortPlaceholder')" controls-position="right" />
          </el-form-item>
          <el-form-item :label="$t('capability.k8sApiServer')">
            <el-input v-model="capForm.k8s_api_server"
              :placeholder="$t('capability.k8sApiServerPlaceholder')" />
          </el-form-item>

          <!-- K8S Service -->
          <el-divider content-position="left">{{ $t('capability.svc') }}</el-divider>
          <el-form-item :label="$t('capability.svcEnabled')">
            <el-switch v-model="capForm.svc_enabled" />
            <el-tag :type="capOriginal.svc_enabled !== null ? 'primary' : 'info'" size="small" class="source-tag">
              {{ capOriginal.svc_enabled !== null ? $t('capability.sourceRemote') : $t('capability.sourceLocal') }}
            </el-tag>
          </el-form-item>
          <el-form-item :label="$t('capability.svcLabelSelector')">
            <el-input v-model="capForm.svc_label_selector"
              :placeholder="$t('capability.svcLabelSelectorPlaceholder')" />
          </el-form-item>
          <el-form-item :label="$t('capability.svcNamespaces')">
            <el-input v-model="capForm.svc_namespaces"
              :placeholder="$t('capability.svcNamespacesPlaceholder')" />
          </el-form-item>
          <el-form-item :label="$t('capability.svcListenPortBase')">
            <el-input-number v-model="capForm.svc_listen_port_base" :min="1" :max="65535"
              :placeholder="$t('capability.svcListenPortBasePlaceholder')" controls-position="right" />
          </el-form-item>

          <!-- Endpoint -->
          <el-divider content-position="left">{{ $t('capability.endpoint') }}</el-divider>
          <el-form-item :label="$t('capability.endpointEnabled')">
            <el-switch v-model="capForm.endpoint_enabled" />
            <el-tag :type="capOriginal.endpoint_enabled !== null ? 'primary' : 'info'" size="small" class="source-tag">
              {{ capOriginal.endpoint_enabled !== null ? $t('capability.sourceRemote') : $t('capability.sourceLocal') }}
            </el-tag>
          </el-form-item>
          <el-form-item :label="$t('capability.endpointListenPort')">
            <el-input-number v-model="capForm.endpoint_listen_port" :min="1" :max="65535"
              :placeholder="$t('capability.endpointListenPortPlaceholder')" controls-position="right" />
          </el-form-item>
          <el-form-item :label="$t('capability.endpointToken')">
            <div class="token-row">
              <template v-if="capOriginal.endpoint_token">
                <el-input :model-value="capOriginal.endpoint_token" readonly class="token-input" />
                <el-button size="small" @click="copyToken">{{ $t('common.copy') }}</el-button>
                <el-button size="small" type="warning" @click="regenerateToken">{{ $t('capability.regenerateToken') }}</el-button>
              </template>
              <span v-else class="token-empty">{{ $t('capability.tokenEmpty') }}</span>
            </div>
          </el-form-item>
        </el-form>

        <el-alert type="info" :closable="false" show-icon>
          <template #title>{{ $t('capability.titleTip') }}</template>
        </el-alert>
      </el-card>
    </template>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, reactive, onMounted } from 'vue'
import { useRoute } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import {
  getNode,
  getNodeCapabilities,
  getNodeRDPStatus,
  updateNodeCapabilities,
  updateNodeHostDomainLabel,
  updateNodeRDPAdminDisable,
  resetNodeCapabilities,
  regenerateEndpointToken,
  type DesktopNodeRDPDiagnostics,
  type NodeDetail,
  type NodeSystemInfo,
} from '@/api/node'
import { formatTime } from '@/utils/time'
import { useI18n } from 'vue-i18n'

const { t } = useI18n()
const route = useRoute()
const loading = ref(false)
const node = ref<NodeDetail | null>(null)

// Desktop RDP 只读诊断与熔断
const rdpLoading = ref(false)
const rdpUpdating = ref(false)
const rdpDiag = ref<DesktopNodeRDPDiagnostics | null>(null)

const formatRDPMode = (mode?: string) => {
  if (mode === 'enabled') return '强制开启 (enabled)'
  if (mode === 'disabled') return '已关闭 (disabled)'
  return '自动开放 (auto)'
}

const editHostDomainLabel = async () => {
  if (!node.value || node.value.type !== 'agent') return
  try {
    const result = await ElMessageBox.prompt('SSH 域名将明确指向当前主机，修改后旧域名立即失效。', '编辑 SSH 域名标识', {
      inputValue: node.value.host_domain_label,
      inputPattern: /^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$/,
      inputErrorMessage: '请输入有效的 DNS 单标签',
      confirmButtonText: '保存',
      cancelButtonText: '取消',
    })
    await updateNodeHostDomainLabel(node.value.id, result.value.trim().toLowerCase())
    ElMessage.success('SSH 域名标识已更新')
    await fetchNode()
  } catch (error) {
    if (error !== 'cancel' && error !== 'close') throw error
  }
}

// 能力配置
const capLoading = ref(false)
const capSaving = ref(false)
const capForm = reactive({
  ssh_enabled: false,
  k8s_enabled: false,
  k8s_listen_port: undefined as number | undefined,
  k8s_api_server: '',
  svc_enabled: false,
  svc_label_selector: '',
  svc_namespaces: '',
  svc_listen_port_base: undefined as number | undefined,
  endpoint_enabled: false,
  endpoint_listen_port: undefined as number | undefined,
})
// 保存原始值用于判断来源（远程/本地）
const capOriginal = reactive({
  k8s_enabled: null as boolean | null,
  svc_enabled: null as boolean | null,
  endpoint_enabled: null as boolean | null,
  endpoint_token: '',
})

// 解析系统信息
const systemInfo = computed<NodeSystemInfo | null>(() => {
  if (!node.value?.system_info) return null
  try {
    return JSON.parse(node.value.system_info)
  } catch {
    return null
  }
})

// 获取 Desktop RDP 状态与审计
const fetchDesktopRDPStatus = async () => {
  const id = Number(route.params.id)
  if (!id) return
  rdpLoading.value = true
  try {
    const res = await getNodeRDPStatus(id)
    if (res.success && res.data) {
      rdpDiag.value = res.data
    }
  } catch (error) {
    console.error('获取 Desktop RDP 诊断信息失败:', error)
  } finally {
    rdpLoading.value = false
  }
}

const toggleRDPAdminDisable = async (adminDisabled: boolean) => {
  if (!node.value) return
  try {
    const promptRes = await ElMessageBox.prompt(
      adminDisabled
        ? '启用紧急熔断后，该 Desktop 设备的所有入站远程桌面连接将被立即强制断开，且在解除前无法接收任何远程桌面会话。请输入熔断原因：'
        : '解除紧急熔断后，该设备将恢复遵循机主本人在 Signal Desktop 中设置的开放策略（不会强行代开）。请输入解除备注：',
      adminDisabled ? '确认紧急熔断关闭远程桌面' : '确认解除紧急熔断',
      {
        inputPlaceholder: adminDisabled ? '例如：安全应急处置 / 异常访问阻断' : '例如：核查完毕恢复正常',
        confirmButtonText: adminDisabled ? '立即熔断关闭' : '确认解除',
        cancelButtonText: '取消',
        type: 'warning',
      }
    )
    rdpUpdating.value = true
    const res = await updateNodeRDPAdminDisable(node.value.id, {
      admin_disabled: adminDisabled,
      reason: promptRes.value?.trim() || undefined,
    })
    if (res.success) {
      ElMessage.success(res.message || '操作成功')
      await fetchDesktopRDPStatus()
    }
  } catch (error) {
    if (error !== 'cancel' && error !== 'close') {
      console.error('更新 Desktop RDP 熔断状态失败:', error)
    }
  } finally {
    rdpUpdating.value = false
  }
}

// 获取设备详情
const fetchNode = async () => {
  const id = Number(route.params.id)
  if (!id) return

  loading.value = true
  try {
    const res = await getNode(id)
    if (res.success && res.data) {
      node.value = res.data
      if (res.data.type === 'agent') {
        await fetchCapabilities()
      } else if (res.data.type === 'desktop') {
        await fetchDesktopRDPStatus()
      }
    }
  } catch (error) {
    console.error('获取设备详情失败:', error)
  } finally {
    loading.value = false
  }
}

// 获取能力配置
const fetchCapabilities = async () => {
  const id = Number(route.params.id)
  if (!id) return

  capLoading.value = true
  try {
    const res = await getNodeCapabilities(id)
    if (res.success && res.data) {
      const data = res.data
      capForm.ssh_enabled = data.ssh_enabled
      capForm.k8s_enabled = data.k8s_enabled ?? false
      capForm.k8s_listen_port = data.k8s_listen_port ?? undefined
      capForm.k8s_api_server = data.k8s_api_server || ''
      capForm.svc_enabled = data.svc_enabled ?? false
      capForm.svc_label_selector = data.svc_label_selector || ''
      capForm.svc_namespaces = data.svc_namespaces || ''
      capForm.svc_listen_port_base = data.svc_listen_port_base ?? undefined
      capForm.endpoint_enabled = data.endpoint_enabled ?? false
      capForm.endpoint_listen_port = data.endpoint_listen_port ?? undefined
      // 记录原始值
      capOriginal.k8s_enabled = data.k8s_enabled
      capOriginal.svc_enabled = data.svc_enabled
      capOriginal.endpoint_enabled = data.endpoint_enabled
      capOriginal.endpoint_token = data.endpoint_token || ''
    }
  } catch (error) {
    console.error('获取能力配置失败:', error)
  } finally {
    capLoading.value = false
  }
}

// 保存能力配置
const saveCapabilities = async () => {
  const id = Number(route.params.id)
  if (!id) return

  capSaving.value = true
  try {
    const data: Record<string, any> = {
      ssh_enabled: capForm.ssh_enabled,
      k8s_enabled: capForm.k8s_enabled,
      svc_enabled: capForm.svc_enabled,
      endpoint_enabled: capForm.endpoint_enabled,
    }
    if (capForm.k8s_listen_port !== undefined) {
      data.k8s_listen_port = capForm.k8s_listen_port
    }
    if (capForm.k8s_api_server) {
      data.k8s_api_server = capForm.k8s_api_server
    }
    if (capForm.svc_label_selector) {
      data.svc_label_selector = capForm.svc_label_selector
    }
    if (capForm.svc_namespaces) {
      data.svc_namespaces = capForm.svc_namespaces
    }
    if (capForm.svc_listen_port_base !== undefined) {
      data.svc_listen_port_base = capForm.svc_listen_port_base
    }
    if (capForm.endpoint_listen_port !== undefined) {
      data.endpoint_listen_port = capForm.endpoint_listen_port
    }
    const res = await updateNodeCapabilities(id, data)
    if (res.success) {
      ElMessage.success(t('capability.saveSuccess'))
      await fetchCapabilities()
    } else {
      ElMessage.error(res.message || t('capability.saveFailed'))
    }
  } catch (error) {
    ElMessage.error(t('capability.saveFailed'))
  } finally {
    capSaving.value = false
  }
}

// 重置能力配置
const resetCapabilitiesAction = async () => {
  const id = Number(route.params.id)
  if (!id) return

  try {
    const res = await resetNodeCapabilities(id)
    if (res.success) {
      ElMessage.success(t('capability.resetSuccess'))
      await fetchCapabilities()
    }
  } catch (error) {
    ElMessage.error(t('common.operationFailed'))
  }
}

const resetCapabilities = () => {
  ElMessageBox.confirm(t('capability.resetConfirm'), t('common.warning'), {
    confirmButtonText: t('common.confirm'),
    cancelButtonText: t('common.cancel'),
    type: 'warning',
  }).then(() => {
    resetCapabilitiesAction()
  }).catch(() => {})
}

// 复制 Endpoint Token
const copyToken = async () => {
  try {
    await navigator.clipboard.writeText(capOriginal.endpoint_token)
    ElMessage.success(t('common.copySuccess'))
  } catch {
    ElMessage.error(t('common.copyFailed'))
  }
}

// 重新生成 Endpoint Token
const regenerateToken = () => {
  ElMessageBox.confirm(t('capability.regenerateTokenConfirm'), t('common.warning'), {
    confirmButtonText: t('common.confirm'),
    cancelButtonText: t('common.cancel'),
    type: 'warning',
  }).then(async () => {
    const id = Number(route.params.id)
    if (!id) return
    try {
      const res = await regenerateEndpointToken(id)
      if (res.success) {
        ElMessage.success(t('capability.regenerateTokenSuccess'))
        await fetchCapabilities()
      }
    } catch {
      ElMessage.error(t('common.operationFailed'))
    }
  }).catch(() => {})
}

onMounted(() => {
  fetchNode()
})
</script>

<style scoped>
.node-detail {
  width: 100%;
}

.info-card {
  margin-bottom: 20px;
}

.card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 12px;
  flex-wrap: wrap;
}

.rdp-header-left {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}

.rdp-header-actions {
  display: flex;
  align-items: center;
  gap: 8px;
}

.rdp-privacy-alert {
  margin-bottom: 16px;
}

.rdp-descriptions {
  margin-bottom: 16px;
}

.rdp-sub-table {
  margin-bottom: 16px;
}

.session-sub {
  color: var(--el-text-color-secondary);
  font-size: 12px;
}

.user-link {
  color: var(--el-color-primary);
  text-decoration: none;
}

.user-link:hover {
  text-decoration: underline;
}

.ip-tag {
  margin-right: 8px;
  margin-bottom: 4px;
}

.tag-item {
  margin-right: 8px;
  margin-bottom: 4px;
}

.source-tag {
  margin-left: 8px;
}

.token-row {
  display: flex;
  align-items: center;
  gap: 8px;
  width: 100%;
}

.token-input {
  flex: 1;
}

.token-empty {
  color: var(--el-text-color-placeholder);
  font-size: 13px;
}
</style>

<style>
.node-detail .el-descriptions__body .el-descriptions__table {
  table-layout: fixed;
}

.node-detail .el-descriptions__label {
  width: 110px !important;
  min-width: 110px !important;
  max-width: 110px !important;
}
</style>
