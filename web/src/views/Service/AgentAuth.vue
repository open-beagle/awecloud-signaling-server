<template>
  <div class="agent-auth-container">
    <el-card>
      <template #header>
        <div class="card-header">
          <span>Agent授权管理</span>
          <el-button type="primary" @click="handleAddAuth">
            <el-icon><Plus /></el-icon>
            添加授权
          </el-button>
        </div>
      </template>

      <!-- 说明提示 -->
      <el-alert
        title="说明"
        type="info"
        :closable="false"
        style="margin-bottom: 20px"
      >
        <template #default>
          <div>此列表仅显示跨组或无分组 Agent 的显式授权记录。同组 Agent 默认可互访所有端口，无需显式授权。</div>
        </template>
      </el-alert>

      <!-- 筛选区域 -->
      <el-form :inline="true" class="filter-form">
        <el-form-item label="目标服务">
          <el-select v-model="filters.serviceId" placeholder="选择目标服务" clearable style="width: 200px">
            <el-option label="全部服务" :value="null" />
            <el-option
              v-for="service in serviceList"
              :key="service.id"
              :label="service.name"
              :value="service.id"
            />
          </el-select>
        </el-form-item>
        <el-form-item label="访问Agent">
          <el-select v-model="filters.agentId" placeholder="选择访问Agent" clearable style="width: 200px">
            <el-option label="全部Agent" :value="null" />
            <el-option
              v-for="agent in agentList"
              :key="agent.id"
              :label="agent.agent_name"
              :value="agent.id"
            />
          </el-select>
        </el-form-item>
        <el-form-item>
          <el-button type="primary" @click="handleSearch">查询</el-button>
          <el-button @click="handleReset">重置</el-button>
        </el-form-item>
      </el-form>

      <!-- 授权列表 -->
      <el-table :data="filteredAuthList" v-loading="loading" border>
        <el-table-column prop="id" label="ID" width="80" />
        <el-table-column label="访问方Agent" min-width="150">
          <template #default="{ row }">
            <div>{{ row.agent_name }}</div>
            <div class="text-secondary">{{ row.agent_ip }}</div>
          </template>
        </el-table-column>
        <el-table-column label="目标服务" min-width="200">
          <template #default="{ row }">
            <div>{{ row.service_name }}</div>
            <div class="text-secondary">{{ row.service_addr }}</div>
          </template>
        </el-table-column>
        <el-table-column prop="granted_at" label="授权时间" width="180">
          <template #default="{ row }">
            {{ formatTime(row.granted_at) }}
          </template>
        </el-table-column>
        <el-table-column label="操作" width="120" fixed="right">
          <template #default="{ row }">
            <el-button type="danger" size="small" @click="handleRevoke(row)">
              撤销
            </el-button>
          </template>
        </el-table-column>
      </el-table>
    </el-card>

    <!-- 添加授权对话框 -->
    <el-dialog v-model="dialogVisible" title="添加Agent授权" width="600px">
      <el-form :model="form" label-width="120px">
        <el-form-item label="访问方Agent" required>
          <el-select v-model="form.agentId" placeholder="选择访问方Agent" style="width: 100%">
            <el-option
              v-for="agent in agentList"
              :key="agent.id"
              :label="`${agent.agent_name} (${agent.tailscale_ip || '未连接'}) ${agent.group_name ? '- ' + agent.group_name : '- 无分组'}`"
              :value="agent.id"
            />
          </el-select>
          <div class="form-tip">选择需要访问目标服务的 Agent</div>
        </el-form-item>
        <el-form-item label="要访问的服务" required>
          <el-select
            v-model="form.serviceIds"
            placeholder="选择要访问的服务（可多选）"
            multiple
            style="width: 100%"
          >
            <el-option
              v-for="service in serviceList"
              :key="service.id"
              :label="`${service.name} (${service.agent_name} / ${service.agent_ts_ip}:${service.listen_port})`"
              :value="service.id"
            />
          </el-select>
          <div class="form-tip">选择要被访问的服务，可以选择多个</div>
        </el-form-item>
        <el-alert
          title="提示"
          type="warning"
          :closable="false"
          style="margin-bottom: 20px"
        >
          <template #default>
            <div>授权后，该 Agent 可以通过 Tailscale 网络访问选中的服务</div>
            <div>权限变更将同步到 Headscale ACL，立即生效</div>
          </template>
        </el-alert>
      </el-form>
      <template #footer>
        <el-button @click="dialogVisible = false">取消</el-button>
        <el-button type="primary" @click="handleSubmit" :loading="submitting">授权</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, onMounted, computed } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Plus } from '@element-plus/icons-vue'
import { getAgentServicePermissions, addAgentServicePermission, removeAgentServicePermission } from '@/api/agentPermission'
import { getAgents } from '@/api/agent'
import { getServices } from '@/api/service'
import type { AgentServicePermission } from '@/api/agentPermission'

