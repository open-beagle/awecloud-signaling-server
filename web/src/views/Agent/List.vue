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
        <el-table-column prop="tailscale_ip" label="Tailscale IP" min-width="130">
          <template #default="{ row }">
            <span>{{ row.tailscale_ip || '-' }}</span>
          </template>
        </el-table-column>
        <el-table-column :label="t('agent.group')" min-width="150">
          <template #default="{ row }">
            <el-input
              v-model="row.group_name"
              :placeholder="t('agent.noGroup')"
              size="small"
              @blur="handleUpdateGroup(row)"
              @keyup.enter="handleUpdateGroup(row)"
            />
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
        <el-table-column :label="t('agent.lastHeartbeat')" min-width="120">
          <template #default="{ row }">
            <TimeAgo :time="row.last_heartbeat" />
          </template>
        </el-table-column>
        <el-table-column :label="t('common.actions')" width="150" fixed="right">
          <template #default="{ row }">
            <div class="action-buttons">
              <el-tooltip :content="t('agent.viewDetail')" placement="top">
                <el-button size="small" :icon="InfoFilled" @click="handleViewDetail(row)" />
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
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Plus, Delete, View, InfoFilled } from '@element-plus/icons-vue'
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

// 保存原始分组名称，用于检测变化
const originalGroupNames = ref<Map<number, string>>(new Map())

const loadAgents = async () => {
  loading.value = true
  try {
    const res = await getAgents()
    if (res.success && res.data) {
      agents.value = res.data
      // 保存原始分组名称
      originalGroupNames.value.clear()
      res.data.forEach(agent => {
        originalGroupNames.value.set(agent.id, agent.group_name || '')
      })
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

const handleUpdateGroup = async (agent: Agent) => {
  const originalName = originalGroupNames.value.get(agent.id) || ''
  const newName = agent.group_name || ''
  
  // 如果没有变化，不调用 API
  if (originalName === newName) {
    return
  }
  
  try {
    const res = await updateAgent(agent.id, { group_name: newName })
    if (res.success) {
      ElMessage.success(t('agent.groupUpdateSuccess'))
      // 更新原始值
      originalGroupNames.value.set(agent.id, newName)
    } else {
      ElMessage.error(res.message || t('agent.groupUpdateFailed'))
      // 恢复原始值
      agent.group_name = originalName
    }
  } catch (error) {
    ElMessage.error(t('agent.groupUpdateFailed'))
    // 恢复原始值
    agent.group_name = originalName
  }
}

const handleViewServices = (agent: Agent) => {
  // 跳转到服务列表页面，并筛选该 Agent 的服务
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
