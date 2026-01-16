<template>
  <div class="ssh-policy">
    <el-card>
      <template #header>
        <div class="card-header">
          <span class="card-title">SSH 策略</span>
          <el-button :icon="Refresh" @click="loadSSHRules">刷新</el-button>
        </div>
      </template>

      <el-alert type="info" :closable="false" style="margin-bottom: 16px">
        <template #title>
          <div style="display: flex; align-items: center; gap: 8px">
            <el-icon><InfoFilled /></el-icon>
            <span>只读视图</span>
          </div>
        </template>
        此页面展示 Headscale 中实际生效的 SSH 规则。要修改 SSH 授权，请前往「SSH 管理」页面。
      </el-alert>

      <div v-loading="loading">
        <el-table :data="sshRules" stripe border>
          <el-table-column type="index" label="#" width="60" />
          <el-table-column label="来源" min-width="200">
            <template #default="{ row }">
              <el-tag v-for="src in row.src" :key="src" size="small" style="margin: 2px">
                {{ src }}
              </el-tag>
            </template>
          </el-table-column>
          <el-table-column label="目标" min-width="200">
            <template #default="{ row }">
              <el-tag v-for="dst in row.dst" :key="dst" size="small" type="info" style="margin: 2px">
                {{ dst }}
              </el-tag>
            </template>
          </el-table-column>
          <el-table-column label="SSH 用户" min-width="180">
            <template #default="{ row }">
              <el-tag v-for="user in row.users" :key="user" size="small" type="warning" style="margin: 2px">
                {{ user }}
              </el-tag>
            </template>
          </el-table-column>
          <el-table-column prop="action" label="动作" width="100">
            <template #default="{ row }">
              <el-tag :type="row.action === 'accept' ? 'success' : 'warning'" size="small">
                {{ row.action === 'accept' ? '允许' : row.action }}
              </el-tag>
            </template>
          </el-table-column>
        </el-table>

        <div v-if="sshRules.length === 0 && !loading" class="empty-tip">
          <el-empty description="暂无 SSH 规则">
            <el-button type="primary" @click="goToSSHManagement">前往 SSH 管理</el-button>
          </el-empty>
        </div>
      </div>

      <div class="sync-info" v-if="lastSyncedAt">
        <el-icon><Clock /></el-icon>
        最后同步时间: {{ formatDate(lastSyncedAt) }}
      </div>
    </el-card>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import { Refresh, InfoFilled, Clock } from '@element-plus/icons-vue'
import { getTunnelACLRules, type SSHRule } from '@/api/tunnel'

const router = useRouter()

const loading = ref(false)
const sshRules = ref<SSHRule[]>([])
const lastSyncedAt = ref<string>('')

const formatDate = (dateStr: string) => {
  if (!dateStr) return '-'
  const date = new Date(dateStr)
  return date.toLocaleString('zh-CN')
}

const loadSSHRules = async () => {
  loading.value = true
  try {
    const res = await getTunnelACLRules()
    if (res.success && res.data) {
      sshRules.value = res.data.ssh_rules || []
      lastSyncedAt.value = res.data.last_synced_at || ''
    }
  } catch (error) {
    ElMessage.error('加载 SSH 规则失败')
  } finally {
    loading.value = false
  }
}

const goToSSHManagement = () => {
  router.push('/ssh')
}

onMounted(() => {
  loadSSHRules()
})
</script>

<style scoped>
.card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.card-title {
  font-size: 16px;
  font-weight: 500;
}

.empty-tip {
  padding: 40px 0;
  text-align: center;
}

.sync-info {
  margin-top: 16px;
  padding: 12px;
  background: #f5f7fa;
  border-radius: 4px;
  font-size: 13px;
  color: #606266;
  display: flex;
  align-items: center;
  gap: 8px;
}
</style>
