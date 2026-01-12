<template>
  <div class="client-detail">
    <el-card v-loading="loading">
      <template #header>
        <div class="card-header">
          <div class="header-left">
            <el-button :icon="ArrowLeft" @click="goBack" circle />
            <span class="page-title">{{ t('client.detail') }}: {{ clientDetail?.client_id }}</span>
          </div>
          <el-button type="primary" :icon="Refresh" @click="loadClientDetail">
            {{ t('common.refresh') }}
          </el-button>
        </div>
      </template>

      <!-- 基本信息 -->
      <div class="info-section">
        <div class="section-header">
          <h3>{{ t('client.basicInfo') }}</h3>
          <el-button size="small" :icon="Edit" @click="showEditDialog">
            {{ t('common.edit') }}
          </el-button>
        </div>
        <div class="info-grid">
          <div class="info-item">
            <label>{{ t('client.clientId') }}:</label>
            <span>{{ clientDetail?.client_id }}</span>
          </div>
          <div class="info-item">
            <label>{{ t('client.enabled') }}:</label>
            <el-tag :type="clientDetail?.enabled ? 'success' : 'danger'" size="small">
              {{ clientDetail?.enabled ? t('client.enable') : t('client.disable') }}
            </el-tag>
          </div>
          <div class="info-item">
            <label>{{ t('agent.tailscaleIp') }}:</label>
            <span>{{ clientDetail?.tailscale_ip || '-' }}</span>
          </div>
          <div class="info-item">
            <label>{{ t('client.createdAt') }}:</label>
            <span>{{ formatTime(clientDetail?.created_at) }}</span>
          </div>
        </div>
      </div>

      <!-- 所属分组 -->
      <div class="info-section" v-if="clientDetail?.groups && clientDetail.groups.length > 0">
        <div class="section-header">
          <h3>{{ t('client.groups') }}</h3>
        </div>
        <div class="groups-container">
          <el-tag
            v-for="group in clientDetail.groups"
            :key="group"
            type="info"
            class="group-tag"
          >
            {{ group }}
          </el-tag>
        </div>
      </div>

      <!-- 客户端设备 -->
      <div class="info-section">
        <div class="section-header">
          <h3>{{ t('client.desktops') }} ({{ clientDetail?.desktops?.length || 0 }})</h3>
        </div>
        <el-table :data="clientDetail?.desktops" stripe>
          <el-table-column prop="id" label="ID" width="80" />
          <el-table-column prop="device_name" :label="t('client.deviceName')" min-width="150" />
          <el-table-column prop="tailscale_ip" :label="t('agent.tailscaleIp')" width="150">
            <template #default="{ row }">
              <span>{{ row.tailscale_ip || '-' }}</span>
            </template>
          </el-table-column>
          <el-table-column :label="t('common.status')" width="100">
            <template #default="{ row }">
              <el-tag :type="row.online ? 'success' : 'danger'" size="small">
                {{ row.online ? t('common.online') : t('common.offline') }}
              </el-tag>
            </template>
          </el-table-column>
          <el-table-column :label="t('client.lastSeen')" width="150">
            <template #default="{ row }">
              <TimeAgo :time="row.last_seen_at" />
            </template>
          </el-table-column>
          <el-table-column :label="t('client.createdAt')" width="150">
            <template #default="{ row }">
              <TimeAgo :time="row.created_at" />
            </template>
          </el-table-column>
          <el-table-column :label="t('common.actions')" width="100" fixed="right">
            <template #default="{ row }">
              <el-tooltip :content="t('client.revokeDevice')" placement="top">
                <el-button
                  size="small"
                  type="danger"
                  :icon="Delete"
                  @click="handleRevokeDesktop(row)"
                />
              </el-tooltip>
            </template>
          </el-table-column>
        </el-table>
      </div>

      <!-- 已授权服务 -->
      <div class="info-section">
        <div class="section-header">
          <h3>{{ t('client.authorizedServices') }} ({{ clientDetail?.permissions?.length || 0 }})</h3>
        </div>
        <el-table :data="clientDetail?.permissions" stripe>
          <el-table-column prop="service_name" :label="t('service.serviceName')" min-width="150" />
          <el-table-column prop="agent_name" :label="t('agent.name')" width="120" />
          <el-table-column prop="access_address" :label="t('service.accessAddress')" min-width="180" />
          <el-table-column :label="t('service.grantType')" width="120">
            <template #default="{ row }">
              <el-tag :type="row.grant_type === 'direct' ? 'primary' : 'warning'" size="small">
                {{ row.grant_type === 'direct' ? t('service.directGrant') : t('service.groupGrant') }}
              </el-tag>
            </template>
          </el-table-column>
          <el-table-column :label="t('service.grantSource')" min-width="150">
            <template #default="{ row }">
              <span v-if="row.grant_type === 'direct'">{{ t('service.directAuth') }}</span>
              <span v-else>{{ t('service.groupAuth') }}: {{ row.group_name }}</span>
            </template>
          </el-table-column>
          <el-table-column :label="t('service.grantedAt')" width="150">
            <template #default="{ row }">
              <TimeAgo :time="row.granted_at" />
            </template>
          </el-table-column>
        </el-table>
      </div>
    </el-card>

    <!-- 编辑对话框 -->
    <el-dialog v-model="editDialogVisible" :title="t('client.editClient')" width="500px">
      <el-form :model="editForm" label-width="120px">
        <el-form-item :label="t('client.clientId')">
          <el-input v-model="editForm.client_id" :placeholder="t('client.clientIdPlaceholder')" />
        </el-form-item>
        <el-form-item :label="t('client.enabled')">
          <el-switch v-model="editForm.enabled" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="editDialogVisible = false">{{ t('common.cancel') }}</el-button>
        <el-button type="primary" :loading="saving" @click="handleSaveEdit">{{ t('common.save') }}</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { ElMessage, ElMessageBox } from 'element-plus'
