<template>
  <div class="agent-detail">
    <div v-loading="loading">
      <!-- 基本信息卡片 -->
      <el-card class="info-card">
        <template #header>
          <div class="card-header">
            <span class="card-title">{{ t('agent.basicInfo') }}</span>
            <div class="header-actions">
              <el-button size="small" :icon="Refresh" @click="loadAgent">
                {{ t('common.refresh') }}
              </el-button>
              <el-button
                v-if="!isEditing"
                type="primary"
                size="small"
                :icon="Edit"
                @click="startEditing"
              >
                {{ t('common.edit') }}
              </el-button>
              <template v-else>
                <el-button size="small" @click="cancelEditing">
                  {{ t('common.cancel') }}
                </el-button>
                <el-button type="primary" size="small" @click="saveChanges">
                  {{ t('common.save') }}
                </el-button>
              </template>
            </div>
          </div>
        </template>
        
        <div class="info-row">
          <div class="info-item">
            <span class="info-label">{{ t('agent.name') }}</span>
            <span class="info-value">{{ agent?.name || '-' }}</span>
          </div>
          <div class="info-item">
            <span class="info-label">{{ t('agent.alias') }}</span>
            <el-input
              v-if="isEditing"
              v-model="editForm.alias"
              :placeholder="t('agent.aliasPlaceholder')"
              size="small"
              style="width: 150px"
            />
            <span v-else class="info-value">{{ agent?.alias || '-' }}</span>
          </div>
          <div class="info-item">
            <span class="info-label">{{ t('common.status') }}</span>
            <span class="info-value status-value">
              <template v-if="agent?.status === 'online'">
                <el-tag type="success" size="small">{{ t('common.online') }}</el-tag>
                <span class="status-time">
                  <TimeAgo v-if="realtimeInfo?.tunnel_connected_time" :time="realtimeInfo.tunnel_connected_time * 1000" />
                </span>
              </template>
              <template v-else>
                <el-tag type="info" size="small">{{ t('common.offline') }}</el-tag>
                <span class="status-time">
                  (<TimeAgo v-if="agent?.last_online" :time="agent.last_online" />)
                </span>
              </template>
            </span>
          </div>
          <div class="info-item">
            <span class="info-label">{{ t('agent.version') }}</span>
            <span class="info-value">{{ agent?.version || '-' }}</span>
          </div>
          <div class="info-item">
            <span class="info-label">{{ t('agent.createdAt') }}</span>
            <span class="info-value">
              {{ agent?.created_at ? formatDate(agent.created_at) : '-' }}
            </span>
          </div>
        </div>
      </el-card>

      <!-- 运行环境卡片 -->
      <el-card class="info-card">
        <template #header>
          <span class="card-title">{{ t('agent.runtimeEnv') }}</span>
        </template>
        
        <div class="info-row">
          <div class="info-item">
            <span class="info-label">{{ t('agent.hostname') }}</span>
            <span class="info-value">{{ realtimeInfo?.hostname || '-' }}</span>
          </div>
          <div class="info-item">
            <span class="info-label">{{ t('agent.runtimeEnv') }}</span>
            <span class="info-value">
              <span v-if="realtimeInfo?.runtime === 'docker'">🐳 {{ t('agent.runtimeDocker') }}</span>
              <span v-else-if="realtimeInfo?.runtime === 'kubernetes'">☸️ {{ t('agent.runtimeKubernetes') }}</span>
              <span v-else-if="realtimeInfo?.runtime === 'physical'">🖥️ {{ t('agent.runtimeNative') }}</span>
              <span v-else>-</span>
            </span>
          </div>
        </div>
      </el-card>

      <!-- 网络信息卡片 -->
      <el-card class="info-card">
        <template #header>
          <span class="card-title">{{ t('agent.networkInfo') }}</span>
        </template>
        
        <el-table 
          v-if="realtimeInfo?.networks && realtimeInfo.networks.length > 0" 
          :data="realtimeInfo.networks" 
          stripe
          size="small"
          :show-header="true"
          table-layout="fixed"
        >
          <el-table-column prop="name" :label="t('agent.lanInterface')" />
          <el-table-column prop="ip" :label="t('agent.lanIp')" />
          <el-table-column prop="mask" :label="t('agent.lanMask')" />
          <el-table-column prop="gateway" :label="t('agent.lanGateway')" />
        </el-table>
        <div v-else class="info-row">
          <div class="info-item">
            <span class="info-value">-</span>
          </div>
        </div>
      </el-card>

      <!-- 隧道信息卡片 -->
      <el-card class="info-card">
        <template #header>
          <span class="card-title">{{ t('agent.tailscaleInfo') }}</span>
        </template>
        
        <div class="info-row">
          <div class="info-item">
            <span class="info-label">{{ t('agent.tailscaleIp') }}</span>
            <span class="info-value">{{ realtimeInfo?.tunnel_ip || agent?.ip || '-' }}</span>
          </div>
          <div class="info-item">
            <span class="info-label">{{ t('common.status') }}</span>
            <span class="info-value status-value">
              <template v-if="realtimeInfo?.tunnel_connected">
                <el-tag type="success" size="small">{{ t('agent.tsConnected') }}</el-tag>
                <span class="status-time">
                  <TimeAgo v-if="realtimeInfo?.tunnel_connected_time" :time="realtimeInfo.tunnel_connected_time * 1000" />
                </span>
              </template>
              <template v-else>
                <el-tag type="info" size="small">{{ t('agent.tsDisconnected') }}</el-tag>
              </template>
            </span>
          </div>
        </div>
      </el-card>

      <!-- 端口映射服务列表 -->
      <el-card class="info-card">
        <template #header>
          <div class="card-header">
            <span class="card-title">
              {{ t('agent.serviceList') }} ({{ agent?.services?.length || 0 }})
            </span>
            <el-button type="primary" size="small" :icon="Plus" @click="handleCreateService">
              {{ t('agent.createService') }}
            </el-button>
          </div>
        </template>
        
        <el-table v-if="agent?.services && agent.services.length > 0" :data="agent.services" stripe>
          <el-table-column prop="name" :label="t('service.name')" min-width="120" />
          <el-table-column prop="alias" :label="t('agent.alias')" min-width="100">
            <template #default="{ row }">
              {{ row.alias || '-' }}
            </template>
          </el-table-column>
          <el-table-column prop="source_addr" label="源地址" min-width="150" />
          <el-table-column prop="target_addr" :label="t('service.targetAddr')" min-width="150" />
          <el-table-column label="状态" width="100">
            <template #default="{ row }">
              <el-tooltip v-if="row.display_status === 'error'" :content="row.error_msg || '未知错误'" placement="top">
                <span class="status-text status-error">🔴 错误</span>
              </el-tooltip>
              <span v-else-if="row.display_status === 'running'" class="status-text status-running">🟢 运行</span>
              <span v-else-if="row.display_status === 'disabled'" class="status-text status-disabled">⚫ 禁用</span>
              <span v-else-if="row.display_status === 'offline'" class="status-text status-offline">🔵 离线</span>
              <span v-else-if="row.display_status === 'pending'" class="status-text status-pending">🟡 等待</span>
              <span v-else class="status-text">-</span>
            </template>
          </el-table-column>
          <el-table-column :label="t('common.actions')" width="200" fixed="right">
            <template #default="{ row }">
              <div class="action-buttons">
                <el-tooltip v-if="row.display_status === 'error'" content="重试" placement="top">
                  <el-button size="small" :icon="RefreshRight" circle @click="handleRetryService(row)" />
                </el-tooltip>
                <el-tooltip v-else-if="row.display_status === 'running'" content="禁用" placement="top">
                  <el-button size="small" :icon="VideoPause" circle @click="handleToggleService(row, false)" />
                </el-tooltip>
                <el-tooltip v-else content="启用" placement="top">
                  <el-button size="small" :icon="VideoPlay" circle @click="handleToggleService(row, true)" />
                </el-tooltip>
                <el-tooltip content="编辑" placement="top">
                  <el-button size="small" :icon="Edit" circle @click="handleEditService(row)" />
                </el-tooltip>
                <el-tooltip content="删除" placement="top">
                  <el-button
                    size="small"
                    type="danger"
                    :icon="Delete"
                    circle
                    @click="handleDeleteService(row)"
                  />
                </el-tooltip>
              </div>
            </template>
          </el-table-column>
        </el-table>
        
        <el-empty v-else :description="t('agent.noServices')" />
      </el-card>

      <!-- 端口访问服务列表 (Visitor) -->
      <el-card class="info-card">
        <template #header>
          <div class="card-header">
            <span class="card-title">
              {{ t('agent.visitorList') }} ({{ agent?.forwards?.length || 0 }})
            </span>
            <el-button type="primary" size="small" :icon="Plus" @click="handleAddVisitor">
              {{ t('agent.addVisitor') }}
            </el-button>
          </div>
        </template>
        
        <el-table v-if="agent?.forwards && agent.forwards.length > 0" :data="agent.forwards" stripe>
          <el-table-column prop="name" :label="t('service.name')" min-width="100" />
          <el-table-column prop="alias" :label="t('agent.alias')" min-width="80">
            <template #default="{ row }">
              {{ row.alias || '-' }}
            </template>
          </el-table-column>
          <el-table-column :label="t('agent.targetService')" min-width="150">
            <template #default="{ row }">
              {{ row.target_agent_name || '-' }}/{{ row.target_service_name || '-' }}
            </template>
          </el-table-column>
          <el-table-column prop="source_addr" label="源地址" min-width="150" />
          <el-table-column prop="target_addr" :label="t('service.targetAddr')" min-width="150" />
          <el-table-column label="状态" width="100">
            <template #default="{ row }">
              <el-tooltip v-if="row.display_status === 'error'" :content="row.error_msg || '未知错误'" placement="top">
                <span class="status-text status-error">🔴 错误</span>
              </el-tooltip>
              <span v-else-if="row.display_status === 'running'" class="status-text status-running">🟢 运行</span>
              <span v-else-if="row.display_status === 'disabled'" class="status-text status-disabled">⚫ 禁用</span>
              <span v-else-if="row.display_status === 'offline'" class="status-text status-offline">🔵 离线</span>
              <span v-else-if="row.display_status === 'pending'" class="status-text status-pending">🟡 等待</span>
              <span v-else class="status-text">-</span>
            </template>
          </el-table-column>
          <el-table-column :label="t('common.actions')" width="200" fixed="right">
            <template #default="{ row }">
              <div class="action-buttons">
                <el-tooltip v-if="row.display_status === 'error'" content="重试" placement="top">
                  <el-button size="small" :icon="RefreshRight" circle @click="handleRetryVisitor(row)" />
                </el-tooltip>
                <el-tooltip v-else-if="row.display_status === 'running'" content="禁用" placement="top">
                  <el-button size="small" :icon="VideoPause" circle @click="handleToggleVisitor(row, false)" />
                </el-tooltip>
                <el-tooltip v-else content="启用" placement="top">
                  <el-button size="small" :icon="VideoPlay" circle @click="handleToggleVisitor(row, true)" />
                </el-tooltip>
                <el-tooltip content="编辑" placement="top">
                  <el-button size="small" :icon="Edit" circle @click="handleEditVisitor(row)" />
                </el-tooltip>
                <el-tooltip content="删除" placement="top">
                  <el-button
                    size="small"
                    type="danger"
                    :icon="Delete"
                    circle
                    @click="handleDeleteVisitor(row)"
                  />
                </el-tooltip>
              </div>
            </template>
          </el-table-column>
        </el-table>
        
        <el-empty v-else :description="t('agent.noVisitors')" />
      </el-card>
    </div>

    <!-- 创建本地服务对话框 -->
    <el-dialog v-model="createServiceDialogVisible" title="创建本地服务" width="600px">
      <el-form :model="createServiceForm" label-width="100px">
        <el-form-item label="服务名称" required>
          <el-input v-model="createServiceForm.name" placeholder="例如: ssh" />
        </el-form-item>
        <el-form-item label="别名">
          <el-input v-model="createServiceForm.alias" placeholder="例如: 内网 Web服务" />
        </el-form-item>
        <el-form-item label="源地址" required>
          <div style="display: flex; gap: 8px; align-items: center;">
            <el-input v-model="vpnIp" readonly style="flex: 1;" placeholder="VPN IP" />
            <span>:</span>
            <el-input v-model="createServiceForm.source_port" placeholder="2222" style="flex: 1;" />
          </div>
          <div class="form-tip">VPN IP（自动填充） : 源端口</div>
          <div class="form-tip">ⓘ 内网中，在保证端口不冲突的情况下，可以使用相同的源端口</div>
        </el-form-item>
        <el-form-item label="目标地址" required>
          <el-input v-model="createServiceForm.target_addr" placeholder="例如: 127.0.0.1:22" />
          <div class="form-tip">ⓘ 本机内网IP地址和端口</div>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="createServiceDialogVisible = false">取消</el-button>
        <el-button type="primary" @click="handleSubmitCreateService" :loading="creatingService">
          创建
        </el-button>
      </template>
    </el-dialog>

    <!-- 创建远程服务对话框 -->
    <el-dialog v-model="createVisitorDialogVisible" title="创建远程服务" width="600px">
      <el-form :model="createVisitorForm" label-width="100px">
        <el-form-item label="远程 Agent" required>
          <el-select 
            v-model="createVisitorForm.target_agent_id" 
            placeholder="选择远程 Agent" 
            style="width: 100%"
            @change="handleRemoteAgentChange"
          >
            <el-option
              v-for="ag in availableRemoteAgents"
              :key="ag.id"
              :label="ag.alias ? `${ag.name} (${ag.alias})` : ag.name"
              :value="ag.id"
            />
          </el-select>
        </el-form-item>
        <el-form-item label="远程服务" required>
          <el-select 
            v-model="createVisitorForm.target_service_id" 
            placeholder="选择要访问的服务" 
            style="width: 100%"
            @change="handleRemoteServiceChange"
          >
            <el-option
              v-for="svc in remoteAgentServices"
              :key="svc.id"
              :label="svc.alias ? `${svc.name} (${svc.alias})` : svc.name"
              :value="svc.id"
            />
          </el-select>
          <div v-if="selectedRemoteService" class="form-tip">
            目标地址: {{ selectedRemoteService.listen_addr }}
          </div>
        </el-form-item>
        <el-form-item label="源地址" required>
          <div style="display: flex; gap: 8px; align-items: center;">
            <el-select v-model="createVisitorForm.network_interface" placeholder="选择网卡" style="flex: 1;">
              <el-option
                v-for="net in realtimeInfo?.networks"
                :key="net.name"
                :label="`${net.name}: ${net.ip}`"
                :value="net.name"
              />
            </el-select>
            <span>:</span>
            <el-input v-model="createVisitorForm.source_port" placeholder="13306" style="flex: 1;" />
          </div>
          <div class="form-tip">本地网卡 : 源端口</div>
          <div class="form-tip">ⓘ 选择本地网卡和端口，用于访问远程服务</div>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="createVisitorDialogVisible = false">取消</el-button>
        <el-button type="primary" @click="handleSubmitCreateVisitor" :loading="creatingVisitor">
          创建
        </el-button>
      </template>
    </el-dialog>

    <!-- 编辑本地服务对话框 -->
    <el-dialog v-model="editServiceDialogVisible" title="编辑本地服务" width="600px">
      <el-form :model="editServiceForm" label-width="100px">
        <el-form-item label="别名">
          <el-input v-model="editServiceForm.alias" placeholder="例如: Web服务" />
        </el-form-item>
        <el-form-item label="源地址" required>
          <el-input v-model="editServiceForm.source_addr" placeholder="例如: 100.64.0.7:8080" />
          <div class="form-tip">VPN IP:端口</div>
        </el-form-item>
        <el-form-item label="目标地址" required>
          <el-input v-model="editServiceForm.target_addr" placeholder="例如: 192.168.1.10:80" />
          <div class="form-tip">Agent 内网中的服务地址</div>
        </el-form-item>
        <el-form-item label="状态">
          <el-switch v-model="editServiceForm.enabled" active-text="启用" inactive-text="禁用" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="editServiceDialogVisible = false">取消</el-button>
        <el-button type="primary" @click="handleSubmitEditService" :loading="editingService">
          保存
        </el-button>
      </template>
    </el-dialog>

    <!-- 编辑远程服务对话框 -->
    <el-dialog v-model="editVisitorDialogVisible" title="编辑远程服务" width="600px">
      <el-form :model="editVisitorForm" label-width="100px">
        <el-form-item label="别名">
          <el-input v-model="editVisitorForm.alias" placeholder="例如: MySQL数据库" />
        </el-form-item>
        <el-form-item label="目标服务" required>
          <el-select v-model="editVisitorForm.target_service_id" placeholder="选择要访问的服务" style="width: 100%">
            <el-option
              v-for="svc in availableServices"
              :key="svc.id"
              :label="`${svc.agent_name}/${svc.name} (${svc.listen_addr})`"
              :value="svc.id"
            />
          </el-select>
          <div class="form-tip">选择其他 Agent 提供的服务</div>
        </el-form-item>
        <el-form-item label="本地监听" required>
          <el-input v-model="editVisitorForm.listen_addr" placeholder="例如: 192.168.1.100:13306" />
          <div class="form-tip">在当前 Agent 的内网中监听的地址</div>
        </el-form-item>
        <el-form-item label="状态">
          <el-switch v-model="editVisitorForm.enabled" active-text="启用" inactive-text="禁用" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="editVisitorDialogVisible = false">取消</el-button>
        <el-button type="primary" @click="handleSubmitEditVisitor" :loading="editingVisitor">
          保存
        </el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted, watch, reactive } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Plus, Delete, Edit, Refresh, VideoPlay, VideoPause, RefreshRight } from '@element-plus/icons-vue'
