<template>
  <div class="ssh-list-container">
    <el-card>
      <template #header>
        <div class="card-header">
          <span>{{ $t('ssh.title') }}</span>
        </div>
      </template>

      <!-- 筛选区域 -->
      <div class="filter-bar">
        <el-input
          v-model="filters.search"
          :placeholder="$t('ssh.searchPlaceholder')"
          clearable
          style="width: 300px"
        >
          <template #prefix>
            <el-icon><Search /></el-icon>
          </template>
        </el-input>
      </div>

      <!-- Agent 列表 -->
      <el-table :data="filteredAgentList" v-loading="loading" stripe>
        <el-table-column type="index" label="#" width="60" :index="indexMethod" />
        <el-table-column prop="name" :label="$t('ssh.agentName')" min-width="180">
          <template #default="{ row }">
            <router-link :to="`/ssh/${row.id}`" class="agent-link">
              {{ row.alias ? `${row.alias} (${row.name})` : row.name }}
            </router-link>
          </template>
        </el-table-column>
        <el-table-column prop="tailscale_ip" :label="$t('ssh.agentIp')" width="140" />
        <el-table-column :label="$t('ssh.groupCount')" width="100" align="center">
          <template #default="{ row }">
            <el-button
              type="text"
              :class="row.client_group_count > 0 ? 'count-link' : 'count-zero'"
              @click="handleViewGroups(row)"
            >
              {{ row.client_group_count || 0 }}
            </el-button>
          </template>
        </el-table-column>
        <el-table-column :label="$t('ssh.userCount')" width="100" align="center">
          <template #default="{ row }">
            <el-button
              type="text"
              :class="row.client_count > 0 ? 'count-link' : 'count-zero'"
              @click="handleViewUsers(row)"
            >
              {{ row.client_count || 0 }}
            </el-button>
          </template>
        </el-table-column>
        <el-table-column :label="$t('common.actions')" width="200" fixed="right">
          <template #default="{ row }">
            <el-button type="primary" size="small" @click="handleAddGroup(row)">
              <el-icon><Plus /></el-icon>
              {{ $t('ssh.addGroup') }}
            </el-button>
            <el-button type="success" size="small" @click="handleAddUser(row)">
              <el-icon><Plus /></el-icon>
              {{ $t('ssh.addUser') }}
            </el-button>
          </template>
        </el-table-column>
      </el-table>

      <!-- 空状态 -->
      <div v-if="filteredAgentList.length === 0 && !loading" class="empty-state">
        <el-empty description="暂无启用 SSH 的 Agent">
          <el-button type="primary" @click="goToAgentManagement">前往代理管理</el-button>
        </el-empty>
      </div>

      <!-- 分页 -->
      <el-pagination
        v-model:current-page="pagination.page"
        v-model:page-size="pagination.pageSize"
        :total="pagination.total"
        :page-sizes="[10, 20, 50, 100]"
        layout="total, sizes, prev, pager, next, jumper"
        class="pagination"
      />
    </el-card>

    <!-- 添加分组授权对话框 -->
    <el-dialog v-model="addGroupDialogVisible" :title="$t('ssh.addGroupAuth')" width="600px">
      <div class="agent-info-box">
        <p><strong>{{ $t('ssh.agent') }}:</strong> {{ currentAgent?.alias || currentAgent?.name }}</p>
        <p><strong>{{ $t('ssh.agentIp') }}:</strong> {{ currentAgent?.tailscale_ip || '-' }}</p>
      </div>
      <el-form :model="addGroupForm" label-width="100px" style="margin-top: 20px">
        <el-form-item :label="$t('ssh.selectGroup')" required>
          <!-- 带搜索的分组选择器 -->
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
                <div
                  v-for="group in filteredGroupList"
                  :key="group.id"
                  class="selector-item"
                >
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
          <!-- SSH 用户标签输入 -->
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
                v-model="sshUserInput"
                :placeholder="$t('ssh.sshUserInputPlaceholder')"
                class="tag-input"
                @keyup.enter="addSSHUser(addGroupForm.sshUsers)"
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

    <!-- 添加用户授权对话框 -->
    <el-dialog v-model="addUserDialogVisible" :title="$t('ssh.addUserAuth')" width="600px">
      <div class="agent-info-box">
        <p><strong>{{ $t('ssh.agent') }}:</strong> {{ currentAgent?.alias || currentAgent?.name }}</p>
        <p><strong>{{ $t('ssh.agentIp') }}:</strong> {{ currentAgent?.tailscale_ip || '-' }}</p>
      </div>
      <el-form :model="addUserForm" label-width="100px" style="margin-top: 20px">
        <el-form-item :label="$t('ssh.selectUser')" required>
          <!-- 带搜索的用户选择器 -->
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
                <div
                  v-for="client in filteredClientList"
                  :key="client.id"
                  class="selector-item"
                >
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
          <!-- SSH 用户标签输入 -->
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
                @keyup.enter="addSSHUser(addUserForm.sshUsers, true)"
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

    <!-- 查看分组授权对话框 -->
    <el-dialog v-model="viewGroupsDialogVisible" :title="$t('ssh.groupAuthList')" width="800px">
      <div class="dialog-header">
        <span>{{ $t('ssh.agent') }}: {{ currentAgent?.alias || currentAgent?.name }}</span>
        <el-button type="primary" size="small" @click="handleAddGroupFromView">
          <el-icon><Plus /></el-icon>
          {{ $t('ssh.addGroup') }}
        </el-button>
      </div>
      <el-table :data="groupAuthList" v-loading="loadingGroupAuth" border>
        <el-table-column prop="id" label="ID" width="80" />
        <el-table-column prop="group_name" :label="$t('ssh.groupName')" min-width="120" />
        <el-table-column :label="$t('ssh.memberCount')" width="100">
          <template #default="{ row }">
            {{ row.member_count || 0 }}
          </template>
        </el-table-column>
        <el-table-column :label="$t('ssh.sshUsers')" min-width="180">
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
    </el-dialog>

    <!-- 查看用户授权对话框 -->
    <el-dialog v-model="viewUsersDialogVisible" :title="$t('ssh.userAuthList')" width="800px">
      <div class="dialog-header">
        <span>{{ $t('ssh.agent') }}: {{ currentAgent?.alias || currentAgent?.name }}</span>
        <el-button type="primary" size="small" @click="handleAddUserFromView">
          <el-icon><Plus /></el-icon>
          {{ $t('ssh.addUser') }}
        </el-button>
      </div>
      <el-table :data="userAuthList" v-loading="loadingUserAuth" border>
        <el-table-column prop="id" label="ID" width="80" />
        <el-table-column prop="client_name" :label="$t('ssh.userName')" min-width="120" />
        <el-table-column prop="client_ip" :label="$t('ssh.userIp')" width="130" />
        <el-table-column :label="$t('ssh.sshUsers')" min-width="180">
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
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, onMounted, computed } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Plus, Search } from '@element-plus/icons-vue'
import { useI18n } from 'vue-i18n'
import request from '@/utils/request'
import { getClients } from '@/api/client'
import {
  getAgentSSHStats,
  getAgentSSHPermissions,
  createClientPermission,
  createClientGroupPermission,
  deleteClientPermission,
  deleteClientGroupPermission,
  type AgentSSHStats
} from '@/api/ssh'

