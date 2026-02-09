<template>
  <div class="domain-list">
    <!-- 搜索和筛选 -->
    <el-card class="search-card" shadow="never">
      <el-form :inline="true" :model="searchForm" class="search-form">
        <el-form-item :label="$t('domain.type')">
          <el-select v-model="searchForm.type" :placeholder="$t('common.all')" clearable style="width: 160px">
            <el-option v-for="item in domainTypes" :key="item.value" :label="item.label" :value="item.value" />
          </el-select>
        </el-form-item>
        <el-form-item :label="$t('common.status')">
          <el-select v-model="searchForm.status" :placeholder="$t('common.all')" clearable style="width: 120px">
            <el-option :label="$t('common.online')" value="online" />
            <el-option :label="$t('common.offline')" value="offline" />
          </el-select>
        </el-form-item>
        <el-form-item :label="$t('common.search')">
          <el-input v-model="searchForm.search" :placeholder="$t('domain.searchPlaceholder')" clearable style="width: 200px" @keyup.enter="handleSearch" />
        </el-form-item>
        <el-form-item>
          <el-button type="primary" @click="handleSearch">{{ $t('common.search') }}</el-button>
          <el-button @click="handleReset">{{ $t('common.reset') }}</el-button>
        </el-form-item>
      </el-form>
    </el-card>

    <!-- 域名列表 -->
    <el-card shadow="never">
      <el-table v-loading="loading" :data="domains" stripe>
        <el-table-column prop="domain" :label="$t('domain.domain')" min-width="200" />
        <el-table-column prop="type" :label="$t('domain.type')" width="160">
          <template #default="{ row }">
            <el-tag size="small" :type="getDomainTypeTag(row.type)">
              {{ getDomainTypeLabel(row.type) }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column :label="$t('domain.agent')" min-width="120">
          <template #default="{ row }">
            <router-link v-if="row.agent_name" :to="`/users/${row.agent_user_id}`" class="agent-link">
              {{ row.agent_name }}
            </router-link>
            <span v-else>-</span>
          </template>
        </el-table-column>
        <el-table-column prop="target_port" :label="$t('domain.targetPort')" width="100">
          <template #default="{ row }">
            {{ row.target_port || '-' }}
          </template>
        </el-table-column>
        <el-table-column prop="namespace" :label="$t('domain.namespace')" width="120">
          <template #default="{ row }">
            {{ row.namespace || '-' }}
          </template>
        </el-table-column>
        <el-table-column prop="service_name" :label="$t('domain.serviceName')" width="120">
          <template #default="{ row }">
            {{ row.service_name || '-' }}
          </template>
        </el-table-column>
        <el-table-column :label="$t('common.status')" width="100">
          <template #default="{ row }">
            <el-tag :type="row.status === 'online' ? 'success' : 'info'" size="small">
              {{ row.status === 'online' ? $t('common.online') : $t('common.offline') }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="created_at" :label="$t('common.createdAt')" width="180" />
      </el-table>

      <!-- 分页 -->
      <div class="pagination-wrapper">
        <el-pagination
          v-model:current-page="pagination.page"
          v-model:page-size="pagination.size"
          :total="pagination.total"
          :page-sizes="[10, 20, 50, 100]"
          layout="total, sizes, prev, pager, next, jumper"
          @size-change="fetchDomains"
          @current-change="fetchDomains"
        />
      </div>
    </el-card>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, computed, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { getDomains, type DomainItem, type DomainType } from '@/api/domain'

const { t } = useI18n()

const loading = ref(false)
const domains = ref<DomainItem[]>([])

const searchForm = reactive({
  type: '',
  status: '',
  search: ''
})

const pagination = reactive({
  page: 1,
  size: 20,
  total: 0
})

// 域名类型选项
const domainTypes = computed(() => [
  { value: 'agent_ssh', label: t('domain.typeAgentSSH') },
  { value: 'agent_k8sapi', label: t('domain.typeAgentK8SAPI') },
  { value: 'agent_k8s_service', label: t('domain.typeAgentK8SService') },
  { value: 'agent_service', label: t('domain.typeAgentService') },
  { value: 'endpoint_ssh', label: t('domain.typeEndpointSSH') },
  { value: 'endpoint_k8sapi', label: t('domain.typeEndpointK8SAPI') },
  { value: 'endpoint_k8s_service', label: t('domain.typeEndpointK8SService') }
])

// 域名类型标签颜色
const getDomainTypeTag = (type: DomainType) => {
  const map: Record<string, string> = {
    agent_ssh: 'success',
    agent_k8sapi: 'warning',
    agent_k8s_service: '',
    agent_service: 'info',
    endpoint_ssh: 'success',
    endpoint_k8sapi: 'warning',
    endpoint_k8s_service: ''
  }
  return map[type] || 'info'
}

// 域名类型标签文字
const getDomainTypeLabel = (type: DomainType) => {
  const map: Record<string, string> = {
    agent_ssh: t('domain.typeAgentSSH'),
    agent_k8sapi: t('domain.typeAgentK8SAPI'),
    agent_k8s_service: t('domain.typeAgentK8SService'),
    agent_service: t('domain.typeAgentService'),
    endpoint_ssh: t('domain.typeEndpointSSH'),
    endpoint_k8sapi: t('domain.typeEndpointK8SAPI'),
    endpoint_k8s_service: t('domain.typeEndpointK8SService')
  }
  return map[type] || type
}

// 获取域名列表
const fetchDomains = async () => {
  loading.value = true
  try {
    const res = await getDomains({
      type: searchForm.type || undefined,
      status: searchForm.status || undefined,
      search: searchForm.search || undefined,
      page: pagination.page,
      size: pagination.size
    })
    if (res.success && res.data) {
      domains.value = res.data
      pagination.total = res.total
    }
  } catch (error) {
    console.error('获取域名列表失败:', error)
  } finally {
    loading.value = false
  }
}

// 搜索
const handleSearch = () => {
  pagination.page = 1
  fetchDomains()
}

// 重置
const handleReset = () => {
  searchForm.type = ''
  searchForm.status = ''
  searchForm.search = ''
  pagination.page = 1
  fetchDomains()
}

onMounted(() => {
  fetchDomains()
})
</script>

<style scoped>
.domain-list {
  width: 100%;
}

.search-card {
  margin-bottom: 20px;
}

.search-form {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
}

.pagination-wrapper {
  margin-top: 20px;
  display: flex;
  justify-content: flex-end;
}

.agent-link {
  color: var(--el-color-primary);
  text-decoration: none;
}

.agent-link:hover {
  text-decoration: underline;
}
</style>