import { getAgent, getAgentRealtime, updateAgent } from '@/api/agent'
import { deleteService } from '@/api/service'
import request from '@/utils/request'
import type { AgentDetail, ProxyService, PortForward } from '@/types/models'
import TimeAgo from '@/components/Common/TimeAgo.vue'

// 实时信息接口
interface RealtimeInfo {
  hostname: string
  runtime: string
  tunnel_ip: string
  tunnel_connected: boolean
  tunnel_connected_time: number
  networks: Array<{
    name: string
    ip: string
    gateway: string
  }>
}

const { t } = useI18n()
const route = useRoute()
const router = useRouter()

const loading = ref(false)
const agent = ref<AgentDetail | null>(null)
const realtimeInfo = ref<RealtimeInfo | null>(null)
const isEditing = ref(false)
const editForm = reactive({
  alias: ''
})

// 创建本地服务对话框
const createServiceDialogVisible = ref(false)
const createServiceForm = reactive({
  name: '',
  alias: '',
  source_port: '',
  target_addr: ''
})
const creatingService = ref(false)
const vpnIp = ref('') // VPN IP，自动填充

// 编辑本地服务对话框
const editServiceDialogVisible = ref(false)
const editServiceForm = reactive({
  id: '',
  alias: '',
  source_addr: '',
  target_addr: '',
  enabled: true
})
const editingService = ref(false)

