<template>
  <div class="page-container">
    <div class="page-header">
      <div>
        <h1>成员设备</h1>
        <p>设备属于实名用户。这里仅展示当前租户有效成员登录过的 Desktop 设备。</p>
      </div>
      <el-button :loading="loading" @click="fetchDevices">刷新</el-button>
    </div>
    <div class="filters">
      <el-input v-model="search" clearable placeholder="搜索成员、设备或主机名" @keyup.enter="applySearch" @clear="applySearch" />
    </div>
    <el-table v-loading="loading" :data="devices" empty-text="当前租户还没有成员设备">
      <el-table-column label="成员" min-width="180"><template #default="scope"><strong>{{ scope.row.user_alias || scope.row.user_name }}</strong><div class="secondary">{{ scope.row.user_name }}</div></template></el-table-column>
      <el-table-column prop="device_name" label="设备" min-width="180" />
      <el-table-column prop="hostname" label="主机名" min-width="180" show-overflow-tooltip />
      <el-table-column prop="ip" label="安全网络 IP" width="150" />
      <el-table-column prop="version" label="客户端版本" width="130" />
      <el-table-column label="状态" width="110"><template #default="scope"><el-tag size="small" effect="plain" :type="scope.row.online ? 'success' : 'info'">{{ scope.row.online ? '在线' : '离线' }}</el-tag></template></el-table-column>
      <el-table-column label="最后心跳" min-width="180"><template #default="scope">{{ formatTime(scope.row.last_heartbeat) }}</template></el-table-column>
    </el-table>
    <div class="pagination"><el-pagination v-model:current-page="page" v-model:page-size="size" layout="total, sizes, prev, pager, next" :total="total" @current-change="fetchDevices" @size-change="resetAndFetch" /></div>
  </div>
</template>

<script setup lang="ts">
import { onMounted, ref, watch } from 'vue'
import { getTenantMemberDevices, type TenantMemberDevice } from '@/api/tenantBusiness'
import { useTenantStore } from '@/stores/tenant'

const tenantStore = useTenantStore()
const loading = ref(false)
const devices = ref<TenantMemberDevice[]>([])
const search = ref('')
const page = ref(1)
const size = ref(20)
const total = ref(0)
const formatTime = (value?: string) => value ? new Date(value).toLocaleString() : '从未在线'
const fetchDevices = async () => {
  if (!tenantStore.tenantId) return
  loading.value = true
  try {
    const response = await getTenantMemberDevices(tenantStore.tenantId, { search: search.value || undefined, page: page.value, size: size.value })
    devices.value = response.success && response.data ? response.data : []
    total.value = response.total || 0
  } finally { loading.value = false }
}
const applySearch = () => { page.value = 1; fetchDevices() }
const resetAndFetch = () => { page.value = 1; fetchDevices() }
onMounted(fetchDevices)
watch(() => tenantStore.contextRevision, () => { search.value = ''; page.value = 1; fetchDevices() })
</script>

<style scoped>
.page-header { display: flex; align-items: flex-start; justify-content: space-between; gap: 24px; margin-bottom: 18px; }
h1 { margin: 0; color: var(--text-primary); font-size: 24px; line-height: 32px; }
.page-header p { margin: 5px 0 0; color: var(--text-secondary); font-size: 13px; }
.filters { display: flex; width: min(420px, 100%); margin-bottom: 12px; }
.secondary { margin-top: 2px; color: var(--text-secondary); font-size: 12px; }
.pagination { display: flex; justify-content: flex-end; padding-top: 14px; }
</style>
