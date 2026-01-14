<template>
  <div class="client-detail">
    <el-card v-loading="loading">
      <template #header>
        <div class="card-header">
          <span class="page-title">客户详情</span>
          <el-button type="primary" :icon="Refresh" @click="loadAllData">
            刷新
          </el-button>
        </div>
      </template>

      <!-- 基本信息 -->
      <div class="info-section">
        <div class="section-header">
          <h3>基本信息</h3>
          <el-button size="small" :icon="Edit" @click="showEditDialog">
            编辑
          </el-button>
        </div>
        <div class="info-grid">
          <div class="info-item">
            <label>用户名:</label>
            <span>{{ clientDetail?.name }}</span>
          </div>
          <div class="info-item">
            <label>别名:</label>
            <span>{{ clientDetail?.alias || '-' }}</span>
          </div>
          <div class="info-item">
            <label>创建时间:</label>
            <span>{{ formatTime(clientDetail?.created_at) }}</span>
          </div>
        </div>
      </div>

      <!-- 所属分组 -->
      <div class="info-section">
        <div class="section-header">
          <h3>所属分组</h3>
        </div>
        <div class="groups-container" v-if="groups.length > 0">
          <el-tag
            v-for="group in groups"
            :key="group.id"
            type="info"
            class="group-tag"
          >
            {{ group.name }}
          </el-tag>
        </div>
        <div v-else class="empty-text">暂无分组</div>
      </div>

      <!-- 设备 -->
      <div class="info-section">
        <div class="section-header">
          <h3>设备 ({{ desktops.length }})</h3>
        </div>
        <el-table :data="desktops" stripe v-if="desktops.length > 0">
          <el-table-column prop="device_name" label="设备名称" min-width="150" />
          <el-table-column prop="tunnel_ip" label="隧道IP" width="150">
            <template #default="{ row }">
              <span>{{ row.tunnel_ip || '-' }}</span>
            </template>
          </el-table-column>
          <el-table-column label="状态" width="100">
            <template #default="{ row }">
              <el-tag :type="row.status === 'online' ? 'success' : 'info'" size="small">
                {{ row.status === 'online' ? '在线' : '离线' }}
              </el-tag>
            </template>
          </el-table-column>
          <el-table-column label="最后在线" width="120">
            <template #default="{ row }">
              <TimeAgo v-if="row.last_online" :time="row.last_online" />
              <span v-else>-</span>
            </template>
          </el-table-column>
          <el-table-column label="操作" width="120" fixed="right">
            <template #default="{ row }">
              <el-tooltip v-if="row.status === 'online'" content="注销" placement="top">
                <el-button
                  size="small"
                  type="warning"
                  :icon="SwitchButton"
                  @click="handleLogoutDesktop(row)"
                />
              </el-tooltip>
              <el-tooltip content="删除" placement="top">
                <el-button
                  size="small"
                  type="danger"
                  :icon="Delete"
                  @click="handleDeleteDesktop(row)"
                />
              </el-tooltip>
            </template>
          </el-table-column>
        </el-table>
        <div v-else class="empty-text">暂无设备</div>
      </div>

      <!-- 已授权服务 -->
      <div class="info-section">
        <div class="section-header">
          <h3>已授权服务 ({{ services.length }})</h3>
        </div>
        <el-table :data="services" stripe v-if="services.length > 0">
          <el-table-column prop="name" label="服务名称" min-width="150" />
          <el-table-column prop="agent_name" label="所属Agent" width="120" />
          <el-table-column prop="listen_addr" label="访问地址" min-width="180" />
          <el-table-column label="授权方式" width="120">
            <template #default="{ row }">
              <el-tag :type="row.auth_type === '单独授权' ? 'primary' : 'warning'" size="small">
                {{ row.auth_type }}
              </el-tag>
            </template>
          </el-table-column>
          <el-table-column label="授权时间" width="120">
            <template #default="{ row }">
              <TimeAgo :time="row.granted_at" />
            </template>
          </el-table-column>
        </el-table>
        <div v-else class="empty-text">暂无已授权服务</div>
      </div>
    </el-card>

    <!-- 编辑对话框 -->
    <el-dialog v-model="editDialogVisible" title="编辑客户" width="500px">
      <el-form :model="editForm" label-width="80px">
        <el-form-item label="用户名">
          <el-input :model-value="clientDetail?.name" disabled />
        </el-form-item>
        <el-form-item label="别名">
          <el-input v-model="editForm.alias" placeholder="请输入别名（可选）" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="editDialogVisible = false">取消</el-button>
        <el-button type="primary" :loading="saving" @click="handleSaveEdit">保存</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Refresh, Edit, Delete, SwitchButton } from '@element-plus/icons-vue'
