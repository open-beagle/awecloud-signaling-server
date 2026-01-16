<template>
  <div class="agent-auth-container">
    <el-card>
      <template #header>
        <div class="card-header">
          <span>{{ $t('agentLevelAuth.desktopTitle') }}</span>
        </div>
      </template>

      <!-- 筛选区域 -->
      <div class="filter-bar">
        <el-input
          v-model="filters.search"
          :placeholder="$t('agentLevelAuth.searchPlaceholder')"
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
        <el-table-column type="index" label="序号" width="80" :index="indexMethod" />
        <el-table-column prop="name" :label="$t('agentLevelAuth.agentName')" min-width="150">
          <template #default="{ row }">
            {{ row.alias ? `${row.alias} (${row.name})` : row.name }}
          </template>
        </el-table-column>
        <el-table-column prop="tailscale_ip" :label="$t('agentLevelAuth.agentIp')" width="140" />
        <el-table-column :label="$t('agentLevelAuth.serviceCount')" width="100" align="center">
          <template #default="{ row }">
            {{ row.service_count || 0 }}
          </template>
        </el-table-column>
        <el-table-column :label="$t('agentLevelAuth.groupCount')" width="100" align="center">
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
        <el-table-column :label="$t('agentLevelAuth.userCount')" width="100" align="center">
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
              {{ $t('agentLevelAuth.addGroup') }}
            </el-button>
            <el-button type="success" size="small" @click="handleAddUser(row)">
              <el-icon><Plus /></el-icon>
              {{ $t('agentLevelAuth.addUser') }}
            </el-button>
          </template>
        </el-table-column>
      </el-table>

      <!-- 分页 -->
      <el-pagination
        v-model:current-page="pagination.page"
        v-model:page-size="pagination.pageSize"
        :total="pagination.total"
        :page-sizes="[10, 20, 50, 100]"
        layout="total, sizes, prev, pager, next, jumper"
        @size-change="loadAgents"
        @current-change="loadAgents"
        class="pagination"
      />
    </el-card>

    <!-- 添加分组授权对话框 -->
    <el-dialog v-model="addGroupDialogVisible" :title="$t('agentLevelAuth.addGroupAuth')" width="600px">
      <div class="agent-info-box">
        <p><strong>{{ $t('agentLevelAuth.agent') }}:</strong> {{ currentAgent?.alias || currentAgent?.name }}</p>
        <p><strong>{{ $t('agentLevelAuth.agentIp') }}:</strong> {{ currentAgent?.tailscale_ip || '-' }}</p>
        <p><strong>{{ $t('agentLevelAuth.serviceCount') }}:</strong> {{ currentAgent?.service_count || 0 }}</p>
      </div>
      <el-form :model="addGroupForm" label-width="120px" style="margin-top: 20px">
        <el-form-item :label="$t('agentLevelAuth.selectGroup')" required>
          <el-select
            v-model="addGroupForm.groupIds"
            :placeholder="$t('agentLevelAuth.selectGroupPlaceholder')"
            multiple
            style="width: 100%"
          >
            <el-option
              v-for="group in clientGroupList"
              :key="group.id"
              :label="`${group.name} (${group.member_count || 0} ${$t('agentLevelAuth.members')})`"
              :value="group.id"
            />
          </el-select>
        </el-form-item>
        <el-alert type="info" :closable="false">
          {{ $t('agentLevelAuth.agentAuthTip') }}
        </el-alert>
        <el-alert type="warning" :closable="false" style="margin-top: 10px">
          {{ $t('agentLevelAuth.aclSyncWarning') }}
        </el-alert>
      </el-form>
      <template #footer>
        <el-button @click="addGroupDialogVisible = false">{{ $t('common.cancel') }}</el-button>
        <el-button type="primary" @click="handleSubmitGroupAuth" :loading="submitting">
          {{ $t('agentLevelAuth.authorize') }}
        </el-button>
      </template>
    </el-dialog>

    <!-- 添加用户授权对话框 -->
    <el-dialog v-model="addUserDialogVisible" :title="$t('agentLevelAuth.addUserAuth')" width="600px">
      <div class="agent-info-box">
        <p><strong>{{ $t('agentLevelAuth.agent') }}:</strong> {{ currentAgent?.alias || currentAgent?.name }}</p>
        <p><strong>{{ $t('agentLevelAuth.agentIp') }}:</strong> {{ currentAgent?.tailscale_ip || '-' }}</p>
        <p><strong>{{ $t('agentLevelAuth.serviceCount') }}:</strong> {{ currentAgent?.service_count || 0 }}</p>
      </div>
      <el-form :model="addUserForm" label-width="120px" style="margin-top: 20px">
        <el-form-item :label="$t('agentLevelAuth.selectUser')" required>
          <el-select
            v-model="addUserForm.clientIds"
            :placeholder="$t('agentLevelAuth.selectUserPlaceholder')"
            multiple
            filterable
            style="width: 100%"
          >
            <el-option
              v-for="client in clientList"
              :key="client.id"
              :label="`${client.client_id} (${client.tailscale_ip || '-'})`"
              :value="client.id"
            />
          </el-select>
        </el-form-item>
        <el-alert type="info" :closable="false">
          {{ $t('agentLevelAuth.agentAuthTip') }}
        </el-alert>
        <el-alert type="warning" :closable="false" style="margin-top: 10px">
          {{ $t('agentLevelAuth.aclSyncWarning') }}
        </el-alert>
      </el-form>
      <template #footer>
        <el-button @click="addUserDialogVisible = false">{{ $t('common.cancel') }}</el-button>
        <el-button type="primary" @click="handleSubmitUserAuth" :loading="submitting">
          {{ $t('agentLevelAuth.authorize') }}
        </el-button>
      </template>
    </el-dialog>

    <!-- 查看分组授权对话框 -->
    <el-dialog v-model="viewGroupsDialogVisible" :title="$t('agentLevelAuth.groupAuthList')" width="800px">
      <div class="dialog-header">
        <span>{{ $t('agentLevelAuth.agent') }}: {{ currentAgent?.alias || currentAgent?.name }}</span>
        <el-button type="primary" size="small" @click="handleAddGroupFromView">
          <el-icon><Plus /></el-icon>
          {{ $t('agentLevelAuth.addGroup') }}
        </el-button>
      </div>
      <el-table :data="groupAuthList" v-loading="loadingGroupAuth" border>
        <el-table-column prop="id" label="ID" width="80" />
        <el-table-column prop="name" :label="$t('agentLevelAuth.groupName')" min-width="150">
          <template #default="{ row }">
            {{ row.alias ? `${row.alias} (${row.name})` : row.name }}
          </template>
        </el-table-column>
        <el-table-column :label="$t('agentLevelAuth.memberCount')" width="120">
          <template #default="{ row }">
            {{ row.member_count || 0 }} {{ $t('agentLevelAuth.members') }}
          </template>
        </el-table-column>
        <el-table-column :label="$t('agentLevelAuth.grantedAt')" width="180">
          <template #default="{ row }">
            {{ formatTime(row.granted_at) }}
          </template>
        </el-table-column>
        <el-table-column :label="$t('common.actions')" width="100" fixed="right">
          <template #default="{ row }">
            <el-button type="danger" size="small" @click="handleRevokeGroupAuth(row)">
              {{ $t('agentLevelAuth.revoke') }}
            </el-button>
          </template>
        </el-table-column>
      </el-table>
    </el-dialog>

    <!-- 查看用户授权对话框 -->
    <el-dialog v-model="viewUsersDialogVisible" :title="$t('agentLevelAuth.userAuthList')" width="800px">
      <div class="dialog-header">
        <span>{{ $t('agentLevelAuth.agent') }}: {{ currentAgent?.alias || currentAgent?.name }}</span>
        <el-button type="primary" size="small" @click="handleAddUserFromView">
          <el-icon><Plus /></el-icon>
          {{ $t('agentLevelAuth.addUser') }}
        </el-button>
      </div>
      <el-table :data="userAuthList" v-loading="loadingUserAuth" border>
        <el-table-column prop="id" label="ID" width="80" />
        <el-table-column prop="client_name" :label="$t('agentLevelAuth.userName')" min-width="150" />
        <el-table-column prop="client_ip" :label="$t('agentLevelAuth.userIp')" width="140" />
        <el-table-column :label="$t('agentLevelAuth.grantedAt')" width="180">
          <template #default="{ row }">
            {{ formatTime(row.granted_at) }}
          </template>
        </el-table-column>
        <el-table-column :label="$t('common.actions')" width="100" fixed="right">
          <template #default="{ row }">
            <el-button type="danger" size="small" @click="handleRevokeUserAuth(row)">
              {{ $t('agentLevelAuth.revoke') }}
            </el-button>
          </template>
        </el-table-column>
      </el-table>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, onMounted, computed } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Plus, Search } from '@element-plus/icons-vue'
