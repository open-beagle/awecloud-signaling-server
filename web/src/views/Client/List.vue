<template>
  <div class="client-list">
    <el-card>
      <template #header>
        <div class="card-header">
          <span class="card-title">{{ t('client.list') }}</span>
          <el-button type="primary" :icon="Plus" @click="showCreateDialog">
            {{ t('client.create') }}
          </el-button>
        </div>
      </template>
      
      <el-table v-loading="loading" :data="clients" stripe>
        <el-table-column prop="id" label="ID" width="80" />
        <el-table-column prop="name" label="用户名" min-width="150">
          <template #default="{ row }">
            <el-link type="primary" :underline="false" @click="handleViewDetail(row)">
              {{ row.name }}
              <span v-if="row.alias" class="alias-text">({{ row.alias }})</span>
            </el-link>
          </template>
        </el-table-column>
        <el-table-column label="客户端" width="100">
          <template #default="{ row }">
            <el-link type="primary" :underline="false" @click="handleViewDetail(row)">
              {{ row.desktop_count || 0 }}
            </el-link>
          </template>
        </el-table-column>
        <el-table-column :label="t('common.status')" width="100">
          <template #default="{ row }">
            <el-tag :type="row.status === 'online' ? 'success' : 'info'" size="small">
              {{ row.status === 'online' ? t('common.online') : t('common.offline') }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="最后在线" width="120">
          <template #default="{ row }">
            <TimeAgo v-if="row.last_online" :time="row.last_online" />
            <span v-else style="color: var(--el-text-color-secondary)">-</span>
          </template>
        </el-table-column>
        <el-table-column :label="t('common.actions')" width="120" fixed="right">
          <template #default="{ row }">
            <el-tooltip content="详情" placement="top">
              <el-button
                size="small"
                :icon="View"
                @click="handleViewDetail(row)"
              />
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

    <CreateDialog
      v-model="createDialogVisible"
      @success="handleCreateSuccess"
    />
    <SecretDialog
      v-model="secretDialogVisible"
      :client-id="currentClientId"
      :client-secret="currentSecret"
    />
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Plus, Delete, View } from '@element-plus/icons-vue'
import { getClients, deleteClient, type Client } from '@/api/client'
import TimeAgo from '@/components/Common/TimeAgo.vue'
import CreateDialog from './components/CreateDialog.vue'
import SecretDialog from './components/SecretDialog.vue'

const { t } = useI18n()
const router = useRouter()

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
    clients.value = res.data || []
  } catch (error: any) {
    ElMessage.error(t('common.failed'))
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

const handleDelete = async (row: Client) => {
  try {
    await ElMessageBox.confirm(t('client.deleteConfirm'), {
      type: 'warning'
    })
    await deleteClient(row.id)
    ElMessage.success(t('common.deleteSuccess'))
    loadClients()
  } catch (error: any) {
    if (error !== 'cancel') {
      ElMessage.error(t('common.failed'))
    }
  }
}

const handleViewDetail = (row: Client) => {
  router.push(`/clients/${row.id}`)
}

onMounted(() => {
  loadClients()
})
</script>

<style scoped>
.client-list {
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

.alias-text {
  color: #909399;
  font-size: 12px;
  margin-left: 4px;
}
</style>