const { t } = useI18n()
const router = useRouter()

// 筛选条件
const filters = reactive({
  search: ''
})

// 分页
const pagination = reactive({
  page: 1,
  pageSize: 20,
  total: 0
})

// 数据列表
const agentList = ref<AgentSSHStats[]>([])
const clientList = ref<any[]>([])
const clientGroupList = ref<any[]>([])
const loading = ref(false)

// 当前操作的 Agent
const currentAgent = ref<AgentSSHStats | null>(null)

// 搜索文本
const groupSearchText = ref('')
const userSearchText = ref('')
const sshUserInput = ref('')
const sshUserInputForUser = ref('')

// 添加分组授权对话框
const addGroupDialogVisible = ref(false)
const addGroupForm = reactive({
  groupIds: [] as number[],
  sshUsers: ['root'] as string[]
})

// 添加用户授权对话框
const addUserDialogVisible = ref(false)
const addUserForm = reactive({
  clientIds: [] as number[],
  sshUsers: ['root'] as string[]
})

const submitting = ref(false)

// 查看分组授权对话框
const viewGroupsDialogVisible = ref(false)
const groupAuthList = ref<any[]>([])
const loadingGroupAuth = ref(false)

// 查看用户授权对话框
const viewUsersDialogVisible = ref(false)
const userAuthList = ref<any[]>([])
const loadingUserAuth = ref(false)

