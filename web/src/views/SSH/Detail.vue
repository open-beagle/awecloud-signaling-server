<template>
  <div class="ssh-detail-container">
    <!-- 基本信息 -->
    <el-card class="info-card" v-loading="loading">
      <template #header>
        <span>{{ $t('ssh.basicInfo') }}</span>
      </template>
      <el-descriptions :column="2" border>
        <el-descriptions-item :label="$t('ssh.agentName')">
          {{ agentInfo?.alias ? `${agentInfo.alias} (${agentInfo.name})` : agentInfo?.name || '-' }}
        </el-descriptions-item>
        <el-descriptions-item :label="$t('ssh.agentIp')">
          {{ agentInfo?.tailscale_ip || '-' }}
        </el-descriptions-item>
      </el-descriptions>
    </el-card>

    <!-- 用户授权 -->
    <el-card class="auth-card">
      <template #header>
        <div class="card-header">
          <span>{{ $t('ssh.userAuth') }}</span>
          <el-button type="primary" size="small" @click="handleAddUser">
            <el-icon><Plus /></el-icon>
            {{ $t('ssh.addUser') }}
          </el-button>
        </div>
      </template>
      <el-table :data="clientPermissions" v-loading="loading" stripe>
        <el-table-column type="index" label="#" width="60" />
        <el-table-column prop="client_name" :label="$t('ssh.userName')" min-width="120" />
        <el-table-column prop="client_ip" :label="$t('ssh.userIp')" width="130" />
        <el-table-column :label="$t('ssh.sshUsers')" min-width="200">
          <template #default="{ row }">
            <el-tag v-for="user in row.ssh_users" :key="user" size="small" style="margin-right: 4px">
              {{ user }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column :label="$t('ssh.grantedAt')" width="160">
          <template #default="{ row }">
            {{ formatTime(row.created_at) }}
          </template>
        </el-table-column>
        <el-table-column :label="$t('common.actions')" width="100" fixed="right">
          <template #default="{ row }">
            <el-button type="danger" size="small" @click="handleRevokeUserAuth(row)">
              {{ $t('ssh.revoke') }}
            </el-button>
          </template>
        </el-table-column>
      </el-table>
      <div v-if="clientPermissions.length === 0 && !loading" class="empty-tip">
        {{ $t('common.noData') }}
      </div>
    </el-card>

    <!-- 分组授权 -->
    <el-card class="auth-card">
      <template #header>
        <div class="card-header">
          <span>{{ $t('ssh.groupAuth') }}</span>
          <el-button type="primary" size="small" @click="handleAddGroup">
            <el-icon><Plus /></el-icon>
            {{ $t('ssh.addGroup') }}
          </el-button>
        </div>
      </template>
      <el-table :data="groupPermissions" v-loading="loading" stripe>
        <el-table-column type="index" label="#" width="60" />
        <el-table-column prop="group_name" :label="$t('ssh.groupName')" min-width="120" />
        <el-table-column :label="$t('ssh.memberCount')" width="100">
          <template #default="{ row }">
            {{ row.member_count || 0 }}
          </template>
        </el-table-column>
        <el-table-column :label="$t('ssh.sshUsers')" min-width="200">
          <template #default="{ row }">
            <el-tag v-for="user in row.ssh_users" :key="user" size="small" style="margin-right: 4px">
              {{ user }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column :label="$t('ssh.grantedAt')" width="160">
          <template #default="{ row }">
            {{ formatTime(row.created_at) }}
          </template>
        </el-table-column>
        <el-table-column :label="$t('common.actions')" width="100" fixed="right">
          <template #default="{ row }">
            <el-button type="danger" size="small" @click="handleRevokeGroupAuth(row)">
              {{ $t('ssh.revoke') }}
            </el-button>
          </template>
        </el-table-column>
      </el-table>
      <div v-if="groupPermissions.length === 0 && !loading" class="empty-tip">
        {{ $t('common.noData') }}
      </div>
    </el-card>

    <!-- 添加用户授权对话框 -->
    <el-dialog v-model="addUserDialogVisible" :title="$t('ssh.addUserAuth')" width="600px">
      <el-form :model="addUserForm" label-width="100px">
        <el-form-item :label="$t('ssh.selectUser')" required>
          <div class="selector-container">
            <el-input
              v-model="userSearchText"
              :placeholder="$t('ssh.searchUserPlaceholder')"
              clearable
              class="selector-search"
            >
              <template #prefix>
                <el-icon><Search /></el-icon>
              </template>
            </el-input>
            <div class="selector-list">
              <el-checkbox-group v-model="addUserForm.clientIds">
                <div v-for="client in filteredClientList" :key="client.id" class="selector-item">
                  <el-checkbox :value="client.id">
                    {{ client.client_id }} ({{ client.tailscale_ip || '-' }})
                  </el-checkbox>
                </div>
              </el-checkbox-group>
              <div v-if="filteredClientList.length === 0" class="selector-empty">
                {{ $t('common.noData') }}
              </div>
            </div>
            <div class="selector-footer">
              {{ $t('ssh.selected') }}: {{ addUserForm.clientIds.length }} {{ $t('ssh.users') }}
            </div>
          </div>
        </el-form-item>
        <el-form-item :label="$t('ssh.sshUsers')" required>
          <div class="tag-input-container">
            <div class="tag-input-wrapper">
              <el-tag
                v-for="user in addUserForm.sshUsers"
                :key="user"
                closable
                @close="removeSSHUser(addUserForm.sshUsers, user)"
                class="ssh-user-tag"
              >
                {{ user }}
              </el-tag>
              <el-input
                v-model="sshUserInputForUser"
                :placeholder="$t('ssh.sshUserInputPlaceholder')"
                class="tag-input"
                @keyup.enter="addSSHUserToList(addUserForm.sshUsers, sshUserInputForUser); sshUserInputForUser = ''"
              />
            </div>
            <div class="quick-options">
              {{ $t('ssh.quickOptions') }}:
              <el-button size="small" @click="addQuickSSHUser(addUserForm.sshUsers, 'root')">root</el-button>
              <el-button size="small" @click="addQuickSSHUser(addUserForm.sshUsers, 'autogroup:nonroot')">autogroup:nonroot</el-button>
            </div>
            <div class="form-tip">{{ $t('ssh.sshUsersTip') }}</div>
          </div>
        </el-form-item>
        <el-alert type="warning" :closable="false">
          {{ $t('ssh.aclSyncWarning') }}
        </el-alert>
      </el-form>
      <template #footer>
        <el-button @click="addUserDialogVisible = false">{{ $t('common.cancel') }}</el-button>
        <el-button type="primary" @click="handleSubmitUserAuth" :loading="submitting">
          {{ $t('ssh.authorize') }}
        </el-button>
      </template>
    </el-dialog>

    <!-- 添加分组授权对话框 -->
    <el-dialog v-model="addGroupDialogVisible" :title="$t('ssh.addGroupAuth')" width="600px">
      <el-form :model="addGroupForm" label-width="100px">
        <el-form-item :label="$t('ssh.selectGroup')" required>
          <div class="selector-container">
            <el-input
              v-model="groupSearchText"
              :placeholder="$t('ssh.searchGroupPlaceholder')"
              clearable
              class="selector-search"
            >
              <template #prefix>
                <el-icon><Search /></el-icon>
              </template>
            </el-input>
            <div class="selector-list">
              <el-checkbox-group v-model="addGroupForm.groupIds">
                <div v-for="group in filteredGroupList" :key="group.id" class="selector-item">
                  <el-checkbox :value="group.id">
                    {{ group.name }} ({{ group.member_count || 0 }} {{ $t('ssh.members') }})
                  </el-checkbox>
                </div>
              </el-checkbox-group>
              <div v-if="filteredGroupList.length === 0" class="selector-empty">
                {{ $t('common.noData') }}
              </div>
            </div>
            <div class="selector-footer">
              {{ $t('ssh.selected') }}: {{ addGroupForm.groupIds.length }} {{ $t('ssh.groups') }}
            </div>
          </div>
        </el-form-item>
        <el-form-item :label="$t('ssh.sshUsers')" required>
          <div class="tag-input-container">
            <div class="tag-input-wrapper">
              <el-tag
                v-for="user in addGroupForm.sshUsers"
                :key="user"
                closable
                @close="removeSSHUser(addGroupForm.sshUsers, user)"
                class="ssh-user-tag"
              >
                {{ user }}
              </el-tag>
              <el-input
                v-model="sshUserInputForGroup"
                :placeholder="$t('ssh.sshUserInputPlaceholder')"
                class="tag-input"
                @keyup.enter="addSSHUserToList(addGroupForm.sshUsers, sshUserInputForGroup); sshUserInputForGroup = ''"
              />
            </div>
            <div class="quick-options">
              {{ $t('ssh.quickOptions') }}:
              <el-button size="small" @click="addQuickSSHUser(addGroupForm.sshUsers, 'root')">root</el-button>
              <el-button size="small" @click="addQuickSSHUser(addGroupForm.sshUsers, 'autogroup:nonroot')">autogroup:nonroot</el-button>
            </div>
            <div class="form-tip">{{ $t('ssh.sshUsersTip') }}</div>
          </div>
        </el-form-item>
        <el-alert type="warning" :closable="false">
          {{ $t('ssh.aclSyncWarning') }}
        </el-alert>
      </el-form>
      <template #footer>
        <el-button @click="addGroupDialogVisible = false">{{ $t('common.cancel') }}</el-button>
        <el-button type="primary" @click="handleSubmitGroupAuth" :loading="submitting">
          {{ $t('ssh.authorize') }}
        </el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, onMounted, computed } from 'vue'
import { useRoute } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Plus, Search } from '@element-plus/icons-vue'
import { useI18n } from 'vue-i18n'
import request from '@/utils/request'
import { getClients } from '@/api/client'
import {
  getAgentSSHPermissions,
  createClientPermission,
  createClientGroupPermission,
  deleteClientPermission,
  deleteClientGroupPermission
} from '@/api/ssh'

