<template>
  <div class="platform-page">
    <PageHeader title="用户模拟" description="以目标用户在指定 Tenant 或 Provider 的实际权限浏览和排障；模拟不会继承操作者的平台权限。">
      <template #actions><el-button v-if="canCreate" type="primary" :icon="Plus" @click="openCreate">开始模拟</el-button><el-button :icon="Refresh" :loading="loading" @click="load">刷新</el-button></template>
    </PageHeader>

    <el-alert class="safety-alert" title="模拟期间每个管理请求都重新校验目标用户权限，并记录真实操作者、实际身份和模拟会话 ID；嵌套模拟与旧管理写入口会被拒绝。" type="warning" show-icon :closable="false" />
    <el-alert v-if="errorMessage" class="state-alert" title="用户模拟记录加载失败" :description="errorMessage" type="error" show-icon :closable="false" />

    <section class="data-surface">
      <el-table v-loading="loading" :data="items" stripe>
        <el-table-column label="模拟会话" min-width="260"><template #default="{ row }"><strong class="mono">{{ row.id }}</strong><span class="secondary">Request {{ row.created_request_id }}</span></template></el-table-column>
        <el-table-column label="真实 / 实际身份" min-width="190"><template #default="{ row }"><strong>{{ row.actor_user_id }} → {{ row.effective_user_id }}</strong><span class="secondary">User ID</span></template></el-table-column>
        <el-table-column label="目标 Scope" min-width="230"><template #default="{ row }"><strong>{{ scopeTypeLabel(row.scope_type) }}</strong><span class="secondary mono">{{ row.scope_id }}</span></template></el-table-column>
        <el-table-column label="原因" min-width="230" prop="reason" show-overflow-tooltip />
        <el-table-column label="状态" width="105"><template #default="{ row }"><el-tag size="small" :type="statusTag(row.status)">{{ statusLabel(row.status) }}</el-tag></template></el-table-column>
        <el-table-column label="有效窗口" width="210"><template #default="{ row }">{{ formatTime(row.started_at) }}<span class="secondary">至 {{ formatTime(row.expires_at) }}</span></template></el-table-column>
        <el-table-column label="权限 Revision" width="135" align="center"><template #default="{ row }">{{ row.permission_revision }}</template></el-table-column>
      </el-table>
      <el-empty v-if="!loading && !errorMessage && items.length === 0" description="暂无用户模拟记录" />
    </section>

    <el-dialog v-model="dialogVisible" title="开始用户模拟" width="620px" :close-on-click-modal="false">
      <el-form label-position="top">
        <el-form-item label="目标用户" required>
          <el-select v-if="userOptions.length" v-model="form.effective_user_id" filterable style="width: 100%" placeholder="选择统一 User">
            <el-option v-for="user in userOptions" :key="user.id" :label="`${user.alias || user.name} · User ${user.id}`" :value="user.id" />
          </el-select>
          <el-input-number v-else v-model="form.effective_user_id" :min="1" :precision="0" controls-position="right" style="width: 100%" placeholder="输入统一 User ID" />
          <div class="form-hint">Server 会确认目标用户在所选 Scope 中存在有效管理成员关系。</div>
        </el-form-item>
        <div class="scope-row">
          <el-form-item label="工作域" required>
            <el-select v-model="form.scope_type" style="width: 100%" @change="form.scope_id = ''"><el-option label="租户" value="tenant" /><el-option label="资源" value="provider" /></el-select>
          </el-form-item>
          <el-form-item label="目标 Scope" required>
            <el-select v-if="scopeOptions.length" v-model="form.scope_id" filterable style="width: 100%" placeholder="选择目标 Scope"><el-option v-for="scope in scopeOptions" :key="scope.scope_id" :label="scope.scope_name || scope.scope_key || scope.scope_id" :value="scope.scope_id" /></el-select>
            <el-input v-else v-model="form.scope_id" maxlength="36" placeholder="输入目标 Scope ID" />
            <div v-if="!scopeOptions.length" class="form-hint">操作者没有该工作域成员目录时可输入精确 ID；Server 仍会验证目标用户资格。</div>
          </el-form-item>
        </div>
        <el-form-item label="到期时间" required><el-date-picker v-model="form.expires_at" type="datetime" style="width: 100%" :disabled-date="disablePastDate" placeholder="选择模拟结束时间" /></el-form-item>
        <el-form-item label="排障原因" required><el-input v-model="form.reason" type="textarea" :rows="3" maxlength="500" show-word-limit placeholder="说明必须模拟该用户的业务原因" /></el-form-item>
      </el-form>
      <template #footer><el-button @click="dialogVisible = false">取消</el-button><el-button type="primary" :loading="submitting" :disabled="!canSubmit" @click="submit">确认并进入目标 Scope</el-button></template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { Plus, Refresh } from '@element-plus/icons-vue'
