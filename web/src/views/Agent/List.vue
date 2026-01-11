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
        <el-table-column prop="agent_name" :label="t('agent.name')" min-width="120">
          <template #default="{ row }">
            <el-link type="primary" :underline="false" @click="handleViewDetail(row)">
              {{ row.agent_name }}
            </el-link>
          </template>
        </el-table-column>
        <el-table-column :label="t('agent.group')" min-width="150">
          <template #default="{ row }">
            <span>{{ row.group_name || t('agent.noGroup') }}</span>
          </template>
        </el-table-column>
        <el-table-column label="Services" width="100" align="center">
          <template #default="{ row }">
            <el-link
              type="primary"
              :underline="false"
              @click="handleViewServices(row)"
            >
              {{ row.service_count || 0 }}
            </el-link>
          </template>
        </el-table-column>
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
        <el-table-column prop="tailscale_ip" label="IP" min-width="130">
          <template #default="{ row }">
            <span>{{ row.tailscale_ip || '-' }}</span>
          </template>
        </el-table-column>
        <el-table-column :label="t('agent.lastHeartbeat')" min-width="120">
          <template #default="{ row }">
            <TimeAgo :time="row.last_heartbeat" />
          </template>
        </el-table-column>
        <el-table-column :label="t('common.actions')" width="150" fixed="right">
          <template #default="{ row }">
            <div class="action-buttons">
              <el-tooltip :content="t('common.edit')" placement="top">
                <el-button size="small" :icon="Edit" @click="handleEdit(row)" />
              </el-tooltip>
              <el-tooltip :content="t('agent.viewToken')" placement="top">
                <el-button size="small" :icon="View" @click="handleViewToken(row)" />
              </el-tooltip>
              <el-tooltip :content="t('common.delete')" placement="top">
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
    
    <!-- 编辑对话框 -->
    <el-dialog v-model="editDialogVisible" :title="t('common.edit')" width="500px">
      <el-form :model="editForm" label-width="100px">
        <el-form-item :label="t('agent.name')">
          <el-input v-model="editForm.agent_name" :placeholder="t('agent.namePlaceholder')" />
        </el-form-item>
        <el-form-item :label="t('agent.group')">
          <el-input v-model="editForm.group_name" :placeholder="t('agent.noGroup')" />
        </el-form-item>
        <el-form-item :label="t('agent.description')">
          <el-input v-model="editForm.description" type="textarea" :rows="3" :placeholder="t('agent.descriptionPlaceholder')" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="editDialogVisible = false">{{ t('common.cancel') }}</el-button>
        <el-button type="primary" :loading="saving" @click="handleSaveEdit">{{ t('common.save') }}</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Plus, Delete, View, Edit } from '@element-plus/icons-vue'
import { getAgents, deleteAgent, updateAgent } from '@/api/agent'
import type { Agent } from '@/types/models'
import StatusTag from '@/components/Common/StatusTag.vue'
import TimeAgo from '@/components/Common/TimeAgo.vue'
import CreateDialog from './components/CreateDialog.vue'
import TokenDialog from './components/TokenDialog.vue'

const { t } = useI18n()
const router = useRouter()

const loading = ref(false)
const agents = ref<Agent[]>([])
const createDialogVisible = ref(false)
const tokenDialogVisible = ref(false)
const currentAgent = ref<Agent | null>(null)

// 编辑对话框
const editDialogVisible = ref(false)
const saving = ref(false)
const editForm = ref({
  id: 0,
  agent_name: '',
  group_name: '',
  description: ''
})

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

const handleViewDetail = (agent: Agent) => {
  router.push(`/agents/${agent.id}`)
}

const handleEdit = (agent: Agent) => {
  editForm.value = {
    id: agent.id,
    agent_name: agent.agent_name || '',
    group_name: agent.group_name || '',
    description: agent.description || ''
  }
  editDialogVisible.value = true
}

const handleSaveEdit = async () => {
  if (!editForm.value.agent_name.trim()) {
    ElMessage.warning(t('agent.nameRequired'))
    return
  }
  
  saving.value = true
  try {
    const res = await updateAgent(editForm.value.id, {
      agent_name: editForm.value.agent_name,
      group_name: editForm.value.group_name,
      description: editForm.value.description
    })
    if (res.success) {
      ElMessage.success(t('common.success'))
      editDialogVisible.value = false
      loadAgents()
    } else {
      ElMessage.error(res.message || t('common.failed'))
    }
  } catch (error) {
    ElMessage.error(t('common.failed'))
  } finally {
    saving.value = false
  }
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

const handleViewServices = (agent: Agent) => {
  router.push({ path: '/services', query: { agent_id: String(agent.id) } })
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
