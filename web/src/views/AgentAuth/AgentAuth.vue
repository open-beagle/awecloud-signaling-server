<template>
  <div class="agent-auth-container">
    <el-card>
      <template #header>
        <div class="card-header">
          <span>{{ $t('agentLevelAuth.agentTitle') }}</span>
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
              :class="row.agent_group_count > 0 ? 'count-link' : 'count-zero'"
              @click="handleViewGroups(row)"
            >
              {{ row.agent_group_count || 0 }}
            </el-button>
          </template>
        </el-table-column>
        <el-table-column :label="$t('agentLevelAuth.agentCount')" width="100" align="center">
          <template #default="{ row }">
            <el-button
              type="text"
              :class="row.agent_count > 0 ? 'count-link' : 'count-zero'"
              @click="handleViewAgents(row)"
            >
              {{ row.agent_count || 0 }}
            </el-button>
          </template>
        </el-table-column>
        <el-table-column :label="$t('common.actions')" width="200" fixed="right">
          <template #default="{ row }">
            <el-button type="primary" size="small" @click="handleAddGroup(row)">
              <el-icon><Plus /></el-icon>
              {{ $t('agentLevelAuth.addGroup') }}
            </el-button>
            <el-button type="success" size="small" @click="handleAddAgent(row)">
              <el-icon><Plus /></el-icon>
              {{ $t('agentLevelAuth.addAgent') }}
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
        <p><strong>{{ $t('agentLevelAuth.targetAgent') }}:</strong> {{ currentAgent?.alias || currentAgent?.name }}</p>
        <p><strong>{{ $t('agentLevelAuth.agentIp') }}:</strong> {{ currentAgent?.tailscale_ip || '-' }}</p>
        <p><strong>{{ $t('agentLevelAuth.serviceCount') }}:</strong> {{ currentAgent?.service_count || 0 }}</p>
      </div>
      <el-form :model="addGroupForm" label-width="120px" style="margin-top: 20px">
        <el-form-item :label="$t('agentLevelAuth.selectGroup')" required>
          <el-select
            v-model="addGroupForm.groupIds"
            :placeholder="$t('agentLevelAuth.selectAgentGroupPlaceholder')"
            multiple
            style="width: 100%"
          >
            <el-option
              v-for="group in agentGroupList"
              :key="group.id"
              :label="`${group.name} (${group.member_count || 0} ${$t('agentLevelAuth.members')})`"
              :value="group.id"
            />
          </el-select>
        </el-form-item>
        <el-alert type="info" :closable="false">
          {{ $t('agentLevelAuth.agentToAgentTip') }}
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

    <!-- 添加代理授权对话框 -->
    <el-dialog v-model="addAgentDialogVisible" :title="$t('agentLevelAuth.addAgentAuth')" width="600px">
      <div class="agent-info-box">
        <p><strong>{{ $t('agentLevelAuth.targetAgent') }}:</strong> {{ currentAgent?.alias || currentAgent?.name }}</p>
        <p><strong>{{ $t('agentLevelAuth.agentIp') }}:</strong> {{ currentAgent?.tailscale_ip || '-' }}</p>
        <p><strong>{{ $t('agentLevelAuth.serviceCount') }}:</strong> {{ currentAgent?.service_count || 0 }}</p>
      </div>
      <el-form :model="addAgentForm" label-width="120px" style="margin-top: 20px">
        <el-form-item :label="$t('agentLevelAuth.selectSourceAgent')" required>
          <el-select
            v-model="addAgentForm.agentIds"
            :placeholder="$t('agentLevelAuth.selectSourceAgentPlaceholder')"
            multiple
            filterable
            style="width: 100%"
          >
            <el-option
              v-for="agent in sourceAgentList"
              :key="agent.id"
              :label="`${agent.alias || agent.name} (${agent.tailscale_ip || '-'})`"
              :value="agent.id"
              :disabled="agent.id === currentAgent?.id"
            />
          </el-select>
        </el-form-item>
        <el-alert type="info" :closable="false">
          {{ $t('agentLevelAuth.agentToAgentTip') }}
        </el-alert>
        <el-alert type="warning" :closable="false" style="margin-top: 10px">
          {{ $t('agentLevelAuth.aclSyncWarning') }}
        </el-alert>
      </el-form>
      <template #footer>
        <el-button @click="addAgentDialogVisible = false">{{ $t('common.cancel') }}</el-button>
        <el-button type="primary" @click="handleSubmitAgentAuth" :loading="submitting">
          {{ $t('agentLevelAuth.authorize') }}
        </el-button>
      </template>
    </el-dialog>

    <!-- 查看分组授权对话框 -->
    <el-dialog v-model="viewGroupsDialogVisible" :title="$t('agentLevelAuth.groupAuthList')" width="800px">
      <div class="dialog-header">
        <span>{{ $t('agentLevelAuth.targetAgent') }}: {{ currentAgent?.alias || currentAgent?.name }}</span>
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

    <!-- 查看代理授权对话框 -->
    <el-dialog v-model="viewAgentsDialogVisible" :title="$t('agentLevelAuth.agentAuthList')" width="800px">
      <div class="dialog-header">
        <span>{{ $t('agentLevelAuth.targetAgent') }}: {{ currentAgent?.alias || currentAgent?.name }}</span>
        <el-button type="primary" size="small" @click="handleAddAgentFromView">
          <el-icon><Plus /></el-icon>
          {{ $t('agentLevelAuth.addAgent') }}
        </el-button>
      </div>
      <el-table :data="agentAuthList" v-loading="loadingAgentAuth" border>
        <el-table-column prop="id" label="ID" width="80" />
        <el-table-column :label="$t('agentLevelAuth.sourceAgentName')" min-width="150">
          <template #default="{ row }">
            {{ row.source_alias || row.source_name }}
          </template>
        </el-table-column>
        <el-table-column prop="source_ip" :label="$t('agentLevelAuth.sourceAgentIp')" width="140" />
        <el-table-column :label="$t('agentLevelAuth.grantedAt')" width="180">
          <template #default="{ row }">
            {{ formatTime(row.granted_at) }}
          </template>
        </el-table-column>
        <el-table-column :label="$t('common.actions')" width="100" fixed="right">
          <template #default="{ row }">
            <el-button type="danger" size="small" @click="handleRevokeAgentAuth(row)">
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
const sourceAgentList = ref<any[]>([])
const agentGroupList = ref<any[]>([])
const loading = ref(false)

