<template>
  <div class="tunnel-nodes">
    <el-card>
      <template #header>
        <div class="card-header">
          <span class="card-title">Node 管理</span>
        </div>
      </template>

      <!-- 筛选栏 -->
      <div class="filter-bar">
        <el-select v-model="filters.status" placeholder="所有状态" clearable @change="loadNodes">
          <el-option label="所有状态" value="" />
          <el-option label="在线" value="online" />
          <el-option label="离线" value="offline" />
        </el-select>
        <el-input
          v-model="filters.search"
          placeholder="搜索名称"
          clearable
          style="width: 200px"
          @keyup.enter="loadNodes"
        >
          <template #prefix>
            <el-icon><Search /></el-icon>
          </template>
        </el-input>
      </div>

      <el-table v-loading="loading" :data="nodes" stripe>
        <el-table-column prop="id" label="ID" width="80" />
        <el-table-column prop="name" label="名称" min-width="120" />
        <el-table-column label="所属User" min-width="150">
          <template #default="{ row }">
            <router-link :to="`/tunnel/users?search=${row.user_name}`" class="link">
              {{ row.user_name }}
            </router-link>
          </template>
        </el-table-column>
        <el-table-column prop="ip_address" label="IP地址" width="130" />
        <el-table-column label="状态" width="80">
          <template #default="{ row }">
            <el-tag :type="row.online ? 'success' : 'danger'" size="small">
              {{ row.online ? '在线' : '离线' }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="Tags" min-width="200">
          <template #default="{ row }">
            <el-tag v-for="tag in row.tags" :key="tag" size="small" style="margin-right: 4px">
              {{ tag }}
            </el-tag>
            <span v-if="!row.tags?.length">-</span>
          </template>
        </el-table-column>
        <el-table-column label="最后在线" width="120">
          <template #default="{ row }">
            {{ formatRelativeTime(row.last_seen) }}
          </template>
        </el-table-column>
        <el-table-column label="操作" width="150" fixed="right">
          <template #default="{ row }">
            <el-tooltip content="编辑" placement="top">
              <el-button size="small" :icon="Edit" @click="handleEdit(row)" />
            </el-tooltip>
            <el-tooltip content="Tags" placement="top">
              <el-button size="small" :icon="PriceTag" @click="handleTags(row)" />
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
          @size-change="loadNodes"
          @current-change="loadNodes"
        />
      </div>
    </el-card>

    <!-- 编辑对话框 -->
    <el-dialog v-model="editDialogVisible" title="编辑 Node" width="500px">
      <el-form :model="editForm" label-width="100px">
        <el-form-item label="Node ID">
          <span>{{ editForm.id }}</span>
        </el-form-item>
        <el-form-item label="所属 User">
          <span>{{ editForm.user_name }}</span>
        </el-form-item>
        <el-form-item label="IP 地址">
          <span>{{ editForm.ip_address }}</span>
        </el-form-item>
        <el-form-item label="显示名称">
          <el-input v-model="editForm.given_name" placeholder="请输入显示名称" />
        </el-form-item>
      </el-form>
      <div class="dialog-tip">
        <el-icon><Warning /></el-icon>
        修改显示名称不会影响本地 Desktop 的设备名称
      </div>
      <template #footer>
        <el-button @click="editDialogVisible = false">取消</el-button>
        <el-button type="primary" @click="submitEdit">保存</el-button>
      </template>
    </el-dialog>

    <!-- Tags 管理对话框 -->
    <el-dialog v-model="tagsDialogVisible" title="管理 Tags" width="600px">
      <el-form :model="tagsForm" label-width="100px">
        <el-form-item label="Node ID">
          <span>{{ tagsForm.id }}</span>
        </el-form-item>
        <el-form-item label="所属 User">
          <span>{{ tagsForm.user_name }}</span>
        </el-form-item>
        <el-form-item label="当前 Tags">
          <div class="tags-container">
            <el-tag
              v-for="tag in tagsForm.tags"
              :key="tag"
              closable
              @close="removeTag(tag)"
              style="margin-right: 8px; margin-bottom: 8px"
            >
              {{ tag }}
            </el-tag>
            <span v-if="!tagsForm.tags.length" class="no-tags">无</span>
          </div>
        </el-form-item>
        <el-form-item label="添加 Tag">
          <div class="add-tag-row">
            <el-input
              v-model="newTag"
              placeholder="tag:xxx"
              style="width: 300px"
              @keyup.enter="addTag"
            />
            <el-button type="primary" @click="addTag">添加</el-button>
          </div>
        </el-form-item>
        <el-form-item label="常用 Tags">
          <div class="common-tags">
            <el-tag
              v-for="opt in commonTags"
              :key="opt.tag"
              :type="tagsForm.tags.includes(opt.tag) ? 'success' : 'info'"
              style="margin-right: 8px; margin-bottom: 8px; cursor: pointer"
              @click="toggleCommonTag(opt.tag)"
            >
              {{ opt.tag }} ({{ opt.count }})
            </el-tag>
            <span v-if="!commonTags.length" class="no-tags">无</span>
          </div>
        </el-form-item>
      </el-form>
      <div class="dialog-tip">
        <el-icon><Warning /></el-icon>
        手动修改 Tags 可能导致与本地分组数据不一致
      </div>
      <template #footer>
        <el-button @click="tagsDialogVisible = false">取消</el-button>
        <el-button type="primary" @click="submitTags">保存</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, onMounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Search, Edit, Delete, PriceTag, Warning } from '@element-plus/icons-vue'
