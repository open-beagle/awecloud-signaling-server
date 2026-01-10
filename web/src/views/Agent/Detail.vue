<template>
  <div class="agent-detail">
    <!-- 面包屑导航 -->
    <el-breadcrumb separator="/" class="breadcrumb">
      <el-breadcrumb-item :to="{ path: '/agents' }">{{ t('agent.list') }}</el-breadcrumb-item>
      <el-breadcrumb-item>{{ agent?.agent_name || t('agent.detail') }}</el-breadcrumb-item>
    </el-breadcrumb>

    <div v-loading="loading">
      <!-- 基本信息卡片 -->
      <el-card class="info-card">
        <template #header>
          <div class="card-header">
            <span class="card-title">{{ t('agent.basicInfo') }}</span>
            <StatusTag :status="agent?.status || 'offline'" />
          </div>
        </template>
        
        <el-descriptions :column="2" border>
          <el-descriptions-item :label="t('agent.name')">
            {{ agent?.agent_name || '-' }}
          </el-descriptions-item>
          <el-descriptions-item :label="t('agent.version')">
            {{ agent?.version || '-' }}
          </el-descriptions-item>
          <el-descriptions-item :label="t('agent.group')">
            <el-input
              v-model="groupName"
              :placeholder="t('agent.noGroup')"
              size="small"
              style="width: 200px"
              @blur="handleUpdateGroup"
              @keyup.enter="handleUpdateGroup"
            />
          </el-descriptions-item>
          <el-descriptions-item :label="t('agent.lastHeartbeat')">
            <TimeAgo :time="agent?.last_heartbeat" />
          </el-descriptions-item>
          <el-descriptions-item :label="t('agent.description')" :span="2">
            {{ agent?.description || '-' }}
          </el-descriptions-item>
        </el-descriptions>
      </el-card>

      <!-- Tailscale 信息卡片 -->
      <el-card class="info-card">
        <template #header>
          <span class="card-title">{{ t('agent.tailscaleInfo') }}</span>
        </template>
        
        <el-descriptions :column="2" border>
          <el-descriptions-item :label="t('agent.tailscaleIp')">
            <span v-if="agent?.tailscale_ip">{{ agent.tailscale_ip }}</span>
            <span v-else class="text-muted">-</span>
          </el-descriptions-item>
          <el-descriptions-item :label="t('common.status')">
            <el-tag v-if="agent?.ts_connected" type="success" size="small">
              {{ t('agent.tsConnected') }}
            </el-tag>
            <el-tag v-else type="info" size="small">
              {{ t('agent.tsDisconnected') }}
            </el-tag>
          </el-descriptions-item>
          <el-descriptions-item :label="t('agent.tsConnType')">
            <span v-if="agent?.ts_conn_type === 'p2p'">{{ t('agent.tsConnTypeP2p') }}</span>
            <span v-else-if="agent?.ts_conn_type === 'derp'">{{ t('agent.tsConnTypeDerp') }}</span>
            <span v-else class="text-muted">-</span>
          </el-descriptions-item>
          <el-descriptions-item :label="t('agent.tsRegisteredAt')">
            <TimeAgo v-if="agent?.ts_registered_at" :time="agent.ts_registered_at" />
            <span v-else class="text-muted">-</span>
          </el-descriptions-item>
        </el-descriptions>
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
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Plus, Delete } from '@element-plus/icons-vue'
import { getAgent, updateAgent } from '@/api/agent'
import { startService, stopService, deleteService } from '@/api/service'
import type { AgentDetail, ProxyService } from '@/types/models'
import StatusTag from '@/components/Common/StatusTag.vue'
import TimeAgo from '@/components/Common/TimeAgo.vue'

const { t } = useI18n()
const route = useRoute()
const router = useRouter()

const loading = ref(false)
const agent = ref<AgentDetail | null>(null)
const groupName = ref('')
const originalGroupName = ref('')

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
      groupName.value = res.data.group_name || ''
      originalGroupName.value = res.data.group_name || ''
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

const handleUpdateGroup = async () => {
  if (!agent.value || groupName.value === originalGroupName.value) {
    return
  }

  try {
    const res = await updateAgent(agent.value.id, { group_name: groupName.value })
    if (res.success) {
      ElMessage.success(t('agent.groupUpdateSuccess'))
      originalGroupName.value = groupName.value
    } else {
      ElMessage.error(res.message || t('agent.groupUpdateFailed'))
      groupName.value = originalGroupName.value
    }
  } catch (error) {
    ElMessage.error(t('agent.groupUpdateFailed'))
    groupName.value = originalGroupName.value
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

.breadcrumb {
  margin-bottom: 16px;
}

.info-card {
  margin-bottom: 16px;
}

.card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.card-title {
  font-size: 16px;
  font-weight: 500;
  color: var(--text-primary);
}

.text-muted {
  color: #909399;
}

.action-buttons {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: nowrap;
}
</style>
