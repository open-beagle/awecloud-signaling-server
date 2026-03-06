<template>
  <div class="acl-k8s-unified-list">
    <!-- 搜索 -->
    <el-card class="search-card" shadow="never">
      <el-form :inline="true" :model="searchForm" class="search-form">
        <el-form-item :label="$t('acl.type')">
          <el-select v-model="searchForm.type" style="width: 140px" @change="handleSearch">
            <el-option :label="$t('common.all')" value="all" />
            <el-option :label="$t('acl.typeAgent')" value="agent" />
            <el-option :label="$t('acl.typeEndpoint')" value="endpoint" />
          </el-select>
        </el-form-item>
        <el-form-item :label="$t('common.search')">
          <el-input v-model="searchForm.search" :placeholder="$t('acl.searchK8SPlaceholder')" clearable style="width: 240px" @keyup.enter="handleSearch" />
        </el-form-item>
        <el-form-item>
          <el-button type="primary" @click="handleSearch">{{ $t('common.search') }}</el-button>
          <el-button @click="handleReset">{{ $t('common.reset') }}</el-button>
        </el-form-item>
      </el-form>
    </el-card>

    <!-- 列表 -->
    <el-card shadow="never">
      <el-table v-loading="loading" :data="list" stripe>
        <el-table-column prop="name" :label="$t('acl.clusterName')" min-width="150">
          <template #default="{ row }">
            <el-link type="primary" :underline="false" @click="handleView(row)">{{ row.name }}</el-link>
          </template>
        </el-table-column>
        <el-table-column prop="alias" :label="$t('user.alias')" min-width="120" />
        <el-table-column :label="$t('acl.provider')" min-width="180">
          <template #default="{ row }">
            <el-tag :type="row.provider_type === 'agent' ? '' : 'warning'" size="small">
              {{ row.provider_type === 'agent' ? $t('acl.typeAgent') : $t('acl.typeEndpoint') }}
            </el-tag>
            <span v-if="row.provider_name" style="margin-left: 8px">{{ row.provider_name }}</span>
          </template>
        </el-table-column>
        <el-table-column prop="user_count" :label="$t('acl.userCount')" width="100" align="center">
          <template #default="{ row }">
            <el-tag type="primary" size="small">{{ row.user_count }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="group_count" :label="$t('acl.groupCount')" width="100" align="center">
          <template #default="{ row }">
            <el-tag type="success" size="small">{{ row.group_count }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="created_at" :label="$t('common.createdAt')" width="180">
          <template #default="{ row }">
            {{ formatTime(row.created_at) }}
          </template>
        </el-table-column>
        <el-table-column :label="$t('common.actions')" width="120" fixed="right">
          <template #default="{ row }">
            <el-button type="primary" link size="small" @click="handleView(row)">{{ $t('acl.manageAuth') }}</el-button>
          </template>
        </el-table-column>
      </el-table>

      <div class="pagination-wrapper">
        <el-pagination
          v-model:current-page="pagination.page"
          v-model:page-size="pagination.size"
          :total="pagination.total"
          :page-sizes="[10, 20, 50, 100]"
          layout="total, sizes, prev, pager, next, jumper"
          @size-change="fetchList"
          @current-change="fetchList"
        />
      </div>
    </el-card>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { getK8SUnifiedACLList, type K8SUnifiedACLItem } from '@/api/aclK8sUnified'
import { formatTime } from '@/utils/time'

const router = useRouter()
const loading = ref(false)
const list = ref<K8SUnifiedACLItem[]>([])
const searchForm = reactive({ type: 'all', search: '' })
const pagination = reactive({ page: 1, size: 20, total: 0 })

const fetchList = async () => {
  loading.value = true
  try {
    const res = await getK8SUnifiedACLList({
      type: searchForm.type !== 'all' ? searchForm.type : undefined,
      search: searchForm.search || undefined,
      page: pagination.page,
      size: pagination.size
    })
    if (res.success && res.data) {
      list.value = res.data
      pagination.total = res.total
    }
  } catch (error) {
    console.error('获取 K8S API 授权合并列表失败:', error)
  } finally {
    loading.value = false
  }
}

const handleSearch = () => { pagination.page = 1; fetchList() }
const handleReset = () => { searchForm.type = 'all'; searchForm.search = ''; pagination.page = 1; fetchList() }

const handleView = (row: K8SUnifiedACLItem) => {
  if (row.provider_type === 'agent') {
    // Agent 提供 → 跳转到 Agent K8S 授权详情页
    router.push({ path: `/acl/k8s/${row.provider_id}`, query: { name: row.name } })
  } else {
    // Endpoint 提供 → 跳转到 Endpoint K8SAPI 授权详情页
    router.push({ path: `/acl/endpoint-k8sapi/${row.provider_id}`, query: { name: row.provider_name } })
  }
}

onMounted(() => { fetchList() })
</script>

<style scoped>
.acl-k8s-unified-list { width: 100%; }
.search-card { margin-bottom: 20px; }
.search-form { display: flex; flex-wrap: wrap; align-items: center; }
.pagination-wrapper { margin-top: 20px; display: flex; justify-content: flex-end; }
</style>
