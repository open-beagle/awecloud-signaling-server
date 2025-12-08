<template>
  <div class="stcp-visitor-list">
    <el-card>
      <template #header>
        <div class="card-header">
          <span class="card-title">STCP访问列表</span>
          <el-button type="primary" :icon="Plus" @click="handleCreate">
            创建
          </el-button>
        </div>
      </template>

      <!-- 筛选条件 -->
      <el-form :inline="true" :model="queryParams" class="filter-form">
        <el-form-item label="Agent">
          <el-select v-model="queryParams.agent_name" placeholder="全部Agent" clearable @change="fetchList" style="width: 200px">
            <el-option v-for="agent in agents" :key="agent.agent_name" :label="agent.agent_name"
              :value="agent.agent_name" />
          </el-select>
        </el-form-item>
        <el-form-item label="状态">
          <el-select v-model="queryParams.enabled" placeholder="全部状态" clearable @change="fetchList" style="width: 150px">
            <el-option label="已启用" :value="true" />
            <el-option label="已禁用" :value="false" />
          </el-select>
        </el-form-item>
        <el-form-item>
          <el-button type="primary" @click="fetchList">查询</el-button>
        </el-form-item>
      </el-form>

      <!-- 列表 -->
      <el-table :data="list" v-loading="loading" stripe>
        <el-table-column prop="id" label="ID" width="80" />
        <el-table-column prop="visitor_name" label="访问名称" min-width="150" />
        <el-table-column prop="agent_name" label="所属Agent" min-width="120" />
        <el-table-column prop="server_name" label="目标服务" min-width="150" />
        <el-table-column label="绑定地址" min-width="180">
          <template #default="{ row }">
            {{ row.bind_addr }}:{{ row.bind_port }}
          </template>
        </el-table-column>
        <el-table-column prop="description" label="描述" min-width="150" show-overflow-tooltip />
        <el-table-column label="状态" width="100">
          <template #default="{ row }">
            <el-tag :type="row.enabled ? 'success' : 'info'" size="small">
              {{ row.enabled ? '已启用' : '已禁用' }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="创建时间" width="100">
          <template #default="{ row }">
            <TimeAgo :time="row.created_at" />
          </template>
        </el-table-column>
        <el-table-column label="操作" width="200" fixed="right">
          <template #default="{ row }">
            <el-tooltip :content="row.enabled ? '禁用' : '启用'" placement="top">
              <el-button
                v-if="!row.enabled"
                type="success"
                size="small"
                :icon="CircleCheck"
                @click="handleEnable(row)"
              />
              <el-button
                v-else
                type="warning"
                size="small"
                :icon="CircleClose"
                @click="handleDisable(row)"
              />
            </el-tooltip>
            <el-tooltip content="编辑" placement="top">
              <el-button
                type="primary"
                size="small"
                :icon="Edit"
                @click="handleEdit(row)"
              />
            </el-tooltip>
            <el-tooltip content="删除" placement="top">
              <el-button
                type="danger"
                size="small"
                :icon="Delete"
                @click="handleDelete(row)"
              />
            </el-tooltip>
          </template>
        </el-table-column>
      </el-table>
    </el-card>

    <!-- 创建对话框 -->
    <CreateDialog v-model="createDialogVisible" @success="fetchList" />

    <!-- 编辑对话框 -->
    <EditDialog v-model="editDialogVisible" :visitor="currentVisitor" @success="fetchList" />
  </div>
</template>

<script setup>
import { ref, reactive, onMounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Plus, Delete, Edit, CircleCheck, CircleClose } from '@element-plus/icons-vue'
import { getSTCPVisitors, enableSTCPVisitor, disableSTCPVisitor, deleteSTCPVisitor } from '@/api/stcp-visitor'
import { getAgents } from '@/api/agent'
import TimeAgo from '@/components/Common/TimeAgo.vue'
import CreateDialog from './components/CreateDialog.vue'
import EditDialog from './components/EditDialog.vue'

const loading = ref(false)
const list = ref([])
const agents = ref([])
const createDialogVisible = ref(false)
const editDialogVisible = ref(false)
const currentVisitor = ref(null)

const queryParams = reactive({
  agent_name: '',
  enabled: null
})

// 获取列表
const fetchList = async () => {
  loading.value = true
  try {
    const params = {}
    if (queryParams.agent_name) params.agent_name = queryParams.agent_name
    if (queryParams.enabled !== null) params.enabled = queryParams.enabled

    const res = await getSTCPVisitors(params)
    list.value = res.data || []
  } catch (error) {
    ElMessage.error('获取STCP访问列表失败')
  } finally {
    loading.value = false
  }
}

// 获取Agent列表
const fetchAgents = async () => {
  try {
    const res = await getAgents()
    agents.value = res.data || []
  } catch (error) {
    console.error('获取Agent列表失败', error)
  }
}

// 新建
const handleCreate = () => {
  createDialogVisible.value = true
}

// 编辑
const handleEdit = (row) => {
  currentVisitor.value = { ...row }
  editDialogVisible.value = true
}

// 启用
const handleEnable = async (row) => {
  try {
    await enableSTCPVisitor(row.id)
    ElMessage.success('启用成功')
    fetchList()
  } catch (error) {
    ElMessage.error(error.response?.data?.error || '启用失败')
  }
}

// 禁用
const handleDisable = async (row) => {
  try {
    await disableSTCPVisitor(row.id)
    ElMessage.success('禁用成功')
    fetchList()
  } catch (error) {
    ElMessage.error(error.response?.data?.error || '禁用失败')
  }
}

// 删除
const handleDelete = async (row) => {
  try {
    await ElMessageBox.confirm(
      `确定要删除STCP访问"${row.visitor_name}"吗？`,
      '删除确认',
      {
        confirmButtonText: '确定',
        cancelButtonText: '取消',
        type: 'warning'
      }
    )
      await deleteSTCPVisitor(row.id)
    await deleteSTCPVisitor(row.id)
    ElMessage.success('删除成功')
    fetchList()
  } catch (error) {
    if (error !== 'cancel') {
      ElMessage.error(error.response?.data?.error || '删除失败')
    }
  }
}

onMounted(() => {
  fetchList()
  fetchAgents()
})
</script>

<style scoped>
.stcp-visitor-list {
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

.filter-form {
  margin-bottom: 20px;
}
</style>
