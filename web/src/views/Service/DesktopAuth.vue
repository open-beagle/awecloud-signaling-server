<template>
  <div class="desktop-auth-container">
    <el-card>
      <template #header>
        <div class="card-header">
          <span>{{ $t('desktopAuth.title') }}</span>
        </div>
      </template>

      <!-- 筛选区域 -->
      <div class="filter-bar">
        <el-select v-model="filters.agentId" :placeholder="$t('desktopAuth.filterByAgent')" clearable style="width: 200px" @change="loadServices">
          <el-option :label="$t('desktopAuth.allAgents')" :value="null" />
          <el-option
            v-for="agent in agentList"
            :key="agent.id"
            :label="agent.name"
            :value="agent.id"
          />
        </el-select>
        <el-input
          v-model="filters.search"
          :placeholder="$t('desktopAuth.searchPlaceholder')"
          clearable
          style="width: 300px"
          @input="handleSearch"
        >
          <template #prefix>
            <el-icon><Search /></el-icon>
          </template>
        </el-input>
      </div>

      <!-- 服务列表 -->
      <el-table :data="filteredServiceList" v-loading="loading" stripe>
        <el-table-column type="index" label="序号" width="80" :index="indexMethod" />
        <el-table-column prop="name" :label="$t('desktopAuth.serviceName')" min-width="150" />
        <el-table-column prop="agent_name" :label="$t('desktopAuth.agent')" width="120" />
        <el-table-column :label="$t('desktopAuth.serviceAddress')" min-width="180">
          <template #default="{ row }">
            <span v-if="row.listen_addr">{{ row.listen_addr }}</span>
            <span v-else class="text-muted">-</span>
          </template>
        </el-table-column>
        <el-table-column :label="$t('desktopAuth.groupCount')" width="100" align="center">
          <template #default="{ row }">
            <el-button
              type="text"
              :class="row.group_count > 0 ? 'count-link' : 'count-zero'"
              @click="handleViewGroups(row)"
            >
              {{ row.group_count }}
            </el-button>
          </template>
        </el-table-column>
        <el-table-column :label="$t('desktopAuth.userCount')" width="100" align="center">
          <template #default="{ row }">
            <el-button
              type="text"
              :class="row.client_count > 0 ? 'count-link' : 'count-zero'"
              @click="handleViewUsers(row)"
            >
              {{ row.client_count }}
            </el-button>
          </template>
        </el-table-column>
        <el-table-column :label="$t('common.actions')" width="200" fixed="right">
          <template #default="{ row }">
            <el-button type="primary" size="small" @click="handleAddGroup(row)">
              <el-icon><Plus /></el-icon>
              {{ $t('desktopAuth.addGroup') }}
            </el-button>
            <el-button type="success" size="small" @click="handleAddUser(row)">
              <el-icon><Plus /></el-icon>
              {{ $t('desktopAuth.addUser') }}
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
        @size-change="loadServices"
        @current-change="loadServices"
        class="pagination"
      />
    </el-card>

    <!-- 添加分组授权对话框 -->
    <el-dialog v-model="addGroupDialogVisible" :title="$t('desktopAuth.addGroupAuth')" width="600px">
      <div class="service-info-box">
        <p><strong>{{ $t('desktopAuth.service') }}:</strong> {{ currentService?.name }}</p>
        <p><strong>{{ $t('desktopAuth.agent') }}:</strong> {{ currentService?.agent_name }}</p>
        <p><strong>{{ $t('desktopAuth.serviceAddress') }}:</strong> {{ currentService?.listen_addr }}</p>
      </div>
      <el-form :model="addGroupForm" label-width="120px" style="margin-top: 20px">
        <el-form-item :label="$t('desktopAuth.selectGroup')" required>
          <el-select
            v-model="addGroupForm.groupIds"
            :placeholder="$t('desktopAuth.selectGroupPlaceholder')"
            multiple
            style="width: 100%"
          >
            <el-option
              v-for="group in clientGroupList"
              :key="group.id"
              :label="`${group.name} (${group.member_count || 0} ${$t('desktopAuth.members')})`"
              :value="group.id"
            />
          </el-select>
        </el-form-item>
        <el-alert type="warning" :closable="false">
          {{ $t('desktopAuth.aclSyncWarning') }}
        </el-alert>
      </el-form>
      <template #footer>
        <el-button @click="addGroupDialogVisible = false">{{ $t('common.cancel') }}</el-button>
        <el-button type="primary" @click="handleSubmitGroupAuth" :loading="submitting">
          {{ $t('desktopAuth.authorize') }}
        </el-button>
      </template>
    </el-dialog>

    <!-- 添加用户授权对话框 -->
    <el-dialog v-model="addUserDialogVisible" :title="$t('desktopAuth.addUserAuth')" width="600px">
      <div class="service-info-box">
        <p><strong>{{ $t('desktopAuth.service') }}:</strong> {{ currentService?.name }}</p>
        <p><strong>{{ $t('desktopAuth.agent') }}:</strong> {{ currentService?.agent_name }}</p>
        <p><strong>{{ $t('desktopAuth.serviceAddress') }}:</strong> {{ currentService?.listen_addr }}</p>
      </div>
      <el-form :model="addUserForm" label-width="120px" style="margin-top: 20px">
        <el-form-item :label="$t('desktopAuth.selectUser')" required>
          <el-select
            v-model="addUserForm.clientIds"
            :placeholder="$t('desktopAuth.selectUserPlaceholder')"
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
        <el-alert type="warning" :closable="false">
          {{ $t('desktopAuth.aclSyncWarning') }}
        </el-alert>
      </el-form>
      <template #footer>
        <el-button @click="addUserDialogVisible = false">{{ $t('common.cancel') }}</el-button>
        <el-button type="primary" @click="handleSubmitUserAuth" :loading="submitting">
          {{ $t('desktopAuth.authorize') }}
        </el-button>
      </template>
    </el-dialog>

    <!-- 查看分组授权对话框 -->
    <el-dialog v-model="viewGroupsDialogVisible" :title="$t('desktopAuth.groupAuthList')" width="800px">
      <div class="dialog-header">
        <span>{{ $t('desktopAuth.service') }}: {{ currentService?.name }}</span>
        <el-button type="primary" size="small" @click="handleAddGroupFromView">
          <el-icon><Plus /></el-icon>
          {{ $t('desktopAuth.addGroup') }}
        </el-button>
      </div>
      <el-table :data="groupAuthList" v-loading="loadingGroupAuth" border>
        <el-table-column prop="id" label="ID" width="80" />
        <el-table-column prop="name" :label="$t('desktopAuth.groupName')" min-width="150">
          <template #default="{ row }">
            {{ row.alias ? `${row.alias} (${row.name})` : row.name }}
          </template>
        </el-table-column>
        <el-table-column :label="$t('desktopAuth.memberCount')" width="120">
          <template #default="{ row }">
            {{ row.member_count || 0 }} {{ $t('desktopAuth.members') }}
          </template>
        </el-table-column>
        <el-table-column :label="$t('desktopAuth.grantedAt')" width="180">
          <template #default="{ row }">
            {{ formatTime(row.granted_at) }}
          </template>
        </el-table-column>
        <el-table-column :label="$t('common.actions')" width="100" fixed="right">
          <template #default="{ row }">
            <el-button type="danger" size="small" @click="handleRevokeGroupAuth(row)">
              {{ $t('desktopAuth.revoke') }}
            </el-button>
          </template>
        </el-table-column>
      </el-table>
    </el-dialog>

    <!-- 查看用户授权对话框 -->
    <el-dialog v-model="viewUsersDialogVisible" :title="$t('desktopAuth.userAuthList')" width="800px">
      <div class="dialog-header">
        <span>{{ $t('desktopAuth.service') }}: {{ currentService?.name }}</span>
        <el-button type="primary" size="small" @click="handleAddUserFromView">
          <el-icon><Plus /></el-icon>
          {{ $t('desktopAuth.addUser') }}
        </el-button>
      </div>
      <el-table :data="userAuthList" v-loading="loadingUserAuth" border>
        <el-table-column prop="id" label="ID" width="80" />
        <el-table-column prop="client_name" :label="$t('desktopAuth.userName')" min-width="150" />
        <el-table-column prop="client_ip" :label="$t('desktopAuth.userIp')" width="140" />
        <el-table-column :label="$t('desktopAuth.grantedAt')" width="180">
          <template #default="{ row }">
            {{ formatTime(row.granted_at) }}
          </template>
        </el-table-column>
        <el-table-column :label="$t('common.actions')" width="100" fixed="right">
          <template #default="{ row }">
            <el-button type="danger" size="small" @click="handleRevokeUserAuth(row)">
              {{ $t('desktopAuth.revoke') }}
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
import { getAgents } from '@/api/agent'
import { getClients } from '@/api/client'