import {
  getTunnelNodes,
  getTunnelTags,
  updateTunnelNode,
  updateTunnelNodeTags,
  deleteTunnelNode,
  type TunnelNode,
  type TagOption
} from '@/api/tunnel'

const loading = ref(false)
const nodes = ref<TunnelNode[]>([])
const filters = reactive({
  status: '',
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
  user_name: '',
  ip_address: '',
  given_name: ''
})

// Tags 管理相关
const tagsDialogVisible = ref(false)
const tagsForm = reactive({
  id: 0,
  user_name: '',
  tags: [] as string[]
})
const newTag = ref('')
const commonTags = ref<TagOption[]>([])

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
  return new Date(dateStr).toLocaleString('zh-CN')
}

const loadNodes = async () => {
  loading.value = true
  try {
    const res = await getTunnelNodes({
      status: filters.status || undefined,
      search: filters.search || undefined,
      page: pagination.page,
      size: pagination.size
    })
    if (res.success) {
      nodes.value = res.data || []
      pagination.total = res.total || 0
    }
  } catch (error) {
    ElMessage.error('加载失败')
  } finally {
    loading.value = false
  }
}

const loadCommonTags = async () => {
  try {
    const res = await getTunnelTags()
    if (res.success) {
      commonTags.value = res.data || []
    }
  } catch (error) {
    console.error('加载常用 Tags 失败:', error)
  }
}

const handleEdit = (node: TunnelNode) => {
  editForm.id = node.id
  editForm.user_name = node.user_name
  editForm.ip_address = node.ip_address
  editForm.given_name = node.name
  editDialogVisible.value = true
}

const submitEdit = async () => {
  try {
    const res = await updateTunnelNode(editForm.id, {
      given_name: editForm.given_name
    })
    if (res.success) {
      ElMessage.success('更新成功')
      editDialogVisible.value = false
      loadNodes()
    } else {
      ElMessage.error(res.message || '更新失败')
    }
  } catch (error: any) {
    ElMessage.error(error.message || '更新失败')
  }
}

const handleTags = (node: TunnelNode) => {
  tagsForm.id = node.id
  tagsForm.user_name = node.user_name
  tagsForm.tags = [...(node.tags || [])]
  newTag.value = ''
  tagsDialogVisible.value = true
}

const addTag = () => {
  let tag = newTag.value.trim()
  if (!tag) return

  if (!tag.startsWith('tag:')) {
    tag = 'tag:' + tag
  }

  if (tagsForm.tags.includes(tag)) {
    ElMessage.warning('Tag 已存在')
    return
  }

  tagsForm.tags.push(tag)
  newTag.value = ''
}

const removeTag = (tag: string) => {
  const index = tagsForm.tags.indexOf(tag)
  if (index > -1) {
    tagsForm.tags.splice(index, 1)
  }
}

const toggleCommonTag = (tag: string) => {
  if (tagsForm.tags.includes(tag)) {
    removeTag(tag)
  } else {
    tagsForm.tags.push(tag)
  }
}

const submitTags = async () => {
  try {
    const res = await updateTunnelNodeTags(tagsForm.id, tagsForm.tags)
    if (res.success) {
      ElMessage.success('更新成功')
      tagsDialogVisible.value = false
      loadNodes()
    } else {
      ElMessage.error(res.message || '更新失败')
    }
  } catch (error: any) {
    ElMessage.error(error.message || '更新失败')
  }
}

const handleDelete = async (node: TunnelNode) => {
  try {
    await ElMessageBox.confirm(
      `确定要删除 Node "${node.name}" 吗？\n\nNode 信息：\n• 所属 User: ${node.user_name}\n• IP 地址: ${node.ip_address}\n• 状态: ${node.online ? '在线' : '离线'}\n\n此操作将：\n• 从 Headscale 删除该 Node\n• 如果关联本地 Desktop，将同步删除本地记录\n• 该设备需要重新认证才能连接\n\n此操作不可恢复！`,
      '删除 Node',
      { type: 'warning', confirmButtonText: '确认删除', cancelButtonText: '取消' }
    )

    const res = await deleteTunnelNode(node.id)
    if (res.success) {
      ElMessage.success('删除成功')
      loadNodes()
    } else {
      ElMessage.error(res.message || '删除失败')
    }
  } catch (error: any) {
    if (error !== 'cancel') {
      ElMessage.error(error.message || '删除失败')
    }
  }
}

onMounted(() => {
  loadNodes()
  loadCommonTags()
})
</script>

<style scoped>
.tunnel-nodes {
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

.tags-container {
  min-height: 32px;
}

.add-tag-row {
  display: flex;
  gap: 12px;
}

.common-tags {
  min-height: 32px;
}

.no-tags {
  color: var(--el-text-color-secondary);
}
</style>
