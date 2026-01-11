<template>
  <div class="agent-auth-container">
    <el-card>
      <template #header>
        <div class="card-header">
          <span>{{ $t('agentAuth.title') }}</span>
          <el-button type="primary" @click="handleAddAuth">
            <el-icon><Plus /></el-icon>
            {{ $t('agentAuth.addAuth') }}
          </el-button>
        </div>
      </template>

      <!-- 说明提示 -->
      <el-alert
        :title="$t('common.info')"
        type="info"
        :closable="false"
        style="margin-bottom: 16px"
      >
        <template #default>
          <div>{{ $t('agentAuth.sameGroupTip') }}</div>
        </template>
      </el-alert>

      <!-- 筛选区域 -->
      <div class="filter-bar">
        <el-select v-model="filters.serviceId" :placeholder="$t('agentAuth.filterByService')" clearable style="width: 200px">
          <el-option :label="$t('agentAuth.allServices')" :value="null" />
          <el-option
            v-for="service in serviceList"
            :key="service.id"
            :label="service.name"
            :value="service.id"
          />
        </el-select>
        <el-select v-model="filters.agentId" :placeholder="$t('agentAuth.filterByAgent')" clearable style="width: 200px">
          <el-option :label="$t('agentAuth.allAgents')" :value="null" />
          <el-option
            v-for="agent in agentList"
            :key="agent.id"
            :label="agent.agent_name"
            :value="agent.id"
          />
        </el-select>
      </div>

      <!-- 授权列表 -->
      <el-table :data="filteredAuthList" v-loading="loading" stripe>
        <el-table-column prop="id" label="ID" width="80" />
        <el-table-column :label="$t('agentAuth.visitorAgent')" min-width="150">
          <template #default="{ row }">
            <div>{{ row.agent_name }}</div>
            <div class="text-secondary">{{ row.agent_ip }}</div>
          </template>
        </el-table-column>
        <el-table-column :label="$t('agentAuth.targetService')" min-width="200">
          <template #default="{ row }">
            <div>{{ row.service_name }}</div>
            <div class="text-secondary">{{ row.service_addr }}</div>
          </template>
        </el-table-column>
        <el-table-column :label="$t('agentAuth.grantedAt')" width="180">
          <template #default="{ row }">
            {{ formatTime(row.granted_at) }}
          </template>
        </el-table-column>
        <el-table-column :label="$t('common.actions')" width="120" fixed="right">
          <template #default="{ row }">
            <el-button type="danger" size="small" @click="handleRevoke(row)">
              {{ $t('agentAuth.revoke') }}
            </el-button>
          </template>
        </el-table-column>
      </el-table>
    </el-card>

    <!-- 添加授权对话框 -->
    <el-dialog v-model="dialogVisible" :title="$t('agentAuth.addAuth')" width="600px">
      <el-form :model="form" label-width="120px">
        <el-form-item :label="$t('agentAuth.visitorAgent')" required>
          <el-select v-model="form.agentId" :placeholder="$t('agentAuth.selectVisitorAgentPlaceholder')" style="width: 100%">
            <el-option
              v-for="agent in agentList"
              :key="agent.id"
              :label="`${agent.agent_name} (${agent.tailscale_ip || '-'}) ${agent.group_name ? '- ' + agent.group_name : '- ' + $t('agent.noGroup')}`"
              :value="agent.id"
            />
          </el-select>
        </el-form-item>
        <el-form-item :label="$t('agentAuth.targetService')" required>
          <el-select
            v-model="form.serviceIds"
            :placeholder="$t('agentAuth.selectTargetServicePlaceholder')"
            multiple
            style="width: 100%"
          >
            <el-option
              v-for="service in serviceList"
              :key="service.id"
              :label="`${service.name} (${service.agent_name} / ${service.agent_ts_ip}:${service.listen_port})`"
              :value="service.id"
            />
          </el-select>
        </el-form-item>
        <el-alert
          type="warning"
          :closable="false"
          style="margin-bottom: 20px"
        >
          <template #default>
            <div>{{ $t('agentAuth.aclSyncWarning') }}</div>
            <div>{{ $t('agentAuth.aclSyncTip') }}</div>
          </template>
        </el-alert>
      </el-form>
      <template #footer>
        <el-button @click="dialogVisible = false">{{ $t('common.cancel') }}</el-button>
        <el-button type="primary" @click="handleSubmit" :loading="submitting">{{ $t('agentAuth.authorize') }}</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, onMounted, computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Plus } from '@element-plus/icons-vue'
