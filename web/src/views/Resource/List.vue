<template>
  <div class="resource-list">
    <!-- 搜索 -->
    <el-card class="search-card" shadow="never">
      <el-form :inline="true" :model="searchForm" class="search-form">
        <el-form-item :label="$t('common.search')">
          <el-input
            v-model="searchForm.search"
            :placeholder="$t('resource.searchPlaceholder')"
            clearable
            style="width: 240px"
            @keyup.enter="handleSearch"
          />
        </el-form-item>
        <el-form-item>
          <el-button type="primary" @click="handleSearch">
            {{ $t('common.search') }}
          </el-button>
          <el-button @click="handleReset">
            {{ $t('common.reset') }}
          </el-button>
          <el-button
            type="primary"
            :icon="Refresh"
            :loading="syncing"
            @click="handleSync"
          >
            {{ syncing ? $t('resource.syncing') : $t('resource.sync') }}
          </el-button>
        </el-form-item>
      </el-form>
    </el-card>

    <!-- 列表 -->
    <el-card shadow="never">
      <el-table v-loading="loading" :data="filteredList" stripe>
        <el-table-column prop="service_name" :label="$t('resource.serviceName')" min-width="150" />
        <el-table-column prop="namespace" :label="$t('resource.namespace')" min-width="120" />
        <el-table-column prop="agent_name" :label="$t('resource.agentName')" min-width="120" />
        <el-table-column prop="endpoint_name" label="Endpoint" min-width="120">
          <template #default="{ row }">
            <span v-if="row.endpoint_name">{{ row.endpoint_name }}</span>
            <span v-else style="color: #999">-</span>
          </template>
        </el-table-column>
        <el-table-column prop="cluster_ip" :label="$t('resource.clusterIP')" width="140" />
        <el-table-column :label="$t('resource.ports')" min-width="200">
          <template #default="{ row }">
            <el-tag
              v-for="p in (row.ports || [])"
              :key="p.port"
              size="small"
              style="margin-right: 4px"
            >
              {{ p.name ? `${p.name}:` : '' }}{{ p.port }}/{{ p.protocol }}
            </el-tag>
            <span v-if="!row.ports?.length">-</span>
          </template>
        </el-table-column>
      </el-table>
    </el-card>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { Refresh } from '@element-plus/icons-vue'
import { ElMessage } from 'element-plus'
import {
  getDiscoveredK8SServices,
  syncK8SServiceDiscovery,
  type DiscoveredK8SService
} from '@/api/resource'

const loading = ref(false)
const syncing = ref(false)
const list = ref<DiscoveredK8SService[]>([])
const searchForm = ref({ search: '' })

const filteredList = computed(() => {
  const q = searchForm.value.search.toLowerCase()
  if (!q) return list.value
  return list.value.filter(item =>
    item.agent_name.toLowerCase().includes(q) ||
    item.namespace.toLowerCase().includes(q) ||
    item.service_name.toLowerCase().includes(q)
  )
})

const fetchList = async () => {
  loading.value = true
  try {
    const res = await getDiscoveredK8SServices()
    if (res.success && res.data) list.value = res.data
  } catch (e) {
    console.error('获取资源发现列表失败:', e)
  } finally {
    loading.value = false
  }
}

// 触发 Agent 立即上报，3 秒后刷新列表
const handleSync = async () => {
  syncing.value = true
  try {
    await syncK8SServiceDiscovery()
    ElMessage.success('已通知 Agent 上报，3 秒后刷新...')
    setTimeout(async () => {
      await fetchList()
      syncing.value = false
    }, 3000)
  } catch (e) {
    console.error('触发同步失败:', e)
    ElMessage.error('触发同步失败')
    syncing.value = false
  }
}

const handleSearch = () => { /* 前端过滤，无需请求 */ }
const handleReset = () => { searchForm.value.search = '' }

onMounted(() => { fetchList() })
</script>

<style scoped>
.resource-list { width: 100%; }
.search-card { margin-bottom: 20px; }
.search-form { display: flex; flex-wrap: wrap; align-items: center; }
</style>