// 创建远程服务对话框
const createVisitorDialogVisible = ref(false)
const createVisitorForm = reactive({
  target_agent_id: '',
  target_service_id: '',
  network_interface: '',
  source_port: ''
})
const creatingVisitor = ref(false)
const availableRemoteAgents = ref<any[]>([]) // 可选的远程 Agent 列表（排除当前 Agent）
const remoteAgentServices = ref<any[]>([]) // 选中的远程 Agent 的服务列表
const selectedRemoteService = ref<any>(null) // 选中的远程服务

// 编辑远程服务对话框
const editVisitorDialogVisible = ref(false)
const editVisitorForm = reactive({
  id: '',
  alias: '',
  target_service_id: '',
  listen_addr: '',
  enabled: true
})
const editingVisitor = ref(false)

const availableServices = ref<any[]>([]) // 可选的目标服务列表

// 格式化日期
const formatDate = (dateStr: string) => {
  const date = new Date(dateStr)
  return date.toLocaleDateString('zh-CN', {
    year: 'numeric',
    month: '2-digit',
    day: '2-digit'
  })
}

const loadAgent = async () => {
  const id = Number(route.params.id)
  if (!id) {
    ElMessage.error('Invalid Agent ID')
    router.push('/agents')
    return
  }

  loading.value = true
  try {
    const res = await getAgent(id)
    if (res.success && res.data) {
      agent.value = res.data
      // 自动填充 VPN IP
      vpnIp.value = res.data.ip || ''
      // 如果 Agent 在线，获取实时信息
      if (res.data.status === 'online') {
        loadRealtimeInfo(id)
      }
    } else {
      ElMessage.error(res.message || t('common.failed'))
      router.push('/agents')
    }
  } catch (error) {
    ElMessage.error(t('common.failed'))
    router.push('/agents')
  } finally {
    loading.value = false
  }
}

