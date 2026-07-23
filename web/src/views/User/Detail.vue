<template>
  <div class="user-detail">
    <!-- 基本信息 -->
    <el-card v-loading="loading" class="info-card" shadow="never">
      <template #header>
        <div class="card-header">
          <span>{{ $t('user.basicInfo') }}</span>
          <el-button type="primary" size="small" @click="showEditDialog = true">
            {{ $t('common.edit') }}
          </el-button>
        </div>
      </template>

      <el-descriptions :column="2" border>
        <el-descriptions-item label="ID">{{ user?.id }}</el-descriptions-item>
        <el-descriptions-item :label="$t('user.name')">{{ user?.name }}</el-descriptions-item>
        <el-descriptions-item :label="$t('user.alias')">{{ user?.alias || '-' }}</el-descriptions-item>
        <el-descriptions-item :label="$t('user.role')">
          <el-tag :type="user?.role === 'agent' ? 'success' : 'primary'" size="small">
            {{ user?.role === 'agent' ? $t('user.roleAgent') : $t('user.roleClient') }}
          </el-tag>
        </el-descriptions-item>
        <el-descriptions-item :label="$t('user.sshEnabled')">
          <el-tag :type="user?.ssh_enabled ? 'success' : 'info'" size="small">
            {{ user?.ssh_enabled ? $t('common.enabled') : $t('common.disabled') }}
          </el-tag>
        </el-descriptions-item>
        <el-descriptions-item :label="$t('common.createdAt')">{{ formatTime(user?.created_at) }}</el-descriptions-item>
      </el-descriptions>
    </el-card>

    <!-- 设备列表 -->
    <el-card class="nodes-card" shadow="never">
      <template #header>
        <span>{{ $t('user.nodes') }}</span>
      </template>

      <el-table :data="nodes" stripe>
        <el-table-column prop="id" label="ID" width="80" />
        <el-table-column prop="name" :label="$t('node.name')" min-width="120" />
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
            <el-tag :type="row.status === 'online' ? 'success' : 'info'" size="small">
              {{ row.status === 'online' ? $t('common.online') : $t('common.offline') }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="last_heartbeat" :label="$t('node.lastHeartbeat')" width="180">
          <template #default="{ row }">
            {{ formatTime(row.last_heartbeat) }}
          </template>
        </el-table-column>
      </el-table>
    </el-card>

    <!-- 部署历史 -->
    <el-card class="deploy-history-card" shadow="never">
      <template #header>
        <div class="card-header">
          <span>{{ $t('user.deployHistory') }}</span>
          <el-button type="primary" size="small" @click="showDeployDialog = true">
            {{ $t('user.deploy') }}
          </el-button>
        </div>
      </template>

      <!-- Agent 部署历史 -->
      <el-table v-if="user?.role === 'agent'" v-loading="deployLoading" :data="deployTokens" stripe>
        <el-table-column prop="name" :label="$t('agent.tokenName')" min-width="120" />
        <el-table-column :label="$t('common.status')" width="100">
          <template #default="{ row }">
            <el-tag :type="getDeployStatusType(row.status)" size="small">
              {{ getDeployStatusText(row.status) }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="created_by_name" :label="$t('common.createdBy')" width="100" />
        <el-table-column :label="$t('common.createdAt')" width="180">
          <template #default="{ row }">
            {{ formatTime(row.created_at) }}
          </template>
        </el-table-column>
        <el-table-column :label="$t('agent.boundAt')" width="180">
          <template #default="{ row }">
            {{ row.bound_at ? formatTime(row.bound_at) : '-' }}
          </template>
        </el-table-column>
        <el-table-column :label="$t('common.actions')" width="150" fixed="right">
          <template #default="{ row }">
            <el-button
              size="small"
              @click="handleViewCommand(row)"
            >
              {{ $t('common.view') }}
            </el-button>
            <el-button
              size="small"
              type="danger"
              @click="handleRevokeDeployToken(row)"
            >
              {{ $t('common.delete') }}
            </el-button>
          </template>
        </el-table-column>
      </el-table>

      <!-- Client Token 历史 -->
      <el-table v-else-if="user?.role === 'client'" v-loading="tokensLoading" :data="tokens" stripe>
        <el-table-column :label="$t('common.status')" width="100">
          <template #default="{ row }">
            <el-tag :type="getTokenStatusType(row.status)" size="small">
              {{ getTokenStatusText(row.status) }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="name" :label="$t('clientToken.tokenName')" min-width="120" />
        <el-table-column prop="created_by_name" :label="$t('common.createdBy')" width="100" />
        <el-table-column :label="$t('common.createdAt')" width="180">
          <template #default="{ row }">
            {{ formatTime(row.created_at) }}
          </template>
        </el-table-column>
        <el-table-column :label="$t('clientToken.boundAt')" width="180">
          <template #default="{ row }">
            {{ row.bound_at ? formatTime(row.bound_at) : '-' }}
          </template>
        </el-table-column>
        <el-table-column :label="$t('common.actions')" width="150" fixed="right">
          <template #default="{ row }">
            <el-button
              size="small"
              @click="handleViewCommand(row)"
            >
              {{ $t('common.view') }}
            </el-button>
            <el-button
              size="small"
              type="danger"
              @click="handleDeleteToken(row)"
            >
              {{ $t('common.delete') }}
            </el-button>
          </template>
        </el-table-column>
      </el-table>
    </el-card>

    <!-- 编辑弹窗 -->
    <el-dialog v-model="showEditDialog" :title="$t('user.edit')" width="500px">
      <el-form ref="editFormRef" :model="editForm" label-width="100px">
        <el-form-item :label="$t('user.alias')">
          <el-input v-model="editForm.alias" :placeholder="$t('user.aliasPlaceholder')" />
        </el-form-item>
        <el-form-item :label="$t('user.sshEnabled')">
          <el-switch v-model="editForm.ssh_enabled" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="showEditDialog = false">{{ $t('common.cancel') }}</el-button>
        <el-button type="primary" :loading="saving" @click="handleSave">{{ $t('common.confirm') }}</el-button>
      </template>
    </el-dialog>

    <!-- 密钥弹窗 -->
    <el-dialog v-model="showSecretDialog" :title="$t('user.newSecret')" width="500px">
      <el-alert type="warning" :closable="false" show-icon>
        <template #title>{{ $t('user.secretWarning') }}</template>
      </el-alert>
      <div class="secret-box">
        <el-input v-model="newSecret" readonly>
          <template #append>
            <el-button @click="copySecret">{{ $t('common.copy') }}</el-button>
          </template>
        </el-input>
      </div>
      <template #footer>
        <el-button type="primary" @click="showSecretDialog = false">{{ $t('common.close') }}</el-button>
      </template>
    </el-dialog>

    <!-- 部署弹窗（Agent 和 Client 通用） -->
    <DeployDialog v-model="showDeployDialog" :user="user" @success="loadDeployHistory" />

    <!-- 查看部署命令弹窗 -->
    <el-dialog v-model="showCommandDialog" :title="$t('user.deployCommand')" width="600px">
      <el-alert type="info" :closable="false" show-icon style="margin-bottom: 16px">
        <template #title>{{ $t('user.deployCommandTip') }}</template>
      </el-alert>
      <div v-if="deployCommand" class="command-box">
        <pre>{{ deployCommand }}</pre>
        <el-button type="primary" @click="copyCommand">{{ $t('common.copy') }}</el-button>
      </div>
      <div v-else class="loading-box">
        <el-icon class="is-loading"><Loading /></el-icon>
        <span>{{ $t('common.loading') }}</span>
      </div>
      <template #footer>
        <el-button @click="showCommandDialog = false">{{ $t('common.close') }}</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, onMounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Loading } from '@element-plus/icons-vue'
import { useI18n } from 'vue-i18n'
import { getUser, updateUser, deleteUser, regenerateUserSecret, type User, type UserDetail, type UserNode } from '@/api/user'
import { getDeployTokens, revokeDeployToken, getDeployCommand, type DeployToken } from '@/api/deployToken'
import { formatTime } from '@/utils/time'
import DeployDialog from './components/DeployDialog.vue'

const { t } = useI18n()
const route = useRoute()
const router = useRouter()

const loading = ref(false)
const saving = ref(false)
const user = ref<UserDetail | null>(null)
const nodes = ref<UserNode[]>([])
const showEditDialog = ref(false)
const showSecretDialog = ref(false)
const newSecret = ref('')

// Client Token 相关
const showTokenDialog = ref(false)
const tokensLoading = ref(false)
const tokens = ref<DeployToken[]>([])

// Agent 部署相关
const showDeployDialog = ref(false)
const deployLoading = ref(false)
const deployTokens = ref<DeployToken[]>([])

// 查看部署命令
const showCommandDialog = ref(false)
const deployCommand = ref('')

const editForm = reactive({
  alias: '',
  ssh_enabled: false
})

// 获取用户详情
const fetchUser = async () => {
  const username = route.params.username as string
  if (!username) return

  loading.value = true
  try {
    const res = await getUser(username)
    if (res.success && res.data) {
      user.value = res.data
      nodes.value = res.data.nodes || []
      editForm.alias = res.data.alias || ''
      editForm.ssh_enabled = res.data.ssh_enabled || false
      
      // 如果是 client 用户，加载 Token 列表
      if (res.data.role === 'client') {
        loadTokens()
      }
      
      // 如果是 agent 用户，加载部署历史
      if (res.data.role === 'agent') {
        loadDeployHistory()
      }
    }
  } catch (error) {
    console.error('获取用户详情失败:', error)
  } finally {
    loading.value = false
  }
}

// 加载 Token 列表
const loadTokens = async () => {
  if (!user.value) return
  tokensLoading.value = true
  try {
    const res = await getDeployTokens(user.value.name)
    if (res.success && res.data) {
      tokens.value = res.data
    }
  } catch (error) {
    console.error('加载 Token 列表失败:', error)
  } finally {
    tokensLoading.value = false
  }
}

// 删除 Token
const handleDeleteToken = async (token: DeployToken) => {
  try {
    await ElMessageBox.confirm(
      t('clientToken.deleteConfirm'),
      t('common.warning'),
      { type: 'warning' }
    )
    const res = await revokeDeployToken(token.id)
    if (res.success) {
      ElMessage.success(t('common.deleteSuccess'))
      loadTokens()
    } else {
      ElMessage.error(res.message || t('common.deleteFailed'))
    }
  } catch (error: any) {
    if (error !== 'cancel') {
      console.error('删除 Token 失败:', error)
      ElMessage.error(t('common.deleteFailed'))
    }
  }
}

// Token 状态类型
const getTokenStatusType = (status: string) => {
  switch (status) {
    case 'pending': return 'warning'
    case 'bound': return 'success'
    default: return 'info'
  }
}

// Token 状态文本
const getTokenStatusText = (status: string) => {
  switch (status) {
    case 'pending': return t('clientToken.statusPending')
    case 'bound': return t('clientToken.statusBound')
    default: return status
  }
}

// 加载部署历史
const loadDeployHistory = async () => {
  if (!user.value) return
  deployLoading.value = true
  try {
    const res = await getDeployTokens(user.value.name)
    if (res.success && res.data) {
      deployTokens.value = res.data
    }
  } catch (error) {
    console.error('加载部署历史失败:', error)
  } finally {
    deployLoading.value = false
  }
}

// 撤销部署 Token
const handleRevokeDeployToken = async (token: DeployToken) => {
  try {
    await ElMessageBox.confirm(
      t('agent.revokeConfirm'),
      t('common.warning'),
      { type: 'warning' }
    )
    const res = await revokeDeployToken(token.id)
    if (res.success) {
      ElMessage.success(t('common.deleteSuccess'))
      loadDeployHistory()
    } else {
      ElMessage.error(res.message || t('common.deleteFailed'))
    }
  } catch (error: any) {
    if (error !== 'cancel') {
      console.error('撤销部署 Token 失败:', error)
      ElMessage.error(t('common.deleteFailed'))
    }
  }
}

// 部署状态类型
const getDeployStatusType = (status: string) => {
  switch (status) {
    case 'pending': return 'warning'
    case 'bound': return 'success'
    case 'expired': return 'info'
    case 'revoked': return 'danger'
    default: return 'info'
  }
}

// 部署状态文本
const getDeployStatusText = (status: string) => {
  switch (status) {
    case 'pending': return t('agent.statusPending')
    case 'bound': return t('agent.statusBound')
    case 'expired': return t('agent.statusExpired')
    case 'revoked': return t('agent.statusRevoked')
    default: return status
  }
}

// 保存编辑
const handleSave = async () => {
  if (!user.value) return

  saving.value = true
  try {
    const res = await updateUser(user.value.name, editForm)
    if (res.success) {
      ElMessage.success(t('common.saveSuccess'))
      showEditDialog.value = false
      fetchUser()
    } else {
      ElMessage.error(res.message || t('common.saveFailed'))
    }
  } catch (error) {
    console.error('保存失败:', error)
    ElMessage.error(t('common.saveFailed'))
  } finally {
    saving.value = false
  }
}

// 重新生成密钥
const handleRegenerateSecret = async () => {
  if (!user.value) return

  try {
    await ElMessageBox.confirm(
      t('user.regenerateSecretConfirm'),
      t('common.warning'),
      { type: 'warning' }
    )
    const res = await regenerateUserSecret(user.value.name)
    if (res.success && res.data) {
      newSecret.value = res.data.secret
      showSecretDialog.value = true
    } else {
      ElMessage.error(res.message || t('common.operationFailed'))
    }
  } catch (error) {
    if (error !== 'cancel') {
      console.error('重新生成密钥失败:', error)
    }
  }
}

// 删除用户
const handleDelete = async () => {
  if (!user.value) return

  try {
    await ElMessageBox.confirm(
      t('user.deleteConfirm', { name: user.value.name }),
      t('common.warning'),
      { type: 'warning' }
    )
    const res = await deleteUser(user.value.name)
    if (res.success) {
      ElMessage.success(t('common.deleteSuccess'))
      router.push('/platform-identities')
    } else {
      ElMessage.error(res.message || t('common.deleteFailed'))
    }
  } catch (error) {
    if (error !== 'cancel') {
      console.error('删除用户失败:', error)
    }
  }
}

// 复制密钥
const copySecret = async () => {
  try {
    await navigator.clipboard.writeText(newSecret.value)
    ElMessage.success(t('common.copySuccess'))
  } catch (error) {
    ElMessage.error(t('common.copyFailed'))
  }
}

// 查看部署命令
const handleViewCommand = async (token: DeployToken) => {
  showCommandDialog.value = true
  deployCommand.value = ''
  
  try {
    const res = await getDeployCommand(token.id)
    if (res.success && res.data) {
      deployCommand.value = res.data.install_command || res.data.env_config || ''
    } else {
      ElMessage.error(res.message || t('common.loadFailed'))
      showCommandDialog.value = false
    }
  } catch (error) {
    console.error('获取部署命令失败:', error)
    ElMessage.error(t('common.loadFailed'))
    showCommandDialog.value = false
  }
}

// 复制部署命令
const copyCommand = async () => {
  try {
    await navigator.clipboard.writeText(deployCommand.value)
    ElMessage.success(t('common.copySuccess'))
  } catch (error) {
    ElMessage.error(t('common.copyFailed'))
  }
}

onMounted(() => {
  fetchUser()
})
</script>

<style scoped>
.user-detail {
  width: 100%;
}

.info-card {
  margin-bottom: 20px;
}

.nodes-card {
  margin-bottom: 20px;
}

.tokens-card {
  margin-bottom: 20px;
}

.card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.secret-box {
  margin-top: 15px;
}

.command-box {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.command-box pre {
  background: var(--el-fill-color-light);
  padding: 12px;
  border-radius: 4px;
  overflow-x: auto;
  margin: 0;
  font-family: 'Courier New', Courier, monospace;
  font-size: 13px;
  line-height: 1.5;
  white-space: pre-wrap;
  word-break: break-all;
}

.loading-box {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 8px;
  padding: 40px 0;
  color: var(--el-text-color-secondary);
}
</style>
