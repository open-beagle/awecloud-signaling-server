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
        <el-table-column prop="domain" :label="$t('domain.domain')" min-width="220" />
        <el-table-column prop="type" :label="$t('domain.type')" width="130">
          <template #default="{ row }">
            <el-tag size="small" :type="getDomainTypeTag(row.type)">
              {{ getDomainTypeLabel(row.type) }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column :label="$t('domain.user')" min-width="120">
          <template #default="{ row }">
            <router-link v-if="row.user_name" :to="`/users/${row.user_id}`" class="user-link">
              {{ row.user_name }}
            </router-link>
            <span v-else>-</span>
          </template>
        </el-table-column>
        <el-table-column prop="target_ip" :label="$t('domain.targetIP')" width="140">
          <template #default="{ row }">
            {{ row.target_ip || '-' }}
          </template>
        </el-table-column>
        <el-table-column prop="target_port" :label="$t('domain.targetPort')" width="100">
          <template #default="{ row }">
            {{ row.target_port || '-' }}
          </template>
        </el-table-column>
        <el-table-column :label="$t('common.status')" width="100">
          <template #default="{ row }">
            <el-tag :type="row.status === 'online' ? 'success' : 'info'" size="small">
              {{ row.status === 'online' ? $t('common.online') : $t('common.offline') }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="updated_at" :label="$t('domain.updatedAt')" width="180" />
        <el-table-column :label="$t('common.action')" width="100" fixed="right">
          <template #default="{ row }">
            <el-popconfirm :title="$t('common.deleteConfirm')" @confirm="handleDelete(row)">
              <template #reference>
                <el-button type="danger" link size="small">{{ $t('common.delete') }}</el-button>
              </template>
            </el-popconfirm>
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
import { getDomains, deleteDomain, type DomainItem, type DomainType } from '@/api/domain'
import { ElMessage } from 'element-plus'

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
  { value: 'ssh', label: t('domain.typeSSH') },
  { value: 'k8sapi', label: t('domain.typeK8SAPI') },
  { value: 'k8ssvc', label: t('domain.typeK8SSVC') }
])

// 域名类型标签颜色
const getDomainTypeTag = (type: DomainType) => {
  const map: Record<string, string> = {
    ssh: 'success',
    k8sapi: 'warning',
    k8ssvc: ''
  }
  return map[type] || 'info'
}

// 域名类型标签文字
const getDomainTypeLabel = (type: DomainType) => {
  const map: Record<string, string> = {
    ssh: t('domain.typeSSH'),
    k8sapi: t('domain.typeK8SAPI'),
    k8ssvc: t('domain.typeK8SSVC')
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

// 删除域名
const handleDelete = async (row: DomainItem) => {
  try {
    const res = await deleteDomain(row.id)
    if (res.success) {
      ElMessage.success(t('common.deleteSuccess'))
      fetchDomains()
    }
  } catch (error) {
    console.error('删除域名失败:', error)
  }
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

.user-link {
  color: var(--el-color-primary);
  text-decoration: none;
}

.user-link:hover {
  text-decoration: underline;
}
</style>