const { t } = useI18n()

// 筛选条件
const filters = reactive({
  agentId: null as number | null,
  search: ''
})

// 分页
const pagination = reactive({
  page: 1,
  pageSize: 20,
  total: 0
})

// 数据列表
const serviceList = ref<any[]>([])
const agentList = ref<any[]>([])
const clientList = ref<any[]>([])
const clientGroupList = ref<any[]>([])
const loading = ref(false)

// 当前操作的服务
const currentService = ref<any>(null)

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

// 过滤后的服务列表
const filteredServiceList = computed(() => {
  let list = serviceList.value

  if (filters.search) {
    const search = filters.search.toLowerCase()
    list = list.filter(s =>
      s.name.toLowerCase().includes(search) ||
      s.agent_name?.toLowerCase().includes(search) ||
      s.listen_addr?.toLowerCase().includes(search)
    )
  }

  return list
})

// 加载服务列表
const loadServices = async () => {
  loading.value = true
  try {
    const params: any = {
      page: pagination.page,
      size: pagination.pageSize
    }
    if (filters.agentId) {
      params.agent_id = filters.agentId
    }

    const response = await request({
      url: '/api/v1/admin/services',
      method: 'get',
      params
    })

    if (response.success) {
      serviceList.value = response.data || []
      pagination.total = response.total || 0
    }
  } catch (error) {
    ElMessage.error(t('desktopAuth.loadServicesFailed'))
  } finally {
    loading.value = false
  }
}