const loadRealtimeInfo = async (id: number) => {
  try {
    const res = await getAgentRealtime(id)
    if (res.success && res.data) {
      realtimeInfo.value = res.data
      // 更新 VPN IP（优先使用实时信息）
      if (res.data.tunnel_ip) {
        vpnIp.value = res.data.tunnel_ip
      }
    }
  } catch (error) {
    console.error('Failed to load realtime info:', error)
  }
}

const startEditing = () => {
  editForm.alias = agent.value?.alias || ''
  isEditing.value = true
}

const cancelEditing = () => {
  isEditing.value = false
}

const saveChanges = async () => {
  if (!agent.value) return

  try {
    const res = await updateAgent(agent.value.id, { alias: editForm.alias })
    if (res.success) {
      ElMessage.success(t('common.success'))
      isEditing.value = false
      loadAgent()
    } else {
      ElMessage.error(res.message || t('common.failed'))
    }
  } catch (error) {
    ElMessage.error(t('common.failed'))
  }
}

const handleCreateService = () => {
  createServiceDialogVisible.value = true
  createServiceForm.name = ''
  createServiceForm.alias = ''
  createServiceForm.source_port = ''
  createServiceForm.target_addr = ''
}

const handleEditService = (service: ProxyService) => {
  editServiceForm.id = service.id
  editServiceForm.alias = service.alias || ''
  editServiceForm.source_addr = service.source_addr
  editServiceForm.target_addr = service.target_addr
  editServiceForm.enabled = service.enabled
  editServiceDialogVisible.value = true
}