// 筛选条件
const filters = reactive({
  serviceId: null as number | null,
  agentId: null as number | null
})

// 授权列表
const authList = ref<AgentServicePermission[]>([])
const loading = ref(false)

// Agent 和服务列表
const agentList = ref<any[]>([])
const serviceList = ref<any[]>([])

// 对话框
const dialogVisible = ref(false)
const submitting = ref(false)
const form = reactive({
  agentId: null as number | null,
  serviceIds: [] as number[]
})

// 过滤后的授权列表
const filteredAuthList = computed(() => {
  let list = authList.value
  
  if (filters.serviceId) {
    list = list.filter(item => item.service_id === filters.serviceId)
  }
  
  if (filters.agentId) {
    list = list.filter(item => item.agent_id === filters.agentId)
  }
  
  return list
})

// 加载授权列表
const loadAuthList = async () => {
  loading.value = true
  try {
    const response = await getAgentServicePermissions()
    if (response.success) {
      authList.value = response.data || []
    } else {
      ElMessage.error(response.message || '加载授权列表失败')
    }
  } catch (error: any) {
    console.error('加载授权列表失败:', error)
    ElMessage.error(error.message || '加载授权列表失败')
  } finally {
    loading.value = false
  }
}

// 加载 Agent 列表
const loadAgentList = async () => {
  try {
    const response = await getAgents()
    if (response.success) {
      agentList.value = response.data || []
    }
  } catch (error) {
    console.error('加载 Agent 列表失败:', error)
  }
}

// 加载服务列表
const loadServiceList = async () => {
  try {
    const response = await getServices()
    if (response.success) {
      serviceList.value = response.data || []
    }
  } catch (error) {
    console.error('加载服务列表失败:', error)
  }
}

// 查询
const handleSearch = () => {
  // 前端过滤，无需重新加载
}

// 重置
const handleReset = () => {
  filters.serviceId = null
  filters.agentId = null
}

// 添加授权
const handleAddAuth = () => {
  form.agentId = null
  form.serviceIds = []
  dialogVisible.value = true
}

// 提交授权
const handleSubmit = async () => {
  if (!form.agentId || form.serviceIds.length === 0) {
    ElMessage.warning('请选择访问方Agent和要访问的服务')
    return
  }

  submitting.value = true
  try {
    const response = await addAgentServicePermission({
      agent_id: form.agentId,
      service_ids: form.serviceIds
    })
    
    if (response.success) {
      ElMessage.success('授权成功')
      dialogVisible.value = false
      loadAuthList()
    } else {
      ElMessage.error(response.message || '授权失败')
    }
  } catch (error: any) {
    console.error('授权失败:', error)
    // 检查是否是 ACL 同步错误
    if (error.response?.data?.message?.includes('ACL') || error.response?.data?.message?.includes('Headscale')) {
      ElMessage.error('权限同步失败，请稍后重试')
    } else {
      ElMessage.error(error.response?.data?.message || error.message || '授权失败')
    }
  } finally {
    submitting.value = false
  }
}

// 撤销授权
const handleRevoke = async (row: AgentServicePermission) => {
  try {
    await ElMessageBox.confirm(
      `确认撤销 ${row.agent_name} 对 ${row.service_name} 的访问权限吗？`,
      '提示',
      {
        type: 'warning',
        confirmButtonText: '确定',
        cancelButtonText: '取消'
      }
    )
    
    const response = await removeAgentServicePermission(row.id)
    if (response.success) {
      ElMessage.success('撤销成功')
      loadAuthList()
    } else {
      ElMessage.error(response.message || '撤销失败')
    }
  } catch (error: any) {
    if (error !== 'cancel') {
      console.error('撤销失败:', error)
      // 检查是否是 ACL 同步错误
      if (error.response?.data?.message?.includes('ACL') || error.response?.data?.message?.includes('Headscale')) {
        ElMessage.error('权限同步失败，请稍后重试')
      } else {
        ElMessage.error(error.response?.data?.message || error.message || '撤销失败')
      }
    }
  }
}

// 格式化时间
const formatTime = (time: string) => {
  if (!time) return '-'
  return new Date(time).toLocaleString('zh-CN')
}

onMounted(() => {
  loadAuthList()
  loadAgentList()
  loadServiceList()
})
</script>

<style scoped>
.agent-auth-container {
  padding: 20px;
}

.card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.filter-form {
  margin-bottom: 20px;
}

.pagination {
  margin-top: 20px;
  display: flex;
  justify-content: flex-end;
}

.text-secondary {
  font-size: 12px;
  color: #909399;
  margin-top: 4px;
}

.form-tip {
  font-size: 12px;
  color: #909399;
  margin-top: 4px;
}
</style>
