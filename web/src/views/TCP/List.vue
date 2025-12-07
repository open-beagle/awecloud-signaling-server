<template>
  <div class="tcp-list">
    <el-card>
      <template #header>
        <div class="card-header">
          <span class="card-title">{{ t('tcp.list') }}</span>
          <el-button type="primary" :icon="Plus" @click="handleCreate">
            {{ t('tcp.create') }}
          </el-button>
        </div>
      </template>

      <!-- 筛选条件 -->
      <div class="filter-bar">
        <el-select
          v-model="filterAgentId"
          :placeholder="t('tcp.filterByAgent')"
          clearable
          style="width: 200px; margin-right: 10px"
          @change="loadServices"
        >
          <el-option :label="t('tcp.all')" :value="undefined" />
          <el-option
            v-for="agent in agents"
            :key="agent.id"
            :label="agent.agent_name"
            :value="agent.id"
          />
        </el-select>
        <el-select
          v-model="filterEnabled"
          :placeholder="t('tcp.filterByStatus')"
          clearable
          style="width: 200px; margin-right: 10px"
          @change="loadServices"
        >
          <el-option :label="t('tcp.all')" :value="undefined" />
          <el-option :label="t('tcp.enabledOnly')" :value="true" />
          <el-option :label="t('tcp.disabledOnly')" :value="false" />
        </el-select>
        <el-button :icon="Refresh" @click="loadServices">{{ t('common.refresh') }}</el-button>
      </div>
      
      <el-table v-loading="loading" :data="services" stripe>
        <el-table-column prop="id" label="ID" width="80" />
        <el-table-column prop="service_name" :label="t('tcp.serviceName')" min-width="150" />
        <el-table-column :label="t('tcp.agent')" width="150">
          <template #default="{ row }">
            {{ row.agent_name || '-' }}
          </template>
        </el-table-column>
        <el-table-column :label="t('tcp.localAddress')" min-width="180">
          <template #default="{ row }">
            {{ row.local_ip }}:{{ row.local_port }}
          </template>
        </el-table-column>
        <el-table-column :label="t('tcp.remotePort')" width="120">
          <template #default="{ row }">
            <el-tag type="success">{{ row.remote_port }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column :label="t('tcp.enabled')" width="100">
          <template #default="{ row }">
            <el-tag v-if="row.enabled" type="success" size="small">启用</el-tag>
            <el-tag v-else type="info" size="small">禁用</el-tag>
          </template>
        </el-table-column>
        <el-table-column :label="t('tcp.accessControl')" width="100">
          <template #default="{ row }">
            <el-tag v-if="row.access_control === 'public'" type="success" size="small">
              {{ t('tcp.public') }}
            </el-tag>
            <el-tag v-else type="warning" size="small">
              {{ t('tcp.whitelist') }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="description" :label="t('tcp.description')" min-width="200" />
        <el-table-column :label="t('agent.createdAt')" width="100">
          <template #default="{ row }">
            <TimeAgo :time="row.created_at" />
          </template>
        </el-table-column>
        <el-table-column :label="t('common.actions')" width="180" fixed="right">
          <template #default="{ row }">
            <el-tooltip v-if="!row.enabled" content="启用" placement="top">
              <el-button
                size="small"
                type="success"
                :icon="VideoPlay"
                @click="handleEnable(row)"
              />
            </el-tooltip>
            <el-tooltip v-else content="禁用" placement="top">
              <el-button
                size="small"
                type="warning"
                :icon="VideoPause"
                @click="handleDisable(row)"
              />
            </el-tooltip>
            <el-tooltip content="编辑" placement="top">
              <el-button
                size="small"
                :icon="Edit"
                @click="handleEdit(row)"
              />
            </el-tooltip>
            <el-tooltip content="删除" placement="top">
              <el-button
                size="small"
                type="danger"
                :icon="Delete"
                @click="handleDelete(row)"
              />
            </el-tooltip>
          </template>
        </el-table-column>
      </el-table>
    </el-card>

    <CreateDialog v-model="createDialogVisible" @success="loadServices" />
    <EditDialog
      v-model="editDialogVisible"
      :service="selectedService"
      @success="loadServices"
    />
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Plus, Delete, Edit, Refresh, VideoPlay, VideoPause } from '@element-plus/icons-vue'
import { getTCPServices, deleteTCPService, enableTCPService, disableTCPService } from '@/api/tcp'
import { getAgents } from '@/api/agent'
import type { TCPService, Agent } from '@/types/models'
import TimeAgo from '@/components/Common/TimeAgo.vue'
import CreateDialog from './components/CreateDialog.vue'
import EditDialog from './components/EditDialog.vue'

const { t } = useI18n()

const loading = ref(false)
const services = ref<TCPService[]>([])
const agents = ref<Agent[]>([])
const createDialogVisible = ref(false)
const editDialogVisible = ref(false)
const selectedService = ref<TCPService | null>(null)
const filterAgentId = ref<number | undefined>(undefined)
const filterEnabled = ref<boolean | undefined>(undefined)

const loadServices = async () => {
  loading.value = true
  try {
    const params: any = {}
    if (filterAgentId.value !== undefined) {
      params.agent_id = filterAgentId.value
    }
    if (filterEnabled.value !== undefined) {
      params.enabled = filterEnabled.value
    }
    
    const res = await getTCPServices(params)
    if (res.success && res.data) {
      services.value = res.data
    }
  } catch (error) {
    ElMessage.error(t('common.failed'))
  } finally {
    loading.value = false
  }
}

const loadAgents = async () => {
  try {
    const res = await getAgents()
    if (res.success && res.data) {
      agents.value = res.data
    }
  } catch (error) {
    console.error('Failed to load agents:', error)
  }
}

const handleCreate = () => {
  createDialogVisible.value = true
}

const handleEdit = (service: TCPService) => {
  selectedService.value = service
  editDialogVisible.value = true
}

const handleEnable = async (service: TCPService) => {
  try {
    await ElMessageBox.confirm(t('tcp.enableConfirm'), {
      type: 'warning'
    })
    
    const res = await enableTCPService(service.id)
    if (res.success) {
      ElMessage.success(t('tcp.enableSuccess'))
      loadServices()
    }
  } catch (error: any) {
    if (error !== 'cancel') {
      ElMessage.error(error.response?.data?.error || t('common.failed'))
    }
  }
}

const handleDisable = async (service: TCPService) => {
  try {
    await ElMessageBox.confirm(t('tcp.disableConfirm'), {
      type: 'warning'
    })
    
    const res = await disableTCPService(service.id)
    if (res.success) {
      ElMessage.success(t('tcp.disableSuccess'))
      loadServices()
    }
  } catch (error: any) {
    if (error !== 'cancel') {
      ElMessage.error(t('common.failed'))
    }
  }
}

const handleDelete = async (service: TCPService) => {
  try {
    await ElMessageBox.confirm(t('tcp.deleteConfirm'), {
      type: 'warning'
    })
    
    const res = await deleteTCPService(service.id)
    if (res.success) {
      ElMessage.success(t('common.deleteSuccess'))
      loadServices()
    }
  } catch (error: any) {
    if (error !== 'cancel') {
      ElMessage.error(t('common.failed'))
    }
  }
}

onMounted(() => {
  loadServices()
  loadAgents()
})
</script>

<style scoped>
.tcp-list {
  width: 100%;
}

.card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.card-title {
  font-size: 18px;
  font-weight: 500;
  color: var(--text-primary);
}

.filter-bar {
  margin-bottom: 16px;
  display: flex;
  align-items: center;
}
</style>
