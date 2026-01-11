<template>
  <div class="desktop-auth-container">
    <el-card>
      <template #header>
        <div class="card-header">
          <span>{{ $t('desktopAuth.title') }}</span>
          <el-button type="primary" @click="handleAddAuth">
            <el-icon><Plus /></el-icon>
            {{ $t('desktopAuth.addAuth') }}
          </el-button>
        </div>
      </template>

      <!-- 筛选区域 -->
      <div class="filter-bar">
        <el-select v-model="filters.serviceId" :placeholder="$t('desktopAuth.selectService')" clearable style="width: 200px">
          <el-option :label="$t('desktopAuth.allServices')" :value="null" />
          <el-option
            v-for="svc in serviceList"
            :key="svc.id"
            :label="svc.name"
            :value="svc.id"
          />
        </el-select>
        <el-select v-model="filters.accessType" :placeholder="$t('desktopAuth.selectAccessType')" clearable style="width: 150px">
          <el-option :label="$t('desktopAuth.allTypes')" :value="null" />
          <el-option :label="$t('desktopAuth.public')" value="public" />
          <el-option :label="$t('desktopAuth.private')" value="private" />
          <el-option :label="$t('desktopAuth.group')" value="group" />
        </el-select>
      </div>

      <!-- 服务权限列表 -->
      <el-table :data="filteredServiceList" v-loading="loading" stripe>
        <el-table-column prop="id" label="ID" width="80" />
        <el-table-column prop="name" :label="$t('desktopAuth.serviceName')" min-width="150" />
        <el-table-column prop="agent_name" :label="$t('desktopAuth.agent')" width="120" />
        <el-table-column :label="$t('desktopAuth.serviceAddress')" min-width="180">
          <template #default="{ row }">
            <span v-if="row.agent_ts_ip">{{ row.agent_ts_ip }}:{{ row.listen_port }}</span>
            <span v-else class="text-muted">-</span>
          </template>
        </el-table-column>
        <el-table-column :label="$t('desktopAuth.accessType')" width="120">
          <template #default="{ row }">
            <el-tag :type="getAccessTypeTag(row.access_type)">
              {{ getAccessTypeLabel(row.access_type) }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column :label="$t('desktopAuth.authTarget')" min-width="150">
          <template #default="{ row }">
            <span v-if="row.access_type === 'public'">{{ $t('desktopAuth.allDesktops') }}</span>
            <span v-else-if="row.access_type === 'private'">{{ getOwnerName(row.owner_id) }}</span>
            <span v-else-if="row.access_type === 'group'">{{ getGroupName(row.group_id) }}</span>
            <span v-else>-</span>
          </template>
        </el-table-column>
        <el-table-column :label="$t('desktopAuth.extraAuth')" width="100">
          <template #default="{ row }">
            <el-badge :value="getExtraAuthCount(row.id)" :hidden="getExtraAuthCount(row.id) === 0" class="extra-auth-badge">
              <el-button size="small" @click="handleViewExtraAuth(row)">
                {{ $t('desktopAuth.view') }}
              </el-button>
            </el-badge>
          </template>
        </el-table-column>
        <el-table-column :label="$t('common.actions')" width="150" fixed="right">
          <template #default="{ row }">
            <el-button type="primary" size="small" @click="handleEditPermission(row)">
              {{ $t('desktopAuth.edit') }}
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
        @size-change="handleSearch"
        @current-change="handleSearch"
        class="pagination"
      />
    </el-card>

    <!-- 编辑权限对话框 -->
    <el-dialog v-model="editDialogVisible" :title="$t('desktopAuth.editPermission')" width="600px">
      <el-form :model="editForm" label-width="120px">
        <el-form-item :label="$t('desktopAuth.serviceInfo')">
          <div class="service-info">
            <p><strong>{{ $t('desktopAuth.serviceName') }}:</strong> {{ editForm.serviceName }}</p>
            <p><strong>{{ $t('desktopAuth.serviceAddress') }}:</strong> {{ editForm.serviceAddress }}</p>
            <p><strong>{{ $t('desktopAuth.agent') }}:</strong> {{ editForm.agentName }}</p>
          </div>
        </el-form-item>
        <el-form-item :label="$t('desktopAuth.accessType')" required>
          <el-radio-group v-model="editForm.accessType">
            <el-radio value="public">
              <span>🌐 {{ $t('desktopAuth.public') }}</span>
              <span class="radio-desc">{{ $t('desktopAuth.publicDesc') }}</span>
            </el-radio>
            <el-radio value="private">
              <span>🔒 {{ $t('desktopAuth.private') }}</span>
              <span class="radio-desc">{{ $t('desktopAuth.privateDesc') }}</span>
            </el-radio>
            <el-radio value="group">
              <span>👥 {{ $t('desktopAuth.group') }}</span>
              <span class="radio-desc">{{ $t('desktopAuth.groupDesc') }}</span>
            </el-radio>
          </el-radio-group>
        </el-form-item>
        <el-form-item v-if="editForm.accessType === 'group'" :label="$t('desktopAuth.selectGroup')" required>
          <el-select v-model="editForm.groupId" :placeholder="$t('desktopAuth.selectGroupPlaceholder')" style="width: 100%">
            <el-option
              v-for="group in groupList"
              :key="group.id"
              :label="group.name"
              :value="group.id"
            />
          </el-select>
        </el-form-item>
        <el-alert type="warning" :closable="false" style="margin-top: 10px">
          {{ $t('desktopAuth.aclSyncWarning') }}
        </el-alert>
      </el-form>
      <template #footer>
        <el-button @click="editDialogVisible = false">{{ $t('common.cancel') }}</el-button>
        <el-button type="primary" @click="handleSavePermission" :loading="saving">{{ $t('common.save') }}</el-button>
      </template>
    </el-dialog>

    <!-- 添加额外授权对话框 -->
    <el-dialog v-model="addAuthDialogVisible" :title="$t('desktopAuth.addExtraAuth')" width="600px">
      <el-form :model="addAuthForm" label-width="120px">
        <el-form-item :label="$t('desktopAuth.selectService')" required>
          <el-select v-model="addAuthForm.serviceId" :placeholder="$t('desktopAuth.selectServicePlaceholder')" style="width: 100%">
            <el-option
              v-for="svc in serviceList"
              :key="svc.id"
              :label="`${svc.name} (${svc.agent_ts_ip || '-'}:${svc.listen_port})`"
              :value="svc.id"
            />
          </el-select>
        </el-form-item>
        <el-form-item :label="$t('desktopAuth.selectClient')" required>
          <el-select v-model="addAuthForm.clientId" :placeholder="$t('desktopAuth.selectClientPlaceholder')" style="width: 100%">
            <el-option
              v-for="client in clientList"
              :key="client.id"
              :label="`${client.client_id} (${client.tailscale_ip || '-'})`"
              :value="client.id"
            />
          </el-select>
        </el-form-item>
        <el-form-item :label="$t('desktopAuth.expiresAt')">
          <el-radio-group v-model="addAuthForm.expireType" style="margin-bottom: 10px">
            <el-radio value="permanent">{{ $t('desktopAuth.permanent') }}</el-radio>
            <el-radio value="custom">{{ $t('desktopAuth.customExpire') }}</el-radio>
          </el-radio-group>
          <el-date-picker
            v-if="addAuthForm.expireType === 'custom'"
            v-model="addAuthForm.expiresAt"
            type="datetime"
            :placeholder="$t('desktopAuth.selectExpireTime')"
            style="width: 100%"
          />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="addAuthDialogVisible = false">{{ $t('common.cancel') }}</el-button>
        <el-button type="primary" @click="handleSubmitAuth" :loading="submitting">{{ $t('desktopAuth.authorize') }}</el-button>
      </template>
    </el-dialog>

    <!-- 查看额外授权对话框 -->
    <el-dialog v-model="extraAuthDialogVisible" :title="$t('desktopAuth.extraAuthList')" width="800px">
      <div class="extra-auth-header">
        <span>{{ $t('desktopAuth.service') }}: {{ currentService?.name }}</span>
        <el-button type="primary" size="small" @click="handleAddExtraAuth">
          <el-icon><Plus /></el-icon>
          {{ $t('desktopAuth.addAuth') }}
        </el-button>
      </div>
      <el-table :data="extraAuthList" v-loading="loadingExtraAuth" border>
        <el-table-column prop="id" label="ID" width="80" />
        <el-table-column prop="client_name" :label="$t('desktopAuth.clientName')" min-width="120" />
        <el-table-column prop="client_ip" :label="$t('desktopAuth.clientIp')" width="140" />
        <el-table-column :label="$t('desktopAuth.grantedAt')" width="180">
          <template #default="{ row }">
            {{ formatTime(row.granted_at) }}
          </template>
        </el-table-column>
        <el-table-column :label="$t('desktopAuth.expiresAt')" width="180">
          <template #default="{ row }">
            <span v-if="row.expires_at">{{ formatTime(row.expires_at) }}</span>
            <el-tag v-else type="success" size="small">{{ $t('desktopAuth.permanent') }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column :label="$t('common.actions')" width="100" fixed="right">
          <template #default="{ row }">
            <el-button type="danger" size="small" @click="handleRevokeAuth(row)">
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
import { Plus } from '@element-plus/icons-vue'
import { useI18n } from 'vue-i18n'
import { getServices, type ProxyService } from '@/api/service'
import { getClients, type Client } from '@/api/client'
import { getGroups, type Group } from '@/api/group'
import {
  getServicePermissions,
  addServicePermission,
  removeServicePermission,
  updateServiceAccessType,
  getAllServicePermissions,
  type ServicePermission
} from '@/api/servicePermission'

const { t } = useI18n()

// 筛选条件
const filters = reactive({
  serviceId: null as number | null,
  accessType: null as string | null
})

// 分页
const pagination = reactive({
  page: 1,
  pageSize: 20,
  total: 0
})

// 数据列表
const serviceList = ref<ProxyService[]>([])
const clientList = ref<Client[]>([])
const groupList = ref<Group[]>([])
const permissionMap = ref<Map<number, ServicePermission[]>>(new Map())
const loading = ref(false)

// 编辑权限对话框
const editDialogVisible = ref(false)
const editForm = reactive({
  serviceId: 0,
  serviceName: '',
  serviceAddress: '',
  agentName: '',
  accessType: 'public',
  groupId: null as number | null
})
const saving = ref(false)

// 添加授权对话框
const addAuthDialogVisible = ref(false)
const addAuthForm = reactive({
  serviceId: null as number | null,
  clientId: null as number | null,
  expireType: 'permanent',
  expiresAt: null as Date | null
})
const submitting = ref(false)

// 额外授权对话框
const extraAuthDialogVisible = ref(false)
const currentService = ref<ProxyService | null>(null)
const extraAuthList = ref<ServicePermission[]>([])
const loadingExtraAuth = ref(false)

// 计算过滤后的服务列表
const filteredServiceList = computed(() => {
  let list = serviceList.value

  if (filters.serviceId) {
    list = list.filter(s => s.id === filters.serviceId)
  }

  if (filters.accessType) {
    list = list.filter(s => s.access_type === filters.accessType)
  }

  pagination.total = list.length

  const start = (pagination.page - 1) * pagination.pageSize
  const end = start + pagination.pageSize
  return list.slice(start, end)
})

// 加载服务列表
const loadServices = async () => {
  loading.value = true
  try {
    const response = await getServices()
    if (response.data?.success) {
      serviceList.value = response.data.data || []
    }
  } catch (error) {
    ElMessage.error(t('desktopAuth.loadServicesFailed'))
  } finally {
    loading.value = false
  }
}

// 加载客户端列表
const loadClients = async () => {
  try {
    const response = await getClients()
    if (response.clients) {
      clientList.value = response.clients
    }
  } catch (error) {
    console.error('Failed to load clients:', error)
  }
}

// 加载分组列表
const loadGroups = async () => {
  try {
    const response = await getGroups()
    if (response.data) {
      groupList.value = response.data
    }
  } catch (error) {
    console.error('Failed to load groups:', error)
  }
}

// 加载所有服务权限
const loadAllPermissions = async () => {
  try {
    const response = await getAllServicePermissions()
    if (response.data?.success) {
      const perms = response.data.data || []
      const map = new Map<number, ServicePermission[]>()
      for (const perm of perms) {
        const list = map.get(perm.service_id) || []
        list.push(perm)
        map.set(perm.service_id, list)
      }
      permissionMap.value = map
    }
  } catch (error) {
    console.error('Failed to load permissions:', error)
  }
}

// 查询
const handleSearch = () => {
  pagination.page = 1
}

// 获取访问类型标签样式
const getAccessTypeTag = (type: string) => {
  switch (type) {
    case 'public': return 'success'
    case 'private': return 'danger'
    case 'group': return 'warning'
    default: return 'info'
  }
}

// 获取访问类型标签文本
const getAccessTypeLabel = (type: string) => {
  switch (type) {
    case 'public': return t('desktopAuth.public')
    case 'private': return t('desktopAuth.private')
    case 'group': return t('desktopAuth.group')
    default: return type
  }
}

// 获取所有者名称
const getOwnerName = (ownerId: number) => {
  if (!ownerId) return t('desktopAuth.creator')
  const client = clientList.value.find(c => c.id === ownerId)
  return client ? client.client_id : `Client #${ownerId}`
}

// 获取分组名称
const getGroupName = (groupId: number | null) => {
  if (!groupId) return '-'
  const group = groupList.value.find(g => g.id === groupId)
  return group ? group.name : `Group #${groupId}`
}

// 获取额外授权数量
const getExtraAuthCount = (serviceId: number) => {
  return permissionMap.value.get(serviceId)?.length || 0
}

// 编辑权限
const handleEditPermission = (row: ProxyService) => {
  editForm.serviceId = row.id
  editForm.serviceName = row.name
  editForm.serviceAddress = `${row.agent_ts_ip || '-'}:${row.listen_port}`
  editForm.agentName = row.agent_name || '-'
  editForm.accessType = row.access_type || 'public'
  editForm.groupId = row.group_id || null
  editDialogVisible.value = true
}

// 保存权限
const handleSavePermission = async () => {
  if (editForm.accessType === 'group' && !editForm.groupId) {
    ElMessage.warning(t('desktopAuth.selectGroupRequired'))
    return
  }

  saving.value = true
  try {
    await updateServiceAccessType(editForm.serviceId, {
      access_type: editForm.accessType,
      group_id: editForm.accessType === 'group' ? editForm.groupId : null
    })
    ElMessage.success(t('desktopAuth.updateSuccess'))
    editDialogVisible.value = false
    await loadServices()
  } catch (error: any) {
    // 检查是否是 ACL 同步错误
    if (error.response?.data?.message?.includes('ACL') || error.response?.data?.message?.includes('Headscale')) {
      ElMessage.error(t('desktopAuth.aclSyncError'))
    } else {
      ElMessage.error(error.response?.data?.message || t('desktopAuth.updateFailed'))
    }
  } finally {
    saving.value = false
  }
}

// 添加授权
const handleAddAuth = () => {
  addAuthForm.serviceId = null
  addAuthForm.clientId = null
  addAuthForm.expireType = 'permanent'
  addAuthForm.expiresAt = null
  addAuthDialogVisible.value = true
}

// 提交授权
const handleSubmitAuth = async () => {
  if (!addAuthForm.serviceId || !addAuthForm.clientId) {
    ElMessage.warning(t('desktopAuth.selectServiceAndClient'))
    return
  }

  submitting.value = true
  try {
    const expiresAt = addAuthForm.expireType === 'custom' && addAuthForm.expiresAt
      ? addAuthForm.expiresAt.toISOString()
      : undefined

    await addServicePermission(addAuthForm.serviceId, {
      client_id: addAuthForm.clientId,
      expires_at: expiresAt
    })
    ElMessage.success(t('desktopAuth.authSuccess'))
    addAuthDialogVisible.value = false
    await loadAllPermissions()
  } catch (error: any) {
    // 检查是否是 ACL 同步错误
    if (error.response?.data?.message?.includes('ACL') || error.response?.data?.message?.includes('Headscale')) {
      ElMessage.error(t('desktopAuth.aclSyncError'))
    } else {
      ElMessage.error(error.response?.data?.message || t('desktopAuth.authFailed'))
    }
  } finally {
    submitting.value = false
  }
}

// 查看额外授权
const handleViewExtraAuth = async (row: ProxyService) => {
  currentService.value = row
  extraAuthDialogVisible.value = true
  loadingExtraAuth.value = true

  try {
    const response = await getServicePermissions(row.id)
    if (response.data?.success) {
      extraAuthList.value = response.data.data || []
    }
  } catch (error) {
    ElMessage.error(t('desktopAuth.loadPermissionsFailed'))
  } finally {
    loadingExtraAuth.value = false
  }
}

// 从额外授权对话框添加授权
const handleAddExtraAuth = () => {
  if (currentService.value) {
    addAuthForm.serviceId = currentService.value.id
    addAuthForm.clientId = null
    addAuthForm.expireType = 'permanent'
    addAuthForm.expiresAt = null
    addAuthDialogVisible.value = true
  }
}

// 撤销授权
const handleRevokeAuth = async (row: ServicePermission) => {
  try {
    await ElMessageBox.confirm(t('desktopAuth.revokeConfirm'), t('common.confirm'), {
      type: 'warning'
    })

    await removeServicePermission(currentService.value!.id, row.id)
    ElMessage.success(t('desktopAuth.revokeSuccess'))

    // 刷新列表
    await handleViewExtraAuth(currentService.value!)
    await loadAllPermissions()
  } catch (error: any) {
    if (error !== 'cancel') {
      // 检查是否是 ACL 同步错误
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
  return new Date(time).toLocaleString('zh-CN')
}

onMounted(async () => {
  await Promise.all([
    loadServices(),
    loadClients(),
    loadGroups(),
    loadAllPermissions()
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

.service-info {
  background: #f5f7fa;
  padding: 12px;
  border-radius: 4px;
}

.service-info p {
  margin: 4px 0;
}

.el-radio-group {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.radio-desc {
  color: #909399;
  font-size: 12px;
  margin-left: 8px;
}

.extra-auth-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 16px;
}

.extra-auth-badge :deep(.el-badge__content) {
  top: 0;
  right: 10px;
}
</style>