// 当前操作的 Agent
const currentAgent = ref<any>(null)

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
        type: 'agent' // 代理授权
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

// 加载所有 Agent（用于选择源 Agent）
const loadAllAgents = async () => {
  try {
    const response = await request({
      url: '/api/v1/admin/agents',
      method: 'get'
    })
    if (response.success) {
      sourceAgentList.value = response.data || []
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

// 添加分组授权
const handleAddGroup = (row: any) => {
  currentAgent.value = row
  addGroupForm.groupIds = []
  addGroupDialogVisible.value = true
}

// 添加代理授权
const handleAddAgent = (row: any) => {
  currentAgent.value = row
  addAgentForm.agentIds = []
  addAgentDialogVisible.value = true
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
        url: `/api/v1/admin/agents/${currentAgent.value.id}/agent-group-permissions`,
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

// 提交代理授权
const handleSubmitAgentAuth = async () => {
  if (addAgentForm.agentIds.length === 0) {
    ElMessage.warning(t('agentLevelAuth.selectAgentRequired'))
    return
  }

  submitting.value = true
  try {
    const promises = addAgentForm.agentIds.map(agentId =>
      request({
        url: `/api/v1/admin/agents/${currentAgent.value.id}/agent-permissions`,
        method: 'post',
        data: { source_agent_id: agentId }
      })
    )

    await Promise.all(promises)

    ElMessage.success(t('agentLevelAuth.authSuccess'))
    addAgentDialogVisible.value = false
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
      url: `/api/v1/admin/agents/${row.id}/agent-group-permissions`,
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

// 查看代理授权
const handleViewAgents = async (row: any) => {
  currentAgent.value = row
  viewAgentsDialogVisible.value = true
  loadingAgentAuth.value = true

  try {
    const response = await request({
      url: `/api/v1/admin/agents/${row.id}/agent-permissions`,
      method: 'get'
    })
    if (response.success) {
      agentAuthList.value = response.data || []
    }
  } catch (error) {
    ElMessage.error(t('agentLevelAuth.loadPermissionsFailed'))
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
    await ElMessageBox.confirm(t('agentLevelAuth.revokeConfirm'), t('common.confirm'), {
      type: 'warning'
    })

    await request({
      url: `/api/v1/admin/agents/${currentAgent.value.id}/agent-group-permissions/${row.id}`,
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

// 撤销代理授权
const handleRevokeAgentAuth = async (row: any) => {
  try {
    await ElMessageBox.confirm(t('agentLevelAuth.revokeConfirm'), t('common.confirm'), {
      type: 'warning'
    })

    await request({
      url: `/api/v1/admin/agents/${currentAgent.value.id}/agent-permissions/${row.id}`,
      method: 'delete'
    })

    ElMessage.success(t('agentLevelAuth.revokeSuccess'))
    await handleViewAgents(currentAgent.value)
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
    loadAllAgents(),
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
