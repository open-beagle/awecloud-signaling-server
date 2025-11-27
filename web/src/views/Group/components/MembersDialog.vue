<template>
  <el-dialog
    v-model="visible"
    :title="`成员管理 - ${group?.name}`"
    width="700px"
    @close="handleClose"
  >
    <div class="members-dialog">
      <div class="add-member">
        <el-select
          v-model="selectedClientId"
          placeholder="选择用户"
          filterable
          style="width: 300px; margin-right: 10px"
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

      <el-table :data="members" stripe style="margin-top: 20px">
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
    </div>
  </el-dialog>
</template>

<script setup lang="ts">
import { ref, watch, computed } from 'vue'
import { ElMessage } from 'element-plus'
import { Plus, Delete } from '@element-plus/icons-vue'
import { getGroupMembers, addGroupMember, removeGroupMember } from '@/api/group'
import { getClients } from '@/api/client'
import type { Group, GroupMember } from '@/api/group'

interface Props {
  modelValue: boolean
  group: Group | null
}

interface Emits {
  (e: 'update:modelValue', value: boolean): void
  (e: 'success'): void
}

const props = defineProps<Props>()
const emit = defineEmits<Emits>()

const visible = computed({
  get: () => props.modelValue,
  set: (val) => emit('update:modelValue', val)
})

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
  if (!props.group) return
  
  try {
    const res = await getGroupMembers(props.group.id)
    if (res.success && res.data) {
      members.value = res.data
    }
  } catch (error) {
    ElMessage.error('加载成员失败')
  }
}

const loadClients = async () => {
  try {
    const res = await getClients()
    if (res.success && res.data) {
      allClients.value = res.data
    }
  } catch (error) {
    ElMessage.error('加载用户列表失败')
  }
}

const handleAddMember = async () => {
  if (!props.group || !selectedClientId.value) {
    ElMessage.warning('请选择用户')
    return
  }

  try {
    const res = await addGroupMember(props.group.id, selectedClientId.value, selectedRole.value)
    if (res.success) {
      ElMessage.success('添加成功')
      selectedClientId.value = null
      selectedRole.value = 'member'
      loadMembers()
      emit('success')
    }
  } catch (error) {
    ElMessage.error('添加失败')
  }
}

const handleRemoveMember = async (member: GroupMember) => {
  if (!props.group) return

  try {
    const res = await removeGroupMember(props.group.id, member.client_id)
    if (res.success) {
      ElMessage.success('移除成功')
      loadMembers()
      emit('success')
    }
  } catch (error) {
    ElMessage.error('移除失败')
  }
}

const handleClose = () => {
  members.value = []
  selectedClientId.value = null
  selectedRole.value = 'member'
}

watch(() => props.modelValue, (val) => {
  if (val && props.group) {
    loadMembers()
    loadClients()
  }
})
</script>

<style scoped>
.members-dialog {
  padding: 10px 0;
}

.add-member {
  display: flex;
  align-items: center;
}
</style>
