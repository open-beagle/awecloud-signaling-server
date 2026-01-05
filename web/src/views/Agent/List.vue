<template>
  <div class="agent-list">
    <el-card>
      <template #header>
        <div class="card-header">
          <span class="card-title">{{ t('agent.list') }}</span>
          <el-button type="primary" :icon="Plus" @click="handleCreate">
            {{ t('agent.create') }}
          </el-button>
        </div>
      </template>
      
      <el-table v-loading="loading" :data="agents" stripe>
        <el-table-column prop="id" label="ID" width="80" />
        <el-table-column prop="agent_name" :label="t('agent.name')" min-width="150" />
        <el-table-column prop="description" :label="t('agent.description')" min-width="200" />
        <el-table-column :label="t('common.status')" width="100">
          <template #default="{ row }">
            <StatusTag :status="row.status || 'offline'" />
          </template>
        </el-table-column>
        <el-table-column prop="version" :label="t('agent.version')" width="100">
          <template #default="{ row }">
            <span>{{ row.version || '-' }}</span>
          </template>
        </el-table-column>
        <el-table-column :label="t('agent.lastHeartbeat')" width="120">
          <template #default="{ row }">
            <TimeAgo :time="row.last_heartbeat" />
          </template>
        </el-table-column>
        <el-table-column :label="t('agent.createdAt')" width="100">
          <template #default="{ row }">
            <TimeAgo :time="row.created_at" />
          </template>
        </el-table-column>
        <el-table-column :label="t('common.actions')" width="120" fixed="right">
          <template #default="{ row }">
            <div class="action-buttons">
              <el-tooltip content="查看Token" placement="top">
                <el-button size="small" :icon="View" @click="handleViewToken(row)" />
              </el-tooltip>
              <el-tooltip content="删除" placement="top">
                <el-button
                  size="small"
                  type="danger"
                  :icon="Delete"
                  @click="handleDelete(row)"
                />
              </el-tooltip>
            </div>
          </template>
        </el-table-column>
      </el-table>
    </el-card>

    <CreateDialog v-model="createDialogVisible" @success="handleCreateSuccess" />
    <TokenDialog v-model="tokenDialogVisible" :agent="currentAgent" />
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Plus, Delete, View } from '@element-plus/icons-vue'
import { getAgents, deleteAgent } from '@/api/agent'
import type { Agent } from '@/types/models'
import StatusTag from '@/components/Common/StatusTag.vue'
import TimeAgo from '@/components/Common/TimeAgo.vue'
import CreateDialog from './components/CreateDialog.vue'
import TokenDialog from './components/TokenDialog.vue'

const { t } = useI18n()

const loading = ref(false)
const agents = ref<Agent[]>([])
const createDialogVisible = ref(false)
const tokenDialogVisible = ref(false)
const currentAgent = ref<Agent | null>(null)

const loadAgents = async () => {
  loading.value = true
  try {
    const res = await getAgents()
    if (res.success && res.data) {
      agents.value = res.data
    }
  } catch (error) {
    ElMessage.error(t('common.failed'))
  } finally {
    loading.value = false
  }
}

const handleCreate = () => {
  createDialogVisible.value = true
}

const handleCreateSuccess = (agent: Agent) => {
  loadAgents()
  // 显示新创建的Agent的Token
  currentAgent.value = agent
  tokenDialogVisible.value = true
}

const handleViewToken = (agent: Agent) => {
  currentAgent.value = agent
  tokenDialogVisible.value = true
}

const handleDelete = async (agent: Agent) => {
  try {
    await ElMessageBox.confirm(t('agent.deleteConfirm'), {
      type: 'warning'
    })
    
    const res = await deleteAgent(agent.id)
    if (res.success) {
      ElMessage.success(t('common.deleteSuccess'))
      loadAgents()
    }
  } catch (error: any) {
    if (error !== 'cancel') {
      ElMessage.error(t('common.failed'))
    }
  }
}

onMounted(() => {
  loadAgents()
})
</script>

<style scoped>
.agent-list {
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

.action-buttons {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: nowrap;
}
</style>