// 启用/禁用服务
const handleToggleService = async (service: ProxyService, enabled: boolean) => {
  try {
    const res = await request({
      url: `/api/v1/admin/services/${service.id}/toggle`,
      method: 'put',
      data: { enabled }
    })

    if (res.success) {
      ElMessage.success(enabled ? '服务已启用' : '服务已禁用')
      loadAgent()
    } else {
      ElMessage.error(res.message || '操作失败')
    }
  } catch (error) {
    ElMessage.error('操作失败')
  }
}

// 重试错误状态的服务
const handleRetryService = async (service: ProxyService) => {
  try {
    const res = await request({
      url: `/api/v1/admin/services/${service.id}/retry`,
      method: 'post'
    })

    if (res.success) {
      ElMessage.success('重试成功')
      loadAgent()
    } else {
      ElMessage.error(res.message || '重试失败')
    }
  } catch (error) {
    ElMessage.error('重试失败')
  }
}

// 旧的启用/禁用方法（保留兼容性，但建议使用 handleToggleService）
const handleEnableService = async (service: ProxyService) => {
  await handleToggleService(service, true)
}

const handleDisableService = async (service: ProxyService) => {
  await handleToggleService(service, false)
}

const handleDeleteService = async (service: ProxyService) => {
  try {
    await ElMessageBox.confirm(t('common.deleteConfirm'), {
      type: 'warning'
    })
    
    const res = await deleteService(service.id)
    if (res.success) {
      ElMessage.success(t('common.deleteSuccess'))
      loadAgent()
    } else {
      ElMessage.error(res.message || t('common.failed'))
    }
  } catch (error: any) {
    if (error !== 'cancel') {
      ElMessage.error(t('common.failed'))
    }
  }
}

