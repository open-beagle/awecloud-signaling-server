<template>
  <div class="user-list">
    <div class="page-header">
      <div>
        <div class="eyebrow">兼容身份治理</div>
        <h1>访问主体目录</h1>
        <p>管理 Agent 与 Desktop 使用的全局 User 身份。它不同于平台管理账号，也不等同于租户成员关系。</p>
      </div>
    </div>
    <el-alert class="identity-alert" title="此目录保留旧连接、部署令牌与授权链路；租户资源资格仍由租户成员和访问策略决定。" type="info" show-icon :closable="false" />
    <!-- 搜索和筛选 -->
    <el-card class="search-card" shadow="never">
      <el-form :inline="true" :model="searchForm" class="search-form">
        <el-form-item :label="$t('user.role')">
          <el-select v-model="searchForm.role" :placeholder="$t('common.all')" clearable style="width: 120px">
            <el-option :label="$t('user.roleAgent')" value="agent" />
            <el-option :label="$t('user.roleClient')" value="client" />
          </el-select>
        </el-form-item>
        <el-form-item :label="$t('user.enabled')">
          <el-select v-model="searchForm.enabled" :placeholder="$t('common.all')" clearable style="width: 120px">
            <el-option :label="$t('user.enabledTrue')" value="true" />
            <el-option :label="$t('user.enabledFalse')" value="false" />
          </el-select>
        </el-form-item>
        <el-form-item :label="$t('user.source')">
          <el-select v-model="searchForm.source" :placeholder="$t('common.all')" clearable style="width: 120px">
            <el-option :label="$t('user.sourceManual')" value="manual" />
            <el-option :label="$t('user.sourceLogto')" value="logto" />
          </el-select>
        </el-form-item>
        <el-form-item :label="$t('common.search')">
          <el-input v-model="searchForm.search" :placeholder="$t('user.searchPlaceholder')" clearable style="width: 240px" @keyup.enter="handleSearch" />
        </el-form-item>
        <el-form-item>
          <el-button type="primary" @click="handleSearch">{{ $t('common.search') }}</el-button>
          <el-button @click="handleReset">{{ $t('common.reset') }}</el-button>
        </el-form-item>
        <el-form-item v-if="canWrite" style="float: right">
          <el-button type="primary" @click="showCreateDialog = true">
            <el-icon><Plus /></el-icon>
            {{ $t('user.create') }}
          </el-button>
        </el-form-item>
      </el-form>
    </el-card>

    <!-- 用户列表 -->
    <el-card shadow="never">
      <el-table v-loading="loading" :data="users" stripe>
        <el-table-column prop="id" label="ID" width="80" />
        <el-table-column prop="name" :label="$t('user.name')" min-width="120">
          <template #default="{ row }">
            <el-link type="primary" :underline="false" @click="handleView(row)">{{ row.name }}</el-link>
          </template>
        </el-table-column>
        <el-table-column prop="alias" :label="$t('user.alias')" min-width="120" />
        <el-table-column prop="role" :label="$t('user.role')" width="100">
          <template #default="{ row }">
            <el-tag :type="row.role === 'agent' ? 'success' : 'primary'" size="small">
              {{ row.role === 'agent' ? $t('user.roleAgent') : $t('user.roleClient') }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="node_count" :label="$t('user.nodeCount')" width="100" align="center" />
        <el-table-column prop="service_count" :label="$t('user.serviceCount')" width="100" align="center" />
        <el-table-column prop="versions" :label="$t('user.version')" width="180" align="center">
          <template #default="{ row }">
            <div v-if="row.versions && row.versions.length > 0">
              <div v-for="(version, index) in row.versions" :key="index">
                <el-tag size="small" :type="needsUpgrade(version, row.latest_version) ? 'warning' : 'success'">
                  {{ version }}
                </el-tag>
              </div>
              <el-tooltip v-if="needsUpgrade(row.versions, row.latest_version)" :content="$t('user.upgradeHint', { version: row.latest_version })" placement="top">
                <el-icon color="#E6A23C" style="margin-top: 4px"><Warning /></el-icon>
              </el-tooltip>
            </div>
            <el-text v-else type="info" size="small">{{ $t('user.noVersion') }}</el-text>
          </template>
        </el-table-column>
        <el-table-column prop="enabled" :label="$t('user.enabled')" width="100" align="center">
          <template #default="{ row }">
            <el-tag v-if="row.enabled" type="success" size="small">{{ $t('user.enabledTrue') }}</el-tag>
            <el-tag v-else type="danger" size="small">{{ row.source === 'logto' ? $t('user.pendingApproval') : $t('user.enabledFalse') }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="source" :label="$t('user.source')" width="100" align="center">
          <template #default="{ row }">
            <el-tag v-if="row.source === 'logto'" type="warning" size="small">{{ $t('user.sourceLogto') }}</el-tag>
            <el-tag v-else type="info" size="small">{{ $t('user.sourceManual') }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="created_at" :label="$t('common.createdAt')" width="180">
          <template #default="{ row }">
            {{ formatTime(row.created_at) }}
          </template>
        </el-table-column>
        <el-table-column v-if="canWrite" :label="$t('common.actions')" width="260" fixed="right">
          <template #default="{ row }">
            <el-button v-if="!row.enabled" type="success" link size="small" @click="handleEnable(row)">{{ $t('user.enabledTrue') }}</el-button>
            <el-button v-else type="warning" link size="small" @click="handleDisable(row)">{{ $t('user.enabledFalse') }}</el-button>
            <el-button type="primary" link size="small" :icon="Upload" @click="handleDeploy(row)">{{ $t('user.deploy') }}</el-button>
            <el-button type="primary" link size="small" @click="handleEdit(row)">{{ $t('common.edit') }}</el-button>
            <el-button type="danger" link size="small" @click="handleDelete(row)">{{ $t('common.delete') }}</el-button>
          </template>
        </el-table-column>
      </el-table>

      <!-- 分页 -->
      <div class="pagination-wrapper">
        <el-pagination
          v-model:current-page="pagination.page"
          v-model:page-size="pagination.size"
          :total="pagination.total"
          :page-sizes="[10, 20, 50, 100]"
          layout="total, sizes, prev, pager, next, jumper"
          @size-change="fetchUsers"
          @current-change="fetchUsers"
        />
      </div>
    </el-card>

    <!-- 创建用户弹窗 -->
    <CreateDialog v-model="showCreateDialog" @success="handleCreateSuccess" />

    <!-- 部署弹窗（Agent 和 Client 通用） -->
    <DeployDialog v-model="showDeployDialog" :user="selectedUser" @success="fetchUsers" />
  </div>
</template>

<script setup lang="ts">
import { computed, ref, reactive, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Plus, Upload } from '@element-plus/icons-vue'
import { Warning } from '@element-plus/icons-vue'
import { useI18n } from 'vue-i18n'
import { getUsers, deleteUser, enableUser, disableUser, type User, type UserRole, type UserSource } from '@/api/user'
import { formatTime } from '@/utils/time'
import CreateDialog from './components/CreateDialog.vue'
import DeployDialog from './components/DeployDialog.vue'
import { useWorkspaceStore } from '@/stores/workspace'

const { t } = useI18n()
const router = useRouter()
const workspaceStore = useWorkspaceStore()
const canWrite = computed(() => workspaceStore.can('platform.identities.write'))

const loading = ref(false)
const users = ref<User[]>([])
const showCreateDialog = ref(false)
const showDeployDialog = ref(false)
const selectedUser = ref<User | null>(null)

const searchForm = reactive({
  role: '' as UserRole | '',
  search: '',
  enabled: '' as string,
  source: '' as UserSource | ''
})

const pagination = reactive({
  page: 1,
  size: 20,
  total: 0
})

// 获取用户列表
const fetchUsers = async () => {
  loading.value = true
  try {
    const res = await getUsers({
      role: (searchForm.role || undefined) as UserRole | undefined,
      search: searchForm.search || undefined,
      enabled: searchForm.enabled || undefined,
      source: (searchForm.source || undefined) as UserSource | undefined,
      page: pagination.page,
      size: pagination.size
    })
    if (res.success && res.data) {
      users.value = res.data
      pagination.total = res.total
    }
  } catch (error) {
    console.error('获取用户列表失败:', error)
  } finally {
    loading.value = false
  }
}

// 搜索
const handleSearch = () => {
  pagination.page = 1
  fetchUsers()
}

// 重置
const handleReset = () => {
  searchForm.role = ''
  searchForm.search = ''
  searchForm.enabled = ''
  searchForm.source = ''
  pagination.page = 1
  fetchUsers()
}

// 查看详情
const handleView = (row: User) => {
  router.push(`/platform-identities/${row.name}`)
}

// 编辑
const handleEdit = (row: User) => {
  if (!canWrite.value) return
  router.push(`/platform-identities/${row.name}?edit=true`)
}

// 删除
const handleDelete = async (row: User) => {
  if (!canWrite.value) return
  try {
    await ElMessageBox.confirm(
      t('user.deleteConfirm', { name: row.name }),
      t('common.warning'),
      { type: 'warning' }
    )
    const res = await deleteUser(row.name)
    if (res.success) {
      ElMessage.success(t('common.deleteSuccess'))
      fetchUsers()
    } else {
      ElMessage.error(res.message || t('common.deleteFailed'))
    }
  } catch (error) {
    if (error !== 'cancel') {
      console.error('删除用户失败:', error)
    }
  }
}

// 部署（Agent 和 Client 通用）
const handleDeploy = (row: User) => {
  if (!canWrite.value) return
  selectedUser.value = row
  showDeployDialog.value = true
}

// 启用用户
const handleEnable = async (row: User) => {
  if (!canWrite.value) return
  try {
    await ElMessageBox.confirm(
      t('user.enableConfirm', { name: row.name }),
      t('common.warning'),
      { type: 'warning' }
    )
    const res = await enableUser(row.name)
    if (res.success) {
      ElMessage.success(t('user.enableSuccess'))
      fetchUsers()
    } else {
      ElMessage.error(res.message || t('common.failed'))
    }
  } catch (error) {
    if (error !== 'cancel') {
      console.error('启用用户失败:', error)
    }
  }
}

// 禁用用户
const handleDisable = async (row: User) => {
  if (!canWrite.value) return
  try {
    await ElMessageBox.confirm(
      t('user.disableConfirm', { name: row.name }),
      t('common.warning'),
      { type: 'warning' }
    )
    const res = await disableUser(row.name)
    if (res.success) {
      ElMessage.success(t('user.disableSuccess'))
      fetchUsers()
    } else {
      ElMessage.error(res.message || t('common.failed'))
    }
  } catch (error) {
    if (error !== 'cancel') {
      console.error('禁用用户失败:', error)
    }
  }
}

// 创建成功
const handleCreateSuccess = () => {
  showCreateDialog.value = false
  fetchUsers()
}

// 检查是否需要升级
const needsUpgrade = (versions: string | string[], latestVersion: string) => {
  if (!latestVersion) return false
  
  const versionList = Array.isArray(versions) ? versions : [versions]
  
  // 如果任何一个版本低于最新版本，则需要升级
  return versionList.some(v => compareVersion(v, latestVersion) < 0)
}

// 版本号比较函数
const compareVersion = (v1: string, v2: string): number => {
  if (!v1 || !v2) return 0
  
  const parts1 = v1.split('.').map(Number)
  const parts2 = v2.split('.').map(Number)
  
  const maxLength = Math.max(parts1.length, parts2.length)
  
  for (let i = 0; i < maxLength; i++) {
    const num1 = parts1[i] || 0
    const num2 = parts2[i] || 0
    
    if (num1 > num2) return 1
    if (num1 < num2) return -1
  }
  
  return 0
}

onMounted(() => {
  fetchUsers()
})
</script>

<style scoped>
.user-list {
  width: 100%;
}

.page-header {
  margin-bottom: 14px;
}

.eyebrow {
  margin-bottom: 5px;
  color: var(--text-secondary);
  font-size: 12px;
}

.page-header h1 {
  margin: 0;
  color: var(--text-primary);
  font-size: 24px;
  line-height: 32px;
}

.page-header p {
  margin: 5px 0 0;
  color: var(--text-regular);
  font-size: 13px;
}

.identity-alert {
  margin-bottom: 14px;
}

.search-card {
  margin-bottom: 20px;
}

.search-form {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
}

.pagination-wrapper {
  margin-top: 20px;
  display: flex;
  justify-content: flex-end;
}
</style>