const { t } = useI18n()
const route = useRoute()

const agentId = Number(route.params.id)

// 数据
const agentInfo = ref<any>(null)
const clientPermissions = ref<any[]>([])
const groupPermissions = ref<any[]>([])
const clientList = ref<any[]>([])
const clientGroupList = ref<any[]>([])
const loading = ref(false)

// 搜索
const userSearchText = ref('')
const groupSearchText = ref('')
const sshUserInputForUser = ref('')
const sshUserInputForGroup = ref('')

// 对话框
const addUserDialogVisible = ref(false)
const addGroupDialogVisible = ref(false)
const submitting = ref(false)

const addUserForm = reactive({
  clientIds: [] as number[],
  sshUsers: ['root'] as string[]
})

const addGroupForm = reactive({
  groupIds: [] as number[],
  sshUsers: ['root'] as string[]
})

// 过滤列表
const filteredClientList = computed(() => {
  if (!userSearchText.value) return clientList.value
  const search = userSearchText.value.toLowerCase()
  return clientList.value.filter(c =>
    c.client_id?.toLowerCase().includes(search) ||
    c.tailscale_ip?.toLowerCase().includes(search)
  )
})

const filteredGroupList = computed(() => {
  if (!groupSearchText.value) return clientGroupList.value
  const search = groupSearchText.value.toLowerCase()
  return clientGroupList.value.filter(g => g.name.toLowerCase().includes(search))
})