import { getAgentServicePermissions, addAgentServicePermission, removeAgentServicePermission } from '@/api/agentPermission'
import { getAgents } from '@/api/agent'
import { getServices } from '@/api/service'
import type { AgentServicePermission } from '@/api/agentPermission'

const { t } = useI18n()

// 筛选条件
const filters = reactive({
  serviceId: null as number | null,
  agentId: null as number | null
})

// 授权列表
const authList = ref<AgentServicePermission[]>([])
const loading = ref(false)

// Agent 和服务列表
const agentList = ref<any[]>([])
const serviceList = ref<any[]>([])

// 对话框
const dialogVisible = ref(false)
const submitting = ref(false)
const form = reactive({
  agentId: null as number | null,
  serviceIds: [] as number[]
})

// 过滤后的授权列表
const filteredAuthList = computed(() => {
  let list = authList.value
  
  if (filters.serviceId) {
    list = list.filter(item => item.service_id === filters.serviceId)
  }
  
  if (filters.agentId) {
    list = list.filter(item => item.agent_id === filters.agentId)
  }
  
  return list
})

// 加载授权列表
const loadAuthList = async () => {
  loading.value = true
  try {
    const response = await getAgentServicePermissions()
    if (response.success) {
      authList.value = response.data || []
    } else {
      ElMessage.error(response.message || t('agentAuth.loadPermissionsFailed'))
    }
  } catch (error: any) {
    console.error('Load auth list failed:', error)
    ElMessage.error(error.message || t('agentAuth.loadPermissionsFailed'))
  } finally {
    loading.value = false
  }
}

// 加载 Agent 列表
const loadAgentList = async () => {
  try {
    const response = await getAgents()
    if (response.success) {
      agentList.value = response.data || []
    }
  } catch (error) {
    console.error('Load agent list failed:', error)
  }
}

// 加载服务列表
const loadServiceList = async () => {
  try {
    const response = await getServices()
    if (response.success) {
      serviceList.value = response.data || []
    }
  } catch (error) {
    console.error('Load service list failed:', error)
  }
}

// 添加授权
const handleAddAuth = () => {
  form.agentId = null
  form.serviceIds = []
  dialogVisible.value = true
}

// 提交授权
const handleSubmit = async () => {
  if (!form.agentId || form.serviceIds.length === 0) {
    ElMessage.warning(t('agentAuth.selectAgentAndService'))
    return
  }

  submitting.value = true
  try {
    const response = await addAgentServicePermission({
      agent_id: form.agentId,
      service_ids: form.serviceIds
    })
    
    if (response.success) {
      ElMessage.success(t('agentAuth.authSuccess'))
      dialogVisible.value = false
      loadAuthList()
    } else {
      ElMessage.error(response.message || t('agentAuth.authFailed'))
    }
  } catch (error: any) {
    console.error('Auth failed:', error)
    if (error.response?.data?.message?.includes('ACL') || error.response?.data?.message?.includes('Headscale')) {
      ElMessage.error(t('agentAuth.aclSyncError'))
    } else {
      ElMessage.error(error.response?.data?.message || error.message || t('agentAuth.authFailed'))
    }
  } finally {
    submitting.value = false
  }
}

// 撤销授权
const handleRevoke = async (row: AgentServicePermission) => {
  try {
    await ElMessageBox.confirm(
      t('agentAuth.revokeConfirm'),
      t('common.confirm'),
      {
        type: 'warning'
      }
    )
    
    const response = await removeAgentServicePermission(row.id)
    if (response.success) {
      ElMessage.success(t('agentAuth.revokeSuccess'))
      loadAuthList()
    } else {
      ElMessage.error(response.message || t('agentAuth.revokeFailed'))
    }
  } catch (error: any) {
    if (error !== 'cancel') {
      console.error('Revoke failed:', error)
      if (error.response?.data?.message?.includes('ACL') || error.response?.data?.message?.includes('Headscale')) {
        ElMessage.error(t('agentAuth.aclSyncError'))
      } else {
        ElMessage.error(error.response?.data?.message || error.message || t('agentAuth.revokeFailed'))
      }
    }
  }
}

// 格式化时间
const formatTime = (time: string) => {
  if (!time) return '-'
  return new Date(time).toLocaleString('zh-CN')
}

onMounted(() => {
  loadAuthList()
  loadAgentList()
  loadServiceList()
})
</script>

<style scoped>
.card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.filter-bar {
  margin-bottom: 16px;
  display: flex;
  gap: 12px;
}

.text-secondary {
  font-size: 12px;
  color: #909399;
  margin-top: 4px;
}
</style>
