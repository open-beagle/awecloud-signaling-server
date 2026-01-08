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
        <el-select v-model="filterAgentId" :placeholder="$t('service.filterAgent')" clearable @change="loadServices">
          <el-option :label="$t('service.allAgents')" :value="0" />
          <el-option v-for="agent in agents" :key="agent.id" :label="agent.agent_name" :value="agent.id" />
        </el-select>
        <el-select v-model="filterStatus" :placeholder="$t('service.filterStatus')" clearable @change="loadServices">
          <el-option :label="$t('service.allStatus')" value="" />
          <el-option :label="$t('service.running')" value="running" />
          <el-option :label="$t('service.stopped')" value="stopped" />
        </el-select>
      </div>

      <!-- 服务列表 -->
      <el-table :data="filteredServices" v-loading="loading" stripe>
        <el-table-column prop="id" label="ID" width="80" />
        <el-table-column prop="name" :label="$t('service.name')" min-width="120" />
        <el-table-column :label="$t('service.agent')" min-width="120">
          <template #default="{ row }">
            <span>{{ row.agent_name }}</span>
            <el-tag v-if="row.agent_ts_connected" type="success" size="small" style="margin-left: 8px">TS</el-tag>
          </template>
        </el-table-column>
        <el-table-column :label="$t('service.listenAddr')" min-width="180">
          <template #default="{ row }">
            <span v-if="row.agent_ts_ip">{{ row.agent_ts_ip }}:{{ row.listen_port }}</span>
            <span v-else>:{{ row.listen_port }}</span>
          </template>
        </el-table-column>
        <el-table-column prop="target_addr" :label="$t('service.targetAddr')" min-width="180" />
        <el-table-column :label="$t('service.status')" width="100">
          <template #default="{ row }">
            <el-tag :type="row.status === 'running' ? 'success' : 'info'">
              {{ row.status === 'running' ? $t('service.running') : $t('service.stopped') }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="connections" :label="$t('service.connections')" width="100" />
        <el-table-column :label="$t('common.actions')" width="200" fixed="right">
          <template #default="{ row }">
            <el-button v-if="row.status !== 'running'" type="success" size="small" @click="handleStart(row)">
              {{ $t('service.start') }}
            </el-button>
            <el-button v-else type="warning" size="small" @click="handleStop(row)">
              {{ $t('service.stop') }}
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
          <el-select v-model="createForm.agent_id" :placeholder="$t('service.selectAgent')">
            <el-option v-for="agent in onlineAgents" :key="agent.id" 
              :label="`${agent.agent_name} (${agent.tailscale_ip || 'No IP'})`" 
              :value="agent.id" />
          </el-select>
        </el-form-item>
        <el-form-item :label="$t('service.name')" prop="name">
          <el-input v-model="createForm.name" :placeholder="$t('service.namePlaceholder')" />
        </el-form-item>
        <el-form-item :label="$t('service.listenPort')" prop="listen_port">
          <el-input-number v-model="createForm.listen_port" :min="1" :max="65535" />
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
        <el-button type="primary" @click="handleCreate">{{ $t('common.confirm') }}</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Plus } from '@element-plus/icons-vue'
import { getServices, createService, deleteService, startService, stopService, type ProxyService, type CreateServiceRequest } from '@/api/service'
import { getAgents } from '@/api/agent'
import type { Agent, ApiResponse } from '@/types/models'

const loading = ref(false)
const services = ref<ProxyService[]>([])
const agents = ref<Agent[]>([])
const filterAgentId = ref(0)
const filterStatus = ref('')

const createDialogVisible = ref(false)
const createFormRef = ref()
const createForm = ref<CreateServiceRequest>({
  name: '',
  agent_id: 0,
  listen_port: 0,
  target_addr: '',
  remark: ''
})

const createRules = {
  name: [{ required: true, message: '请输入服务名称', trigger: 'blur' }],
  agent_id: [{ required: true, message: '请选择 Agent', trigger: 'change' }],
  listen_port: [{ required: true, message: '请输入监听端口', trigger: 'blur' }],
  target_addr: [{ required: true, message: '请输入目标地址', trigger: 'blur' }]
}

const filteredServices = computed(() => {
  let result = services.value
  if (filterAgentId.value > 0) {
    result = result.filter(s => s.agent_id === filterAgentId.value)
  }
  if (filterStatus.value) {
    result = result.filter(s => s.status === filterStatus.value)
  }
  return result
})

const onlineAgents = computed(() => {
  return agents.value.filter(a => a.status === 'online')
})

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

const showCreateDialog = () => {
  createForm.value = { name: '', agent_id: 0, listen_port: 0, target_addr: '', remark: '' }
  createDialogVisible.value = true
}

const handleCreate = async () => {
  const valid = await createFormRef.value?.validate()
  if (!valid) return

  try {
    const res = await createService(createForm.value) as ApiResponse
    if (res.success) {
      ElMessage.success('创建成功')
      createDialogVisible.value = false
      loadServices()
    } else {
      ElMessage.error(res.message || '创建失败')
    }
  } catch (error) {
    ElMessage.error('创建失败')
  }
}

const handleStart = async (row: ProxyService) => {
  try {
    const res = await startService(row.id) as ApiResponse
    if (res.success) {
      ElMessage.success('启动成功')
      loadServices()
    } else {
      ElMessage.error(res.message || '启动失败')
    }
  } catch (error) {
    ElMessage.error('启动失败')
  }
}

const handleStop = async (row: ProxyService) => {
  try {
    const res = await stopService(row.id) as ApiResponse
    if (res.success) {
      ElMessage.success('停止成功')
      loadServices()
    } else {
      ElMessage.error(res.message || '停止失败')
    }
  } catch (error) {
    ElMessage.error('停止失败')
  }
}

const handleDelete = async (row: ProxyService) => {
  try {
    await ElMessageBox.confirm('确定要删除该服务吗？', '提示', { type: 'warning' })
    const res = await deleteService(row.id) as ApiResponse
    if (res.success) {
      ElMessage.success('删除成功')
      loadServices()
    } else {
      ElMessage.error(res.message || '删除失败')
    }
  } catch (error) {
    if (error !== 'cancel') {
      ElMessage.error('删除失败')
    }
  }
}

onMounted(() => {
  loadServices()
  loadAgents()
})
</script>

<style scoped>
.service-container {
  padding: 20px;
}
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
</style>