// 过滤后的 Agent 列表
const filteredAgentList = computed(() => {
  let list = agentList.value
  if (filters.search) {
    const search = filters.search.toLowerCase()
    list = list.filter(a =>
      a.name.toLowerCase().includes(search) ||
      a.alias?.toLowerCase().includes(search) ||
      a.tailscale_ip?.toLowerCase().includes(search)
    )
  }
  return list
})

// 过滤后的分组列表
const filteredGroupList = computed(() => {
  if (!groupSearchText.value) return clientGroupList.value
  const search = groupSearchText.value.toLowerCase()
  return clientGroupList.value.filter(g => g.name.toLowerCase().includes(search))
})

// 过滤后的用户列表
const filteredClientList = computed(() => {
  if (!userSearchText.value) return clientList.value
  const search = userSearchText.value.toLowerCase()
  return clientList.value.filter(c =>
    c.client_id?.toLowerCase().includes(search) ||
    c.tailscale_ip?.toLowerCase().includes(search)
  )
})

// 加载 Agent SSH 统计
const loadAgents = async () => {
  loading.value = true
  try {
    const response = await getAgentSSHStats()
    if (response.success) {
      agentList.value = response.data || []
      pagination.total = response.data?.length || 0
    }
  } catch (error) {
    ElMessage.error(t('ssh.loadAgentsFailed'))
  } finally {
    loading.value = false
  }
}

// 加载客户端列表
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

// 加载客户端分组列表
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
const addSSHUser = (list: string[], isUserForm = false) => {
  const input = isUserForm ? sshUserInputForUser.value : sshUserInput.value
  const trimmed = input.trim()
  if (trimmed && !list.includes(trimmed)) {
    list.push(trimmed)
  }
  if (isUserForm) {
    sshUserInputForUser.value = ''
  } else {
    sshUserInput.value = ''
  }
}

const removeSSHUser = (list: string[], user: string) => {
  const index = list.indexOf(user)
  if (index > -1) {
    list.splice(index, 1)
  }
}

const addQuickSSHUser = (list: string[], user: string) => {
  if (!list.includes(user)) {
    list.push(user)
  }
}

// 添加分组授权
const handleAddGroup = (row: AgentSSHStats) => {
  currentAgent.value = row
  addGroupForm.groupIds = []
  addGroupForm.sshUsers = ['root']
  groupSearchText.value = ''
  addGroupDialogVisible.value = true
}

// 添加用户授权
const handleAddUser = (row: AgentSSHStats) => {
  currentAgent.value = row
  addUserForm.clientIds = []
  addUserForm.sshUsers = ['root']
  userSearchText.value = ''
  addUserDialogVisible.value = true
}

// 前往代理管理
const goToAgentManagement = () => {
  router.push('/agents')
}

// 提交分组授权
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
        agent_id: currentAgent.value!.id,
        ssh_users: addGroupForm.sshUsers
      })
    )
    await Promise.all(promises)
    ElMessage.success(t('ssh.authSuccess'))
    addGroupDialogVisible.value = false
    await loadAgents()
  } catch (error: any) {
    ElMessage.error(error.response?.data?.message || t('ssh.authFailed'))
  } finally {
    submitting.value = false
  }
}