import { 
  getClientDetail,
  getClientGroups,
  getClientDesktops,
  getClientServices,
  updateClient,
  logoutDesktop,
  deleteDesktop,
  type ClientDetail,
  type ClientGroupItem,
  type Desktop,
  type ServicePermission
} from '@/api/client'
import TimeAgo from '@/components/Common/TimeAgo.vue'

const route = useRoute()
const router = useRouter()

const clientId = Number(route.params.id)
const loading = ref(false)
const clientDetail = ref<ClientDetail | null>(null)
const groups = ref<ClientGroupItem[]>([])
const desktops = ref<Desktop[]>([])
const services = ref<ServicePermission[]>([])

// 编辑对话框
const editDialogVisible = ref(false)
const saving = ref(false)
const editForm = ref({ alias: '' })

const loadClientDetail = async () => {
  try {
    const res = await getClientDetail(clientId)
    if (res.success && res.data) {
      clientDetail.value = res.data
    }
  } catch (error) {
    ElMessage.error('加载客户信息失败')
  }
}

const loadGroups = async () => {
  try {
    const res = await getClientGroups(clientId)
    if (res.success && res.data) {
      groups.value = res.data
    }
  } catch (error) {
    console.error('加载分组失败:', error)
  }
}

const loadDesktops = async () => {
  try {
    const res = await getClientDesktops(clientId)
    if (res.success && res.data) {
      desktops.value = res.data
    }
  } catch (error) {
    console.error('加载客户端失败:', error)
  }
}

const loadServices = async () => {
  try {
    const res = await getClientServices(clientId)
    if (res.success && res.data) {
      services.value = res.data
    }
  } catch (error) {
    console.error('加载服务失败:', error)
  }
}

const loadAllData = async () => {
  loading.value = true
  try {
    await Promise.all([
      loadClientDetail(),
      loadGroups(),
      loadDesktops(),
      loadServices()
    ])
  } finally {
    loading.value = false
  }
}

const showEditDialog = () => {
  if (clientDetail.value) {
    editForm.value = { alias: clientDetail.value.alias || '' }
    editDialogVisible.value = true
  }
}

const handleSaveEdit = async () => {
  saving.value = true
  try {
    const res = await updateClient(clientId, editForm.value)
    if (res.success) {
      ElMessage.success('更新成功')
      editDialogVisible.value = false
      await loadClientDetail()
    } else {
      ElMessage.error(res.message || '更新失败')
    }
  } catch (error) {
    ElMessage.error('更新失败')
  } finally {
    saving.value = false
  }
}

const handleLogoutDesktop = async (desktop: Desktop) => {
  try {
    await ElMessageBox.confirm(`确定要注销设备 "${desktop.device_name}" 吗？设备需要重新认证。`, {
      type: 'warning'
    })
    const res = await logoutDesktop(clientId, desktop.id)
    if (res.success) {
      ElMessage.success('注销成功')
      await loadDesktops()
    } else {
      ElMessage.error(res.message || '注销失败')
    }
  } catch (error: any) {
    if (error !== 'cancel') {
      ElMessage.error('注销失败')
    }
  }
}

const handleDeleteDesktop = async (desktop: Desktop) => {
  try {
    await ElMessageBox.confirm(`确定要删除设备 "${desktop.device_name}" 吗？`, {
      type: 'warning'
    })
    const res = await deleteDesktop(clientId, desktop.id)
    if (res.success) {
      ElMessage.success('删除成功')
      await loadDesktops()
    } else {
      ElMessage.error(res.message || '删除失败')
    }
  } catch (error: any) {
    if (error !== 'cancel') {
      ElMessage.error('删除失败')
    }
  }
}

const formatTime = (time?: string) => {
  if (!time) return '-'
  return new Date(time).toLocaleString('zh-CN')
}

onMounted(() => {
  loadAllData()
})
</script>

<style scoped>
.client-detail {
  width: 100%;
}

.card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.page-title {
  font-size: 18px;
  font-weight: 500;
  color: var(--el-text-color-primary);
}

.info-section {
  margin-bottom: 24px;
}

.info-section:last-child {
  margin-bottom: 0;
}

.section-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 16px;
}

.section-header h3 {
  margin: 0;
  font-size: 16px;
  font-weight: 500;
  color: var(--el-text-color-primary);
}

.info-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(250px, 1fr));
  gap: 16px;
  padding: 16px;
  background: var(--el-fill-color-lighter);
  border-radius: 6px;
}

.info-item {
  display: flex;
  align-items: center;
}

.info-item label {
  font-weight: 500;
  color: var(--el-text-color-regular);
  margin-right: 8px;
  min-width: 80px;
}

.groups-container {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}

.group-tag {
  margin: 0;
}

.empty-text {
  color: var(--el-text-color-secondary);
  font-size: 14px;
}
</style>