import { ArrowLeft, Refresh, Edit, Delete } from '@element-plus/icons-vue'
import { 
  getClientDetail, 
  enableClient, 
  disableClient, 
  revokeDesktop,
  type ClientDetail,
  type Desktop 
} from '@/api/client'
import TimeAgo from '@/components/Common/TimeAgo.vue'

const { t } = useI18n()
const route = useRoute()
const router = useRouter()

const clientId = Number(route.params.id)
const loading = ref(false)
const clientDetail = ref<ClientDetail | null>(null)

// 编辑对话框
const editDialogVisible = ref(false)
const saving = ref(false)
const editForm = ref({
  client_id: '',
  enabled: false
})

const loadClientDetail = async () => {
  loading.value = true
  try {
    const res = await getClientDetail(clientId)
    clientDetail.value = res
  } catch (error: any) {
    ElMessage.error(t('common.failed'))
  } finally {
    loading.value = false
  }
}

const goBack = () => {
  router.push('/clients')
}

const showEditDialog = () => {
  if (clientDetail.value) {
    editForm.value = {
      client_id: clientDetail.value.client_id,
      enabled: clientDetail.value.enabled
    }
    editDialogVisible.value = true
  }
}

const handleSaveEdit = async () => {
  if (!clientDetail.value) return

  saving.value = true
  try {
    // 如果启用状态发生变化，调用相应的API
    if (editForm.value.enabled !== clientDetail.value.enabled) {
      if (editForm.value.enabled) {
        await enableClient(clientId)
      } else {
        await disableClient(clientId)
      }
    }
    
    ElMessage.success(t('common.success'))
    editDialogVisible.value = false
    await loadClientDetail()
  } catch (error: any) {
    ElMessage.error(t('common.failed'))
  } finally {
    saving.value = false
  }
}

const handleRevokeDesktop = async (desktop: Desktop) => {
  try {
    await ElMessageBox.confirm(
      t('client.revokeDeviceConfirm', { deviceName: desktop.device_name }),
      t('common.confirm'),
      { type: 'warning' }
    )
    
    await revokeDesktop(desktop.id)
    ElMessage.success(t('client.revokeSuccess'))
    await loadClientDetail()
  } catch (error: any) {
    if (error !== 'cancel') {
      ElMessage.error(t('common.failed'))
    }
  }
}

const formatTime = (time?: string) => {
  if (!time) return '-'
  return new Date(time).toLocaleString('zh-CN')
}

onMounted(() => {
  loadClientDetail()
})
</script>

<style scoped>
.client-detail {
  width: 100%;
}

.card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.header-left {
  display: flex;
  align-items: center;
  gap: 12px;
}

.page-title {
  font-size: 18px;
  font-weight: 500;
  color: var(--el-text-color-primary);
}

.info-section {
  margin-bottom: 24px;
}

.info-section:last-child {
  margin-bottom: 0;
}

.section-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 16px;
}

.section-header h3 {
  margin: 0;
  font-size: 16px;
  font-weight: 500;
  color: var(--el-text-color-primary);
}

.info-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(300px, 1fr));
  gap: 16px;
  padding: 16px;
  background: var(--el-fill-color-lighter);
  border-radius: 6px;
}

.info-item {
  display: flex;
  align-items: center;
}

.info-item label {
  font-weight: 500;
  color: var(--el-text-color-regular);
  margin-right: 8px;
  min-width: 100px;
}

.groups-container {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}

.group-tag {
  margin: 0;
}
</style>