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
        <el-descriptions-item v-if="user?.role === 'agent'" :label="$t('user.sshEnabled')">
          <el-tag :type="user?.ssh_enabled ? 'success' : 'info'" size="small">
            {{ user?.ssh_enabled ? $t('common.enabled') : $t('common.disabled') }}
          </el-tag>
        </el-descriptions-item>
        <el-descriptions-item :label="$t('common.createdAt')">{{ formatTime(user?.created_at) }}</el-descriptions-item>
      </el-descriptions>

      <!-- 操作按钮 -->
      <div class="action-buttons">
        <el-button type="warning" @click="handleRegenerateSecret">
          {{ $t('user.regenerateSecret') }}
        </el-button>
        <el-button type="danger" @click="handleDelete">
          {{ $t('common.delete') }}
        </el-button>
      </div>
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
            <el-tag :type="row.online ? 'success' : 'info'" size="small">
              {{ row.online ? $t('common.online') : $t('common.offline') }}
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

    <!-- 编辑弹窗 -->
    <el-dialog v-model="showEditDialog" :title="$t('user.edit')" width="500px">
      <el-form ref="editFormRef" :model="editForm" label-width="100px">
        <el-form-item :label="$t('user.alias')">
          <el-input v-model="editForm.alias" :placeholder="$t('user.aliasPlaceholder')" />
        </el-form-item>
        <el-form-item v-if="user?.role === 'agent'" :label="$t('user.sshEnabled')">
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
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, onMounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { useI18n } from 'vue-i18n'
import { getUser, updateUser, deleteUser, regenerateUserSecret, type User, type UserDetail } from '@/api/user'
import { getNodesByUser, type Node } from '@/api/node'
import { formatTime } from '@/utils/time'

const { t } = useI18n()
const route = useRoute()
const router = useRouter()

const loading = ref(false)
const saving = ref(false)
const user = ref<UserDetail | null>(null)
const nodes = ref<Node[]>([])
const showEditDialog = ref(false)
const showSecretDialog = ref(false)
const newSecret = ref('')

const editForm = reactive({
  alias: '',
  ssh_enabled: false
})

// 获取用户详情
const fetchUser = async () => {
  const id = Number(route.params.id)
  if (!id) return

  loading.value = true
  try {
    const res = await getUser(id)
    if (res.success && res.data) {
      user.value = res.data
      editForm.alias = res.data.alias || ''
      editForm.ssh_enabled = res.data.ssh_enabled || false
    }
  } catch (error) {
    console.error('获取用户详情失败:', error)
  } finally {
    loading.value = false
  }
}

// 获取设备列表
const fetchNodes = async () => {
  const id = Number(route.params.id)
  if (!id) return

  try {
    const res = await getNodesByUser(id)
    if (res.success && res.data) {
      nodes.value = res.data
    }
  } catch (error) {
    console.error('获取设备列表失败:', error)
  }
}

// 保存编辑
const handleSave = async () => {
  if (!user.value) return

  saving.value = true
  try {
    const res = await updateUser(user.value.id, editForm)
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
    const res = await regenerateUserSecret(user.value.id)
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
    const res = await deleteUser(user.value.id)
    if (res.success) {
      ElMessage.success(t('common.deleteSuccess'))
      router.push('/users')
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

onMounted(() => {
  fetchUser()
  fetchNodes()
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

.card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.action-buttons {
  margin-top: 20px;
  display: flex;
  gap: 10px;
}

.secret-box {
  margin-top: 15px;
}
</style>