// 提交用户授权
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
        agent_id: currentAgent.value!.id,
        ssh_users: addUserForm.sshUsers
      })
    )
    await Promise.all(promises)
    ElMessage.success(t('ssh.authSuccess'))
    addUserDialogVisible.value = false
    await loadAgents()
  } catch (error: any) {
    ElMessage.error(error.response?.data?.message || t('ssh.authFailed'))
  } finally {
    submitting.value = false
  }
}

// 查看分组授权
const handleViewGroups = async (row: AgentSSHStats) => {
  currentAgent.value = row
  viewGroupsDialogVisible.value = true
  loadingGroupAuth.value = true
  try {
    const response = await getAgentSSHPermissions(row.id)
    if (response.success) {
      groupAuthList.value = response.data?.group_permissions || []
    }
  } catch (error) {
    ElMessage.error(t('ssh.loadPermissionsFailed'))
  } finally {
    loadingGroupAuth.value = false
  }
}

// 查看用户授权
const handleViewUsers = async (row: AgentSSHStats) => {
  currentAgent.value = row
  viewUsersDialogVisible.value = true
  loadingUserAuth.value = true
  try {
    const response = await getAgentSSHPermissions(row.id)
    if (response.success) {
      userAuthList.value = response.data?.client_permissions || []
    }
  } catch (error) {
    ElMessage.error(t('ssh.loadPermissionsFailed'))
  } finally {
    loadingUserAuth.value = false
  }
}

// 从查看对话框添加
const handleAddGroupFromView = () => {
  viewGroupsDialogVisible.value = false
  handleAddGroup(currentAgent.value!)
}

const handleAddUserFromView = () => {
  viewUsersDialogVisible.value = false
  handleAddUser(currentAgent.value!)
}

// 撤销授权
const handleRevokeGroupAuth = async (row: any) => {
  try {
    await ElMessageBox.confirm(t('ssh.revokeConfirm'), t('common.confirm'), { type: 'warning' })
    await deleteClientGroupPermission(row.id)
    ElMessage.success(t('ssh.revokeSuccess'))
    await handleViewGroups(currentAgent.value!)
    await loadAgents()
  } catch (error: any) {
    if (error !== 'cancel') {
      ElMessage.error(error.response?.data?.message || t('ssh.revokeFailed'))
    }
  }
}

const handleRevokeUserAuth = async (row: any) => {
  try {
    await ElMessageBox.confirm(t('ssh.revokeConfirm'), t('common.confirm'), { type: 'warning' })
    await deleteClientPermission(row.id)
    ElMessage.success(t('ssh.revokeSuccess'))
    await handleViewUsers(currentAgent.value!)
    await loadAgents()
  } catch (error: any) {
    if (error !== 'cancel') {
      ElMessage.error(error.response?.data?.message || t('ssh.revokeFailed'))
    }
  }
}

// 格式化时间
const formatTime = (time: string) => {
  if (!time) return '-'
  return new Date(time).toLocaleString('zh-CN')
}

// 计算序号
const indexMethod = (index: number) => {
  return (pagination.page - 1) * pagination.pageSize + index + 1
}

onMounted(async () => {
  await Promise.all([loadAgents(), loadClients(), loadClientGroups()])
})
</script>

<style scoped>
.card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.filter-bar {
  margin-bottom: 16px;
}

.pagination {
  margin-top: 20px;
  display: flex;
  justify-content: flex-end;
}

.agent-link {
  color: #409eff;
  text-decoration: none;
}

.agent-link:hover {
  text-decoration: underline;
}

.count-link {
  color: #409eff;
  font-weight: 500;
  padding: 0;
}

.count-link:hover {
  color: #66b1ff;
}

.count-zero {
  color: #909399;
  padding: 0;
  cursor: default;
}

.empty-state {
  padding: 40px 0;
  text-align: center;
}

.agent-info-box {
  background: #f5f7fa;
  padding: 12px;
  border-radius: 4px;
}

.agent-info-box p {
  margin: 4px 0;
}

.dialog-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 16px;
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
