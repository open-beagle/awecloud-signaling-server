<template>
  <div class="acl-k8s-service-list">
    <el-card class="search-card" shadow="never">
      <el-form :inline="true" :model="searchForm" class="search-form">
        <el-form-item :label="$t('common.search')">
          <el-input v-model="searchForm.search" :placeholder="$t('acl.searchK8SServicePlaceholder')" clearable style="width: 240px" @keyup.enter="handleSearch" />
        </el-form-item>
        <el-form-item>
          <el-button type="primary" @click="handleSearch">{{ $t('common.search') }}</el-button>
          <el-button @click="handleReset">{{ $t('common.reset') }}</el-button>
        </el-form-item>
      </el-form>
    </el-card>

    <el-card shadow="never">
      <el-table v-loading="loading" :data="list" stripe>
        <el-table-column prop="name" :label="$t('acl.agentName')" min-width="150">
          <template #default="{ row }">
            <el-link type="primary" :underline="false" @click="handleView(row)">{{ row.name }}</el-link>
          </template>
        </el-table-column>
        <el-table-column prop="alias" :label="$t('user.alias')" min-width="120" />
        <el-table-column prop="role" :label="$t('user.role')" width="100" align="center">
          <template #default="{ row }">
            <el-tag :type="row.role === 'agent' ? 'success' : 'primary'" size="small">
              {{ row.role === 'agent' ? $t('user.roleAgent') : $t('user.roleClient') }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="user_count" :label="$t('acl.userCount')" width="100" align="center">
          <template #default="{ row }"><el-tag type="primary" size="small">{{ row.user_count }}</el-tag></template>
        </el-table-column>
        <el-table-column prop="group_count" :label="$t('acl.groupCount')" width="100" align="center">
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
import { getK8SServiceACLList, type K8SServiceACLItem } from '@/api/aclK8sService'
import { formatTime } from '@/utils/time'

const router = useRouter()
const loading = ref(false)
const list = ref<K8SServiceACLItem[]>([])
const searchForm = reactive({ search: '' })
const pagination = reactive({ page: 1, size: 20, total: 0 })

const fetchList = async () => {
  loading.value = true
  try {
    const res = await getK8SServiceACLList({ search: searchForm.search || undefined, page: pagination.page, size: pagination.size })
    if (res.success && res.data) { list.value = res.data; pagination.total = res.total }
  } catch (error) { console.error('获取 K8S Service 授权列表失败:', error) }
  finally { loading.value = false }
}

const handleSearch = () => { pagination.page = 1; fetchList() }
const handleReset = () => { searchForm.search = ''; pagination.page = 1; fetchList() }
const handleView = (row: K8SServiceACLItem) => { router.push({ path: `/acl/k8s-service/${row.id}`, query: { name: row.name } }) }
onMounted(() => { fetchList() })
</script>

<style scoped>
.acl-k8s-service-list { width: 100%; }
.search-card { margin-bottom: 20px; }
.search-form { display: flex; flex-wrap: wrap; align-items: center; }
.pagination-wrapper { margin-top: 20px; display: flex; justify-content: flex-end; }
</style>
