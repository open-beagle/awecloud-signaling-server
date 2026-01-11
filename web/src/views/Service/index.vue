<template>
  <div class="service-container">
    <el-card>
      <template #header>
        <div class="card-header">
          <span>{{ $t('service.title') }}</span>
          <el-button type="primary" @click="showCreateDialog">
            <el-icon><Plus /></el-icon>
            {{ $t('service.create') }}
          </el-button>
        </div>
      </template>

      <!-- 筛选 -->
      <div class="filter-bar">
        <el-select v-model="filterAgentId" :placeholder="$t('service.filterAgent')" clearable @change="handleFilterChange">
          <el-option :label="$t('service.allAgents')" :value="0" />
          <el-option v-for="agent in agents" :key="agent.id" :label="agent.agent_name" :value="agent.id" />
        </el-select>
        <el-select v-model="filterStatus" :placeholder="$t('service.filterStatus')" clearable @change="handleFilterChange">
          <el-option :label="$t('service.allStatus')" value="" />
          <el-option :label="$t('service.running')" value="running" />
          <el-option :label="$t('service.stopped')" value="stopped" />
        </el-select>
        <el-input 
          v-model="searchKeyword" 
          :placeholder="$t('service.searchPlaceholder')" 
          clearable 
          style="width: 200px"
          @input="handleFilterChange"
        >
          <template #prefix>
            <el-icon><Search /></el-icon>
          </template>
        </el-input>
      </div>

      <!-- 服务列表 -->
      <el-table :data="filteredServices" v-loading="loading" stripe>
        <el-table-column prop="id" label="ID" width="80" />
        <el-table-column prop="name" :label="$t('service.name')" min-width="120" />
        <el-table-column :label="$t('service.agent')" min-width="150">
          <template #default="{ row }">
            <div class="agent-cell">
              <span>{{ row.agent_name }}</span>
              <el-tag v-if="row.agent_ts_connected" type="success" size="small" style="margin-left: 8px">TS</el-tag>
              <el-tag v-if="row.agent_status === 'offline'" type="danger" size="small" style="margin-left: 4px">{{ $t('common.offline') }}</el-tag>
            </div>
          </template>
        </el-table-column>
        <el-table-column :label="$t('service.listenAddr')" min-width="180">
          <template #default="{ row }">
            <span v-if="row.agent_ts_ip" class="listen-addr">{{ row.agent_ts_ip }}:{{ row.listen_port }}</span>
            <span v-else class="listen-addr">:{{ row.listen_port }}</span>
          </template>
        </el-table-column>
        <el-table-column prop="target_addr" :label="$t('service.targetAddr')" min-width="180" />
        <el-table-column :label="$t('service.status')" width="100">
          <template #default="{ row }">
            <el-tag :type="getStatusType(row.status)">
              {{ getStatusText(row.status) }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="connections" :label="$t('service.connections')" width="100" />
        <el-table-column :label="$t('common.actions')" width="280" fixed="right">
          <template #default="{ row }">
            <el-button v-if="row.status !== 'running'" type="success" size="small" @click="handleStart(row)">
              {{ $t('service.start') }}
            </el-button>
            <el-button v-else type="warning" size="small" @click="handleStop(row)">
              {{ $t('service.stop') }}
            </el-button>
            <el-button type="primary" size="small" @click="handleStats(row)">
              {{ $t('service.stats') }}
            </el-button>
            <el-button type="danger" size="small" @click="handleDelete(row)">
              {{ $t('common.delete') }}
            </el-button>
          </template>
        </el-table-column>
      </el-table>
    </el-card>

    <!-- 创建对话框 -->
    <el-dialog v-model="createDialogVisible" :title="$t('service.create')" width="500px">
      <el-form :model="createForm" :rules="createRules" ref="createFormRef" label-width="120px">
        <el-form-item :label="$t('service.agent')" prop="agent_id">
          <el-select v-model="createForm.agent_id" :placeholder="$t('service.selectAgent')" @change="handleAgentChange">
            <el-option v-for="agent in onlineAgents" :key="agent.id" 
              :label="`${agent.agent_name} (${agent.tailscale_ip || 'No IP'})`" 
              :value="agent.id" />
          </el-select>
        </el-form-item>
        <el-form-item :label="$t('service.name')" prop="name">
          <el-input v-model="createForm.name" :placeholder="$t('service.namePlaceholder')" />
        </el-form-item>
        <el-form-item :label="$t('service.listenPort')" prop="listen_port">
          <el-input-number v-model="createForm.listen_port" :min="1" :max="65535" @change="validatePort" />
          <div v-if="portConflictError" class="port-conflict-error">
            <el-icon><Warning /></el-icon>
            {{ portConflictError }}
          </div>
        </el-form-item>
        <el-form-item :label="$t('service.targetAddr')" prop="target_addr">
          <el-input v-model="createForm.target_addr" :placeholder="$t('service.targetAddrPlaceholder')" />
        </el-form-item>
        <el-form-item :label="$t('service.remark')">
          <el-input v-model="createForm.remark" type="textarea" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="createDialogVisible = false">{{ $t('common.cancel') }}</el-button>
        <el-button type="primary" @click="handleCreate" :disabled="!!portConflictError">{{ $t('common.confirm') }}</el-button>
      </template>
    </el-dialog>

    <!-- 统计对话框 -->
    <el-dialog v-model="statsDialogVisible" :title="$t('service.statsTitle')" width="450px">
      <div v-if="currentStats" class="stats-container">
        <el-descriptions :column="1" border>
          <el-descriptions-item :label="$t('service.name')">{{ currentStats.name }}</el-descriptions-item>
          <el-descriptions-item :label="$t('service.status')">
            <el-tag :type="getStatusType(currentStats.status)">{{ getStatusText(currentStats.status) }}</el-tag>
          </el-descriptions-item>
          <el-descriptions-item :label="$t('service.connections')">{{ currentStats.connections }}</el-descriptions-item>
          <el-descriptions-item :label="$t('service.bytesIn')">{{ formatBytes(currentStats.bytes_in) }}</el-descriptions-item>
          <el-descriptions-item :label="$t('service.bytesOut')">{{ formatBytes(currentStats.bytes_out) }}</el-descriptions-item>
        </el-descriptions>
      </div>
      <div v-else class="stats-loading">
        <el-icon class="is-loading"><Loading /></el-icon>
        {{ $t('common.loading') }}
      </div>
      <template #footer>
        <el-button @click="statsDialogVisible = false">{{ $t('common.confirm') }}</el-button>
      </template>
    </el-dialog>
  </div>
</template>


<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Plus, Search, Warning, Loading } from '@element-plus/icons-vue'
import { getServices, createService, deleteService, startService, stopService, getServiceStats, type ProxyService, type CreateServiceRequest } from '@/api/service'
import { getAgents } from '@/api/agent'
import type { Agent, ApiResponse } from '@/types/models'

