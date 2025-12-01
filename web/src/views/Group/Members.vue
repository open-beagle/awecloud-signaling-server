<template>
  <div class="members-page">
    <el-breadcrumb separator="/" class="breadcrumb">
      <el-breadcrumb-item :to="{ path: '/groups' }">用户组管理</el-breadcrumb-item>
      <el-breadcrumb-item>{{ group?.name || '成员管理' }}</el-breadcrumb-item>
    </el-breadcrumb>

    <el-card>
      <template #header>
        <div class="card-header">
          <span class="card-title">成员管理</span>
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
                :label="client.client_id"
                :value="client.id"
              />
            </el-select>
            <el-select
              v-model="selectedRole"
              placeholder="角色"
              style="width: 120px; margin-right: 10px"
            >
              <el-option label="成员" value="member" />
              <el-option label="管理员" value="admin" />
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
          {{ row.client?.client_id || '-' }}
        </template>
      </el-table-column>
      <el-table-column prop="role" label="角色" width="120">
        <template #default="{ row }">
          <el-tag :type="row.role === 'admin' ? 'danger' : 'info'">
            {{ row.role === 'admin' ? '管理员' : '成员' }}
          </el-tag>
        </template>
      </el-table-column>
      <el-table-column label="加入时间" width="180">
        <template #default="{ row }">
          {{ formatDate(row.created_at) }}
        </template>
      </el-table-column>
      <el-table-column label="操作" width="100">
        <template #default="{ row }">
          <el-button
            size="small"
            type="danger"
            :icon="Delete"
            @click="handleRemoveMember(row)"
          />
        </template>
      </el-table-column>
      </el-table>
    </el-card>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import { Plus, Delete } from '@element-plus/icons-vue'
import { getGroupMembers, addGroupMember, removeGroupMember } from '@/api/group'
import { getClients } from '@/api/client'
import type { Group, GroupMember } from '@/api/group'

const route = useRoute()
const router = useRouter()

const loading = ref(false)
const group = ref<Group | null>(null)
const members = ref<GroupMember[]>([])
const allClients = ref<any[]>([])
const selectedClientId = ref<number | null>(null)
const selectedRole = ref('member')

const availableClients = computed(() => {
  const memberClientIds = new Set(members.value.map(m => m.client_id))
  return allClients.value.filter(c => !memberClientIds.has(c.id))
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
    console.log('Group members response:', res)
    if (res.success && res.data) {
      console.log('Members data:', res.data)
      members.value = res.data
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
    const res = await getClients()
    if (res.success && res.clients) {
      allClients.value = res.clients
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
    const res = await addGroupMember(groupId, selectedClientId.value, selectedRole.value)
    if (res.success) {
      ElMessage.success('添加成功')
      selectedClientId.value = null
      selectedRole.value = 'member'
      loadMembers()
    }
  } catch (error) {
    ElMessage.error('添加失败')
  }
}

const handleRemoveMember = async (member: GroupMember) => {
  const groupId = Number(route.params.id)
  if (!groupId) return

  try {
    const res = await removeGroupMember(groupId, member.client_id)
    if (res.success) {
      ElMessage.success('移除成功')
      loadMembers()
    }
  } catch (error) {
    ElMessage.error('移除失败')
  }
}

onMounted(() => {
  // 从路由参数获取组信息
  if (route.params.name) {
    group.value = {
      id: Number(route.params.id),
      name: route.params.name as string,
      description: '',
      created_at: '',
      updated_at: ''
    }
  }
  loadMembers()
  loadClients()
})
</script>

<style scoped>
.members-page {
  width: 100%;
}

.breadcrumb {
  margin-bottom: 20px;
}

.card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.card-title {
  font-size: 18px;
  font-weight: 500;
}

.header-actions {
  display: flex;
  align-items: center;
}
</style>
