<template>
  <div class="tunnel-users">
    <el-card>
      <template #header>
        <div class="card-header">
          <span class="card-title">User 管理</span>
        </div>
      </template>

      <!-- 筛选栏 -->
      <div class="filter-bar">
        <el-select v-model="filters.type" placeholder="所有类型" clearable @change="loadUsers">
          <el-option label="所有类型" value="" />
          <el-option label="Agent" value="agent" />
          <el-option label="Client" value="client" />
          <el-option label="孤立" value="orphan" />
        </el-select>
        <el-input
          v-model="filters.search"
          placeholder="搜索名称"
          clearable
          style="width: 200px"
          @keyup.enter="loadUsers"
        >
          <template #prefix>
            <el-icon><Search /></el-icon>
          </template>
        </el-input>
      </div>

      <el-table v-loading="loading" :data="users" stripe>
        <el-table-column prop="id" label="ID" width="80" />
        <el-table-column prop="name" label="名称" min-width="150" />
        <el-table-column prop="display_name" label="显示名称" min-width="120" />
        <el-table-column label="类型" width="100">
          <template #default="{ row }">
            <el-tag :type="getTypeTagType(row.type)" size="small">
              {{ getTypeLabel(row.type) }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="关联实体" min-width="120">
          <template #default="{ row }">
            <template v-if="row.linked_id">
              <router-link
                v-if="row.type === 'agent'"
                :to="{ path: `/agents/${row.linked_id}`, query: { name: row.linked_entity } }"
                class="link"
              >
                {{ row.linked_entity }}
              </router-link>
              <router-link
                v-else-if="row.type === 'client'"
                :to="{ path: `/clients/${row.linked_id}`, query: { name: row.linked_entity } }"
                class="link"
              >
                {{ row.linked_entity }}
              </router-link>
              <span v-else>{{ row.linked_entity || '-' }}</span>
            </template>
            <span v-else>-</span>
          </template>
        </el-table-column>
        <el-table-column label="Node数" width="80">
          <template #default="{ row }">
            <el-button link type="primary" @click="viewNodes(row)">
              {{ row.node_count }}
            </el-button>
          </template>
        </el-table-column>
        <el-table-column label="创建时间" width="180">
          <template #default="{ row }">
            {{ formatDate(row.created_at) }}
          </template>
        </el-table-column>
        <el-table-column label="操作" width="120" fixed="right">
          <template #default="{ row }">
            <el-tooltip content="编辑" placement="top">
              <el-button size="small" :icon="Edit" @click="handleEdit(row)" />
            </el-tooltip>
            <el-tooltip content="删除" placement="top">
              <el-button
                size="small"
                type="danger"
                :icon="Delete"
                @click="handleDelete(row)"
              />
            </el-tooltip>
          </template>
        </el-table-column>
      </el-table>

      <!-- 分页 -->
      <div class="pagination-wrapper">
        <el-pagination
          v-model:current-page="pagination.page"
          v-model:page-size="pagination.size"
          :total="pagination.total"
          :page-sizes="[10, 20, 50]"
          layout="total, sizes, prev, pager, next"
          @size-change="loadUsers"
          @current-change="loadUsers"
        />
      </div>
    </el-card>

    <!-- 编辑对话框 -->
    <el-dialog v-model="editDialogVisible" title="编辑 User" width="500px">
      <el-form :model="editForm" label-width="100px">
        <el-form-item label="User ID">
          <span>{{ editForm.id }}</span>
        </el-form-item>
        <el-form-item label="User Name">
          <span>{{ editForm.name }}</span>
        </el-form-item>
        <el-form-item label="显示名称">
          <el-input v-model="editForm.display_name" placeholder="请输入显示名称" />
        </el-form-item>
      </el-form>
      <div class="dialog-tip">
        <el-icon><Warning /></el-icon>
        修改显示名称不会影响本地 Agent/Client 的别名
      </div>
      <template #footer>
        <el-button @click="editDialogVisible = false">取消</el-button>
        <el-button type="primary" @click="submitEdit">保存</el-button>
      </template>
    </el-dialog>

    <!-- Node 列表对话框 -->
    <el-dialog v-model="nodesDialogVisible" :title="`User: ${selectedUser?.name} 的 Node 列表`" width="800px">
      <el-table :data="userNodes" stripe>
        <el-table-column prop="id" label="ID" width="80" />
        <el-table-column prop="name" label="名称" min-width="120" />
        <el-table-column prop="ip_address" label="IP地址" width="130" />
        <el-table-column label="状态" width="80">
          <template #default="{ row }">
            <el-tag :type="row.online ? 'success' : 'danger'" size="small">
              {{ row.online ? '在线' : '离线' }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="Tags" min-width="150">
          <template #default="{ row }">
            <el-tag v-for="tag in row.tags" :key="tag" size="small" style="margin-right: 4px">
              {{ tag }}
            </el-tag>
            <span v-if="!row.tags?.length">-</span>
          </template>
        </el-table-column>
        <el-table-column label="最后在线" width="150">
          <template #default="{ row }">
            {{ formatRelativeTime(row.last_seen) }}
          </template>
        </el-table-column>
      </el-table>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Search, Edit, Delete, Warning } from '@element-plus/icons-vue'
import {
  getTunnelUsers,
  getTunnelUserNodes,
  updateTunnelUser,
  deleteTunnelUser,
  type TunnelUser,
  type TunnelNode
} from '@/api/tunnel'

const router = useRouter()

const loading = ref(false)
const users = ref<TunnelUser[]>([])
const filters = reactive({
  type: '',
  search: ''
})
const pagination = reactive({
  page: 1,
  size: 20,
  total: 0
})

// 编辑相关
const editDialogVisible = ref(false)
const editForm = reactive({
  id: 0,
  name: '',
  display_name: ''
})

// Node 列表相关
const nodesDialogVisible = ref(false)
const selectedUser = ref<TunnelUser | null>(null)
const userNodes = ref<TunnelNode[]>([])

const formatDate = (dateStr: string) => {
  if (!dateStr) return '-'
  return new Date(dateStr).toLocaleString('zh-CN')
}

const formatRelativeTime = (dateStr: string) => {
  if (!dateStr) return '-'
  const date = new Date(dateStr)
  const now = new Date()
  const diff = now.getTime() - date.getTime()
  const minutes = Math.floor(diff / 60000)
  if (minutes < 1) return '刚刚'
  if (minutes < 60) return `${minutes}分钟前`
  const hours = Math.floor(minutes / 60)
  if (hours < 24) return `${hours}小时前`
  const days = Math.floor(hours / 24)
  if (days < 30) return `${days}天前`
  return formatDate(dateStr)
}

const getTypeTagType = (type: string) => {
  switch (type) {
    case 'agent': return 'primary'
    case 'client': return 'success'
    case 'orphan': return 'warning'
    default: return 'info'
  }
}

const getTypeLabel = (type: string) => {
  switch (type) {
    case 'agent': return 'Agent'
    case 'client': return 'Client'
    case 'orphan': return '孤立'
    default: return '未知'
  }
}

const loadUsers = async () => {
  loading.value = true
  try {
    const res = await getTunnelUsers({
      type: filters.type || undefined,
      search: filters.search || undefined,
      page: pagination.page,
      size: pagination.size
    })
    if (res.success) {
      users.value = res.data || []
      pagination.total = res.total || 0
    }
  } catch (error) {
    ElMessage.error('加载失败')
  } finally {
    loading.value = false
  }
}

const handleEdit = (user: TunnelUser) => {
  editForm.id = user.id
  editForm.name = user.name
  editForm.display_name = user.display_name
  editDialogVisible.value = true
}

const submitEdit = async () => {
  try {
    const res = await updateTunnelUser(editForm.id, {
      display_name: editForm.display_name
    })
    if (res.success) {
      ElMessage.success('更新成功')
      editDialogVisible.value = false
      loadUsers()
    } else {
      ElMessage.error(res.message || '更新失败')
    }
  } catch (error: any) {
    ElMessage.error(error.message || '更新失败')
  }
}

const handleDelete = async (user: TunnelUser) => {
  try {
    await ElMessageBox.confirm(
      `确定要删除 User "${user.name}" 吗？\n\n此操作将：\n• 删除该 User 下的所有 Node（共 ${user.node_count} 个）\n• 删除该 User 的所有 PreAuthKey\n• 如果关联本地 Agent/Client，将同步删除本地记录\n\n此操作不可恢复！`,
      '删除 User',
      { type: 'warning', confirmButtonText: '确认删除', cancelButtonText: '取消' }
    )

    const res = await deleteTunnelUser(user.id)
    if (res.success) {
      ElMessage.success('删除成功')
      loadUsers()
    } else {
      ElMessage.error(res.message || '删除失败')
    }
  } catch (error: any) {
    if (error !== 'cancel') {
      ElMessage.error(error.message || '删除失败')
    }
  }
}

const viewNodes = async (user: TunnelUser) => {
  selectedUser.value = user
  try {
    const res = await getTunnelUserNodes(user.id)
    if (res.success) {
      userNodes.value = res.data || []
      nodesDialogVisible.value = true
    }
  } catch (error) {
    ElMessage.error('获取 Node 列表失败')
  }
}

onMounted(() => {
  loadUsers()
})
</script>

<style scoped>
.tunnel-users {
  width: 100%;
}

.card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.card-title {
  font-size: 18px;
  font-weight: 500;
}

.filter-bar {
  display: flex;
  gap: 12px;
  margin-bottom: 16px;
}

.pagination-wrapper {
  margin-top: 16px;
  display: flex;
  justify-content: flex-end;
}

.link {
  color: var(--el-color-primary);
  text-decoration: none;
}

.link:hover {
  text-decoration: underline;
}

.dialog-tip {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 12px;
  background: var(--el-color-warning-light-9);
  border-radius: 4px;
  color: var(--el-color-warning);
  font-size: 13px;
}
</style>
