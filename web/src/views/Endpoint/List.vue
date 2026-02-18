<template>
  <div class="endpoint-list">
    <!-- 搜索区 -->
    <el-card class="filter-card" shadow="never">
      <el-form :inline="true" :model="searchForm" class="filter-form">
        <el-form-item>
          <el-select v-model="searchForm.agent_id" :placeholder="$t('endpoint.ownerAgent')" clearable filterable style="width: 160px">
            <el-option v-for="u in agents" :key="u.id" :label="u.alias || u.name" :value="String(u.id)" />
          </el-select>
        </el-form-item>
        <el-form-item>
          <el-select v-model="searchForm.status" :placeholder="$t('common.status')" clearable style="width: 160px">
            <el-option :label="$t('common.online')" value="online" />
            <el-option :label="$t('common.offline')" value="offline" />
          </el-select>
        </el-form-item>
        <el-form-item>
          <el-input v-model="searchForm.search" :placeholder="$t('endpoint.searchPlaceholder')" clearable style="width: 240px" @keyup.enter="handleSearch" />
        </el-form-item>
        <el-form-item class="filter-buttons">
          <el-button type="primary" @click="handleSearch">{{ $t('common.search') }}</el-button>
          <el-button @click="handleReset">{{ $t('common.reset') }}</el-button>
        </el-form-item>
      </el-form>
    </el-card>

    <!-- 数据区 -->
    <el-card shadow="never">
      <el-table v-loading="loading" :data="list" stripe>
        <el-table-column prop="name" :label="$t('endpoint.name')" min-width="130">
          <template #default="{ row }">
            <el-link type="primary" @click="goDetail(row)">{{ row.name }}</el-link>
          </template>
        </el-table-column>
        <el-table-column prop="alias" :label="$t('endpoint.alias')" min-width="100">
          <template #default="{ row }">{{ row.alias || '-' }}</template>
        </el-table-column>
        <el-table-column :label="$t('endpoint.capabilities')" min-width="180">
          <template #default="{ row }">
            <el-tag v-if="row.ssh_enabled" size="small" class="cap-tag">SSH</el-tag>
            <el-tag v-if="row.k8sapi_enabled" type="warning" size="small" class="cap-tag">K8S API</el-tag>
            <el-tag v-if="row.k8sservice_enabled" type="success" size="small" class="cap-tag">K8S Service</el-tag>
            <span v-if="!row.ssh_enabled && !row.k8sapi_enabled && !row.k8sservice_enabled">-</span>
          </template>
        </el-table-column>
        <el-table-column prop="agent_name" :label="$t('endpoint.ownerAgent')" min-width="100">
          <template #default="{ row }">{{ row.agent_name || '-' }}</template>
        </el-table-column>
        <el-table-column :label="$t('common.status')" width="80" align="center">
          <template #default="{ row }">
            <el-tag :type="row.status === 'online' ? 'success' : 'info'" size="small">
              {{ row.status === 'online' ? $t('common.online') : $t('common.offline') }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column :label="$t('common.createdAt')" width="170">
          <template #default="{ row }">{{ formatTime(row.created_at) }}</template>
        </el-table-column>
        <el-table-column :label="$t('common.actions')" width="80" fixed="right">
          <template #default="{ row }">
            <el-button type="danger" link size="small" @click="handleRevoke(row)">{{ $t('endpoint.revoke') }}</el-button>
          </template>
        </el-table-column>
      </el-table>
      <div class="pagination-wrapper">
        <el-pagination v-model:current-page="pagination.page" v-model:page-size="pagination.size" :total="pagination.total" :page-sizes="[10, 20, 50, 100]" layout="total, sizes, prev, pager, next, jumper" @size-change="fetchList" @current-change="fetchList" />
      </div>
    </el-card>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { ElMessage, ElMessageBox } from 'element-plus'
import { getEndpoints, revokeEndpoint, type EndpointItem } from '@/api/endpoint'
import { getUsers } from '@/api/user'
import { formatTime } from '@/utils/time'

const { t } = useI18n()
const router = useRouter()
const loading = ref(false)
const list = ref<EndpointItem[]>([])
const agents = ref<{ id: number; name: string; alias?: string }[]>([])
const searchForm = reactive({ search: '', agent_id: '', status: '' })
const pagination = reactive({ page: 1, size: 20, total: 0 })

const fetchList = async () => {
  loading.value = true
  try {
    const res = await getEndpoints({
      search: searchForm.search || undefined,
      agent_id: searchForm.agent_id || undefined,
      status: searchForm.status || undefined,
      page: pagination.page,
      size: pagination.size,
    })
    if (res.success && res.data) { list.value = res.data; pagination.total = res.total }
  } catch (e) { console.error(e) } finally { loading.value = false }
}

const fetchAgents = async () => {
  try {
    const res = await getUsers({ role: 'agent', size: 1000 })
    if (res.success && res.data) agents.value = res.data.map(u => ({ id: u.id, name: u.name, alias: u.alias }))
  } catch (e) { console.error(e) }
}

const handleSearch = () => { pagination.page = 1; fetchList() }
const handleReset = () => { searchForm.search = ''; searchForm.agent_id = ''; searchForm.status = ''; pagination.page = 1; fetchList() }

const goDetail = (row: EndpointItem) => {
  router.push({ path: `/endpoints/${row.id}`, query: { name: row.name } })
}

const handleRevoke = async (row: EndpointItem) => {
  try {
    await ElMessageBox.confirm(t('endpoint.revokeConfirm'), t('common.warning'), { type: 'warning' })
    const res = await revokeEndpoint(row.id)
    if (res.success) { ElMessage.success(t('endpoint.revokeSuccess')); fetchList() }
  } catch { /* cancelled */ }
}

onMounted(() => { fetchList(); fetchAgents() })
</script>

<style scoped>
.endpoint-list { width: 100%; }
.filter-card { margin-bottom: 16px; }
.filter-form { display: flex; flex-wrap: wrap; align-items: center; }
.filter-buttons { margin-left: auto; }
.pagination-wrapper { margin-top: 16px; display: flex; justify-content: flex-end; }
.cap-tag { margin-right: 4px; }
</style>
