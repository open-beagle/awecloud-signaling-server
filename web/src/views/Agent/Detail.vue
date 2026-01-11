<template>
  <div class="agent-detail">
    <div v-loading="loading">
      <!-- 基本信息卡片 -->
      <el-card class="info-card">
        <template #header>
          <div class="card-header">
            <span class="card-title">{{ t('agent.basicInfo') }}</span>
            <div class="header-actions">
              <StatusTag :status="agent?.status || 'offline'" />
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
        
        <!-- K8s 风格：单行多字段 -->
        <div class="info-row">
          <div class="info-item">
            <span class="info-label">{{ t('agent.name') }}</span>
            <span class="info-value">{{ agent?.agent_name || '-' }}</span>
          </div>
          <div class="info-item">
            <span class="info-label">{{ t('agent.group') }}</span>
            <el-input
              v-if="isEditing"
              v-model="editForm.groupName"
              :placeholder="t('agent.noGroup')"
              size="small"
              style="width: 150px"
            />
            <span v-else class="info-value">{{ agent?.group_name || '-' }}</span>
          </div>
          <div class="info-item">
            <span class="info-label">{{ t('agent.version') }}</span>
            <span class="info-value">{{ agent?.version || '-' }}</span>
          </div>
          <div class="info-item">
            <span class="info-label">{{ t('agent.createdAt') }}</span>
            <span class="info-value">
              <TimeAgo v-if="agent?.created_at" :time="agent.created_at" />
              <span v-else>-</span>
            </span>
          </div>
          <div class="info-item">
            <span class="info-label">{{ t('agent.lastHeartbeat') }}</span>
            <span class="info-value">
              <TimeAgo v-if="agent?.last_heartbeat" :time="agent.last_heartbeat" />
              <span v-else>-</span>
            </span>
          </div>
        </div>
        <div v-if="agent?.description" class="info-row">
          <div class="info-item">
            <span class="info-label">{{ t('agent.description') }}</span>
            <span class="info-value">{{ agent.description }}</span>
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
            <span class="info-value">{{ agent?.tailscale_ip || '-' }}</span>
          </div>
          <div class="info-item">
            <span class="info-label">{{ t('common.status') }}</span>
            <span class="info-value">
              <el-tag v-if="agent?.ts_connected" type="success" size="small">
                {{ t('agent.tsConnected') }}
              </el-tag>
              <el-tag v-else type="info" size="small">
                {{ t('agent.tsDisconnected') }}
              </el-tag>
            </span>
          </div>
          <div class="info-item">
            <span class="info-label">{{ agent?.ts_connected ? t('agent.tsConnectedAt') : t('agent.tsDisconnectedAt') }}</span>
            <span class="info-value">
              <template v-if="agent?.ts_connected">
                <TimeAgo v-if="agent?.ts_connected_at" :time="agent.ts_connected_at" />
                <span v-else>-</span>
              </template>
              <template v-else>
                <TimeAgo v-if="agent?.last_heartbeat" :time="agent.last_heartbeat" />
                <span v-else>-</span>
              </template>
            </span>
          </div>
        </div>
      </el-card>

      <!-- 网络信息卡片 -->
      <el-card class="info-card">
        <template #header>
          <span class="card-title">{{ t('agent.networkInfo') }}</span>
        </template>
        
        <div class="info-row">
          <div class="info-item">
            <span class="info-label">{{ t('agent.lanIp') }}</span>
            <span class="info-value">{{ agent?.network_info?.lan_ip || '-' }}</span>
          </div>
          <div class="info-item">
            <span class="info-label">{{ t('agent.lanGateway') }}</span>
            <span class="info-value">{{ agent?.network_info?.lan_gateway || '-' }}</span>
          </div>
          <div class="info-item">
            <span class="info-label">{{ t('agent.lanInterface') }}</span>
            <span class="info-value">{{ agent?.network_info?.lan_interface || '-' }}</span>
          </div>
          <div class="info-item">
            <span class="info-label">{{ t('agent.runtimeEnv') }}</span>
            <span class="info-value">
              <span v-if="agent?.network_info?.runtime_env === 'docker'">🐳 {{ t('agent.runtimeDocker') }}</span>
              <span v-else-if="agent?.network_info?.runtime_env === 'kubernetes'">☸️ {{ t('agent.runtimeKubernetes') }}</span>
              <span v-else-if="agent?.network_info?.runtime_env === 'native'">🖥️ {{ t('agent.runtimeNative') }}</span>
              <span v-else>-</span>
            </span>
          </div>
          <div class="info-item">
            <span class="info-label">{{ t('agent.hostname') }}</span>
            <span class="info-value">{{ agent?.network_info?.hostname || '-' }}</span>
          </div>
        </div>
        <div v-if="agent?.network_info?.lan_ip" class="info-tip">
          💡 {{ t('agent.networkTip') }}
        </div>
      </el-card>

      <!-- 端口映射服务列表 -->
      <el-card class="info-card">
        <template #header>
          <div class="card-header">
            <span class="card-title">
              {{ t('agent.serviceList') }} ({{ agent?.service_count || 0 }})
            </span>
            <el-button type="primary" size="small" :icon="Plus" @click="handleCreateService">
              {{ t('agent.createService') }}
            </el-button>
          </div>
        </template>
        
        <el-table v-if="agent?.services && agent.services.length > 0" :data="agent.services" stripe>
          <el-table-column prop="name" :label="t('service.name')" min-width="120" />
          <el-table-column :label="t('service.listenAddr')" min-width="150">
            <template #default="{ row }">
              {{ agent?.tailscale_ip || '-' }}:{{ row.listen_port }}
            </template>
          </el-table-column>
          <el-table-column prop="target_addr" :label="t('service.targetAddr')" min-width="150" />
          <el-table-column :label="t('service.status')" width="100">
            <template #default="{ row }">
              <el-tag v-if="row.status === 'running'" type="success" size="small">
                {{ t('service.running') }}
              </el-tag>
              <el-tag v-else type="info" size="small">
                {{ t('service.stopped') }}
              </el-tag>
            </template>
          </el-table-column>
          <el-table-column prop="connections" :label="t('service.connections')" width="100" align="center" />
          <el-table-column :label="t('common.actions')" width="150" fixed="right">
            <template #default="{ row }">
              <div class="action-buttons">
                <el-button
                  v-if="row.status !== 'running'"
                  size="small"
                  type="success"
                  @click="handleStartService(row)"
                >
                  {{ t('service.start') }}
                </el-button>
                <el-button
                  v-else
                  size="small"
                  type="warning"
                  @click="handleStopService(row)"
                >
                  {{ t('service.stop') }}
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
              {{ t('agent.visitorList') }} ({{ agent?.visitors?.length || 0 }})
            </span>
            <el-button type="primary" size="small" :icon="Plus" @click="handleAddVisitor">
              {{ t('agent.addVisitor') }}
            </el-button>
          </div>
        </template>
        
        <el-table v-if="agent?.visitors && agent.visitors.length > 0" :data="agent.visitors" stripe>
          <el-table-column prop="name" :label="t('service.name')" min-width="120" />
          <el-table-column :label="t('agent.localListenAddr')" min-width="180">
            <template #default="{ row }">
              {{ agent?.network_info?.lan_ip || '-' }}:{{ row.listen_port }}
            </template>
          </el-table-column>
          <el-table-column :label="t('agent.targetService')" min-width="180">
            <template #default="{ row }">
              {{ row.target_agent_name }}/{{ row.target_service_name }}
            </template>
          </el-table-column>
          <el-table-column :label="t('service.status')" width="100">
            <template #default="{ row }">
              <el-tag v-if="row.status === 'running'" type="success" size="small">
                {{ t('service.running') }}
              </el-tag>
              <el-tag v-else type="info" size="small">
                {{ t('service.stopped') }}
              </el-tag>
            </template>
          </el-table-column>
          <el-table-column prop="connections" :label="t('service.connections')" width="100" align="center" />
          <el-table-column :label="t('common.actions')" width="150" fixed="right">
            <template #default="{ row }">
              <div class="action-buttons">
                <el-button
                  v-if="row.status !== 'running'"
                  size="small"
                  type="success"
                  @click="handleStartVisitor(row)"
                >
                  {{ t('service.start') }}
                </el-button>
                <el-button
                  v-else
                  size="small"
                  type="warning"
                  @click="handleStopVisitor(row)"
                >
                  {{ t('service.stop') }}
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
        
        <template v-else>
          <el-empty :description="t('agent.noVisitors')" />
        </template>
        
        <div v-if="agent?.visitors && agent.visitors.length > 0" class="info-tip">
          💡 {{ t('agent.visitorTip') }}
        </div>
      </el-card>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted, watch, reactive } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Plus, Delete, Edit } from '@element-plus/icons-vue'