const { t } = useI18n()

const loading = ref(false)
const services = ref<ProxyService[]>([])
const agents = ref<Agent[]>([])
const filterAgentId = ref(0)
const filterStatus = ref('')
const searchKeyword = ref('')

// 创建对话框
const createDialogVisible = ref(false)
const createFormRef = ref()
const createForm = ref<CreateServiceRequest>({
  name: '',
  agent_id: 0,
  listen_port: 0,
  target_addr: '',
  remark: ''
})
const portConflictError = ref('')

// 统计对话框
const statsDialogVisible = ref(false)
const currentStats = ref<{
  id: number
  name: string
  status: string
  connections: number
  bytes_in: number
  bytes_out: number
} | null>(null)

const createRules = {
  name: [{ required: true, message: () => t('service.nameRequired'), trigger: 'blur' }],
  agent_id: [{ required: true, message: () => t('service.agentRequired'), trigger: 'change' }],
  listen_port: [{ required: true, message: () => t('service.portRequired'), trigger: 'blur' }],
  target_addr: [{ required: true, message: () => t('service.targetAddrRequired'), trigger: 'blur' }]
}

const filteredServices = computed(() => {
  let result = services.value
  if (filterAgentId.value > 0) {
    result = result.filter(s => s.agent_id === filterAgentId.value)
  }
  if (filterStatus.value) {
    result = result.filter(s => s.status === filterStatus.value)
  }
  if (searchKeyword.value) {
    const keyword = searchKeyword.value.toLowerCase()
    result = result.filter(s => 
      s.name.toLowerCase().includes(keyword) || 
      s.target_addr.toLowerCase().includes(keyword) ||
      (s.agent_name && s.agent_name.toLowerCase().includes(keyword))
    )
  }
  return result
})

const onlineAgents = computed(() => {
  return agents.value.filter(a => a.status === 'online')
})

// 获取状态类型
const getStatusType = (status: string) => {
  switch (status) {
    case 'running': return 'success'
    case 'stopped': return 'info'
    case 'error': return 'danger'
    default: return 'info'
  }
}

// 获取状态文本
const getStatusText = (status: string) => {
  switch (status) {
    case 'running': return t('service.running')
    case 'stopped': return t('service.stopped')
    case 'error': return t('service.error')
    default: return status
  }
}

// 格式化字节数
const formatBytes = (bytes: number) => {
  if (bytes === 0) return '0 B'
  const k = 1024
  const sizes = ['B', 'KB', 'MB', 'GB', 'TB']
  const i = Math.floor(Math.log(bytes) / Math.log(k))
  return parseFloat((bytes / Math.pow(k, i)).toFixed(2)) + ' ' + sizes[i]
}

