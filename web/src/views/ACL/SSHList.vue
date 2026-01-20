<template>
  <div class="acl-ssh-list">
    <!-- 搜索 -->
    <el-card class="search-card" shadow="never">
      <el-form :inline="true" :model="searchForm" class="search-form">
        <el-form-item :label="$t('common.search')">
          <el-input v-model="searchForm.search" :placeholder="$t('acl.searchSSHPlaceholder')" clearable style="width: 240px" @keyup.enter="handleSearch" />
        </el-form-item>
        <el-form-item>
          <el-button type="primary" @click="handleSearch">{{ $t('common.search') }}</el-button>
          <el-button @click="handleReset">{{ $t('common.reset') }}</el-button>
        </el-form-item>
      </el-form>
    </el-card>

    <!-- SSH 列表 -->
    <el-card shadow="never">
      <el-table v-loading="loading" :data="sshList" stripe>
        <el-table-column prop="name" :label="$t('acl.agentName')" min-width="150">
          <template #default="{ row }">
            <el-link type="primary" :underline="false" @click="handleView(row)">{{ row.name }}</el-link>
          </template>
        </el-table-column>
        <el-table-column prop="alias" :label="$t('user.alias')" min-width="120" />
        <el-table-column prop="ssh_enabled" :label="$t('acl.sshEnabled')" width="100" align="center">
          <template #default="{ row }">
            <el-tag :type="row.ssh_enabled ? 'success' : 'info'" size="small">
              {{ row.ssh_enabled ? $t('common.enabled') : $t('common.disabled') }}
            </el-tag>
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

      <!-- 分页 -->
      <div class="pagination-wrapper">
        <el-pagination
          v-model:current-page="pagination.page"
          v-model:page-size="pagination.size"
          :total="pagination.total"
          :page-sizes="[10, 20, 50, 100]"
          layout="total, sizes, prev, pager, next, jumper"
          @size-change="fetchSSHList"
          @current-change="fetchSSHList"
        />
      </div>
    </el-card>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { getSSHACLList, type SSHACLItem } from '@/api/acl'
import { formatTime } from '@/utils/time'

const router = useRouter()

const loading = ref(false)
const sshList = ref<SSHACLItem[]>([])

const searchForm = reactive({
  search: ''
})

const pagination = reactive({
  page: 1,
  size: 20,
  total: 0
})

// 获取 SSH 列表
const fetchSSHList = async () => {
  loading.value = true
  try {
    const res = await getSSHACLList({
      search: searchForm.search || undefined,
      page: pagination.page,
      size: pagination.size
    })
    if (res.success && res.data) {
      sshList.value = res.data
      pagination.total = res.total
    }
  } catch (error) {
    console.error('获取 SSH 列表失败:', error)
  } finally {
    loading.value = false
  }
}

// 搜索
const handleSearch = () => {
  pagination.page = 1
  fetchSSHList()
}

// 重置
const handleReset = () => {
  searchForm.search = ''
  pagination.page = 1
  fetchSSHList()
}

// 查看详情
const handleView = (row: SSHACLItem) => {
  router.push(`/acl/ssh/${row.id}`)
}

onMounted(() => {
  fetchSSHList()
})
</script>

<style scoped>
.acl-ssh-list {
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
</style>
