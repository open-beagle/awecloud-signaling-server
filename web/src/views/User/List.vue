<template>
  <div class="user-list">
    <!-- 搜索和筛选 -->
    <el-card class="search-card" shadow="never">
      <el-form :inline="true" :model="searchForm" class="search-form">
        <el-form-item :label="$t('user.role')">
          <el-select v-model="searchForm.role" :placeholder="$t('common.all')" clearable style="width: 120px">
            <el-option :label="$t('user.roleAgent')" value="agent" />
            <el-option :label="$t('user.roleClient')" value="client" />
          </el-select>
        </el-form-item>
        <el-form-item :label="$t('common.search')">
          <el-input v-model="searchForm.search" :placeholder="$t('user.searchPlaceholder')" clearable style="width: 200px" @keyup.enter="handleSearch" />
        </el-form-item>
        <el-form-item>
          <el-button type="primary" @click="handleSearch">{{ $t('common.search') }}</el-button>
          <el-button @click="handleReset">{{ $t('common.reset') }}</el-button>
        </el-form-item>
        <el-form-item style="float: right">
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
        <el-table-column prop="ssh_enabled" :label="$t('user.sshEnabled')" width="100" align="center">
          <template #default="{ row }">
            <el-tag v-if="row.role === 'agent'" :type="row.ssh_enabled ? 'success' : 'info'" size="small">
              {{ row.ssh_enabled ? $t('common.enabled') : $t('common.disabled') }}
            </el-tag>
            <span v-else>-</span>
          </template>
        </el-table-column>
        <el-table-column prop="created_at" :label="$t('common.createdAt')" width="180">
          <template #default="{ row }">
            {{ formatTime(row.created_at) }}
          </template>
        </el-table-column>
        <el-table-column :label="$t('common.actions')" width="200" fixed="right">
          <template #default="{ row }">
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
import { ref, reactive, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Plus, Upload } from '@element-plus/icons-vue'
import { useI18n } from 'vue-i18n'
import { getUsers, deleteUser, type User, type UserRole } from '@/api/user'
import { formatTime } from '@/utils/time'
import CreateDialog from './components/CreateDialog.vue'
import DeployDialog from './components/DeployDialog.vue'

const { t } = useI18n()
const router = useRouter()

const loading = ref(false)
const users = ref<User[]>([])
const showCreateDialog = ref(false)
const showDeployDialog = ref(false)
const selectedUser = ref<User | null>(null)

const searchForm = reactive({
  role: '' as UserRole | '',
  search: ''
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
  pagination.page = 1
  fetchUsers()
}

// 查看详情
const handleView = (row: User) => {
  router.push(`/users/${row.name}`)
}

// 编辑
const handleEdit = (row: User) => {
  router.push(`/users/${row.name}?edit=true`)
}

// 删除
const handleDelete = async (row: User) => {
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
  selectedUser.value = row
  showDeployDialog.value = true
}

// 创建成功
const handleCreateSuccess = () => {
  showCreateDialog.value = false
  fetchUsers()
}

onMounted(() => {
  fetchUsers()
})
</script>

<style scoped>
.user-list {
  width: 100%;
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
