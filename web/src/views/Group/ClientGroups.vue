<template>
  <div class="group-list">
    <el-card>
      <template #header>
        <div class="card-header">
          <span class="card-title">用户分组</span>
          <el-button type="primary" :icon="Plus" @click="handleCreate">
            创建分组
          </el-button>
        </div>
      </template>
      
      <el-table v-loading="loading" :data="groups" stripe>
        <el-table-column prop="name" label="分组" width="150" />
        <el-table-column prop="alias" label="别名" width="120" />
        <el-table-column label="成员" width="100">
          <template #default="{ row }">
            <el-button link type="primary" @click="handleMembers(row)">
              {{ row.member_count || 0 }}
            </el-button>
          </template>
        </el-table-column>
        <el-table-column prop="description" label="描述" min-width="150" />
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
    </el-card>

    <!-- 创建/编辑对话框 -->
    <el-dialog
      v-model="dialogVisible"
      :title="editingGroup ? '编辑分组' : '创建分组'"
      width="500px"
    >
      <el-form :model="form" label-width="80px">
        <el-form-item label="分组名称" required>
          <el-input v-model="form.name" placeholder="请输入分组名称" :disabled="!!editingGroup" />
        </el-form-item>
        <el-form-item label="别名">
          <el-input v-model="form.alias" placeholder="请输入别名（可选）" />
        </el-form-item>
        <el-form-item label="描述">
          <el-input
            v-model="form.description"
            type="textarea"
            :rows="3"
            placeholder="请输入描述（可选）"
          />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="dialogVisible = false">取消</el-button>
        <el-button type="primary" @click="handleSubmit">确定</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Plus, Delete, Edit } from '@element-plus/icons-vue'
import { getGroups, createGroup, updateGroup, deleteGroup } from '@/api/group'
import type { Group } from '@/api/group'

const router = useRouter()

const loading = ref(false)
const groups = ref<Group[]>([])
const dialogVisible = ref(false)
const editingGroup = ref<Group | null>(null)

const form = ref({
  name: '',
  alias: '',
  description: ''
})

const formatDate = (dateStr: string) => {
  return new Date(dateStr).toLocaleString('zh-CN')
}

const loadGroups = async () => {
  loading.value = true
  try {
    const res = await getGroups()
    if (res.success && res.data) {
      groups.value = res.data
    }
  } catch (error) {
    ElMessage.error('加载失败')
  } finally {
    loading.value = false
  }
}

const handleCreate = () => {
  editingGroup.value = null
  form.value = { name: '', alias: '', description: '' }
  dialogVisible.value = true
}

const handleEdit = (group: Group) => {
  editingGroup.value = group
  form.value = {
    name: group.name,
    alias: group.alias || '',
    description: group.description
  }
  dialogVisible.value = true
}

const handleSubmit = async () => {
  if (!form.value.name) {
    ElMessage.warning('请输入分组名称')
    return
  }

  try {
    if (editingGroup.value) {
      const res = await updateGroup(editingGroup.value.id, {
        alias: form.value.alias,
        description: form.value.description
      })
      if (res.success) {
        ElMessage.success('更新成功')
        dialogVisible.value = false
        loadGroups()
      } else {
        ElMessage.error(res.message || '更新失败')
      }
    } else {
      const res = await createGroup(form.value)
      if (res.success) {
        ElMessage.success('创建成功')
        dialogVisible.value = false
        loadGroups()
      } else {
        ElMessage.error(res.message || '创建失败')
      }
    }
  } catch (error: any) {
    console.error('操作失败:', error)
    ElMessage.error(error.message || error.response?.data?.message || '操作失败')
  }
}

const handleMembers = (group: Group) => {
  router.push({
    name: 'ClientGroupMembers',
    params: { id: group.id }
  })
}

const handleDelete = async (group: Group) => {
  try {
    await ElMessageBox.confirm(`确定要删除分组 "${group.name}" 吗？`, {
      type: 'warning'
    })
    
    const res = await deleteGroup(group.id)
    if (res.success) {
      ElMessage.success('删除成功')
      loadGroups()
    }
  } catch (error: any) {
    if (error !== 'cancel') {
      ElMessage.error('删除失败')
    }
  }
}

onMounted(() => {
  loadGroups()
})
</script>

<style scoped>
.group-list {
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
  color: var(--text-primary);
}
</style>