// 加载 Agent 列表
const loadAgents = async () => {
  try {
    const response = await getAgents()
    if (response.success) {
      agentList.value = response.data || []
    }
  } catch (error) {
    console.error('Failed to load agents:', error)
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

// 搜索
const handleSearch = () => {
  // 搜索是实时的，通过 computed 实现
}

// 添加分组授权
const handleAddGroup = (row: any) => {
  currentService.value = row
  addGroupForm.groupIds = []
  addGroupDialogVisible.value = true
}

// 添加用户授权
const handleAddUser = (row: any) => {
  currentService.value = row
  addUserForm.clientIds = []
  addUserDialogVisible.value = true
}

// 提交分组授权
const handleSubmitGroupAuth = async () => {
  if (addGroupForm.groupIds.length === 0) {
    ElMessage.warning(t('desktopAuth.selectGroupRequired'))
    return
  }

  submitting.value = true
  try {
    // 批量添加分组授权
    const promises = addGroupForm.groupIds.map(groupId =>
      request({
        url: `/api/v1/admin/services/${currentService.value.id}/client-groups`,
        method: 'post',
        data: {
          group_id: groupId
        }
      })
    )

    await Promise.all(promises)

    ElMessage.success(t('desktopAuth.authSuccess'))
    addGroupDialogVisible.value = false
    await loadServices()
  } catch (error: any) {
    if (error.response?.data?.message?.includes('ACL') || error.response?.data?.message?.includes('Headscale')) {
      ElMessage.error(t('desktopAuth.aclSyncError'))
    } else {
      ElMessage.error(error.response?.data?.message || t('desktopAuth.authFailed'))
    }
  } finally {
    submitting.value = false
  }
}

// 提交用户授权
const handleSubmitUserAuth = async () => {
  if (addUserForm.clientIds.length === 0) {
    ElMessage.warning(t('desktopAuth.selectUserRequired'))
    return
  }

  submitting.value = true
  try {
    // 批量添加用户授权
    const promises = addUserForm.clientIds.map(clientId =>
      request({
        url: `/api/v1/admin/services/${currentService.value.id}/clients`,
        method: 'post',
        data: {
          client_id: clientId
        }
      })
    )

    await Promise.all(promises)

    ElMessage.success(t('desktopAuth.authSuccess'))
    addUserDialogVisible.value = false
    await loadServices()
  } catch (error: any) {
    if (error.response?.data?.message?.includes('ACL') || error.response?.data?.message?.includes('Headscale')) {
      ElMessage.error(t('desktopAuth.aclSyncError'))
    } else {
      ElMessage.error(error.response?.data?.message || t('desktopAuth.authFailed'))
    }
  } finally {
    submitting.value = false
  }
}

// 查看分组授权
const handleViewGroups = async (row: any) => {
  currentService.value = row
  viewGroupsDialogVisible.value = true
  loadingGroupAuth.value = true

  try {
    const response = await request({
      url: `/api/v1/admin/services/${row.id}/client-groups`,
      method: 'get'
    })
    if (response.success) {
      groupAuthList.value = response.data || []
    }
  } catch (error) {
    ElMessage.error(t('desktopAuth.loadPermissionsFailed'))
  } finally {
    loadingGroupAuth.value = false
  }
}

// 查看用户授权
const handleViewUsers = async (row: any) => {
  currentService.value = row
  viewUsersDialogVisible.value = true
  loadingUserAuth.value = true

  try {
    const response = await request({
      url: `/api/v1/admin/services/${row.id}/clients`,
      method: 'get'
    })
    if (response.success) {
      userAuthList.value = response.data || []
    }
  } catch (error) {
    ElMessage.error(t('desktopAuth.loadPermissionsFailed'))
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
    await ElMessageBox.confirm(t('desktopAuth.revokeConfirm'), t('common.confirm'), {
      type: 'warning'
    })

    await request({
      url: `/api/v1/admin/services/${currentService.value.id}/client-groups/${row.id}`,
      method: 'delete'
    })

    ElMessage.success(t('desktopAuth.revokeSuccess'))
    await handleViewGroups(currentService.value)
    await loadServices()
  } catch (error: any) {
    if (error !== 'cancel') {
      if (error.response?.data?.message?.includes('ACL') || error.response?.data?.message?.includes('Headscale')) {
        ElMessage.error(t('desktopAuth.aclSyncError'))
      } else {
        ElMessage.error(error.response?.data?.message || t('desktopAuth.revokeFailed'))
      }
    }
  }
}

// 撤销用户授权
const handleRevokeUserAuth = async (row: any) => {
  try {
    await ElMessageBox.confirm(t('desktopAuth.revokeConfirm'), t('common.confirm'), {
      type: 'warning'
    })

    await request({
      url: `/api/v1/admin/services/${currentService.value.id}/clients/${row.id}`,
      method: 'delete'
    })

    ElMessage.success(t('desktopAuth.revokeSuccess'))
    await handleViewUsers(currentService.value)
    await loadServices()
  } catch (error: any) {
    if (error !== 'cancel') {
      if (error.response?.data?.message?.includes('ACL') || error.response?.data?.message?.includes('Headscale')) {
        ElMessage.error(t('desktopAuth.aclSyncError'))
      } else {
        ElMessage.error(error.response?.data?.message || t('desktopAuth.revokeFailed'))
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
    loadServices(),
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

.text-muted {
  color: #909399;
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

.service-info-box {
  background: #f5f7fa;
  padding: 12px;
  border-radius: 4px;
  margin-bottom: 16px;
}

.service-info-box p {
  margin: 4px 0;
}

.dialog-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 16px;
}
</style>
