<template>
  <div class="access-page">
    <el-breadcrumb separator="/" class="breadcrumb">
      <el-breadcrumb-item :to="{ path: '/stcp-instances' }">STCP实例</el-breadcrumb-item>
      <el-breadcrumb-item>{{ instance?.instance_name || '授权管理' }}</el-breadcrumb-item>
    </el-breadcrumb>

    <el-card v-if="instance" class="instance-card">
      <div class="instance-info">
        <p><strong>实例名称:</strong> {{ instance.instance_name }}</p>
        <p><strong>本地地址:</strong> {{ instance.local_ip }}:{{ instance.local_port }}</p>
      </div>
    </el-card>

    <el-card>
      <template #header>
        <div class="card-header">
          <span class="card-title">授权管理</span>
          <div class="header-actions">
            <el-select
              v-model="selectedClientId"
              placeholder="选择Client"
              filterable
              style="width: 300px; margin-right: 10px"
            >
              <el-option
                v-for="client in availableClients"
                :key="client.id"
                :label="client.client_id"
                :value="client.id"
              />
            </el-select>
            <el-button
              type="primary"
              :icon="Plus"
              :loading="granting"
              :disabled="!selectedClientId"
              @click="handleGrant"
            >
              添加授权
            </el-button>
          </div>
        </div>
      </template>

      <el-table v-loading="loading" :data="grantedClients" stripe>
      <el-table-column :label="t('stcp.user')" min-width="200">
        <template #default="{ row }">
          {{ row.client?.client_id || '-' }}
        </template>
      </el-table-column>
      <el-table-column label="授权时间" width="180">
        <template #default="{ row }">
          {{ formatDate(row.created_at) }}
        </template>
      </el-table-column>
      <el-table-column label="操作" width="100">
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
    </el-card>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Plus, Delete } from '@element-plus/icons-vue'
import { grantSTCPAccess, revokeSTCPAccess, getSTCPAccesses } from '@/api/stcp'
import { getClients } from '@/api/client'
import type { STCPInstance, Client } from '@/types/models'

const route = useRoute()
const router = useRouter()
const { t } = useI18n()

const loading = ref(false)
const granting = ref(false)
const instance = ref<STCPInstance | null>(null)
const selectedClientId = ref<number>(0)
const grantedClients = ref<any[]>([])
const allClients = ref<Client[]>([])

const availableClients = computed(() => {
  const grantedIds = new Set(grantedClients.value.map(g => g.client?.id))
  return allClients.value.filter(c => !grantedIds.has(c.id))
})

const formatDate = (dateStr: string) => {
  return new Date(dateStr).toLocaleString('zh-CN')
}

const loadGrantedClients = async () => {
  const instanceId = Number(route.params.id)
  if (!instanceId) return

  loading.value = true
  try {
    const res = await getSTCPAccesses(instanceId)
    console.log('STCP accesses response:', res)
    if (res.success && res.data) {
      console.log('Granted clients data:', res.data)
      grantedClients.value = res.data
    }
  } catch (error) {
    console.error('Load granted clients error:', error)
    ElMessage.error('加载授权列表失败')
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

const handleGrant = async () => {
  const instanceId = Number(route.params.id)
  if (!instanceId || !selectedClientId.value) return

  granting.value = true
  try {
    const res = await grantSTCPAccess(instanceId, selectedClientId.value)
    if (res.success) {
      ElMessage.success('授权成功')
      selectedClientId.value = 0
      loadGrantedClients()
    }
  } catch (error) {
    ElMessage.error('授权失败')
  } finally {
    granting.value = false
  }
}

const handleRevoke = async (access: any) => {
  const instanceId = Number(route.params.id)
  if (!instanceId) return

  try {
    await ElMessageBox.confirm('确认撤销此授权吗？', {
      type: 'warning'
    })

    const res = await revokeSTCPAccess(instanceId, access.client_id)
    if (res.success) {
      ElMessage.success('撤销成功')
      loadGrantedClients()
    }
  } catch (error: any) {
    if (error !== 'cancel') {
      ElMessage.error('撤销失败')
    }
  }
}

onMounted(() => {
  // 从路由参数获取实例信息
  if (route.params.name) {
    instance.value = {
      id: Number(route.params.id),
      instance_name: route.params.name as string,
      local_ip: route.params.ip as string,
      local_port: Number(route.params.port),
      agent_id: 0,
      secret_key: '',
      description: '',
      created_at: '',
      updated_at: ''
    }
  }
  loadGrantedClients()
  loadClients()
})
</script>

<style scoped>
.access-page {
  width: 100%;
}

.breadcrumb {
  margin-bottom: 20px;
}

.instance-card {
  margin-bottom: 20px;
}

.instance-info p {
  margin: 8px 0;
  font-size: 14px;
  color: var(--el-text-color-regular);
}

.instance-info p:first-child {
  margin-top: 0;
}

.instance-info p:last-child {
  margin-bottom: 0;
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

.header-actions {
  display: flex;
  align-items: center;
}
</style>