import { getAgent, updateAgent } from '@/api/agent'
import { startService, stopService, deleteService } from '@/api/service'
import { startVisitor, stopVisitor, deleteVisitor } from '@/api/visitor'
import type { AgentDetail, ProxyService, Visitor } from '@/types/models'
import StatusTag from '@/components/Common/StatusTag.vue'
import TimeAgo from '@/components/Common/TimeAgo.vue'

const { t } = useI18n()
const route = useRoute()
const router = useRouter()

const loading = ref(false)
const agent = ref<AgentDetail | null>(null)
const isEditing = ref(false)
const editForm = reactive({
  groupName: ''
})

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

const startEditing = () => {
  editForm.groupName = agent.value?.group_name || ''
  isEditing.value = true
}

const cancelEditing = () => {
  isEditing.value = false
}

const saveChanges = async () => {
  if (!agent.value) return

  try {
    const res = await updateAgent(agent.value.id, { group_name: editForm.groupName })
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

const handleStartService = async (service: ProxyService) => {
  try {
    const res = await startService(service.id)
    if (res.success) {
      ElMessage.success(t('common.success'))
      loadAgent()
    } else {
      ElMessage.error(res.message || t('common.failed'))
    }
  } catch (error) {
    ElMessage.error(t('common.failed'))
  }
}

const handleStopService = async (service: ProxyService) => {
  try {
    const res = await stopService(service.id)
    if (res.success) {
      ElMessage.success(t('common.success'))
      loadAgent()
    } else {
      ElMessage.error(res.message || t('common.failed'))
    }
  } catch (error) {
    ElMessage.error(t('common.failed'))
  }
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
  // 跳转到 Agent 授权页面，预选当前 Agent
  router.push({ path: '/services/agent-auth', query: { agent_id: String(agent.value?.id) } })
}

const handleStartVisitor = async (visitor: Visitor) => {
  try {
    const res = await startVisitor(visitor.id)
    if (res.success) {
      ElMessage.success(t('common.success'))
      loadAgent()
    } else {
      ElMessage.error(res.message || t('common.failed'))
    }
  } catch (error) {
    ElMessage.error(t('common.failed'))
  }
}

const handleStopVisitor = async (visitor: Visitor) => {
  try {
    const res = await stopVisitor(visitor.id)
    if (res.success) {
      ElMessage.success(t('common.success'))
      loadAgent()
    } else {
      ElMessage.error(res.message || t('common.failed'))
    }
  } catch (error) {
    ElMessage.error(t('common.failed'))
  }
}

const handleDeleteVisitor = async (visitor: Visitor) => {
  try {
    await ElMessageBox.confirm(t('common.deleteConfirm'), {
      type: 'warning'
    })
    
    const res = await deleteVisitor(visitor.id)
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
  gap: 12px;
}

.card-title {
  font-size: 16px;
  font-weight: 500;
  color: var(--text-primary);
}

/* K8s 风格的单行多字段布局 */
.info-row {
  display: flex;
  flex-wrap: wrap;
  gap: 32px;
  padding: 8px 0;
}

.info-row + .info-row {
  border-top: 1px solid var(--el-border-color-lighter);
  padding-top: 12px;
}

.info-item {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.info-label {
  font-size: 12px;
  color: var(--el-text-color-secondary);
}

.info-value {
  font-size: 14px;
  color: var(--el-text-color-primary);
}

.action-buttons {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: nowrap;
}

.info-tip {
  margin-top: 12px;
  padding: 8px 12px;
  background-color: var(--el-fill-color-light);
  border-radius: 4px;
  font-size: 13px;
  color: var(--el-text-color-secondary);
}
</style>