// 加载数据
const loadData = async () => {
  loading.value = true
  try {
    const response = await getAgentSSHPermissions(agentId) as any
    if (response.success) {
      agentInfo.value = response.data?.agent
      clientPermissions.value = response.data?.client_permissions || []
      groupPermissions.value = response.data?.group_permissions || []
    }
  } catch (error) {
    ElMessage.error(t('ssh.loadPermissionsFailed'))
  } finally {
    loading.value = false
  }
}

const loadClients = async () => {
  try {
    const response = await getClients()
    if (response.data) {
      clientList.value = response.data
    }
  } catch (error) {
    console.error('Failed to load clients:', error)
  }
}

const loadClientGroups = async () => {
  try {
    const response = await request({
      url: '/api/v1/admin/client-groups',
      method: 'get'
    })
    if (response.success) {
      clientGroupList.value = response.data || []
    }
  } catch (error) {
    console.error('Failed to load client groups:', error)
  }
}

// SSH 用户操作
const addSSHUserToList = (list: string[], input: string) => {
  const trimmed = input.trim()
  if (trimmed && !list.includes(trimmed)) {
    list.push(trimmed)
  }
}

const removeSSHUser = (list: string[], user: string) => {
  const index = list.indexOf(user)
  if (index > -1) list.splice(index, 1)
}

const addQuickSSHUser = (list: string[], user: string) => {
  if (!list.includes(user)) list.push(user)
}

// 添加授权
const handleAddUser = () => {
  addUserForm.clientIds = []
  addUserForm.sshUsers = ['root']
  userSearchText.value = ''
  sshUserInputForUser.value = ''
  addUserDialogVisible.value = true
}

const handleAddGroup = () => {
  addGroupForm.groupIds = []
  addGroupForm.sshUsers = ['root']
  groupSearchText.value = ''
  sshUserInputForGroup.value = ''
  addGroupDialogVisible.value = true
}

