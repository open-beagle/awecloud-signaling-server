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
        <el-table-column :label="t('agent.createdAt')" width="100">
          <template #default="{ row }">
            <TimeAgo :time="row.created_at" />
          </template>
        </el-table-column>
        <el-table-column :label="t('common.actions')" width="100" fixed="right">
          <template #default="{ row }">
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

const { t } = useI18n()

const loading = ref(false)
const instances = ref<STCPInstance[]>([])
const createDialogVisible = ref(false)

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
