<template>
  <el-dialog
    v-model="visible"
    :title="t('stcp.grantAccess')"
    width="600px"
    @close="handleClose"
  >
    <div v-if="instance" class="instance-info">
      <p><strong>{{ t('stcp.instanceName') }}:</strong> {{ instance.instance_name }}</p>
      <p><strong>{{ t('stcp.localAddress') }}:</strong> {{ instance.local_ip }}:{{ instance.local_port }}</p>
    </div>

    <el-divider />

    <div class="grant-section">
      <div class="section-header">
        <h4>{{ t('stcp.grantToClient') }}</h4>
        <el-button
          type="primary"
          size="small"
          :icon="Plus"
          @click="showAddClient = true"
        >
          {{ t('stcp.addClient') }}
        </el-button>
      </div>

      <el-table v-loading="loading" :data="grantedClients" stripe>
        <el-table-column :label="t('stcp.user')" min-width="200">
          <template #default="{ row }">
            {{ row.client?.client_id || '-' }}
          </template>
        </el-table-column>
        <el-table-column :label="t('agent.createdAt')" width="100">
          <template #default="{ row }">
            <TimeAgo :time="row.created_at" />
          </template>
        </el-table-column>
        <el-table-column :label="t('common.actions')" width="100">
          <template #default="{ row }">
            <el-button
              size="small"
              type="danger"
              :icon="Delete"
              @click="handleRevoke(row)"
            />
          </template>
        </el-table-column>
      </el-table>
    </div>

    <el-dialog
      v-model="showAddClient"
      :title="t('stcp.selectClient')"
      width="400px"
      append-to-body
    >
      <el-select
        v-model="selectedClientId"
        :placeholder="t('stcp.selectClient')"
        style="width: 100%"
        filterable
      >
        <el-option
          v-for="client in availableClients"
          :key="client.id"
          :label="client.client_id"
          :value="client.id"
        />
      </el-select>
      <template #footer>
        <el-button @click="showAddClient = false">{{ t('common.cancel') }}</el-button>
        <el-button
          type="primary"
          :loading="granting"
          :disabled="!selectedClientId"
          @click="handleGrant"
        >
          {{ t('common.confirm') }}
        </el-button>
      </template>
    </el-dialog>
  </el-dialog>
</template>

<script setup lang="ts">
import { ref, watch, computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Plus, Delete } from '@element-plus/icons-vue'
import { grantSTCPAccess, revokeSTCPAccess, getSTCPAccesses } from '@/api/stcp'
import { getClients } from '@/api/client'
import type { STCPInstance, Client } from '@/types/models'
import TimeAgo from '@/components/Common/TimeAgo.vue'

const props = defineProps<{
  modelValue: boolean
  instance: STCPInstance | null
}>()

const emit = defineEmits<{
  (e: 'update:modelValue', value: boolean): void
  (e: 'success'): void
}>()

const { t } = useI18n()

const visible = ref(false)
const loading = ref(false)
const granting = ref(false)
const showAddClient = ref(false)
const selectedClientId = ref<number>(0)
const grantedClients = ref<any[]>([])
const allClients = ref<Client[]>([])

const availableClients = computed(() => {
  const grantedIds = new Set(grantedClients.value.map(g => g.client?.id))
  return allClients.value.filter(c => !grantedIds.has(c.id))
})

const loadGrantedClients = async () => {
  if (!props.instance) return
  
  loading.value = true
  try {
    const res = await getSTCPAccesses(props.instance.id)
    if (res.success && res.data) {
      grantedClients.value = res.data
    }
  } catch (error) {
    ElMessage.error(t('common.failed'))
  } finally {
    loading.value = false
  }
}

const loadClients = async () => {
  try {
    const res = await getClients()
    if (res.success && res.clients) {
      allClients.value = res.clients
    }
  } catch (error) {
    // ignore
  }
}

watch(
  () => props.modelValue,
  (val) => {
    visible.value = val
    if (val && props.instance) {
      loadGrantedClients()
      loadClients()
    }
  }
)

watch(visible, (val) => {
  emit('update:modelValue', val)
})

const handleClose = () => {
  visible.value = false
  showAddClient.value = false
  selectedClientId.value = 0
}

const handleGrant = async () => {
  if (!props.instance || !selectedClientId.value) return

  granting.value = true
  try {
    const res = await grantSTCPAccess(props.instance.id, selectedClientId.value)
    if (res.success) {
      ElMessage.success(t('stcp.grantSuccess'))
      showAddClient.value = false
      selectedClientId.value = 0
      loadGrantedClients()
      emit('success')
    }
  } catch (error) {
    ElMessage.error(t('common.failed'))
  } finally {
    granting.value = false
  }
}

const handleRevoke = async (access: any) => {
  if (!props.instance) return

  try {
    await ElMessageBox.confirm(t('stcp.revokeConfirm'), {
      type: 'warning'
    })
    
    const res = await revokeSTCPAccess(props.instance.id, access.client_id)
    if (res.success) {
      ElMessage.success(t('stcp.revokeSuccess'))
      loadGrantedClients()
      emit('success')
    }
  } catch (error: any) {
    if (error !== 'cancel') {
      ElMessage.error(t('common.failed'))
    }
  }
}
</script>

<style scoped>
.instance-info {
  padding: 10px;
  background: var(--bg-secondary);
  border-radius: 4px;
  margin-bottom: 10px;
}

.instance-info p {
  margin: 5px 0;
}

.grant-section {
  margin-top: 20px;
}

.section-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 15px;
}

.section-header h4 {
  margin: 0;
  font-size: 16px;
  font-weight: 500;
}
</style>
