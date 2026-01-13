<template>
  <div class="members-page">
    <el-card class="members-card">
      <template #header>
        <div class="card-header">
          <div class="header-left">
            <span class="card-title">用户分组成员管理</span>
            <span v-if="groupInfo" class="group-info">
              分组: {{ groupInfo.alias || groupInfo.name }} ({{ groupInfo.name }})
            </span>
          </div>
          <div class="header-actions">
            <el-select
              v-model="selectedClientId"
              placeholder="选择用户"
              filterable
              style="width: 250px; margin-right: 10px"
            >
              <el-option
                v-for="client in availableClients"
                :key="client.id"
                :label="client.name + (client.alias ? ` (${client.alias})` : '')"
                :value="client.id"
              />
            </el-select>
            <el-button type="primary" :icon="Plus" @click="handleAddMember">
              添加成员
            </el-button>
          </div>
        </div>
      </template>

      <el-table v-loading="loading" :data="members" stripe>
        <el-table-column label="用户" min-width="200">
          <template #default="{ row }">
            {{ row.name || '-' }}
            <span v-if="row.alias" class="alias-text">({{ row.alias }})</span>
          </template>
        </el-table-column>
        <el-table-column label="加入时间" width="180">
          <template #default="{ row }">
            {{ formatDate(row.joined_at) }}
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
import { useRoute } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Plus, Delete } from '@element-plus/icons-vue'
import { getGroupMembers, addGroupMember, removeGroupMember } from '@/api/group'
import { getClients } from '@/api/client'

const route = useRoute()

const loading = ref(false)
const members = ref<any[]>([])
const allClients = ref<any[]>([])
const selectedClientId = ref<number | null>(null)
const groupInfo = ref<any>(null)

const availableClients = computed(() => {
  const memberIds = new Set(members.value.map(m => m.id))
  return allClients.value.filter(c => !memberIds.has(c.id))
})

const formatDate = (dateStr: string) => {
  return new Date(dateStr).toLocaleString('zh-CN')
}

const loadMembers = async () => {
  const groupId = Number(route.params.id)
  if (!groupId) return

  loading.value = true
  try {
    const res = await getGroupMembers(groupId)
    if (res.success && res.data) {
      // 兼容新旧两种 API 响应格式
      if (res.data.group && res.data.members) {
        // 新格式: { group: {...}, members: [...] }
        groupInfo.value = res.data.group
        members.value = res.data.members
      } else if (Array.isArray(res.data)) {
        // 旧格式: 直接返回数组
        members.value = res.data
      }
    }
  } catch (error) {
    console.error('Load members error:', error)
    ElMessage.error('加载成员失败')
  } finally {
    loading.value = false
  }
}

const loadClients = async () => {
  try {
    const res = await getClients(1, 1000)
    if (res.success && res.data) {
      allClients.value = res.data
    }
  } catch (error) {
    ElMessage.error('加载用户列表失败')
  }
}

const handleAddMember = async () => {
  const groupId = Number(route.params.id)
  if (!groupId || !selectedClientId.value) {
    ElMessage.warning('请选择用户')
    return
  }

  try {
    const res = await addGroupMember(groupId, selectedClientId.value)
    if (res.success) {
      ElMessage.success('添加成功')
      selectedClientId.value = null
      loadMembers()
    } else {
      ElMessage.error(res.message || '添加失败')
    }
  } catch (error) {
    ElMessage.error('添加失败')
  }
}

const handleRemoveMember = async (member: any) => {
  const groupId = Number(route.params.id)
  if (!groupId) return

  try {
    await ElMessageBox.confirm('确定要将该成员移出分组吗？', { type: 'warning' })
    const res = await removeGroupMember(groupId, member.id)
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
  loadClients()
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
  gap: 16px;
}

.card-title {
  font-size: 18px;
  font-weight: 500;
}

.group-info {
  font-size: 14px;
  color: #606266;
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