import { useRouter } from 'vue-router'
import PageHeader from '@/components/Common/PageHeader.vue'
import { getUsers, type User } from '@/api/user'
import { createUserSimulation, getUserSimulations, revokeUserSimulation, type UserSimulationScopeType, type UserSimulationSession, type UserSimulationStatus } from '@/api/userSimulation'
import { useWorkspaceStore, workspaceHome } from '@/stores/workspace'

const router = useRouter()
const workspaceStore = useWorkspaceStore()
const loading = ref(false)
const submitting = ref(false)
const errorMessage = ref('')
const items = ref<UserSimulationSession[]>([])
const userOptions = ref<User[]>([])
const dialogVisible = ref(false)
const form = reactive<{ effective_user_id?: number; scope_type: UserSimulationScopeType; scope_id: string; expires_at?: Date; reason: string }>({ effective_user_id: undefined, scope_type: 'tenant', scope_id: '', expires_at: undefined, reason: '' })
const canCreate = computed(() => workspaceStore.can('platform.user_simulations.write') && !workspaceStore.isSimulationActive)
const scopeOptions = computed(() => workspaceStore.contexts.filter(item => item.scope_type === form.scope_type && item.scope_id))
const canSubmit = computed(() => canCreate.value && !!form.effective_user_id && !!form.scope_id && !!form.expires_at && form.expires_at.getTime() > Date.now() && !!form.reason.trim())

const load = async () => {
  loading.value = true
  errorMessage.value = ''
  try {
    const response = await getUserSimulations()
    items.value = response.success && response.data ? response.data : []
  } catch {
    items.value = []
    errorMessage.value = '请确认用户模拟读取权限和服务状态后重试。'
  } finally {
    loading.value = false
  }
}

const loadUsers = async () => {
  if (!workspaceStore.can('platform.identities.read')) return
  try {
    const response = await getUsers({ role: 'client', enabled: 'true', page: 1, size: 100 })
    userOptions.value = response.success && response.data ? response.data : []
  } catch {
    userOptions.value = []
  }
}

const openCreate = () => {
  Object.assign(form, { effective_user_id: undefined, scope_type: 'tenant', scope_id: '', expires_at: new Date(Date.now() + 60 * 60 * 1000), reason: '' })
  dialogVisible.value = true
  loadUsers()
}

const submit = async () => {
  if (!canSubmit.value || !form.effective_user_id || !form.expires_at) return
  submitting.value = true
  let created: UserSimulationSession | undefined
  try {
    const response = await createUserSimulation({ effective_user_id: form.effective_user_id, scope_type: form.scope_type, scope_id: form.scope_id, reason: form.reason.trim(), expires_at: form.expires_at.toISOString() })
    if (!response.success || !response.data) throw new Error('创建用户模拟失败')
    created = response.data
    const target = userOptions.value.find(item => item.id === form.effective_user_id)
    await workspaceStore.activateSimulation(created, target?.alias || target?.name || '')
    dialogVisible.value = false
    ElMessage.warning('已进入用户模拟，Header 将持续显示真实/实际身份和到期时间。')
    await router.push(workspaceHome(created.scope_type))
  } catch {
    if (created && !workspaceStore.isSimulationActive) {
      await revokeUserSimulation(created.id, created.row_version, '前端未能建立模拟上下文，自动撤销').catch(() => undefined)
    }
  } finally {
    submitting.value = false
  }
}

const disablePastDate = (date: Date) => date.getTime() < new Date().setHours(0, 0, 0, 0)
const scopeTypeLabel = (type: UserSimulationScopeType) => type === 'tenant' ? '租户' : '资源'
const statusLabel = (status: UserSimulationStatus) => ({ active: '进行中', revoked: '已撤销', expired: '已到期' }[status])
const statusTag = (status: UserSimulationStatus) => ({ active: 'warning', revoked: 'danger', expired: 'info' }[status] as any)
const formatTime = (value?: string) => value ? new Date(value).toLocaleString('zh-CN', { hour12: false }) : '-'

onMounted(load)
</script>

<style scoped>
.platform-page { width: 100%; }
.safety-alert, .state-alert { margin-bottom: 14px; }
.data-surface { overflow: hidden; border: 1px solid var(--border-light); border-radius: 6px; background: #fff; }
.secondary { display: block; margin-top: 3px; color: var(--text-secondary); font-size: 12px; }
.mono { font-family: 'SFMono-Regular', Consolas, 'Liberation Mono', monospace; font-size: 12px; }
.scope-row { display: grid; grid-template-columns: 160px minmax(0, 1fr); gap: 12px; }
.form-hint { margin-top: 5px; color: var(--text-secondary); font-size: 12px; }
</style>
