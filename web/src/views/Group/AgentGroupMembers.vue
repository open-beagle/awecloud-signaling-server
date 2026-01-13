<template>
  <div class="members-page">
    <el-card class="members-card">
      <template #header>
        <div class="card-header">
          <div class="header-left">
            <el-button :icon="ArrowLeft" @click="goBack">返回</el-button>
            <span class="card-title">代理分组成员管理</span>
          </div>
          <div class="header-actions">
            <el-select
              v-model="selectedAgentId"
              placeholder="选择代理"
              filterable
              style="width: 250px; margin-right: 10px"
            >
              <el-option
                v-for="agent in availableAgents"
                :key="agent.id"
                :label="agent.name + (agent.alias ? ` (${agent.alias})` : '')"
                :value="agent.id"
              />
            </el-select>
            <el-button type="primary" :icon="Plus" @click="handleAddMember">
              添加成员
            </el-button>
          </div>
        </div>
      </template>

      <el-table v-loading="loading" :data="members" stripe>
        <el-table-column label="代理" min-width="150">
          <template #default="{ row }">
            {{ row.agent?.name || '-' }}
            <span v-if="row.agent?.alias" class="alias-text">({{ row.agent.alias }})</span>
          </template>
        </el-table-column>
        <el-table-column label="隧道 IP" width="150">
          <template #default="{ row }">
            {{ row.agent?.ip || '-' }}
          </template>
        </el-table-column>
        <el-table-column label="加入时间" width="180">
          <template #default="{ row }">
            {{ formatDate(row.created_at) }}
          </template>
        </el-table-column>
        <el-table-column label="操作" width="100">
          <template #default="{ row }">
            <el-tooltip content="移除" placement="top">
              <el-button
                size="small"
                type="danger"
                :icon="Delete"
                @click="handleRemoveMember(row)"
              />
            </el-tooltip>
          </template>
        </el-table-column>
      </el-table>
    </el-card>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Plus, Delete, ArrowLeft } from '@element-plus/icons-vue'
import { getAgentGroupMembers, addAgentGroupMember, removeAgentGroupMember, type AgentGroupMember } from '@/api/group'
import { getAgents } from '@/api/agent'

const route = useRoute()
const router = useRouter()

const loading = ref(false)
const members = ref<AgentGroupMember[]>([])
const allAgents = ref<any[]>([])
const selectedAgentId = ref<number | null>(null)

const availableAgents = computed(() => {
  const memberAgentIds = new Set(members.value.map(m => m.agent_id))
  return allAgents.value.filter(a => !memberAgentIds.has(a.id))
})

const formatDate = (dateStr: string) => {
  return new Date(dateStr).toLocaleString('zh-CN')
}

const goBack = () => {
  router.push({ name: 'AgentGroups' })
}

const loadMembers = async () => {
  const groupId = Number(route.params.id)
  if (!groupId) return

  loading.value = true
  try {
    const res = await getAgentGroupMembers(groupId)
    if (res.success && res.data) {
      members.value = res.data
    }
  } catch (error) {
    console.error('Load members error:', error)
    ElMessage.error('加载成员失败')
  } finally {
    loading.value = false
  }
}

const loadAgents = async () => {
  try {
    const res = await getAgents()
    if (res.success && res.data) {
      allAgents.value = res.data
    }
  } catch (error) {
    ElMessage.error('加载代理列表失败')
  }
}

const handleAddMember = async () => {
  const groupId = Number(route.params.id)
  if (!groupId || !selectedAgentId.value) {
    ElMessage.warning('请选择代理')
    return
  }

  try {
    const res = await addAgentGroupMember(groupId, selectedAgentId.value)
    if (res.success) {
      ElMessage.success('添加成功')
      selectedAgentId.value = null
      loadMembers()
    } else {
      ElMessage.error(res.message || '添加失败')
    }
  } catch (error) {
    ElMessage.error('添加失败')
  }
}

const handleRemoveMember = async (member: AgentGroupMember) => {
  const groupId = Number(route.params.id)
  if (!groupId) return

  try {
    await ElMessageBox.confirm('确定要将该代理移出分组吗？', { type: 'warning' })
    const res = await removeAgentGroupMember(groupId, member.agent_id)
    if (res.success) {
      ElMessage.success('移除成功')
      loadMembers()
    } else {
      ElMessage.error(res.message || '移除失败')
    }
  } catch (error: any) {
    if (error !== 'cancel') {
      ElMessage.error('移除失败')
    }
  }
}

onMounted(() => {
  loadMembers()
  loadAgents()
})
</script>

<style scoped>
.members-page {
  width: 100%;
  height: 100%;
  display: flex;
  flex-direction: column;
}

.members-card {
  flex: 1;
  display: flex;
  flex-direction: column;
}

.members-card :deep(.el-card__body) {
  flex: 1;
  display: flex;
  flex-direction: column;
}

.members-card :deep(.el-table) {
  flex: 1;
}

.card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.header-left {
  display: flex;
  align-items: center;
  gap: 12px;
}

.card-title {
  font-size: 18px;
  font-weight: 500;
}

.header-actions {
  display: flex;
  align-items: center;
}

.alias-text {
  color: #909399;
  font-size: 12px;
}
</style>