// Visitor 操作
const handleAddVisitor = async () => {
  // 加载可用的远程 Agent 列表（排除当前 Agent）
  await loadAvailableRemoteAgents()
  createVisitorDialogVisible.value = true
  createVisitorForm.target_agent_id = ''
  createVisitorForm.target_service_id = ''
  createVisitorForm.network_interface = ''
  createVisitorForm.source_port = ''
  remoteAgentServices.value = []
  selectedRemoteService.value = null
}

const handleEditVisitor = (visitor: PortForward) => {
  editVisitorForm.id = visitor.id
  editVisitorForm.alias = visitor.alias || ''
  editVisitorForm.target_service_id = visitor.target_service_id || ''
  editVisitorForm.listen_addr = visitor.listen_addr
  editVisitorForm.enabled = visitor.enabled
  editVisitorDialogVisible.value = true
}

// 启用/禁用远程服务
const handleToggleVisitor = async (visitor: PortForward, enabled: boolean) => {
  try {
    const res = await request({
      url: `/api/v1/admin/port-forwards/${visitor.id}/toggle`,
      method: 'put',
      data: { enabled }
    })
    if (res.success) {
      ElMessage.success(enabled ? '远程服务已启用' : '远程服务已禁用')
      loadAgent()
    } else {
      ElMessage.error(res.message || '操作失败')
    }
  } catch (error) {
    ElMessage.error('操作失败')
  }
}

// 重试错误状态的远程服务
const handleRetryVisitor = async (visitor: PortForward) => {
  try {
    const res = await request({
      url: `/api/v1/admin/port-forwards/${visitor.id}/retry`,
      method: 'post'
    })
    if (res.success) {
      ElMessage.success('重试成功')
      loadAgent()
    } else {
      ElMessage.error(res.message || '重试失败')
    }
  } catch (error) {
    ElMessage.error('重试失败')
  }
}

