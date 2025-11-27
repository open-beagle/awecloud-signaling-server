<template>
  <div class="stcp-list">
    <el-card>
      <template #header>
        <div class="card-header">
          <span class="card-title">{{ t('stcp.list') }}</span>
          <el-button type="primary" :icon="Plus" @click="handleCreate">
            {{ t('stcp.create') }}
          </el-button>
        </div>
      </template>
      
      <el-table v-loading="loading" :data="instances" stripe>
        <el-table-column prop="id" label="ID" width="80" />
        <el-table-column prop="instance_name" :label="t('stcp.instanceName')" min-width="150" />
        <el-table-column :label="t('stcp.agent')" width="150">
          <template #default="{ row }">
            {{ row.agent?.agent_name || '-' }}
          </template>
        </el-table-column>
        <el-table-column :label="t('stcp.localAddress')" min-width="180">
          <template #default="{ row }">
            {{ row.local_ip }}:{{ row.local_port }}
          </template>
        </el-table-column>
        <el-table-column prop="description" :label="t('agent.description')" min-width="200" />
        <el-table-column label="状态" width="100">
          <template #default="{ row }">
            <el-tag v-if="row.status === 'online'" type="success" size="small">在线</el-tag>
            <el-tag v-else type="info" size="small">离线</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="访问权限" width="120">
          <template #default="{ row }">
            <el-tag v-if="row.access_type === 'public'" type="success">Public</el-tag>
            <el-tag v-else-if="row.access_type === 'private'" type="warning">Private</el-tag>
            <el-tag v-else-if="row.access_type === 'group'" type="info">Group</el-tag>
            <el-tag v-else>Public</el-tag>
          </template>
        </el-table-column>
        <el-table-column :label="t('agent.createdAt')" width="100">
          <template #default="{ row }">
            <TimeAgo :time="row.created_at" />
          </template>
        </el-table-column>
        <el-table-column :label="t('common.actions')" width="280" fixed="right">
          <template #default="{ row }">
            <el-button
              size="small"
              @click="handleSetAccess(row)"
            >
              权限设置
            </el-button>
            <el-button
              size="small"
              type="primary"
              @click="handleGrant(row)"
            >
              {{ t('stcp.grantAccess') }}
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

    <CreateDialog v-model="createDialogVisible" @success="loadInstances" />
    <AccessDialog
      v-model="accessDialogVisible"
      :instance="selectedInstance"
      @success="loadInstances"
    />
    <GrantDialog
      v-model="grantDialogVisible"
      :instance="selectedInstance"
      @success="loadInstances"
    />
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Plus, Delete } from '@element-plus/icons-vue'
import { getSTCPInstances, deleteSTCPInstance } from '@/api/stcp'
import type { STCPInstance } from '@/types/models'
import TimeAgo from '@/components/Common/TimeAgo.vue'
import CreateDialog from './components/CreateDialog.vue'
import AccessDialog from './components/AccessDialog.vue'
import GrantDialog from './components/GrantDialog.vue'

const { t } = useI18n()

const loading = ref(false)
const instances = ref<STCPInstance[]>([])
const createDialogVisible = ref(false)
const grantDialogVisible = ref(false)
const accessDialogVisible = ref(false)
const selectedInstance = ref<STCPInstance | null>(null)

const loadInstances = async () => {
  loading.value = true
  try {
    const res = await getSTCPInstances()
    if (res.success && res.data) {
      instances.value = res.data
    }
  } catch (error) {
    ElMessage.error(t('common.failed'))
  } finally {
    loading.value = false
  }
}

const handleCreate = () => {
  createDialogVisible.value = true
}

const handleSetAccess = (instance: STCPInstance) => {
  selectedInstance.value = instance
  accessDialogVisible.value = true
}

const handleGrant = (instance: STCPInstance) => {
  selectedInstance.value = instance
  grantDialogVisible.value = true
}

const handleDelete = async (instance: STCPInstance) => {
  try {
    await ElMessageBox.confirm(t('stcp.deleteConfirm'), {
      type: 'warning'
    })
    
    const res = await deleteSTCPInstance(instance.id)
    if (res.success) {
      ElMessage.success(t('common.deleteSuccess'))
      loadInstances()
    }
  } catch (error: any) {
    if (error !== 'cancel') {
      ElMessage.error(t('common.failed'))
    }
  }
}

onMounted(() => {
  loadInstances()
})
</script>

<style scoped>
.stcp-list {
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
