<template>
  <div class="group-members">
    <!-- 分组信息 -->
    <el-card shadow="never">
      <template #header>
        <div class="card-header">
          <span>{{ $t('group.info') }}</span>
          <el-button type="primary" link @click="router.back()">{{ $t('common.back') }}</el-button>
        </div>
      </template>
      <el-descriptions :column="2" border>
        <el-descriptions-item :label="$t('group.name')">{{ group?.name }}</el-descriptions-item>
        <el-descriptions-item :label="$t('group.description')">{{ group?.description || '-' }}</el-descriptions-item>
        <el-descriptions-item :label="$t('group.memberCount')">{{ group?.member_count || 0 }}</el-descriptions-item>
        <el-descriptions-item :label="$t('common.createdAt')">{{ formatTime(group?.created_at) }}</el-descriptions-item>
      </el-descriptions>
    </el-card>

    <!-- 成员列表 -->
    <el-card shadow="never" style="margin-top: 20px">
      <template #header>
        <div class="card-header">
          <span>{{ $t('group.memberList') }} ({{ members.length }})</span>
          <el-button type="primary" size="small" :disabled="!canManage" @click="showAddDialog = true">
            <el-icon><Plus /></el-icon>
            {{ $t('group.addMember') }}
          </el-button>
        </div>
      </template>
      <el-table v-loading="loading" :data="members" stripe>
        <el-table-column prop="user.name" :label="$t('user.name')" min-width="150" />
        <el-table-column prop="user.alias" :label="$t('user.alias')" min-width="120" />
        <el-table-column prop="user.role" :label="$t('user.role')" width="100">
          <template #default="{ row }">
            <el-tag :type="row.user?.role === 'agent' ? 'success' : 'primary'" size="small">
              {{ row.user?.role === 'agent' ? $t('user.roleAgent') : $t('user.roleClient') }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="created_at" :label="$t('group.joinedAt')" width="180">
          <template #default="{ row }">
            {{ formatTime(row.created_at) }}
          </template>
        </el-table-column>
        <el-table-column :label="$t('common.actions')" width="100" fixed="right">
          <template #default="{ row }">
            <el-button type="danger" link size="small" :disabled="!canManage" @click="handleRemove(row)">{{ $t('group.removeMember') }}</el-button>
          </template>
        </el-table-column>
      </el-table>
    </el-card>

    <!-- 添加成员弹窗 -->
    <el-dialog v-model="showAddDialog" :title="$t('group.addMember')" width="500px" @close="handleCloseDialog">
      <el-form ref="formRef" :model="form" :rules="rules" label-width="80px">
        <el-form-item :label="$t('group.selectUser')" prop="userId">
          <el-select
            v-model="form.userId"
            filterable
            :placeholder="$t('group.selectUserPlaceholder')"
            style="width: 100%"
            :loading="loadingUsers"
          >
            <el-option
              v-for="user in availableUsers"
              :key="user.id"
              :label="`${user.name}${user.alias ? ' (' + user.alias + ')' : ''}`"
              :value="user.id"
            />
          </el-select>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="handleCloseDialog">{{ $t('common.cancel') }}</el-button>
        <el-button type="primary" :loading="submitting" @click="handleAddMember">{{ $t('common.confirm') }}</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, computed, onMounted, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Plus } from '@element-plus/icons-vue'
import type { FormInstance, FormRules } from 'element-plus'
import { useI18n } from 'vue-i18n'
import { getGroup, getGroupMembers, addGroupMember, removeGroupMember, type Group, type GroupMember } from '@/api/group'
import { getTenantMembers, type TenantMember } from '@/api/resource'
import { formatTime } from '@/utils/time'
import { useTenantStore } from '@/stores/tenant'

const { t } = useI18n()
const route = useRoute()
const router = useRouter()
const tenantStore = useTenantStore()

const groupId = Number(route.params.id)
const loading = ref(false)
const loadingUsers = ref(false)
const submitting = ref(false)
const group = ref<Group | null>(null)
const members = ref<GroupMember[]>([])
const tenantMembers = ref<TenantMember[]>([])
const showAddDialog = ref(false)
const formRef = ref<FormInstance>()

const form = reactive({
  userId: null as number | null
})

const rules: FormRules = {
  userId: [{ required: true, message: t('group.selectUserRequired'), trigger: 'change' }]
}

// 可选用户（排除已是成员的）
const availableUsers = computed(() => {
  const memberUserIds = members.value.map(m => m.user_id)
  return tenantMembers.value.filter(member => member.enabled && !memberUserIds.includes(member.user_id)).map(member => ({ id: member.user_id, name: member.name, alias: member.alias }))
})
const canManage = computed(() => tenantStore.canTenant('tenant.groups.write') && !!group.value?.tenant_id && tenantStore.tenantId === group.value.tenant_id)

// 获取分组信息
const fetchGroup = async () => {
  try {
    const res = await getGroup(groupId)
    if (res.success && res.data) {
      group.value = res.data
      await fetchUsers()
    }
  } catch (error) {
    console.error('获取分组信息失败:', error)
  }
}

// 获取成员列表
const fetchMembers = async () => {
  loading.value = true
  try {
    const res = await getGroupMembers(groupId)
    if (res.success && res.data) {
      members.value = res.data
    }
  } catch (error) {
    console.error('获取成员列表失败:', error)
  } finally {
    loading.value = false
  }
}

// 获取用户列表
const fetchUsers = async () => {
  loadingUsers.value = true
  try {
    if (group.value?.tenant_id && group.value.tenant_id === tenantStore.tenantId) {
      const res = await getTenantMembers(group.value.tenant_id)
      if (res.success && res.data) tenantMembers.value = res.data
    }
  } catch (error) {
    console.error('获取用户列表失败:', error)
  } finally {
    loadingUsers.value = false
  }
}

// 添加成员
const handleAddMember = async () => {
  if (!formRef.value) return
  if (!canManage.value) return
  
  await formRef.value.validate(async (valid) => {
    if (!valid || !form.userId) return
    
    submitting.value = true
    try {
      const res = await addGroupMember(groupId, form.userId)
      if (res.success) {
        ElMessage.success(t('group.addSuccess'))
        handleCloseDialog()
        fetchMembers()
        fetchGroup()
      } else {
        ElMessage.error(res.message || t('group.addFailed'))
      }
    } catch (error) {
      console.error('添加成员失败:', error)
    } finally {
      submitting.value = false
    }
  })
}

// 移除成员
const handleRemove = async (row: GroupMember) => {
  if (!canManage.value) return
  try {
    await ElMessageBox.confirm(
      t('group.removeMemberConfirm', { name: row.user?.name }),
      t('common.warning'),
      { type: 'warning' }
    )
    const res = await removeGroupMember(groupId, row.user_id)
    if (res.success) {
      ElMessage.success(t('group.removeSuccess'))
      fetchMembers()
      fetchGroup()
    } else {
      ElMessage.error(res.message || t('group.removeFailed'))
    }
  } catch (error) {
    if (error !== 'cancel') {
      console.error('移除成员失败:', error)
    }
  }
}

// 关闭弹窗
const handleCloseDialog = () => {
  showAddDialog.value = false
  form.userId = null
  formRef.value?.resetFields()
}

onMounted(() => {
  fetchGroup()
  fetchMembers()
})
watch(() => tenantStore.contextRevision, () => router.replace('/groups'))
</script>

<style scoped>
.group-members {
  width: 100%;
}

.card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}
</style>