// 旧的启用/禁用方法（保留兼容性）
const handleEnableVisitor = async (visitor: PortForward) => {
  await handleToggleVisitor(visitor, true)
}

const handleDisableVisitor = async (visitor: PortForward) => {
  await handleToggleVisitor(visitor, false)
}

const handleDeleteVisitor = async (visitor: PortForward) => {
  try {
    await ElMessageBox.confirm(t('common.deleteConfirm'), {
      type: 'warning'
    })
    
    const res = await request({
      url: `/api/v1/admin/port-forwards/${visitor.id}`,
      method: 'delete'
    })
    if (res.success) {
      ElMessage.success(t('common.deleteSuccess'))
      loadAgent()
    } else {
      ElMessage.error(res.message || t('common.failed'))
    }
  } catch (error: any) {
    if (error !== 'cancel') {
      ElMessage.error(t('common.failed'))
    }
  }
}

// Watch for route changes
watch(() => route.params.id, () => {
  if (route.params.id) {
    loadAgent()
  }
})

// 提交创建本地服务
const handleSubmitCreateService = async () => {
  if (!agent.value || !createServiceForm.name || !createServiceForm.source_port || !createServiceForm.target_addr) {
    ElMessage.warning('请填写完整信息')
    return
  }

  // 组合源地址：VPN IP + 端口
  const source_addr = `${vpnIp.value}:${createServiceForm.source_port}`

  creatingService.value = true
  try {
    const res = await request({
      url: '/api/v1/admin/services',
      method: 'post',
      data: {
        agent_id: agent.value.id,
        name: createServiceForm.name,
        alias: createServiceForm.alias,
        source_addr: source_addr,
        target_addr: createServiceForm.target_addr
      }
    })

    if (res.success) {
      ElMessage.success('创建成功')
      createServiceDialogVisible.value = false
      // 重置表单
      createServiceForm.name = ''
      createServiceForm.alias = ''
      createServiceForm.source_port = ''
      createServiceForm.target_addr = ''
      loadAgent()
    } else {
      ElMessage.error(res.message || '创建失败')
    }
  } catch (error) {
    ElMessage.error('创建失败')
  } finally {
    creatingService.value = false
  }
}

// 加载可用的远程 Agent 列表（排除当前 Agent）
const loadAvailableRemoteAgents = async () => {
  if (!agent.value) return
  
  try {
    const res = await request({
      url: '/api/v1/admin/agents',
      method: 'get',
      params: { page: 1, size: 1000, status: 'online' }
    })
    if (res.success && res.data) {
      // 排除当前 Agent
      availableRemoteAgents.value = (res.data || []).filter((ag: any) => ag.id !== agent.value?.id)
    }
  } catch (error) {
    console.error('Failed to load remote agents:', error)
  }
}

// 当选择远程 Agent 时，加载该 Agent 的服务列表
const handleRemoteAgentChange = async (agentId: string) => {
  createVisitorForm.target_service_id = ''
  selectedRemoteService.value = null
  
  if (!agentId) {
    remoteAgentServices.value = []
    return
  }
  
  try {
    const res = await request({
      url: '/api/v1/admin/services',
      method: 'get',
      params: { agent_id: agentId, status: 'running' }
    })
    if (res.success && res.data) {
      remoteAgentServices.value = res.data || []
    }
  } catch (error) {
    console.error('Failed to load remote agent services:', error)
    ElMessage.error('加载远程服务失败')
  }
}

// 当选择远程服务时，更新选中的服务信息
const handleRemoteServiceChange = (serviceId: string) => {
  selectedRemoteService.value = remoteAgentServices.value.find(svc => svc.id === serviceId)
}

