<template>
  <div class="acl-user-detail">
    <!-- 基本信息 -->
    <el-card shadow="never">
      <template #header>
        <div class="card-header">
          <span>{{ $t('acl.userInfo') }}</span>
          <el-button type="primary" link @click="router.back()">{{ $t('common.back') }}</el-button>
        </div>
      </template>
      <el-descriptions :column="2" border>
        <el-descriptions-item :label="$t('acl.userName')">{{ user?.name }}</el-descriptions-item>
        <el-descriptions-item :label="$t('user.alias')">{{ user?.alias || '-' }}</el-descriptions-item>
        <el-descriptions-item :label="$t('user.role')">
          <el-tag :type="user?.role === 'agent' ? 'success' : 'primary'" size="small">
            {{ user?.role === 'agent' ? $t('user.roleAgent') : $t('user.roleClient') }}
          </el-tag>
        </el-descriptions-item>
      </el-descriptions>
    </el-card>

    <!-- 用户授权 -->
    <el-card shadow="never" style="margin-top: 20px">
      <template #header>
        <div class="card-header">
          <span>{{ $t('acl.userAuth') }} ({{ user?.users?.length || 0 }})</span>
          <el-button type="primary" size="small" @click="showAddUserDialog = true">
            <el-icon><Plus /></el-icon>
            {{ $t('acl.addUser') }}
          </el-button>
        </div>
      </template>
      <el-table :data="user?.users || []" stripe>
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
          <span>{{ $t('acl.groupAuth') }} ({{ user?.groups?.length || 0 }})</span>
          <el-button type="primary" size="small" @click="showAddGroupDialog = true">
            <el-icon><Plus /></el-icon>
            {{ $t('acl.addGroup') }}
          </el-button>
        </div>
      </template>
      <el-table :data="user?.groups || []" stripe>
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
    <AddUserDialog v-model="showAddUserDialog" type="user" :target-id="userId" @success="fetchUser" />
    
    <!-- 添加分组授权弹窗 -->
    <AddGroupDialog v-model="showAddGroupDialog" type="user" :target-id="userId" @success="fetchUser" />
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Plus } from '@element-plus/icons-vue'
import { useI18n } from 'vue-i18n'
import { getUserACL, removeUserACLUser, removeUserACLGroup, type UserACLDetail, type ACLPermissionItem } from '@/api/acl'
import { formatTime } from '@/utils/time'
import AddUserDialog from './components/AddUserDialog.vue'
import AddGroupDialog from './components/AddGroupDialog.vue'

const { t } = useI18n()
const route = useRoute()
const router = useRouter()

const userId = Number(route.params.id)
const user = ref<UserACLDetail | null>(null)
const showAddUserDialog = ref(false)
const showAddGroupDialog = ref(false)

// 获取用户详情
const fetchUser = async () => {
  try {
    const res = await getUserACL(userId)
    if (res.success && res.data) {
      user.value = res.data
    }
  } catch (error) {
    console.error('获取用户详情失败:', error)
  }
}

// 撤销用户授权
const handleRevokeUser = async (row: ACLPermissionItem) => {
  try {
    await ElMessageBox.confirm(
      t('acl.revokeUserConfirm', { name: row.name }),
      t('common.warning'),
      { type: 'warning' }
    )
    const res = await removeUserACLUser(userId, row.id)
    if (res.success) {
      ElMessage.success(t('acl.revokeSuccess'))
      fetchUser()
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
    const res = await removeUserACLGroup(userId, row.id)
    if (res.success) {
      ElMessage.success(t('acl.revokeSuccess'))
      fetchUser()
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
  fetchUser()
})
</script>

<style scoped>
.acl-user-detail {
  width: 100%;
}

.card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}
</style>