const loadServices = async () => {
  loading.value = true
  try {
    const res = await getServices() as ApiResponse<ProxyService[]>
    if (res.success) {
      services.value = res.data || []
    }
  } catch (error) {
    console.error('Load services error:', error)
  } finally {
    loading.value = false
  }
}

const loadAgents = async () => {
  try {
    const res = await getAgents()
    if (res.success) {
      agents.value = res.data || []
    }
  } catch (error) {
    console.error('Load agents error:', error)
  }
}

const handleFilterChange = () => {
  // 筛选在 computed 中处理，这里可以添加额外逻辑
}

const showCreateDialog = () => {
  createForm.value = { name: '', agent_id: 0, listen_port: 0, target_addr: '', remark: '' }
  portConflictError.value = ''
  createDialogVisible.value = true
}

// 验证端口是否冲突
const validatePort = () => {
  if (!createForm.value.agent_id || !createForm.value.listen_port) {
    portConflictError.value = ''
    return
  }
  
  const conflict = services.value.find(s => 
    s.agent_id === createForm.value.agent_id && 
    s.listen_port === createForm.value.listen_port
  )
  
  if (conflict) {
    portConflictError.value = t('service.portConflict', { port: createForm.value.listen_port, name: conflict.name })
  } else {
    portConflictError.value = ''
  }
}

// Agent 选择变化时重新验证端口
const handleAgentChange = () => {
  validatePort()
}

const handleCreate = async () => {
  const valid = await createFormRef.value?.validate()
  if (!valid) return

  // 再次检查端口冲突
  validatePort()
  if (portConflictError.value) {
    ElMessage.error(portConflictError.value)
    return
  }

  try {
    const res = await createService(createForm.value) as ApiResponse
    if (res.success) {
      ElMessage.success(t('common.createSuccess'))
      createDialogVisible.value = false
      loadServices()
    } else {
      ElMessage.error(res.message || t('common.failed'))
    }
  } catch (error) {
    ElMessage.error(t('common.failed'))
  }
}

const handleStart = async (row: ProxyService) => {
  // 检查 Agent 是否在线
  if (row.agent_status === 'offline') {
    ElMessage.error(t('service.agentOfflineError'))
    return
  }

  try {
    const res = await startService(row.id) as ApiResponse
    if (res.success) {
      ElMessage.success(t('service.startSuccess'))
      loadServices()
    } else {
      // 显示具体错误信息
      ElMessage.error(res.message || t('service.startFailed'))
    }
  } catch (error) {
    ElMessage.error(t('service.startFailed'))
  }
}

const handleStop = async (row: ProxyService) => {
  try {
    const res = await stopService(row.id) as ApiResponse
    if (res.success) {
      ElMessage.success(t('service.stopSuccess'))
      loadServices()
    } else {
      ElMessage.error(res.message || t('service.stopFailed'))
    }
  } catch (error) {
    ElMessage.error(t('service.stopFailed'))
  }
}

const handleDelete = async (row: ProxyService) => {
  try {
    // 如果服务正在运行，提示会先停止
    let confirmMessage = t('service.deleteConfirm')
    if (row.status === 'running') {
      confirmMessage = t('service.deleteRunningConfirm')
    }
    
    await ElMessageBox.confirm(confirmMessage, t('common.confirm'), { type: 'warning' })
    const res = await deleteService(row.id) as ApiResponse
    if (res.success) {
      ElMessage.success(t('common.deleteSuccess'))
      loadServices()
    } else {
      ElMessage.error(res.message || t('common.failed'))
    }
  } catch (error) {
    if (error !== 'cancel') {
      ElMessage.error(t('common.failed'))
    }
  }
}

const handleStats = async (row: ProxyService) => {
  statsDialogVisible.value = true
  currentStats.value = null
  
  try {
    const res = await getServiceStats(row.id) as ApiResponse<{
      id: number
      name: string
      status: string
      connections: number
      bytes_in: number
      bytes_out: number
    }>
    if (res.success) {
      currentStats.value = res.data
    } else {
      ElMessage.error(res.message || t('service.statsFailed'))
      statsDialogVisible.value = false
    }
  } catch (error) {
    ElMessage.error(t('service.statsFailed'))
    statsDialogVisible.value = false
  }
}

onMounted(() => {
  loadServices()
  loadAgents()
})
</script>

<style scoped>
.card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}
.filter-bar {
  margin-bottom: 16px;
  display: flex;
  gap: 12px;
}
.agent-cell {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
}
.listen-addr {
  font-family: monospace;
}
.port-conflict-error {
  color: var(--el-color-danger);
  font-size: 12px;
  margin-top: 4px;
  display: flex;
  align-items: center;
  gap: 4px;
}
.stats-container {
  padding: 10px 0;
}
.stats-loading {
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 40px;
  gap: 8px;
  color: var(--el-text-color-secondary);
}
</style>