// 提交创建远程服务
const handleSubmitCreateVisitor = async () => {
  if (!agent.value || !createVisitorForm.target_service_id || !createVisitorForm.network_interface || !createVisitorForm.source_port) {
    ElMessage.warning('请填写完整信息')
    return
  }

  // 获取选中网卡的 IP
  const selectedNetwork = realtimeInfo.value?.networks?.find(net => net.name === createVisitorForm.network_interface)
  if (!selectedNetwork) {
    ElMessage.error('未找到选中的网卡信息')
    return
  }

  // 组合监听地址：网卡IP + 端口
  const listen_addr = `${selectedNetwork.ip}:${createVisitorForm.source_port}`

  // 自动生成服务名称：remote_agent_name/service_name
  const targetService = selectedRemoteService.value
  if (!targetService) {
    ElMessage.error('未找到目标服务信息')
    return
  }

  const targetAgent = availableRemoteAgents.value.find(ag => ag.id === createVisitorForm.target_agent_id)
  const name = `${targetAgent?.name || 'unknown'}/${targetService.name}`

  creatingVisitor.value = true
  try {
    const res = await request({
      url: '/api/v1/admin/port-forwards',
      method: 'post',
      data: {
        agent_id: agent.value.id,
        name: name,
        target_service_id: createVisitorForm.target_service_id,
        listen_addr: listen_addr
      }
    })

    if (res.success) {
      ElMessage.success('创建成功')
      createVisitorDialogVisible.value = false
      loadAgent()
    } else {
      ElMessage.error(res.message || '创建失败')
    }
  } catch (error) {
    ElMessage.error('创建失败')
  } finally {
    creatingVisitor.value = false
  }
}

// 提交编辑本地服务
const handleSubmitEditService = async () => {
  if (!editServiceForm.target_addr || !editServiceForm.source_addr) {
    ElMessage.warning('请填写完整信息')
    return
  }

  editingService.value = true
  try {
    const res = await request({
      url: `/api/v1/admin/services/${editServiceForm.id}`,
      method: 'put',
      data: {
        alias: editServiceForm.alias,
        source_addr: editServiceForm.source_addr,
        target_addr: editServiceForm.target_addr,
        enabled: editServiceForm.enabled
      }
    })

    if (res.success) {
      ElMessage.success('更新成功')
      editServiceDialogVisible.value = false
      loadAgent()
    } else {
      ElMessage.error(res.message || '更新失败')
    }
  } catch (error) {
    ElMessage.error('更新失败')
  } finally {
    editingService.value = false
  }
}

// 提交编辑远程服务
const handleSubmitEditVisitor = async () => {
  if (!editVisitorForm.target_service_id || !editVisitorForm.listen_addr) {
    ElMessage.warning('请填写完整信息')
    return
  }

  editingVisitor.value = true
  try {
    const res = await request({
      url: `/api/v1/admin/port-forwards/${editVisitorForm.id}`,
      method: 'put',
      data: {
        alias: editVisitorForm.alias,
        target_service_id: editVisitorForm.target_service_id,
        listen_addr: editVisitorForm.listen_addr,
        enabled: editVisitorForm.enabled
      }
    })

    if (res.success) {
      ElMessage.success('更新成功')
      editVisitorDialogVisible.value = false
      loadAgent()
    } else {
      ElMessage.error(res.message || '更新失败')
    }
  } catch (error) {
    ElMessage.error('更新失败')
  } finally {
    editingVisitor.value = false
  }
}

onMounted(() => {
  loadAgent()
})
</script>

<style scoped>
.agent-detail {
  width: 100%;
}

.info-card {
  margin-bottom: 16px;
}

.card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.header-actions {
  display: flex;
  align-items: center;
  gap: 8px;
}

.card-title {
  font-size: 16px;
  font-weight: 500;
  color: var(--text-primary);
}

.info-row {
  display: flex;
  flex-wrap: wrap;
  gap: 32px;
  padding: 8px 0;
  min-height: 40px;
}

.info-item {
  display: flex;
  flex-direction: column;
  gap: 4px;
  min-width: 120px;
}

.info-label {
  font-size: 12px;
  color: var(--el-text-color-secondary);
  line-height: 20px;
}

.info-value {
  font-size: 14px;
  color: var(--el-text-color-primary);
  line-height: 22px;
}

.status-value {
  display: flex;
  align-items: center;
  gap: 8px;
}

.status-time {
  font-size: 12px;
  color: var(--el-text-color-secondary);
}

.action-buttons {
  display: flex;
  align-items: center;
  gap: 4px;
  flex-wrap: nowrap;
}

.status-text {
  font-size: 14px;
  line-height: 22px;
  white-space: nowrap;
}

.status-running {
  color: var(--el-color-success);
}

.status-error {
  color: var(--el-color-danger);
  cursor: pointer;
}

.status-disabled {
  color: var(--el-text-color-secondary);
}

.status-offline {
  color: var(--el-color-info);
}

.status-pending {
  color: var(--el-color-warning);
}

.form-tip {
  font-size: 12px;
  color: var(--el-text-color-secondary);
  margin-top: 4px;
}
</style>
