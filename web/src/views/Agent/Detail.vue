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
          <el-table-column prop="target_addr" :label="t('service.targetAddr')" min-width="150" />
          <el-table-column :label="t('service.listenAddr')" min-width="150">
            <template #default="{ row }">
              {{ row.listen_addr || '-' }}
            </template>
          </el-table-column>
          <el-table-column :label="t('common.actions')" width="180" fixed="right">
            <template #default="{ row }">
              <div class="action-buttons">
                <el-button size="small" :icon="Edit" @click="handleEditService(row)" />
                <el-button
                  v-if="row.enabled"
                  size="small"
                  type="warning"
                  @click="handleDisableService(row)"
                >
                  {{ t('tcp.disable') }}
                </el-button>
                <el-button
                  v-else
                  size="small"
                  type="success"
                  @click="handleEnableService(row)"
                >
                  {{ t('tcp.enable') }}
                </el-button>
                <el-button
                  size="small"
                  type="danger"
                  :icon="Delete"
                  @click="handleDeleteService(row)"
                />
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
          <el-table-column prop="target_addr" :label="t('service.targetAddr')" min-width="150" />
          <el-table-column prop="listen_addr" :label="t('agent.localListenAddr')" min-width="150" />
          <el-table-column :label="t('common.actions')" width="180" fixed="right">
            <template #default="{ row }">
              <div class="action-buttons">
                <el-button size="small" :icon="Edit" @click="handleEditVisitor(row)" />
                <el-button
                  v-if="row.enabled"
                  size="small"
                  type="warning"
                  @click="handleDisableVisitor(row)"
                >
                  {{ t('tcp.disable') }}
                </el-button>
                <el-button
                  v-else
                  size="small"
                  type="success"
                  @click="handleEnableVisitor(row)"
                >
                  {{ t('tcp.enable') }}
                </el-button>
                <el-button
                  size="small"
                  type="danger"
                  :icon="Delete"
                  @click="handleDeleteVisitor(row)"
                />
              </div>
            </template>
          </el-table-column>
        </el-table>
        
        <el-empty v-else :description="t('agent.noVisitors')" />
      </el-card>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted, watch, reactive } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Plus, Delete, Edit, Refresh } from '@element-plus/icons-vue'
import { getAgent, getAgentRealtime, updateAgent } from '@/api/agent'
import { deleteService } from '@/api/service'
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
  router.push({ path: '/services', query: { create: 'true', agent_id: String(agent.value?.id) } })
}

const handleEditService = (service: ProxyService) => {
  router.push({ path: '/services', query: { edit: String(service.id) } })
}

const handleEnableService = async (service: ProxyService) => {
  ElMessage.info('启用服务功能待实现')
}

const handleDisableService = async (service: ProxyService) => {
  ElMessage.info('禁用服务功能待实现')
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
const handleAddVisitor = () => {
  router.push({ path: '/services/agent-auth', query: { agent_id: String(agent.value?.id) } })
}

const handleEditVisitor = (visitor: PortForward) => {
  ElMessage.info('编辑端口访问功能待实现')
}

const handleEnableVisitor = async (visitor: PortForward) => {
  ElMessage.info('启用端口访问功能待实现')
}

const handleDisableVisitor = async (visitor: PortForward) => {
  ElMessage.info('禁用端口访问功能待实现')
}

const handleDeleteVisitor = async (visitor: PortForward) => {
  try {
    await ElMessageBox.confirm(t('common.deleteConfirm'), {
      type: 'warning'
    })
    ElMessage.info('删除端口访问功能待实现')
  } catch (error: any) {
    // 用户取消
  }
}

// Watch for route changes
watch(() => route.params.id, () => {
  if (route.params.id) {
    loadAgent()
  }
})

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
</style>