const handleSubmitUserAuth = async () => {
  if (addUserForm.clientIds.length === 0) {
    ElMessage.warning(t('ssh.selectUserRequired'))
    return
  }
  if (addUserForm.sshUsers.length === 0) {
    ElMessage.warning(t('ssh.sshUsersRequired'))
    return
  }

  submitting.value = true
  try {
    const promises = addUserForm.clientIds.map(clientId =>
      createClientPermission({
        client_id: clientId,
        agent_id: agentId,
        ssh_users: addUserForm.sshUsers
      })
    )
    await Promise.all(promises)
    ElMessage.success(t('ssh.authSuccess'))
    addUserDialogVisible.value = false
    await loadData()
  } catch (error: any) {
    ElMessage.error(error.response?.data?.message || t('ssh.authFailed'))
  } finally {
    submitting.value = false
  }
}

const handleSubmitGroupAuth = async () => {
  if (addGroupForm.groupIds.length === 0) {
    ElMessage.warning(t('ssh.selectGroupRequired'))
    return
  }
  if (addGroupForm.sshUsers.length === 0) {
    ElMessage.warning(t('ssh.sshUsersRequired'))
    return
  }

  submitting.value = true
  try {
    const promises = addGroupForm.groupIds.map(groupId =>
      createClientGroupPermission({
        group_id: groupId,
        agent_id: agentId,
        ssh_users: addGroupForm.sshUsers
      })
    )
    await Promise.all(promises)
    ElMessage.success(t('ssh.authSuccess'))
    addGroupDialogVisible.value = false
    await loadData()
  } catch (error: any) {
    ElMessage.error(error.response?.data?.message || t('ssh.authFailed'))
  } finally {
    submitting.value = false
  }
}

// 撤销授权
const handleRevokeUserAuth = async (row: any) => {
  try {
    await ElMessageBox.confirm(t('ssh.revokeConfirm'), t('common.confirm'), { type: 'warning' })
    await deleteClientPermission(row.id)
    ElMessage.success(t('ssh.revokeSuccess'))
    await loadData()
  } catch (error: any) {
    if (error !== 'cancel') {
      ElMessage.error(error.response?.data?.message || t('ssh.revokeFailed'))
    }
  }
}

const handleRevokeGroupAuth = async (row: any) => {
  try {
    await ElMessageBox.confirm(t('ssh.revokeConfirm'), t('common.confirm'), { type: 'warning' })
    await deleteClientGroupPermission(row.id)
    ElMessage.success(t('ssh.revokeSuccess'))
    await loadData()
  } catch (error: any) {
    if (error !== 'cancel') {
      ElMessage.error(error.response?.data?.message || t('ssh.revokeFailed'))
    }
  }
}

const formatTime = (time: string) => {
  if (!time) return '-'
  return new Date(time).toLocaleString('zh-CN')
}

onMounted(async () => {
  await Promise.all([loadData(), loadClients(), loadClientGroups()])
})
</script>

<style scoped>
.info-card {
  margin-bottom: 16px;
}

.auth-card {
  margin-bottom: 16px;
}

.card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.empty-tip {
  text-align: center;
  color: #909399;
  padding: 20px;
}

/* 选择器样式 */
.selector-container {
  width: 100%;
  border: 1px solid #dcdfe6;
  border-radius: 4px;
}

.selector-search {
  border-bottom: 1px solid #ebeef5;
}

.selector-search :deep(.el-input__wrapper) {
  box-shadow: none;
}

.selector-list {
  max-height: 200px;
  overflow-y: auto;
  padding: 8px;
}

.selector-item {
  padding: 4px 0;
}

.selector-empty {
  text-align: center;
  color: #909399;
  padding: 20px;
}

.selector-footer {
  padding: 8px 12px;
  border-top: 1px solid #ebeef5;
  font-size: 12px;
  color: #909399;
}

/* 标签输入样式 */
.tag-input-container {
  width: 100%;
}

.tag-input-wrapper {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
  padding: 4px;
  border: 1px solid #dcdfe6;
  border-radius: 4px;
  min-height: 32px;
  align-items: center;
}

.ssh-user-tag {
  margin: 2px;
}

.tag-input {
  flex: 1;
  min-width: 120px;
}

.tag-input :deep(.el-input__wrapper) {
  box-shadow: none;
}

.quick-options {
  margin-top: 8px;
  font-size: 12px;
  color: #606266;
}

.quick-options .el-button {
  margin-left: 8px;
}

.form-tip {
  font-size: 12px;
  color: #909399;
  margin-top: 4px;
  line-height: 1.4;
}
</style>
