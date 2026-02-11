<template>
  <div class="acl-ssh-detail">
    <!-- 基本信息 -->
    <el-card shadow="never">
      <template #header>
        <div class="card-header">
          <span>{{ $t('acl.sshInfo') }}</span>
          <el-button type="primary" link @click="router.back()">{{ $t('common.back') }}</el-button>
        </div>
      </template>
      <el-descriptions :column="2" border>
        <el-descriptions-item :label="$t('acl.agentName')">{{ ssh?.name }}</el-descriptions-item>
        <el-descriptions-item :label="$t('user.alias')">{{ ssh?.alias || '-' }}</el-descriptions-item>
        <el-descriptions-item :label="$t('user.role')">
          <el-tag :type="ssh?.role === 'agent' ? 'success' : 'primary'" size="small">
            {{ ssh?.role === 'agent' ? $t('user.roleAgent') : $t('user.roleClient') }}
          </el-tag>
        </el-descriptions-item>
        <el-descriptions-item :label="$t('acl.sshEnabled')">
          <template v-if="ssh?.role === 'client'">
            <el-tag type="success" size="small">{{ $t('acl.autoEnabled') }}</el-tag>
          </template>
          <template v-else>
            <el-tag :type="ssh?.ssh_enabled ? 'success' : 'info'" size="small">
              {{ ssh?.ssh_enabled ? $t('common.enabled') : $t('common.disabled') }}
            </el-tag>
          </template>
        </el-descriptions-item>
      </el-descriptions>
    </el-card>

    <!-- 用户授权 -->
    <el-card shadow="never" style="margin-top: 20px">
      <template #header>
        <div class="card-header">
          <span>{{ $t('acl.userAuth') }} ({{ ssh?.users?.length || 0 }})</span>
          <el-button type="primary" size="small" @click="showAddUserDialog = true">
            <el-icon><Plus /></el-icon>
            {{ $t('acl.addUser') }}
          </el-button>
        </div>
      </template>
      <el-table :data="ssh?.users || []" stripe>
        <el-table-column prop="name" :label="$t('acl.userName')" min-width="120" />
        <el-table-column prop="alias" :label="$t('user.alias')" min-width="100" />
        <el-table-column prop="ssh_users" :label="$t('acl.sshUsers')" min-width="150">
          <template #default="{ row }">
            <el-tag v-for="user in row.ssh_users" :key="user" size="small" style="margin-right: 4px">{{ user }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="enabled" :label="$t('common.status')" width="80" align="center">
          <template #default="{ row }">
            <el-tag :type="row.enabled ? 'success' : 'info'" size="small">
              {{ row.enabled ? $t('common.enabled') : $t('common.disabled') }}
            </el-tag>
          </template>
        </el-table-column>
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
          <span>{{ $t('acl.groupAuth') }} ({{ ssh?.groups?.length || 0 }})</span>
          <el-button type="primary" size="small" @click="showAddGroupDialog = true">
            <el-icon><Plus /></el-icon>
            {{ $t('acl.addGroup') }}
          </el-button>
        </div>
      </template>
      <el-table :data="ssh?.groups || []" stripe>
        <el-table-column prop="name" :label="$t('acl.groupName')" min-width="120" />
        <el-table-column prop="alias" :label="$t('group.description')" min-width="100" />
        <el-table-column prop="ssh_users" :label="$t('acl.sshUsers')" min-width="150">
          <template #default="{ row }">
            <el-tag v-for="user in row.ssh_users" :key="user" size="small" style="margin-right: 4px">{{ user }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="enabled" :label="$t('common.status')" width="80" align="center">
          <template #default="{ row }">
            <el-tag :type="row.enabled ? 'success' : 'info'" size="small">
              {{ row.enabled ? $t('common.enabled') : $t('common.disabled') }}
            </el-tag>
          </template>
        </el-table-column>
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
    <AddSSHUserDialog v-model="showAddUserDialog" :agent-id="sshId" @success="fetchSSH" />
    
    <!-- 添加分组授权弹窗 -->
    <AddSSHGroupDialog v-model="showAddGroupDialog" :agent-id="sshId" @success="fetchSSH" />
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Plus } from '@element-plus/icons-vue'
import { useI18n } from 'vue-i18n'
import { getSSHACL, removeSSHACLUser, removeSSHACLGroup, type SSHACLDetail, type SSHACLPermissionItem } from '@/api/acl'
import { formatTime } from '@/utils/time'
import AddSSHUserDialog from './components/AddSSHUserDialog.vue'
import AddSSHGroupDialog from './components/AddSSHGroupDialog.vue'

const { t } = useI18n()
const route = useRoute()
const router = useRouter()

const sshId = Number(route.params.id)
const ssh = ref<SSHACLDetail | null>(null)
const showAddUserDialog = ref(false)
const showAddGroupDialog = ref(false)

// 获取 SSH 详情
const fetchSSH = async () => {
  try {
    const res = await getSSHACL(sshId)
    if (res.success && res.data) {
      ssh.value = res.data
    }
  } catch (error) {
    console.error('获取 SSH 详情失败:', error)
  }
}

// 撤销用户授权
const handleRevokeUser = async (row: SSHACLPermissionItem) => {
  try {
    await ElMessageBox.confirm(
      t('acl.revokeUserConfirm', { name: row.name }),
      t('common.warning'),
      { type: 'warning' }
    )
    const res = await removeSSHACLUser(sshId, row.id)
    if (res.success) {
      ElMessage.success(t('acl.revokeSuccess'))
      fetchSSH()
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
const handleRevokeGroup = async (row: SSHACLPermissionItem) => {
  try {
    await ElMessageBox.confirm(
      t('acl.revokeGroupConfirm', { name: row.name }),
      t('common.warning'),
      { type: 'warning' }
    )
    const res = await removeSSHACLGroup(sshId, row.id)
    if (res.success) {
      ElMessage.success(t('acl.revokeSuccess'))
      fetchSSH()
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
  fetchSSH()
})
</script>

<style scoped>
.acl-ssh-detail {
  width: 100%;
}

.card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}
</style>
