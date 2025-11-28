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
        <el-table-column prop="client_id" :label="t('client.clientId')" min-width="200" />
        <el-table-column :label="t('client.enabled')" width="100">
          <template #default="{ row }">
            <el-tag :type="row.enabled ? 'success' : 'danger'" size="small">
              {{ row.enabled ? t('client.enable') : t('client.disable') }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column :label="t('client.createdAt')" width="100">
          <template #default="{ row }">
            <TimeAgo :time="row.created_at" />
          </template>
        </el-table-column>
        <el-table-column :label="t('common.actions')" width="280" fixed="right">
          <template #default="{ row }">
            <el-button
              v-if="!row.enabled"
              size="small"
              type="success"
              @click="handleEnable(row)"
            >
              {{ t('client.enable') }}
            </el-button>
            <el-button
              v-else
              size="small"
              type="warning"
              @click="handleDisable(row)"
            >
              {{ t('client.disable') }}
            </el-button>
            <el-button
              size="small"
              @click="handleRegenerateSecret(row)"
            >
              {{ t('client.regenerateSecret') }}
            </el-button>
            <el-button
              size="small"
              type="danger"
              :icon="Delete"
              @click="handleDelete(row)"
            />
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
import { ElMessage, ElMessageBox } from 'element-plus'
import { Plus, Delete } from '@element-plus/icons-vue'
import { getClients, enableClient, disableClient, deleteClient, regenerateSecret, type Client } from '@/api/client'
import TimeAgo from '@/components/Common/TimeAgo.vue'
import CreateDialog from './components/CreateDialog.vue'
import SecretDialog from './components/SecretDialog.vue'

const { t } = useI18n()

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

const handleEnable = async (row: Client) => {
  try {
    await ElMessageBox.confirm(t('client.enableConfirm'), {
      type: 'warning'
    })
    await enableClient(row.id)
    ElMessage.success(t('common.success'))
    loadClients()
  } catch (error: any) {
    if (error !== 'cancel') {
      ElMessage.error(t('common.failed'))
    }
  }
}

const handleDisable = async (row: Client) => {
  try {
    await ElMessageBox.confirm(t('client.disableConfirm'), {
      type: 'warning'
    })
    await disableClient(row.id)
    ElMessage.success(t('common.success'))
    loadClients()
  } catch (error: any) {
    if (error !== 'cancel') {
      ElMessage.error(t('common.failed'))
    }
  }
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

const handleRegenerateSecret = async (row: Client) => {
  try {
    await ElMessageBox.confirm(t('client.regenerateConfirm'), {
      type: 'warning'
    })
    const res = await regenerateSecret(row.id)
    currentClientId.value = row.client_id
    currentSecret.value = res.client_secret
    secretDialogVisible.value = true
    ElMessage.success(t('common.success'))
  } catch (error: any) {
    if (error !== 'cancel') {
      ElMessage.error(t('common.failed'))
    }
  }
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
</style>
