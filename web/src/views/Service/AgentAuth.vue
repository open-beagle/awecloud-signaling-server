<template>
  <div class="agent-auth-container">
    <el-card>
      <template #header>
        <div class="card-header">
          <span>{{ $t('agentAuth.title') }}</span>
        </div>
      </template>

      <!-- 说明提示 -->
      <el-alert type="info" :closable="false" style="margin-bottom: 16px">
        {{ $t('agentAuth.sameGroupTip') }}
      </el-alert>

      <!-- 筛选区域 -->
      <div class="filter-bar">
        <el-select v-model="filters.agentId" :placeholder="$t('agentAuth.filterByAgent')" clearable style="width: 200px" @change="loadServices">
          <el-option :label="$t('agentAuth.allAgents')" :value="null" />
          <el-option
            v-for="agent in agentList"
            :key="agent.id"
            :label="agent.name"
            :value="agent.id"
          />
        </el-select>
        <el-input
          v-model="filters.search"
          :placeholder="$t('agentAuth.searchPlaceholder')"
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
        <el-table-column prop="name" :label="$t('agentAuth.serviceName')" min-width="150" />
        <el-table-column prop="agent_name" :label="$t('agentAuth.agent')" width="120" />
        <el-table-column :label="$t('agentAuth.serviceAddress')" min-width="180">
          <template #default="{ row }">
            <span v-if="row.listen_addr">{{ row.listen_addr }}</span>
            <span v-else class="text-muted">-</span>
          </template>
        </el-table-column>
        <el-table-column :label="$t('agentAuth.groupCount')" width="100" align="center">
          <template #default="{ row }">
            <el-button
              type="text"
              :class="row.agent_group_count > 0 ? 'count-link' : 'count-zero'"
              @click="handleViewGroups(row)"
            >
              {{ row.agent_group_count }}
            </el-button>
          </template>
        </el-table-column>
        <el-table-column :label="$t('agentAuth.agentCount')" width="100" align="center">
          <template #default="{ row }">
            <el-button
              type="text"
              :class="row.agent_count > 0 ? 'count-link' : 'count-zero'"
              @click="handleViewAgents(row)"
            >
              {{ row.agent_count }}
            </el-button>
          </template>
        </el-table-column>
        <el-table-column :label="$t('common.actions')" width="200" fixed="right">
          <template #default="{ row }">
            <el-button type="primary" size="small" @click="handleAddGroup(row)">
              <el-icon><Plus /></el-icon>
              {{ $t('agentAuth.addGroup') }}
            </el-button>
            <el-button type="success" size="small" @click="handleAddAgent(row)">
              <el-icon><Plus /></el-icon>
              {{ $t('agentAuth.addAgent') }}
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
    <el-dialog v-model="addGroupDialogVisible" :title="$t('agentAuth.addGroupAuth')" width="600px">
      <div class="service-info-box">
        <p><strong>{{ $t('agentAuth.service') }}:</strong> {{ currentService?.name }}</p>
        <p><strong>{{ $t('agentAuth.agent') }}:</strong> {{ currentService?.agent_name }}</p>
        <p><strong>{{ $t('agentAuth.serviceAddress') }}:</strong> {{ currentService?.listen_addr }}</p>
      </div>
      <el-form :model="addGroupForm" label-width="120px" style="margin-top: 20px">
        <el-form-item :label="$t('agentAuth.selectGroup')" required>
          <el-select
            v-model="addGroupForm.groupIds"
            :placeholder="$t('agentAuth.selectGroupPlaceholder')"
            multiple
            style="width: 100%"
          >
            <el-option
              v-for="group in agentGroupList"
              :key="group.id"
              :label="`${group.name} (${group.member_count || 0} ${$t('agentAuth.members')})`"
              :value="group.id"
            />
          </el-select>
        </el-form-item>
        <el-alert type="warning" :closable="false">
          {{ $t('agentAuth.aclSyncWarning') }}
        </el-alert>
      </el-form>
      <template #footer>
        <el-button @click="addGroupDialogVisible = false">{{ $t('common.cancel') }}</el-button>
        <el-button type="primary" @click="handleSubmitGroupAuth" :loading="submitting">
          {{ $t('agentAuth.authorize') }}
        </el-button>
      </template>
    </el-dialog>

    <!-- 添加代理授权对话框 -->
    <el-dialog v-model="addAgentDialogVisible" :title="$t('agentAuth.addAgentAuth')" width="600px">
      <div class="service-info-box">
        <p><strong>{{ $t('agentAuth.service') }}:</strong> {{ currentService?.name }}</p>
        <p><strong>{{ $t('agentAuth.agent') }}:</strong> {{ currentService?.agent_name }}</p>
        <p><strong>{{ $t('agentAuth.serviceAddress') }}:</strong> {{ currentService?.listen_addr }}</p>
      </div>
      <el-form :model="addAgentForm" label-width="120px" style="margin-top: 20px">
        <el-form-item :label="$t('agentAuth.selectAgent')" required>
          <el-select
            v-model="addAgentForm.agentIds"
            :placeholder="$t('agentAuth.selectAgentPlaceholder')"
            multiple
            filterable
            style="width: 100%"
          >
            <el-option
              v-for="agent in agentList"
              :key="agent.id"
              :label="`${agent.name} (${agent.ip || '-'})`"
              :value="agent.id"
            />
          </el-select>
        </el-form-item>
        <el-alert type="warning" :closable="false">
          {{ $t('agentAuth.aclSyncWarning') }}
        </el-alert>
      </el-form>
      <template #footer>
        <el-button @click="addAgentDialogVisible = false">{{ $t('common.cancel') }}</el-button>
        <el-button type="primary" @click="handleSubmitAgentAuth" :loading="submitting">
          {{ $t('agentAuth.authorize') }}
        </el-button>
      </template>
    </el-dialog>

    <!-- 查看分组授权对话框 -->
    <el-dialog v-model="viewGroupsDialogVisible" :title="$t('agentAuth.groupAuthList')" width="800px">
      <div class="dialog-header">
        <span>{{ $t('agentAuth.service') }}: {{ currentService?.name }}</span>
        <el-button type="primary" size="small" @click="handleAddGroupFromView">
          <el-icon><Plus /></el-icon>
          {{ $t('agentAuth.addGroup') }}
        </el-button>
      </div>
      <el-table :data="groupAuthList" v-loading="loadingGroupAuth" border>
        <el-table-column prop="id" label="ID" width="80" />
        <el-table-column prop="name" :label="$t('agentAuth.groupName')" min-width="150">
          <template #default="{ row }">
            {{ row.alias ? `${row.alias} (${row.name})` : row.name }}
          </template>
        </el-table-column>
        <el-table-column :label="$t('agentAuth.memberCount')" width="120">
          <template #default="{ row }">
            {{ row.member_count || 0 }} {{ $t('agentAuth.members') }}
          </template>
        </el-table-column>
        <el-table-column :label="$t('agentAuth.grantedAt')" width="180">
          <template #default="{ row }">
            {{ formatTime(row.granted_at) }}
          </template>
        </el-table-column>
        <el-table-column :label="$t('common.actions')" width="100" fixed="right">
          <template #default="{ row }">
            <el-button type="danger" size="small" @click="handleRevokeGroupAuth(row)">
              {{ $t('agentAuth.revoke') }}
            </el-button>
          </template>
        </el-table-column>
      </el-table>
    </el-dialog>

    <!-- 查看代理授权对话框 -->
    <el-dialog v-model="viewAgentsDialogVisible" :title="$t('agentAuth.agentAuthList')" width="800px">
      <div class="dialog-header">
        <span>{{ $t('agentAuth.service') }}: {{ currentService?.name }}</span>
        <el-button type="primary" size="small" @click="handleAddAgentFromView">
          <el-icon><Plus /></el-icon>
          {{ $t('agentAuth.addAgent') }}
        </el-button>
      </div>
      <el-table :data="agentAuthList" v-loading="loadingAgentAuth" border>
        <el-table-column prop="id" label="ID" width="80" />
        <el-table-column prop="name" :label="$t('agentAuth.agentName')" min-width="150" />
        <el-table-column prop="alias" :label="$t('agentAuth.agentIp')" width="140" />
        <el-table-column :label="$t('agentAuth.grantedAt')" width="180">
          <template #default="{ row }">
            {{ formatTime(row.granted_at) }}
          </template>
        </el-table-column>
        <el-table-column :label="$t('common.actions')" width="100" fixed="right">
          <template #default="{ row }">
            <el-button type="danger" size="small" @click="handleRevokeAgentAuth(row)">
              {{ $t('agentAuth.revoke') }}
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
const agentGroupList = ref<any[]>([])
const loading = ref(false)

