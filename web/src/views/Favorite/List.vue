<template>
  <div class="favorite-list">
    <el-card>
      <template #header>
        <div class="card-header">
          <span class="card-title">收藏管理</span>
          <el-button type="primary" :icon="Refresh" @click="loadFavorites" :loading="loading">
            刷新
          </el-button>
        </div>
      </template>

      <!-- 统计信息 -->
      <el-row :gutter="20" class="stats-row">
        <el-col :span="8">
          <el-statistic title="总收藏数" :value="totalCount">
            <template #prefix>
              <el-icon><Star /></el-icon>
            </template>
          </el-statistic>
        </el-col>
        <el-col :span="8">
          <el-statistic title="用户数" :value="uniqueClients">
            <template #prefix>
              <el-icon><User /></el-icon>
            </template>
          </el-statistic>
        </el-col>
        <el-col :span="8">
          <el-statistic title="服务数" :value="uniqueServices">
            <template #prefix>
              <el-icon><Connection /></el-icon>
            </template>
          </el-statistic>
        </el-col>
      </el-row>

      <!-- 搜索栏 -->
      <el-form :inline="true" class="search-form">
        <el-form-item label="用户">
          <el-input v-model="searchQuery.client" placeholder="搜索用户" clearable />
        </el-form-item>
        <el-form-item label="服务">
          <el-input v-model="searchQuery.service" placeholder="搜索服务" clearable />
        </el-form-item>
        <el-form-item label="Agent">
          <el-input v-model="searchQuery.agent" placeholder="搜索Agent" clearable />
        </el-form-item>
        <el-form-item>
          <el-button type="primary" @click="handleSearch">搜索</el-button>
          <el-button @click="handleReset">重置</el-button>
        </el-form-item>
      </el-form>

      <!-- 收藏列表 -->
      <el-table
        :data="filteredFavorites"
        v-loading="loading"
        stripe
      >
        <el-table-column prop="id" label="ID" width="80" />
        <el-table-column prop="client_name" label="用户" min-width="150" />
        <el-table-column prop="instance_name" label="服务名称" min-width="150" />
        <el-table-column prop="agent_name" label="Agent" min-width="120" />
        <el-table-column prop="local_port" label="本地端口" width="120">
          <template #default="{ row }">
            <el-tag v-if="row.local_port > 0" type="success">{{ row.local_port }}</el-tag>
            <el-tag v-else type="info">默认</el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="created_at" label="创建时间" min-width="160" />
        <el-table-column prop="updated_at" label="更新时间" min-width="160" />
      </el-table>

      <!-- 分页 -->
      <el-pagination
        v-if="filteredFavorites.length > 0"
        class="pagination"
        :current-page="currentPage"
        :page-size="pageSize"
        :page-sizes="[10, 20, 50, 100]"
        :total="filteredFavorites.length"
        layout="total, sizes, prev, pager, next, jumper"
        @size-change="handleSizeChange"
        @current-change="handleCurrentChange"
      />
    </el-card>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import { Refresh, Star, User, Connection } from '@element-plus/icons-vue'
import request from '@/utils/request'

const loading = ref(false)
const favorites = ref([])
const totalCount = ref(0)
const currentPage = ref(1)
const pageSize = ref(20)

const searchQuery = ref({
  client: '',
  service: '',
  agent: ''
})

// 统计信息
const uniqueClients = computed(() => {
  const clients = new Set(favorites.value.map(f => f.client_id))
  return clients.size
})

const uniqueServices = computed(() => {
  const services = new Set(favorites.value.map(f => f.stcp_instance_id))
  return services.size
})

// 过滤后的收藏列表
const filteredFavorites = computed(() => {
  let result = favorites.value

  if (searchQuery.value.client) {
    const query = searchQuery.value.client.toLowerCase()
    result = result.filter(f => 
      f.client_name.toLowerCase().includes(query)
    )
  }

  if (searchQuery.value.service) {
    const query = searchQuery.value.service.toLowerCase()
    result = result.filter(f => 
      f.instance_name.toLowerCase().includes(query)
    )
  }

  if (searchQuery.value.agent) {
    const query = searchQuery.value.agent.toLowerCase()
    result = result.filter(f => 
      f.agent_name.toLowerCase().includes(query)
    )
  }

  return result
})

// 加载收藏列表
const loadFavorites = async () => {
  loading.value = true
  try {
    // 注意：request拦截器已经返回了response.data，所以这里的data就是API返回的数据
    const data = await request.get('/api/v1/admin/favorites')
    
    // 检查响应是否有效
    if (!data) {
      console.error('Invalid response:', data)
      ElMessage.error('服务器响应无效')
      return
    }
    
    if (data.success) {
      favorites.value = data.favorites || []
      totalCount.value = data.total_count || 0
      ElMessage.success('加载成功')
    } else {
      ElMessage.error(data.message || '加载失败')
    }
  } catch (error) {
    console.error('加载收藏列表失败:', error)
    ElMessage.error('加载收藏列表失败')
  } finally {
    loading.value = false
  }
}

// 搜索
const handleSearch = () => {
  currentPage.value = 1
}

// 重置
const handleReset = () => {
  searchQuery.value = {
    client: '',
    service: '',
    agent: ''
  }
  currentPage.value = 1
}

// 分页
const handleSizeChange = (val) => {
  pageSize.value = val
  currentPage.value = 1
}

const handleCurrentChange = (val) => {
  currentPage.value = val
}

onMounted(() => {
  loadFavorites()
})
</script>

<style scoped>
.favorite-list {
  width: 100%;
}

.card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.card-title {
  font-size: 18px;
  font-weight: 500;
  color: var(--text-primary);
}

.stats-row {
  margin-bottom: 20px;
}

.search-form {
  margin-bottom: 20px;
}

.pagination {
  margin-top: 20px;
  display: flex;
  justify-content: flex-end;
}
</style>
