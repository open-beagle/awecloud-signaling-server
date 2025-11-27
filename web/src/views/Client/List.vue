<template>
  <div class="client-list">
    <div class="header">
      <h2>Client 管理</h2>
      <el-button type="primary" @click="showCreateDialog">
        <el-icon><Plus /></el-icon>
        创建 Client
      </el-button>
    </div>

    <el-table :data="clients" style="width: 100%" v-loading="loading">
      <el-table-column prop="id" label="ID" width="80" />
      <el-table-column prop="client_id" label="Client ID" min-width="200" />
      <el-table-column label="状态" width="100">
        <template #default="{ row }">
          <el-tag :type="row.enabled ? 'success' : 'danger'">
            {{ row.enabled ? '启用' : '禁用' }}
          </el-tag>
        </template>
      </el-table-column>
      <el-table-column prop="created_at" label="创建时间" width="180">
        <template #default="{ row }">
          {{ formatDate(row.created_at) }}
        </template>
      </el-table-column>
      <el-table-column label="操作" width="300" fixed="right">
        <template #default="{ row }">
          <el-button
            v-if="!row.enabled"
            size="small"
            type="success"
            @click="handleEnable(row)"
          >
            启用
          </el-button>
          <el-button
            v-else
            size="small"
            type="warning"
            @click="handleDisable(row)"
          >
            禁用
          </el-button>
          <el-button
            size="small"
            type="primary"
            @click="handleRegenerateSecret(row)"
          >
            重新生成Secret
          </el-button>
          <el-button
            size="small"
            type="danger"
            @click="handleDelete(row)"
          >
            删除
          </el-button>
        </template>
      </el-table-column>
    </el-table>

    <!-- 创建Client对话框 -->
    <CreateDialog
      v-model="createDialogVisible"
      @success="handleCreateSuccess"
    />

    <!-- 显示Secret对话框 -->
    <SecretDialog
      v-model="secretDialogVisible"
      :client-id="currentClientId"
      :client-secret="currentSecret"
    />
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Plus } from '@element-plus/icons-vue'
import { getClients, enableClient, disableClient, deleteClient, regenerateSecret, type Client } from '@/api/client'
import CreateDialog from './components/CreateDialog.vue'
import SecretDialog from './components/SecretDialog.vue'

const clients = ref<Client[]>([])
const loading = ref(false)
const createDialogVisible = ref(false)
const secretDialogVisible = ref(false)
const currentClientId = ref('')
const currentSecret = ref('')

const loadClients = async () => {
  loading.value = true
  try {
    const res = await getClients()
    clients.value = res.clients || []
  } catch (error: any) {
    ElMessage.error(error.message || '加载Client列表失败')
  } finally {
    loading.value = false
  }
}

const showCreateDialog = () => {
  createDialogVisible.value = true
}

const handleCreateSuccess = (clientId: string, secret: string) => {
  currentClientId.value = clientId
  currentSecret.value = secret
  secretDialogVisible.value = true
  loadClients()
}

const handleEnable = async (row: Client) => {
  try {
    await enableClient(row.id)
    ElMessage.success('启用成功')
    loadClients()
  } catch (error: any) {
    ElMessage.error(error.message || '启用失败')
  }
}

const handleDisable = async (row: Client) => {
  try {
    await ElMessageBox.confirm('确定要禁用此Client吗？', '提示', {
      type: 'warning'
    })
    await disableClient(row.id)
    ElMessage.success('禁用成功')
    loadClients()
  } catch (error: any) {
    if (error !== 'cancel') {
      ElMessage.error(error.message || '禁用失败')
    }
  }
}

const handleDelete = async (row: Client) => {
  try {
    await ElMessageBox.confirm(
      `确定要删除Client "${row.client_id}" 吗？此操作不可恢复！`,
      '警告',
      {
        type: 'warning',
        confirmButtonText: '确定删除',
        cancelButtonText: '取消'
      }
    )
    await deleteClient(row.id)
    ElMessage.success('删除成功')
    loadClients()
  } catch (error: any) {
    if (error !== 'cancel') {
      ElMessage.error(error.message || '删除失败')
    }
  }
}

const handleRegenerateSecret = async (row: Client) => {
  try {
    await ElMessageBox.confirm(
      '重新生成Secret后，旧的Secret将失效，确定继续吗？',
      '警告',
      {
        type: 'warning'
      }
    )
    const res = await regenerateSecret(row.id)
    currentClientId.value = row.client_id
    currentSecret.value = res.client_secret
    secretDialogVisible.value = true
    ElMessage.success('Secret已重新生成')
  } catch (error: any) {
    if (error !== 'cancel') {
      ElMessage.error(error.message || '重新生成失败')
    }
  }
}

const formatDate = (dateStr: string) => {
  return new Date(dateStr).toLocaleString('zh-CN')
}

onMounted(() => {
  loadClients()
})
</script>

<style scoped>
.client-list {
  padding: 20px;
}

.header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 20px;
}

.header h2 {
  margin: 0;
}
</style>