import { useI18n } from 'vue-i18n'
import request from '@/utils/request'
import { getClients } from '@/api/client'

const { t } = useI18n()

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
const agentList = ref<any[]>([])
const clientList = ref<any[]>([])
const clientGroupList = ref<any[]>([])
const loading = ref(false)

// 当前操作的 Agent
const currentAgent = ref<any>(null)

// 添加分组授权对话框
const addGroupDialogVisible = ref(false)
const addGroupForm = reactive({
  groupIds: [] as number[]
})

// 添加用户授权对话框
const addUserDialogVisible = ref(false)
const addUserForm = reactive({
  clientIds: [] as number[]
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

// 加载 Agent 列表（带授权统计）
const loadAgents = async () => {
  loading.value = true
  try {
    const response = await request({
      url: '/api/v1/admin/agents/auth-stats',
      method: 'get',
      params: {
        page: pagination.page,
        size: pagination.pageSize,
        type: 'client' // 桌面授权
      }
    })

    if (response.success) {
      agentList.value = response.data || []
      pagination.total = response.total || 0
    }
  } catch (error) {
    // 如果新接口不存在，回退到普通接口
    try {
      const response = await request({
        url: '/api/v1/admin/agents',
        method: 'get'
      })
      if (response.success) {
        agentList.value = response.data || []
        pagination.total = response.data?.length || 0
      }
    } catch (e) {
      ElMessage.error(t('agentLevelAuth.loadAgentsFailed'))
    }
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

// 添加分组授权
const handleAddGroup = (row: any) => {
  currentAgent.value = row
  addGroupForm.groupIds = []
  addGroupDialogVisible.value = true
}

// 添加用户授权
const handleAddUser = (row: any) => {
  currentAgent.value = row
  addUserForm.clientIds = []
  addUserDialogVisible.value = true
}

// 提交分组授权
const handleSubmitGroupAuth = async () => {
  if (addGroupForm.groupIds.length === 0) {
    ElMessage.warning(t('agentLevelAuth.selectGroupRequired'))
    return
  }

  submitting.value = true
  try {
    const promises = addGroupForm.groupIds.map(groupId =>
      request({
        url: `/api/v1/admin/agents/${currentAgent.value.id}/client-group-permissions`,
        method: 'post',
        data: { group_id: groupId }
      })
    )

    await Promise.all(promises)

    ElMessage.success(t('agentLevelAuth.authSuccess'))
    addGroupDialogVisible.value = false
    await loadAgents()
  } catch (error: any) {
    if (error.response?.data?.message?.includes('ACL') || error.response?.data?.message?.includes('Headscale')) {
      ElMessage.error(t('agentLevelAuth.aclSyncError'))
    } else {
      ElMessage.error(error.response?.data?.message || t('agentLevelAuth.authFailed'))
    }
  } finally {
    submitting.value = false
  }
}

// 提交用户授权
const handleSubmitUserAuth = async () => {
  if (addUserForm.clientIds.length === 0) {
    ElMessage.warning(t('agentLevelAuth.selectUserRequired'))
    return
  }

  submitting.value = true
  try {
    const promises = addUserForm.clientIds.map(clientId =>
      request({
        url: `/api/v1/admin/agents/${currentAgent.value.id}/client-permissions`,
        method: 'post',
        data: { client_id: clientId }
      })
    )

    await Promise.all(promises)

    ElMessage.success(t('agentLevelAuth.authSuccess'))
    addUserDialogVisible.value = false
    await loadAgents()
  } catch (error: any) {
    if (error.response?.data?.message?.includes('ACL') || error.response?.data?.message?.includes('Headscale')) {
      ElMessage.error(t('agentLevelAuth.aclSyncError'))
    } else {
      ElMessage.error(error.response?.data?.message || t('agentLevelAuth.authFailed'))
    }
  } finally {
    submitting.value = false
  }
}

// 查看分组授权
const handleViewGroups = async (row: any) => {
  currentAgent.value = row
  viewGroupsDialogVisible.value = true
  loadingGroupAuth.value = true

  try {
    const response = await request({
      url: `/api/v1/admin/agents/${row.id}/client-group-permissions`,
      method: 'get'
    })
    if (response.success) {
      groupAuthList.value = response.data || []
    }
  } catch (error) {
    ElMessage.error(t('agentLevelAuth.loadPermissionsFailed'))
  } finally {
    loadingGroupAuth.value = false
  }
}

// 查看用户授权
const handleViewUsers = async (row: any) => {
  currentAgent.value = row
  viewUsersDialogVisible.value = true
  loadingUserAuth.value = true

  try {
    const response = await request({
      url: `/api/v1/admin/agents/${row.id}/client-permissions`,
      method: 'get'
    })
    if (response.success) {
      userAuthList.value = response.data || []
    }
  } catch (error) {
    ElMessage.error(t('agentLevelAuth.loadPermissionsFailed'))
  } finally {
    loadingUserAuth.value = false
  }
}

// 从查看对话框添加分组
const handleAddGroupFromView = () => {
  viewGroupsDialogVisible.value = false
  addGroupForm.groupIds = []
  addGroupDialogVisible.value = true
}

// 从查看对话框添加用户
const handleAddUserFromView = () => {
  viewUsersDialogVisible.value = false
  addUserForm.clientIds = []
  addUserDialogVisible.value = true
}

// 撤销分组授权
const handleRevokeGroupAuth = async (row: any) => {
  try {
    await ElMessageBox.confirm(t('agentLevelAuth.revokeConfirm'), t('common.confirm'), {
      type: 'warning'
    })

    await request({
      url: `/api/v1/admin/agents/${currentAgent.value.id}/client-group-permissions/${row.id}`,
      method: 'delete'
    })

    ElMessage.success(t('agentLevelAuth.revokeSuccess'))
    await handleViewGroups(currentAgent.value)
    await loadAgents()
  } catch (error: any) {
    if (error !== 'cancel') {
      if (error.response?.data?.message?.includes('ACL') || error.response?.data?.message?.includes('Headscale')) {
        ElMessage.error(t('agentLevelAuth.aclSyncError'))
      } else {
        ElMessage.error(error.response?.data?.message || t('agentLevelAuth.revokeFailed'))
      }
    }
  }
}

// 撤销用户授权
const handleRevokeUserAuth = async (row: any) => {
  try {
    await ElMessageBox.confirm(t('agentLevelAuth.revokeConfirm'), t('common.confirm'), {
      type: 'warning'
    })

    await request({
      url: `/api/v1/admin/agents/${currentAgent.value.id}/client-permissions/${row.id}`,
      method: 'delete'
    })

    ElMessage.success(t('agentLevelAuth.revokeSuccess'))
    await handleViewUsers(currentAgent.value)
    await loadAgents()
  } catch (error: any) {
    if (error !== 'cancel') {
      if (error.response?.data?.message?.includes('ACL') || error.response?.data?.message?.includes('Headscale')) {
        ElMessage.error(t('agentLevelAuth.aclSyncError'))
      } else {
        ElMessage.error(error.response?.data?.message || t('agentLevelAuth.revokeFailed'))
      }
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
  await Promise.all([
    loadAgents(),
    loadClients(),
    loadClientGroups()
  ])
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
  display: flex;
  gap: 12px;
}

.pagination {
  margin-top: 20px;
  display: flex;
  justify-content: flex-end;
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

.agent-info-box {
  background: #f5f7fa;
  padding: 12px;
  border-radius: 4px;
  margin-bottom: 16px;
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
</style>
