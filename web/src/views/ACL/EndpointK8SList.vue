<template>
  <div class="acl-endpoint-k8s-list">
    <el-card class="search-card" shadow="never">
      <el-form :inline="true" :model="searchForm" class="search-form">
        <el-form-item :label="$t('common.search')">
          <el-input v-model="searchForm.search" :placeholder="$t('aclEndpoint.searchPlaceholder')" clearable style="width: 240px" @keyup.enter="handleSearch" />
        </el-form-item>
        <el-form-item>
          <el-button type="primary" @click="handleSearch">{{ $t('common.search') }}</el-button>
          <el-button @click="handleReset">{{ $t('common.reset') }}</el-button>
        </el-form-item>
      </el-form>
    </el-card>

    <el-card shadow="never">
      <el-table v-loading="loading" :data="list" stripe>
        <el-table-column prop="name" :label="$t('endpoint.name')" min-width="150">
          <template #default="{ row }">
            <el-link type="primary" :underline="false" @click="handleView(row)">{{ row.name }}</el-link>
          </template>
        </el-table-column>
        <el-table-column prop="alias" :label="$t('endpoint.alias')" min-width="100" />
        <el-table-column prop="agent_name" :label="$t('endpoint.ownerAgent')" min-width="120" />
        <el-table-column prop="api_server" :label="$t('endpoint.apiServer')" min-width="180" />
        <el-table-column prop="status" :label="$t('endpoint.status')" width="80" align="center">
          <template #default="{ row }">
            <el-tag :type="row.status === 'online' ? 'success' : 'info'" size="small">
              {{ row.status === 'online' ? $t('common.online') : $t('common.offline') }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="user_count" :label="$t('acl.userCount')" width="80" align="center">
          <template #default="{ row }"><el-tag type="primary" size="small">{{ row.user_count }}</el-tag></template>
        </el-table-column>
        <el-table-column prop="group_count" :label="$t('acl.groupCount')" width="80" align="center">
          <template #default="{ row }"><el-tag type="success" size="small">{{ row.group_count }}</el-tag></template>
        </el-table-column>
        <el-table-column prop="created_at" :label="$t('common.createdAt')" width="180">
          <template #default="{ row }">{{ formatTime(row.created_at) }}</template>
        </el-table-column>
        <el-table-column :label="$t('common.actions')" width="120" fixed="right">
          <template #default="{ row }">
            <el-button type="primary" link size="small" @click="handleView(row)">{{ $t('acl.manageAuth') }}</el-button>
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
import { getEndpointK8SAPIACLList, type EndpointK8SAPIACLItem } from '@/api/aclEndpointK8sapi'
import { formatTime } from '@/utils/time'

const router = useRouter()
const loading = ref(false)
const list = ref<EndpointK8SAPIACLItem[]>([])
const searchForm = reactive({ search: '' })
const pagination = reactive({ page: 1, size: 20, total: 0 })

const fetchList = async () => {
  loading.value = true
  try {
    const res = await getEndpointK8SAPIACLList({ search: searchForm.search || undefined, page: pagination.page, size: pagination.size })
    if (res.success && res.data) { list.value = res.data; pagination.total = res.total }
  } catch (error) { console.error('获取列表失败:', error) }
  finally { loading.value = false }
}

const handleSearch = () => { pagination.page = 1; fetchList() }
const handleReset = () => { searchForm.search = ''; pagination.page = 1; fetchList() }
const handleView = (row: EndpointK8SAPIACLItem) => { router.push({ path: `/acl/endpoint-k8sapi/${row.id}`, query: { name: row.name } }) }

onMounted(() => { fetchList() })
</script>

<style scoped>
.acl-endpoint-k8s-list { width: 100%; }
.search-card { margin-bottom: 20px; }
.search-form { display: flex; flex-wrap: wrap; align-items: center; }
.pagination-wrapper { margin-top: 20px; display: flex; justify-content: flex-end; }
</style>
