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
        <el-table-column :label="t('agent.createdAt')" width="100">
          <template #default="{ row }">
            <TimeAgo :time="row.created_at" />
          </template>
        </el-table-column>
        <el-table-column :label="t('common.actions')" width="200" fixed="right">
          <template #default="{ row }">
            <el-button size="small" @click="handleViewToken(row)">
              {{ t('agent.viewToken') }}
            </el-button>
            <el-button
              size="small"
              type="danger"
              :icon="Delete"
              @click="handleDelete(row)"
            />
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
import { Plus, Delete } from '@element-plus/icons-vue'
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
</style>
