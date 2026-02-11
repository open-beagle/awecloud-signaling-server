<template>
  <div class="acl-group-detail">
    <!-- 基本信息 -->
    <el-card shadow="never">
      <template #header>
        <div class="card-header">
          <span>{{ $t('acl.groupInfo') }}</span>
        </div>
      </template>
      <el-descriptions :column="2" border>
        <el-descriptions-item :label="$t('acl.groupName')">{{ group?.name }}</el-descriptions-item>
        <el-descriptions-item :label="$t('group.description')">{{ group?.alias || '-' }}</el-descriptions-item>
      </el-descriptions>
    </el-card>

    <!-- 用户授权 -->
    <el-card shadow="never" style="margin-top: 20px">
      <template #header>
        <div class="card-header">
          <span>{{ $t('acl.userAuth') }} ({{ group?.users?.length || 0 }})</span>
          <el-button type="primary" size="small" @click="showAddUserDialog = true">
            <el-icon><Plus /></el-icon>
            {{ $t('acl.addUser') }}
          </el-button>
        </div>
      </template>
      <el-table :data="group?.users || []" stripe>
        <el-table-column prop="name" :label="$t('acl.userName')" min-width="150" />
        <el-table-column prop="alias" :label="$t('user.alias')" min-width="120" />
        <el-table-column prop="granted_at" :label="$t('acl.grantedAt')" width="180">
          <template #default="{ row }">
            {{ formatTime(row.granted_at) }}
          </template>
        </el-table-column>
        <el-table-column :label="$t('common.actions')" width="100" fixed="right">
          <template #default="{ row }">
            <el-button type="danger" link size="small" @click="handleRevokeUser(row)">{{ $t('acl.revoke') }}</el-button>
          </template>
        </el-table-column>
      </el-table>
    </el-card>

    <!-- 分组授权 -->
    <el-card shadow="never" style="margin-top: 20px">
      <template #header>
        <div class="card-header">
          <span>{{ $t('acl.groupAuth') }} ({{ group?.groups?.length || 0 }})</span>
          <el-button type="primary" size="small" @click="showAddGroupDialog = true">
            <el-icon><Plus /></el-icon>
            {{ $t('acl.addGroup') }}
          </el-button>
        </div>
      </template>
      <el-table :data="group?.groups || []" stripe>
        <el-table-column prop="name" :label="$t('acl.groupName')" min-width="150" />
        <el-table-column prop="alias" :label="$t('group.description')" min-width="120" />
        <el-table-column prop="granted_at" :label="$t('acl.grantedAt')" width="180">
          <template #default="{ row }">
            {{ formatTime(row.granted_at) }}
          </template>
        </el-table-column>
        <el-table-column :label="$t('common.actions')" width="100" fixed="right">
          <template #default="{ row }">
            <el-button type="danger" link size="small" @click="handleRevokeGroup(row)">{{ $t('acl.revoke') }}</el-button>
          </template>
        </el-table-column>
      </el-table>
    </el-card>

    <!-- 添加用户授权弹窗 -->
    <AuthGrantDialog
      ref="addUserDialogRef"
      v-model="showAddUserDialog"
      :title="$t('acl.addUserAuth')"
      mode="user"
      :client-only="false"
      @confirm="handleConfirmUser"
    />
    
    <!-- 添加分组授权弹窗 -->
    <AuthGrantDialog
      ref="addGroupDialogRef"
      v-model="showAddGroupDialog"
      :title="$t('acl.addGroupAuth')"
      mode="group"
      @confirm="handleConfirmGroup"
    />
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useRoute } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Plus } from '@element-plus/icons-vue'
import { useI18n } from 'vue-i18n'
import { getGroupACL, removeGroupACLUser, removeGroupACLGroup, addGroupACLUsers, addGroupACLGroups, type GroupACLDetail, type ACLPermissionItem } from '@/api/acl'
import { formatTime } from '@/utils/time'
import AuthGrantDialog from '@/components/Common/AuthGrantDialog.vue'

const { t } = useI18n()
const route = useRoute()

const groupId = Number(route.params.id)
const group = ref<GroupACLDetail | null>(null)
const showAddUserDialog = ref(false)
const showAddGroupDialog = ref(false)
const addUserDialogRef = ref<InstanceType<typeof AuthGrantDialog>>()
const addGroupDialogRef = ref<InstanceType<typeof AuthGrantDialog>>()

// 获取分组详情
const fetchGroup = async () => {
  try {
    const res = await getGroupACL(groupId)
    if (res.success && res.data) {
      group.value = res.data
    }
  } catch (error) {
    console.error('获取分组详情失败:', error)
  }
}

// 确认用户授权
const handleConfirmUser = async (userIds: number[]) => {
  addUserDialogRef.value?.setSubmitting(true)
  try {
    const res = await addGroupACLUsers(groupId, userIds)
    if (res?.success) { ElMessage.success(t('acl.authSuccess')); addUserDialogRef.value?.close(); fetchGroup() }
    else { ElMessage.error(res?.message || t('acl.authFailed')) }
  } catch { ElMessage.error(t('acl.authFailed')) }
  finally { addUserDialogRef.value?.setSubmitting(false) }
}

// 确认分组授权
const handleConfirmGroup = async (groupIds: number[]) => {
  addGroupDialogRef.value?.setSubmitting(true)
  try {
    const res = await addGroupACLGroups(groupId, groupIds)
    if (res?.success) { ElMessage.success(t('acl.authSuccess')); addGroupDialogRef.value?.close(); fetchGroup() }
    else { ElMessage.error(res?.message || t('acl.authFailed')) }
  } catch { ElMessage.error(t('acl.authFailed')) }
  finally { addGroupDialogRef.value?.setSubmitting(false) }
}

// 撤销用户授权
const handleRevokeUser = async (row: ACLPermissionItem) => {
  try {
    await ElMessageBox.confirm(
      t('acl.revokeUserConfirm', { name: row.name }),
      t('common.warning'),
      { type: 'warning' }
    )
    const res = await removeGroupACLUser(groupId, row.id)
    if (res.success) {
      ElMessage.success(t('acl.revokeSuccess'))
      fetchGroup()
    } else {
      ElMessage.error(res.message || t('acl.revokeFailed'))
    }
  } catch (error) {
    if (error !== 'cancel') {
      console.error('撤销授权失败:', error)
    }
  }
}

// 撤销分组授权
const handleRevokeGroup = async (row: ACLPermissionItem) => {
  try {
    await ElMessageBox.confirm(
      t('acl.revokeGroupConfirm', { name: row.name }),
      t('common.warning'),
      { type: 'warning' }
    )
    const res = await removeGroupACLGroup(groupId, row.id)
    if (res.success) {
      ElMessage.success(t('acl.revokeSuccess'))
      fetchGroup()
    } else {
      ElMessage.error(res.message || t('acl.revokeFailed'))
    }
  } catch (error) {
    if (error !== 'cancel') {
      console.error('撤销授权失败:', error)
    }
  }
}

onMounted(() => {
  fetchGroup()
})
</script>

<style scoped>
.acl-group-detail {
  width: 100%;
}

.card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}
</style>
