<template>
  <div class="node-list">
    <!-- 搜索和筛选 -->
    <el-card class="search-card" shadow="never">
      <el-form :inline="true" :model="searchForm" class="search-form">
        <el-form-item v-if="!props.fixedType" :label="$t('node.type')">
          <el-select v-model="searchForm.type" :placeholder="$t('common.all')" clearable style="width: 120px">
            <el-option :label="$t('node.typeAgent')" value="agent" />
            <el-option :label="$t('node.typeDesktop')" value="desktop" />
          </el-select>
        </el-form-item>
        <el-form-item :label="$t('common.search')">
          <el-input v-model="searchForm.search" :placeholder="$t('node.searchPlaceholder')" clearable style="width: 240px" @keyup.enter="handleSearch" />
        </el-form-item>
        <el-form-item>
          <el-button type="primary" @click="handleSearch">{{ $t('common.search') }}</el-button>
          <el-button @click="handleReset">{{ $t('common.reset') }}</el-button>
        </el-form-item>
      </el-form>
    </el-card>

    <!-- 设备列表 -->
    <el-card shadow="never">
      <el-table v-loading="loading" :data="nodes" stripe>
        <el-table-column prop="id" label="ID" width="80" />
        <el-table-column prop="name" :label="$t('node.name')" min-width="120">
          <template #default="{ row }">
            <el-link type="primary" @click="goDetail(row)">{{ row.name }}</el-link>
          </template>
        </el-table-column>
        <el-table-column :label="$t('node.user')" min-width="120">
          <template #default="{ row }">
            <router-link v-if="row.user" :to="`/platform-identities/${row.user.id}`" class="user-link">
              {{ getUserDisplayName(row.user) }}
            </router-link>
            <span v-else>-</span>
          </template>
        </el-table-column>
        <el-table-column prop="type" :label="$t('node.type')" width="100">
          <template #default="{ row }">
            <el-tag :type="row.type === 'agent' ? 'success' : 'primary'" size="small">
              {{ row.type === 'agent' ? $t('node.typeAgent') : $t('node.typeDesktop') }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="ip" :label="$t('node.ip')" width="140" />
        <el-table-column prop="hostname" :label="$t('node.hostname')" min-width="120" />
        <el-table-column :label="$t('node.status')" width="100">
          <template #default="{ row }">
            <el-tag :type="isOnline(row) ? 'success' : 'info'" size="small">
              {{ isOnline(row) ? $t('common.online') : $t('common.offline') }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="last_heartbeat" :label="$t('node.lastHeartbeat')" width="180">
          <template #default="{ row }">
            {{ formatTime(row.last_heartbeat) }}
          </template>
        </el-table-column>
        <el-table-column :label="$t('common.actions')" width="120" fixed="right">
          <template #default="{ row }">
            <el-button type="danger" link size="small" @click="handleDelete(row)">{{ $t('common.delete') }}</el-button>
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
          @size-change="fetchNodes"
          @current-change="fetchNodes"
        />
      </div>
    </el-card>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, onMounted, watch } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { useI18n } from 'vue-i18n'
import { getNodes, deleteNode, type Node, type NodeType } from '@/api/node'
import { formatTime } from '@/utils/time'

const props = defineProps<{
  fixedType?: NodeType
}>()

const { t } = useI18n()
const router = useRouter()

const loading = ref(false)
const nodes = ref<Node[]>([])

const searchForm = reactive({
  type: '',
  search: ''
})

const pagination = reactive({
  page: 1,
  size: 20,
  total: 0
})

// 判断是否在线（60秒内有心跳）
const isOnline = (node: Node) => {
  if (!node.last_heartbeat) return false
  const lastHeartbeat = new Date(node.last_heartbeat).getTime()
  const now = Date.now()
  return now - lastHeartbeat < 60000
}

const getUserDisplayName = (user: NonNullable<Node['user']>) => {
  const alias = user.alias?.trim()
  return alias || user.name
}

// 获取设备列表
const fetchNodes = async () => {
  loading.value = true
  try {
    const res = await getNodes({
      type: props.fixedType || searchForm.type || undefined,
      search: searchForm.search || undefined,
      page: pagination.page,
      size: pagination.size
    })
    if (res.success && res.data) {
      nodes.value = res.data
      pagination.total = res.total
    }
  } catch (error) {
    console.error('获取设备列表失败:', error)
  } finally {
    loading.value = false
  }
}

// 搜索
const handleSearch = () => {
  pagination.page = 1
  fetchNodes()
}

// 重置
const handleReset = () => {
  if (!props.fixedType) {
    searchForm.type = ''
  }
  searchForm.search = ''
  pagination.page = 1
  fetchNodes()
}

// 跳转到详情页
const goDetail = (row: Node) => {
  router.push({ path: `/nodes/${row.id}`, query: { name: row.name, type: row.type } })
}

// 删除
const handleDelete = async (row: Node) => {
  try {
    await ElMessageBox.confirm(
      t('node.deleteConfirm', { name: row.name }),
      t('common.warning'),
      { type: 'warning' }
    )
    const res = await deleteNode(row.id)
    if (res.success) {
      ElMessage.success(t('common.deleteSuccess'))
      fetchNodes()
    } else {
      ElMessage.error(res.message || t('common.deleteFailed'))
    }
  } catch (error) {
    if (error !== 'cancel') {
      console.error('删除设备失败:', error)
    }
  }
}

onMounted(() => {
  fetchNodes()
})

watch(() => props.fixedType, () => {
  pagination.page = 1
  fetchNodes()
})
</script>

<style scoped>
.node-list {
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