// 当前操作的服务
const currentService = ref<any>(null)

// 添加分组授权对话框
const addGroupDialogVisible = ref(false)
const addGroupForm = reactive({
  groupIds: [] as number[]
})

// 添加代理授权对话框
const addAgentDialogVisible = ref(false)
const addAgentForm = reactive({
  agentIds: [] as number[]
})

const submitting = ref(false)

// 查看分组授权对话框
const viewGroupsDialogVisible = ref(false)
const groupAuthList = ref<any[]>([])
const loadingGroupAuth = ref(false)

// 查看代理授权对话框
const viewAgentsDialogVisible = ref(false)
const agentAuthList = ref<any[]>([])
const loadingAgentAuth = ref(false)

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
    ElMessage.error(t('agentAuth.loadServicesFailed'))
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

// 加载 Agent 分组列表
const loadAgentGroups = async () => {
  try {
    const response = await request({
      url: '/api/v1/admin/agent-groups',
      method: 'get'
    })
    if (response.success) {
      agentGroupList.value = response.data || []
    }
  } catch (error) {
    console.error('Failed to load agent groups:', error)
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

// 添加代理授权
const handleAddAgent = (row: any) => {
  currentService.value = row
  addAgentForm.agentIds = []
  addAgentDialogVisible.value = true
}

// 提交分组授权
const handleSubmitGroupAuth = async () => {
  if (addGroupForm.groupIds.length === 0) {
    ElMessage.warning(t('agentAuth.selectGroupRequired'))
    return
  }

  submitting.value = true
  try {
    // 批量添加分组授权
    const promises = addGroupForm.groupIds.map(groupId =>
      request({
        url: `/api/v1/admin/services/${currentService.value.id}/agent-groups`,
        method: 'post',
        data: {
          group_id: groupId
        }
      })
    )

    await Promise.all(promises)

    ElMessage.success(t('agentAuth.authSuccess'))
    addGroupDialogVisible.value = false
    await loadServices()
  } catch (error: any) {
    if (error.response?.data?.message?.includes('ACL') || error.response?.data?.message?.includes('Headscale')) {
      ElMessage.error(t('agentAuth.aclSyncError'))
    } else {
      ElMessage.error(error.response?.data?.message || t('agentAuth.authFailed'))
    }
  } finally {
    submitting.value = false
  }
}

// 提交代理授权
const handleSubmitAgentAuth = async () => {
  if (addAgentForm.agentIds.length === 0) {
    ElMessage.warning(t('agentAuth.selectAgentRequired'))
    return
  }

  submitting.value = true
  try {
    // 批量添加代理授权
    const promises = addAgentForm.agentIds.map(agentId =>
      request({
        url: `/api/v1/admin/services/${currentService.value.id}/agents`,
        method: 'post',
        data: {
          agent_id: agentId
        }
      })
    )

    await Promise.all(promises)

    ElMessage.success(t('agentAuth.authSuccess'))
    addAgentDialogVisible.value = false
    await loadServices()
  } catch (error: any) {
    if (error.response?.data?.message?.includes('ACL') || error.response?.data?.message?.includes('Headscale')) {
      ElMessage.error(t('agentAuth.aclSyncError'))
    } else {
      ElMessage.error(error.response?.data?.message || t('agentAuth.authFailed'))
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
      url: `/api/v1/admin/services/${row.id}/agent-groups`,
      method: 'get'
    })
    if (response.success) {
      groupAuthList.value = response.data || []
    }
  } catch (error) {
    ElMessage.error(t('agentAuth.loadPermissionsFailed'))
  } finally {
    loadingGroupAuth.value = false
  }
}

// 查看代理授权
const handleViewAgents = async (row: any) => {
  currentService.value = row
  viewAgentsDialogVisible.value = true
  loadingAgentAuth.value = true

  try {
    const response = await request({
      url: `/api/v1/admin/services/${row.id}/agents`,
      method: 'get'
    })
    if (response.success) {
      agentAuthList.value = response.data || []
    }
  } catch (error) {
    ElMessage.error(t('agentAuth.loadPermissionsFailed'))
  } finally {
    loadingAgentAuth.value = false
  }
}

// 从查看对话框添加分组
const handleAddGroupFromView = () => {
  viewGroupsDialogVisible.value = false
  addGroupForm.groupIds = []
  addGroupDialogVisible.value = true
}

// 从查看对话框添加代理
const handleAddAgentFromView = () => {
  viewAgentsDialogVisible.value = false
  addAgentForm.agentIds = []
  addAgentDialogVisible.value = true
}

// 撤销分组授权
const handleRevokeGroupAuth = async (row: any) => {
  try {
    await ElMessageBox.confirm(t('agentAuth.revokeConfirm'), t('common.confirm'), {
      type: 'warning'
    })

    await request({
      url: `/api/v1/admin/services/${currentService.value.id}/agent-groups/${row.id}`,
      method: 'delete'
    })

    ElMessage.success(t('agentAuth.revokeSuccess'))
    await handleViewGroups(currentService.value)
    await loadServices()
  } catch (error: any) {
    if (error !== 'cancel') {
      if (error.response?.data?.message?.includes('ACL') || error.response?.data?.message?.includes('Headscale')) {
        ElMessage.error(t('agentAuth.aclSyncError'))
      } else {
        ElMessage.error(error.response?.data?.message || t('agentAuth.revokeFailed'))
      }
    }
  }
}

// 撤销代理授权
const handleRevokeAgentAuth = async (row: any) => {
  try {
    await ElMessageBox.confirm(t('agentAuth.revokeConfirm'), t('common.confirm'), {
      type: 'warning'
    })

    await request({
      url: `/api/v1/admin/services/${currentService.value.id}/agents/${row.id}`,
      method: 'delete'
    })

    ElMessage.success(t('agentAuth.revokeSuccess'))
    await handleViewAgents(currentService.value)
    await loadServices()
  } catch (error: any) {
    if (error !== 'cancel') {
      if (error.response?.data?.message?.includes('ACL') || error.response?.data?.message?.includes('Headscale')) {
        ElMessage.error(t('agentAuth.aclSyncError'))
      } else {
        ElMessage.error(error.response?.data?.message || t('agentAuth.revokeFailed'))
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
    loadAgentGroups()
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
